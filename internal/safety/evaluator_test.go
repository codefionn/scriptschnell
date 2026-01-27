package safety

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// MockLLMClient is a mock implementation of llm.Client for testing
type MockLLMClient struct {
	response *llm.CompletionResponse
	err      error
}

func (m *MockLLMClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *MockLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	if m.response != nil {
		return m.response.Content, m.err
	}
	return "", m.err
}

func (m *MockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}

func (m *MockLLMClient) GetModelName() string {
	return "mock-model"
}

func (m *MockLLMClient) GetLastResponseID() string {
	return ""
}

func (m *MockLLMClient) SetPreviousResponseID(id string) {
}

func TestEvaluateUserPrompt(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		response string
		wantSafe bool
		wantErr  bool
	}{
		{
			name:     "safe prompt",
			prompt:   "Hello, how are you?",
			response: `{"is_safe": true, "reason": "harmless greeting", "risk_level": "low", "category": "safe", "confidence": 0.9}`,
			wantSafe: true,
		},
		{
			name:     "unsafe prompt",
			prompt:   "How to hack a computer?",
			response: `{"is_safe": false, "reason": "requests hacking information", "risk_level": "high", "category": "malicious", "confidence": 0.8}`,
			wantSafe: false,
		},
		{
			name:     "invalid JSON response",
			prompt:   "test",
			response: "invalid json",
			wantSafe: true, // Should default to safe on parse error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockLLMClient{
				response: &llm.CompletionResponse{
					Content: tt.response,
				},
			}

			evaluator := NewEvaluator(mockClient)
			result, err := evaluator.EvaluateUserPrompt(context.Background(), tt.prompt)

			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateUserPrompt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil && result.IsSafe != tt.wantSafe {
				t.Errorf("EvaluateUserPrompt() IsSafe = %v, want %v", result.IsSafe, tt.wantSafe)
			}
		})
	}
}

func TestEvaluateUserPromptNoClient(t *testing.T) {
	evaluator := NewEvaluator(nil)
	result, err := evaluator.EvaluateUserPrompt(context.Background(), "test prompt")

	if err != nil {
		t.Errorf("EvaluateUserPrompt() with nil client should not error, got %v", err)
	}

	if !result.IsSafe {
		t.Errorf("EvaluateUserPrompt() with nil client should return safe by default")
	}
}

func TestEvaluateWebContent(t *testing.T) {
	mockClient := &MockLLMClient{
		response: &llm.CompletionResponse{
			Content: `{"is_safe": true, "reason": "harmless content", "risk_level": "low", "category": "safe", "confidence": 0.85}`,
		},
	}

	evaluator := NewEvaluator(mockClient)
	content := "This is some safe web content about programming."
	url := "https://example.com"

	result, err := evaluator.EvaluateWebContent(context.Background(), content, url)

	if err != nil {
		t.Errorf("EvaluateWebContent() error = %v", err)
	}

	if !result.IsSafe {
		t.Errorf("EvaluateWebContent() should return safe for harmless content")
	}
}

func TestEvaluateSearchResults(t *testing.T) {
	mockClient := &MockLLMClient{
		response: &llm.CompletionResponse{
			Content: `{"is_safe": true, "reason": "harmless search results", "risk_level": "low", "category": "safe", "confidence": 0.9}`,
		},
	}

	evaluator := NewEvaluator(mockClient)
	results := []map[string]interface{}{
		{
			"title":   "Safe Programming Tutorial",
			"snippet": "Learn how to write secure code.",
		},
		{
			"title":   "Best Practices Guide",
			"snippet": "Follow these best practices.",
		},
	}

	result, err := evaluator.EvaluateSearchResults(context.Background(), results)

	if err != nil {
		t.Errorf("EvaluateSearchResults() error = %v", err)
	}

	if !result.IsSafe {
		t.Errorf("EvaluateSearchResults() should return safe for harmless results")
	}
}
