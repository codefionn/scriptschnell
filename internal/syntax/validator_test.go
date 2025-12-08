package syntax

import (
	"testing"
)

func TestValidator_ValidCode(t *testing.T) {
	validator := NewValidator()

	testCases := []struct {
		name     string
		language string
		code     string
	}{
		{
			name:     "Valid Go code",
			language: "go",
			code: `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`,
		},
		{
			name:     "Valid Python code",
			language: "python",
			code: `def hello():
    print("Hello, World!")

if __name__ == "__main__":
    hello()`,
		},
		{
			name:     "Valid TypeScript code",
			language: "typescript",
			code: `function hello(): void {
    console.log("Hello, World!");
}

hello();`,
		},
		{
			name:     "Valid JavaScript code",
			language: "javascript",
			code: `function hello() {
    console.log("Hello, World!");
}

hello();`,
		},
		{
			name:     "Valid Bash code",
			language: "bash",
			code: `#!/bin/bash

echo "Hello, World!"

for i in 1 2 3; do
    echo "Number: $i"
done`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.Validate(tc.code, tc.language)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !result.Valid {
				t.Errorf("Expected valid code, but got %d errors: %+v", len(result.Errors), result.Errors)
			}

			if result.Language != tc.language {
				t.Errorf("Expected language %s, got %s", tc.language, result.Language)
			}

			if result.ParsedBytes <= 0 {
				t.Errorf("Expected parsed bytes > 0, got %d", result.ParsedBytes)
			}
		})
	}
}

func TestValidator_InvalidSyntax(t *testing.T) {
	validator := NewValidator()

	testCases := []struct {
		name     string
		language string
		code     string
		wantErr  bool // Whether we expect errors to be detected
	}{
		{
			name:     "Go missing closing brace",
			language: "go",
			code: `package main

func main() {
    fmt.Println("Hello")
// Missing closing brace`,
			wantErr: true,
		},
		{
			name:     "Python invalid indentation",
			language: "python",
			code: `def hello():
print("Invalid")  # Should be indented`,
			wantErr: true,
		},
		{
			name:     "TypeScript missing semicolon in strict",
			language: "typescript",
			code: `function test() {
    let x = 5
    return x
}  // Tree-sitter may or may not flag missing semicolons`,
			wantErr: false, // Semicolons are optional in TS/JS
		},
		{
			name:     "JavaScript unexpected token",
			language: "javascript",
			code: `function test() {
    let x = ;
}`,
			wantErr: true,
		},
		{
			name:     "Bash unclosed quote",
			language: "bash",
			code: `echo "Hello
# Missing closing quote`,
			wantErr: true,
		},
		{
			name:     "Go unclosed parenthesis",
			language: "go",
			code: `package main

func main() {
    fmt.Println("test"
}`,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.Validate(tc.code, tc.language)
			if err != nil {
				t.Fatalf("Unexpected validation error: %v", err)
			}

			if tc.wantErr && result.Valid {
				t.Errorf("Expected syntax errors to be detected, but code was marked as valid")
			}

			if tc.wantErr && len(result.Errors) == 0 {
				t.Errorf("Expected syntax errors, but got none")
			}

			if tc.wantErr {
				t.Logf("Detected %d error(s):", len(result.Errors))
				for i, err := range result.Errors {
					t.Logf("  Error %d: Line %d, Column %d: %s (node: %s)",
						i+1, err.Line, err.Column, err.Message, err.ErrorNode)
				}
			}
		})
	}
}

func TestValidator_EmptyCode(t *testing.T) {
	validator := NewValidator()

	testCases := []struct {
		name     string
		language string
		code     string
	}{
		{
			name:     "Empty string",
			language: "go",
			code:     "",
		},
		{
			name:     "Whitespace only",
			language: "python",
			code:     "   \n  \t  \n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.Validate(tc.code, tc.language)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !result.Valid {
				t.Errorf("Expected empty/whitespace code to be valid, but got errors: %+v", result.Errors)
			}

			if result.ParsedBytes != 0 {
				t.Errorf("Expected parsed bytes = 0 for empty code, got %d", result.ParsedBytes)
			}
		})
	}
}

func TestValidator_UnsupportedLanguage(t *testing.T) {
	validator := NewValidator()

	testCases := []struct {
		name     string
		language string
		code     string
	}{
		{
			name:     "Unsupported language - Rust",
			language: "rust",
			code:     `fn main() { println!("test"); }`,
		},
		{
			name:     "Unsupported language - Java",
			language: "java",
			code:     `public class Test { }`,
		},
		{
			name:     "Unknown language",
			language: "unknown",
			code:     "some code",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.Validate(tc.code, tc.language)
			if err == nil {
				t.Fatalf("Expected error for unsupported language %s, but got result: %+v", tc.language, result)
			}

			if result != nil {
				t.Errorf("Expected nil result for unsupported language, but got: %+v", result)
			}
		})
	}
}

func TestValidator_SupportsLanguage(t *testing.T) {
	validator := NewValidator()

	testCases := []struct {
		language  string
		supported bool
	}{
		{"go", true},
		{"golang", true},
		{"python", true},
		{"py", true},
		{"typescript", true},
		{"ts", true},
		{"javascript", true},
		{"js", true},
		{"bash", true},
		{"sh", true},
		{"rust", false}, // Not yet added
		{"java", false}, // Not yet added
		{"unknown", false},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.language, func(t *testing.T) {
			result := validator.SupportsLanguage(tc.language)
			if result != tc.supported {
				t.Errorf("SupportsLanguage(%q) = %v, want %v", tc.language, result, tc.supported)
			}
		})
	}
}

func TestValidator_ErrorDetails(t *testing.T) {
	validator := NewValidator()

	// Test with code that has a known syntax error
	code := `package main

func main() {
    x := 5
    y :=
}`

	result, err := validator.Validate(code, "go")
	if err != nil {
		t.Fatalf("Unexpected validation error: %v", err)
	}

	if result.Valid {
		t.Fatal("Expected code to be invalid")
	}

	if len(result.Errors) == 0 {
		t.Fatal("Expected at least one syntax error")
	}

	// Check that error has line and column information
	firstError := result.Errors[0]
	if firstError.Line <= 0 {
		t.Errorf("Expected line > 0, got %d", firstError.Line)
	}

	if firstError.Column <= 0 {
		t.Errorf("Expected column > 0, got %d", firstError.Column)
	}

	if firstError.Message == "" {
		t.Error("Expected non-empty error message")
	}

	if firstError.ErrorNode == "" {
		t.Error("Expected non-empty error node type")
	}

	t.Logf("Error details: Line %d, Column %d: %s (node: %s)",
		firstError.Line, firstError.Column, firstError.Message, firstError.ErrorNode)
}

func TestSupportedValidationLanguages(t *testing.T) {
	languages := SupportedValidationLanguages()

	if len(languages) == 0 {
		t.Fatal("Expected at least one supported language")
	}

	// Check that known languages are in the list
	expectedLanguages := []string{"go", "python", "typescript", "javascript", "bash"}
	for _, expected := range expectedLanguages {
		found := false
		for _, lang := range languages {
			if lang == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected %s to be in supported languages, but it wasn't", expected)
		}
	}
}

func TestIsValidationSupported(t *testing.T) {
	testCases := []struct {
		language  string
		supported bool
	}{
		{"go", true},
		{"python", true},
		{"typescript", true},
		{"javascript", true},
		{"bash", true},
		{"unknown", false},
		{"rust", false}, // Not yet in supported list (will be added in Phase 4)
	}

	for _, tc := range testCases {
		t.Run(tc.language, func(t *testing.T) {
			result := IsValidationSupported(tc.language)
			if result != tc.supported {
				t.Errorf("IsValidationSupported(%q) = %v, want %v", tc.language, result, tc.supported)
			}
		})
	}
}
