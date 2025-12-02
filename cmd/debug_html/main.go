package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/codefionn/scriptschnell/internal/htmlconv"
)

func main() {
	// Read HTML from stdin
	var builder strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			builder.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	input := builder.String()

	fmt.Fprintf(os.Stderr, "Input size: %d bytes\n", len(input))
	fmt.Fprintf(os.Stderr, "First 200 chars: %s\n\n", input[:min(200, len(input))])

	markdown, converted := htmlconv.ConvertIfHTML(input)

	if !converted {
		fmt.Fprintln(os.Stderr, "❌ HTML was not detected")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "✓ HTML detected and converted\n")
	fmt.Fprintf(os.Stderr, "Output size: %d bytes\n\n", len(markdown))

	if len(markdown) == 0 {
		fmt.Fprintln(os.Stderr, "❌ Conversion produced 0 characters")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Converted markdown:\n")
	fmt.Fprintf(os.Stderr, "======================\n")
	fmt.Println(markdown)
	fmt.Fprintf(os.Stderr, "======================\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
