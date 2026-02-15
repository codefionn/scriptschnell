package planning

import (
	"context"
	"errors"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
	"github.com/codefionn/scriptschnell/internal/orchestrator/loop"
)

// mockPlanningLLMClient is a mock LLM client for testing
type mockPlanningLLMClient struct {
	responses []*llm.CompletionResponse
	callCount int
	mockError error
}

func (m *mockPlanningLLMClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.mockError != nil {
		return nil, m.mockError
	}
	if m.callCount < len(m.responses) {
		resp := m.responses[m.callCount]
		m.callCount++
		return resp, nil
	}
	return &llm.CompletionResponse{Content: "Default response"}, nil
}

func (m *mockPlanningLLMClient) GetModelName() string      { return "mock-model" }
func (m *mockPlanningLLMClient) GetLastResponseID() string { return "" }
func (m *mockPlanningLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	return "", nil
}
func (m *mockPlanningLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}
func (m *mockPlanningLLMClient) SetPreviousResponseID(id string) {}

func TestPlanningIterationExecute(t *testing.T) {
	t.Run("Successful completion without tool calls", func(t *testing.T) {
		mockClient := &mockPlanningLLMClient{
			responses: []*llm.CompletionResponse{
				{Content: "<answer>{\"mode\": \"simple\", \"plan\": [\"Step 1\"], \"complete\": true}</answer>"},
			},
		}

		agent := &PlanningAgent{}
		deps := &PlanningDependencies{
			Agent:        agent,
			LLMClient:    mockClient,
			ToolRegistry: NewPlanningToolRegistry(),
			Request: &PlanningRequest{
				Objective:      "Test objective",
				AllowQuestions: false,
			},
			Messages:       make([]*llm.Message, 0),
			LoopDetector:   loopdetector.NewLoopDetector(),
			QuestionsAsked: 0,
		}

		iteration := NewPlanningIteration(deps)
		state := loop.NewDefaultState(loop.DefaultConfig())

		outcome, err := iteration.Execute(context.Background(), state)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if outcome.Result != loop.Break {
			t.Errorf("Expected Break result, got %v", outcome.Result)
		}

		if !outcome.HasToolCalls {
			// No tool calls is expected for this test case
		} else {
			t.Error("Expected no tool calls, but HasToolCalls is true")
		}

		if mockClient.callCount != 1 {
			t.Errorf("Expected 1 LLM call, got %d", mockClient.callCount)
		}
	})

	t.Run("Handles tool calls", func(t *testing.T) {
		mockClient := &mockPlanningLLMClient{
			responses: []*llm.CompletionResponse{
				{
					Content: "Using search tool",
					ToolCalls: []map[string]interface{}{
						{
							"id":   "call_1",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "search_files",
								"arguments": `{"pattern": "*.go"}`,
							},
						},
					},
				},
			},
		}

		agent := &PlanningAgent{}
		registry := NewPlanningToolRegistry()
		// Register a mock search tool
		registry.Register(&mockPlanningTool{
			name:   "search_files",
			result: map[string]interface{}{"files": []string{"test.go"}},
		})

		deps := &PlanningDependencies{
			Agent:        agent,
			LLMClient:    mockClient,
			ToolRegistry: registry,
			Request: &PlanningRequest{
				Objective:      "Test objective",
				AllowQuestions: false,
			},
			Messages:       make([]*llm.Message, 0),
			LoopDetector:   loopdetector.NewLoopDetector(),
			QuestionsAsked: 0,
		}

		iteration := NewPlanningIteration(deps)
		state := loop.NewDefaultState(loop.DefaultConfig())

		outcome, err := iteration.Execute(context.Background(), state)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !outcome.HasToolCalls {
			t.Error("Expected HasToolCalls to be true")
		}

		if outcome.Result != loop.Continue {
			t.Errorf("Expected Continue result, got %v", outcome.Result)
		}
	})

	t.Run("Handles LLM error", func(t *testing.T) {
		mockClient := &mockPlanningLLMClient{
			mockError: errors.New("llm error"),
		}

		agent := &PlanningAgent{}
		deps := &PlanningDependencies{
			Agent:        agent,
			LLMClient:    mockClient,
			ToolRegistry: NewPlanningToolRegistry(),
			Request: &PlanningRequest{
				Objective:      "Test objective",
				AllowQuestions: false,
			},
			Messages:       make([]*llm.Message, 0),
			LoopDetector:   loopdetector.NewLoopDetector(),
			QuestionsAsked: 0,
		}

		iteration := NewPlanningIteration(deps)
		state := loop.NewDefaultState(loop.DefaultConfig())

		outcome, err := iteration.Execute(context.Background(), state)

		if err == nil {
			t.Fatal("Expected error, got nil")
		}

		if outcome.Result != loop.Error {
			t.Errorf("Expected Error result, got %v", outcome.Result)
		}
	})

	t.Run("Detects text loop", func(t *testing.T) {
		// The loop detector requires loopThreshold (10) repetitions of the same
		// sentence pattern to trigger. Provide enough responses.
		responses := make([]*llm.CompletionResponse, 12)
		for i := range responses {
			responses[i] = &llm.CompletionResponse{Content: "Repeating text pattern"}
		}

		mockClient := &mockPlanningLLMClient{
			responses: responses,
		}

		agent := &PlanningAgent{}
		deps := &PlanningDependencies{
			Agent:        agent,
			LLMClient:    mockClient,
			ToolRegistry: NewPlanningToolRegistry(),
			Request: &PlanningRequest{
				Objective:      "Test objective",
				AllowQuestions: false,
			},
			Messages:       make([]*llm.Message, 0),
			LoopDetector:   loopdetector.NewLoopDetector(),
			QuestionsAsked: 0,
		}

		iteration := NewPlanningIteration(deps)
		state := loop.NewDefaultState(loop.DefaultConfig())

		// Execute multiple times to trigger loop detection (threshold is 10)
		for i := 0; i < 12; i++ {
			outcome, err := iteration.Execute(context.Background(), state)
			if err != nil {
				t.Fatalf("Iteration %d: Expected no error, got %v", i, err)
			}

			// Check if loop was detected
			if outcome.Result == loop.BreakLoopDetected {
				// Success - loop was detected
				return
			}
		}

		t.Error("Expected loop detection to trigger after repeated identical content")
	})
}

// mockPlanningTool is a mock planning tool for testing
type mockPlanningTool struct {
	name        string
	description string
	params      map[string]interface{}
	result      interface{}
	mockError   string
}

func (m *mockPlanningTool) Name() string { return m.name }
func (m *mockPlanningTool) Description() string {
	if m.description != "" {
		return m.description
	}
	return "Mock tool"
}
func (m *mockPlanningTool) Parameters() map[string]interface{} {
	if m.params != nil {
		return m.params
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (m *mockPlanningTool) Execute(ctx context.Context, params map[string]interface{}) *PlanningToolResult {
	return &PlanningToolResult{
		Result: m.result,
		Error:  m.mockError,
	}
}

func TestPlanningIterationImplementsInterface(t *testing.T) {
	// Verify PlanningIteration implements loop.Iteration
	var _ loop.Iteration = (*PlanningIteration)(nil)
}
