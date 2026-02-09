//go:build cgo

package syntax

import (
	"fmt"
	"testing"
)

// This is a manual test to visualize the highlighting output
func TestHighlighter_Visual(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping visual test in short mode")
	}

	h := NewHighlighter()

	goCode := `package main

import "fmt"

func main() {
	x := 42
	name := "World"
	fmt.Println("Hello,", name)
}`

	result, err := h.Highlight(goCode, "go")
	if err != nil {
		t.Fatalf("Highlight failed: %v", err)
	}

	fmt.Println("\n=== Go Code Highlighted ===")
	fmt.Println(result)
	fmt.Println("=== End ===")

	pythonCode := `def greet(name):
    """Greet someone"""
    message = f"Hello, {name}!"
    return message

result = greet("World")
print(result)`

	result, err = h.Highlight(pythonCode, "python")
	if err != nil {
		t.Fatalf("Highlight failed: %v", err)
	}

	fmt.Println("\n=== Python Code Highlighted ===")
	fmt.Println(result)
	fmt.Println("=== End ===")
}
