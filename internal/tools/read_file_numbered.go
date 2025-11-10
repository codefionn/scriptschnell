package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// ReadFileNumberedTool reads files and prefixes each line with its line number.
type ReadFileNumberedTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewReadFileNumberedTool(filesystem fs.FileSystem, sess *session.Session) *ReadFileNumberedTool {
	return &ReadFileNumberedTool{
		fs:      filesystem,
		session: sess,
	}
}

func (t *ReadFileNumberedTool) Name() string {
	return "read_file"
}

func (t *ReadFileNumberedTool) Description() string {
	return "Read a file from the filesystem (format: [padded line number][space][line]). Supports entire file or specific line ranges (max 2000 lines)."
}

func (t *ReadFileNumberedTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read (relative to working directory)",
			},
			"from_line": map[string]interface{}{
				"type":        "integer",
				"description": "Starting line number (1-indexed, optional)",
			},
			"to_line": map[string]interface{}{
				"type":        "integer",
				"description": "Ending line number (1-indexed, optional, max 2000 lines from start)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileNumberedTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	fromLine := GetIntParam(params, "from_line", 0)
	toLine := GetIntParam(params, "to_line", 0)

	logger.Debug("read_file (numbered): path=%s, from_line=%d, to_line=%d", path, fromLine, toLine)

	// Check if file exists
	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("read_file (numbered): error checking if file exists: %v", err)
		return nil, fmt.Errorf("error checking file: %w", err)
	}
	if !exists {
		logger.Warn("read_file (numbered): file not found: %s", path)
		return nil, fmt.Errorf("file not found: %s", path)
	}

	var (
		lines             []string
		content           string
		truncationMessage string
		startLineNumber   = 1
	)

	if fromLine > 0 && toLine > 0 {
		// Read specific line range
		if toLine-fromLine+1 > 2000 {
			return nil, fmt.Errorf("cannot read more than 2000 lines at once")
		}

		lines, err = t.fs.ReadFileLines(ctx, path, fromLine, toLine)
		if err != nil {
			return nil, fmt.Errorf("error reading file lines: %w", err)
		}
		startLineNumber = fromLine
	} else {
		// Read entire file
		data, err := t.fs.ReadFile(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("error reading file: %w", err)
		}
		rawContent := string(data)
		totalLineCount := strings.Count(rawContent, "\n") + 1

		if totalLineCount > 2000 {
			// Read only first 2000 lines
			lines, err = t.fs.ReadFileLines(ctx, path, 1, 2000)
			if err != nil {
				return nil, fmt.Errorf("error reading file lines: %w", err)
			}
			truncationMessage = fmt.Sprintf("[... file truncated, %d total lines, showing first 2000 lines. Use from_line and to_line parameters to read more]", totalLineCount)
		} else {
			lines = splitPreserveLines(rawContent)
		}
	}

	content = prependFormatNotice(formatLinesWithNumbers(lines, startLineNumber))
	if truncationMessage != "" {
		content += "\n\n" + truncationMessage
	}

	// Track file as read in session
	if t.session != nil {
		t.session.TrackFileRead(path, content)
	}

	lineCount := len(lines)
	logger.Info("read_file (numbered): successfully read %s (%d lines)", path, lineCount)

	return map[string]interface{}{
		"path":    path,
		"content": content,
		"lines":   lineCount,
		"format":  "[padded line number] [line]",
	}, nil
}

func splitPreserveLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}

func formatLinesWithNumbers(lines []string, start int) string {
	if len(lines) == 0 {
		return ""
	}

	endLine := start + len(lines) - 1
	width := len(fmt.Sprintf("%d", endLine))
	formatted := make([]string, len(lines))

	for i, line := range lines {
		lineNumber := start + i
		formatted[i] = fmt.Sprintf("%*d %s", width, lineNumber, line)
	}

	return strings.Join(formatted, "\n")
}

func prependFormatNotice(content string) string {
	const notice = "[Line format: [padded line number] [line]]"
	if content == "" {
		return notice
	}
	return notice + "\n" + content
}
