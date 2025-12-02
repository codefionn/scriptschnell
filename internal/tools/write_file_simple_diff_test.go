package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/statcode-ai/scriptschnell/internal/fs"
	"github.com/statcode-ai/scriptschnell/internal/session"
)

func TestWriteFileSimpleDiffTool_Name(t *testing.T) {
	tool := NewWriteFileSimpleDiffTool(nil, nil)
	if tool.Name() != ToolNameWriteFileDiff {
		t.Fatalf("expected name '%s', got '%s'", ToolNameWriteFileDiff, tool.Name())
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

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"diff": diff,
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

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"diff": diff,
	})
	if result.Error != "" {
		t.Fatalf("unexpected error applying diff: %s", result.Error)
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

	result := tool.Execute(context.Background(), map[string]interface{}{"diff": "data"})
	if result.Error == "" || !strings.Contains(result.Error, "path is required") {
		t.Fatalf("expected path required error, got %s", result.Error)
	}

	result = tool.Execute(context.Background(), map[string]interface{}{"path": "file.txt"})
	if result.Error == "" || !strings.Contains(result.Error, "diff is required") {
		t.Fatalf("expected diff required error, got %s", result.Error)
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

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"diff": "--- a/file.txt\n+++ b/file.txt\n-content\n+new content",
	})
	if result.Error == "" || !strings.Contains(result.Error, "was not read") {
		t.Fatalf("expected unread error, got %s", result.Error)
	}
}

func TestApplySimpleDiffHandlesNumberedLines(t *testing.T) {
	original := "alpha\nbeta\n\ngamma\n"
	diff := numberedDiff([]string{
		"--- a/file.txt",
		"+++ b/file.txt",
		" alpha",
		" beta",
		" ",
		"+delta",
		"+epsilon",
		" gamma",
	})

	got, err := applySimpleDiff(original, diff)
	if err != nil {
		t.Fatalf("applySimpleDiff returned error: %v", err)
	}

	want := "alpha\nbeta\n\ndelta\nepsilon\ngamma\n"
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestApplySimpleDiffKeepsDigitLeadingContent(t *testing.T) {
	original := "123 apples\n456 oranges\n"
	diff := `--- a/file.txt
+++ b/file.txt
 123 apples
-456 oranges
+789 kiwis
`

	got, err := applySimpleDiff(original, diff)
	if err != nil {
		t.Fatalf("applySimpleDiff returned error: %v", err)
	}

	want := "123 apples\n789 kiwis\n"
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func numberedDiff(lines []string) string {
	var b strings.Builder
	lineNo := 1
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			b.WriteString(line)
			continue
		}
		fmt.Fprintf(&b, "%2d %s", lineNo, line)
		lineNo++
	}
	return b.String()
}

func TestApplySimpleDiffHandlesNumberedContextWithBlankLines(t *testing.T) {
	original := "}\n\nBoth fields are optional.\n"
	diff := `--- a/file.txt
+++ b/file.txt
 132 }
 133 
 134 Both fields are optional.
+## Build and Run
+- Step One
`

	got, err := applySimpleDiff(original, diff)
	if err != nil {
		t.Fatalf("applySimpleDiff returned error: %v", err)
	}

	want := "}\n\nBoth fields are optional.\n## Build and Run\n- Step One\n"
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestApplySimpleDiffRemovesBlankLinesMarkedWithSpace(t *testing.T) {
	original := "alpha\n\nbeta\n"
	diff := `--- a/file.txt
+++ b/file.txt
 alpha
- 
 beta
`

	got, err := applySimpleDiff(original, diff)
	if err != nil {
		t.Fatalf("applySimpleDiff returned error: %v", err)
	}

	want := "alpha\nbeta\n"
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestApplySimpleDiffRemovesListItemMissingBulletHyphen(t *testing.T) {
	original := "- Multiple providers supported\n- Native search\n"
	diff := `--- a/file.txt
+++ b/file.txt
 - Multiple providers supported
- Native search
+## Build and Run
`

	got, err := applySimpleDiff(original, diff)
	if err != nil {
		t.Fatalf("applySimpleDiff returned error: %v", err)
	}

	want := "- Multiple providers supported\n## Build and Run\n"
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}

func TestApplySimpleDiffStripsGlobalLineNumbers(t *testing.T) {
	original := "alpha\nbeta\ngamma\n"
	diff := `--- a/file.txt
+++ b/file.txt
  10 alpha
  11 -beta
  12 +beta updated
  13 gamma
`

	got, err := applySimpleDiff(original, diff)
	if err != nil {
		t.Fatalf("applySimpleDiff returned error: %v", err)
	}

	want := "alpha\nbeta updated\ngamma\n"
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%q\n\ngot:\n%q", want, got)
	}
}
