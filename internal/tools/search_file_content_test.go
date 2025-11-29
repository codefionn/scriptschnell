package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/fs"
)

func TestSearchFileContentTool_Execute(t *testing.T) {
	mockFS := fs.NewMockFS()
	tool := NewSearchFileContentTool(mockFS)
	ctx := context.Background()

	// Setup files
	mockFS.WriteFile(ctx, "main.go", []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello")
	fmt.Println("World") // match me
	fmt.Println("Foo")
}
`))
	mockFS.WriteFile(ctx, "test.txt", []byte(`line 1
line 2
line 3
line 4
line 5
`))
	// Binary file
	mockFS.WriteFile(ctx, "bin.dat", []byte{0, 1, 2, 3})

	tests := []struct {
		name    string
		params  map[string]interface{}
		want    string
		wantErr bool
	}{
		{
			name: "Basic match",
			params: map[string]interface{}{
				"pattern": "World",
				"path":    ".",
			},
			want: `main.go:
 5: func main() {
 6: 	fmt.Println("Hello")
 7: 	fmt.Println("World") // match me
 8: 	fmt.Println("Foo")
 9: }
`,
		},
		{
			name: "Context control",
			params: map[string]interface{}{
				"pattern": "line 3",
				"path":    "test.txt",
				"context": 0,
			},
			want: `test.txt:
 3: line 3
`,
		},
		{
			name: "Padding check",
			params: map[string]interface{}{
				"pattern": "line 3",
				"path":    "test.txt",
				"context": 1,
			},
			// max line 5 -> digits 1 -> padding 2
			want: `test.txt:
 2: line 2
 3: line 3
 4: line 4
`,
		},
		{
			name: "Glob filtering",
			params: map[string]interface{}{
				"pattern": "fmt",
				"path":    ".",
				"glob":    "*.go",
			},
			// Should only match main.go
			want: `main.go:`,
		},
		{
			name: "No match",
			params: map[string]interface{}{
				"pattern": "NonExistent",
				"path":    ".",
			},
			want: "No matches found.",
		},
		{
			name: "Binary skip",
			params: map[string]interface{}{
				"pattern": ".", // Match anything
				"path":    "bin.dat",
			},
			want: "No matches found.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.Execute(ctx, tt.params)
			if (got.Error != "") != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", got.Error, tt.wantErr)
				return
			}
			gotStr := got.Result.(string)
			if !strings.Contains(gotStr, tt.want) {
				t.Errorf("Execute() = %q, want substring %q", gotStr, tt.want)
			}
		})
	}
}
