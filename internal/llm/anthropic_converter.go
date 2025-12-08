package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// AnthropicConverterImpl implements NativeConverter for Anthropic/Claude models
type AnthropicConverterImpl struct{}

func (c *AnthropicConverterImpl) GetProviderName() string {
	return "anthropic"
}

func (c *AnthropicConverterImpl) GetModelFamily(modelID string) string {
	return extractModelFamily(modelID)
}

func (c *AnthropicConverterImpl) SupportsNativeStorage() bool {
	return true
}

// ConvertToNative converts unified messages to Anthropic BetaMessageParam format
func (c *AnthropicConverterImpl) ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error) {
	// Use existing conversion logic
	systemBlocks, chatMessages, err := convertMessagesToAnthropicNative(systemPrompt, messages, enableCaching, cacheTTL)
	if err != nil {
		return nil, err
	}

	// Store as wrapped native messages
	result := make([]interface{}, 0, len(chatMessages)+1)

	// Store system blocks separately (they're not part of chatMessages)
	if len(systemBlocks) > 0 {
		result = append(result, map[string]interface{}{
			"_anthropic_system": systemBlocks,
		})
	}

	// Convert chatMessages to []interface{}
	for _, msg := range chatMessages {
		result = append(result, msg)
	}

	return result, nil
}

// ConvertFromNative converts Anthropic BetaMessageParam back to unified Message format
func (c *AnthropicConverterImpl) ConvertFromNative(native []interface{}) ([]*Message, error) {
	result := make([]*Message, 0, len(native))

	for _, item := range native {
		// Skip system blocks (they're metadata, not conversation messages)
		if m, ok := item.(map[string]interface{}); ok {
			if _, isSystem := m["_anthropic_system"]; isSystem {
				continue
			}
		}

		// Try to convert to BetaMessageParam
		msg, err := convertAnthropicMessageToUnified(item)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			result = append(result, msg)
		}
	}

	return result, nil
}

// convertAnthropicMessageToUnified converts a single Anthropic message to unified format
func convertAnthropicMessageToUnified(native interface{}) (*Message, error) {
	// Try to unmarshal to BetaMessageParam structure
	data, err := json.Marshal(native)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal native message: %w", err)
	}

	var anthropicMsg struct {
		Role    string                   `json:"role"`
		Content []map[string]interface{} `json:"content"`
	}

	if err := json.Unmarshal(data, &anthropicMsg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to anthropic message: %w", err)
	}

	msg := &Message{
		Role:      strings.ToLower(anthropicMsg.Role),
		Content:   "",
		ToolCalls: make([]map[string]interface{}, 0),
	}

	// Extract content from blocks
	for _, block := range anthropicMsg.Content {
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			if text, ok := block["text"].(string); ok {
				if msg.Content != "" {
					msg.Content += "\n"
				}
				msg.Content += text
			}
		case "tool_use":
			// Convert to unified tool call format
			toolCall := map[string]interface{}{
				"id":   block["id"],
				"type": "function",
				"function": map[string]interface{}{
					"name":      block["name"],
					"arguments": block["input"],
				},
			}
			msg.ToolCalls = append(msg.ToolCalls, toolCall)
		case "tool_result":
			// This is a tool result message
			msg.Role = "tool"
			if toolID, ok := block["tool_use_id"].(string); ok {
				msg.ToolID = toolID
			}
			if content, ok := block["content"].([]interface{}); ok && len(content) > 0 {
				if textBlock, ok := content[0].(map[string]interface{}); ok {
					if text, ok := textBlock["text"].(string); ok {
						msg.Content = text
					}
				}
			}
		}
	}

	return msg, nil
}

// convertMessagesToAnthropicNative is refactored from anthropic_client.go:convertMessagesToAnthropic
func convertMessagesToAnthropicNative(systemPrompt string, messages []*Message, enableCaching bool, cacheTTL string) ([]anthropic.BetaTextBlockParam, []anthropic.BetaMessageParam, error) {
	systemBlocks := make([]anthropic.BetaTextBlockParam, 0, 1)
	if sys := strings.TrimSpace(systemPrompt); sys != "" {
		block := anthropic.BetaTextBlockParam{Text: sys}
		// Add cache control to system prompt if caching is enabled
		if enableCaching {
			block.CacheControl = makeCacheControlNative(cacheTTL)
		}
		systemBlocks = append(systemBlocks, block)
	}

	chatMessages := make([]anthropic.BetaMessageParam, 0, len(messages))
	for idx, msg := range messages {
		if msg == nil {
			continue
		}

		role := normalizeRoleNative(msg.Role)
		switch role {
		case "system":
			if text := strings.TrimSpace(msg.Content); text != "" {
				systemBlocks = append(systemBlocks, anthropic.BetaTextBlockParam{Text: text})
			}
			continue
		case "assistant":
			blocks, err := buildAnthropicAssistantBlocksNative(msg)
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
			userMsg, err := buildAnthropicToolMessageNative(msg)
			if err != nil {
				return nil, nil, err
			}
			if userMsg.Role != "" {
				chatMessages = append(chatMessages, userMsg)
			}
		default:
			blocks := buildAnthropicTextBlocksNative(msg.Content)
			if len(blocks) == 0 {
				continue
			}
			if msg.CacheControl {
				applyCacheControlToBlocksNative(blocks, enableCaching, cacheTTL)
			}
			chatMessages = append(chatMessages, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleUser,
				Content: blocks,
			})
		}
	}

	return systemBlocks, chatMessages, nil
}

func buildAnthropicAssistantBlocksNative(msg *Message) ([]anthropic.BetaContentBlockParamUnion, error) {
	blocks := make([]anthropic.BetaContentBlockParamUnion, 0, 1+len(msg.ToolCalls))

	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewBetaTextBlock(msg.Content))
	}

	toolBlocks, err := convertAnthropicToolUsesNative(msg.ToolCalls)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, toolBlocks...)

	return blocks, nil
}

func convertAnthropicToolUsesNative(toolCalls []map[string]interface{}) ([]anthropic.BetaContentBlockParamUnion, error) {
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

		input := parseToolArgumentsNative(function["arguments"])
		result = append(result, anthropic.NewBetaToolUseBlock(callID, input, name))
	}

	return result, nil
}

func buildAnthropicToolMessageNative(msg *Message) (anthropic.BetaMessageParam, error) {
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

func buildAnthropicTextBlocksNative(content string) []anthropic.BetaContentBlockParamUnion {
	if content == "" {
		return nil
	}
	return []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(content)}
}

func parseToolArgumentsNative(raw interface{}) any {
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

func normalizeRoleNative(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return "user"
	}
	return role
}

func makeCacheControlNative(ttl string) anthropic.BetaCacheControlEphemeralParam {
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

// applyCacheControlToBlocksNative marks the last text block with cache metadata when enabled.
func applyCacheControlToBlocksNative(blocks []anthropic.BetaContentBlockParamUnion, enableCaching bool, cacheTTL string) {
	if !enableCaching {
		return
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		if text := blocks[i].OfText; text != nil {
			text.CacheControl = makeCacheControlNative(cacheTTL)
			return
		}
	}
}

// Helper function to convert tool definitions
func convertAnthropicToolsNative(tools []map[string]interface{}, enableCaching bool, cacheTTL string) []anthropic.BetaToolUnionParam {
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
			if req := extractStringSliceNative(params["required"]); len(req) > 0 {
				schema.Required = req
			}
			if schemaType, ok := params["type"].(string); ok && schemaType != "" {
				schema.Type = constant.Object(schemaType)
			}
			if extras := copyExtraFieldsNative(params, "type", "properties", "required"); len(extras) > 0 {
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
			tool.CacheControl = makeCacheControlNative(cacheTTL)
		}

		result = append(result, anthropic.BetaToolUnionParam{OfTool: tool})
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func extractStringSliceNative(value interface{}) []string {
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

func copyExtraFieldsNative(src map[string]interface{}, skip ...string) map[string]any {
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
