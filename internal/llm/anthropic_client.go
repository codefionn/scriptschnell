package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/logger"
)

const (
	defaultAnthropicModel = "claude-haiku-4-5"
)

var defaultAnthropicMaxTokens = consts.DefaultMaxTokens

// AnthropicClient implements the Client interface using the official Anthropic SDK.
type AnthropicClient struct {
	client anthropic.Client
	model  string
}

// NewAnthropicClient creates an Anthropic client backed by the official SDK.
func NewAnthropicClient(apiKey, modelName string) (Client, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, fmt.Errorf("anthropic client requires an API key")
	}

	model := strings.TrimSpace(modelName)
	if model == "" {
		model = defaultAnthropicModel
	}

	return &AnthropicClient{
		client: anthropic.NewClient(option.WithAPIKey(key)),
		model:  model,
	}, nil
}

func (c *AnthropicClient) GetModelName() string {
	return c.model
}

func (c *AnthropicClient) GetLastResponseID() string {
	return "" // Not applicable for Anthropic
}

func (c *AnthropicClient) SetPreviousResponseID(responseID string) {
	// Not applicable for Anthropic
}

func (c *AnthropicClient) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := c.CompleteWithRequest(ctx, &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 1.0,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (c *AnthropicClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	params, err := c.buildMessageParams(req)
	if err != nil {
		return nil, err
	}

	var resp *CompletionResponse
	opErr := c.executeWithRetry(ctx, func() error {
		stream := c.client.Beta.Messages.NewStreaming(ctx, params)
		if stream == nil {
			return fmt.Errorf("anthropic stream failed: no stream returned")
		}
		defer stream.Close()

		var (
			contentBuilder strings.Builder
			toolCalls      []map[string]interface{}
			// temporary storage for the current tool call being built
			currentToolIndex int = -1
			currentToolJSON  strings.Builder
			stopReason       string
			usage            map[string]interface{}
			reasoningBuilder strings.Builder
		)

		for stream.Next() {
			event := stream.Current()

			switch e := event.AsAny().(type) {
			case anthropic.BetaRawMessageStartEvent:
				// Capture usage information from message start event
				usage = map[string]interface{}{
					"input_tokens":  e.Message.Usage.InputTokens,
					"output_tokens": e.Message.Usage.OutputTokens,
				}
				// Include cache-related tokens if available
				if e.Message.Usage.CacheCreationInputTokens > 0 {
					usage["cache_creation_input_tokens"] = e.Message.Usage.CacheCreationInputTokens
				}
				if e.Message.Usage.CacheReadInputTokens > 0 {
					usage["cache_read_input_tokens"] = e.Message.Usage.CacheReadInputTokens
				}
			case anthropic.BetaRawContentBlockStartEvent:
				if e.ContentBlock.Type == "tool_use" {
					currentToolIndex++
					currentToolJSON.Reset()
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   e.ContentBlock.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      e.ContentBlock.Name,
							"arguments": "", // will be filled by deltas
						},
					})
				}
				// Track reasoning/thinking blocks (extended thinking models)
				if e.ContentBlock.Type == "thinking" || e.ContentBlock.Type == "reasoning" {
					// Reset reasoning builder for a new reasoning block
					reasoningBuilder.Reset()
				}
			case anthropic.BetaRawContentBlockDeltaEvent:
				if e.Delta.Type == "text_delta" {
					contentBuilder.WriteString(e.Delta.Text)
				} else if e.Delta.Type == "input_json_delta" {
					if currentToolIndex >= 0 && currentToolIndex < len(toolCalls) {
						currentToolJSON.WriteString(e.Delta.PartialJSON)
					}
				} else if e.Delta.Type == "thinking_delta" || e.Delta.Type == "reasoning_delta" {
					// Capture reasoning/thinking content
					reasoningBuilder.WriteString(e.Delta.Text)
				}
			case anthropic.BetaRawContentBlockStopEvent:
				if currentToolIndex >= 0 && currentToolIndex < len(toolCalls) {
					// Finalize the current tool call arguments
					args := currentToolJSON.String()
					if fn, ok := toolCalls[currentToolIndex]["function"].(map[string]interface{}); ok {
						fn["arguments"] = args
					}
				}
			case anthropic.BetaRawMessageDeltaEvent:
				if e.Delta.StopReason != "" {
					stopReason = string(e.Delta.StopReason)
				}
			}
		}

		if err := stream.Err(); err != nil {
			return fmt.Errorf("anthropic stream failed: %w", err)
		}

		resp = &CompletionResponse{
			Content:    contentBuilder.String(),
			Reasoning:  reasoningBuilder.String(),
			ToolCalls:  toolCalls,
			StopReason: stopReason,
			Usage:      usage,
		}
		return nil
	})

	if opErr != nil {
		return nil, opErr
	}
	return resp, nil
}

func (c *AnthropicClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	params, err := c.buildMessageParams(req)
	if err != nil {
		return err
	}

	return c.executeWithRetry(ctx, func() error {
		stream := c.client.Beta.Messages.NewStreaming(ctx, params)
		if stream == nil {
			return fmt.Errorf("anthropic stream failed: no stream returned")
		}
		defer stream.Close()

		for stream.Next() {
			event := stream.Current()

			deltaEvent, ok := event.AsAny().(anthropic.BetaRawContentBlockDeltaEvent)
			if !ok {
				continue
			}

			if deltaEvent.Delta.Type != "text_delta" {
				continue
			}

			text := deltaEvent.Delta.Text
			if strings.TrimSpace(text) == "" {
				continue
			}

			if err := callback(text); err != nil {
				return err
			}
		}

		if err := stream.Err(); err != nil {
			return fmt.Errorf("anthropic stream failed: %w", err)
		}

		return nil
	})
}

func (c *AnthropicClient) executeWithRetry(ctx context.Context, operation func() error) error {
	maxRetries := consts.DefaultMaxRetries
	baseDelay := 1 * time.Second

	var err error
	for i := 0; i <= maxRetries; i++ {
		err = operation()
		if err == nil {
			return nil
		}

		if !isRateLimitError(err) {
			return err
		}

		if i == maxRetries {
			break
		}

		delay := baseDelay * time.Duration(1<<i)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			continue
		}
	}
	return err
}

func isRateLimitError(err error) bool {
	// Check for anthropic.Error and StatusCode 429
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429
	}
	// Fallback string check
	return strings.Contains(err.Error(), "429")
}

func (c *AnthropicClient) buildMessageParams(req *CompletionRequest) (anthropic.BetaMessageNewParams, error) {
	if req == nil {
		return anthropic.BetaMessageNewParams{}, fmt.Errorf("anthropic completion request cannot be nil")
	}

	// Check if messages have native Anthropic format - if so, use directly for caching
	hasNativeFormat := len(req.Messages) > 0 && req.Messages[0].NativeFormat != nil && req.Messages[0].NativeProvider == "anthropic"
	var systemBlocks []anthropic.BetaTextBlockParam
	var chatMessages []anthropic.BetaMessageParam
	var err error

	if hasNativeFormat {
		// Use native format directly to preserve caching metadata
		logger.Debug("Using native Anthropic message format (%d messages)", len(req.Messages))
		systemBlocks, chatMessages, err = extractNativeAnthropicMessages(req.Messages, req.EnableCaching, req.CacheTTL)
		if err != nil {
			logger.Warn("Failed to extract native messages, falling back to conversion: %v", err)
			// Fall back to conversion
			systemBlocks, chatMessages, err = convertMessagesToAnthropic(req.SystemPrompt, req.Messages, req.EnableCaching, req.CacheTTL)
			if err != nil {
				return anthropic.BetaMessageNewParams{}, err
			}
		}
	} else {
		// Convert from unified format
		systemBlocks, chatMessages, err = convertMessagesToAnthropic(req.SystemPrompt, req.Messages, req.EnableCaching, req.CacheTTL)
		if err != nil {
			return anthropic.BetaMessageNewParams{}, err
		}
	}

	if len(chatMessages) == 0 {
		logger.Warn("anthropic completion request has no user or assistant messages; injecting fallback prompt")
		fallback := buildFallbackAnthropicUserMessage(req.SystemPrompt)
		chatMessages = append(chatMessages, fallback)
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}

	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(maxTokens),
		Messages:  chatMessages,
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if req.Temperature > 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	if len(req.Tools) > 0 {
		params.Tools = convertAnthropicTools(req.Tools, req.EnableCaching, req.CacheTTL)
	}

	if req.EnableCaching {
		params.Betas = append(params.Betas, anthropic.AnthropicBetaPromptCaching2024_07_31)

		// Extended TTL header is required when using 1h caches (the default)
		ttl := strings.TrimSpace(req.CacheTTL)
		if ttl == "" || strings.EqualFold(ttl, "1h") {
			params.Betas = append(params.Betas, anthropic.AnthropicBetaExtendedCacheTTL2025_04_11)
		}
	}

	return params, nil
}

// extractNativeAnthropicMessages extracts native Anthropic messages from Message objects
func extractNativeAnthropicMessages(messages []*Message, enableCaching bool, cacheTTL string) ([]anthropic.BetaTextBlockParam, []anthropic.BetaMessageParam, error) {
	systemBlocks := make([]anthropic.BetaTextBlockParam, 0)
	chatMessages := make([]anthropic.BetaMessageParam, 0, len(messages))

	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			continue
		}

		// Check for system blocks (stored as special map)
		if m, ok := msg.NativeFormat.(map[string]interface{}); ok {
			if systemData, isSystem := m["_anthropic_system"]; isSystem {
				// This is a system block
				data, err := json.Marshal(systemData)
				if err == nil {
					var blocks []anthropic.BetaTextBlockParam
					if err := json.Unmarshal(data, &blocks); err == nil {
						if enableCaching {
							cacheControl := makeCacheControl(cacheTTL)
							for i := range blocks {
								blocks[i].CacheControl = cacheControl
							}
						}
						systemBlocks = append(systemBlocks, blocks...)
					}
				}
				continue
			}
		}

		// Convert native format to BetaMessageParam
		data, err := json.Marshal(msg.NativeFormat)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var betaMsg anthropic.BetaMessageParam
		if err := json.Unmarshal(data, &betaMsg); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal to BetaMessageParam: %w", err)
		}

		// Reapply cache control for requests that toggle it dynamically
		if enableCaching && msg.CacheControl {
			applyCacheControlToBlocks(betaMsg.Content, enableCaching, cacheTTL)
		}

		chatMessages = append(chatMessages, betaMsg)
	}

	return systemBlocks, chatMessages, nil
}

func convertMessagesToAnthropic(systemPrompt string, messages []*Message, enableCaching bool, cacheTTL string) ([]anthropic.BetaTextBlockParam, []anthropic.BetaMessageParam, error) {
	systemBlocks := make([]anthropic.BetaTextBlockParam, 0, 1)
	if sys := strings.TrimSpace(systemPrompt); sys != "" {
		block := anthropic.BetaTextBlockParam{Text: sys}
		// Add cache control to system prompt if caching is enabled
		if enableCaching {
			block.CacheControl = makeCacheControl(cacheTTL)
		}
		systemBlocks = append(systemBlocks, block)
	}

	chatMessages := make([]anthropic.BetaMessageParam, 0, len(messages))
	for idx, msg := range messages {
		if msg == nil {
			continue
		}

		role := normalizeRole(msg.Role)
		switch role {
		case "system":
			if text := strings.TrimSpace(msg.Content); text != "" {
				systemBlocks = append(systemBlocks, anthropic.BetaTextBlockParam{Text: text})
			}
			continue
		case "assistant":
			blocks, err := buildAnthropicAssistantBlocks(msg)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid assistant message at index %d: %w", idx, err)
			}
			if len(blocks) == 0 {
				continue
			}
			chatMessages = append(chatMessages, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleAssistant,
				Content: blocks,
			})
		case "tool":
			userMsg, err := buildAnthropicToolMessage(msg)
			if err != nil {
				return nil, nil, err
			}
			if userMsg.Role != "" {
				chatMessages = append(chatMessages, userMsg)
			}
		default:
			blocks := buildAnthropicTextBlocks(msg.Content)
			if len(blocks) == 0 {
				continue
			}
			if msg.CacheControl {
				applyCacheControlToBlocks(blocks, enableCaching, cacheTTL)
			}
			chatMessages = append(chatMessages, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleUser,
				Content: blocks,
			})
		}
	}

	return systemBlocks, chatMessages, nil
}

func buildAnthropicAssistantBlocks(msg *Message) ([]anthropic.BetaContentBlockParamUnion, error) {
	blocks := make([]anthropic.BetaContentBlockParamUnion, 0, 1+len(msg.ToolCalls))

	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewBetaTextBlock(msg.Content))
	}

	toolBlocks, err := convertAnthropicToolUses(msg.ToolCalls)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, toolBlocks...)

	return blocks, nil
}

func convertAnthropicToolUses(toolCalls []map[string]interface{}) ([]anthropic.BetaContentBlockParamUnion, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	result := make([]anthropic.BetaContentBlockParamUnion, 0, len(toolCalls))
	for idx, call := range toolCalls {
		if call == nil {
			continue
		}

		function, ok := call["function"].(map[string]interface{})
		if !ok || function == nil {
			return nil, fmt.Errorf("tool call %d is missing function details", idx)
		}

		name := strings.TrimSpace(toString(function["name"]))
		if name == "" {
			return nil, fmt.Errorf("tool call %d is missing a function name", idx)
		}

		callID := strings.TrimSpace(toString(call["id"]))
		if callID == "" {
			callID = fmt.Sprintf("tool_call_%d", idx)
		}

		input := parseToolArguments(function["arguments"])
		result = append(result, anthropic.NewBetaToolUseBlock(callID, input, name))
	}

	return result, nil
}

func buildAnthropicToolMessage(msg *Message) (anthropic.BetaMessageParam, error) {
	toolID := strings.TrimSpace(msg.ToolID)
	if toolID == "" {
		// Fall back to sending the result as plain user text if no tool reference exists.
		if msg.Content == "" {
			return anthropic.BetaMessageParam{}, nil
		}
		return anthropic.BetaMessageParam{
			Role:    anthropic.BetaMessageParamRoleUser,
			Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(msg.Content)},
		}, nil
	}

	toolResult := anthropic.BetaToolResultBlockParam{
		ToolUseID: toolID,
	}
	if msg.Content != "" {
		textBlock := anthropic.BetaTextBlockParam{Text: msg.Content}
		toolResult.Content = []anthropic.BetaToolResultBlockParamContentUnion{
			{OfText: &textBlock},
		}
	}

	return anthropic.BetaMessageParam{
		Role: anthropic.BetaMessageParamRoleUser,
		Content: []anthropic.BetaContentBlockParamUnion{
			{OfToolResult: &toolResult},
		},
	}, nil
}

func buildAnthropicTextBlocks(content string) []anthropic.BetaContentBlockParamUnion {
	if content == "" {
		return nil
	}
	return []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(content)}
}

// buildFallbackAnthropicUserMessage creates a minimal user message when the request otherwise lacks any chat turns.
func buildFallbackAnthropicUserMessage(systemPrompt string) anthropic.BetaMessageParam {
	content := strings.TrimSpace(systemPrompt)
	if content == "" {
		content = "Please describe how I can help."
	}

	return anthropic.BetaMessageParam{
		Role: anthropic.BetaMessageParamRoleUser,
		Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock(content),
		},
	}
}

func convertAnthropicTools(tools []map[string]interface{}, enableCaching bool, cacheTTL string) []anthropic.BetaToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	result := make([]anthropic.BetaToolUnionParam, 0, len(tools))
	for idx, raw := range tools {
		if raw == nil {
			continue
		}

		function, ok := raw["function"].(map[string]interface{})
		if !ok || function == nil {
			continue
		}

		name := strings.TrimSpace(toString(function["name"]))
		if name == "" {
			continue
		}

		schema := anthropic.BetaToolInputSchemaParam{
			Type: constant.Object("object"),
		}

		if params, ok := function["parameters"].(map[string]interface{}); ok {
			if props, ok := params["properties"]; ok {
				schema.Properties = props
			}
			if req := extractStringSlice(params["required"]); len(req) > 0 {
				schema.Required = req
			}
			if schemaType, ok := params["type"].(string); ok && schemaType != "" {
				schema.Type = constant.Object(schemaType)
			}
			if extras := copyExtraFields(params, "type", "properties", "required"); len(extras) > 0 {
				schema.ExtraFields = extras
			}
		}

		tool := &anthropic.BetaToolParam{
			Name:        name,
			InputSchema: schema,
			Type:        anthropic.BetaToolTypeCustom,
		}

		if desc := strings.TrimSpace(toString(function["description"])); desc != "" {
			tool.Description = anthropic.String(desc)
		}

		// Add cache control to the last tool definition if caching is enabled
		// This creates a cache breakpoint after all tool definitions
		if enableCaching && idx == len(tools)-1 {
			tool.CacheControl = makeCacheControl(cacheTTL)
		}

		result = append(result, anthropic.BetaToolUnionParam{OfTool: tool})
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func parseToolArguments(raw interface{}) any {
	switch value := raw.(type) {
	case nil:
		return map[string]interface{}{}
	case string:
		if strings.TrimSpace(value) == "" {
			return map[string]interface{}{}
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			return decoded
		}
		return value
	default:
		return value
	}
}

func extractStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func copyExtraFields(src map[string]interface{}, skip ...string) map[string]any {
	if len(src) == 0 {
		return nil
	}
	skipSet := make(map[string]struct{}, len(skip))
	for _, key := range skip {
		skipSet[key] = struct{}{}
	}

	extras := make(map[string]any)
	for key, val := range src {
		if _, shouldSkip := skipSet[key]; shouldSkip {
			continue
		}
		extras[key] = val
	}

	if len(extras) == 0 {
		return nil
	}
	return extras
}

func normalizeRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return "user"
	}
	return role
}

// makeCacheControl creates a cache control parameter with the specified TTL
func makeCacheControl(ttl string) anthropic.BetaCacheControlEphemeralParam {
	cacheControl := anthropic.NewBetaCacheControlEphemeralParam()

	// Set TTL based on configuration (default to 1h for longer sessions)
	switch strings.ToLower(strings.TrimSpace(ttl)) {
	case "5m":
		cacheControl.TTL = anthropic.BetaCacheControlEphemeralTTLTTL5m
	case "1h", "":
		cacheControl.TTL = anthropic.BetaCacheControlEphemeralTTLTTL1h
	default:
		// Default to 1h if unrecognized
		cacheControl.TTL = anthropic.BetaCacheControlEphemeralTTLTTL1h
	}

	return cacheControl
}

// applyCacheControlToBlocks applies cache control metadata to the last text block if enabled.
func applyCacheControlToBlocks(blocks []anthropic.BetaContentBlockParamUnion, enableCaching bool, cacheTTL string) {
	if !enableCaching {
		return
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		if text := blocks[i].OfText; text != nil {
			text.CacheControl = makeCacheControl(cacheTTL)
			return
		}
	}
}
