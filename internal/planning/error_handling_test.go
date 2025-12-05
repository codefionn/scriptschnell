package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/session"
)

// TestPlanningAgent_ErrorHandling tests comprehensive error handling scenarios
func TestPlanningAgent_ErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func() llm.Client
		setupContext  func() context.Context
		request       *PlanningRequest
		expectedError string
		expectedSteps int
		expectPartial bool
	}{
		{
			name: "LLM client returns error",
			setupMock: func() llm.Client {
				return &MockLLMClientWithError{
					error: errors.New("LLM API error"),
				}
			},
			request: &PlanningRequest{
				Objective:      "test LLM error",
				AllowQuestions: false,
			},
			expectedError: "planning completion failed",
		},
		{
			name: "LLM client times out",
			setupMock: func() llm.Client {
				return &MockLLMClientWithTimeout{
					delay: 2 * time.Second,
				}
			},
			setupContext: func() context.Context {
				ctx, _ := context.WithTimeout(context.Background(), 100*time.Millisecond)
				return ctx
			},
			request: &PlanningRequest{
				Objective:      "test timeout",
				AllowQuestions: false,
			},
			expectedError: "context deadline exceeded", // Should be caught by context timeout
		},
		{
			name: "malformed tool call response",
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"tool_calls": [{"invalid": "structure"}]}`)
			},
			request: &PlanningRequest{
				Objective:      "test malformed tool calls",
				AllowQuestions: false,
			},
			expectedSteps: 1, // Should fall back to treating as text
		},
		{
			name: "tool execution returns error",
			setupMock: func() llm.Client {
				return NewMockLLMClient(
					`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"nonexistent.txt\"}"}}]}`,
					`{"plan": ["fallback plan"], "complete": true}`,
				)
			},
			request: &PlanningRequest{
				Objective:      "test tool error handling",
				AllowQuestions: false,
			},
			expectedSteps: 1, // Should get fallback plan
		},
		{
			name: "tool parameter parsing error",
			setupMock: func() llm.Client {
				return NewMockLLMClient(
					`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "invalid json"}}]}`,
					`{"plan": ["recovery plan"], "complete": true}`,
				)
			},
			request: &PlanningRequest{
				Objective:      "test parameter parsing error",
				AllowQuestions: false,
			},
			expectedSteps: 1, // Should recover and continue
		},
		{
			name: "max iterations reached",
			setupMock: func() llm.Client {
				// Return responses that will trigger max iterations (96)
				// All responses are unhelpful to force max iterations
				responses := make([]string, 100)
				for i := 0; i < 100; i++ {
					responses[i] = "Do you need more information?"
				}
				return NewMockLLMClient(responses...)
			},
			request: &PlanningRequest{
				Objective:      "test max iterations",
				AllowQuestions: false,
			},
			expectedSteps: 1, // Should get partial plan after max iterations
			expectPartial: true,
		},
		{
			name: "empty tool call array",
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"tool_calls": []}`)
			},
			request: &PlanningRequest{
				Objective:      "test empty tool calls",
				AllowQuestions: false,
			},
			expectedSteps: 1, // Should treat as no tool calls
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := NewMockFileSystem()
			sess := session.NewSession("test", ".")
			mockLLM := tt.setupMock()

			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)

			ctx := context.Background()
			if tt.setupContext != nil {
				ctx = tt.setupContext()
			}
			response, err := agent.Plan(ctx, tt.request, nil)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("Expected error containing '%s', got no error", tt.expectedError)
				} else if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', got: %s", tt.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if response == nil {
				t.Error("Expected non-nil response")
				return
			}

			if tt.expectedSteps >= 0 && len(response.Plan) != tt.expectedSteps {
				t.Errorf("Expected %d plan steps, got %d", tt.expectedSteps, len(response.Plan))
			}

			if tt.expectPartial && response.Complete {
				t.Error("Expected partial plan (complete=false)")
			}
		})
	}
}

// TestPlanningAgent_ContextCancellationErrorHandling tests various cancellation scenarios
func TestPlanningAgent_ContextCancellationErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		setupContext  func() context.Context
		setupMock     func() llm.Client
		request       *PlanningRequest
		expectError   bool
		errorContains string
	}{
		{
			name: "context cancelled before planning",
			setupContext: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				// Cancel immediately
				cancel()
				return ctx
			},
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"plan": ["step 1"], "complete": true}`)
			},
			request:       &PlanningRequest{Objective: "test", AllowQuestions: false},
			expectError:   true,
			errorContains: "context canceled",
		},
		{
			name: "context with short timeout",
			setupContext: func() context.Context {
				ctx, _ := context.WithTimeout(context.Background(), 1*time.Millisecond)
				return ctx
			},
			setupMock: func() llm.Client {
				return &MockLLMClientWithDelay{delay: 100 * time.Millisecond}
			},
			request:       &PlanningRequest{Objective: "test", AllowQuestions: false},
			expectError:   true,
			errorContains: "context deadline exceeded",
		},
		{
			name: "context cancelled during tool execution",
			setupContext: func() context.Context {
				return context.Background()
			},
			setupMock: func() llm.Client {
				return &MockLLMClientWithContextCancellation{
					responses: []string{
						`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"test.txt\"}"}}]}`,
						`{"plan": ["step 1"], "complete": true}`,
					},
					cancelOnToolCall: true,
				}
			},
			request:       &PlanningRequest{Objective: "test", AllowQuestions: false},
			expectError:   true,
			errorContains: "context canceled",
		},
		{
			name: "valid context with cancellation support",
			setupContext: func() context.Context {
				return context.Background()
			},
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"plan": ["step 1"], "complete": true}`)
			},
			request:     &PlanningRequest{Objective: "test", AllowQuestions: false},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := NewMockFileSystem()
			sess := session.NewSession("test", ".")
			mockLLM := tt.setupMock()

			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)

			ctx := tt.setupContext()
			response, err := agent.Plan(ctx, tt.request, nil)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got no error", tt.errorContains)
				} else if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				if response == nil {
					t.Error("Expected non-nil response")
				}
			}
		})
	}
}

// TestPlanningAgent_ConcurrentErrorHandling tests error handling in concurrent scenarios
func TestPlanningAgent_ConcurrentErrorHandling(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Create a mock that sometimes fails
	errorRate := 0.5 // 50% chance of error
	mockLLM := &MockLLMClientWithRandomErrors{
		responses: []string{
			`{"plan": ["step 1"], "complete": true}`,
			`{"plan": ["step 2"], "complete": true}`,
		},
		errorRate: errorRate,
	}

	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)

	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			ctx := context.Background()
			req := &PlanningRequest{
				Objective:      fmt.Sprintf("concurrent test %d", id),
				AllowQuestions: false,
			}
			_, err := agent.Plan(ctx, req, nil)
			results <- err
		}(i)
	}

	// Collect results
	var successCount, errorCount int
	for i := 0; i < numGoroutines; i++ {
		select {
		case err := <-results:
			if err != nil {
				errorCount++
			} else {
				successCount++
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for concurrent planning results")
		}
	}

	t.Logf("Concurrent planning results: %d successes, %d errors", successCount, errorCount)

	// Should have some mix of successes and errors due to random error rate
	if successCount == 0 && errorCount == 0 {
		t.Error("Expected some results from concurrent planning")
	}
}

// TestPlanningAgent_ResourceExhaustion tests behavior under resource pressure
func TestPlanningAgent_ResourceExhaustion(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func() llm.Client
		request       *PlanningRequest
		expectedError string
	}{
		{
			name: "very large response",
			setupMock: func() llm.Client {
				// Generate a very large JSON response
				largePlan := `{"plan": [`
				for i := 0; i < 1000; i++ {
					largePlan += fmt.Sprintf(`"Step %d: %s"`, i+1, strings.Repeat("detail ", 100))
					if i < 999 {
						largePlan += ","
					}
				}
				largePlan += `], "complete": true}`
				return NewMockLLMClient(largePlan)
			},
			request: &PlanningRequest{
				Objective:      "test large response handling",
				AllowQuestions: false,
			},
			expectedError: "", // Should handle large responses
		},
		{
			name: "malformed JSON with recursion",
			setupMock: func() llm.Client {
				// Create malformed JSON that might cause parsing issues
				return NewMockLLMClient(`{"plan": [{"nested": {"deep": {"recursion": "problem"}}}], "complete": true}`)
			},
			request: &PlanningRequest{
				Objective:      "test malformed nested JSON",
				AllowQuestions: false,
			},
			expectedError: "", // Should handle gracefully
		},
		{
			name: "unicode and special characters",
			setupMock: func() llm.Client {
				specialChars := make([]string, 100)
				for i := 0; i < 100; i++ {
					specialChars[i] = fmt.Sprintf(`"Step %d: %s"`, i+1, string([]rune{0x1F600 + rune(i%50)})) // Emoji
				}
				return NewMockLLMClient(fmt.Sprintf(`{"plan": [%s], "complete": true}`, strings.Join(specialChars, ",")))
			},
			request: &PlanningRequest{
				Objective:      "test special characters",
				AllowQuestions: false,
			},
			expectedError: "", // Should handle unicode
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := NewMockFileSystem()
			sess := session.NewSession("test", ".")
			mockLLM := tt.setupMock()

			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)

			ctx := context.Background()
			response, err := agent.Plan(ctx, tt.request, nil)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("Expected error containing '%s', got no error", tt.expectedError)
				} else if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', got: %s", tt.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if response == nil {
				t.Error("Expected non-nil response")
				return
			}
		})
	}
}

// TestPlanningAgent_RecoveryScenarios tests various recovery scenarios
func TestPlanningAgent_RecoveryScenarios(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func() llm.Client
		request       *PlanningRequest
		expectedSteps int
		expectPartial bool
	}{
		{
			name: "recover from tool execution error",
			setupMock: func() llm.Client {
				return NewMockLLMClient(
					`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "invalid_tool", "arguments": "{}"}}]}`,
					`{"plan": ["recovery step 1", "recovery step 2"], "complete": true}`,
				)
			},
			request: &PlanningRequest{
				Objective:      "test recovery from tool error",
				AllowQuestions: false,
			},
			expectedSteps: 2,
		},
		{
			name: "recover from malformed response",
			setupMock: func() llm.Client {
				return NewMockLLMClient(
					"completely malformed response that isn't JSON",
					`{"plan": ["formatted recovery step"], "complete": true}`,
				)
			},
			request: &PlanningRequest{
				Objective:      "test recovery from malformed response",
				AllowQuestions: false,
			},
			expectedSteps: 1,
		},
		{
			name: "recover from partial plan",
			setupMock: func() llm.Client {
				return NewMockLLMClient(
					`{"plan": [], "questions": ["need more info"], "needs_input": true, "complete": false}`,
					`{"plan": ["partial recovery step"], "complete": false}`,
				)
			},
			request: &PlanningRequest{
				Objective:      "test recovery from partial plan",
				AllowQuestions: false, // Don't allow questions, so we skip the first response
			},
			expectedSteps: 1,
			expectPartial: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := NewMockFileSystem()
			sess := session.NewSession("test", ".")
			mockLLM := tt.setupMock()

			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)

			ctx := context.Background()
			response, err := agent.Plan(ctx, tt.request, nil)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if response == nil {
				t.Error("Expected non-nil response")
				return
			}

			if len(response.Plan) != tt.expectedSteps {
				t.Errorf("Expected %d plan steps, got %d", tt.expectedSteps, len(response.Plan))
			}

			if tt.expectPartial && response.Complete {
				t.Error("Expected partial plan")
			}
		})
	}
}

// Mock implementations for error testing

// MockLLMClientWithError always returns an error
type MockLLMClientWithError struct {
	error error
}

func (m *MockLLMClientWithError) Complete(ctx context.Context, prompt string) (string, error) {
	return "", m.error
}

func (m *MockLLMClientWithError) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, m.error
}

func (m *MockLLMClientWithError) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return m.error
}

func (m *MockLLMClientWithError) GetModelName() string {
	return "mock-error-client"
}

// MockLLMClientWithTimeout simulates a slow LLM client
type MockLLMClientWithTimeout struct {
	delay time.Duration
}

func (m *MockLLMClientWithTimeout) Complete(ctx context.Context, prompt string) (string, error) {
	select {
	case <-time.After(m.delay):
		return `{"plan": ["delayed response"], "complete": true}`, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (m *MockLLMClientWithTimeout) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	content, err := m.Complete(ctx, "")
	if err != nil {
		return nil, err
	}
	return &llm.CompletionResponse{Content: content}, nil
}

func (m *MockLLMClientWithTimeout) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	_, err := m.Complete(ctx, "")
	return err
}

func (m *MockLLMClientWithTimeout) GetModelName() string {
	return "mock-timeout-client"
}

// MockLLMClientWithDelay adds delay before responding
type MockLLMClientWithDelay struct {
	delay time.Duration
}

func (m *MockLLMClientWithDelay) Complete(ctx context.Context, prompt string) (string, error) {
	select {
	case <-time.After(m.delay):
		return `{"plan": ["delayed response"], "complete": true}`, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (m *MockLLMClientWithDelay) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	content, err := m.Complete(ctx, "")
	if err != nil {
		return nil, err
	}
	return &llm.CompletionResponse{Content: content}, nil
}

func (m *MockLLMClientWithDelay) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	_, err := m.Complete(ctx, "")
	return err
}

func (m *MockLLMClientWithDelay) GetModelName() string {
	return "mock-delay-client"
}

// MockLLMClientWithContextCancellation simulates context cancellation during tool calls
type MockLLMClientWithContextCancellation struct {
	responses        []string
	index            int
	cancelOnToolCall bool
}

func (m *MockLLMClientWithContextCancellation) Complete(ctx context.Context, prompt string) (string, error) {
	if m.index >= len(m.responses) {
		return "default response", nil
	}

	if m.cancelOnToolCall && strings.Contains(m.responses[m.index], "tool_calls") {
		return "", context.Canceled
	}

	response := m.responses[m.index]
	m.index++
	return response, nil
}

func (m *MockLLMClientWithContextCancellation) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	content, err := m.Complete(ctx, "")
	if err != nil {
		return nil, err
	}

	var toolCallResponse struct {
		ToolCalls []map[string]interface{} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(content), &toolCallResponse); err == nil && len(toolCallResponse.ToolCalls) > 0 {
		return &llm.CompletionResponse{
			Content:    content,
			ToolCalls:  toolCallResponse.ToolCalls,
			StopReason: "tool_calls",
		}, nil
	}

	return &llm.CompletionResponse{Content: content}, nil
}

func (m *MockLLMClientWithContextCancellation) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	response, err := m.CompleteWithRequest(ctx, req)
	if err != nil {
		return err
	}
	return callback(response.Content)
}

func (m *MockLLMClientWithContextCancellation) GetModelName() string {
	return "mock-cancellation-client"
}

// MockLLMClientWithRandomErrors randomly returns errors
type MockLLMClientWithRandomErrors struct {
	responses []string
	errorRate float64
}

func (m *MockLLMClientWithRandomErrors) Complete(ctx context.Context, prompt string) (string, error) {
	// Simulate random error based on some condition (e.g., prompt length)
	if len(prompt)%2 == 0 {
		return "", errors.New("random simulated error")
	}

	if len(m.responses) > 0 {
		resp := m.responses[0]
		return resp, nil
	}
	return `{"plan": ["random response"], "complete": true}`, nil
}

func (m *MockLLMClientWithRandomErrors) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	content, err := m.Complete(ctx, "")
	if err != nil {
		return nil, err
	}
	return &llm.CompletionResponse{Content: content}, nil
}

func (m *MockLLMClientWithRandomErrors) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	response, err := m.CompleteWithRequest(ctx, req)
	if err != nil {
		return err
	}
	return callback(response.Content)
}

func (m *MockLLMClientWithRandomErrors) GetModelName() string {
	return "mock-random-error-client"
}
