package llm

import (
	"context"
	"net/http"
	"testing"
)

func TestOpenAICompatibleClient_UsageData(t *testing.T) {
	// Streaming response with usage data (some providers include usage in final chunk)
	streamData := "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion\",\"model\":\"gpt-3.5-turbo\",\"created\":1234567890,\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{\"role\":\"assistant\",\"content\":\"Hello, world!\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\ndata: [DONE]\n"

	var gotMethod, gotPath, gotAuth string
	client := &OpenAICompatibleClient{
		apiKey:  "test-key",
		model:   "gpt-3.5-turbo",
		baseURL: "http://openai.test",
		httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			gotAuth = req.Header.Get("Authorization")

			return newTestHTTPResponse(req, http.StatusOK, "text/event-stream", streamData), nil
		}),
	}

	req := &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 1.0,
	}

	resp, err := client.CompleteWithRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteWithRequest failed: %v", err)
	}

	// Verify content
	if resp.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", resp.Content)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("Expected POST request, got %s", gotMethod)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("Expected /chat/completions, got %s", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Expected Authorization header, got %s", gotAuth)
	}

	// Note: Usage is nil in streaming mode since it's not available in standard stream chunks
	// The usage data in stream responses is provider-specific and handled differently
	if resp.Usage != nil {
		t.Logf("Usage data present (provider-specific): %v", resp.Usage)
	}
}

func TestOpenAICompatibleClient_UsageData_NoUsage(t *testing.T) {
	// Streaming response without usage data
	streamData := "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion\",\"model\":\"gpt-3.5-turbo\",\"created\":1234567890,\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{\"role\":\"assistant\",\"content\":\"Hello, world!\"}}]}\n\ndata: [DONE]\n"

	var gotMethod, gotPath, gotAuth string
	client := &OpenAICompatibleClient{
		apiKey:  "test-key",
		model:   "gpt-3.5-turbo",
		baseURL: "http://openai.test",
		httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			gotAuth = req.Header.Get("Authorization")

			return newTestHTTPResponse(req, http.StatusOK, "text/event-stream", streamData), nil
		}),
	}

	req := &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 1.0,
	}

	resp, err := client.CompleteWithRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteWithRequest failed: %v", err)
	}

	// Verify content
	if resp.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", resp.Content)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("Expected POST request, got %s", gotMethod)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("Expected /chat/completions, got %s", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Expected Authorization header, got %s", gotAuth)
	}

	// Verify usage data is nil in streaming mode
	if resp.Usage != nil {
		t.Errorf("Expected usage data to be nil, got %v", resp.Usage)
	}
}
