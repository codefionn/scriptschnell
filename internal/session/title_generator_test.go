package session

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// MockLLMClient is a mock implementation of llm.Client for testing
type MockLLMClient struct {
	response string
	err      error
}

func (m *MockLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func (m *MockLLMClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Return a simple response
	return &llm.CompletionResponse{
		Content: m.response,
	}, nil
}

func (m *MockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(string) error) error {
	if m.err != nil {
		return m.err
	}
	return callback(m.response)
}

func (m *MockLLMClient) GetModelName() string {
	return "mock-model"
}

func TestGenerateSimpleTitle(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		expected string
	}{
		{
			name:     "basic prompt",
			prompt:   "fix the login bug",
			expected: "Fix the login bug",
		},
		{
			name:     "prompt with please prefix",
			prompt:   "please add a logout button",
			expected: "Add a logout button",
		},
		{
			name:     "long prompt truncation",
			prompt:   "This is a very long prompt that exceeds the maximum allowed length for a session title and should be truncated properly with ellipsis",
			expected: "This is a very long prompt that exceeds the maximum allowed length for a sess...",
		},
		{
			name:     "empty prompt",
			prompt:   "",
			expected: "New session",
		},
		{
			name:     "multiline prompt",
			prompt:   "fix authentication\nand also refactor the code",
			expected: "Fix authentication",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateSimpleTitle(tt.prompt)
			if result != tt.expected {
				t.Errorf("generateSimpleTitle(%q) = %q, want %q", tt.prompt, result, tt.expected)
			}
		})
	}
}

func TestTitleGeneratorWithMockLLM(t *testing.T) {
	tests := []struct {
		name              string
		llmResponse       string
		llmError          error
		userPrompt        string
		expectedSubstring string
	}{
		{
			name:              "successful LLM generation",
			llmResponse:       `{"title": "Fix authentication bug"}`,
			llmError:          nil,
			userPrompt:        "please fix the authentication bug in the login handler",
			expectedSubstring: "Fix authentication bug",
		},
		{
			name:              "LLM returns markdown wrapped JSON",
			llmResponse:       "```json\n{\"title\": \"Add dark mode toggle\"}\n```",
			llmError:          nil,
			userPrompt:        "add a dark mode toggle to the settings page",
			expectedSubstring: "Add dark mode toggle",
		},
		{
			name:              "fallback on LLM failure",
			llmResponse:       "",
			llmError:          nil, // Empty response triggers fallback
			userPrompt:        "refactor user service",
			expectedSubstring: "Refactor user service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockLLMClient{
				response: tt.llmResponse,
				err:      tt.llmError,
			}

			generator := NewTitleGenerator(mockClient)
			ctx := context.Background()

			title, err := generator.GenerateTitle(ctx, tt.userPrompt, []string{}, map[string]string{})
			if err != nil && tt.llmError == nil {
				t.Errorf("unexpected error: %v", err)
			}

			if title != tt.expectedSubstring {
				t.Errorf("GenerateTitle() = %q, want %q", title, tt.expectedSubstring)
			}
		})
	}
}

func TestTitleGeneratorWithNilClient(t *testing.T) {
	generator := NewTitleGenerator(nil)
	ctx := context.Background()

	title, err := generator.GenerateTitle(ctx, "test prompt", []string{}, map[string]string{})
	if err != nil {
		t.Errorf("unexpected error with nil client: %v", err)
	}

	if title != "Test prompt" {
		t.Errorf("expected fallback title 'Test prompt', got %q", title)
	}
}
