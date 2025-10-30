package llm

import (
	"testing"
)

func TestGetCerebrasDisplayName(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"llama-4-scout-17b-16e-instruct", "Llama 4 Scout (17B, 16-Expert)"},
		{"llama3.1-8b", "Llama 3.1 (8B)"},
		{"llama-3.3-70b", "Llama 3.3 (70B)"},
		{"qwen-3-32b", "Qwen 3 (32B)"},
		{"gpt-oss-120b", "GPT-OSS (120B)"},
		{"unknown-model", "unknown-model"}, // Should return the ID itself
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := getCerebrasDisplayName(tt.modelID)
			if result != tt.expected {
				t.Errorf("getCerebrasDisplayName(%q) = %q, want %q", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestGetCerebrasContextWindow(t *testing.T) {
	tests := []struct {
		modelID  string
		expected int
	}{
		{"llama3.1-8b", 128000},
		{"llama-3.3-70b", 128000},
		{"qwen-3-32b", 128000},
		{"gpt-oss-120b", 128000},
		{"unknown-model", 128000}, // Should return default
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := getCerebrasContextWindow(tt.modelID)
			if result != tt.expected {
				t.Errorf("getCerebrasContextWindow(%q) = %d, want %d", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestGetCerebrasMaxOutputTokens(t *testing.T) {
	tests := []struct {
		modelID  string
		expected int
	}{
		{"llama3.1-8b", 8192},
		{"llama-3.3-70b", 65536},
		{"qwen-3-32b", 40960},
		{"gpt-oss-120b", 8192},
		{"unknown-model", 8192}, // Should return default
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := getCerebrasMaxOutputTokens(tt.modelID)
			if result != tt.expected {
				t.Errorf("getCerebrasMaxOutputTokens(%q) = %d, want %d", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestCerebrasSupportsToolCalling(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		{"llama3.1-8b", true},
		{"llama-3.3-70b", true},
		{"qwen-3-32b", true},
		{"gpt-oss-120b", true},
		{"unknown-model", true}, // Default to true
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := cerebrasSupportsToolCalling(tt.modelID)
			if result != tt.expected {
				t.Errorf("cerebrasSupportsToolCalling(%q) = %v, want %v", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestGetCerebrasModelInfo(t *testing.T) {
	modelID := "llama3.1-8b"
	info := GetCerebrasModelInfo(modelID)

	if info.ID != modelID {
		t.Errorf("GetCerebrasModelInfo(%q).ID = %q, want %q", modelID, info.ID, modelID)
	}

	if info.Provider != "cerebras" {
		t.Errorf("GetCerebrasModelInfo(%q).Provider = %q, want %q", modelID, info.Provider, "cerebras")
	}

	if info.Name == "" {
		t.Errorf("GetCerebrasModelInfo(%q).Name is empty", modelID)
	}

	if info.ContextWindow <= 0 {
		t.Errorf("GetCerebrasModelInfo(%q).ContextWindow = %d, want > 0", modelID, info.ContextWindow)
	}

	if info.MaxOutputTokens <= 0 {
		t.Errorf("GetCerebrasModelInfo(%q).MaxOutputTokens = %d, want > 0", modelID, info.MaxOutputTokens)
	}
}

func TestGetCerebrasDescription(t *testing.T) {
	tests := []struct {
		modelID  string
		contains string
	}{
		{"llama3.1-8b", "Llama 3.1"},
		{"llama-3.3-70b", "Llama 3.3"},
		{"qwen-3-32b", "Qwen 3"},
		{"gpt-oss-120b", "GPT-OSS"},
		{"unknown-model", "Cerebras Cloud AI model"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := getCerebrasDescription(tt.modelID)
			if result == "" {
				t.Errorf("getCerebrasDescription(%q) returned empty string", tt.modelID)
			}
		})
	}
}

func TestNewCerebrasProvider(t *testing.T) {
	apiKey := "test-api-key"
	provider := NewCerebrasProvider(apiKey)

	if provider == nil {
		t.Fatal("NewCerebrasProvider returned nil")
	}

	if provider.GetName() != "cerebras" {
		t.Errorf("provider.GetName() = %q, want %q", provider.GetName(), "cerebras")
	}

	if provider.apiKey != apiKey {
		t.Errorf("provider.apiKey = %q, want %q", provider.apiKey, apiKey)
	}

	if provider.client == nil {
		t.Error("provider.client is nil")
	}
}

func TestGetFallbackModels(t *testing.T) {
	provider := NewCerebrasProvider("test-api-key")
	models := provider.getFallbackModels()

	if len(models) == 0 {
		t.Fatal("getFallbackModels returned empty slice")
	}

	// Check that all models have required fields
	for _, model := range models {
		if model.ID == "" {
			t.Error("Model has empty ID")
		}
		if model.Provider != "cerebras" {
			t.Errorf("Model %q has provider %q, want %q", model.ID, model.Provider, "cerebras")
		}
		if model.Name == "" {
			t.Errorf("Model %q has empty Name", model.ID)
		}
		if model.ContextWindow <= 0 {
			t.Errorf("Model %q has invalid ContextWindow: %d", model.ID, model.ContextWindow)
		}
		if model.MaxOutputTokens <= 0 {
			t.Errorf("Model %q has invalid MaxOutputTokens: %d", model.ID, model.MaxOutputTokens)
		}
	}

	// Check for specific models
	expectedModels := []string{
		"llama-4-scout-17b-16e-instruct",
		"llama3.1-8b",
		"llama-3.3-70b",
		"qwen-3-32b",
		"gpt-oss-120b",
	}

	for _, expectedID := range expectedModels {
		found := false
		for _, model := range models {
			if model.ID == expectedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected model %q not found in fallback models", expectedID)
		}
	}
}
