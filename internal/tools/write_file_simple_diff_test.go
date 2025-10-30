package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

func TestWriteFileSimpleDiffTool_Name(t *testing.T) {
	tool := NewWriteFileSimpleDiffTool(nil, nil)
	if tool.Name() != "write_file_diff" {
		t.Fatalf("expected name 'write_file_diff', got '%s'", tool.Name())
	}
}

func TestWriteFileSimpleDiffTool_Parameters(t *testing.T) {
	tool := NewWriteFileSimpleDiffTool(nil, nil)
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
	if _, ok := properties["diff"]; !ok {
		t.Fatalf("expected diff property")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("expected required slice")
	}
	if len(required) != 2 || required[0] != "path" || required[1] != "diff" {
		t.Fatalf("expected required [path diff], got %v", required)
	}
}

func TestWriteFileSimpleDiffTool_AppliesDiffWithoutHunks(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileSimpleDiffTool(mockFS, sess)

	original := "line 1\nline 2\nline 3"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	diff := `--- a/file.txt
+++ b/file.txt
 line 1
-line 2
+line 2 updated
 line 3`

	result, err := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"diff": diff,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if updated, _ := resultMap["updated"].(bool); !updated {
		t.Fatalf("expected updated=true, got %v", resultMap["updated"])
	}

	data, err := mockFS.ReadFile(ctx, "file.txt")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "line 1\nline 2 updated\nline 3" {
		t.Fatalf("unexpected file content: %s", data)
	}
}

func TestWriteFileSimpleDiffTool_AddsLinesAtEnd(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileSimpleDiffTool(mockFS, sess)

	original := "alpha\nbeta"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	diff := `--- a/file.txt
+++ b/file.txt
 alpha
 beta
+gamma`

	if _, err := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"diff": diff,
	}); err != nil {
		t.Fatalf("unexpected error applying diff: %v", err)
	}

	data, err := mockFS.ReadFile(ctx, "file.txt")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "alpha\nbeta\ngamma" {
		t.Fatalf("unexpected file content: %s", data)
	}
}

func TestWriteFileSimpleDiffTool_RequiresPathAndDiff(t *testing.T) {
	tool := NewWriteFileSimpleDiffTool(fs.NewMockFS(), nil)

	if _, err := tool.Execute(context.Background(), map[string]interface{}{"diff": "data"}); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected path required error, got %v", err)
	}

	if _, err := tool.Execute(context.Background(), map[string]interface{}{"path": "file.txt"}); err == nil || !strings.Contains(err.Error(), "diff is required") {
		t.Fatalf("expected diff required error, got %v", err)
	}
}

func TestWriteFileSimpleDiffTool_FailsIfNotRead(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileSimpleDiffTool(mockFS, sess)

	if err := mockFS.WriteFile(ctx, "file.txt", []byte("content")); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	_, err := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"diff": "--- a/file.txt\n+++ b/file.txt\n-content\n+new content",
	})
	if err == nil || !strings.Contains(err.Error(), "was not read") {
		t.Fatalf("expected unread error, got %v", err)
	}
}
