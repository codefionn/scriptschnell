package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codefionn/scriptschnell/internal/syntax"
)

// formatValidationWarning creates a human-readable warning message from validation results
// with context lines around each error location
func formatValidationWarning(path string, content string, result *syntax.ValidationResult) string {
	if result.Valid {
		return ""
	}

	lines := strings.Split(content, "\n")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d syntax error(s) in %s:\n",
		len(result.Errors), filepath.Base(path)))

	// Limit to first 5 errors for readability
	maxErrors := 5
	for i, syntaxErr := range result.Errors {
		if i >= maxErrors {
			sb.WriteString(fmt.Sprintf("  ... and %d more error(s)\n",
				len(result.Errors)-maxErrors))
			break
		}

		sb.WriteString(fmt.Sprintf("  • Line %d, Column %d: %s\n",
			syntaxErr.Line, syntaxErr.Column, syntaxErr.Message))

		// Extract context lines (4 lines before and after)
		context := extractContextLines(lines, syntaxErr.Line, 4)
		if context != "" {
			sb.WriteString(context)
		}
	}

	return sb.String()
}

// extractContextLines extracts surrounding lines around the error line
func extractContextLines(lines []string, errorLine int, contextLines int) string {
	// errorLine is 1-indexed
	startLine := errorLine - 1 - contextLines
	if startLine < 0 {
		startLine = 0
	}

	endLine := errorLine - 1 + contextLines
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	var sb strings.Builder
	sb.WriteString("\n")
	for i := startLine; i <= endLine; i++ {
		lineNum := i + 1 // 1-indexed
		prefix := "    "
		if i == errorLine-1 {
			prefix = "  > " // Mark the error line
		}
		sb.WriteString(fmt.Sprintf("%s%d: %s\n", prefix, lineNum, lines[i]))
	}

	return sb.String()
}
