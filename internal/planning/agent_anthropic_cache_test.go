package planning

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// TestPlanningAgent_EnablesCaching verifies that the planning agent
// passes EnableCaching=true to the LLM client
func TestPlanningAgent_EnablesCaching(t *testing.T) {
	// Mock client that captures completion requests
	mockClient := &CaptureRequestClient{
		responses: []llm.CompletionResponse{
			{
				Content:    "<answer>{\"plan\": [\"Step 1\"], \"complete\": true}</answer>",
				StopReason: "end_turn",
			},
		},
	}

	agent := &PlanningAgent{
		id:           "test-agent",
		client:       mockClient,
		toolRegistry: NewPlanningToolRegistry(),
	}

	req := &PlanningRequest{
		Objective:      "Create a simple plan",
		AllowQuestions: false,
	}

	ctx := context.Background()
	_, err := agent.plan(ctx, req, nil, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify that we received at least one request
	if len(mockClient.capturedRequests) == 0 {
		t.Fatal("Expected at least one LLM request, got none")
	}

	// Verify that EnableCaching is true in the first request
	firstReq := mockClient.capturedRequests[0]
	if !firstReq.EnableCaching {
		t.Error("Expected EnableCaching to be true, got false")
	}

	// Verify that CacheTTL is set
	if firstReq.CacheTTL != "5m" {
		t.Errorf("Expected CacheTTL to be '5m', got '%s'", firstReq.CacheTTL)
	}
}

// CaptureRequestClient captures all completion requests for inspection
type CaptureRequestClient struct {
	capturedRequests []llm.CompletionRequest
	responses        []llm.CompletionResponse
	callIndex        int
}

func (c *CaptureRequestClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Capture the request
	c.capturedRequests = append(c.capturedRequests, *req)

	// Return the next response
	if c.callIndex >= len(c.responses) {
		return &llm.CompletionResponse{
			Content:    "<answer>{\"plan\": [], \"complete\": true}</answer>",
			StopReason: "end_turn",
		}, nil
	}

	resp := c.responses[c.callIndex]
	c.callIndex++
	return &resp, nil
}

func (c *CaptureRequestClient) Complete(ctx context.Context, prompt string) (string, error) {
	panic("not implemented")
}

func (c *CaptureRequestClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	resp, err := c.CompleteWithRequest(ctx, req)
	if err != nil {
		return err
	}
	return callback(resp.Content)
}

func (c *CaptureRequestClient) GetModelName() string {
	return "test-capture-model"
}
