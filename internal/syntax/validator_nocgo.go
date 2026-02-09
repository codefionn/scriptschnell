//go:build !cgo

package syntax

import (
	"context"
	"fmt"
)

// Validator provides syntax validation for code using tree-sitter parsers.
type Validator struct{}

// SyntaxError represents a single syntax error found during validation.
type SyntaxError struct {
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Message   string `json:"message"`
	ErrorNode string `json:"error_node"`
}

// ValidationResult contains the results of syntax validation.
type ValidationResult struct {
	Valid       bool          `json:"valid"`
	Errors      []SyntaxError `json:"errors,omitempty"`
	Language    string        `json:"language"`
	ParsedBytes int           `json:"parsed_bytes"`
}

// NewValidator creates a new syntax validator (no-op without CGo).
func NewValidator() *Validator {
	return &Validator{}
}

// Validate always returns valid without CGo (tree-sitter unavailable).
func (v *Validator) Validate(code string, language string) (*ValidationResult, error) {
	return &ValidationResult{
		Valid:       true,
		Language:    language,
		ParsedBytes: len([]byte(code)),
	}, nil
}

// ValidateFile always returns valid without CGo (tree-sitter unavailable).
func (v *Validator) ValidateFile(ctx context.Context, readFile func(context.Context, string) ([]byte, error), path string) (*ValidationResult, error) {
	language := DetectLanguage(path)
	if language == "" {
		return nil, fmt.Errorf("cannot detect language from file: %s", path)
	}
	data, err := readFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return &ValidationResult{
		Valid:       true,
		Language:    language,
		ParsedBytes: len(data),
	}, nil
}

// SupportsLanguage always returns false without CGo.
func (v *Validator) SupportsLanguage(language string) bool {
	return false
}
