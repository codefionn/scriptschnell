package orchestrator

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/provider"
)

func TestProcessPromptSendsInitialMessages(t *testing.T) {
	ctx := context.Background()

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

	mockClient := &captureRequestClient{}
	orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, fs.NewMockFS())
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	defer orch.Close()

	// Disable planning so we only send a single request
	orch.featureFlags.SetPlanningEnabled(false)

	orch.orchestrationClient = mockClient

	if err := orch.ProcessPrompt(ctx, "hello world", nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessPrompt failed: %v", err)
	}

	if len(mockClient.requests) == 0 {
		t.Fatalf("expected at least one completion request, got none")
	}

	req := mockClient.requests[0]
	if req.SystemPrompt == "" {
		t.Fatalf("expected system prompt to be sent, got empty string")
	}
	if len(req.Messages) == 0 {
		t.Fatalf("expected user message to be sent, got none")
	}

	first := req.Messages[0]
	if first.Role != "user" {
		t.Fatalf("expected first message role user, got %s", first.Role)
	}
	if first.Content != "hello world" {
		t.Fatalf("expected first message content to match prompt, got %q", first.Content)
	}
}

type captureRequestClient struct {
	requests []*llm.CompletionRequest
}

func (c *captureRequestClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	c.requests = append(c.requests, req)
	return &llm.CompletionResponse{
		Content:    "ok",
		StopReason: "stop",
	}, nil
}

func (c *captureRequestClient) Complete(ctx context.Context, prompt string) (string, error) {
	return "", nil
}

func (c *captureRequestClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	_, err := c.CompleteWithRequest(ctx, req)
	return err
}

func (c *captureRequestClient) GetModelName() string {
	return "test-model"
}
