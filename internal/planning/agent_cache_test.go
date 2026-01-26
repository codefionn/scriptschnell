package planning

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
)

// TestPlanningAgentMessagePrefixStability ensures that the message prefix
// remains immutable during the planning lifecycle to preserve prompt caching.
func TestPlanningAgentMessagePrefixStability(t *testing.T) {
	tests := []struct {
		name            string
		objective       string
		context         string
		contextFiles    []string
		expectedMinMsgs int
		description     string
	}{
		{
			name:            "objective_only",
			objective:       "Create a plan for implementing feature X",
			context:         "",
			contextFiles:    nil,
			expectedMinMsgs: 1, // user objective
			description:     "Only objective provided",
		},
		{
			name:            "with_context",
			objective:       "Create a plan for implementing feature X",
			context:         "This is additional context about the feature",
			contextFiles:    nil,
			expectedMinMsgs: 2, // user objective + user context
			description:     "Objective with additional context",
		},
		{
			name:            "with_context_files",
			objective:       "Create a plan for implementing feature X",
			context:         "",
			contextFiles:    []string{"testdata/example.go"},
			expectedMinMsgs: 1, // user objective (context files empty)
			description:     "Objective with context files",
		},
		{
			name:            "with_both",
			objective:       "Create a plan for implementing feature X",
			context:         "Additional context",
			contextFiles:    []string{"testdata/example.go"},
			expectedMinMsgs: 2, // user objective + user context
			description:     "Objective with both context and files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client that tracks message sequences
			mockClient := &MessageTrackingClient{
				responses: []llm.CompletionResponse{
					{
						Content:    "<answer>{\"plan\": [\"Step 1\", \"Step 2\"], \"complete\": true}</answer>",
						ToolCalls:  nil,
						StopReason: "end_turn",
					},
				},
			}

			agent := &PlanningAgent{
				id:           "test-agent",
				client:       mockClient,
				toolRegistry: NewPlanningToolRegistry(),
				loopDetector: loopdetector.NewLoopDetector(),
			}

			req := &PlanningRequest{
				Objective:      tt.objective,
				Context:        tt.context,
				ContextFiles:   tt.contextFiles,
				AllowQuestions: false,
			}

			ctx := context.Background()
			_, err := agent.plan(ctx, req, nil, nil, nil, nil)
			if err != nil {
				t.Fatalf("Planning failed: %v", err)
			}

			// Verify that we received at least one request
			if len(mockClient.requests) == 0 {
				t.Fatal("Expected at least one LLM request, got none")
			}

			// Verify the first request has the expected prefix length
			firstReq := mockClient.requests[0]
			if len(firstReq.Messages) < tt.expectedMinMsgs {
				t.Errorf("First request: expected at least %d messages, got %d", tt.expectedMinMsgs, len(firstReq.Messages))
			}

			// Verify objective message is first
			if len(firstReq.Messages) > 0 && firstReq.Messages[0].Role != "user" {
				t.Errorf("First message should be user, got: %s", firstReq.Messages[0].Role)
			}

			// Verify any additional context message is also user
			if len(firstReq.Messages) > 1 && firstReq.Messages[1].Role != "user" {
				t.Errorf("Second message should be user, got: %s", firstReq.Messages[1].Role)
			}

			// If we have multiple iterations, verify prefix stability
			if len(mockClient.requests) > 1 {
				initialPrefix := firstReq.Messages[:tt.expectedMinMsgs]

				for i := 1; i < len(mockClient.requests); i++ {
					req := mockClient.requests[i]

					// Check that we have at least the initial prefix
					if len(req.Messages) < tt.expectedMinMsgs {
						t.Errorf("Request %d: prefix was truncated! Expected at least %d messages, got %d",
							i, tt.expectedMinMsgs, len(req.Messages))
						continue
					}

					// Verify prefix messages match exactly
					for j := 0; j < tt.expectedMinMsgs; j++ {
						if req.Messages[j].Role != initialPrefix[j].Role {
							t.Errorf("Request %d, message %d: role mismatch. Expected %s, got %s",
								i, j, initialPrefix[j].Role, req.Messages[j].Role)
						}
						if req.Messages[j].Content != initialPrefix[j].Content {
							t.Errorf("Request %d, message %d: content changed (CACHE BREAK)",
								i, j)
						}
					}
				}
			}
		})
	}
}

// TestPlanningAgentMessageSequenceWithToolCalls verifies that the message
// sequence follows the correct pattern: user -> assistant -> tool -> assistant
func TestPlanningAgentMessageSequenceWithToolCalls(t *testing.T) {
	// Mock client that simulates tool calls
	mockClient := &MessageTrackingClient{
		responses: []llm.CompletionResponse{
			{
				Content: "I need to ask a question",
				ToolCalls: []map[string]interface{}{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "ask_user",
							"arguments": `{"question": "What should we do?"}`,
						},
					},
				},
				StopReason: "tool_use",
			},
			{
				Content:    "<answer>{\"plan\": [\"Step 1\"], \"complete\": true}</answer>",
				ToolCalls:  nil,
				StopReason: "end_turn",
			},
		},
	}

	agent := &PlanningAgent{
		id:           "test-agent",
		client:       mockClient,
		toolRegistry: NewPlanningToolRegistry(),
		loopDetector: loopdetector.NewLoopDetector(),
	}

	req := &PlanningRequest{
		Objective:      "Create a plan",
		AllowQuestions: false,
	}

	ctx := context.Background()
	_, err := agent.plan(ctx, req, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// We should have 2 requests: one with tool call, one after tool result
	if len(mockClient.requests) != 2 {
		t.Fatalf("Expected 2 requests, got %d", len(mockClient.requests))
	}

	// First request: user objective (and optional context)
	firstReq := mockClient.requests[0]
	if len(firstReq.Messages) < 1 {
		t.Fatalf("First request should have at least 1 message (user objective)")
	}

	// Second request: user + assistant (with tool calls) + tool result
	secondReq := mockClient.requests[1]
	expectedSecondLen := 3 // user + assistant + tool
	if len(secondReq.Messages) != expectedSecondLen {
		t.Errorf("Second request should have %d messages, got %d", expectedSecondLen, len(secondReq.Messages))
	}

	// Verify message sequence in second request
	if len(secondReq.Messages) >= 3 {
		if secondReq.Messages[0].Role != "user" {
			t.Errorf("Message 0 should be user, got %s", secondReq.Messages[0].Role)
		}
		if secondReq.Messages[1].Role != "assistant" {
			t.Errorf("Message 1 should be assistant, got %s", secondReq.Messages[1].Role)
		}
		if secondReq.Messages[2].Role != "tool" {
			t.Errorf("Message 2 should be tool, got %s", secondReq.Messages[2].Role)
		}
	}

	// Verify that prefix (initial user messages) is identical between requests
	if len(firstReq.Messages) > 0 && len(secondReq.Messages) > 0 && firstReq.Messages[0].Content != secondReq.Messages[0].Content {
		t.Error("Initial objective message content changed between requests (CACHE BREAK)")
	}
}

// MessageTrackingClient tracks all requests for verification
type MessageTrackingClient struct {
	requests  []llm.CompletionRequest
	responses []llm.CompletionResponse
	callIndex int
}

func (m *MessageTrackingClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Store the request for later verification
	m.requests = append(m.requests, *req)

	// Return the next response
	if m.callIndex >= len(m.responses) {
		// Return empty response if we run out
		return &llm.CompletionResponse{
			Content:    "<answer>{\"plan\": [], \"complete\": true}</answer>",
			StopReason: "end_turn",
		}, nil
	}

	resp := m.responses[m.callIndex]
	m.callIndex++
	return &resp, nil
}

func (m *MessageTrackingClient) Complete(ctx context.Context, prompt string) (string, error) {
	panic("not implemented")
}

func (m *MessageTrackingClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	resp, err := m.CompleteWithRequest(ctx, req)
	if err != nil {
		return err
	}
	return callback(resp.Content)
}

func (m *MessageTrackingClient) GetModelName() string {
	return "test-tracking-model"
}

func (m *MessageTrackingClient) GetLastResponseID() string {
	return "" // Not used in tests
}

func (m *MessageTrackingClient) SetPreviousResponseID(responseID string) {
	// Not used in tests
}
