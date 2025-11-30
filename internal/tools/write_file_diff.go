package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/sourcegraph/go-diff/diff"
	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// WriteFileDiffToolSpec is the static specification for the write_file_diff tool
type WriteFileDiffToolSpec struct{}

func (s *WriteFileDiffToolSpec) Name() string {
	return ToolNameWriteFileDiff
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
func (t *WriteFileDiffTool) Name() string        { return ToolNameWriteFileDiff }
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

	logger.Debug("write_file_diff: path=%s", path)

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_diff: error checking if file exists: %v", err)
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

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_diff: error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_diff: updated %s (%d bytes)", path, len(finalContent))

	return &ToolResult{
		Result: map[string]interface{}{
			"path":          path,
			"bytes_written": len(finalContent),
			"updated":       true,
		},
		UIResult: generateGitDiff(path, string(currentData), finalContent),
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

	for _, hunk := range fileDiff.Hunks {
		// Copy unchanged lines before this hunk
		hunkStartLine := int(hunk.OrigStartLine) - 1 // Convert to 0-indexed
		for currentOrigLine < hunkStartLine && currentOrigLine < len(originalLines) {
			result = append(result, originalLines[currentOrigLine])
			currentOrigLine++
		}

		// Apply the hunk
		hunkLines := strings.Split(string(hunk.Body), "\n")
		for _, line := range hunkLines {
			if len(line) == 0 {
				continue
			}

			switch line[0] {
			case ' ': // Context line - copy from original
				if currentOrigLine < len(originalLines) {
					result = append(result, originalLines[currentOrigLine])
					currentOrigLine++
				}
			case '-': // Deleted line - skip in original
				if currentOrigLine < len(originalLines) {
					currentOrigLine++
				}
			case '+': // Added line - add to result
				result = append(result, line[1:])
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
