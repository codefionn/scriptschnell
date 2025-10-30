package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// ReadFileTool allows reading files
type ReadFileTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewReadFileTool(filesystem fs.FileSystem, sess *session.Session) *ReadFileTool {
	return &ReadFileTool{
		fs:      filesystem,
		session: sess,
	}
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read a file from the filesystem. Can read entire file or specific line ranges. Maximum 2000 lines per read. Files read during the session are tracked for diff operations."
}

func (t *ReadFileTool) Parameters() map[string]interface{} {
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

func (t *ReadFileTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	fromLine := GetIntParam(params, "from_line", 0)
	toLine := GetIntParam(params, "to_line", 0)

	logger.Debug("read_file: path=%s, from_line=%d, to_line=%d", path, fromLine, toLine)

	// Check if file exists
	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("read_file: error checking if file exists: %v", err)
		return nil, fmt.Errorf("error checking file: %w", err)
	}
	if !exists {
		logger.Warn("read_file: file not found: %s", path)
		return nil, fmt.Errorf("file not found: %s", path)
	}

	var content string
	var lines []string

	if fromLine > 0 && toLine > 0 {
		// Read specific line range
		if toLine-fromLine+1 > 2000 {
			return nil, fmt.Errorf("cannot read more than 2000 lines at once")
		}

		lines, err = t.fs.ReadFileLines(ctx, path, fromLine, toLine)
		if err != nil {
			return nil, fmt.Errorf("error reading file lines: %w", err)
		}
		content = strings.Join(lines, "\n")
	} else {
		// Read entire file
		data, err := t.fs.ReadFile(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("error reading file: %w", err)
		}
		content = string(data)

		// Check line limit
		lineCount := strings.Count(content, "\n") + 1
		if lineCount > 2000 {
			// Read only first 2000 lines
			lines, err = t.fs.ReadFileLines(ctx, path, 1, 2000)
			if err != nil {
				return nil, fmt.Errorf("error reading file lines: %w", err)
			}
			content = strings.Join(lines, "\n")
			content += fmt.Sprintf("\n\n[... file truncated, %d total lines, showing first 2000 lines. Use from_line and to_line parameters to read more]", lineCount)
		}
	}

	// Track file as read in session
	if t.session != nil {
		t.session.TrackFileRead(path, content)
	}

	lineCount := len(strings.Split(content, "\n"))
	logger.Info("read_file: successfully read %s (%d lines)", path, lineCount)

	return map[string]interface{}{
		"path":    path,
		"content": content,
		"lines":   lineCount,
	}, nil
}
