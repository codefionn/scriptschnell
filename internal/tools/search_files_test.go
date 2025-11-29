package tools

import (
	"context"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/fs"
)

func TestSearchFilesTool(t *testing.T) {
	// Create mock filesystem
	mockFS := fs.NewMockFS()

	// Add test files
	write := func(path, contents string) {
		if err := mockFS.WriteFile(context.Background(), path, []byte(contents)); err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}

	write("main.go", "package main\n\nfunc main() {\n}")
	write("internal/tools/read_file.go", "package tools\n\ntype ReadFileTool struct {}")
	write("internal/tools/write_file_diff.go", "package tools\n\ntype WriteFileDiffTool struct {}")
	write("README.md", "# Project\n\nThis is a test project")
	write("docs/guide.md", "# Guide\n\nUsage instructions")

	tool := NewSearchFilesTool(mockFS)

	tests := []struct {
		name          string
		params        map[string]interface{}
		expectedCount int
		expectError   bool
		checkMatches  func([]string) bool
	}{
		{
			name: "search for go files",
			params: map[string]interface{}{
				"pattern": "*.go",
			},
			expectedCount: 3, // main.go, read_file.go, write_file_diff.go
			expectError:   false,
		},
		{
			name: "search for markdown files",
			params: map[string]interface{}{
				"pattern": "*.md",
			},
			expectedCount: 2, // README.md, guide.md
			expectError:   false,
		},
		{
			name: "search with content regex",
			params: map[string]interface{}{
				"pattern":       "*.go",
				"content_regex": "ReadFileTool",
			},
			expectedCount: 1, // Only read_file.go contains "ReadFileTool"
			expectError:   false,
		},
		{
			name: "search with max_results",
			params: map[string]interface{}{
				"pattern":     "*.go",
				"max_results": 2,
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name: "invalid content regex",
			params: map[string]interface{}{
				"pattern":       "*.go",
				"content_regex": "[invalid(regex",
			},
			expectError: true,
		},
		{
			name:        "missing pattern",
			params:      map[string]interface{}{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), tt.params)

			if tt.expectError {
				if result.Error == "" {
					t.Errorf("expected error but got none")
				}
				return
			}

			if result.Error != "" {
				t.Fatalf("unexpected error: %s", result.Error)
			}

			resultMap, ok := result.Result.(map[string]interface{})
			if !ok {
				t.Fatalf("expected result to be map[string]interface{}, got %T", result.Result)
			}

			matches, ok := resultMap["matches"].([]string)
			if !ok {
				t.Fatalf("expected matches to be []string, got %T", resultMap["matches"])
			}

			count := resultMap["count"].(int)
			if count != tt.expectedCount {
				t.Errorf("expected %d matches, got %d. Matches: %v", tt.expectedCount, count, matches)
			}

			if tt.checkMatches != nil && !tt.checkMatches(matches) {
				t.Errorf("match validation failed for matches: %v", matches)
			}
		})
	}
}

func TestSearchFilesToolGlobPattern(t *testing.T) {
	mockFS := fs.NewMockFS()

	// Create a directory structure
	write := func(path, contents string) {
		if err := mockFS.WriteFile(context.Background(), path, []byte(contents)); err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}

	write("cmd/main.go", "package main")
	write("internal/tools/tool.go", "package tools")
	write("internal/fs/fs.go", "package fs")
	write("test.go", "package test")

	tool := NewSearchFilesTool(mockFS)

	tests := []struct {
		name          string
		pattern       string
		expectedCount int
	}{
		{
			name:          "recursive search with **",
			pattern:       "**/*.go",
			expectedCount: 4, // All .go files
		},
		{
			name:          "specific directory pattern",
			pattern:       "internal/**/*.go",
			expectedCount: 2, // tool.go and fs.go
		},
		{
			name:          "simple pattern matches all",
			pattern:       "*.go",
			expectedCount: 4, // All .go files (matches basename anywhere)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), map[string]interface{}{
				"pattern": tt.pattern,
			})

			if result.Error != "" {
				t.Fatalf("unexpected error: %s", result.Error)
			}

			resultMap := result.Result.(map[string]interface{})
			matches := resultMap["matches"].([]string)
			count := resultMap["count"].(int)

			if count != tt.expectedCount {
				t.Errorf("expected %d matches, got %d. Matches: %v", tt.expectedCount, count, matches)
			}
		})
	}
}
