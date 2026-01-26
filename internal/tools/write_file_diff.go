package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/syntax"
	"github.com/sourcegraph/go-diff/diff"
)

// WriteFileDiffToolSpec is the static specification for the write_file_diff tool
type WriteFileDiffToolSpec struct{}

func (s *WriteFileDiffToolSpec) Name() string {
	return ToolNameEditFile
}

func (s *WriteFileDiffToolSpec) Description() string {
	return "Update an existing file by applying a unified diff (with standard hunk headers). The file must have been read earlier in the session."
}

func (s *WriteFileDiffToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to update (relative to working directory)",
			},
			"diff": map[string]interface{}{
				"type": "string",
				"description": `Unified diff describing the desired changes; must include file headers and hunk markers. Here are some examples:
Example file 1 (main.go):

package main

import (
	"fmt"
	"invalid"
)

Example update diff 1:

--- a/main.go
+++ b/main.go
@@ -2,5 +2,10 @@ package main

 import (
        "fmt"
-       "invalid"
+  "os"
 )
+
+func main() {
+  fmt.Println("Hello, World!")
+  os.Exit(0)
+}

Example file 2 (numbers.txt):
0
1
2
3
4
5
6
7

Example update diff 2:
--- a/numbers.txt
+++ b/numbers.txt
@@ -3,6 +3,9 @@
 2
 3
 4
+This is a different line.
+This is yet another line.
+This is yet another different line.
 5
 6
 7
`,
			},
		},
		"required": []string{"path", "diff"},
	}
}

func (s *WriteFileDiffToolSpec) RequiresExclusiveExecution() bool { return true }

// WriteFileDiffTool applies unified diffs to existing files.
// This tool required actual git-like diffs with hunk headers
// which seems to be a big problem in today's LLMs.
type WriteFileDiffTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewWriteFileDiffTool(filesystem fs.FileSystem, sess *session.Session) *WriteFileDiffTool {
	return &WriteFileDiffTool{
		fs:      filesystem,
		session: sess,
	}
}

// Legacy interface implementation for backward compatibility
func (t *WriteFileDiffTool) Name() string        { return ToolNameEditFile }
func (t *WriteFileDiffTool) Description() string { return (&WriteFileDiffToolSpec{}).Description() }
func (t *WriteFileDiffTool) Parameters() map[string]interface{} {
	return (&WriteFileDiffToolSpec{}).Parameters()
}

func (t *WriteFileDiffTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	diff, ok := params["diff"].(string)
	if !ok || diff == "" {
		return &ToolResult{Error: "diff is required"}
	}

	logger.Debug("edit_file: path=%s", path)

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("edit_file: error checking if file exists: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}

	if !exists {
		return &ToolResult{Error: fmt.Sprintf("cannot apply diff to non-existent file: %s (use create_file instead)", path)}
	}

	if t.session != nil && !t.session.WasFileRead(path) {
		return &ToolResult{Error: fmt.Sprintf("file %s was not read in this session; read it before applying a diff", path)}
	}

	currentData, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error reading current file: %v", err)}
	}

	finalContent, err := applyUnifiedDiff(string(currentData), diff)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error applying diff: %v", err)}
	}

	// Validate syntax before writing (non-blocking)
	var validationWarning string
	language := syntax.DetectLanguage(path)
	if language != "" && syntax.IsValidationSupported(language) {
		validator := syntax.NewValidator()
		validationResult, err := validator.Validate(finalContent, language)
		if err == nil && !validationResult.Valid {
			validationWarning = formatValidationWarning(path, finalContent, validationResult)
			logger.Warn("edit_file: syntax validation found %d error(s) in %s", len(validationResult.Errors), path)
		}
	}

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("edit_file: error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("edit_file: updated %s (%d bytes)", path, len(finalContent))

	// Build result with optional validation warning
	resultMap := map[string]interface{}{
		"path":          path,
		"bytes_written": len(finalContent),
		"updated":       true,
	}
	if validationWarning != "" {
		resultMap["validation_warning"] = validationWarning
	}

	// Generate UI result with validation warning if present
	uiResult := generateGitDiff(path, string(currentData), finalContent)
	if validationWarning != "" {
		uiResult = fmt.Sprintf("%s\n\n⚠️  **Syntax Validation**\n%s", uiResult, validationWarning)
	}

	return &ToolResult{
		Result:   resultMap,
		UIResult: uiResult,
	}
}

// applyUnifiedDiff applies a unified diff to content using github.com/sourcegraph/go-diff
func applyUnifiedDiff(original, diffText string) (string, error) {
	// Ensure diff has proper file headers (--- and +++)
	// If it doesn't start with ---, add minimal headers
	if !strings.HasPrefix(diffText, "---") && !strings.HasPrefix(diffText, "diff ") {
		diffText = "--- a/file\n+++ b/file\n" + diffText
	}

	// Parse the unified diff
	fileDiff, err := diff.ParseFileDiff([]byte(diffText))
	if err != nil {
		return "", fmt.Errorf("failed to parse unified diff: %w", err)
	}

	// Split original content into lines
	originalLines := strings.Split(original, "\n")

	// Apply each hunk to build the result
	result := make([]string, 0, len(originalLines))
	currentOrigLine := 0

	for hunkIdx, hunk := range fileDiff.Hunks {
		// Copy unchanged lines before this hunk
		hunkStartLine := int(hunk.OrigStartLine) - 1 // Convert to 0-indexed
		for currentOrigLine < hunkStartLine && currentOrigLine < len(originalLines) {
			result = append(result, originalLines[currentOrigLine])
			currentOrigLine++
		}

		// Validate and apply the hunk with detailed error reporting
		hunkLines := strings.Split(string(hunk.Body), "\n")

		// Track position within the hunk for detailed error reporting
		hunkLineNum := 0
		expectedLine := currentOrigLine + 1 // Convert to 1-indexed for user reports

		for _, line := range hunkLines {
			if len(line) == 0 {
				continue
			}

			switch line[0] {
			case ' ': // Context line - must match original
				if currentOrigLine >= len(originalLines) {
					return "", fmt.Errorf("hunk %d, context mismatch at line %d: file ends unexpectedly, expected context line: %q",
						hunkIdx+1, expectedLine, strings.TrimSpace(line))
				}

				expected := strings.TrimPrefix(line, " ")
				actual := originalLines[currentOrigLine]
				if actual != expected {
					// Calculate context for error message
					startLine := maxInt(0, currentOrigLine-2)
					endLine := minInt(len(originalLines), currentOrigLine+3)

					var linesWithNumbers strings.Builder
					for i := startLine; i < endLine; i++ {
						prefix := "  "
						if i == currentOrigLine {
							prefix = "->"
						}
						linesWithNumbers.WriteString(fmt.Sprintf("%s %3d: %s\n", prefix, i+1, originalLines[i]))
					}

					return "", fmt.Errorf("hunk %d, context mismatch at line %d:\n\nExpected:\n%q\n\nActual:\n%q\n\nSurrounding content:\n%s",
						hunkIdx+1, expectedLine, expected, actual, linesWithNumbers.String())
				}

				result = append(result, originalLines[currentOrigLine])
				currentOrigLine++
				hunkLineNum++
				expectedLine++

			case '-': // Deleted line - must match original
				if currentOrigLine >= len(originalLines) {
					return "", fmt.Errorf("hunk %d, deletion failure at line %d: file ends unexpectedly, expected to delete line: %q",
						hunkIdx+1, expectedLine, strings.TrimPrefix(line, "-"))
				}

				expectedDeletion := strings.TrimPrefix(line, "-")
				actualLine := originalLines[currentOrigLine]
				if actualLine != expectedDeletion {
					startLine := maxInt(0, currentOrigLine-2)
					endLine := minInt(len(originalLines), currentOrigLine+3)

					var linesWithNumbers strings.Builder
					for i := startLine; i < endLine; i++ {
						prefix := "  "
						if i == currentOrigLine {
							prefix = "->"
						}
						linesWithNumbers.WriteString(fmt.Sprintf("%s %3d: %s\n", prefix, i+1, originalLines[i]))
					}

					return "", fmt.Errorf("hunk %d, deletion failure at line %d:\n\nExpected to delete:\n%q\n\nActual content:\n%q\n\nSurrounding content:\n%s",
						hunkIdx+1, expectedLine, expectedDeletion, actualLine, linesWithNumbers.String())
				}

				currentOrigLine++
				hunkLineNum++
				expectedLine++

			case '+': // Added line - add to result
				result = append(result, line[1:])
				hunkLineNum++
				// Note: we don't increment expectedLine here since added lines don't consume original lines
			}
		}
	}

	// Copy remaining unchanged lines after all hunks
	for currentOrigLine < len(originalLines) {
		result = append(result, originalLines[currentOrigLine])
		currentOrigLine++
	}

	return strings.Join(result, "\n"), nil
}

// Helper functions
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// generateGitDiff creates a unified diff in git format for UI display
func generateGitDiff(path, original, modified string) string {
	var diff strings.Builder

	// Handle new file creation (original is empty)
	if original == "" {
		diff.WriteString("--- /dev/null\n")
		diff.WriteString(fmt.Sprintf("+++ b/%s\n", path))
		modifiedLines := strings.Split(modified, "\n")
		diff.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(modifiedLines)))
		for _, line := range modifiedLines {
			diff.WriteString("+")
			diff.WriteString(line)
			diff.WriteString("\n")
		}
		return diff.String()
	}

	// Handle file deletion (modified is empty)
	if modified == "" {
		diff.WriteString(fmt.Sprintf("--- a/%s\n", path))
		diff.WriteString("+++ /dev/null\n")
		originalLines := strings.Split(original, "\n")
		diff.WriteString(fmt.Sprintf("@@ -1,%d +0,0 @@\n", len(originalLines)))
		for _, line := range originalLines {
			diff.WriteString("-")
			diff.WriteString(line)
			diff.WriteString("\n")
		}
		return diff.String()
	}

	originalLines := strings.Split(original, "\n")
	modifiedLines := strings.Split(modified, "\n")

	diff.WriteString(fmt.Sprintf("--- a/%s\n", path))
	diff.WriteString(fmt.Sprintf("+++ b/%s\n", path))

	// Simple diff generation - find common prefix and suffix
	commonPrefix := 0
	for commonPrefix < len(originalLines) && commonPrefix < len(modifiedLines) &&
		originalLines[commonPrefix] == modifiedLines[commonPrefix] {
		commonPrefix++
	}

	commonSuffix := 0
	for commonSuffix < len(originalLines)-commonPrefix && commonSuffix < len(modifiedLines)-commonPrefix &&
		originalLines[len(originalLines)-1-commonSuffix] == modifiedLines[len(modifiedLines)-1-commonSuffix] {
		commonSuffix++
	}

	// Generate hunk header
	oldStart := commonPrefix + 1
	oldCount := len(originalLines) - commonPrefix - commonSuffix
	newStart := commonPrefix + 1
	newCount := len(modifiedLines) - commonPrefix - commonSuffix

	diff.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))

	// Add context lines before changes (up to 3 lines)
	contextStart := commonPrefix - 3
	if contextStart < 0 {
		contextStart = 0
	}
	for i := contextStart; i < commonPrefix; i++ {
		diff.WriteString(" ")
		diff.WriteString(originalLines[i])
		diff.WriteString("\n")
	}

	// Add removed lines
	for i := commonPrefix; i < len(originalLines)-commonSuffix; i++ {
		diff.WriteString("-")
		diff.WriteString(originalLines[i])
		diff.WriteString("\n")
	}

	// Add added lines
	for i := commonPrefix; i < len(modifiedLines)-commonSuffix; i++ {
		diff.WriteString("+")
		diff.WriteString(modifiedLines[i])
		diff.WriteString("\n")
	}

	// Add context lines after changes (up to 3 lines)
	contextEnd := len(originalLines) - commonSuffix + 3
	if contextEnd > len(originalLines) {
		contextEnd = len(originalLines)
	}
	for i := len(originalLines) - commonSuffix; i < contextEnd; i++ {
		diff.WriteString(" ")
		diff.WriteString(originalLines[i])
		diff.WriteString("\n")
	}

	return diff.String()
}

// NewWriteFileDiffToolFactory creates a factory for WriteFileDiffTool
func NewWriteFileDiffToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWriteFileDiffTool(filesystem, sess)
	}
}
