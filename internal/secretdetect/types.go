package secretdetect

import (
	"regexp"
)

// Severity represents the severity level of a detected secret.
type Severity string

const (
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// SecretPattern defines a pattern to search for.
type SecretPattern struct {
	Name        string
	Regex       *regexp.Regexp
	Description string
	Severity    Severity
}

// SecretMatch represents a detected secret.
type SecretMatch struct {
	PatternName string
	MatchedText string
	LineNumber  int
	Column      int
	FilePath    string
	Confidence  float64 // 0.0 to 1.0
}

// Detector defines the interface for secret detection.
type Detector interface {
	Scan(content string) []SecretMatch
	ScanFile(path string) ([]SecretMatch, error)
	AddPattern(pattern SecretPattern)
}
