package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/syntax"
)

// CreateFileToolSpec is the static specification for the create_file tool
type CreateFileToolSpec struct{}

func (s *CreateFileToolSpec) Name() string {
	return ToolNameCreateFile
}

func (s *CreateFileToolSpec) Description() string {
	return "Create a new file with the provided content. Fails if the file already exists."
}

func (s *CreateFileToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to create (relative to working directory)",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write into the new file (optional, defaults to empty file)",
			},
		},
		"required": []string{"path"},
	}
}

// CreateFileTool is the executor with runtime dependencies
type CreateFileTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewCreateFileTool(filesystem fs.FileSystem, sess *session.Session) *CreateFileTool {
	return &CreateFileTool{
		fs:      filesystem,
		session: sess,
	}
}

// Legacy interface implementation for backward compatibility
func (t *CreateFileTool) Name() string        { return ToolNameCreateFile }
func (t *CreateFileTool) Description() string { return (&CreateFileToolSpec{}).Description() }
func (t *CreateFileTool) Parameters() map[string]interface{} {
	return (&CreateFileToolSpec{}).Parameters()
}

func (t *CreateFileTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	content, ok := params["content"].(string)
	if !ok {
		content = ""
	}

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("create_file: error checking if file exists: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}

	if exists {
		return &ToolResult{Error: fmt.Sprintf("file already exists: %s (use write_file_diff to update existing files)", path)}
	}

	// Validate syntax before writing (non-blocking)
	var validationWarning string
	language := syntax.DetectLanguage(path)
	if language != "" && syntax.IsValidationSupported(language) {
		validator := syntax.NewValidator()
		validationResult, err := validator.Validate(content, language)
		if err == nil && !validationResult.Valid {
			validationWarning = formatValidationWarning(path, validationResult)
			logger.Warn("create_file: syntax validation found %d error(s) in %s", len(validationResult.Errors), path)
		}
	}

	if err := t.fs.WriteFile(ctx, path, []byte(content)); err != nil {
		logger.Error("create_file: error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
		// Also track as read so subsequent diff operations work
		t.session.TrackFileRead(path, content)
	}

	logger.Info("create_file: created %s (%d bytes)", path, len(content))

	// Build result with optional validation warning
	resultMap := map[string]interface{}{
		"path":          path,
		"bytes_written": len(content),
		"created":       true,
	}
	if validationWarning != "" {
		resultMap["validation_warning"] = validationWarning
	}

	// Generate UI result with validation warning if present
	uiResult := generateGitDiff(path, "", content)
	if validationWarning != "" {
		uiResult = fmt.Sprintf("%s\n\n⚠️  **Syntax Validation**\n%s", uiResult, validationWarning)
	}

	return &ToolResult{
		Result:   resultMap,
		UIResult: uiResult,
	}
}

// formatValidationWarning creates a human-readable warning message from validation results
func formatValidationWarning(path string, result *syntax.ValidationResult) string {
	if result.Valid {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d syntax error(s) in %s:\n",
		len(result.Errors), filepath.Base(path)))

	// Limit to first 5 errors for readability
	maxErrors := 5
	for i, err := range result.Errors {
		if i >= maxErrors {
			sb.WriteString(fmt.Sprintf("  ... and %d more error(s)\n",
				len(result.Errors)-maxErrors))
			break
		}
		sb.WriteString(fmt.Sprintf("  • Line %d, Column %d: %s\n",
			err.Line, err.Column, err.Message))
	}

	return sb.String()
}

// NewCreateFileToolFactory creates a factory for CreateFileTool
func NewCreateFileToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewCreateFileTool(filesystem, sess)
	}
}
