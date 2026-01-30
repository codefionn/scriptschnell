package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestOpenAICompatibleClient_UsageData(t *testing.T) {
	response := openAIChatResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Model:   "gpt-3.5-turbo",
		Created: 1234567890,
		Choices: []openAIChatChoice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: &openAIChatMessage{
					Role:    "assistant",
					Content: "Hello, world!",
				},
			},
		},
		Usage: map[string]interface{}{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
	var gotMethod, gotPath, gotAuth string
	client := &OpenAICompatibleClient{
		apiKey:  "test-key",
		model:   "gpt-3.5-turbo",
		baseURL: "http://openai.test",
		httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			gotAuth = req.Header.Get("Authorization")

			payload, err := json.Marshal(response)
			if err != nil {
				return newTestHTTPResponse(req, http.StatusInternalServerError, "text/plain", err.Error()), nil
			}
			return newTestHTTPResponse(req, http.StatusOK, "application/json", string(payload)), nil
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

	// Verify usage data
	if resp.Usage == nil {
		t.Fatal("Expected usage data, got nil")
	}

	promptTokens, ok := resp.Usage["prompt_tokens"].(float64)
	if !ok || promptTokens != 10 {
		t.Errorf("Expected prompt_tokens 10, got %v (type: %T)", resp.Usage["prompt_tokens"], resp.Usage["prompt_tokens"])
	}

	completionTokens, ok := resp.Usage["completion_tokens"].(float64)
	if !ok || completionTokens != 5 {
		t.Errorf("Expected completion_tokens 5, got %v (type: %T)", resp.Usage["completion_tokens"], resp.Usage["completion_tokens"])
	}

	totalTokens, ok := resp.Usage["total_tokens"].(float64)
	if !ok || totalTokens != 15 {
		t.Errorf("Expected total_tokens 15, got %v (type: %T)", resp.Usage["total_tokens"], resp.Usage["total_tokens"])
	}
}

func TestOpenAICompatibleClient_UsageData_NoUsage(t *testing.T) {
	response := openAIChatResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Model:   "gpt-3.5-turbo",
		Created: 1234567890,
		Choices: []openAIChatChoice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: &openAIChatMessage{
					Role:    "assistant",
					Content: "Hello, world!",
				},
			},
		},
		// No Usage field
	}
	var gotMethod, gotPath, gotAuth string
	client := &OpenAICompatibleClient{
		apiKey:  "test-key",
		model:   "gpt-3.5-turbo",
		baseURL: "http://openai.test",
		httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			gotAuth = req.Header.Get("Authorization")

			payload, err := json.Marshal(response)
			if err != nil {
				return newTestHTTPResponse(req, http.StatusInternalServerError, "text/plain", err.Error()), nil
			}
			return newTestHTTPResponse(req, http.StatusOK, "application/json", string(payload)), nil
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

	// Verify usage data is nil when not provided
	if resp.Usage != nil {
		t.Errorf("Expected usage data to be nil, got %v", resp.Usage)
	}
}
