package llm

import (
	"context"
	"testing"
)

func TestGroqRequiresResponsesAPI(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{
			name:     "OpenAI prefixed model uses Responses API",
			model:    "openai/gpt-oss-120b",
			expected: true,
		},
		{
			name:     "OpenAI prefixed model uppercase",
			model:    "OPENAI/GPT-OSS-120B",
			expected: true,
		},
		{
			name:     "Standard Groq model uses chat API",
			model:    "llama-3.1-8b-instant",
			expected: false,
		},
		{
			name:     "Mixtral model uses chat API",
			model:    "mixtral-8x7b-32768",
			expected: false,
		},
		{
			name:     "Empty string",
			model:    "",
			expected: false,
		},
		{
			name:     "Whitespace only",
			model:    "   ",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := groqRequiresResponsesAPI(tt.model)
			if result != tt.expected {
				t.Errorf("groqRequiresResponsesAPI(%q) = %v, want %v", tt.model, result, tt.expected)
			}
		})
	}
}

func TestGroqClient_GetModelName(t *testing.T) {
	client := &GroqClient{
		model: "test-model",
	}

	if got := client.GetModelName(); got != "test-model" {
		t.Errorf("GetModelName() = %v, want %v", got, "test-model")
	}
}

func TestGroqClient_BuildResponsesInput(t *testing.T) {
	client := &GroqClient{}

	tests := []struct {
		name     string
		messages []*Message
		expected string
		wantErr  bool
	}{
		{
			name:     "No messages",
			messages: []*Message{},
			expected: "",
			wantErr:  true,
		},
		{
			name: "Single user message",
			messages: []*Message{
				{Role: "user", Content: "Hello"},
			},
			expected: "User: Hello",
			wantErr:  false,
		},
		{
			name: "System and user message",
			messages: []*Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "Hello"},
			},
			expected: "System: You are helpful\n\nUser: Hello",
			wantErr:  false,
		},
		{
			name: "System, user, and assistant message",
			messages: []*Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
			},
			expected: "System: You are helpful\n\nUser: Hello\n\nAssistant: Hi there!",
			wantErr:  false,
		},
		{
			name: "Empty content messages",
			messages: []*Message{
				{Role: "system", Content: "   "},
				{Role: "user", Content: "Valid content"},
				{Role: "assistant", Content: ""},
			},
			expected: "User: Valid content",
			wantErr:  false,
		},
		{
			name:     "Nil messages",
			messages: []*Message{nil, {Role: "user", Content: "test"}},
			expected: "User: test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.buildGroqResponsesInput(tt.messages)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildGroqResponsesInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("buildGroqResponsesInput() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGroqClient_ConvertResponsesResponse(t *testing.T) {
	client := &GroqClient{}

	tests := []struct {
		name     string
		response *groqResponsesResponse
		expected *CompletionResponse
	}{
		{
			name:     "Nil response",
			response: nil,
			expected: &CompletionResponse{StopReason: "unknown"},
		},
		{
			name: "Simple response with text",
			response: &groqResponsesResponse{
				Status: "completed",
				Output: []groqResponseOutput{
					{
						Type: "message",
						Role: "assistant",
						Content: []groqResponseContent{
							{Type: "output_text", Text: "Hello, world!"},
						},
					},
				},
			},
			expected: &CompletionResponse{
				Content:    "Hello, world!",
				ToolCalls:  nil,
				StopReason: "completed",
			},
		},
		{
			name: "Response with no message output",
			response: &groqResponsesResponse{
				Status: "completed",
				Output: []groqResponseOutput{
					{
						Type: "other_type",
						Role: "assistant",
					},
				},
			},
			expected: &CompletionResponse{
				Content:    "",
				ToolCalls:  nil,
				StopReason: "completed",
			},
		},
		{
			name: "Response with no assistant role",
			response: &groqResponsesResponse{
				Status: "completed",
				Output: []groqResponseOutput{
					{
						Type: "message",
						Role: "user",
						Content: []groqResponseContent{
							{Type: "output_text", Text: "Should not be included"},
						},
					},
				},
			},
			expected: &CompletionResponse{
				Content:    "",
				ToolCalls:  nil,
				StopReason: "completed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.convertGroqResponsesResponse(tt.response)

			if got.Content != tt.expected.Content {
				t.Errorf("convertGroqResponsesResponse() content = %v, want %v", got.Content, tt.expected.Content)
			}
			if got.StopReason != tt.expected.StopReason {
				t.Errorf("convertGroqResponsesResponse() stopReason = %v, want %v", got.StopReason, tt.expected.StopReason)
			}
			if (got.ToolCalls != nil) != (tt.expected.ToolCalls != nil) {
				t.Errorf("convertGroqResponsesResponse() toolCalls = %v, want %v", got.ToolCalls, tt.expected.ToolCalls)
			}
		})
	}
}

func TestNewGroqClient(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		model     string
		wantErr   bool
		expectAPI bool
	}{
		{
			name:    "Empty API key",
			apiKey:  "",
			model:   "llama-3.1-8b-instant",
			wantErr: true,
		},
		{
			name:      "Standard model uses chat API",
			apiKey:    "test-key",
			model:     "llama-3.1-8b-instant",
			wantErr:   false,
			expectAPI: false,
		},
		{
			name:      "OpenAI model uses Responses API",
			apiKey:    "test-key",
			model:     "openai/gpt-oss-120b",
			wantErr:   false,
			expectAPI: true,
		},
		{
			name:      "Default model when empty",
			apiKey:    "test-key",
			model:     "",
			wantErr:   false,
			expectAPI: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewGroqClient(tt.apiKey, tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewGroqClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				groqClient, ok := client.(*GroqClient)
				if !ok {
					t.Errorf("NewGroqClient() returned wrong type, got %T, want *GroqClient", client)
					return
				}

				if groqClient.useResponsesAPI != tt.expectAPI {
					t.Errorf("NewGroqClient().useResponsesAPI = %v, want %v", groqClient.useResponsesAPI, tt.expectAPI)
				}

				if groqClient.apiKey != tt.apiKey {
					t.Errorf("NewGroqClient().apiKey = %v, want %v", groqClient.apiKey, tt.apiKey)
				}

				expectedModel := tt.model
				if expectedModel == "" {
					expectedModel = "llama-3.1-8b-instant"
				}
				if groqClient.model != expectedModel {
					t.Errorf("NewGroqClient().model = %v, want %v", groqClient.model, expectedModel)
				}
			}
		})
	}
}

func TestGroqClient_Complete(t *testing.T) {
	// This is a mock test - real API integration would require HTTP mocking
	client := &GroqClient{
		model:           "test-model",
		useResponsesAPI: false,
	}

	// Test with nil request (should error)
	_, err := client.CompleteWithRequest(context.Background(), nil)
	if err == nil {
		t.Error("CompleteWithRequest() with nil request should return error")
	}

	// Test with nil request for streaming
	err = client.Stream(context.Background(), nil, func(chunk string) error { return nil })
	if err == nil {
		t.Error("Stream() with nil request should return error")
	}
}

func TestGroqClient_UsageData_ConvertResponsesResponse(t *testing.T) {
	client := &GroqClient{}

	tests := []struct {
		name     string
		response *groqResponsesResponse
		expected *CompletionResponse
	}{
		{
			name: "With usage data",
			response: &groqResponsesResponse{
				Status: "completed",
				Output: []groqResponseOutput{
					{
						Type: "message",
						Role: "assistant",
						Content: []groqResponseContent{
							{Type: "output_text", Text: "Test response with usage"},
						},
					},
				},
				Usage: &groqUsage{
					InputTokens:  100,
					OutputTokens: 50,
					TotalTokens:  150,
					InputTokensDetails: map[string]interface{}{
						"cached_tokens": 25,
					},
					OutputTokensDetails: map[string]interface{}{
						"reasoning_tokens": 10,
					},
				},
			},
			expected: &CompletionResponse{
				Content:    "Test response with usage",
				StopReason: "completed",
				Usage: map[string]interface{}{
					"input_tokens":      float64(100),
					"output_tokens":     float64(50),
					"total_tokens":      float64(150),
					"prompt_tokens":     float64(100),
					"completion_tokens": float64(50),
					"cached_tokens":     float64(25),
					"reasoning_tokens":  float64(10),
					"input_tokens_details": map[string]interface{}{
						"cached_tokens": float64(25),
					},
					"output_tokens_details": map[string]interface{}{
						"reasoning_tokens": float64(10),
					},
				},
			},
		},
		{
			name: "Without usage data",
			response: &groqResponsesResponse{
				Status: "completed",
				Output: []groqResponseOutput{
					{
						Type: "message",
						Role: "assistant",
						Content: []groqResponseContent{
							{Type: "output_text", Text: "Test response without usage"},
						},
					},
				},
				// No Usage field
			},
			expected: &CompletionResponse{
				Content:    "Test response without usage",
				StopReason: "completed",
				Usage:      nil,
			},
		},
		{
			name: "With usage data but no details",
			response: &groqResponsesResponse{
				Status: "completed",
				Output: []groqResponseOutput{
					{
						Type: "message",
						Role: "assistant",
						Content: []groqResponseContent{
							{Type: "output_text", Text: "Test response minimal usage"},
						},
					},
				},
				Usage: &groqUsage{
					InputTokens:  75,
					OutputTokens: 25,
					TotalTokens:  100,
					// No details fields
				},
			},
			expected: &CompletionResponse{
				Content:    "Test response minimal usage",
				StopReason: "completed",
				Usage: map[string]interface{}{
					"input_tokens":      float64(75),
					"output_tokens":     float64(25),
					"total_tokens":      float64(100),
					"prompt_tokens":     float64(75),
					"completion_tokens": float64(25),
					// No details in expected result
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.convertGroqResponsesResponse(tt.response)

			if result.Content != tt.expected.Content {
				t.Errorf("Content mismatch: expected '%s', got '%s'", tt.expected.Content, result.Content)
			}

			if result.StopReason != tt.expected.StopReason {
				t.Errorf("StopReason mismatch: expected '%s', got '%s'", tt.expected.StopReason, result.StopReason)
			}

			if tt.expected.Usage == nil && result.Usage != nil {
				t.Errorf("Expected Usage to be nil, got %v", result.Usage)
			}

			if tt.expected.Usage != nil {
				if result.Usage == nil {
					t.Fatal("Expected Usage to be set, got nil")
				}

				// Check main usage fields
				for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
					if result.Usage[key] != tt.expected.Usage[key] {
						t.Errorf("Usage[%s] mismatch: expected %v, got %v", key, tt.expected.Usage[key], result.Usage[key])
					}
				}

				// Check details if they exist in expected
				if expectedInputDetails, exists := tt.expected.Usage["input_tokens_details"]; exists {
					resultInputDetails, ok := result.Usage["input_tokens_details"].(map[string]interface{})
					if !ok {
						t.Fatal("Expected input_tokens_details to be map[string]interface{}")
					}
					for k, v := range expectedInputDetails.(map[string]interface{}) {
						if resultInputDetails[k] != v {
							t.Errorf("input_tokens_details[%s] mismatch: expected %v, got %v", k, v, resultInputDetails[k])
						}
					}
				}

				if expectedOutputDetails, exists := tt.expected.Usage["output_tokens_details"]; exists {
					resultOutputDetails, ok := result.Usage["output_tokens_details"].(map[string]interface{})
					if !ok {
						t.Fatal("Expected output_tokens_details to be map[string]interface{}")
					}
					for k, v := range expectedOutputDetails.(map[string]interface{}) {
						if resultOutputDetails[k] != v {
							t.Errorf("output_tokens_details[%s] mismatch: expected %v, got %v", k, v, resultOutputDetails[k])
						}
					}
				}
			}
		})
	}
}
