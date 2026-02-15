package loop

import (
	"context"
	"errors"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/progress"
)

// MockLLMClient is a mock implementation of llm.Client for testing
type MockLLMClient struct {
	MockResponse *llm.CompletionResponse
	MockError    error
	CallCount    int
	LastRequest  *llm.CompletionRequest
}

func (m *MockLLMClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.CallCount++
	m.LastRequest = req
	return m.MockResponse, m.MockError
}

func (m *MockLLMClient) GetModelName() string {
	return "mock-model"
}

func (m *MockLLMClient) GetLastResponseID() string {
	return ""
}

func (m *MockLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	if m.MockError != nil {
		return "", m.MockError
	}
	if m.MockResponse != nil {
		return m.MockResponse.Content, nil
	}
	return "", nil
}

func (m *MockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}

func (m *MockLLMClient) SetPreviousResponseID(id string) {
}

// MockSystemPromptProvider is a mock implementation of SystemPromptProvider
type MockSystemPromptProvider struct {
	MockPrompt  string
	MockError   error
	MockModelID string
}

func (m *MockSystemPromptProvider) GetSystemPrompt(ctx context.Context) (string, error) {
	return m.MockPrompt, m.MockError
}

func (m *MockSystemPromptProvider) GetModelID() string {
	return m.MockModelID
}

// MockContextManager is a mock implementation of ContextManager
type MockContextManager struct {
	MockTotalTokens      int
	MockPerMessageTokens []int
	MockShouldCompact    bool
	MockError            error
	CompactCalled        bool
}

func (m *MockContextManager) EstimateTokens(modelID, systemPrompt string, messages []Message) (total int, perMessage []int, err error) {
	return m.MockTotalTokens, m.MockPerMessageTokens, m.MockError
}

func (m *MockContextManager) ShouldCompact(modelID, systemPrompt string, messages []Message) bool {
	return m.MockShouldCompact
}

func (m *MockContextManager) Compact(ctx context.Context, modelID, systemPrompt string, progressCb progress.Callback) error {
	m.CompactCalled = true
	return m.MockError
}

// MockToolRegistry is a mock implementation of ToolRegistry
type MockToolRegistry struct {
	MockSchema []map[string]interface{}
}

func (m *MockToolRegistry) ToJSONSchema() []map[string]interface{} {
	return m.MockSchema
}

// MockSession is a mock implementation of Session for testing
type MockSession struct {
	Messages []Message
}

func (m *MockSession) AddMessage(msg Message) {
	m.Messages = append(m.Messages, msg)
}

func (m *MockSession) GetMessages() []Message {
	return m.Messages
}

// Ensure MockSession implements Session interface
var _ Session = (*MockSession)(nil)

func TestIterationExecutorExecute(t *testing.T) {
	t.Run("Successful completion without tool calls", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "Test response",
			},
		}

		session := &MockSession{}
		deps := &Dependencies{
			LLMClient:            mockClient,
			Session:              session,
			SystemPromptProvider: &MockSystemPromptProvider{MockPrompt: "system prompt"},
			ToolRegistry:         &MockToolRegistry{},
		}

		executor := NewIterationExecutor(deps)
		state := NewDefaultState(DefaultConfig())

		outcome, err := executor.Execute(context.Background(), state)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if outcome.Result != Break {
			t.Errorf("Expected Break result, got %v", outcome.Result)
		}

		if outcome.Content != "Test response" {
			t.Errorf("Expected content 'Test response', got %q", outcome.Content)
		}

		if mockClient.CallCount != 1 {
			t.Errorf("Expected 1 LLM call, got %d", mockClient.CallCount)
		}
	})

	t.Run("Successful completion with tool calls", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "Using tool",
				ToolCalls: []map[string]interface{}{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "test_tool",
							"arguments": `{"param": "value"}`,
						},
					},
				},
			},
		}

		session := &MockSession{}
		deps := &Dependencies{
			LLMClient:            mockClient,
			Session:              session,
			SystemPromptProvider: &MockSystemPromptProvider{MockPrompt: "system prompt"},
			ToolRegistry:         &MockToolRegistry{},
		}

		executor := NewIterationExecutor(deps)
		state := NewDefaultState(DefaultConfig())

		outcome, err := executor.Execute(context.Background(), state)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !outcome.HasToolCalls {
			t.Error("Expected HasToolCalls to be true")
		}

		if outcome.Result != Continue {
			t.Errorf("Expected Continue result, got %v", outcome.Result)
		}
	})

	t.Run("LLM error", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockError: errors.New("llm error"),
		}

		session := &MockSession{}
		deps := &Dependencies{
			LLMClient:            mockClient,
			Session:              session,
			SystemPromptProvider: &MockSystemPromptProvider{MockPrompt: "system prompt"},
			ToolRegistry:         &MockToolRegistry{},
		}

		executor := NewIterationExecutor(deps)
		state := NewDefaultState(DefaultConfig())

		outcome, err := executor.Execute(context.Background(), state)

		if err == nil {
			t.Fatal("Expected error, got nil")
		}

		if outcome.Result != Error {
			t.Errorf("Expected Error result, got %v", outcome.Result)
		}
	})

	t.Run("System prompt error", func(t *testing.T) {
		mockClient := &MockLLMClient{}

		session := &MockSession{}
		deps := &Dependencies{
			LLMClient:            mockClient,
			Session:              session,
			SystemPromptProvider: &MockSystemPromptProvider{MockError: errors.New("prompt error")},
			ToolRegistry:         &MockToolRegistry{},
		}

		executor := NewIterationExecutor(deps)
		state := NewDefaultState(DefaultConfig())

		_, err := executor.Execute(context.Background(), state)

		if err == nil {
			t.Fatal("Expected error, got nil")
		}
	})

	t.Run("Compaction triggered", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "Test response",
			},
		}

		mockContextManager := &MockContextManager{
			MockShouldCompact: true,
		}

		session := &MockSession{}
		deps := &Dependencies{
			LLMClient:            mockClient,
			Session:              session,
			SystemPromptProvider: &MockSystemPromptProvider{MockPrompt: "system prompt"},
			ToolRegistry:         &MockToolRegistry{},
			ContextManager:       mockContextManager,
		}

		executor := NewIterationExecutor(deps)
		state := NewDefaultState(DefaultConfig())

		// First call should trigger compaction
		outcome, err := executor.Execute(context.Background(), state)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if outcome.Result != CompactionNeeded {
			t.Errorf("Expected CompactionNeeded result, got %v", outcome.Result)
		}
	})

	t.Run("Message added to session", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content:   "Assistant response",
				Reasoning: "Let me think...",
			},
		}

		session := &MockSession{}
		deps := &Dependencies{
			LLMClient:            mockClient,
			Session:              session,
			SystemPromptProvider: &MockSystemPromptProvider{MockPrompt: "system prompt"},
			ToolRegistry:         &MockToolRegistry{},
		}

		executor := NewIterationExecutor(deps)
		state := NewDefaultState(DefaultConfig())

		_, err := executor.Execute(context.Background(), state)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(session.Messages) != 1 {
			t.Errorf("Expected 1 message in session, got %d", len(session.Messages))
		}

		msg := session.Messages[0]
		if msg.GetContent() != "Assistant response" {
			t.Errorf("Expected message content 'Assistant response', got %q", msg.GetContent())
		}

		if msg.GetReasoning() != "Let me think..." {
			t.Errorf("Expected reasoning 'Let me think...', got %q", msg.GetReasoning())
		}

		if msg.GetRole() != "assistant" {
			t.Errorf("Expected role 'assistant', got %q", msg.GetRole())
		}
	})
}

func TestConvertSessionMessages(t *testing.T) {
	executor := &IterationExecutor{}

	sessionMessages := []Message{
		&SimpleMessage{
			Role:    "user",
			Content: "Hello",
		},
		&SimpleMessage{
			Role:      "assistant",
			Content:   "Hi there",
			Reasoning: "Thinking...",
			ToolCalls: []map[string]interface{}{{"id": "call_1"}},
			ToolID:    "call_1",
			ToolName:  "test_tool",
		},
	}

	llmMessages := executor.convertSessionMessages(sessionMessages)

	if len(llmMessages) != 2 {
		t.Errorf("Expected 2 LLM messages, got %d", len(llmMessages))
	}

	if llmMessages[0].Role != "user" {
		t.Errorf("Expected first message role 'user', got %q", llmMessages[0].Role)
	}

	if llmMessages[1].Role != "assistant" {
		t.Errorf("Expected second message role 'assistant', got %q", llmMessages[1].Role)
	}

	if llmMessages[1].Reasoning != "Thinking..." {
		t.Errorf("Expected reasoning 'Thinking...', got %q", llmMessages[1].Reasoning)
	}

	if len(llmMessages[1].ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(llmMessages[1].ToolCalls))
	}
}

func TestBuildCompletionRequest(t *testing.T) {
	mockClient := &MockLLMClient{}
	mockToolRegistry := &MockToolRegistry{
		MockSchema: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "test_tool"}},
		},
	}

	deps := &Dependencies{
		LLMClient:    mockClient,
		ToolRegistry: mockToolRegistry,
	}

	executor := NewIterationExecutor(deps)

	messages := []*llm.Message{
		{Role: "user", Content: "Hello"},
	}

	req := executor.buildCompletionRequest(messages, "system prompt")

	if req.SystemPrompt != "system prompt" {
		t.Errorf("Expected system prompt 'system prompt', got %q", req.SystemPrompt)
	}

	if len(req.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(req.Messages))
	}

	if len(req.Tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(req.Tools))
	}
}

func TestExecuteCompletionRetry(t *testing.T) {
	t.Run("Success on first attempt", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{Content: "Success"},
		}

		deps := &Dependencies{LLMClient: mockClient}
		executor := NewIterationExecutor(deps)

		req := &llm.CompletionRequest{Messages: []*llm.Message{{Role: "user", Content: "test"}}}
		resp, err := executor.executeCompletion(context.Background(), req)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if resp.Content != "Success" {
			t.Errorf("Expected 'Success', got %q", resp.Content)
		}

		if mockClient.CallCount != 1 {
			t.Errorf("Expected 1 call, got %d", mockClient.CallCount)
		}
	})

	t.Run("Context cancellation stops retry", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockError: context.Canceled,
		}

		deps := &Dependencies{LLMClient: mockClient}
		executor := NewIterationExecutor(deps)

		req := &llm.CompletionRequest{Messages: []*llm.Message{{Role: "user", Content: "test"}}}
		_, err := executor.executeCompletion(context.Background(), req)

		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled, got %v", err)
		}

		if mockClient.CallCount != 1 {
			t.Errorf("Expected 1 call (no retry), got %d", mockClient.CallCount)
		}
	})
}
