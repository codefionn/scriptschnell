package syntax

import (
	"strings"
	"testing"
)

func TestHighlighter_Go(t *testing.T) {
	h := NewHighlighter()

	code := `package main

import "fmt"

func main() {
	x := 42
	fmt.Println("Hello, World!")
}`

	result, err := h.Highlight(code, "go")
	if err != nil {
		t.Fatalf("Highlight failed: %v", err)
	}

	// Check that some ANSI codes are present (indicating highlighting)
	if !strings.Contains(result, "\033[") {
		t.Error("Expected ANSI color codes in output")
	}

	// Check that result contains the original code (without ANSI codes)
	stripped := stripANSI(result)
	if stripped != code {
		t.Errorf("Highlighted code doesn't match original\nExpected: %q\nGot: %q", code, stripped)
	}
}

func TestHighlighter_Python(t *testing.T) {
	h := NewHighlighter()

	code := `def hello():
    print("Hello, World!")
    return 42`

	result, err := h.Highlight(code, "python")
	if err != nil {
		t.Fatalf("Highlight failed: %v", err)
	}

	if !strings.Contains(result, "\033[") {
		t.Error("Expected ANSI color codes in output")
	}
}

func TestHighlighter_JavaScript(t *testing.T) {
	h := NewHighlighter()

	code := `function hello() {
    const x = 42;
    console.log("Hello, World!");
}`

	result, err := h.Highlight(code, "javascript")
	if err != nil {
		t.Fatalf("Highlight failed: %v", err)
	}

	if !strings.Contains(result, "\033[") {
		t.Error("Expected ANSI color codes in output")
	}
}

func TestHighlighter_Bash(t *testing.T) {
	h := NewHighlighter()

	code := `#!/bin/bash
for i in 1 2 3; do
    echo "Number: $i"
done`

	result, err := h.Highlight(code, "bash")
	if err != nil {
		t.Fatalf("Highlight failed: %v", err)
	}

	if !strings.Contains(result, "\033[") {
		t.Error("Expected ANSI color codes in output")
	}
}

func TestHighlighter_UnsupportedLanguage(t *testing.T) {
	h := NewHighlighter()

	code := "some code"
	result, err := h.Highlight(code, "unsupported")

	if err != nil {
		t.Fatalf("Highlight failed: %v", err)
	}

	// Should return code as-is for unsupported languages
	if result != code {
		t.Errorf("Expected unchanged code for unsupported language\nExpected: %q\nGot: %q", code, result)
	}
}

// stripANSI removes ANSI escape codes from a string
func stripANSI(s string) string {
	var result strings.Builder
	inEscape := false

	for i := 0; i < len(s); i++ {
		if s[i] == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if s[i] == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteByte(s[i])
	}

	return result.String()
}
