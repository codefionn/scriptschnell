package tools

import (
	"context"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/syntax"
)

// ReplaceFileToolSpec is the static specification for the replace_file tool
type ReplaceFileToolSpec struct{}

func (s *ReplaceFileToolSpec) Name() string {
	return ToolNameReplaceFile
}

func (s *ReplaceFileToolSpec) Description() string {
	return "Replace the entire content of an existing file with the provided content. Requires the file to exist. Use create_file to create new files."
}

func (s *ReplaceFileToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to replace (relative to working directory)",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "New content to write into the file (replaces all existing content)",
			},
		},
		"required": []string{"path", "content"},
	}
}

// ReplaceFileTool is the executor with runtime dependencies
type ReplaceFileTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewReplaceFileTool(filesystem fs.FileSystem, sess *session.Session) *ReplaceFileTool {
	return &ReplaceFileTool{
		fs:      filesystem,
		session: sess,
	}
}

// Legacy interface implementation for backward compatibility
func (t *ReplaceFileTool) Name() string        { return ToolNameReplaceFile }
func (t *ReplaceFileTool) Description() string { return (&ReplaceFileToolSpec{}).Description() }
func (t *ReplaceFileTool) Parameters() map[string]interface{} {
	return (&ReplaceFileToolSpec{}).Parameters()
}

func (t *ReplaceFileTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	content, ok := params["content"].(string)
	if !ok {
		return &ToolResult{Error: "content is required"}
	}

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("replace_file: error checking if file exists: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}

	if !exists {
		return &ToolResult{Error: fmt.Sprintf("file does not exist: %s (use create_file to create new files)", path)}
	}

	// Read existing content for diff comparison
	var oldContent string
	if oldBytes, err := t.fs.ReadFile(ctx, path); err == nil {
		oldContent = string(oldBytes)
	}

	// Validate syntax before writing (non-blocking)
	var validationWarning string
	language := syntax.DetectLanguage(path)
	if language != "" && syntax.IsValidationSupported(language) {
		validator := syntax.NewValidator()
		validationResult, err := validator.Validate(content, language)
		if err == nil && !validationResult.Valid {
			validationWarning = formatValidationWarning(path, validationResult)
			logger.Warn("replace_file: syntax validation found %d error(s) in %s", len(validationResult.Errors), path)
		}
	}

	if err := t.fs.WriteFile(ctx, path, []byte(content)); err != nil {
		logger.Error("replace_file: error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
		// Also track as read so subsequent diff operations work
		t.session.TrackFileRead(path, content)
	}

	logger.Info("replace_file: replaced %s (%d bytes)", path, len(content))

	// Build result with optional validation warning
	resultMap := map[string]interface{}{
		"path":          path,
		"bytes_written": len(content),
		"replaced":      true,
	}
	if validationWarning != "" {
		resultMap["validation_warning"] = validationWarning
	}

	// Generate UI result with validation warning if present
	uiResult := generateGitDiff(path, oldContent, content)
	if validationWarning != "" {
		uiResult = fmt.Sprintf("%s\n\n⚠️  **Syntax Validation**\n%s", uiResult, validationWarning)
	}

	return &ToolResult{
		Result:   resultMap,
		UIResult: uiResult,
	}
}

// NewReplaceFileToolFactory creates a factory for ReplaceFileTool
func NewReplaceFileToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewReplaceFileTool(filesystem, sess)
	}
}
