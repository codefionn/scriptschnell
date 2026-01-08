package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
)

// Mock feature flags provider for testing
type mockFeatureFlags struct{}

func (m *mockFeatureFlags) IsToolEnabled(toolName string) bool {
	// Enable all features for testing
	return true
}

func TestWebFetchToolSpec(t *testing.T) {
	spec := &WebFetchToolSpec{}
	if spec.Name() != ToolNameWebFetch {
		t.Fatalf("expected name %s, got %s", ToolNameWebFetch, spec.Name())
	}
	if spec.Description() == "" {
		t.Fatalf("description should not be empty")
	}
	params := spec.Parameters()
	if _, ok := params["properties"]; !ok {
		t.Fatalf("expected parameters to define properties")
	}
}

func TestWebFetchToolFetchesHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET request, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h1>Hello</h1></body></html>")
	}))
	defer server.Close()

	tool := NewWebFetchTool(server.Client(), nil, &mockAuthorizer{}, secretdetect.NewDetector(), &mockFeatureFlags{})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"url": server.URL,
	})

	if result == nil || result.Error != "" {
		t.Fatalf("expected success, got error: %v", result)
	}

	resMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result map, got %T", result.Result)
	}

	if resMap["status_code"] != http.StatusOK {
		t.Fatalf("expected status 200, got %v", resMap["status_code"])
	}

	body, _ := resMap["body"].(string)
	if body == "" || !strings.Contains(body, "<h1>Hello</h1>") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestWebFetchToolSummarizesWhenPromptProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><p>Example content</p></body></html>")
	}))
	defer server.Close()

	mockLLM := &mockLLMClient{response: "short summary"}
	tool := NewWebFetchTool(server.Client(), mockLLM, &mockAuthorizer{}, secretdetect.NewDetector(), &mockFeatureFlags{})

	result := tool.Execute(context.Background(), map[string]interface{}{
		"url":              server.URL,
		"summarize_prompt": "summarize this page",
	})

	if result == nil || result.Error != "" {
		t.Fatalf("expected success, got error: %v", result)
	}

	resMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result map, got %T", result.Result)
	}

	summary, _ := resMap["summary"].(string)
	if summary != mockLLM.response {
		t.Fatalf("expected summary %q, got %q", mockLLM.response, summary)
	}
}

type mockLLMClient struct {
	response string
}

func (m *mockLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	return m.response, nil
}

func (m *mockLLMClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: m.response}, nil
}

func (m *mockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	if err := callback(m.response); err != nil {
		return err
	}
	return nil
}

func (m *mockLLMClient) GetModelName() string {
	return "mock-llm"
}
