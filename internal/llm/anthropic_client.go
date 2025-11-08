package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

const (
	defaultAnthropicModel     = "claude-3-5-sonnet-20241022"
	defaultAnthropicMaxTokens = 1024
)

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

	msg, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic completion failed: %w", err)
	}

	return buildAnthropicCompletionResponse(msg), nil
}

func (c *AnthropicClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	params, err := c.buildMessageParams(req)
	if err != nil {
		return err
	}

	stream := c.client.Messages.NewStreaming(ctx, params)
	if stream == nil {
		return fmt.Errorf("anthropic stream failed: no stream returned")
	}
	defer stream.Close()

	for stream.Next() {
		event := stream.Current()

		deltaEvent, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}

		textDelta, ok := deltaEvent.Delta.AsAny().(anthropic.TextDelta)
		if !ok {
			continue
		}

		text := textDelta.Text
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
}

func (c *AnthropicClient) buildMessageParams(req *CompletionRequest) (anthropic.MessageNewParams, error) {
	if req == nil {
		return anthropic.MessageNewParams{}, fmt.Errorf("anthropic completion request cannot be nil")
	}

	systemBlocks, chatMessages, err := convertMessagesToAnthropic(req.SystemPrompt, req.Messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	if len(chatMessages) == 0 {
		return anthropic.MessageNewParams{}, fmt.Errorf("anthropic completion requires at least one user or assistant message")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}

	params := anthropic.MessageNewParams{
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
		params.Tools = convertAnthropicTools(req.Tools)
	}

	return params, nil
}

func buildAnthropicCompletionResponse(msg *anthropic.Message) *CompletionResponse {
	if msg == nil {
		return &CompletionResponse{}
	}

	content := collectAnthropicText(msg.Content)
	toolCalls := convertAnthropicToolCalls(msg.Content)
	stopReason := string(msg.StopReason)
	if stopReason == "" {
		stopReason = msg.StopSequence
	}

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  toolCalls,
		StopReason: stopReason,
	}
}

func convertMessagesToAnthropic(systemPrompt string, messages []*Message) ([]anthropic.TextBlockParam, []anthropic.MessageParam, error) {
	systemBlocks := make([]anthropic.TextBlockParam, 0, 1)
	if sys := strings.TrimSpace(systemPrompt); sys != "" {
		systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: sys})
	}

	chatMessages := make([]anthropic.MessageParam, 0, len(messages))
	for idx, msg := range messages {
		if msg == nil {
			continue
		}

		role := normalizeRole(msg.Role)
		switch role {
		case "system":
			if text := strings.TrimSpace(msg.Content); text != "" {
				systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: text})
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
			chatMessages = append(chatMessages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
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
			chatMessages = append(chatMessages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: blocks,
			})
		}
	}

	return systemBlocks, chatMessages, nil
}

func buildAnthropicAssistantBlocks(msg *Message) ([]anthropic.ContentBlockParamUnion, error) {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(msg.ToolCalls))

	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}

	toolBlocks, err := convertAnthropicToolUses(msg.ToolCalls)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, toolBlocks...)

	return blocks, nil
}

func convertAnthropicToolUses(toolCalls []map[string]interface{}) ([]anthropic.ContentBlockParamUnion, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	result := make([]anthropic.ContentBlockParamUnion, 0, len(toolCalls))
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
		result = append(result, anthropic.NewToolUseBlock(callID, input, name))
	}

	return result, nil
}

func buildAnthropicToolMessage(msg *Message) (anthropic.MessageParam, error) {
	toolID := strings.TrimSpace(msg.ToolID)
	if toolID == "" {
		// Fall back to sending the result as plain user text if no tool reference exists.
		if msg.Content == "" {
			return anthropic.MessageParam{}, nil
		}
		return anthropic.MessageParam{
			Role:    anthropic.MessageParamRoleUser,
			Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)},
		}, nil
	}

	block := anthropic.NewToolResultBlock(toolID, msg.Content, false)
	return anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleUser,
		Content: []anthropic.ContentBlockParamUnion{block},
	}, nil
}

func buildAnthropicTextBlocks(content string) []anthropic.ContentBlockParamUnion {
	if content == "" {
		return nil
	}
	return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(content)}
}

func convertAnthropicTools(tools []map[string]interface{}) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, raw := range tools {
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

		schema := anthropic.ToolInputSchemaParam{
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

		tool := &anthropic.ToolParam{
			Name:        name,
			InputSchema: schema,
			Type:        anthropic.ToolTypeCustom,
		}

		if desc := strings.TrimSpace(toString(function["description"])); desc != "" {
			tool.Description = anthropic.String(desc)
		}

		result = append(result, anthropic.ToolUnionParam{OfTool: tool})
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func collectAnthropicText(blocks []anthropic.ContentBlockUnion) string {
	if len(blocks) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(block.Text)
	}
	return sb.String()
}

func convertAnthropicToolCalls(blocks []anthropic.ContentBlockUnion) []map[string]interface{} {
	if len(blocks) == 0 {
		return nil
	}

	var toolCalls []map[string]interface{}
	for _, block := range blocks {
		if block.Type != "tool_use" {
			continue
		}

		arguments := "{}"
		if len(block.Input) > 0 {
			arguments = string(block.Input)
		}

		call := map[string]interface{}{
			"id":   block.ID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      block.Name,
				"arguments": arguments,
			},
		}
		toolCalls = append(toolCalls, call)
	}

	return toolCalls
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
