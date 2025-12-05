package secretdetect

import (
	"testing"
)

func TestPatterns(t *testing.T) {
	patterns := GetDefaultPatterns()
	patternMap := make(map[string]SecretPattern)
	for _, p := range patterns {
		patternMap[p.Name] = p
	}

	tests := []struct {
		patternName string
		valid       []string
		invalid     []string
	}{
		{
			patternName: "AWS Access Key ID",
			valid:       []string{"AKIAIOSFODNN7EXAMPLE", "ASIAIOSFODNN7EXAMPLE"},
			invalid:     []string{"AKIA123", "XXXXIOSFODNN7EXAMPLE"},
		},
		{
			patternName: "OpenAI API Key",
			valid:       []string{"sk-1234567890abcdef1234567890abcdef"},
			invalid:     []string{"sk-123"},
		},
		{
			patternName: "GitHub PAT",
			valid:       []string{"ghp_1234567890abcdef1234567890abcdef36ch"},
			invalid:     []string{"ghp_short"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.patternName, func(t *testing.T) {
			p, ok := patternMap[tt.patternName]
			if !ok {
				t.Fatalf("Pattern %s not found", tt.patternName)
			}

			for _, s := range tt.valid {
				if !p.Regex.MatchString(s) {
					t.Errorf("%s should match %q", tt.patternName, s)
				}
			}

			for _, s := range tt.invalid {
				if p.Regex.MatchString(s) {
					t.Errorf("%s should NOT match %q", tt.patternName, s)
				}
			}
		})
	}
}
