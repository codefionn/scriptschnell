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

func (t *WriteFileDiffTool) Name() string {
	return ToolNameWriteFileDiff
}

func (t *WriteFileDiffTool) Description() string {
	return "Update an existing file by applying a unified diff (with standard hunk headers). The file must have been read earlier in the session."
}

func (t *WriteFileDiffTool) Parameters() map[string]interface{} {
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

func (t *WriteFileDiffTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	diff, ok := params["diff"].(string)
	if !ok || diff == "" {
		return nil, fmt.Errorf("diff is required")
	}

	logger.Debug("write_file_diff: path=%s", path)

	if t.fs == nil {
		return nil, fmt.Errorf("file system is not configured")
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_diff: error checking if file exists: %v", err)
		return nil, fmt.Errorf("error checking file: %w", err)
	}

	if !exists {
		return nil, fmt.Errorf("cannot apply diff to non-existent file: %s (use create_file instead)", path)
	}

	if t.session != nil && !t.session.WasFileRead(path) {
		return nil, fmt.Errorf("file %s was not read in this session; read it before applying a diff", path)
	}

	currentData, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("error reading current file: %w", err)
	}

	finalContent, err := applyUnifiedDiff(string(currentData), diff)
	if err != nil {
		return nil, fmt.Errorf("error applying diff: %w", err)
	}

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_diff: error writing file: %v", err)
		return nil, fmt.Errorf("error writing file: %w", err)
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_diff: updated %s (%d bytes)", path, len(finalContent))

	return map[string]interface{}{
		"path":          path,
		"bytes_written": len(finalContent),
		"updated":       true,
	}, nil
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
