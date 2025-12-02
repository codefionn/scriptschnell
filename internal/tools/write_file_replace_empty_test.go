package tools

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestWriteFileReplaceTool_EmptyFile(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceTool(mockFS, sess)

	// Create an empty file
	if err := mockFS.WriteFile(ctx, "empty.txt", []byte("")); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}
	sess.TrackFileRead("empty.txt", "")

	// Test 1: Empty file with non-empty old_string (old_string is ignored)
	newContent := "This is the new content"
	result := tool.Execute(ctx, map[string]interface{}{
		"path":       "empty.txt",
		"old_string": "irrelevant_old_string",
		"new_string": newContent,
	})

	if result.Error != "" {
		t.Fatalf("unexpected error on empty file replace: %v", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result.Result)
	}
	if updated, _ := resultMap["updated"].(bool); !updated {
		t.Fatalf("expected updated=true")
	}

	// Verify content
	data, err := mockFS.ReadFile(ctx, "empty.txt")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != newContent {
		t.Fatalf("expected file content '%s', got '%s'", newContent, string(data))
	}
}

func TestWriteFileReplaceTool_EmptyFileEmptyOldString(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileReplaceTool(mockFS, sess)

	// Create an empty file
	if err := mockFS.WriteFile(ctx, "empty2.txt", []byte("")); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}
	sess.TrackFileRead("empty2.txt", "")

	// Test 2: Empty file with empty old_string (this is the bug fix)
	newContent := "New content from empty old_string"
	result := tool.Execute(ctx, map[string]interface{}{
		"path":       "empty2.txt",
		"old_string": "", // Empty old_string should work for empty files
		"new_string": newContent,
	})

	if result.Error != "" {
		t.Fatalf("unexpected error on empty file with empty old_string: %v", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result.Result)
	}
	if updated, _ := resultMap["updated"].(bool); !updated {
		t.Fatalf("expected updated=true")
	}

	// Verify content
	data, err := mockFS.ReadFile(ctx, "empty2.txt")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != newContent {
		t.Fatalf("expected file content '%s', got '%s'", newContent, string(data))
	}
}
