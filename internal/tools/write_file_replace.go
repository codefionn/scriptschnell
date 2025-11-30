package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// WriteFileReplaceToolSpec is the static specification for the write_file_replace tool
type WriteFileReplaceToolSpec struct{}

func (s *WriteFileReplaceToolSpec) Name() string {
	return ToolNameWriteFileDiff
}

func (s *WriteFileReplaceToolSpec) Description() string {
	return "Update an existing file by replacing a specific string with a new string. Ensure the old_string matches exactly (including whitespace and indentation)."
}

func (s *WriteFileReplaceToolSpec) Parameters() map[string]interface{} {
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

// WriteFileReplaceTool is the executor with runtime dependencies
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

// Legacy interface implementation for backward compatibility
func (t *WriteFileReplaceTool) Name() string { return ToolNameWriteFileDiff }
func (t *WriteFileReplaceTool) Description() string {
	return (&WriteFileReplaceToolSpec{}).Description()
}
func (t *WriteFileReplaceTool) Parameters() map[string]interface{} {
	return (&WriteFileReplaceToolSpec{}).Parameters()
}

func (t *WriteFileReplaceTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	oldString := GetStringParam(params, "old_string", "")
	newString := GetStringParam(params, "new_string", "")
	// newString can be empty (deletion)

	expectedReplacements := GetIntParam(params, "expected_replacements", 1)

	logger.Debug("write_file_diff(replace): path=%s replacements=%d", path, expectedReplacements)

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_diff(replace): error checking if file exists: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}

	if !exists {
		return &ToolResult{Error: fmt.Sprintf("cannot update non-existent file: %s", path)}
	}

	if t.session != nil && !t.session.WasFileRead(path) {
		return &ToolResult{Error: fmt.Sprintf("file %s was not read in this session; read it before updating", path)}
	}

	currentData, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error reading current file: %v", err)}
	}

	content := string(currentData)

	// Special case: if the file is empty, we just write the new content.
	// This allows initializing or overwriting empty files without needing to match an old_string.
	if len(content) == 0 {
		if err := t.fs.WriteFile(ctx, path, []byte(newString)); err != nil {
			logger.Error("write_file_diff(replace): error writing file: %v", err)
			return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
		}

		if t.session != nil {
			t.session.TrackFileModified(path)
		}

		logger.Info("write_file_diff(replace): updated empty file %s", path)

		return &ToolResult{
			Result: map[string]interface{}{
				"path":         path,
				"replacements": 0,
				"updated":      true,
			},
			UIResult: generateGitDiff(path, "", newString),
		}
	}

	// For non-empty files, old_string is required
	if oldString == "" {
		return &ToolResult{Error: "old_string is required for non-empty files"}
	}

	count := strings.Count(content, oldString)

	if count == 0 {
		return &ToolResult{Error: "old_string not found in file"}
	}

	if count != expectedReplacements {
		return &ToolResult{Error: fmt.Sprintf("found %d occurrences of old_string, but expected %d", count, expectedReplacements)}
	}

	finalContent := strings.Replace(content, oldString, newString, -1)

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_diff(replace): error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_diff(replace): updated %s (%d replacements)", path, count)

	return &ToolResult{
		Result: map[string]interface{}{
			"path":         path,
			"replacements": count,
			"updated":      true,
		},
		UIResult: generateGitDiff(path, content, finalContent),
	}
}

// NewWriteFileReplaceToolFactory creates a factory for WriteFileReplaceTool
func NewWriteFileReplaceToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWriteFileReplaceTool(filesystem, sess)
	}
}
