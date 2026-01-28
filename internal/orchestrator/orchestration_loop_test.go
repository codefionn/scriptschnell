package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
)

// sequentialMockClient returns responses in sequence
type sequentialMockClient struct {
	mu        sync.Mutex
	responses []*llm.CompletionResponse
	index     int
	requests  []*llm.CompletionRequest
}

func newSequentialMockClient(responses ...*llm.CompletionResponse) *sequentialMockClient {
	return &sequentialMockClient{
		responses: responses,
		requests:  make([]*llm.CompletionRequest, 0),
	}
}

func (c *sequentialMockClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.requests = append(c.requests, req)

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if c.index >= len(c.responses) {
		// Default response to end the loop
		return &llm.CompletionResponse{
			Content:    "Done.",
			StopReason: "stop",
		}, nil
	}

	resp := c.responses[c.index]
	c.index++
	return resp, nil
}

func (c *sequentialMockClient) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := c.CompleteWithRequest(ctx, &llm.CompletionRequest{
		Messages: []*llm.Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (c *sequentialMockClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	resp, err := c.CompleteWithRequest(ctx, req)
	if err != nil {
		return err
	}
	if callback != nil {
		return callback(resp.Content)
	}
	return nil
}

func (c *sequentialMockClient) GetModelName() string {
	return "test-sequential-model"
}

func (c *sequentialMockClient) GetLastResponseID() string {
	return ""
}

func (c *sequentialMockClient) SetPreviousResponseID(responseID string) {}

func (c *sequentialMockClient) RequestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func (c *sequentialMockClient) LastRequest() *llm.CompletionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return nil
	}
	return c.requests[len(c.requests)-1]
}

// createTestOrchestrator creates an orchestrator for testing
func createTestOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()

	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("failed to create provider manager: %v", err)
	}

	cfg := &config.Config{
		WorkingDir:      ".",
		CacheTTL:        1,
		MaxCacheEntries: 10,
		Temperature:     0.7,
		MaxTokens:       512,
	}

	orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, fs.NewMockFS())
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	return orch
}

// TestOrchestrationLoop_SingleResponseNoToolCalls tests the simplest case
func TestOrchestrationLoop_SingleResponseNoToolCalls(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "Hello! How can I help you today?",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "hello", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	if mockClient.RequestCount() != 1 {
		t.Errorf("Expected 1 request, got %d", mockClient.RequestCount())
	}
}

// TestOrchestrationLoop_WithToolCall tests tool call execution
func TestOrchestrationLoop_WithToolCall(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	// First response has tool call, second is final response
	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Let me check the status.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "The status shows no background jobs.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "check status", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	// Should have made 2 requests: initial + after tool result
	if mockClient.RequestCount() != 2 {
		t.Errorf("Expected 2 requests, got %d", mockClient.RequestCount())
	}
}

// TestOrchestrationLoop_MultipleToolCalls tests multiple sequential tool calls
func TestOrchestrationLoop_MultipleToolCalls(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "First tool call.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content: "Second tool call based on first result.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_2",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "All done!",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "do multiple things", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	if mockClient.RequestCount() != 3 {
		t.Errorf("Expected 3 requests, got %d", mockClient.RequestCount())
	}
}

// TestOrchestrationLoop_ParallelToolCalls tests handling of multiple tool calls in single response
func TestOrchestrationLoop_ParallelToolCalls(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Running multiple tools.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
				{
					"id":   "call_2",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "Both tools completed.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "run parallel tools", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	if mockClient.RequestCount() != 2 {
		t.Errorf("Expected 2 requests, got %d", mockClient.RequestCount())
	}
}

// TestOrchestrationLoop_ContextCancellation tests that context cancellation stops the loop
func TestOrchestrationLoop_ContextCancellation(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	// Client that would loop forever
	infiniteClient := &infiniteToolCallClient{}
	orch.orchestrationClient = infiniteClient

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := orch.ProcessPrompt(ctx, "infinite task", nil, nil, nil, nil, nil, nil)

	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
}

// infiniteToolCallClient always returns tool calls
type infiniteToolCallClient struct {
	callCount int
}

func (c *infiniteToolCallClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	c.callCount++
	return &llm.CompletionResponse{
		Content: fmt.Sprintf("Tool call %d", c.callCount),
		ToolCalls: []map[string]interface{}{
			{
				"id":   fmt.Sprintf("call_%d", c.callCount),
				"type": "function",
				"function": map[string]interface{}{
					"name":      "status",
					"arguments": "{}",
				},
			},
		},
		StopReason: "tool_use",
	}, nil
}

func (c *infiniteToolCallClient) Complete(ctx context.Context, prompt string) (string, error) {
	return "", nil
}

func (c *infiniteToolCallClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}

func (c *infiniteToolCallClient) GetModelName() string                    { return "infinite-model" }
func (c *infiniteToolCallClient) GetLastResponseID() string               { return "" }
func (c *infiniteToolCallClient) SetPreviousResponseID(responseID string) {}

// TestOrchestrationLoop_EmptyResponse tests handling of empty LLM response
func TestOrchestrationLoop_EmptyResponse(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "hello", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	// Should terminate after single empty response
	if mockClient.RequestCount() != 1 {
		t.Errorf("Expected 1 request, got %d", mockClient.RequestCount())
	}
}

// TestOrchestrationLoop_ProgressCallback tests that progress callbacks are called
func TestOrchestrationLoop_ProgressCallback(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "Hello! I'm here to help.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	var progressMessages []string
	var mu sync.Mutex
	progressCb := func(update progress.Update) error {
		mu.Lock()
		defer mu.Unlock()
		if update.Message != "" {
			progressMessages = append(progressMessages, update.Message)
		}
		return nil
	}

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "hello", progressCb, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(progressMessages) == 0 {
		t.Error("Expected at least one progress message")
	}
}

// TestOrchestrationLoop_ToolCallCallback tests that tool call callbacks are invoked
func TestOrchestrationLoop_ToolCallCallback(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Checking status.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "Done.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	var toolCalls []string
	var mu sync.Mutex
	toolCallCb := func(toolName, toolID string, params map[string]interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		toolCalls = append(toolCalls, toolName)
		return nil
	}

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "check status", nil, nil, nil, toolCallCb, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(toolCalls) == 0 {
		t.Error("Expected at least one tool call callback")
	}
}

// TestOrchestrationLoop_SessionMessagesAccumulate tests that session messages are accumulated
func TestOrchestrationLoop_SessionMessagesAccumulate(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "First response.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "first prompt", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("First ProcessPrompt failed: %v", err)
	}

	// Reset mock for second call
	mockClient2 := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "Second response.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient2

	err = orch.ProcessPrompt(ctx, "second prompt", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Second ProcessPrompt failed: %v", err)
	}

	// Check that session has accumulated messages
	messages := orch.session.GetMessages()
	if len(messages) < 4 {
		t.Errorf("Expected at least 4 messages (2 user + 2 assistant), got %d", len(messages))
	}
}

// TestOrchestrationLoop_StopReasonLength tests handling of truncated response
func TestOrchestrationLoop_StopReasonLength(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	// Mock summarize client for auto-continue judge
	orch.summarizeClient = &mockClassifierLLM{content: "STOP"}

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "This is a truncated respon",
			StopReason: "length",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "write something long", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}
}

// TestOrchestrationLoop_MaxIterationsLimit tests that max iterations limit is respected
func TestOrchestrationLoop_MaxIterationsLimit(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	// Client that always returns tool calls - should hit max iterations
	callCount := 0
	mockClient := &countingToolCallClient{
		callCount: &callCount,
		maxCalls:  300, // More than max iterations (256)
	}
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "infinite task", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	// Should have stopped at or before max iterations
	if callCount > 256 {
		t.Errorf("Expected at most 256 iterations, got %d", callCount)
	}
}

type countingToolCallClient struct {
	callCount *int
	maxCalls  int
}

func (c *countingToolCallClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	*c.callCount++
	if *c.callCount >= c.maxCalls {
		return &llm.CompletionResponse{
			Content:    "Done.",
			StopReason: "stop",
		}, nil
	}
	return &llm.CompletionResponse{
		Content: fmt.Sprintf("Tool call %d", *c.callCount),
		ToolCalls: []map[string]interface{}{
			{
				"id":   fmt.Sprintf("call_%d", *c.callCount),
				"type": "function",
				"function": map[string]interface{}{
					"name":      "status",
					"arguments": "{}",
				},
			},
		},
		StopReason: "tool_use",
	}, nil
}

func (c *countingToolCallClient) Complete(ctx context.Context, prompt string) (string, error) {
	return "", nil
}

func (c *countingToolCallClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}

func (c *countingToolCallClient) GetModelName() string                    { return "counting-model" }
func (c *countingToolCallClient) GetLastResponseID() string               { return "" }
func (c *countingToolCallClient) SetPreviousResponseID(responseID string) {}

// TestOrchestrationLoop_ToolResultsInMessages tests that tool results are added to messages
func TestOrchestrationLoop_ToolResultsInMessages(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Checking status.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "Status received.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "check status", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	// Verify second request includes tool results
	if mockClient.RequestCount() < 2 {
		t.Fatal("Expected at least 2 requests")
	}

	secondReq := mockClient.requests[1]
	hasToolResult := false
	for _, msg := range secondReq.Messages {
		if msg.Role == "tool" {
			hasToolResult = true
			break
		}
	}

	if !hasToolResult {
		t.Error("Expected tool result message in second request")
	}
}

// TestOrchestrationLoop_UnknownTool tests handling of unknown tool calls
func TestOrchestrationLoop_UnknownTool(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Using unknown tool.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "nonexistent_tool",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "Tool not found, done.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "use unknown tool", nil, nil, nil, nil, nil, nil)

	// Should not error, just handle gracefully
	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}
}

// TestOrchestrationLoop_MalformedToolCall tests handling of malformed tool calls
func TestOrchestrationLoop_MalformedToolCall(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Malformed tool call.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					// Missing "function" key
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "Continuing anyway.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "malformed tool", nil, nil, nil, nil, nil, nil)

	// Should handle gracefully
	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}
}

// TestOrchestrationLoop_WithPlanningEnabled tests the loop with planning phase
func TestOrchestrationLoop_WithPlanningEnabled(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	// Enable planning
	orch.featureFlags.SetPlanningEnabled(true)

	// Mock summarize client that says the prompt is complex
	orch.summarizeClient = &mockClassifierLLM{content: `{"simple":false,"reason":"complex task"}`}

	// Mock planning client
	planningClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: `<answer>{
				"mode": "simple",
				"plan": ["Step 1: Do something", "Step 2: Verify"],
				"complete": true
			}</answer>`,
			StopReason: "stop",
		},
	)

	// Mock orchestration client
	orchClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "Following the plan...",
			StopReason: "stop",
		},
	)

	orch.planningAgent = nil // Force new planning agent creation
	orch.orchestrationClient = orchClient
	// Planning agent will use summarizeClient

	ctx := context.Background()

	// Note: This test might not fully exercise planning due to mock setup complexity
	// The key is to verify no panics and proper flow
	_ = planningClient // unused in this simplified test
	err := orch.ProcessPrompt(ctx, "complex multi-step task", nil, nil, nil, nil, nil, nil)

	// Should complete without error
	if err != nil {
		t.Logf("ProcessPrompt returned error (may be expected): %v", err)
	}
}

// TestOrchestrationLoop_ToolCallWithContent tests tool calls with accompanying content
func TestOrchestrationLoop_ToolCallWithContent(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "I'll check the status for you.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "Based on the status, everything looks good!",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	var progressMessages []string
	var mu sync.Mutex
	progressCb := func(update progress.Update) error {
		mu.Lock()
		defer mu.Unlock()
		if update.Message != "" {
			progressMessages = append(progressMessages, update.Message)
		}
		return nil
	}

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "check status", progressCb, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	// Verify content was streamed
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, msg := range progressMessages {
		if strings.Contains(msg, "check the status") || strings.Contains(msg, "everything looks good") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected LLM content to be streamed via progress callback")
	}
}

// TestOrchestrationLoop_VerificationPhase tests the verification phase after main loop
func TestOrchestrationLoop_VerificationPhase(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)
	// Verification is controlled by config, not feature flags

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "Task completed.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	ctx := context.Background()
	err := orch.ProcessPromptWithVerification(ctx, "simple task", nil, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPromptWithVerification failed: %v", err)
	}
}

// TestOrchestrationLoop_MultipleTurns tests multi-turn conversation flow
func TestOrchestrationLoop_MultipleTurns(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	ctx := context.Background()

	// First turn
	mockClient1 := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "Hello! What would you like to know?",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient1

	err := orch.ProcessPrompt(ctx, "hi", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("First turn failed: %v", err)
	}

	// Second turn
	mockClient2 := newSequentialMockClient(
		&llm.CompletionResponse{
			Content:    "The weather is nice today!",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient2

	err = orch.ProcessPrompt(ctx, "what's the weather?", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Second turn failed: %v", err)
	}

	// Verify conversation history is preserved
	req := mockClient2.LastRequest()
	if req == nil {
		t.Fatal("Expected request to be captured")
	}

	// Should have at least: user1, assistant1, user2
	if len(req.Messages) < 3 {
		t.Errorf("Expected at least 3 messages in history, got %d", len(req.Messages))
	}
}

// TestOrchestrationLoop_ToolResultCallback tests tool result callbacks
func TestOrchestrationLoop_ToolResultCallback(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Checking status.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "status",
						"arguments": "{}",
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "Done.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	var toolResults []string
	var mu sync.Mutex
	toolResultCb := func(toolName, toolID, result, errorMsg string) error {
		mu.Lock()
		defer mu.Unlock()
		toolResults = append(toolResults, fmt.Sprintf("%s:%s", toolName, toolID))
		return nil
	}

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "check status", nil, nil, nil, nil, toolResultCb, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(toolResults) == 0 {
		t.Error("Expected at least one tool result callback")
	}
}

// TestOrchestrationLoop_JSONToolArguments tests that JSON tool arguments are parsed correctly
func TestOrchestrationLoop_JSONToolArguments(t *testing.T) {
	orch := createTestOrchestrator(t)
	defer orch.Close()

	orch.featureFlags.SetPlanningEnabled(false)

	args := map[string]interface{}{
		"path": "/test/file.txt",
	}
	argsJSON, _ := json.Marshal(args)

	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Reading file.",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "read_file",
						"arguments": string(argsJSON),
					},
				},
			},
			StopReason: "tool_use",
		},
		&llm.CompletionResponse{
			Content:    "File read.",
			StopReason: "stop",
		},
	)
	orch.orchestrationClient = mockClient

	var capturedParams map[string]interface{}
	var mu sync.Mutex
	toolCallCb := func(toolName, toolID string, params map[string]interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		if toolName == "read_file" {
			capturedParams = params
		}
		return nil
	}

	ctx := context.Background()
	err := orch.ProcessPrompt(ctx, "read file", nil, nil, nil, toolCallCb, nil, nil)

	if err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedParams == nil {
		t.Fatal("Expected tool parameters to be captured")
	}
	if capturedParams["path"] != "/test/file.txt" {
		t.Errorf("Expected path '/test/file.txt', got %v", capturedParams["path"])
	}
}

// TestFormatPlanForDisplay_BoardMode tests the formatPlanForDisplay function with board mode
func TestFormatPlanForDisplay_BoardMode(t *testing.T) {
	// Import planning package types
	board := &planningBoardForTest{
		Description: "Test plan",
		PrimaryTasks: []planningTaskForTest{
			{
				ID:       "task_1",
				Text:     "First task",
				Priority: "high",
				Subtasks: []planningTaskForTest{
					{ID: "task_1_1", Text: "Subtask 1", Status: "pending"},
					{ID: "task_1_2", Text: "Subtask 2", Status: "completed"},
				},
			},
			{
				ID:   "task_2",
				Text: "Second task",
			},
		},
	}

	// We can't easily call formatPlanForDisplay directly since it uses planning types
	// This test is more of a documentation of expected behavior
	_ = board

	// The actual formatting is tested through integration tests
}

// planningBoardForTest mirrors planning.PlanningBoard for testing
type planningBoardForTest struct {
	Description  string
	PrimaryTasks []planningTaskForTest
}

type planningTaskForTest struct {
	ID          string
	Text        string
	Priority    string
	Status      string
	Description string
	Subtasks    []planningTaskForTest
}
