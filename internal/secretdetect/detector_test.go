package secretdetect

import (
	"testing"
)

func TestDetector_Scan(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		name          string
		content       string
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "No secrets",
			content:       "This is a normal text with no secrets.",
			expectedCount: 0,
		},
		{
			name:          "OpenAI Key",
			content:       "My key is sk-abcdefghijklmnopqrstuvwxyz123456",
			expectedCount: 1,
			expectedNames: []string{"OpenAI API Key"},
		},
		{
			name:          "AWS Key",
			content:       "Access Key: AKIAIOSFODNN7EXAMPLE",
			expectedCount: 1,
			expectedNames: []string{"AWS Access Key ID"},
		},
		{
			name:          "Multiple secrets",
			content:       "sk-abcdefghijklmnopqrstuvwxyz123456\nAKIAIOSFODNN7EXAMPLE",
			expectedCount: 2,
			expectedNames: []string{"OpenAI API Key", "AWS Access Key ID"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := d.Scan(tt.content)
			if len(matches) != tt.expectedCount {
				t.Errorf("Scan() found %d matches, want %d", len(matches), tt.expectedCount)
			}

			if tt.expectedCount > 0 {
				foundNames := make(map[string]bool)
				for _, m := range matches {
					foundNames[m.PatternName] = true
				}

				for _, name := range tt.expectedNames {
					if !foundNames[name] {
						t.Errorf("Scan() missing expected pattern match: %s", name)
					}
				}
			}
		})
	}
}

func TestDetector_Redact(t *testing.T) {
	d := NewDetector()
	content := "Key: sk-abcdefghijklmnopqrstuvwxyz123456"
	matches := d.Scan(content)

	redacted := Redact(content, matches)
	expected := "Key: [REDACTED]"

	if redacted != expected {
		t.Errorf("Redact() = %q, want %q", redacted, expected)
	}
}

func TestEntropy(t *testing.T) {
	lowEntropy := "aaaaaaaaa"
	highEntropy := "7d8f9a2b1c" // Hex string, more entropy

	e1 := CalculateEntropy(lowEntropy)
	e2 := CalculateEntropy(highEntropy)

	if e1 >= e2 {
		t.Errorf("Expected higher entropy for random string. %f vs %f", e1, e2)
	}

	tokens := TokenizeAndCheckEntropy("secret=7d8f9a2b1c4e5f6a", 3.0)
	if len(tokens) == 0 {
		t.Errorf("Expected to find high entropy token")
	}
}
