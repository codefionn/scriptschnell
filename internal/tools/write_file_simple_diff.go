package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// WriteFileSimpleDiffTool applies simplified diffs that omit hunk headers.
// This tool call is more tolerant of LLM output that omits hunk markers
// or other minor formatting differences.
type WriteFileSimpleDiffTool struct {
	fs      fs.FileSystem
	session *session.Session
}

// NewWriteFileSimpleDiffTool creates a new instance of the simplified diff tool.
func NewWriteFileSimpleDiffTool(filesystem fs.FileSystem, sess *session.Session) *WriteFileSimpleDiffTool {
	return &WriteFileSimpleDiffTool{
		fs:      filesystem,
		session: sess,
	}
}

func (t *WriteFileSimpleDiffTool) Name() string {
	return ToolNameWriteFileDiff
}

func (t *WriteFileSimpleDiffTool) Description() string {
	return "Update an existing file using a diff. Include ---/+++ headers and prefix every line with +, -, or space (even for blank lines) while preserving the original whitespace. Do NOT prepend line numbers or other column markers—lines must appear exactly as they should exist in the file."
}

func (t *WriteFileSimpleDiffTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to update (relative to working directory)",
			},
			"diff": map[string]interface{}{
				"type": "string",
				"description": `Simplified unified diff without @@ hunk markers. Always prefix unchanged lines with a space and include +/- for new/deleted blank lines too. Here are some examples:
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
`},
		},
		"required": []string{"path", "diff"},
	}
}

func (t *WriteFileSimpleDiffTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	diffText, ok := params["diff"].(string)
	if !ok || diffText == "" {
		return &ToolResult{Error: "diff is required"}
	}

	logger.Debug("write_file_diff(simple): path=%s", path)

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_diff(simple): error checking if file exists: %v", err)
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

	finalContent, err := applySimpleDiff(string(currentData), diffText)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error applying diff: %v", err)}
	}

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_diff(simple): error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_diff(simple): updated %s (%d bytes)", path, len(finalContent))

	return &ToolResult{
		Result: map[string]interface{}{
			"path":          path,
			"bytes_written": len(finalContent),
			"updated":       true,
		},
		UIResult: generateGitDiff(path, string(currentData), finalContent),
	}
}

func applySimpleDiff(original, diffText string) (string, error) {
	originalLines := strings.Split(original, "\n")
	diffLines := strings.Split(diffText, "\n")
	stripNumbers := shouldStripGlobalLineNumbers(diffLines)

	ops := make([]string, 0, len(diffLines))
	for _, raw := range diffLines {
		line := strings.TrimSuffix(raw, "\r")
		if stripNumbers && shouldAttemptGlobalStrip(line) {
			if stripped, ok := stripNumberPrefixForced(line); ok {
				line = stripped
			}
		}
		switch {
		case strings.HasPrefix(line, "diff "):
			continue
		case strings.HasPrefix(line, "index "):
			continue
		case strings.HasPrefix(line, "--- "):
			continue
		case strings.HasPrefix(line, "+++ "):
			continue
		case strings.HasPrefix(line, "@@"):
			continue // tolerate accidental hunk headers
		case strings.HasPrefix(line, "\\ No newline at end of file"):
			continue
		}
		if line == "" {
			continue
		}
		normalized := normalizeDiffLine(line)
		if normalized == "" {
			continue
		}
		ops = append(ops, normalized)
	}

	result := make([]string, 0, len(originalLines))
	pos := 0

	for _, op := range ops {
		if op == "" {
			continue
		}

		prefix := op[0]
		payload := ""
		if len(op) > 1 {
			payload = op[1:]
		}

		switch prefix {
		case ' ':
			idx := findNextLine(originalLines, pos, payload)
			if idx < 0 {
				return "", fmt.Errorf("context line %q not found in original content", payload)
			}
			for pos <= idx && pos < len(originalLines) {
				result = append(result, originalLines[pos])
				pos++
			}
		case '-':
			idx := findNextLine(originalLines, pos, payload)
			if idx < 0 {
				return "", fmt.Errorf("line to remove %q not found in original content", payload)
			}
			for pos < idx && pos < len(originalLines) {
				result = append(result, originalLines[pos])
				pos++
			}
			if pos >= len(originalLines) {
				return "", fmt.Errorf("mismatched removal line %q", payload)
			}

			if !linesEquivalent(originalLines[pos], payload) {
				if stripped, ok := stripLineNumberPrefix(payload); !ok || !linesEquivalent(originalLines[pos], stripped) {
					return "", fmt.Errorf("mismatched removal line %q", payload)
				}
			}
			pos++
		case '+':
			result = append(result, payload)
		default:
			// Some models occasionally omit the +- prefix; treat these as additions.
			result = append(result, op)
		}
	}

	for pos < len(originalLines) {
		result = append(result, originalLines[pos])
		pos++
	}

	return strings.Join(result, "\n"), nil
}

func findNextLine(lines []string, start int, target string) int {
	if idx := findLineMatch(lines, start, target); idx >= 0 {
		return idx
	}

	if stripped, ok := stripLineNumberPrefix(target); ok {
		return findLineMatch(lines, start, stripped)
	}

	return -1
}

func findLineMatch(lines []string, start int, target string) int {
	for i := start; i < len(lines); i++ {
		if linesEquivalent(lines[i], target) {
			return i
		}
	}
	return -1
}

func normalizeDiffLine(line string) string {
	if normalized, ok := stripNumberedLine(line); ok {
		return normalized
	}
	return line
}

func shouldStripGlobalLineNumbers(lines []string) bool {
	candidates := 0
	numbered := 0
	for _, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		if !shouldAttemptGlobalStrip(line) {
			continue
		}
		candidates++
		if _, ok := stripLineNumberPrefix(line); ok {
			numbered++
		}
	}
	if candidates < 3 {
		return false
	}
	return numbered*3 >= candidates*2
}

func shouldAttemptGlobalStrip(line string) bool {
	if line == "" {
		return false
	}
	switch {
	case strings.HasPrefix(line, "diff "):
		return false
	case strings.HasPrefix(line, "index "):
		return false
	case strings.HasPrefix(line, "--- "):
		return false
	case strings.HasPrefix(line, "+++ "):
		return false
	case strings.HasPrefix(line, "@@"):
		return false
	case strings.HasPrefix(line, "\\ No newline at end of file"):
		return false
	default:
		return true
	}
}

func stripNumberedLine(line string) (string, bool) {
	if line == "" {
		return line, false
	}

	leadingWhitespace := countLeadingWhitespace(line)
	if leadingWhitespace == len(line) {
		return line, false
	}

	idx := leadingWhitespace
	// Require digits after the whitespace; otherwise it's not a numbered line.
	for idx < len(line) && line[idx] >= '0' && line[idx] <= '9' {
		idx++
	}
	if idx == leadingWhitespace {
		return line, false
	}

	// Skip optional punctuation such as ".", ":", ")", "|"
	if idx < len(line) && strings.ContainsRune(".:)|", rune(line[idx])) {
		idx++
	}

	// Skip additional whitespace between the number and the content.
	for idx < len(line) && (line[idx] == ' ' || line[idx] == '\t') {
		idx++
	}

	rest := line[idx:]
	if rest == "" {
		return " ", true
	}

	// If the line originally started with a diff prefix (space/+/−) and the remaining
	// content does not begin with +/-, assume this was a legitimate line that starts
	// with digits and skip normalization to avoid corrupting content.
	if leadingWhitespace > 0 && rest[0] != '+' && rest[0] != '-' {
		return line, false
	}

	switch rest[0] {
	case '+', '-', ' ':
		return rest, true
	default:
		return " " + rest, true
	}
}

func countLeadingWhitespace(s string) int {
	count := 0
	for count < len(s) && (s[count] == ' ' || s[count] == '\t') {
		count++
	}
	return count
}

func isWhitespaceOnly(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

func stripLineNumberPrefix(s string) (string, bool) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}

	startDigits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == startDigits {
		return s, false
	}

	if i < len(s) && strings.ContainsRune(".:)|", rune(s[i])) {
		i++
	}

	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}

	return s[i:], true
}

func linesEquivalent(original, payload string) bool {
	if original == payload {
		return true
	}
	if isWhitespaceOnly(original) && isWhitespaceOnly(payload) {
		return true
	}
	if matchesMissingBullet(original, payload) {
		return true
	}
	return false
}

func matchesMissingBullet(original, payload string) bool {
	if len(original) < 2 {
		return false
	}

	trimStart := 0
	for trimStart < len(original) && original[trimStart] == ' ' {
		trimStart++
	}
	if trimStart >= len(original)-1 {
		return false
	}

	b := original[trimStart]
	if b != '-' && b != '+' && b != '*' {
		return false
	}
	if original[trimStart+1] != ' ' {
		return false
	}

	candidate := original[:trimStart] + original[trimStart+1:]
	return candidate == payload
}

func stripNumberPrefixForced(line string) (string, bool) {
	stripped, ok := stripLineNumberPrefix(line)
	if !ok {
		return line, false
	}
	if stripped == "" {
		return " ", true
	}
	switch stripped[0] {
	case '+', '-', ' ':
		return stripped, true
	default:
		return " " + stripped, true
	}
}
