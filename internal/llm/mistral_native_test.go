package llm

import (
	"testing"
)

func TestNormalizeMistralToolCallID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(string) bool
	}{
		{
			name:  "short alphanumeric ID unchanged",
			input: "abc123",
			validate: func(result string) bool {
				return result == "abc123" && len(result) <= 9
			},
		},
		{
			name:  "ID with underscores removed",
			input: "call_12345",
			validate: func(result string) bool {
				return result == "call12345" && len(result) <= 9
			},
		},
		{
			name:  "long ID gets hashed",
			input: "call_1234567890",
			validate: func(result string) bool {
				return len(result) == 9 && isAlphanumeric(result)
			},
		},
		{
			name:  "Anthropic-style ID gets hashed",
			input: "toolu_01234567890",
			validate: func(result string) bool {
				return len(result) == 9 && isAlphanumeric(result)
			},
		},
		{
			name:  "ID with dashes gets hashed",
			input: "call-with-dashes",
			validate: func(result string) bool {
				return len(result) == 9 && isAlphanumeric(result)
			},
		},
		{
			name:  "empty ID gets default value",
			input: "",
			validate: func(result string) bool {
				return result == "callnull" && len(result) <= 9
			},
		},
		{
			name:  "single character preserved",
			input: "a",
			validate: func(result string) bool {
				return result == "a" && len(result) <= 9
			},
		},
		{
			name:  "exactly 9 chars preserved",
			input: "123456789",
			validate: func(result string) bool {
				return result == "123456789" && len(result) == 9
			},
		},
		{
			name:  "10 chars gets hashed",
			input: "1234567890",
			validate: func(result string) bool {
				return len(result) == 9 && isAlphanumeric(result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeMistralToolCallID(tt.input)

			// All results must be alphanumeric and <= 9 chars
			if len(result) > 9 {
				t.Errorf("normalizeMistralToolCallID() returned ID longer than 9 chars: got length %d", len(result))
			}

			if !isAlphanumeric(result) {
				t.Errorf("normalizeMistralToolCallID() returned non-alphanumeric ID: %q", result)
			}

			// Custom validation
			if !tt.validate(result) {
				t.Errorf("normalizeMistralToolCallID() validation failed for input %q, got %q", tt.input, result)
			}
		})
	}
}

func isAlphanumeric(s string) bool {
	for _, ch := range s {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')) {
			return false
		}
	}
	return true
}

func TestConvertMistralToolCalls(t *testing.T) {
	tests := []struct {
		name     string
		input    []map[string]interface{}
		validate func([]mistralToolCall) bool
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			validate: func(result []mistralToolCall) bool {
				return result == nil
			},
		},
		{
			name:  "empty input returns nil",
			input: []map[string]interface{}{},
			validate: func(result []mistralToolCall) bool {
				return result == nil
			},
		},
		{
			name: "tool call with long ID gets normalized",
			input: []map[string]interface{}{
				{
					"id":   "toolu_01234567890_very_long_id",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "test_function",
						"arguments": `{"arg": "value"}`,
					},
				},
			},
			validate: func(result []mistralToolCall) bool {
				if len(result) != 1 {
					return false
				}
				return len(result[0].ID) <= 9 && isAlphanumeric(result[0].ID)
			},
		},
		{
			name: "multiple tool calls all get normalized IDs",
			input: []map[string]interface{}{
				{
					"id":   "call_123",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "func1",
						"arguments": `{}`,
					},
				},
				{
					"id":   "toolu_very_long_anthropic_id_12345",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "func2",
						"arguments": `{}`,
					},
				},
			},
			validate: func(result []mistralToolCall) bool {
				if len(result) != 2 {
					return false
				}
				for _, call := range result {
					if len(call.ID) > 9 || !isAlphanumeric(call.ID) {
						return false
					}
				}
				return true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMistralToolCalls(tt.input)

			if !tt.validate(result) {
				t.Errorf("convertMistralToolCalls() validation failed")
			}
		})
	}
}

func TestConvertMessages_SkipEmptyAssistantMessages(t *testing.T) {
	client := &MistralClient{
		apiKey: "test-key",
		model:  "mistral-large-latest",
	}

	tests := []struct {
		name     string
		input    *CompletionRequest
		validate func([]mistralChatMessage) bool
	}{
		{
			name: "assistant message with empty content and no tool calls is skipped",
			input: &CompletionRequest{
				Messages: []*Message{
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "", ToolCalls: nil},
					{Role: "user", Content: "Are you there?"},
				},
			},
			validate: func(result []mistralChatMessage) bool {
				// Should only have 2 messages (the empty assistant message should be skipped)
				if len(result) != 2 {
					return false
				}
				return result[0].Role == "user" && result[1].Role == "user"
			},
		},
		{
			name: "assistant message with whitespace-only content is skipped",
			input: &CompletionRequest{
				Messages: []*Message{
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "   \n\t  ", ToolCalls: nil},
					{Role: "user", Content: "Anyone home?"},
				},
			},
			validate: func(result []mistralChatMessage) bool {
				// Should only have 2 messages
				if len(result) != 2 {
					return false
				}
				return result[0].Role == "user" && result[1].Role == "user"
			},
		},
		{
			name: "assistant message with empty content but with tool calls is kept",
			input: &CompletionRequest{
				Messages: []*Message{
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{
						{
							"id":   "call_123",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "test_func",
								"arguments": "{}",
							},
						},
					}},
					{Role: "tool", ToolID: "call_123", Content: "result"},
				},
			},
			validate: func(result []mistralChatMessage) bool {
				// Should have all 3 messages
				if len(result) != 3 {
					return false
				}
				return result[0].Role == "user" &&
					result[1].Role == "assistant" &&
					len(result[1].ToolCalls) > 0 &&
					result[2].Role == "tool"
			},
		},
		{
			name: "assistant message with non-empty content and no tool calls is kept",
			input: &CompletionRequest{
				Messages: []*Message{
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "Hi there!", ToolCalls: nil},
				},
			},
			validate: func(result []mistralChatMessage) bool {
				// Should have both messages
				if len(result) != 2 {
					return false
				}
				return result[0].Role == "user" &&
					result[1].Role == "assistant" &&
					result[1].Content == "Hi there!"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.convertMessages(tt.input)

			if !tt.validate(result) {
				t.Errorf("convertMessages() validation failed for test %q", tt.name)
				t.Logf("Result has %d messages:", len(result))
				for i, msg := range result {
					t.Logf("  [%d] Role=%s, Content=%q, ToolCalls=%d", i, msg.Role, msg.Content, len(msg.ToolCalls))
				}
			}
		})
	}
}
