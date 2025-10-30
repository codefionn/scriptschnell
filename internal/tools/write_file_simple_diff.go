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
	return "write_file_diff"
}

func (t *WriteFileSimpleDiffTool) Description() string {
	return "Update an existing file using a diff. File headers (---/+++) plus +/-/space lines are sufficient"
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
				"type":        "string",
				"description": "Simplified unified diff without @@ hunk markers. Example:\nOriginal file:\n package main\n import (\n \t\"fmt\"\n )\n\nDiff:\n --- a/main.go\n +++ b/main.go\n  package main\n  import (\n -\t\"fmt\"\n +\t\"bufio\"\n  )",
			},
		},
		"required": []string{"path", "diff"},
	}
}

func (t *WriteFileSimpleDiffTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	diffText, ok := params["diff"].(string)
	if !ok || diffText == "" {
		return nil, fmt.Errorf("diff is required")
	}

	logger.Debug("write_file_diff(simple): path=%s", path)

	if t.fs == nil {
		return nil, fmt.Errorf("file system is not configured")
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_diff(simple): error checking if file exists: %v", err)
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

	finalContent, err := applySimpleDiff(string(currentData), diffText)
	if err != nil {
		return nil, fmt.Errorf("error applying diff: %w", err)
	}

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_diff(simple): error writing file: %v", err)
		return nil, fmt.Errorf("error writing file: %w", err)
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_diff(simple): updated %s (%d bytes)", path, len(finalContent))

	return map[string]interface{}{
		"path":          path,
		"bytes_written": len(finalContent),
		"updated":       true,
	}, nil
}

func applySimpleDiff(original, diffText string) (string, error) {
	originalLines := strings.Split(original, "\n")
	diffLines := strings.Split(diffText, "\n")

	ops := make([]string, 0, len(diffLines))
	for _, raw := range diffLines {
		line := strings.TrimSuffix(raw, "\r")
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
		ops = append(ops, line)
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
			if pos >= len(originalLines) || originalLines[pos] != payload {
				return "", fmt.Errorf("mismatched removal line %q", payload)
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
	for i := start; i < len(lines); i++ {
		if lines[i] == target {
			return i
		}
	}
	return -1
}
