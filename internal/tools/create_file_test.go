package tools

import (
	"context"
	"testing"

	"github.com/statcode-ai/scriptschnell/internal/fs"
	"github.com/statcode-ai/scriptschnell/internal/session"
)

func TestCreateFileTool_Name(t *testing.T) {
	tool := NewCreateFileTool(nil, nil)
	if tool.Name() != ToolNameCreateFile {
		t.Fatalf("expected name '%s', got '%s'", ToolNameCreateFile, tool.Name())
	}
}

func TestCreateFileTool_Parameters(t *testing.T) {
	tool := NewCreateFileTool(nil, nil)
	params := tool.Parameters()

	if params["type"] != "object" {
		t.Fatalf("expected type 'object'")
	}

	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties to be a map")
	}

	if _, ok := properties["path"]; !ok {
		t.Fatalf("expected path property")
	}

	if _, ok := properties["content"]; !ok {
		t.Fatalf("expected content property")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("expected required to be []string")
	}
	if len(required) != 1 || required[0] != "path" {
		t.Fatalf("expected required to contain only path, got %v", required)
	}
}

func TestCreateFileTool_CreateNewFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", "/workspace")
	tool := NewCreateFileTool(mockFS, sess)

	params := map[string]interface{}{
		"path":    "newfile.txt",
		"content": "Hello, world!",
	}

	result := tool.Execute(context.Background(), params)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result.Result)
	}

	if resultMap["path"] != "newfile.txt" {
		t.Fatalf("expected path 'newfile.txt', got %v", resultMap["path"])
	}
	if resultMap["bytes_written"] != len("Hello, world!") {
		t.Fatalf("unexpected bytes_written: %v", resultMap["bytes_written"])
	}
	if created, _ := resultMap["created"].(bool); !created {
		t.Fatalf("expected created=true, got %v", resultMap["created"])
	}

	data, err := mockFS.ReadFile(context.Background(), "newfile.txt")
	if err != nil {
		t.Fatalf("file read failed: %v", err)
	}
	if string(data) != "Hello, world!" {
		t.Fatalf("unexpected file content: %s", data)
	}

	modified := sess.GetModifiedFiles()
	if len(modified) != 1 || modified[0] != "newfile.txt" {
		t.Fatalf("expected session to track newfile.txt, got %v", modified)
	}

	// Verify that the file was also tracked as read for subsequent diffs
	if !sess.WasFileRead("newfile.txt") {
		t.Fatalf("expected session to track newfile.txt as read")
	}
}

func TestCreateFileTool_PathRequired(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewCreateFileTool(mockFS, nil)

	result := tool.Execute(context.Background(), map[string]interface{}{})
	if result.Error == "" || result.Error != "path is required" {
		t.Fatalf("expected path required error, got %s", result.Error)
	}
}

func TestCreateFileTool_ExistingFileError(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewCreateFileTool(mockFS, nil)

	if err := mockFS.WriteFile(context.Background(), "file.txt", []byte("existing")); err != nil {
		t.Fatalf("failed to set up file: %v", err)
	}

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path":    "file.txt",
		"content": "new contents",
	})
	if result.Error == "" {
		t.Fatalf("expected error when file already exists")
	}
}

func TestCreateFileTool_ThenWriteDiff(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", "/workspace")
	createTool := NewCreateFileTool(mockFS, sess)
	diffTool := NewWriteFileDiffTool(mockFS, sess)

	// Create a file
	initialContent := "line 1\nline 2\nline 3"
	params := map[string]interface{}{
		"path":    "test.txt",
		"content": initialContent,
	}

	createResult := createTool.Execute(context.Background(), params)
	if createResult.Error != "" {
		t.Fatalf("create failed: %s", createResult.Error)
	}

	// Verify that the file was tracked as read
	if !sess.WasFileRead("test.txt") {
		t.Fatalf("expected file to be tracked as read after creation")
	}

	// Now apply a diff to the file we just created (without reading it first)
	diffParams := map[string]interface{}{
		"path": "test.txt",
		"diff": `@@ -1,3 +1,3 @@
-line 1
+line 1 modified
 line 2
 line 3`,
	}

	result := diffTool.Execute(context.Background(), diffParams)
	if result.Error != "" {
		t.Fatalf("diff failed after create: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result.Result)
	}

	if updated, _ := resultMap["updated"].(bool); !updated {
		t.Fatalf("expected updated=true, got %v", resultMap["updated"])
	}

	// Verify the diff was applied correctly
	data, err := mockFS.ReadFile(context.Background(), "test.txt")
	if err != nil {
		t.Fatalf("file read failed: %v", err)
	}

	expected := "line 1 modified\nline 2\nline 3"
	if string(data) != expected {
		t.Fatalf("unexpected file content after diff:\nGot:\n%s\nExpected:\n%s", data, expected)
	}
}
