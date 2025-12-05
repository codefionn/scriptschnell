package llm

import (
	"strings"
	"testing"
)

func TestDetectModelFamily_Claude45(t *testing.T) {
	tests := []struct {
		modelID  string
		expected ModelFamily
	}{
		{"claude-4-5-sonnet", FamilyClaude45},
		{"claude-4.5-sonnet", FamilyClaude45},
		{"claude-4.5-haiku", FamilyClaude45},
		{"claude-4-5-haiku", FamilyClaude45},
		{"claude-4.5-opus", FamilyClaude45},
		{"claude-4.5", FamilyClaude45},
		// New patterns
		{"claude-haiku-4-5-20251001", FamilyClaude45},
		{"claude-sonnet-4-5-20251001", FamilyClaude45},
		{"claude-opus-4-5-20251001", FamilyClaude45},
		{"claude-opus-4-5-20251101", FamilyClaude45},
		{"claude-sonnet-4.5", FamilyClaude45},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := DetectModelFamily(tt.modelID)
			if got != tt.expected {
				t.Errorf("DetectModelFamily(%q) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestDetectModelFamily_Claude41(t *testing.T) {
	tests := []struct {
		modelID  string
		expected ModelFamily
	}{
		{"claude-4.1-opus", FamilyClaude41},
		{"claude-4.1", FamilyClaude41},
		{"claude-4-1", FamilyClaude41},
		// New pattern assumption
		{"claude-opus-4-1-20250101", FamilyClaude41},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := DetectModelFamily(tt.modelID)
			if got != tt.expected {
				t.Errorf("DetectModelFamily(%q) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestDetectContextWindow_Claude(t *testing.T) {
	tests := []struct {
		modelID  string
		family   ModelFamily
		expected int
	}{
		{"claude-4.5-sonnet", FamilyClaude45, 1000000},
		{"claude-4.5-haiku", FamilyClaude45, 200000},
		{"claude-4.5-opus", FamilyClaude45, 200000},
		{"claude-4.1-opus", FamilyClaude41, 200000},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := DetectContextWindow(tt.modelID, tt.family)
			if got != tt.expected {
				t.Errorf("DetectContextWindow(%q) = %d, want %d", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestDetectMaxOutputTokens_Claude(t *testing.T) {
	tests := []struct {
		modelID  string
		family   ModelFamily
		expected int
	}{
		{"claude-4.5-sonnet", FamilyClaude45, 64000},
		{"claude-4.5-haiku", FamilyClaude45, 64000},
		{"claude-4.5-opus", FamilyClaude45, 64000},
		{"claude-4.1-opus", FamilyClaude41, 32000},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := DetectMaxOutputTokens(tt.modelID, tt.family, 200000)
			if got != tt.expected {
				t.Errorf("DetectMaxOutputTokens(%q) = %d, want %d", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestFormatModelDisplayName_Claude(t *testing.T) {
	tests := []struct {
		modelID  string
		family   ModelFamily
		expected string
	}{
		{"claude-4.5-sonnet", FamilyClaude45, "Claude 4.5 Sonnet"},
		{"claude-4.5-haiku", FamilyClaude45, "Claude 4.5 Haiku"},
		{"claude-4.5-opus", FamilyClaude45, "Claude 4.5 Opus"},
		{"claude-4.1-opus", FamilyClaude41, "Claude 4.1 Opus"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := FormatModelDisplayName(tt.modelID, tt.family)
			if got != tt.expected {
				t.Errorf("FormatModelDisplayName(%q) = %q, want %q", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestGetModelDescription_Claude(t *testing.T) {
	tests := []struct {
		modelID  string
		family   ModelFamily
		contains string
	}{
		{"claude-4.5-sonnet", FamilyClaude45, "most intelligent model"},
		{"claude-4.5-haiku", FamilyClaude45, "fastest model"},
		{"claude-4.5-opus", FamilyClaude45, "Premium model"},
		{"claude-4.1-opus", FamilyClaude41, "specialized reasoning"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := GetModelDescription(tt.modelID, tt.family)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("GetModelDescription(%q) = %q, want to contain %q", tt.modelID, got, tt.contains)
			}
		})
	}
}

func TestIsMistralModel(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		// Mistral models
		{"mistral-large-latest", true},
		{"mistral-medium-latest", true},
		{"mistral-small-latest", true},
		{"codestral-latest", true},
		{"pixtral-latest", true},
		{"mistral/Mistral-Large", true},
		{"open-mistral-7b", true},
		{"open-mistral-nemo", true},

		// Non-Mistral models
		{"gpt-4o", false},
		{"claude-3-5-sonnet", false},
		{"gemini-pro", false},
		{"llama-3-70b", false},
		{"", false},
		{"unknown-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := IsMistralModel(tt.modelID)
			if result != tt.expected {
				t.Errorf("IsMistralModel(%q) = %v, want %v", tt.modelID, result, tt.expected)
			}
		})
	}
}
