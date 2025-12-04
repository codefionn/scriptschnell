package orchestrator

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/tools"
)

type mockClassifierLLM struct {
	content string
}

func (m *mockClassifierLLM) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: m.content}, nil
}

func (m *mockClassifierLLM) Complete(ctx context.Context, prompt string) (string, error) {
	return m.content, nil
}

func (m *mockClassifierLLM) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	if callback == nil {
		return nil
	}
	return callback(m.content)
}

func (m *mockClassifierLLM) GetModelName() string {
	return "mock-classifier"
}

func TestParseSimplicityResponse(t *testing.T) {
	simple, reason, ok := parseSimplicityResponse(`{"simple":true,"reason":"short task"}`)
	if !ok || !simple {
		t.Fatalf("expected simple prompt, got simple=%v ok=%v", simple, ok)
	}
	if reason != "short task" {
		t.Fatalf("unexpected reason: %s", reason)
	}

	simple, reason, ok = parseSimplicityResponse("complex: needs planning")
	if !ok || simple {
		t.Fatalf("expected complex classification, got simple=%v ok=%v", simple, ok)
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for complex classification")
	}
}

func TestClassifyPromptSimplicityUsesSummarizer(t *testing.T) {
	orch := &Orchestrator{
		summarizeClient: &mockClassifierLLM{content: `{"simple":false,"reason":"needs coordinated changes"}`},
	}

	simple, reason := orch.classifyPromptSimplicity(context.Background(), "please refactor multiple services")
	if simple {
		t.Fatal("expected complex classification from summarizer")
	}
	if reason != "needs coordinated changes" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestIsReadOnlyMCPTool(t *testing.T) {
	cfg := &config.Config{
		MCP: config.MCPConfig{
			Servers: map[string]*config.MCPServerConfig{
				"api": {Type: "openapi"},
				"cmd": {Type: "command", Metadata: map[string]string{"read_only": "true"}},
				"ai":  {Type: "openai", Metadata: map[string]string{"read_only": "false"}},
			},
		},
	}

	orch := &Orchestrator{config: cfg}

	getTool := tools.NewOpenAPITool(&tools.OpenAPIToolConfig{
		Name:        "mcp_api_get",
		Description: "GET example",
		Method:      "get",
		BaseURL:     "http://example.com",
		Path:        "/items",
	})
	postTool := tools.NewOpenAPITool(&tools.OpenAPIToolConfig{
		Name:        "mcp_api_post",
		Description: "POST example",
		Method:      "post",
		BaseURL:     "http://example.com",
		Path:        "/items",
	})
	cmdTool := tools.NewCommandTool(&tools.CommandToolConfig{
		Name:        "mcp_cmd_ls",
		Description: "ls",
		Command:     []string{"ls"},
	})
	openaiTool := tools.NewOpenAITool(&tools.OpenAIToolConfig{
		Name:        "mcp_ai",
		Description: "llm",
		Model:       "gpt-mock",
	})

	if !orch.isReadOnlyMCPTool("api", cfg.MCP.Servers["api"], getTool) {
		t.Fatal("expected GET OpenAPI tool to be treated as read-only")
	}

	if orch.isReadOnlyMCPTool("api", cfg.MCP.Servers["api"], postTool) {
		t.Fatal("expected POST OpenAPI tool to be excluded from planning")
	}

	if !orch.isReadOnlyMCPTool("cmd", cfg.MCP.Servers["cmd"], cmdTool) {
		t.Fatal("expected command tool to be allowed because of read_only metadata")
	}

	if orch.isReadOnlyMCPTool("ai", cfg.MCP.Servers["ai"], openaiTool) {
		t.Fatal("metadata override should disable openai MCP for planning")
	}
}
