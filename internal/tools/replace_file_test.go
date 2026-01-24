package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestReplaceFileTool_Name(t *testing.T) {
	tool := NewReplaceFileTool(nil, nil)
	if tool.Name() != ToolNameReplaceFile {
		t.Fatalf("expected name '%s', got '%s'", ToolNameReplaceFile, tool.Name())
	}
}

func TestReplaceFileTool_Description(t *testing.T) {
	tool := NewReplaceFileTool(nil, nil)
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestReplaceFileTool_Parameters(t *testing.T) {
	tool := NewReplaceFileTool(nil, nil)
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	// Check required parameters
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("expected required to be a slice")
	}

	if len(required) != 2 || required[0] != "path" || required[1] != "content" {
		t.Fatalf("expected required to be [path content], got %v", required)
	}

	// Check path property
	pathProp, ok := props["path"].(map[string]interface{})
	if !ok {
		t.Fatal("expected path property to be a map")
	}
	if pathProp["type"] != "string" {
		t.Fatal("expected path type to be string")
	}

	// Check content property
	contentProp, ok := props["content"].(map[string]interface{})
	if !ok {
		t.Fatal("expected content property to be a map")
	}
	if contentProp["type"] != "string" {
		t.Fatal("expected content type to be string")
	}
}

func TestReplaceFileTool_Execute_Success(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReplaceFileTool(mockFS, sess)

	// First create a file
	if err := mockFS.WriteFile(context.Background(), "test.txt", []byte("original content")); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	sess.TrackFileRead("test.txt", "original content")

	// Now replace it
	result := tool.Execute(context.Background(), map[string]interface{}{
		"path":    "test.txt",
		"content": "new content",
	})

	if result.Error != "" {
		t.Fatalf("expected no error, got: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	if resultMap["replaced"] != true {
		t.Fatal("expected replaced to be true")
	}

	if resultMap["bytes_written"] != 11 {
		t.Fatalf("expected 11 bytes written, got %v", resultMap["bytes_written"])
	}

	// Verify file was replaced
	content, err := mockFS.ReadFile(context.Background(), "test.txt")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "new content" {
		t.Fatalf("expected 'new content', got '%s'", string(content))
	}
}

func TestReplaceFileTool_Execute_MissingPath(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReplaceFileTool(mockFS, sess)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"content": "test content",
	})

	if result.Error == "" {
		t.Fatal("expected error for missing path")
	}

	if result.Error != "path is required" {
		t.Fatalf("expected 'path is required' error, got: %s", result.Error)
	}
}

func TestReplaceFileTool_Execute_MissingContent(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReplaceFileTool(mockFS, sess)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})

	if result.Error == "" {
		t.Fatal("expected error for missing content")
	}

	if result.Error != "content is required" {
		t.Fatalf("expected 'content is required' error, got: %s", result.Error)
	}
}

func TestReplaceFileTool_Execute_FileDoesNotExist(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReplaceFileTool(mockFS, sess)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path":    "nonexistent.txt",
		"content": "test content",
	})

	if result.Error == "" {
		t.Fatal("expected error for nonexistent file")
	}

	// Should suggest create_file
	if !strings.Contains(result.Error, "does not exist") || !strings.Contains(result.Error, "create_file") {
		t.Fatalf("expected error to mention file doesn't exist and suggest create_file, got: %s", result.Error)
	}
}

func TestReplaceFileTool_Execute_NilFileSystem(t *testing.T) {
	tool := NewReplaceFileTool(nil, nil)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path":    "test.txt",
		"content": "test content",
	})

	if result.Error == "" {
		t.Fatal("expected error for nil filesystem")
	}

	if result.Error != "file system is not configured" {
		t.Fatalf("expected 'file system is not configured' error, got: %s", result.Error)
	}
}

func TestReplaceFileTool_SessionTracking(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReplaceFileTool(mockFS, sess)

	// Create and read a file
	if err := mockFS.WriteFile(context.Background(), "test.txt", []byte("original")); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	sess.TrackFileRead("test.txt", "original")

	// Replace content
	tool.Execute(context.Background(), map[string]interface{}{
		"path":    "test.txt",
		"content": "replaced",
	})

	// Verify file was tracked as modified
	modified := sess.GetModifiedFiles()
	if len(modified) != 1 || modified[0] != "test.txt" {
		t.Fatalf("expected file to be tracked as modified, got: %v", modified)
	}

	// Verify file was tracked as read (for subsequent diff operations)
	if !sess.WasFileRead("test.txt") {
		t.Fatal("expected file to be tracked as read")
	}
}

func TestReplaceFileToolFactory(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")

	registry := NewRegistry(nil)
	factory := NewReplaceFileToolFactory(mockFS, sess)
	executor := factory(registry)

	if executor == nil {
		t.Fatal("expected non-nil executor")
	}

	replaceTool, ok := executor.(*ReplaceFileTool)
	if !ok {
		t.Fatal("expected executor to be a ReplaceFileTool")
	}

	if replaceTool.fs == nil {
		t.Fatal("expected filesystem to be set")
	}

	if replaceTool.session == nil {
		t.Fatal("expected session to be set")
	}
}