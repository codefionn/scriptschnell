package llm

import (
	"testing"
)

func TestFormatModelDisplayName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"llama-3.3-70b-instruct", "Llama 3.3 70b Instruct"},
		{"mistral-7b-instruct-v0.1", "Mistral 7b Instruct V0.1"},
		{"qwen2.5-7b-instruct", "Qwen2.5 7b Instruct"},
		{"deepseek-coder-33b-instruct", "Deepseek Coder 33b Instruct"},
		{"phi-3-mini-4k-instruct", "Phi 3 Mini 4k Instruct"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			family := DetectModelFamily(tt.input)
			result := FormatModelDisplayName(tt.input, family)
			if result != tt.expected {
				t.Errorf("FormatModelDisplayName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetModelDescription(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"llama-3.3-70b-instruct"},
		{"llama-3.2-1b"},
		{"llama-3.1-8b"},
		{"mistral-large-latest"},
		{"mistral-medium-latest"},
		{"mixtral-8x7b-instruct"},
		{"qwen2.5-7b"},
		{"gemma-2-9b"},
		{"phi-3-mini"},
		{"deepseek-coder"},
		{"unknown-model"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			family := DetectModelFamily(tt.input)
			result := GetModelDescription(tt.input, family)
			if result == "" {
				t.Errorf("GetModelDescription(%q) returned empty string", tt.input)
			}
		})
	}
}

func TestDetectContextWindow(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"llama-3.3-70b-instruct", 131072},
		{"llama-3.2-1b", 131072},
		{"llama-3.1-8b-128k", 131072},
		{"llama-3-8b", 8192},
		{"mistral-large-latest", 128000},
		{"mistral-small-latest", 32000},
		{"mixtral-8x22b-instruct", 32000},
		{"mixtral-8x7b-instruct", 32000},
		{"qwen2.5-7b", 8192},
		{"deepseek-coder-33b", 64000},
		{"model-with-32k-context", 32768},
		{"model-with-16k-context", 16384},
		{"unknown-model", 8192}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			family := DetectModelFamily(tt.input)
			result := DetectContextWindow(tt.input, family)
			if result != tt.expected {
				t.Errorf("DetectContextWindow(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDetectMaxOutputTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"deepseek-coder-33b", 8192},
		{"qwen-2.5-7b", 8192},
		{"mistral-large-latest", 8192},
		{"llama-3.3-70b", 8192},
		{"llama-3.2-1b", 8192},
		{"llama-3.1-8b", 8192},
		{"unknown-model", 2048}, // Default fallback
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			family := DetectModelFamily(tt.input)
			contextWindow := DetectContextWindow(tt.input, family)
			result := DetectMaxOutputTokens(tt.input, family, contextWindow)
			if result != tt.expected {
				t.Errorf("DetectMaxOutputTokens(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewOpenAICompatibleProvider(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		baseURL string
	}{
		{
			name:    "With API key",
			apiKey:  "test-key",
			baseURL: "http://localhost:1234/v1",
		},
		{
			name:    "Without API key (local server)",
			apiKey:  "",
			baseURL: "http://localhost:8080/v1",
		},
		{
			name:    "Trailing slash should be removed",
			apiKey:  "test-key",
			baseURL: "http://localhost:1234/v1/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewOpenAICompatibleProvider(tt.apiKey, tt.baseURL)
			if provider == nil {
				t.Fatalf("NewOpenAICompatibleProvider returned nil")
			}
			if provider.apiKey != tt.apiKey {
				t.Errorf("Expected apiKey %q, got %q", tt.apiKey, provider.apiKey)
			}
			// baseURL should not have trailing slash
			expectedURL := tt.baseURL
			if expectedURL[len(expectedURL)-1] == '/' {
				expectedURL = expectedURL[:len(expectedURL)-1]
			}
			if provider.baseURL != expectedURL {
				t.Errorf("Expected baseURL %q, got %q", expectedURL, provider.baseURL)
			}
			if provider.GetName() != "openai-compatible" {
				t.Errorf("Expected provider name 'openai-compatible', got %q", provider.GetName())
			}
		})
	}
}
