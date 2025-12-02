package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/statcode-ai/scriptschnell/internal/fs"
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
				"  7: \tfmt.Println(\"World\") // match me",
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
