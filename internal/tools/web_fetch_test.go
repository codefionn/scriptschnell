package tools

import (
	"context"
	"io"
	"net/http"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newStubClient(status int, contentType, body string, onRequest func(*http.Request)) *http.Client {
	return &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if onRequest != nil {
				onRequest(req)
			}
			resp := &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}
			if contentType != "" {
				resp.Header.Set("Content-Type", contentType)
			}
			return resp, nil
		}),
	}
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
	url := "https://example.com/hello"
	var gotMethod, gotURL string
	client := newStubClient(http.StatusOK, "text/html", "<html><body><h1>Hello</h1></body></html>", func(req *http.Request) {
		gotMethod = req.Method
		gotURL = req.URL.String()
	})
	tool := NewWebFetchTool(client, nil, &mockAuthorizer{}, secretdetect.NewDetector(), &mockFeatureFlags{})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"url": url,
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

	if gotMethod != http.MethodGet {
		t.Fatalf("expected GET request, got %s", gotMethod)
	}
	if gotURL != url {
		t.Fatalf("expected URL %s, got %s", url, gotURL)
	}
}

func TestWebFetchToolSummarizesWhenPromptProvided(t *testing.T) {
	mockLLM := &mockLLMClient{response: "short summary"}
	client := newStubClient(http.StatusOK, "text/html", "<html><body><p>Example content</p></body></html>", nil)
	tool := NewWebFetchTool(client, mockLLM, &mockAuthorizer{}, secretdetect.NewDetector(), &mockFeatureFlags{})

	result := tool.Execute(context.Background(), map[string]interface{}{
		"url":              "https://example.com/summary",
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

func (m *mockLLMClient) GetLastResponseID() string {
	return ""
}

func (m *mockLLMClient) SetPreviousResponseID(responseID string) {
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
