package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GoogleConverterImpl implements NativeConverter for Google/Gemini models
type GoogleConverterImpl struct{}

func (c *GoogleConverterImpl) GetProviderName() string {
	return "google"
}

func (c *GoogleConverterImpl) GetModelFamily(modelID string) string {
	return extractModelFamily(modelID)
}

func (c *GoogleConverterImpl) SupportsNativeStorage() bool {
	return true // Google supports caching via cachedContent API
}

// ConvertToNative converts unified messages to Google GenAI format
// Note: Google uses a different caching approach (cachedContent API) that requires
// separate content creation. This converter prepares messages in native format
// for potential future caching integration.
func (c *GoogleConverterImpl) ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error) {
	result := make([]interface{}, 0, len(messages))

	// Add system prompt as metadata (Google handles it separately)
	if system := strings.TrimSpace(systemPrompt); system != "" {
		result = append(result, map[string]interface{}{
			"_google_system": system,
			"_cache_enabled": enableCaching,
		})
	}

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		switch msg.Role {
		case "assistant":
			content, err := convertToGoogleAssistantContent(msg)
			if err != nil {
				return nil, err
			}
			result = append(result, content)

		case "tool":
			content, err := convertToGoogleToolResponse(msg)
			if err != nil {
				return nil, err
			}
			result = append(result, content)

		case "system":
			// Skip - handled separately in system prompt
			continue

		default: // user
			if msg.Content == "" {
				continue
			}
			result = append(result, map[string]interface{}{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": msg.Content},
				},
			})
		}
	}

	return result, nil
}

// ConvertFromNative converts Google GenAI messages back to unified format
func (c *GoogleConverterImpl) ConvertFromNative(native []interface{}) ([]*Message, error) {
	result := make([]*Message, 0, len(native))

	for _, item := range native {
		// Skip system metadata
		if m, ok := item.(map[string]interface{}); ok {
			if _, isSystem := m["_google_system"]; isSystem {
				continue
			}
		}

		msg, err := convertGoogleContentToUnified(item)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			result = append(result, msg)
		}
	}

	return result, nil
}

// convertToGoogleAssistantContent converts assistant message to Google format
func convertToGoogleAssistantContent(msg *Message) (map[string]interface{}, error) {
	parts := make([]map[string]interface{}, 0, len(msg.ToolCalls)+1)

	if msg.Content != "" {
		parts = append(parts, map[string]interface{}{
			"text": msg.Content,
		})
	}

	for _, tc := range msg.ToolCalls {
		function, _ := tc["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		if name == "" {
			continue
		}

		argsValue := function["arguments"]
		argsMap := make(map[string]interface{})

		switch v := argsValue.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				if err := json.Unmarshal([]byte(v), &argsMap); err != nil {
					return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
				}
			}
		case map[string]interface{}:
			argsMap = v
		}

		parts = append(parts, map[string]interface{}{
			"functionCall": map[string]interface{}{
				"name": name,
				"args": argsMap,
			},
		})
	}

	return map[string]interface{}{
		"role":  "model", // Google uses "model" instead of "assistant"
		"parts": parts,
	}, nil
}

// convertToGoogleToolResponse converts tool response to Google format
func convertToGoogleToolResponse(msg *Message) (map[string]interface{}, error) {
	if msg.ToolName == "" {
		return nil, fmt.Errorf("tool response missing tool name")
	}

	response := map[string]interface{}{
		"name": msg.ToolName,
	}

	if msg.Content != "" {
		var contentMap map[string]interface{}
		if err := json.Unmarshal([]byte(msg.Content), &contentMap); err == nil {
			response["response"] = contentMap
		} else {
			response["response"] = map[string]interface{}{
				"result": msg.Content,
			}
		}
	} else {
		response["response"] = map[string]interface{}{}
	}

	return map[string]interface{}{
		"role": "function", // Google uses "function" for tool responses
		"parts": []map[string]interface{}{
			{"functionResponse": response},
		},
	}, nil
}

// convertGoogleContentToUnified converts Google content to unified message
func convertGoogleContentToUnified(native interface{}) (*Message, error) {
	data, err := json.Marshal(native)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal native message: %w", err)
	}

	var googleMsg struct {
		Role  string                   `json:"role"`
		Parts []map[string]interface{} `json:"parts"`
	}

	if err := json.Unmarshal(data, &googleMsg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to google message: %w", err)
	}

	// Map Google roles to unified roles
	role := googleMsg.Role
	switch role {
	case "model":
		role = "assistant"
	case "function":
		role = "tool"
	case "user":
		// Keep as-is
	default:
		role = "user"
	}

	msg := &Message{
		Role:      role,
		Content:   "",
		ToolCalls: make([]map[string]interface{}, 0),
	}

	// Extract content from parts
	for _, part := range googleMsg.Parts {
		if text, ok := part["text"].(string); ok {
			if msg.Content != "" {
				msg.Content += "\n"
			}
			msg.Content += text
		}

		if functionCall, ok := part["functionCall"].(map[string]interface{}); ok {
			name, _ := functionCall["name"].(string)
			args, _ := functionCall["args"].(map[string]interface{})

			toolCall := map[string]interface{}{
				"id":   fmt.Sprintf("call_%s", name),
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": args,
				},
			}
			msg.ToolCalls = append(msg.ToolCalls, toolCall)
		}

		if functionResponse, ok := part["functionResponse"].(map[string]interface{}); ok {
			name, _ := functionResponse["name"].(string)
			response, _ := functionResponse["response"].(map[string]interface{})

			msg.ToolName = name
			if responseData, err := json.Marshal(response); err == nil {
				msg.Content = string(responseData)
			}
		}
	}

	return msg, nil
}
