package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/session"
)

type captureAuthorizationLLMClient struct {
	lastReq  *llm.CompletionRequest
	response string
}

func (c *captureAuthorizationLLMClient) CompleteWithRequest(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	c.lastReq = req
	return &llm.CompletionResponse{Content: c.response}, nil
}

func (c *captureAuthorizationLLMClient) Complete(_ context.Context, _ string) (string, error) {
	return c.response, nil
}

func (c *captureAuthorizationLLMClient) Stream(_ context.Context, _ *llm.CompletionRequest, _ func(chunk string) error) error {
	return nil
}

func (c *captureAuthorizationLLMClient) GetModelName() string { return "test" }

func (c *captureAuthorizationLLMClient) GetLastResponseID() string { return "" }

func (c *captureAuthorizationLLMClient) SetPreviousResponseID(string) {}

func TestJudgeDomainWithLLM_IncludesSessionWorkingDirectoryInPrompt(t *testing.T) {
	ctx := context.Background()
	workingDir := "/workspace/demo"
	client := &captureAuthorizationLLMClient{response: `{"safe": true, "reason": "ok", "prefix": ""}`}
	sess := session.NewSession("test", workingDir)
	actor := NewAuthorizationActor("auth", fs.NewMockFS(), sess, client, nil)

	decision, err := actor.judgeDomainWithLLM(ctx, "example.com")
	if err != nil {
		t.Fatalf("judgeDomainWithLLM returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected safe domain decision, got %#v", decision)
	}
	if client.lastReq == nil {
		t.Fatal("expected completion request to be captured")
	}
	if !strings.Contains(client.lastReq.SystemPrompt, "Session working directory: "+workingDir) {
		t.Fatalf("expected prompt to include session working directory, got: %q", client.lastReq.SystemPrompt)
	}
}

func TestJudgeShellCommandWithLLM_IncludesSessionWorkingDirectoryInPrompt(t *testing.T) {
	ctx := context.Background()
	workingDir := "/workspace/demo"
	client := &captureAuthorizationLLMClient{response: `{"harmless": true, "reason": "ok", "prefix": "git status"}`}
	sess := session.NewSession("test", workingDir)
	actor := NewAuthorizationActor("auth", fs.NewMockFS(), sess, client, nil)

	decision, err := actor.judgeShellCommandWithLLM(ctx, "git status")
	if err != nil {
		t.Fatalf("judgeShellCommandWithLLM returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected harmless shell decision, got %#v", decision)
	}
	if client.lastReq == nil {
		t.Fatal("expected completion request to be captured")
	}
	if !strings.Contains(client.lastReq.SystemPrompt, "Session working directory: "+workingDir) {
		t.Fatalf("expected prompt to include session working directory, got: %q", client.lastReq.SystemPrompt)
	}
}
