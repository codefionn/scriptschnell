package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Simple converters for providers that use OpenAI-compatible format but may not support caching

// MistralConverterImpl implements NativeConverter for Mistral models
type MistralConverterImpl struct{}

func (c *MistralConverterImpl) GetProviderName() string {
	return "mistral"
}

func (c *MistralConverterImpl) GetModelFamily(modelID string) string {
	return extractModelFamily(modelID)
}

func (c *MistralConverterImpl) SupportsNativeStorage() bool {
	return false // Mistral doesn't support prompt caching yet
}

func (c *MistralConverterImpl) ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error) {
	// Use OpenAI-compatible format (Mistral uses similar API)
	result := make([]interface{}, 0, len(messages)+1)

	if system := strings.TrimSpace(systemPrompt); system != "" {
		result = append(result, map[string]interface{}{
			"role":    "system",
			"content": system,
		})
	}

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "user"
		}

		nativeMsg := map[string]interface{}{
			"role":    role,
			"content": msg.Content,
		}

		if msg.ToolName != "" {
			nativeMsg["name"] = msg.ToolName
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			nativeMsg["tool_calls"] = msg.ToolCalls
		}

		if role == "tool" && msg.ToolID != "" {
			nativeMsg["tool_call_id"] = msg.ToolID
		}

		result = append(result, nativeMsg)
	}

	return result, nil
}

func (c *MistralConverterImpl) ConvertFromNative(native []interface{}) ([]*Message, error) {
	return convertGenericNativeToUnified(native)
}

// OpenRouterConverterImpl implements NativeConverter for OpenRouter models
type OpenRouterConverterImpl struct{}

func (c *OpenRouterConverterImpl) GetProviderName() string {
	return "openrouter"
}

func (c *OpenRouterConverterImpl) GetModelFamily(modelID string) string {
	// OpenRouter uses provider/model format, extract model part
	parts := strings.Split(modelID, "/")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return extractModelFamily(modelID)
}

func (c *OpenRouterConverterImpl) SupportsNativeStorage() bool {
	return false // Disabled - some providers like Mistral don't support cache_control ephemeral
}

func (c *OpenRouterConverterImpl) ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error) {
	// OpenRouter uses OpenAI-compatible format
	// Note: Disable caching as some providers (like Mistral) don't support cache_control ephemeral
	result := make([]interface{}, 0, len(messages)+1)

	if system := strings.TrimSpace(systemPrompt); system != "" {
		result = append(result, map[string]interface{}{
			"role":    "system",
			"content": system,
		})
	}

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "user"
		}

		nativeMsg := map[string]interface{}{
			"role":    role,
			"content": msg.Content,
		}

		if msg.ToolName != "" {
			nativeMsg["name"] = msg.ToolName
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			nativeMsg["tool_calls"] = msg.ToolCalls
		}

		if role == "tool" && msg.ToolID != "" {
			nativeMsg["tool_call_id"] = msg.ToolID
		}

		result = append(result, nativeMsg)
	}

	return result, nil
}

func (c *OpenRouterConverterImpl) ConvertFromNative(native []interface{}) ([]*Message, error) {
	return convertGenericNativeToUnified(native)
}

// CerebrasConverterImpl implements NativeConverter for Cerebras models
type CerebrasConverterImpl struct{}

func (c *CerebrasConverterImpl) GetProviderName() string {
	return "cerebras"
}

func (c *CerebrasConverterImpl) GetModelFamily(modelID string) string {
	return extractModelFamily(modelID)
}

func (c *CerebrasConverterImpl) SupportsNativeStorage() bool {
	return false // Cerebras doesn't support prompt caching
}

func (c *CerebrasConverterImpl) ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error) {
	// Cerebras uses OpenAI-compatible format
	result := make([]interface{}, 0, len(messages)+1)

	if system := strings.TrimSpace(systemPrompt); system != "" {
		result = append(result, map[string]interface{}{
			"role":    "system",
			"content": system,
		})
	}

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "user"
		}

		nativeMsg := map[string]interface{}{
			"role":    role,
			"content": msg.Content,
		}

		if msg.ToolName != "" {
			nativeMsg["name"] = msg.ToolName
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			nativeMsg["tool_calls"] = msg.ToolCalls
		}

		if role == "tool" && msg.ToolID != "" {
			nativeMsg["tool_call_id"] = msg.ToolID
		}

		result = append(result, nativeMsg)
	}

	return result, nil
}

func (c *CerebrasConverterImpl) ConvertFromNative(native []interface{}) ([]*Message, error) {
	return convertGenericNativeToUnified(native)
}

// convertGenericNativeToUnified is a helper for OpenAI-compatible formats
func convertGenericNativeToUnified(native []interface{}) ([]*Message, error) {
	result := make([]*Message, 0, len(native))

	for _, item := range native {
		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var genericMsg struct {
			Role       string                   `json:"role"`
			Content    interface{}              `json:"content"`
			Name       string                   `json:"name,omitempty"`
			ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
			ToolCallID string                   `json:"tool_call_id,omitempty"`
		}

		if err := json.Unmarshal(data, &genericMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to generic message: %w", err)
		}

		// Skip system messages (they're in the system prompt)
		if genericMsg.Role == "system" {
			continue
		}

		msg := &Message{
			Role:      genericMsg.Role,
			Content:   extractTextContent(genericMsg.Content),
			ToolCalls: genericMsg.ToolCalls,
			ToolName:  genericMsg.Name,
			ToolID:    genericMsg.ToolCallID,
		}

		result = append(result, msg)
	}

	return result, nil
}

// extractTextContent extracts text from content (can be string or array)
func extractTextContent(content interface{}) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []interface{}:
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

// OllamaConverterImpl implements NativeConverter for Ollama models
type OllamaConverterImpl struct{}

func (c *OllamaConverterImpl) GetProviderName() string {
	return "ollama"
}

func (c *OllamaConverterImpl) GetModelFamily(modelID string) string {
	return extractModelFamily(modelID)
}

func (c *OllamaConverterImpl) SupportsNativeStorage() bool {
	return false // Ollama doesn't support prompt caching
}

func (c *OllamaConverterImpl) ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error) {
	// Ollama format stores system prompt separately, not as a message
	// We include it as metadata for extraction purposes
	result := make([]interface{}, 0, len(messages)+1)

	// Store system prompt as metadata (Ollama uses separate field)
	if system := strings.TrimSpace(systemPrompt); system != "" {
		result = append(result, map[string]interface{}{
			"_ollama_system": system,
		})
	}

	// Convert messages
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "user"
		}

		// Skip system messages - they go in the system field
		if role == "system" {
			continue
		}

		nativeMsg := map[string]interface{}{
			"role":    role,
			"content": msg.Content,
		}

		if msg.ToolName != "" {
			nativeMsg["name"] = msg.ToolName
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			nativeMsg["tool_calls"] = msg.ToolCalls
		}

		if role == "tool" && msg.ToolID != "" {
			nativeMsg["tool_call_id"] = msg.ToolID
		}

		result = append(result, nativeMsg)
	}

	return result, nil
}

func (c *OllamaConverterImpl) ConvertFromNative(native []interface{}) ([]*Message, error) {
	result := make([]*Message, 0, len(native))

	for _, item := range native {
		// Skip system metadata
		if m, ok := item.(map[string]interface{}); ok {
			if _, isSystem := m["_ollama_system"]; isSystem {
				continue
			}
		}

		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var genericMsg struct {
			Role       string                   `json:"role"`
			Content    string                   `json:"content"`
			Name       string                   `json:"name,omitempty"`
			ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
			ToolCallID string                   `json:"tool_call_id,omitempty"`
		}

		if err := json.Unmarshal(data, &genericMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to generic message: %w", err)
		}

		msg := &Message{
			Role:      genericMsg.Role,
			Content:   genericMsg.Content,
			ToolCalls: genericMsg.ToolCalls,
			ToolName:  genericMsg.Name,
			ToolID:    genericMsg.ToolCallID,
		}

		result = append(result, msg)
	}

	return result, nil
}
