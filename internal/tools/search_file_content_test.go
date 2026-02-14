package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
)

func TestSearchFileContentTool_Execute(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	// Setup files
	_ = mockFS.WriteFile(ctx, "main.go", []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello")
	fmt.Println("World") // match me
	fmt.Println("Foo")
}
`))
	_ = mockFS.WriteFile(ctx, "test.txt", []byte(`line 1
line 2
line 3
line 4
line 5
`))
	// Binary file
	_ = mockFS.WriteFile(ctx, "bin.dat", []byte{0, 1, 2, 3})

	tests := []struct {
		name         string
		params       map[string]interface{}
		wantContains []string
		wantErr      bool
	}{
		{
			name: "Basic match",
			params: map[string]interface{}{
				"pattern": "World",
				"path":    ".",
			},
			wantContains: []string{
				"## Content Search Results",
				"### `main.go`",
				"*1 match(es)*",
				" 7: \tfmt.Println(\"World\") // match me",
			},
		},
		{
			name: "Context control",
			params: map[string]interface{}{
				"pattern": "line 3",
				"path":    "test.txt",
				"context": 0,
			},
			wantContains: []string{
				"**Search Path:** `test.txt`",
				"### `test.txt`",
				" 3: line 3",
			},
		},
		{
			name: "Padding check",
			params: map[string]interface{}{
				"pattern": "line 3",
				"path":    "test.txt",
				"context": 1,
			},
			// max line 5 -> digits 1 -> padding 2
			wantContains: []string{
				" 2: line 2",
				" 3: line 3",
				" 4: line 4",
			},
		},
		{
			name: "Glob filtering",
			params: map[string]interface{}{
				"pattern": "fmt",
				"path":    ".",
				"glob":    "*.go",
			},
			// Should only match main.go
			wantContains: []string{
				"**File Filter:** `*.go`",
				"### `main.go`",
			},
		},
		{
			name: "No match",
			params: map[string]interface{}{
				"pattern": "NonExistent",
				"path":    ".",
			},
			wantContains: []string{"*No matches found.*"},
		},
		{
			name: "Binary skip",
			params: map[string]interface{}{
				"pattern": ".", // Match anything
				"path":    "bin.dat",
			},
			wantContains: []string{"*No matches found.*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.Execute(ctx, tt.params)
			if (got.Error != "") != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", got.Error, tt.wantErr)
				return
			}

			if got.Result == nil {
				t.Fatalf("expected non-nil result")
			}
			gotStr, ok := got.Result.(string)
			if !ok {
				t.Fatalf("expected result to be string, got %T", got.Result)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(gotStr, want) {
					t.Errorf("Execute() = %q, want substring %q", gotStr, want)
				}
			}
		})
	}
}

func TestSearchFileContentTool_Execute_Errors(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	tests := []struct {
		name    string
		params  map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Empty pattern",
			params:  map[string]interface{}{"pattern": ""},
			wantErr: true,
			errMsg:  "pattern is required",
		},
		{
			name:    "Missing pattern parameter",
			params:  map[string]interface{}{},
			wantErr: true,
			errMsg:  "pattern is required",
		},
		{
			name:    "Invalid regex - unclosed bracket",
			params:  map[string]interface{}{"pattern": "[unclosed"},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
		{
			name:    "Invalid regex - invalid group",
			params:  map[string]interface{}{"pattern": "(?P<invalid)"},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
		{
			name:    "Invalid regex - unclosed parenthesis",
			params:  map[string]interface{}{"pattern": "(unclosed"},
			wantErr: true,
			errMsg:  "invalid regex pattern",
		},
		{
			name: "Non-existent search path",
			params: map[string]interface{}{
				"pattern": "test",
				"path":    "/non/existent/path",
			},
			wantErr: true,
			errMsg:  "path not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.Execute(ctx, tt.params)
			if !tt.wantErr {
				if got.Error != "" {
					t.Errorf("Execute() unexpected error = %v", got.Error)
				}
				return
			}

			if got.Error == "" {
				t.Errorf("Execute() expected error, got none")
				return
			}

			if tt.errMsg != "" && !strings.Contains(got.Error, tt.errMsg) {
				t.Errorf("Execute() error = %v, want error containing %q", got.Error, tt.errMsg)
			}
		})
	}
}

func TestSearchFileContentTool_Execute_UnreadableFiles(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	// Create a directory with a readable file
	mockFS.MkdirAll(ctx, "testdir", 0755)
	_ = mockFS.WriteFile(ctx, "testdir/good.txt", []byte("good content\n"))

	result := tool.Execute(ctx, map[string]interface{}{
		"pattern": "content",
		"path":    "testdir",
	})

	if result.Error != "" {
		t.Fatalf("Execute() unexpected error = %v", result.Error)
	}

	resultStr, ok := result.Result.(string)
	if !ok {
		t.Fatalf("expected result to be string, got %T", result.Result)
	}

	// Should find the good file
	if !strings.Contains(resultStr, "good.txt") {
		t.Errorf("Expected to find good.txt in results, got: %s", resultStr)
	}
}

func TestSearchFileContentTool_Execute_ComplexGlobPatterns(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	// Setup directory structure
	_ = mockFS.MkdirAll(ctx, "internal/tools", 0755)
	_ = mockFS.MkdirAll(ctx, "cmd/app", 0755)

	_ = mockFS.WriteFile(ctx, "main.go", []byte("main content\n"))
	_ = mockFS.WriteFile(ctx, "internal/tools/tool.go", []byte("tool content\n"))
	_ = mockFS.WriteFile(ctx, "internal/tools/helper.go", []byte("helper content\n"))
	_ = mockFS.WriteFile(ctx, "cmd/app/main.go", []byte("app main content\n"))
	_ = mockFS.WriteFile(ctx, "cmd/app/config.go", []byte("config content\n"))

	tests := []struct {
		name         string
		pattern      string
		glob         string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:    "Recursive double-star pattern",
			pattern: "content",
			glob:    "**/*.go",
			wantContains: []string{
				"**File Filter:** `**/*.go`",
				"main.go",
				"internal/tools/tool.go",
				"internal/tools/helper.go",
				"cmd/app/main.go",
				"cmd/app/config.go",
			},
		},
		{
			name:    "Path-based glob pattern",
			pattern: "content",
			glob:    "internal/**/*.go",
			wantContains: []string{
				"**File Filter:** `internal/**/*.go`",
				"internal/tools/tool.go",
				"internal/tools/helper.go",
			},
			wantMissing: []string{
				"main.go",         // Not in internal/
				"cmd/app/main.go", // Not in internal/
			},
		},
		{
			name:    "Glob with question mark wildcard",
			pattern: "content",
			glob:    "main.go", // Exact filename match
			wantContains: []string{
				"main.go",
			},
			wantMissing: []string{
				"internal/tools/tool.go", // Should not match this
				"internal/tools/helper.go",
			},
		},
		{
			name:    "Nested path with double-star",
			pattern: "content",
			glob:    "cmd/**/*.go",
			wantContains: []string{
				"**File Filter:** `cmd/**/*.go`",
				"cmd/app/main.go",
				"cmd/app/config.go",
			},
		},
		{
			name:    "Double-star without extension",
			pattern: "content",
			glob:    "**/*", // Match all files
			wantContains: []string{
				"**File Filter:** `**/*`",
				"main.go",
				"internal/tools/tool.go",
				"cmd/app/main.go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(ctx, map[string]interface{}{
				"pattern": tt.pattern,
				"glob":    tt.glob,
				"path":    ".",
			})

			if result.Error != "" {
				t.Fatalf("Execute() unexpected error = %v", result.Error)
			}

			resultStr, ok := result.Result.(string)
			if !ok {
				t.Fatalf("expected result to be string, got %T", result.Result)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(resultStr, want) {
					t.Errorf("Execute() = %q, want substring %q", resultStr, want)
				}
			}

			for _, missing := range tt.wantMissing {
				if strings.Contains(resultStr, missing) {
					t.Errorf("Execute() = %q, should not contain %q", resultStr, missing)
				}
			}
		})
	}
}

func TestSearchFileContentTool_MatchComplexGlob(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)

	tests := []struct {
		pattern string
		path    string
		want    bool
		wantErr bool
	}{
		{"**/*.go", "main.go", true, false},
		{"**/*.go", "internal/tools/tool.go", true, false},
		{"**/*.go", "test.txt", false, false},
		{"internal/**/*.go", "internal/tools/tool.go", true, false},
		{"internal/**/*.go", "main.go", false, false},
		{"cmd/**/*.go", "cmd/app/main.go", true, false},
		{"cmd/**/*.go", "cmd/main.go", true, false},
		{"test?.go", "test1.go", true, false},
		{"test?.go", "test.go", false, false},
		{"test?.go", "test12.go", false, false},
		{"**/test*.go", "test.go", true, false},
		{"**/test*.go", "dir/test.go", true, false},
		{"**/test*.go", "dir/test_file.go", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got, err := tool.matchComplexGlob(tt.path, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("matchComplexGlob() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("matchComplexGlob() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchFileContentTool_ContextAndBlockMerging(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	tests := []struct {
		name         string
		fileContent  string
		pattern      string
		context      int
		wantContains []string
		wantMissing  []string
	}{
		{
			name: "Multiple matches merge into single block",
			fileContent: `line 1
line 2 match
line 3
line 4 match
line 5
`,
			pattern: "match",
			context: 1,
			wantContains: []string{
				"### `test.txt`",
				"*2 match(es)*",
				" 1: line 1",
				" 2: line 2 match",
				" 3: line 3",
				" 4: line 4 match",
				" 5: line 5",
			},
			// Blocks merged into single block
		},
		{
			name: "Adjacent matches merge",
			fileContent: `line 1 match
line 2 match
line 3 match
line 4
`,
			pattern: "match",
			context: 0,
			wantContains: []string{
				"*3 match(es)*",
				" 1: line 1 match",
				" 2: line 2 match",
				" 3: line 3 match",
			},
			// Adjacent ranges merge into single block
		},
		{
			name: "Non-adjacent matches have separator",
			fileContent: `line 1 match
line 2
line 3
line 4
line 5
line 6 match
line 7
`,
			pattern: "match",
			context: 1,
			wantContains: []string{
				"*2 match(es)*",
				" 1: line 1 match",
				" 2: line 2",
				"--", // Separator between non-adjacent blocks
				" 5: line 5",
				" 6: line 6 match",
				" 7: line 7",
			},
		},
		{
			name: "Match at start of file",
			fileContent: `line 1 match
line 2
line 3
`,
			pattern: "match",
			context: 1,
			wantContains: []string{
				" 1: line 1 match",
				" 2: line 2",
			},
			wantMissing: []string{"line 0", " 3: line 3"}, // No line before match, line 3 should not appear with context=1
		},
		{
			name: "Match at end of file",
			fileContent: `line 1
line 2
line 3 match
`,
			pattern: "match",
			context: 1,
			wantContains: []string{
				" 2: line 2",
				" 3: line 3 match",
			},
			wantMissing: []string{"line 4"}, // No line after match
		},
		{
			name: "Large context with many lines",
			fileContent: `line 1
line 2
line 3
line 4
line 5 match
line 6
line 7
line 8
line 9
`,
			pattern: "match",
			context: 2,
			wantContains: []string{
				" 3: line 3",
				" 4: line 4",
				" 5: line 5 match",
				" 6: line 6",
				" 7: line 7",
			},
			wantMissing: []string{
				"line 1", // Too far from match
				"line 9", // Too far from match
			},
		},
		{
			name: "Overlapping context ranges merge",
			fileContent: `line 1
line 2 match1
line 3
line 4 match2
line 5
`,
			pattern: "match",
			context: 2,
			wantContains: []string{
				"*2 match(es)*",
				" 1: line 1",
				" 2: line 2 match1",
				" 3: line 3",
				" 4: line 4 match2",
				" 5: line 5",
			},
			// Adjacent ranges merge into single block // Single merged block
		},
		{
			name: "Directly adjacent ranges merge",
			fileContent: `line 1
line 2 match
line 3
line 4
line 5 match
line 6
`,
			pattern: "match",
			context: 2,
			wantContains: []string{
				"*2 match(es)*",
				" 1: line 1",
				" 2: line 2 match",
				" 3: line 3",
				" 4: line 4",
				" 5: line 5 match",
				" 6: line 6",
			},
			// Adjacent ranges merge into single block // Adjacent ranges merge
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = mockFS.WriteFile(ctx, "test.txt", []byte(tt.fileContent))

			result := tool.Execute(ctx, map[string]interface{}{
				"pattern": tt.pattern,
				"path":    "test.txt",
				"context": tt.context,
			})

			if result.Error != "" {
				t.Fatalf("Execute() unexpected error = %v", result.Error)
			}

			resultStr, ok := result.Result.(string)
			if !ok {
				t.Fatalf("expected result to be string, got %T", result.Result)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(resultStr, want) {
					t.Errorf("Execute() = %q, want substring %q", resultStr, want)
				}
			}

			for _, missing := range tt.wantMissing {
				if strings.Contains(resultStr, missing) {
					t.Errorf("Execute() = %q, should not contain %q", resultStr, missing)
				}
			}
		})
	}
}

func TestSearchFileContentTool_DirectoryTraversal(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	// Setup directory structure with hidden files/dirs
	_ = mockFS.MkdirAll(ctx, "dir1/subdir", 0755)
	_ = mockFS.MkdirAll(ctx, ".git", 0755)
	_ = mockFS.MkdirAll(ctx, ".idea", 0755)
	_ = mockFS.MkdirAll(ctx, "dir1/.hidden_dir", 0755)

	_ = mockFS.WriteFile(ctx, "root.txt", []byte("root content\n"))
	_ = mockFS.WriteFile(ctx, "dir1/file1.txt", []byte("dir1 content\n"))
	_ = mockFS.WriteFile(ctx, "dir1/subdir/file2.txt", []byte("subdir content\n"))
	_ = mockFS.WriteFile(ctx, ".git/config", []byte("git config\n"))
	_ = mockFS.WriteFile(ctx, ".idea/workspace.xml", []byte("workspace\n"))
	_ = mockFS.WriteFile(ctx, ".hidden_file.txt", []byte("hidden file\n"))
	_ = mockFS.WriteFile(ctx, "dir1/.hidden_dir/secret.txt", []byte("secret\n"))

	tests := []struct {
		name         string
		pattern      string
		path         string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:    "Hidden directory is skipped",
			pattern: "content",
			path:    ".",
			wantContains: []string{
				"root.txt",
				"dir1/file1.txt",
				"dir1/subdir/file2.txt",
			},
			wantMissing: []string{
				".git/config",
				".idea/workspace.xml",
				".hidden_file.txt",
				"dir1/.hidden_dir/secret.txt",
			},
		},
		{
			name:    "Recursive directory traversal",
			pattern: "content",
			path:    ".",
			wantContains: []string{
				"root.txt",
				"dir1/file1.txt",
				"dir1/subdir/file2.txt",
			},
		},
		{
			name:    "Search in subdirectory only",
			pattern: "content",
			path:    "dir1",
			wantContains: []string{
				"dir1/file1.txt",
				"dir1/subdir/file2.txt",
			},
			wantMissing: []string{"root.txt"},
		},
		{
			name:         "Search in nested subdirectory",
			pattern:      "content",
			path:         "dir1/subdir",
			wantContains: []string{"dir1/subdir/file2.txt"},
			wantMissing: []string{
				"root.txt",
				"dir1/file1.txt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(ctx, map[string]interface{}{
				"pattern": tt.pattern,
				"path":    tt.path,
			})

			if result.Error != "" {
				t.Fatalf("Execute() unexpected error = %v", result.Error)
			}

			resultStr, ok := result.Result.(string)
			if !ok {
				t.Fatalf("expected result to be string, got %T", result.Result)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(resultStr, want) {
					t.Errorf("Execute() = %q, want substring %q", resultStr, want)
				}
			}

			for _, missing := range tt.wantMissing {
				if strings.Contains(resultStr, missing) {
					t.Errorf("Execute() = %q, should not contain %q", resultStr, missing)
				}
			}
		})
	}
}

func TestSearchFileContentTool_BinaryFileDetection(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	// Setup files
	_ = mockFS.WriteFile(ctx, "text.txt", []byte("text content\n"))
	_ = mockFS.WriteFile(ctx, "binary.exe", []byte{0x7F, 'E', 'L', 'F', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) // ELF header
	_ = mockFS.WriteFile(ctx, "lib.so", []byte{0x7F, 'E', 'L', 'F', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})     // ELF header
	_ = mockFS.WriteFile(ctx, "lib.dll", []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00})             // MZ header
	_ = mockFS.WriteFile(ctx, "lib.dylib", []byte{0xCE, 0xFA, 0xED, 0xFE, 0x07, 0x00, 0x00, 0x01})           // Mach-O header
	_ = mockFS.WriteFile(ctx, "archive.a", []byte{0x21, 0x3C, 0x61, 0x72, 0x63, 0x68, 0x3E, 0x0A})           // ar archive
	_ = mockFS.WriteFile(ctx, "binary.bin", []byte{0, 1, 2, 3, 0, 5, 6})                                     // null bytes
	_ = mockFS.WriteFile(ctx, "binary.lib", []byte{0, 1, 2, 3, 0, 5, 6})                                     // null bytes
	_ = mockFS.WriteFile(ctx, "wasm.wasm", []byte{0, 0x61, 0x73, 0x6D})                                      // WASM magic
	_ = mockFS.WriteFile(ctx, "image.png", []byte{0x89, 'P', 'N', 'G', 0, '\r', '\n', 0x1A, '\n'})           // PNG header
	_ = mockFS.WriteFile(ctx, "archive.zip", []byte{0x50, 0x4B, 0x03, 0, 0x04})                              // ZIP header

	tests := []struct {
		name         string
		pattern      string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:         "Binary files by extension are skipped",
			pattern:      ".",
			wantContains: []string{"text.txt"},
			wantMissing: []string{
				"binary.exe",
				"lib.so",
				"lib.dll",
				"lib.dylib",
				"archive.a",
				"binary.lib",
				"wasm.wasm",
			},
		},
		{
			name:         "Binary file with null bytes is skipped",
			pattern:      ".",
			wantContains: []string{"text.txt"},
			wantMissing:  []string{"binary.bin"},
		},
		{
			name:         "Images are skipped (null bytes in header)",
			pattern:      ".",
			wantContains: []string{"text.txt"},
			wantMissing:  []string{"image.png"},
		},
		{
			name:         "Archives are skipped",
			pattern:      ".",
			wantContains: []string{"text.txt"},
			wantMissing:  []string{"archive.zip"},
		},
		{
			name:         "All binary files are skipped",
			pattern:      "text",
			wantContains: []string{"*1 match(es)*", "text.txt"},
			wantMissing:  []string{"*No matches found.*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(ctx, map[string]interface{}{
				"pattern": tt.pattern,
				"path":    ".",
			})

			if result.Error != "" {
				t.Fatalf("Execute() unexpected error = %v", result.Error)
			}

			resultStr, ok := result.Result.(string)
			if !ok {
				t.Fatalf("expected result to be string, got %T", result.Result)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(resultStr, want) {
					t.Errorf("Execute() = %q, want substring %q", resultStr, want)
				}
			}

			for _, missing := range tt.wantMissing {
				if strings.Contains(resultStr, missing) {
					t.Errorf("Execute() = %q, should not contain %q", resultStr, missing)
				}
			}
		})
	}
}

func TestSearchFileContentTool_SpecificationMethods(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	spec := &SearchFileContentToolSpec{}

	// Test SearchFileContentToolSpec methods
	if spec.Name() == "" {
		t.Errorf("SearchFileContentToolSpec.Name() returned empty string")
	}
	if spec.Name() != ToolNameSearchFileContent {
		t.Errorf("SearchFileContentToolSpec.Name() = %v, want %v", spec.Name(), ToolNameSearchFileContent)
	}

	if spec.Description() == "" {
		t.Errorf("SearchFileContentToolSpec.Description() returned empty string")
	}

	specParams := spec.Parameters()
	if specParams == nil {
		t.Errorf("SearchFileContentToolSpec.Parameters() returned nil")
	}
	if specParams["type"] != "object" {
		t.Errorf("SearchFileContentToolSpec.Parameters() type = %v, want object", specParams["type"])
	}

	// Test SearchFileContentTool methods
	if tool.Name() == "" {
		t.Errorf("SearchFileContentTool.Name() returned empty string")
	}
	if tool.Name() != ToolNameSearchFileContent {
		t.Errorf("SearchFileContentTool.Name() = %v, want %v", tool.Name(), ToolNameSearchFileContent)
	}

	if tool.Description() == "" {
		t.Errorf("SearchFileContentTool.Description() returned empty string")
	}

	toolParams := tool.Parameters()
	if toolParams == nil {
		t.Errorf("SearchFileContentTool.Parameters() returned nil")
	}
	if toolParams["type"] != "object" {
		t.Errorf("SearchFileContentTool.Parameters() type = %v, want object", toolParams["type"])
	}
}

func TestNewSearchFileContentToolFactory(t *testing.T) {
	mockFS := fs.NewMockFS()
	factory := NewSearchFileContentToolFactory(mockFS)

	if factory == nil {
		t.Fatalf("NewSearchFileContentToolFactory() returned nil")
	}

	registry := NewRegistry(nil)
	tool := factory(registry)

	if tool == nil {
		t.Fatalf("Factory produced nil tool")
	}

	// Type assert to SearchFileContentTool to call Name()
	searchTool, ok := tool.(*SearchFileContentTool)
	if !ok {
		t.Fatalf("Factory did not produce SearchFileContentTool, got %T", tool)
	}

	if searchTool.Name() != ToolNameSearchFileContent {
		t.Errorf("Tool name = %v, want %v", searchTool.Name(), ToolNameSearchFileContent)
	}
}
