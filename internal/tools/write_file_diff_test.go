package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestWriteFileDiffTool_Name(t *testing.T) {
	tool := NewWriteFileDiffTool(nil, nil)
	if tool.Name() != ToolNameEditFile {
		t.Fatalf("expected name '%s', got '%s'", ToolNameEditFile, tool.Name())
	}
}

func TestWriteFileDiffTool_Parameters(t *testing.T) {
	tool := NewWriteFileDiffTool(nil, nil)
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

func TestWriteFileDiffTool_AppliesDiff(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileDiffTool(mockFS, sess)

	original := "line 1\nline 2\nline 3"
	if err := mockFS.WriteFile(ctx, "file.txt", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.txt", original)

	diff := `@@ -1,3 +1,3 @@
-line 1
+line 1 modified
 line 2
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
	if string(data) != "line 1 modified\nline 2\nline 3" {
		t.Fatalf("unexpected file content: %s", data)
	}

	modified := sess.GetModifiedFiles()
	if len(modified) != 1 || modified[0] != "file.txt" {
		t.Fatalf("expected file.txt to be tracked as modified, got %v", modified)
	}
}

func TestWriteFileDiffTool_RequiresPathAndDiff(t *testing.T) {
	tool := NewWriteFileDiffTool(fs.NewMockFS(), nil)

	result := tool.Execute(context.Background(), map[string]interface{}{"diff": "data"})
	if result.Error == "" || !strings.Contains(result.Error, "path is required") {
		t.Fatalf("expected path required error, got %s", result.Error)
	}

	result = tool.Execute(context.Background(), map[string]interface{}{"path": "file.txt"})
	if result.Error == "" || !strings.Contains(result.Error, "diff is required") {
		t.Fatalf("expected diff required error, got %s", result.Error)
	}
}

func TestWriteFileDiffTool_FailsIfNotRead(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileDiffTool(mockFS, sess)

	if err := mockFS.WriteFile(ctx, "file.txt", []byte("content")); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.txt",
		"diff": "@@ -1 +1 @@\n-content\n+new content",
	})
	if result.Error == "" || !strings.Contains(result.Error, "was not read") {
		t.Fatalf("expected unread error, got %s", result.Error)
	}
}

func TestWriteFileDiffTool_FailsIfMissingFile(t *testing.T) {
	tool := NewWriteFileDiffTool(fs.NewMockFS(), nil)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "missing.txt",
		"diff": "@@ -0,0 +1 @@\n+hello",
	})
	if result.Error == "" || !strings.Contains(result.Error, "cannot apply diff to non-existent file") {
		t.Fatalf("expected missing file error, got %s", result.Error)
	}
}

func TestWriteFileDiffTool_SyntaxValidationWithInvalidGo(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileDiffTool(mockFS, sess)

	original := `package main

func main() {
	println("Hello")
}`

	if err := mockFS.WriteFile(ctx, "file.go", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.go", original)

	// Apply a diff that creates invalid Go syntax (missing closing parenthesis)
	diff := `@@ -1,4 +1,4 @@
 package main
 
-func main() {
+func main( {
 	println("Hello")
 }`

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.go",
		"diff": diff,
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got type %T", result.Result)
	}

	// Check that validation warning was included
	_, hasWarning := resultMap["validation_warning"]
	if !hasWarning {
		t.Fatal("expected validation_warning in result for invalid Go code")
	}

	// Check UI result contains warning
	uiResult, ok := result.UIResult.(string)
	if !ok {
		t.Fatal("expected UIResult to be a string")
	}

	if !strings.Contains(uiResult, "Syntax Validation") {
		t.Errorf("expected UIResult to contain 'Syntax Validation'")
	}
}

func TestWriteFileDiffTool_SyntaxValidationWithValidGo(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileDiffTool(mockFS, sess)

	original := `package main

func main() {
	println("Hello")
}`

	if err := mockFS.WriteFile(ctx, "file.go", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.go", original)

	// Apply a diff that creates valid Go syntax
	diff := `@@ -1,4 +1,5 @@
 package main
 
 func main() {
+	fmt.Println("Updated")
 	println("Hello")
 }`

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.go",
		"diff": diff,
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got type %T", result.Result)
	}

	// Check that no validation warning was included
	if _, hasWarning := resultMap["validation_warning"]; hasWarning {
		t.Fatal("expected no validation_warning in result for valid Go code")
	}
}

func TestWriteFileDiffTool_SyntaxValidationContextDisplay(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	tool := NewWriteFileDiffTool(mockFS, sess)

	original := `package main

func main() {
	println("Hello")
}`

	if err := mockFS.WriteFile(ctx, "file.go", []byte(original)); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}
	sess.TrackFileRead("file.go", original)

	// Apply a diff that creates syntax error with context
	diff := `@@ -1,4 +1,8 @@
 package main
 
+// New function
+func helper( {  // Missing closing parenthesis
+	return
+}
+
 func main() {
 	println("Hello")
 }`

	result := tool.Execute(ctx, map[string]interface{}{
		"path": "file.go",
		"diff": diff,
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got type %T", result.Result)
	}

	// Check that validation warning was included
	warning, hasWarning := resultMap["validation_warning"]
	if !hasWarning {
		t.Fatal("expected validation_warning in result")
	}

	warningStr, ok := warning.(string)
	if !ok {
		t.Fatal("expected validation_warning to be a string")
	}

	// Check that context lines are included in the warning
	if !strings.Contains(warningStr, ":") {
		t.Error("expected line numbers in validation warning")
	}
}
