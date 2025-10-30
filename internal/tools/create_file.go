package tools

import (
	"context"
	"fmt"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// CreateFileTool writes brand new files.
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

func (t *CreateFileTool) Name() string {
	return "create_file"
}

func (t *CreateFileTool) Description() string {
	return "Create a new file with the provided content. Fails if the file already exists."
}

func (t *CreateFileTool) Parameters() map[string]interface{} {
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

func (t *CreateFileTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	content, ok := params["content"].(string)
	if !ok {
		content = ""
	}

	if t.fs == nil {
		return nil, fmt.Errorf("file system is not configured")
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("create_file: error checking if file exists: %v", err)
		return nil, fmt.Errorf("error checking file: %w", err)
	}

	if exists {
		return nil, fmt.Errorf("file already exists: %s (use write_file_diff to update existing files)", path)
	}

	if err := t.fs.WriteFile(ctx, path, []byte(content)); err != nil {
		logger.Error("create_file: error writing file: %v", err)
		return nil, fmt.Errorf("error writing file: %w", err)
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
		// Also track as read so subsequent diff operations work
		t.session.TrackFileRead(path, content)
	}

	logger.Info("create_file: created %s (%d bytes)", path, len(content))

	return map[string]interface{}{
		"path":          path,
		"bytes_written": len(content),
		"created":       true,
	}, nil
}
