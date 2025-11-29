package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// WriteFileReplaceTool replaces text in existing files.
// This is a variation of the write_file_diff tool that uses exact string replacement.
type WriteFileReplaceTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewWriteFileReplaceTool(filesystem fs.FileSystem, sess *session.Session) *WriteFileReplaceTool {
	return &WriteFileReplaceTool{
		fs:      filesystem,
		session: sess,
	}
}

func (t *WriteFileReplaceTool) Name() string {
	return ToolNameWriteFileDiff
}

func (t *WriteFileReplaceTool) Description() string {
	return "Update an existing file by replacing a specific string with a new string. Ensure the old_string matches exactly (including whitespace and indentation)."
}

func (t *WriteFileReplaceTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to update (relative to working directory)",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "The exact literal text to replace. Must be unique in the file or expected_replacements must be specified.",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "The text to replace old_string with.",
			},
			"expected_replacements": map[string]interface{}{
				"type":        "integer",
				"description": "Number of replacements expected. Defaults to 1 if not specified.",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (t *WriteFileReplaceTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	oldString := GetStringParam(params, "old_string", "")
	if oldString == "" {
		return nil, fmt.Errorf("old_string is required")
	}

	newString := GetStringParam(params, "new_string", "")
	// newString can be empty (deletion)

	expectedReplacements := GetIntParam(params, "expected_replacements", 1)

	logger.Debug("write_file_diff(replace): path=%s replacements=%d", path, expectedReplacements)

	if t.fs == nil {
		return nil, fmt.Errorf("file system is not configured")
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_diff(replace): error checking if file exists: %v", err)
		return nil, fmt.Errorf("error checking file: %w", err)
	}

	if !exists {
		return nil, fmt.Errorf("cannot update non-existent file: %s", path)
	}

	if t.session != nil && !t.session.WasFileRead(path) {
		return nil, fmt.Errorf("file %s was not read in this session; read it before updating", path)
	}

	currentData, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("error reading current file: %w", err)
	}

	content := string(currentData)
	count := strings.Count(content, oldString)

	if count == 0 {
		return nil, fmt.Errorf("old_string not found in file")
	}

	if count != expectedReplacements {
		return nil, fmt.Errorf("found %d occurrences of old_string, but expected %d", count, expectedReplacements)
	}

	finalContent := strings.Replace(content, oldString, newString, -1)

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_diff(replace): error writing file: %v", err)
		return nil, fmt.Errorf("error writing file: %w", err)
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_diff(replace): updated %s (%d replacements)", path, count)

	return map[string]interface{}{
		"path":         path,
		"replacements": count,
		"updated":      true,
	}, nil
}
