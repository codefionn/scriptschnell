package tools

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/syntax"
)

func TestFormatValidationWarning(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		content       string
		result        *syntax.ValidationResult
		expectEmpty   bool
		checkContains []string
	}{
		{
			name:        "Valid result returns empty string",
			path:        "test.go",
			content:     "package main\n\nfunc main() {}\n",
			result:      &syntax.ValidationResult{Valid: true},
			expectEmpty: true,
		},
		{
			name: "Single error with context",
			path: "test.go",
			content: `package main

import (
	"fmt"
	"invalid"
)

func main() {
	fmt.Println("Hello")
}
`,
			result: &syntax.ValidationResult{
				Valid: false,
				Errors: []syntax.SyntaxError{
					{Line: 5, Column: 2, Message: "syntax error"},
				},
			},
			expectEmpty: false,
			checkContains: []string{
				"Found 1 syntax error(s)",
				"Line 5, Column 2",
				"syntax error",
				"> 5:", // Error line marker
			},
		},
		{
			name: "Multiple errors with context",
			path: "test.py",
			content: `def foo():
    pass

def bar(
    # Missing closing parenthesis
    pass
`,
			result: &syntax.ValidationResult{
				Valid: false,
				Errors: []syntax.SyntaxError{
					{Line: 4, Column: 1, Message: "syntax error"},
					{Line: 7, Column: 5, Message: "syntax error"},
				},
			},
			expectEmpty: false,
			checkContains: []string{
				"Found 2 syntax error(s)",
				"Line 4, Column 1",
				"Line 7, Column 5",
			},
		},
		{
			name: "More than 5 errors shows truncation",
			path: "test.js",
			content: `line 1
line 2
line 3
line 4
line 5
line 6
line 7
line 8
`,
			result: &syntax.ValidationResult{
				Valid: false,
				Errors: []syntax.SyntaxError{
					{Line: 1, Column: 1, Message: "error 1"},
					{Line: 2, Column: 1, Message: "error 2"},
					{Line: 3, Column: 1, Message: "error 3"},
					{Line: 4, Column: 1, Message: "error 4"},
					{Line: 5, Column: 1, Message: "error 5"},
					{Line: 6, Column: 1, Message: "error 6"},
				},
			},
			expectEmpty: false,
			checkContains: []string{
				"Found 6 syntax error(s)",
				"... and 1 more error(s)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValidationWarning(tt.path, tt.content, tt.result)

			if tt.expectEmpty {
				if result != "" {
					t.Errorf("expected empty warning, got: %s", result)
				}
				return
			}

			if result == "" {
				t.Fatal("expected non-empty warning")
			}

			for _, expected := range tt.checkContains {
				if !containsHelper(result, expected) {
					t.Errorf("expected warning to contain %q, got:\n%s", expected, result)
				}
			}
		})
	}
}

func TestExtractContextLines(t *testing.T) {
	tests := []struct {
		name            string
		lines           []string
		errorLine       int
		contextLines    int
		expectEmpty     bool
		checkContains   []string
		checkNotContain []string
	}{
		{
			name: "Error in middle of file",
			lines: []string{
				"line 1",
				"line 2",
				"line 3",
				"line 4",
				"line 5",
				"line 6",
				"line 7",
				"line 8",
			},
			errorLine:       4,
			contextLines:    2,
			expectEmpty:     false,
			checkContains:   []string{"> 4: line 4", "2: line 2", "3: line 3", "5: line 5", "6: line 6"},
			checkNotContain: []string{"1: line 1", "8: line 8"},
		},
		{
			name: "Error at start of file",
			lines: []string{
				"line 1",
				"line 2",
				"line 3",
				"line 4",
				"line 5",
			},
			errorLine:       1,
			contextLines:    2,
			expectEmpty:     false,
			checkContains:   []string{"> 1: line 1", "3: line 3"},
			checkNotContain: []string{"0:", "-1:"},
		},
		{
			name: "Error at end of file",
			lines: []string{
				"line 1",
				"line 2",
				"line 3",
				"line 4",
				"line 5",
			},
			errorLine:       5,
			contextLines:    2,
			expectEmpty:     false,
			checkContains:   []string{"> 5: line 5", "3: line 3"},
			checkNotContain: []string{"6:", "7:"},
		},
		{
			name:          "Small file with error",
			lines:         []string{"line 1", "line 2"},
			errorLine:     1,
			contextLines:  4,
			expectEmpty:   false,
			checkContains: []string{"> 1: line 1", "2: line 2"},
		},
		{
			name:         "Empty lines",
			lines:        []string{},
			errorLine:    1,
			contextLines: 2,
			expectEmpty:  false,
		},
		{
			name:            "Zero context lines",
			lines:           []string{"line 1", "line 2", "line 3"},
			errorLine:       2,
			contextLines:    0,
			expectEmpty:     false,
			checkContains:   []string{"> 2: line 2"},
			checkNotContain: []string{"1:", "3:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContextLines(tt.lines, tt.errorLine, tt.contextLines)

			if tt.expectEmpty {
				if result != "" {
					t.Errorf("expected empty context, got: %s", result)
				}
				return
			}

			if result == "" && len(tt.lines) > 0 {
				t.Fatal("expected non-empty context")
			}

			for _, expected := range tt.checkContains {
				if !containsHelper(result, expected) {
					t.Errorf("expected context to contain %q, got:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.checkNotContain {
				if containsHelper(result, notExpected) {
					t.Errorf("expected context to NOT contain %q, got:\n%s", notExpected, result)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsHelper(s, substr string) bool {
	return len(s) >= len(substr) && findSubstringHelper(s, substr) >= 0
}

// Simple substring search
func findSubstringHelper(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
