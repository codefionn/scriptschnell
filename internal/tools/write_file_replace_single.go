package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

// WriteFileReplaceSingleToolSpec is the static specification for the write_file_replace_single tool
type WriteFileReplaceSingleToolSpec struct{}

func (s *WriteFileReplaceSingleToolSpec) Name() string {
	return ToolNameEditFile
}

func (s *WriteFileReplaceSingleToolSpec) Description() string {
	return `Update an existing file by replacing a single text occurrence. 
Ensure old_string matches exactly (including whitespace and indentation). 
Be careful around opening and closing brackets (e.g. '{' and '}' in C like languages) when editing code.`
}

func (s *WriteFileReplaceSingleToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to update (relative to working directory)",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "Exact text to replace. Must be present in the file.",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "Replacement text. Empty string deletes the match.",
			},
			"expected_replacements": map[string]interface{}{
				"type":        "integer",
				"description": "How many times old_string should appear. Defaults to 1 if omitted.",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (s *WriteFileReplaceSingleToolSpec) RequiresExclusiveExecution() bool { return true }

// WriteFileReplaceSingleTool is the executor with runtime dependencies.
type WriteFileReplaceSingleTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewWriteFileReplaceSingleTool(filesystem fs.FileSystem, sess *session.Session) *WriteFileReplaceSingleTool {
	return &WriteFileReplaceSingleTool{
		fs:      filesystem,
		session: sess,
	}
}

func (t *WriteFileReplaceSingleTool) Name() string { return ToolNameEditFile }
func (t *WriteFileReplaceSingleTool) Description() string {
	return (&WriteFileReplaceSingleToolSpec{}).Description()
}
func (t *WriteFileReplaceSingleTool) Parameters() map[string]interface{} {
	return (&WriteFileReplaceSingleToolSpec{}).Parameters()
}

func (t *WriteFileReplaceSingleTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	oldString := GetStringParam(params, "old_string", "")
	newString := GetStringParam(params, "new_string", "")
	expectedReplacements := GetIntParam(params, "expected_replacements", 1)

	// Check if old_string is empty.
	// Note: Empty old_string is generally not allowed unless the file is empty,
	// but for replacement logic, we usually need something to replace.
	// If the user wants to overwrite an empty file, they should use create_file or we can handle it here.
	// The original write_file_replace handles empty files by allowing empty old_string if file is empty.
	// We'll replicate that logic.

	logger.Debug("write_file_replace_single: path=%s", path)

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_replace_single: error checking if file exists: %v", err)
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

	if len(content) == 0 {
		if err := t.fs.WriteFile(ctx, path, []byte(newString)); err != nil {
			logger.Error("write_file_replace_single: error writing file: %v", err)
			return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
		}

		if t.session != nil {
			t.session.TrackFileModified(path)
		}

		logger.Info("write_file_replace_single: updated empty file %s", path)

		return &ToolResult{
			Result: map[string]interface{}{
				"path":         path,
				"replacements": 0,
				"updated":      true,
			},
			UIResult: generateGitDiff(path, "", newString),
		}
	}

	if oldString == "" {
		return &ToolResult{Error: "old_string is required for non-empty files"}
	}

	if expectedReplacements == 0 {
		expectedReplacements = 1
	}

	count := strings.Count(content, oldString)
	if count == 0 {
		return &ToolResult{Error: "old_string not found in file. Try to read the file again and redo the edit."}
	}
	if count != expectedReplacements {
		return &ToolResult{Error: fmt.Sprintf("found %d occurrences of old_string, but expected %d. Try to read more surrounding text and redo the edit.", count, expectedReplacements)}
	}

	finalContent := strings.Replace(content, oldString, newString, -1)
	totalReplacements := count

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_replace_single: error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_replace_single: updated %s (%d replacements)", path, totalReplacements)

	return &ToolResult{
		Result: map[string]interface{}{
			"path":         path,
			"replacements": totalReplacements,
			"updated":      true,
		},
		UIResult: generateGitDiff(path, content, finalContent),
	}
}

// NewWriteFileReplaceSingleToolFactory creates a factory for WriteFileReplaceSingleTool
func NewWriteFileReplaceSingleToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWriteFileReplaceSingleTool(filesystem, sess)
	}
}
