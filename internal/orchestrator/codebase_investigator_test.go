package orchestrator

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// MockClient mocks the llm.Client interface
type MockClient struct {
	CompleteFunc            func(ctx context.Context, prompt string) (string, error)
	CompleteWithRequestFunc func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func (m *MockClient) Complete(ctx context.Context, prompt string) (string, error) {
	if m.CompleteFunc != nil {
		return m.CompleteFunc(ctx, prompt)
	}
	return "", nil
}

func (m *MockClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.CompleteWithRequestFunc != nil {
		return m.CompleteWithRequestFunc(ctx, req)
	}
	return &llm.CompletionResponse{}, nil
}

func (m *MockClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}

func (m *MockClient) GetModelName() string {
	return "mock-model"
}

func (m *MockClient) GetLastResponseID() string {
	return ""
}

func (m *MockClient) SetPreviousResponseID(responseID string) {
}

// TestMockClientBasic is a minimal test to satisfy import requirements
func TestMockClientBasic(t *testing.T) {
	client := &MockClient{}

	// Test basic operations
	if name := client.GetModelName(); name != "mock-model" {
		t.Errorf("expected model name 'mock-model', got '%s'", name)
	}

	if id := client.GetLastResponseID(); id != "" {
		t.Errorf("expected empty response ID, got '%s'", id)
	}

	// Test that SetPreviousResponseID doesn't panic
	client.SetPreviousResponseID("test-id")
}
