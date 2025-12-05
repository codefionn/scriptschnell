package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestWriteFileReplaceSingleTool_Name(t *testing.T) {
	tool := NewWriteFileReplaceSingleTool(nil, nil)
	if tool.Name() != ToolNameEditFile {
		t.Fatalf("expected name '%s', got '%s'", ToolNameEditFile, tool.Name())
	}
}

func TestWriteFileReplaceSingleTool_Parameters(t *testing.T) {
	tool := NewWriteFileReplaceSingleTool(nil, nil)
	params := tool.Parameters()

	if params["type"] != "object" {
		t.Fatalf("expected type 'object'")
	}

	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties map")
	}

	if _, ok := properties["path"]; !ok {
		t.Fatalf("expected path property")
	}
	if _, ok := properties["old_string"]; !ok {
		t.Fatalf("expected old_string property")
	}
	if _, ok := properties["new_string"]; !ok {
		t.Fatalf("expected new_string property")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("expected required slice")
	}

	expectedRequired := map[string]bool{"path": true, "old_string": true, "new_string": true}
	for _, req := range required {
		if !expectedRequired[req] {
			t.Fatalf("unexpected required field: %s", req)
		}
		delete(expectedRequired, req)
	}
	if len(expectedRequired) > 0 {
		t.Fatalf("missing required fields: %v", expectedRequired)
	}
}

func TestWriteFileReplaceSingleTool_AppliesReplacement(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceSingleTool(mockFS, sess)

	original := "line 1\nline 2\nline 3"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	result := tool.Execute(ctx, map[string]interface{}{
		"path":       "file.txt",
		"old_string": "line 2",
		"new_string": "line 2 modified",
	})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result.Result)
	}
	if updated, _ := resultMap["updated"].(bool); !updated {
		t.Fatalf("expected updated=true, got %v", resultMap["updated"])
	}
	if replacements, _ := resultMap["replacements"].(int); replacements != 1 {
		t.Fatalf("expected replacements=1, got %v", resultMap["replacements"])
	}

	data, err := mockFS.ReadFile(ctx, "file.txt")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "line 1\nline 2 modified\nline 3" {
		t.Fatalf("unexpected file content: %s", data)
	}

	modified := sess.GetModifiedFiles()
	if len(modified) != 1 || modified[0] != "file.txt" {
		t.Fatalf("expected file.txt to be tracked as modified, got %v", modified)
	}
}

func TestWriteFileReplaceSingleTool_RequiresFields(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	tool := NewWriteFileReplaceSingleTool(mockFS, nil)

	// For old_string validation, we need a non-empty file to exist
	if err := mockFS.WriteFile(ctx, "f.txt", []byte("some content")); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test missing path
	result := tool.Execute(ctx, map[string]interface{}{"old_string": "a", "new_string": "b"})
	if result.Error == "" || !strings.Contains(result.Error, "path is required") {
		t.Fatalf("expected path required error, got %s", result.Error)
	}

	// Test missing old_string
	result = tool.Execute(ctx, map[string]interface{}{"path": "f.txt", "new_string": "b"})
	// Note: Execute doesn't strictly enforce required params from spec if they are missing in map,
	// but GetStringParam returns default empty string, and then we check if it is empty.
	// We need to simulate file read requirement or bypass it for this test?
	// The tool checks "was not read" first.
	// So we need to mock session read.
	// But tool.Execute checks session != nil. If session is nil, it skips read check?
	// Let's check tool code:
	// if t.session != nil && !t.session.WasFileRead(path) { ... }
	// So if session is nil, it skips.

	// However, we need to read the file to check content.
	// The tool reads the file from fs.

	// Let's re-instantiate tool with session to be safe, or just rely on nil session behavior.
	// The tool requires old_string for non-empty files.
	// content of f.txt is "some content".
	// old_string default is "".
	// "old_string is required for non-empty files"

	// But wait, GetStringParam returns "" if missing.
	// So it should fail with "old_string is required"
	if result.Error == "" || !strings.Contains(result.Error, "old_string is required") {
		t.Fatalf("expected old_string required error, got %s", result.Error)
	}
}

func TestWriteFileReplaceSingleTool_FailsIfNotRead(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceSingleTool(mockFS, sess)

	if err := mockFS.WriteFile(ctx, "file.txt", []byte("content")); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	result := tool.Execute(ctx, map[string]interface{}{
		"path":       "file.txt",
		"old_string": "content",
		"new_string": "new",
	})
	if result.Error == "" || !strings.Contains(result.Error, "was not read") {
		t.Fatalf("expected unread error, got %s", result.Error)
	}
}

func TestWriteFileReplaceSingleTool_FailsIfOldStringNotFound(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceSingleTool(mockFS, sess)

	if err := mockFS.WriteFile(ctx, "file.txt", []byte("content")); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", "content")

	result := tool.Execute(ctx, map[string]interface{}{
		"path":       "file.txt",
		"old_string": "missing",
		"new_string": "new",
	})
	if result.Error == "" || !strings.Contains(result.Error, "old_string not found") {
		t.Fatalf("expected not found error, got %s", result.Error)
	}
}

func TestWriteFileReplaceSingleTool_FailsIfCountMismatch(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceSingleTool(mockFS, sess)

	original := "foo\nfoo"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	result := tool.Execute(ctx, map[string]interface{}{
		"path":                  "file.txt",
		"old_string":            "foo",
		"new_string":            "bar",
		"expected_replacements": 1, // There are 2
	})
	if result.Error == "" || !strings.Contains(result.Error, "but expected 1") {
		t.Fatalf("expected mismatch error, got %s", result.Error)
	}
}
