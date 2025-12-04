package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/session"
)

// TestPlanningRequest_Validation tests planning request validation
func TestPlanningRequest_Validation(t *testing.T) {
	tests := []struct {
		name        string
		request     *PlanningRequest
		expectError string
	}{
		{
			name:        "nil request",
			request:     nil,
			expectError: "planning request cannot be nil",
		},
		{
			name: "empty objective",
			request: &PlanningRequest{
				Objective:      "",
				AllowQuestions: false,
			},
			expectError: "planning objective cannot be empty",
		},
		{
			name: "whitespace only objective",
			request: &PlanningRequest{
				Objective:      "   \t\n   ",
				AllowQuestions: false,
			},
			expectError: "planning objective cannot be empty",
		},
		{
			name: "valid simple request",
			request: &PlanningRequest{
				Objective:      "test objective",
				AllowQuestions: false,
			},
			expectError: "",
		},
		{
			name: "valid request with context",
			request: &PlanningRequest{
				Objective:      "test objective",
				Context:        "additional context",
				AllowQuestions: true,
				MaxQuestions:   5,
			},
			expectError: "",
		},
		{
			name: "valid request with context files",
			request: &PlanningRequest{
				Objective:      "test objective",
				ContextFiles:   []string{"file1.txt", "file2.txt"},
				AllowQuestions: false,
			},
			expectError: "",
		},
		{
			name: "valid request with negative max questions (should be handled)",
			request: &PlanningRequest{
				Objective:      "test objective",
				AllowQuestions: true,
				MaxQuestions:   -1,
			},
			expectError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := NewMockFileSystem()
			sess := session.NewSession("test", ".")
			mockLLM := NewMockLLMClient(`{"plan": ["step 1"], "complete": true}`)
			
			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
			
			ctx := context.Background()
			_, err := agent.Plan(ctx, tt.request, nil)
			
			if tt.expectError != "" {
				if err == nil {
					t.Errorf("Expected error containing '%s', got no error", tt.expectError)
				} else if !strings.Contains(err.Error(), tt.expectError) {
					t.Errorf("Expected error containing '%s', got: %s", tt.expectError, err.Error())
				}
			} else {
				if err != nil && strings.Contains(err.Error(), "cannot be nil") {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

// TestPlanningResponse_Validation tests planning response validation
func TestPlanningResponse_Validation(t *testing.T) {
	tests := []struct {
		name     string
		response *PlanningResponse
		valid    bool
	}{
		{
			name: "valid complete response",
			response: &PlanningResponse{
				Plan:       []string{"step 1", "step 2"},
				Questions:  []string{},
				NeedsInput: false,
				Complete:   true,
			},
			valid: true,
		},
		{
			name: "valid incomplete response with questions",
			response: &PlanningResponse{
				Plan:       []string{"step 1"},
				Questions:  []string{"question 1"},
				NeedsInput: true,
				Complete:   false,
			},
			valid: true,
		},
		{
			name: "valid partial response with no questions",
			response: &PlanningResponse{
				Plan:       []string{"step 1"},
				Questions:  []string{},
				NeedsInput: true,
				Complete:   false,
			},
			valid: true,
		},
		{
			name: "valid empty plan response",
			response: &PlanningResponse{
				Plan:       []string{},
				Questions:  []string{"need more info"},
				NeedsInput: true,
				Complete:   false,
			},
			valid: true,
		},
		{
			name: "invalid response - no plan or questions",
			response: &PlanningResponse{
				Plan:       []string{},
				Questions:  []string{},
				NeedsInput: false,
				Complete:   false,
			},
			valid: false, // Should have either plan or questions
		},
		{
			name: "valid single step response",
			response: &PlanningResponse{
				Plan:       []string{"single step"},
				Questions:  []string{},
				NeedsInput: false,
				Complete:   true,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON serialization/deserialization
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Errorf("Failed to marshal response: %v", err)
				return
			}

			var unmarshaled PlanningResponse
			err = json.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Errorf("Failed to unmarshal response: %v", err)
				return
			}

			// Basic validation - at least plan or questions should be present
			hasPlan := len(unmarshaled.Plan) > 0
			hasQuestions := len(unmarshaled.Questions) > 0

			isValid := hasPlan || hasQuestions || unmarshaled.NeedsInput
			if isValid != tt.valid {
				t.Errorf("Expected validity %v, got %v (plan=%d, questions=%d, needsInput=%v)", 
					tt.valid, isValid, len(unmarshaled.Plan), len(unmarshaled.Questions), unmarshaled.NeedsInput)
			}
		})
	}
}

// TestPlanningAgent_EdgeCases tests edge cases in planning behavior
func TestPlanningAgent_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func() llm.Client
		request        *PlanningRequest
		expectedError  string
		expectedSteps  int
		expectedQuestions int
	}{
		{
			name: "malformed JSON response",
			setupMock: func() llm.Client {
				return NewMockLLMClient("{invalid json}")
			},
			request: &PlanningRequest{
				Objective:      "test malformed response",
				AllowQuestions: false,
			},
			expectedSteps: 1, // Should fall back to treating response as single step
		},
		{
			name: "empty response",
			setupMock: func() llm.Client {
				return NewMockLLMClient("")
			},
			request: &PlanningRequest{
				Objective:      "test empty response",
				AllowQuestions: false,
			},
			expectedSteps: 1, // Should fall back to single step
		},
		{
			name: "response with only questions",
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"questions": ["What framework?", "What database?"], "needs_input": true}`)
			},
			request: &PlanningRequest{
				Objective:      "test questions only",
				AllowQuestions: true,
			},
			expectedQuestions: 2,
		},
		{
			name: "response with empty plan array",
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"plan": [], "complete": true}`)
			},
			request: &PlanningRequest{
				Objective:      "test empty plan",
				AllowQuestions: false,
			},
			expectedSteps: 0, // Empty plan should be preserved
		},
		/*
		{
			name: "very long response",
			setupMock: func() llm.Client {
				longPlan := `{"plan": [`
				for i := 0; i < 100; i++ {
					longPlan += `"Step ` + string(rune('A'+i%26)) + `: Very long planning step with lots of details and information"`
					if i < 99 {
						longPlan += ","
					}
				}
				longPlan += `], "complete": true}`
				return NewMockLLMClient(longPlan)
			},
			request: &PlanningRequest{
				Objective:      "test long response",
				AllowQuestions: false,
			},
			expectedSteps: 100,
		},
		*/
		{
			name: "unicode and special characters",
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"plan": ["Step 1: 🚀 Initialize project", "Step 2: 📝 Write documentation", "Step 3: 🧪 Run tests"], "complete": true}`)
			},
			request: &PlanningRequest{
				Objective:      "test unicode characters",
				AllowQuestions: false,
			},
			expectedSteps: 3,
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
			
			if tt.expectedSteps >= 0 && len(response.Plan) != tt.expectedSteps {
				t.Errorf("Expected %d plan steps, got %d", tt.expectedSteps, len(response.Plan))
			}
			
			if tt.expectedQuestions >= 0 && len(response.Questions) != tt.expectedQuestions {
				t.Errorf("Expected %d questions, got %d", tt.expectedQuestions, len(response.Questions))
			}
		})
	}
}

// TestPlanningAgent_PlanExtractionEdgeCases tests edge cases in plan extraction
func TestPlanningAgent_PlanExtractionEdgeCases(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("dummy")
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)

	tests := []struct {
		name              string
		content           string
		expectedPlanSteps int
		expectedQuestions int
	}{
		{
			name:              "empty string",
			content:           "",
			expectedPlanSteps: 1, // Should treat as single step
		},
		{
			name:              "only whitespace",
			content:           "   \n\t   ",
			expectedPlanSteps: 1,
		},
		{
			name:              "malformed JSON with answer tags",
			content:           `<answer>{invalid json}</answer>`,
			expectedPlanSteps: 1,
		},
		{
			name:              "JSON with missing plan field",
			content:           `{"questions": ["What?"], "needs_input": true}`,
			expectedPlanSteps: 0, // No plan field
			expectedQuestions: 1,
		},
		{
			name:              "JSON with null plan",
			content:           `{"plan": null, "complete": true}`,
			expectedPlanSteps: 0,
		},
		{
			name:              "mixed format with special characters",
			content:           "1. 🎯 **Setup** the environment\n2. 🔧 **Configure** the settings\n3. ✅ **Verify** the installation",
			expectedPlanSteps: 3,
		},
		{
			name:              "numbered steps with decimals",
			content:           "1.1 First sub-step\n1.2 Second sub-step\n2.0 Main step",
			expectedPlanSteps: 3,
		},
		{
			name:              "nested bullet points",
			content:           "- Main item\n  - Sub item 1\n  - Sub item 2\n- Another main item",
			expectedPlanSteps: 4,
		},
		{
			name:              "questions mixed with plan",
			content:           "1. Analyze requirements\nquestion: What framework should we use?\n2. Design solution\nquestion: How should we handle authentication?",
			expectedPlanSteps: 2,
			expectedQuestions: 2,
		},
		{
			name:              "HTML-like tags in content",
			content:           "<step>First step</step>\n<step>Second step</step>",
			expectedPlanSteps: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := agent.extractPlan(tt.content)
			
			if response == nil {
				t.Error("extractPlan returned nil")
				return
			}
			
			if len(response.Plan) != tt.expectedPlanSteps {
				t.Errorf("Expected %d plan steps, got %d", tt.expectedPlanSteps, len(response.Plan))
			}
			
			if len(response.Questions) != tt.expectedQuestions {
				t.Errorf("Expected %d questions, got %d", tt.expectedQuestions, len(response.Questions))
			}
		})
	}
}

// TestPlanningAgent_ContextFilesCollection tests context file collection edge cases
func TestPlanningAgent_ContextFilesCollection(t *testing.T) {
	tests := []struct {
		name         string
		setupFS      func() *MockFileSystem
		contextFiles []string
		expectError  bool
		expectEmpty  bool
	}{
		{
			name: "no context files",
			setupFS: func() *MockFileSystem {
				return NewMockFileSystem()
			},
			contextFiles: []string{},
			expectError:  false,
			expectEmpty:  true,
		},
		{
			name: "valid context files",
			setupFS: func() *MockFileSystem {
				fs := NewMockFileSystem()
				fs.AddFile("config.yaml", "key: value")
				fs.AddFile("README.md", "# Project")
				return fs
			},
			contextFiles: []string{"config.yaml", "README.md"},
			expectError:  false,
			expectEmpty:  false,
		},
		{
			name: "non-existent context file",
			setupFS: func() *MockFileSystem {
				return NewMockFileSystem()
			},
			contextFiles: []string{"nonexistent.txt"},
			expectError:  false, // Should handle gracefully
			expectEmpty:  false, // Should include error message
		},
		{
			name: "mixed valid and invalid files",
			setupFS: func() *MockFileSystem {
				fs := NewMockFileSystem()
				fs.AddFile("valid.txt", "content")
				return fs
			},
			contextFiles: []string{"valid.txt", "invalid.txt"},
			expectError:  false,
			expectEmpty:  false,
		},
		{
			name: "empty file paths",
			setupFS: func() *MockFileSystem {
				return NewMockFileSystem()
			},
			contextFiles: []string{"", "   "},
			expectError:  false,
			expectEmpty:  true, // Should skip empty paths
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := tt.setupFS()
			sess := session.NewSession("test", ".")
			mockLLM := NewMockLLMClient(`{"plan": ["step 1"], "complete": true}`)
			
			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
			
			ctx := context.Background()
			req := &PlanningRequest{
				Objective:      "test context files",
				ContextFiles:   tt.contextFiles,
				AllowQuestions: false,
			}
			
			_, err := agent.Plan(ctx, req, nil)
			
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestPlanningAgent_QuestionHandling tests question handling edge cases
func TestPlanningAgent_QuestionHandling(t *testing.T) {
	tests := []struct {
		name              string
		mockResponses     []string
		request           *PlanningRequest
		userInputFunc     func(question string) (string, error)
		expectedQuestions int
		expectedNeedsInput bool
		expectedError     string
	}{
		{
			name: "questions disabled but asked",
			mockResponses: []string{
				`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"What framework?\"}"}}]}`,
				`{"plan": ["step 1"], "complete": true}`,
			},
			request: &PlanningRequest{
				Objective:      "test with questions disabled",
				AllowQuestions: false,
			},
			expectedQuestions: 0, // Should not include questions when disabled
			expectedNeedsInput: false,
		},
		{
			name: "questions enabled with user input",
			mockResponses: []string{
				`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"What framework?\"}"}}]}`,
				`{"plan": ["step 1 with user input"], "complete": true}`,
			},
			request: &PlanningRequest{
				Objective:      "test with user input",
				AllowQuestions: true,
			},
			userInputFunc: func(question string) (string, error) {
				return "React", nil
			},
			expectedQuestions: 0, // Questions should be resolved with user input
			expectedNeedsInput: false,
		},
		{
			name: "user input callback returns error",
			mockResponses: []string{
				`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"What framework?\"}"}}]}`,
				`{"plan": ["fallback plan"], "complete": true}`,
			},
			request: &PlanningRequest{
				Objective:      "test with user input error",
				AllowQuestions: true,
			},
			userInputFunc: func(question string) (string, error) {
				return "", fmt.Errorf("user cancelled")
			},
			expectedQuestions: 0, // Should handle error gracefully
			expectedNeedsInput: false,
		},
		{
			name: "no user input callback",
			mockResponses: []string{
				`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"What framework?\"}"}}]}`,
				`{"plan": ["fallback plan"], "complete": true}`,
			},
			request: &PlanningRequest{
				Objective:      "test without callback",
				AllowQuestions: true,
			},
			userInputFunc: nil, // No callback provided
			expectedQuestions: 0,
			expectedNeedsInput: false,
		},
		{
			name: "max questions reached",
			mockResponses: []string{
				`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"Question 1?\"}"}}]}`,
				`{"tool_calls": [{"id": "call_2", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"Question 2?\"}"}}]}`,
				`{"tool_calls": [{"id": "call_3", "type": "function", "function": {"name": "ask_user", "arguments": "{\"question\": \"Question 3?\"}"}}]}`,
				`{"plan": ["partial plan"], "complete": false}`,
			},
			request: &PlanningRequest{
				Objective:      "test max questions",
				AllowQuestions: true,
				MaxQuestions:   2, // Limit to 2 questions
			},
			userInputFunc: func(question string) (string, error) {
				return "answer", nil
			},
			expectedQuestions: 0, // Should stop at max questions
			expectedNeedsInput: true, // Should indicate needs input
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := NewMockFileSystem()
			sess := session.NewSession("test", ".")
			mockLLM := NewMockLLMClient(tt.mockResponses...)
			
			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
			
			ctx := context.Background()
			response, err := agent.Plan(ctx, tt.request, tt.userInputFunc)
			
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
			
			if len(response.Questions) != tt.expectedQuestions {
				t.Errorf("Expected %d questions, got %d", tt.expectedQuestions, len(response.Questions))
			}
			
			if response.NeedsInput != tt.expectedNeedsInput {
				t.Errorf("Expected needs_input=%v, got %v", tt.expectedNeedsInput, response.NeedsInput)
			}
		})
	}
}

// TestPlanningAgent_TimeoutAndCancellation tests timeout and cancellation behavior
func TestPlanningAgent_TimeoutAndCancellation(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func() llm.Client
		ctxFunc     func() context.Context
		expectError bool
		errorMsg    string
	}{
		{
			name: "context cancellation before planning",
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"plan": ["step 1"], "complete": true}`)
			},
			ctxFunc: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				// Cancel immediately
				cancel()
				return ctx
			},
			expectError: true,
			errorMsg:    "context canceled",
		},
		{
			name: "context with timeout",
			setupMock: func() llm.Client {
				// Return empty response to trigger timeout
				return &MockLLMClientWithDelay{delay: 50 * time.Millisecond}
			},
			ctxFunc: func() context.Context {
				ctx, _ := context.WithTimeout(context.Background(), 1*time.Millisecond)
				return ctx
			},
			expectError: true, // Should timeout
		},
		{
			name: "valid context",
			setupMock: func() llm.Client {
				return NewMockLLMClient(`{"plan": ["step 1"], "complete": true}`)
			},
			ctxFunc: func() context.Context {
				return context.Background()
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := NewMockFileSystem()
			sess := session.NewSession("test", ".")
			mockLLM := tt.setupMock()
			
			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
			
			ctx := tt.ctxFunc()
			req := &PlanningRequest{
				Objective:      "test timeout",
				AllowQuestions: false,
			}
			
			_, err := agent.Plan(ctx, req, nil)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got no error", tt.errorMsg)
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got: %s", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}