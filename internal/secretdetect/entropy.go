package secretdetect

import (
	"math"
	"strings"
)

// CalculateEntropy calculates the Shannon entropy of a string.
func CalculateEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	
	counts := make(map[rune]int)
	for _, r := range s {
		counts[r]++
	}
	
	length := float64(len(s))
	var entropy float64
	
	for _, count := range counts {
		freq := float64(count) / length
		entropy -= freq * math.Log2(freq)
	}
	
	return entropy
}

// TokenizeAndCheckEntropy splits a line into tokens and returns those exceeding the threshold.
// This is a simple heuristic approach.
func TokenizeAndCheckEntropy(line string, threshold float64) []string {
	// Split by common delimiters
	f := func(c rune) bool {
		return c == ' ' || c == '"' || c == '\'' || c == '=' || c == ':' || c == ',' || c == ';' || c == '<' || c == '>' || c == '(' || c == ')' || c == '[' || c == ']' || c == '{' || c == '}'
	}
	
	tokens := strings.FieldsFunc(line, f)
	var highEntropyTokens []string
	
	for _, token := range tokens {
		// Ignore short tokens to avoid false positives
		if len(token) < 8 {
			continue
		}
		
		ent := CalculateEntropy(token)
		if ent > threshold {
			highEntropyTokens = append(highEntropyTokens, token)
		}
	}
	
	return highEntropyTokens
}

// DefaultEntropyThreshold is a reasonable default for base64-like secrets.
const DefaultEntropyThreshold = 4.5
