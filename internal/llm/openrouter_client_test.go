package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIsOpenRouterNoToolUseError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "exact OpenRouter error",
			err:      fmt.Errorf(`openrouter completion failed: status 404: {"error":{"message":"No endpoints found that support tool use with provider openai for model o3-pro","code":404}}`),
			expected: true,
		},
		{
			name:     "case insensitive match",
			err:      errors.New("NO ENDPOINTS FOUND that support TOOL USE"),
			expected: true,
		},
		{
			name:     "unrelated 404 error",
			err:      fmt.Errorf("openrouter completion failed: status 404: not found"),
			expected: false,
		},
		{
			name:     "only no endpoints found without tool use",
			err:      errors.New("No endpoints found for this model"),
			expected: false,
		},
		{
			name:     "only tool use without no endpoints found",
			err:      errors.New("This model does not support tool use"),
			expected: false,
		},
		{
			name:     "authentication error",
			err:      fmt.Errorf("openrouter completion failed: status 401: unauthorized"),
			expected: false,
		},
		{
			name:     "rate limit error",
			err:      fmt.Errorf("openrouter completion failed: status 429: rate limited"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOpenRouterNoToolUseError(tt.err)
			if result != tt.expected {
				t.Errorf("isOpenRouterNoToolUseError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestCompleteWithRequest_AutoHealNoToolUse(t *testing.T) {
	var callCount atomic.Int32

	successResponse := openRouterChatResponse{
		ID:    "resp-123",
		Model: "test/model",
		Choices: []openRouterChatChoice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: &openRouterChatResponseMessage{
					Role:    "assistant",
					Content: "Hello without tools",
				},
			},
		},
	}

	client := &OpenRouterClient{
		apiKey:  "test-key",
		model:   "test/no-tool-model",
		baseURL: "http://openrouter.test",
		httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			count := callCount.Add(1)

			// First call: return 404 tool use error
			if count == 1 {
				errorBody := `{"error":{"message":"No endpoints found that support tool use with provider test for model no-tool-model","code":404}}`
				return newTestHTTPResponse(req, http.StatusNotFound, "application/json", errorBody), nil
			}

			// Second call: verify tools are stripped and return success
			var payload openRouterChatRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return newTestHTTPResponse(req, http.StatusInternalServerError, "text/plain", err.Error()), nil
			}

			if len(payload.Tools) > 0 {
				return newTestHTTPResponse(req, http.StatusInternalServerError, "text/plain", "tools should have been stripped on retry"), nil
			}

			body, _ := json.Marshal(successResponse)
			return newTestHTTPResponse(req, http.StatusOK, "application/json", string(body)), nil
		}),
	}

	req := &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 1.0,
		Tools: []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "read_file",
					"description": "Read a file",
				},
			},
		},
	}

	resp, err := client.CompleteWithRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteWithRequest failed: %v", err)
	}

	if resp.Content != "Hello without tools" {
		t.Errorf("Expected content 'Hello without tools', got '%s'", resp.Content)
	}

	if callCount.Load() != 2 {
		t.Errorf("Expected 2 HTTP calls (original + retry), got %d", callCount.Load())
	}
}

func TestCompleteWithRequest_NoRetryWhenNoTools(t *testing.T) {
	var callCount atomic.Int32

	client := &OpenRouterClient{
		apiKey:  "test-key",
		model:   "test/no-tool-model",
		baseURL: "http://openrouter.test",
		httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			callCount.Add(1)
			errorBody := `{"error":{"message":"No endpoints found that support tool use","code":404}}`
			return newTestHTTPResponse(req, http.StatusNotFound, "application/json", errorBody), nil
		}),
	}

	req := &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 1.0,
		// No tools
	}

	_, err := client.CompleteWithRequest(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error when no tools and tool-use error")
	}

	if callCount.Load() != 1 {
		t.Errorf("Expected 1 HTTP call (no retry when no tools), got %d", callCount.Load())
	}
}

func TestStream_AutoHealNoToolUse(t *testing.T) {
	var callCount atomic.Int32

	client := &OpenRouterClient{
		apiKey:  "test-key",
		model:   "test/no-tool-model",
		baseURL: "http://openrouter.test",
		httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			count := callCount.Add(1)

			// First call: return 404 tool use error
			if count == 1 {
				errorBody := `{"error":{"message":"No endpoints found that support tool use","code":404}}`
				return newTestHTTPResponse(req, http.StatusNotFound, "application/json", errorBody), nil
			}

			// Second call: verify tools are stripped and return SSE stream
			var payload openRouterChatRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return newTestHTTPResponse(req, http.StatusInternalServerError, "text/plain", err.Error()), nil
			}

			if len(payload.Tools) > 0 {
				return newTestHTTPResponse(req, http.StatusInternalServerError, "text/plain", "tools should have been stripped on retry"), nil
			}

			chunk := openRouterStreamChunk{
				ID: "resp-stream-123",
				Choices: []openRouterStreamChoice{
					{
						Index: 0,
						Delta: &openRouterStreamDelta{
							Content: "streamed response",
						},
					},
				},
			}
			chunkJSON, _ := json.Marshal(chunk)
			sseBody := fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", string(chunkJSON))
			return newTestHTTPResponse(req, http.StatusOK, "text/event-stream", sseBody), nil
		}),
	}

	req := &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 1.0,
		Tools: []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name": "read_file",
				},
			},
		},
	}

	var chunks []string
	err := client.Stream(context.Background(), req, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	if callCount.Load() != 2 {
		t.Errorf("Expected 2 HTTP calls (original + retry), got %d", callCount.Load())
	}

	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk from stream")
	}

	combined := strings.Join(chunks, "")
	if combined != "streamed response" {
		t.Errorf("Expected 'streamed response', got '%s'", combined)
	}
}

func TestCompleteWithRequest_OtherErrorsNotRetried(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"message":"Invalid API key","code":401}}`,
		},
		{
			name:       "500 internal server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"message":"Internal server error","code":500}}`,
		},
		{
			name:       "404 but not tool use error",
			statusCode: http.StatusNotFound,
			body:       `{"error":{"message":"Model not found","code":404}}`,
		},
		{
			name:       "429 rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"message":"Rate limited","code":429}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var callCount atomic.Int32

			client := &OpenRouterClient{
				apiKey:  "test-key",
				model:   "test/model",
				baseURL: "http://openrouter.test",
				httpClient: newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
					callCount.Add(1)
					return newTestHTTPResponse(req, tt.statusCode, "application/json", tt.body), nil
				}),
			}

			req := &CompletionRequest{
				Messages: []*Message{
					{Role: "user", Content: "Hello"},
				},
				Temperature: 1.0,
				Tools: []map[string]interface{}{
					{
						"type": "function",
						"function": map[string]interface{}{
							"name": "read_file",
						},
					},
				},
			}

			_, err := client.CompleteWithRequest(context.Background(), req)
			if err == nil {
				t.Fatal("Expected error")
			}

			if callCount.Load() != 1 {
				t.Errorf("Expected exactly 1 HTTP call (no retry for %s), got %d", tt.name, callCount.Load())
			}
		})
	}
}
