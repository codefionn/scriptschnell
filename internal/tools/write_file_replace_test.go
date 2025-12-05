package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestWriteFileReplaceTool_Name(t *testing.T) {
	tool := NewWriteFileReplaceTool(nil, nil)
	if tool.Name() != ToolNameEditFile {
		t.Fatalf("expected name '%s', got '%s'", ToolNameEditFile, tool.Name())
	}
}

func TestWriteFileReplaceTool_Parameters(t *testing.T) {
	tool := NewWriteFileReplaceTool(nil, nil)
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
	if _, ok := properties["edits"]; !ok {
		t.Fatalf("expected edits property")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("expected required slice")
	}

	if len(required) != 2 || required[0] != "path" || required[1] != "edits" {
		t.Fatalf("expected required [path edits], got %v", required)
	}
}

func TestWriteFileReplaceTool_AppliesReplacement(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceTool(mockFS, sess)

	original := "line 1\nline 2\nline 3"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"edits": []map[string]interface{}{
			{"old_string": "line 2", "new_string": "line 2 modified"},
		},
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
	if editsApplied, _ := resultMap["edits_applied"].(int); editsApplied != 1 {
		t.Fatalf("expected edits_applied=1, got %v", resultMap["edits_applied"])
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

func TestWriteFileReplaceTool_AppliesMultipleEdits(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceTool(mockFS, sess)

	original := "alpha\nbeta\ngamma"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"edits": []map[string]interface{}{
			{"old_string": "alpha", "new_string": "ALPHA"},
			{"old_string": "gamma", "new_string": "GAMMA"},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := mockFS.ReadFile(ctx, "file.txt")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	expected := "ALPHA\nbeta\nGAMMA"
	if string(data) != expected {
		t.Fatalf("unexpected file content: %s", data)
	}

	if resultMap, ok := result.Result.(map[string]interface{}); ok {
		if applied, _ := resultMap["edits_applied"].(int); applied != 2 {
			t.Fatalf("expected edits_applied=2, got %v", resultMap["edits_applied"])
		}
		if reps, _ := resultMap["replacements"].(int); reps != 2 {
			t.Fatalf("expected replacements=2, got %v", resultMap["replacements"])
		}
	}

	modified := sess.GetModifiedFiles()
	if len(modified) != 1 || modified[0] != "file.txt" {
		t.Fatalf("expected file.txt to be tracked as modified, got %v", modified)
	}
}

func TestWriteFileReplaceTool_RequiresFields(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	tool := NewWriteFileReplaceTool(mockFS, nil)

	// Test missing path
	result := tool.Execute(ctx, map[string]interface{}{"edits": []map[string]interface{}{{"old_string": "a", "new_string": "b"}}})
	if result.Error == "" || !strings.Contains(result.Error, "path is required") {
		t.Fatalf("expected path required error, got %s", result.Error)
	}

	// For old_string validation, we need a non-empty file to exist
	if err := mockFS.WriteFile(ctx, "f.txt", []byte("some content")); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test missing edits
	result = tool.Execute(ctx, map[string]interface{}{"path": "f.txt"})
	if result.Error == "" || !strings.Contains(result.Error, "edits is required") {
		t.Fatalf("expected edits required error, got %s", result.Error)
	}
}

func TestWriteFileReplaceTool_FailsIfNotRead(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceTool(mockFS, sess)

	if err := mockFS.WriteFile(ctx, "file.txt", []byte("content")); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"edits": []map[string]interface{}{
			{"old_string": "content", "new_string": "new"},
		},
	})
	if result.Error == "" || !strings.Contains(result.Error, "was not read") {
		t.Fatalf("expected unread error, got %s", result.Error)
	}
}

func TestWriteFileReplaceTool_FailsIfOldStringNotFound(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceTool(mockFS, sess)

	if err := mockFS.WriteFile(ctx, "file.txt", []byte("content")); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", "content")

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"edits": []map[string]interface{}{
			{"old_string": "missing", "new_string": "new"},
		},
	})
	if result.Error == "" || !strings.Contains(result.Error, "old_string not found") {
		t.Fatalf("expected not found error, got %s", result.Error)
	}
}

func TestWriteFileReplaceTool_FailsIfCountMismatch(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceTool(mockFS, sess)

	original := "foo\nfoo"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"edits": []map[string]interface{}{
			{"old_string": "foo", "new_string": "bar", "expected_replacements": 1}, // There are 2
		},
	})
	if result.Error == "" || !strings.Contains(result.Error, "but expected 1") {
		t.Fatalf("expected mismatch error, got %s", result.Error)
	}
}
