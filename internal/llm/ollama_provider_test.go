package llm

import (
	"context"
	"net/http"
	"testing"
)

func TestNormalizeOllamaBaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "http://localhost:11434"},
		{"localhost:11434", "http://localhost:11434"},
		{"http://localhost:11434/", "http://localhost:11434"},
		{"https://ollama.com/api", "https://ollama.com/api"},
	}

	for _, tc := range tests {
		if got := normalizeOllamaBaseURL(tc.input); got != tc.expected {
			t.Fatalf("normalizeOllamaBaseURL(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestOllamaProviderListModelsFromAPI(t *testing.T) {
	var gotMethod, gotPath string
	provider := NewOllamaProvider("http://ollama.test")
	provider.client = newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		return newTestHTTPResponse(req, http.StatusOK, "application/json", `{
			"models": [
				{
					"name": "llama3.1:8b",
					"details": {
						"family": "llama",
						"parameter_size": "8B",
						"format": "gguf",
						"quantization_level": "Q4_K_M",
						"context_length": 8192
					}
				}
			]
		}`), nil
	})

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	model := models[0]
	if model.ID != "llama3.1:8b" {
		t.Fatalf("expected model ID llama3.1:8b, got %s", model.ID)
	}
	if model.ContextWindow != 8192 {
		t.Fatalf("expected context window 8192, got %d", model.ContextWindow)
	}
	if !model.SupportsToolCalling {
		t.Fatalf("expected SupportsToolCalling to be true")
	}
	if !model.SupportsStreaming {
		t.Fatalf("expected SupportsStreaming to be true")
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("expected GET request, got %s", gotMethod)
	}
	if gotPath != "/api/tags" {
		t.Fatalf("expected path /api/tags, got %s", gotPath)
	}
}

func TestOllamaProviderFallbackOnError(t *testing.T) {
	provider := NewOllamaProvider("http://ollama.test")
	provider.client = newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return newTestHTTPResponse(req, http.StatusInternalServerError, "text/plain", "boom"), nil
	})

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) == 0 {
		t.Fatalf("expected fallback models, got none")
	}
	for _, model := range models {
		if model.Provider != "ollama" {
			t.Fatalf("expected provider ollama, got %s", model.Provider)
		}
	}
}
