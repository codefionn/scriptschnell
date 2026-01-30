package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OpenAIConverterImpl implements NativeConverter for OpenAI models
type OpenAIConverterImpl struct{}

func (c *OpenAIConverterImpl) GetProviderName() string {
	return "openai"
}

func (c *OpenAIConverterImpl) GetModelFamily(modelID string) string {
	return extractModelFamily(modelID)
}

func (c *OpenAIConverterImpl) SupportsNativeStorage() bool {
	return true
}

// ConvertToNative converts unified messages to OpenAI format with enhanced caching
func (c *OpenAIConverterImpl) ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error) {
	result := make([]interface{}, 0, len(messages)+1)

	// Add system message with cache control if enabled
	if system := strings.TrimSpace(systemPrompt); system != "" {
		sysMsg := map[string]interface{}{
			"role":    "system",
			"content": system,
		}
		if enableCaching {
			sysMsg["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
		result = append(result, sysMsg)
	}

	// Count user messages for cache control
	userMessageCount := 0
	for _, msg := range messages {
		if msg != nil && strings.ToLower(strings.TrimSpace(msg.Role)) == "user" {
			userMessageCount++
		}
	}

	// Convert messages with user message caching for last 2 user messages
	currentUserIndex := 0
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "user"
		}

		oMsg := map[string]interface{}{
			"role":    role,
			"content": msg.Content,
		}

		if msg.ToolName != "" {
			oMsg["name"] = msg.ToolName
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			oMsg["tool_calls"] = msg.ToolCalls
		}

		if role == "tool" && msg.ToolID != "" {
			oMsg["tool_call_id"] = msg.ToolID
		}

		// Add cache control to last 2 user messages (OpenAI best practice)
		if enableCaching && role == "user" {
			currentUserIndex++
			if userMessageCount-currentUserIndex < 2 {
				oMsg["cache_control"] = map[string]interface{}{"type": "ephemeral"}
			}
		}

		result = append(result, oMsg)
	}

	return result, nil
}

// ConvertFromNative converts OpenAI messages back to unified format
func (c *OpenAIConverterImpl) ConvertFromNative(native []interface{}) ([]*Message, error) {
	result := make([]*Message, 0, len(native))

	for _, item := range native {
		msg, err := convertOpenAIMessageToUnified(item)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			result = append(result, msg)
		}
	}

	return result, nil
}

// convertOpenAIMessageToUnified converts a single OpenAI message to unified format
func convertOpenAIMessageToUnified(native interface{}) (*Message, error) {
	data, err := json.Marshal(native)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal native message: %w", err)
	}

	var openaiMsg struct {
		Role             string                   `json:"role"`
		Content          interface{}              `json:"content"`
		Reasoning        string                   `json:"reasoning,omitempty"`
		Thinking         string                   `json:"thinking,omitempty"`
		ReasoningContent *string                  `json:"reasoning_content,omitempty"`
		ThinkingContent  *string                  `json:"thinking_content,omitempty"`
		Name             string                   `json:"name,omitempty"`
		ToolCalls        []map[string]interface{} `json:"tool_calls,omitempty"`
		ToolCallID       string                   `json:"tool_call_id,omitempty"`
	}

	if err := json.Unmarshal(data, &openaiMsg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to openai message: %w", err)
	}

	// Skip system messages (they're in the system prompt)
	if openaiMsg.Role == "system" {
		return nil, nil
	}

	msg := &Message{
		Role:    openaiMsg.Role,
		Content: extractOpenAITextContent(openaiMsg.Content),
		Reasoning: extractOpenAIMessageReasoning(&openAIChatMessage{
			Content:          openaiMsg.Content,
			Reasoning:        openaiMsg.Reasoning,
			Thinking:         openaiMsg.Thinking,
			ReasoningContent: openaiMsg.ReasoningContent,
			ThinkingContent:  openaiMsg.ThinkingContent,
		}),
		ToolCalls: openaiMsg.ToolCalls,
		ToolName:  openaiMsg.Name,
		ToolID:    openaiMsg.ToolCallID,
	}

	return msg, nil
}

// extractOpenAITextContent extracts text from OpenAI content (can be string or array)
func extractOpenAITextContent(content interface{}) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []interface{}:
		// Handle array of content blocks
		var result string
		for _, block := range value {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if text, ok := blockMap["text"].(string); ok {
					if result != "" {
						result += "\n"
					}
					result += text
				}
			}
		}
		return result
	default:
		return fmt.Sprintf("%v", value)
	}
}
