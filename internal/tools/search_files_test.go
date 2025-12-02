package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
)

func TestSearchFilesTool(t *testing.T) {
	mockFS := fs.NewMockFS()

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
		expectedFiles []string
	}{
		{
			name: "search for go files",
			params: map[string]interface{}{
				"pattern": "*.go",
			},
			expectedCount: 3,
			expectedFiles: []string{
				"main.go",
				"internal/tools/read_file.go",
				"internal/tools/write_file_diff.go",
			},
		},
		{
			name: "search for markdown files",
			params: map[string]interface{}{
				"pattern": "*.md",
			},
			expectedCount: 2,
			expectedFiles: []string{
				"README.md",
				"docs/guide.md",
			},
		},
		{
			name: "search with content regex",
			params: map[string]interface{}{
				"pattern":       "*.go",
				"content_regex": "ReadFileTool",
			},
			expectedCount: 1,
			expectedFiles: []string{"internal/tools/read_file.go"},
		},
		{
			name: "search with max_results",
			params: map[string]interface{}{
				"pattern":     "*.go",
				"max_results": 2,
			},
			expectedCount: 2,
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

			resultStr, ok := result.Result.(string)
			if !ok {
				t.Fatalf("expected result to be string, got %T", result.Result)
			}

			if !strings.Contains(resultStr, "## Search Results") {
				t.Fatalf("expected markdown header in result: %s", resultStr)
			}

			countFragment := fmt.Sprintf("**Found:** %d file(s)", tt.expectedCount)
			if tt.expectedCount > 0 && !strings.Contains(resultStr, countFragment) {
				t.Fatalf("expected count fragment %q in result: %s", countFragment, resultStr)
			}

			matches := extractMarkdownMatches(resultStr)
			if tt.expectedCount >= 0 && len(matches) != tt.expectedCount {
				t.Fatalf("expected %d matches, got %d (%v)", tt.expectedCount, len(matches), matches)
			}

			for _, expectedFile := range tt.expectedFiles {
				if !containsString(matches, expectedFile) {
					t.Fatalf("expected file %s in matches %v", expectedFile, matches)
				}
			}
		})
	}
}

func TestSearchFilesToolGlobPattern(t *testing.T) {
	mockFS := fs.NewMockFS()

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
			expectedCount: 4,
		},
		{
			name:          "specific directory pattern",
			pattern:       "internal/**/*.go",
			expectedCount: 2,
		},
		{
			name:          "simple pattern matches all",
			pattern:       "*.go",
			expectedCount: 4,
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

			resultStr, ok := result.Result.(string)
			if !ok {
				t.Fatalf("expected result to be string, got %T", result.Result)
			}

			matches := extractMarkdownMatches(resultStr)
			if tt.expectedCount >= 0 && len(matches) != tt.expectedCount {
				t.Fatalf("expected %d matches, got %d (%v)", tt.expectedCount, len(matches), matches)
			}
		})
	}
}

func extractMarkdownMatches(markdown string) []string {
	var matches []string
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- `") && strings.HasSuffix(trimmed, "`") {
			matches = append(matches, strings.Trim(trimmed[3:], "`"))
		}
	}
	return matches
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
