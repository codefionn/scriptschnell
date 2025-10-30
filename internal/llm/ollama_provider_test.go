package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
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
		}`))
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL)
	provider.client = server.Client()

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
}

func TestOllamaProviderFallbackOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := NewOllamaProvider(server.URL)
	provider.client = server.Client()

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
