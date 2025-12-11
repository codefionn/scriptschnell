package tools

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
)

func TestValidateSyntaxToolSpec_Name(t *testing.T) {
	spec := &ValidateSyntaxToolSpec{}
	if spec.Name() != ToolNameValidateSyntax {
		t.Errorf("expected name %s, got %s", ToolNameValidateSyntax, spec.Name())
	}
}

func TestValidateSyntaxToolSpec_Description(t *testing.T) {
	spec := &ValidateSyntaxToolSpec{}
	desc := spec.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	// Check that description mentions key features
	if !contains(desc, "tree-sitter") {
		t.Error("expected description to mention tree-sitter")
	}
}

func TestValidateSyntaxToolSpec_Parameters(t *testing.T) {
	spec := &ValidateSyntaxToolSpec{}
	params := spec.Parameters()

	// Verify parameters structure
	if params["type"] != "object" {
		t.Error("expected type to be 'object'")
	}

	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	// Check that path, code, and language parameters exist
	if _, ok := properties["path"]; !ok {
		t.Error("expected 'path' parameter")
	}
	if _, ok := properties["code"]; !ok {
		t.Error("expected 'code' parameter")
	}
	if _, ok := properties["language"]; !ok {
		t.Error("expected 'language' parameter")
	}

	// Check oneOf constraint exists
	if _, ok := params["oneOf"]; !ok {
		t.Error("expected 'oneOf' constraint for mutual exclusivity")
	}
}

func TestValidateSyntaxTool_ValidateFile_ValidGo(t *testing.T) {
	mockFS := fs.NewMockFS()
	// Use factory to get properly initialized executor
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	// Write valid Go code
	validGoCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	_ = mockFS.WriteFile(context.Background(), "main.go", []byte(validGoCode))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "main.go",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	valid, ok := resultMap["valid"].(bool)
	if !ok || !valid {
		t.Errorf("expected valid=true, got %v", resultMap["valid"])
	}

	language, ok := resultMap["language"].(string)
	if !ok || language != "go" {
		t.Errorf("expected language='go', got %v", resultMap["language"])
	}

	// Check UI result is formatted
	if result.UIResult == "" {
		t.Error("expected non-empty UI result")
	}
}

func TestValidateSyntaxTool_ValidateFile_InvalidGo(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	// Write invalid Go code (missing closing brace)
	invalidGoCode := `package main

func main() {
	fmt.Println("Hello")
// Missing closing brace
`
	_ = mockFS.WriteFile(context.Background(), "main.go", []byte(invalidGoCode))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "main.go",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	valid, ok := resultMap["valid"].(bool)
	if !ok || valid {
		t.Errorf("expected valid=false, got %v", resultMap["valid"])
	}

	// Check that errors are present
	errorCount, ok := resultMap["error_count"].(int)
	if !ok || errorCount == 0 {
		t.Errorf("expected error_count > 0, got %v", resultMap["error_count"])
	}

	errors, ok := resultMap["errors"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected errors to be a slice of maps")
	}

	if len(errors) == 0 {
		t.Error("expected at least one error")
	}

	// Check error structure
	firstError := errors[0]
	if _, ok := firstError["line"]; !ok {
		t.Error("expected error to have 'line' field")
	}
	if _, ok := firstError["column"]; !ok {
		t.Error("expected error to have 'column' field")
	}
	if _, ok := firstError["message"]; !ok {
		t.Error("expected error to have 'message' field")
	}
	if _, ok := firstError["error_node"]; !ok {
		t.Error("expected error to have 'error_node' field")
	}
}

func TestValidateSyntaxTool_ValidateInlineCode_Valid(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"code":     `print("Hello, World!")`,
		"language": "python",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	valid, ok := resultMap["valid"].(bool)
	if !ok || !valid {
		t.Errorf("expected valid=true, got %v", resultMap["valid"])
	}

	language, ok := resultMap["language"].(string)
	if !ok || language != "python" {
		t.Errorf("expected language='python', got %v", resultMap["language"])
	}
}

func TestValidateSyntaxTool_ValidateInlineCode_Invalid(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"code":     `def hello()\n    print("missing colon")`,
		"language": "python",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	valid, ok := resultMap["valid"].(bool)
	if !ok || valid {
		t.Errorf("expected valid=false, got %v", resultMap["valid"])
	}
}

func TestValidateSyntaxTool_ParameterValidation_BothPathAndCode(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path":     "main.go",
		"code":     "package main",
		"language": "go",
	})

	if result.Error == "" {
		t.Error("expected error when both 'path' and 'code' are specified")
	}

	if !contains(result.Error, "cannot specify both") {
		t.Errorf("expected error message about mutual exclusivity, got: %s", result.Error)
	}
}

func TestValidateSyntaxTool_ParameterValidation_NeitherPathNorCode(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"language": "go",
	})

	if result.Error == "" {
		t.Error("expected error when neither 'path' nor 'code' is specified")
	}

	if !contains(result.Error, "must specify either") {
		t.Errorf("expected error message about required parameters, got: %s", result.Error)
	}
}

func TestValidateSyntaxTool_ParameterValidation_InlineCodeWithoutLanguage(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"code": "package main",
	})

	if result.Error == "" {
		t.Error("expected error when 'code' is specified without 'language'")
	}

	if !contains(result.Error, "language parameter is required") {
		t.Errorf("expected error about missing language, got: %s", result.Error)
	}
}

func TestValidateSyntaxTool_LanguageAutoDetection(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	testCases := []struct {
		filename string
		expected string
	}{
		{"test.go", "go"},
		{"test.py", "python"},
		{"test.ts", "typescript"},
		{"test.js", "javascript"},
		{"test.sh", "bash"},
		{"test.bash", "bash"},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			validCode := "// valid code"
			_ = mockFS.WriteFile(context.Background(), tc.filename, []byte(validCode))

			result := executor.Execute(context.Background(), map[string]interface{}{
				"path": tc.filename,
			})

			if result.Error != "" {
				t.Fatalf("unexpected error: %s", result.Error)
			}

			resultMap, ok := result.Result.(map[string]interface{})
			if !ok {
				t.Fatal("expected result to be a map")
			}

			language, ok := resultMap["language"].(string)
			if !ok || language != tc.expected {
				t.Errorf("expected language='%s', got %v", tc.expected, resultMap["language"])
			}
		})
	}
}

func TestValidateSyntaxTool_LanguageOverride(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	// Create a .txt file with Go code
	goCode := `package main`
	_ = mockFS.WriteFile(context.Background(), "code.txt", []byte(goCode))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path":     "code.txt",
		"language": "go", // Override language detection
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	language, ok := resultMap["language"].(string)
	if !ok || language != "go" {
		t.Errorf("expected language='go', got %v", resultMap["language"])
	}
}

func TestValidateSyntaxTool_UnsupportedLanguage(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"code":     "fn main() {}",
		"language": "rust", // Not supported yet
	})

	if result.Error == "" {
		t.Error("expected error for unsupported language")
	}

	if !contains(result.Error, "not supported") {
		t.Errorf("expected error about unsupported language, got: %s", result.Error)
	}
}

func TestValidateSyntaxTool_UndetectableLanguage(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	// Create a file with unknown extension
	_ = mockFS.WriteFile(context.Background(), "code.unknown", []byte("some code"))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "code.unknown",
	})

	if result.Error == "" {
		t.Error("expected error for undetectable language")
	}

	if !contains(result.Error, "cannot detect language") {
		t.Errorf("expected error about language detection, got: %s", result.Error)
	}
}

func TestValidateSyntaxTool_FileNotFound(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "nonexistent.go",
	})

	if result.Error == "" {
		t.Error("expected error for nonexistent file")
	}

	if !contains(result.Error, "failed to read file") {
		t.Errorf("expected error about file reading, got: %s", result.Error)
	}
}

func TestValidateSyntaxTool_MultipleLanguages(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	testCases := []struct {
		name     string
		language string
		code     string
		valid    bool
	}{
		{
			name:     "Valid TypeScript",
			language: "typescript",
			code:     `const x: number = 42;`,
			valid:    true,
		},
		{
			name:     "Valid JavaScript",
			language: "javascript",
			code:     `const x = 42;`,
			valid:    true,
		},
		{
			name:     "Valid Bash",
			language: "bash",
			code:     `echo "hello"`,
			valid:    true,
		},
		{
			name:     "Invalid TypeScript",
			language: "typescript",
			code:     `const x = ;`,
			valid:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := executor.Execute(context.Background(), map[string]interface{}{
				"code":     tc.code,
				"language": tc.language,
			})

			if result.Error != "" {
				t.Fatalf("unexpected error: %s", result.Error)
			}

			resultMap, ok := result.Result.(map[string]interface{})
			if !ok {
				t.Fatal("expected result to be a map")
			}

			valid, ok := resultMap["valid"].(bool)
			if !ok {
				t.Fatal("expected 'valid' field in result")
			}

			if valid != tc.valid {
				t.Errorf("expected valid=%v, got %v", tc.valid, valid)
			}
		})
	}
}

func TestValidateSyntaxTool_UIResultFormat(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	// Test with invalid code to get error output
	invalidCode := `package main

func main() {
	x := 5
	y :=
}`
	_ = mockFS.WriteFile(context.Background(), "test.go", []byte(invalidCode))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "test.go",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// Check UI result contains expected formatting
	uiResult, ok := result.UIResult.(string)
	if !ok || uiResult == "" {
		t.Fatal("expected non-empty string UI result")
	}

	// Check for markdown formatting elements
	if !contains(uiResult, "Syntax Validation") {
		t.Error("expected UI result to contain 'Syntax Validation'")
	}
	if !contains(uiResult, "Language:") {
		t.Error("expected UI result to contain 'Language:'")
	}
	if !contains(uiResult, "Validation Failed") {
		t.Error("expected UI result to contain 'Validation Failed'")
	}
}

func TestValidateSyntaxTool_EmptyFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	// Create empty file
	_ = mockFS.WriteFile(context.Background(), "empty.go", []byte(""))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "empty.go",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	// Empty files should be considered valid
	valid, ok := resultMap["valid"].(bool)
	if !ok || !valid {
		t.Errorf("expected empty file to be valid, got %v", resultMap["valid"])
	}

	parsedBytes, ok := resultMap["parsed_bytes"].(int)
	if !ok || parsedBytes != 0 {
		t.Errorf("expected parsed_bytes=0 for empty file, got %v", resultMap["parsed_bytes"])
	}
}

func TestValidateSyntaxTool_ParsedBytesCount(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewValidateSyntaxToolFactory(mockFS)
	executor := factory(nil).(*ValidateSyntaxToolExecutor)

	code := `package main

func main() {
}
`
	expectedBytes := len(code)

	result := executor.Execute(context.Background(), map[string]interface{}{
		"code":     code,
		"language": "go",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	parsedBytes, ok := resultMap["parsed_bytes"].(int)
	if !ok || parsedBytes != expectedBytes {
		t.Errorf("expected parsed_bytes=%d, got %v", expectedBytes, resultMap["parsed_bytes"])
	}
}
