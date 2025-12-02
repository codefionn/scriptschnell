package tools

import (
	"context"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
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

	return &ToolResult{
		Result: map[string]interface{}{
			"path":          path,
			"bytes_written": len(content),
			"created":       true,
		},
		UIResult: generateGitDiff(path, "", content),
	}
}

// NewCreateFileToolFactory creates a factory for CreateFileTool
func NewCreateFileToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewCreateFileTool(filesystem, sess)
	}
}
