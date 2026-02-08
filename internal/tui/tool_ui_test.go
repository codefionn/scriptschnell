package tui

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/tools"
)

// TestFileOperationSummaries tests summaries for file-related tools
func TestFileOperationSummaries(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		name       string
		toolName   string
		params     map[string]interface{}
		wantInResp []string
	}{
		{
			name:       "read_file with simple path",
			toolName:   tools.ToolNameReadFile,
			params:     map[string]interface{}{"path": "test.go"},
			wantInResp: []string{"read", "test.go"},
		},
		{
			name:     "read_file with line range",
			toolName: tools.ToolNameReadFile,
			params: map[string]interface{}{
				"path":      "main.go",
				"from_line": float64(10),
				"to_line":   float64(20),
			},
			wantInResp: []string{"read", "main.go", "lines"},
		},
		{
			name:     "create_file with content",
			toolName: tools.ToolNameCreateFile,
			params: map[string]interface{}{
				"path":    "newfile.txt",
				"content": "hello world",
			},
			wantInResp: []string{"create", "newfile.txt", "chars"},
		},
		{
			name:     "edit_file with single edit",
			toolName: tools.ToolNameEditFile,
			params: map[string]interface{}{
				"path": "file.go",
				"edits": []interface{}{
					map[string]interface{}{"old_string": "old", "new_string": "new"},
				},
			},
			wantInResp: []string{"edit", "file.go", "1 edit"},
		},
		{
			name:     "edit_file with multiple edits",
			toolName: tools.ToolNameEditFile,
			params: map[string]interface{}{
				"path": "file.go",
				"edits": []interface{}{
					map[string]interface{}{"old_string": "a", "new_string": "b"},
					map[string]interface{}{"old_string": "c", "new_string": "d"},
					map[string]interface{}{"old_string": "e", "new_string": "f"},
				},
			},
			wantInResp: []string{"edit", "3 edits"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := gen.GenerateSummary(tt.toolName, tt.params, ToolStateRunning)
			if summary == "" {
				t.Error("GenerateSummary returned empty string")
				return
			}

			for _, want := range tt.wantInResp {
				if !toolUIContainsStr(summary, want) {
					t.Errorf("Summary %q does not contain %q", summary, want)
				}
			}
		})
	}
}

// TestShellCommandSummaries tests summaries for shell commands
func TestShellCommandSummaries(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantInResp []string
	}{
		{
			name: "simple command",
			params: map[string]interface{}{
				"command": []interface{}{"ls"},
			},
			wantInResp: []string{"ls"},
		},
		{
			name: "command with args",
			params: map[string]interface{}{
				"command": []interface{}{"go", "build", "./..."},
			},
			wantInResp: []string{"go", "build"},
		},
		{
			name: "long command with truncation",
			params: map[string]interface{}{
				"command": []interface{}{"some-very-long-command-name", "--arg1", "--arg2", "--arg3"},
			},
			wantInResp: []string{"some-very-long-command-name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := gen.GenerateSummary(tools.ToolNameShell, tt.params, ToolStateRunning)
			if summary == "" {
				t.Error("GenerateSummary returned empty string")
				return
			}

			for _, want := range tt.wantInResp {
				if !toolUIContainsStr(summary, want) {
					t.Errorf("Summary %q does not contain %q", summary, want)
				}
			}
		})
	}
}

// TestGoSandboxSummaries tests summaries for Go sandbox
func TestGoSandboxSummaries(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantInResp []string
	}{
		{
			name: "with description",
			params: map[string]interface{}{
				"description": "Build and test",
				"code":        "package main\nfunc main() {}",
			},
			wantInResp: []string{"Go:", "Build and test"},
		},
		{
			name: "with function calls",
			params: map[string]interface{}{
				"code": "package main\nfunc main() { ExecuteCommand([]string{\"ls\"}, \"\") }",
			},
			wantInResp: []string{"Go:", "ExecuteCommand"},
		},
		{
			name: "without description or functions",
			params: map[string]interface{}{
				"code": "package main\nfunc main() { fmt.Println(\"hello\") }",
			},
			wantInResp: []string{"executing Go code"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := gen.GenerateSummary(tools.ToolNameGoSandbox, tt.params, ToolStateRunning)
			if summary == "" {
				t.Error("GenerateSummary returned empty string")
				return
			}

			for _, want := range tt.wantInResp {
				if !toolUIContainsStr(summary, want) {
					t.Errorf("Summary %q does not contain %q", summary, want)
				}
			}
		})
	}
}

// TestWebToolSummaries tests summaries for web-related tools
func TestWebToolSummaries(t *testing.T) {
	gen := NewToolSummaryGenerator()

	t.Run("web_search with queries", func(t *testing.T) {
		params := map[string]interface{}{
			"queries": []interface{}{"golang testing", "go test patterns"},
		}
		summary := gen.GenerateSummary(tools.ToolNameWebSearch, params, ToolStateRunning)

		if !toolUIContainsStr(summary, "search") {
			t.Errorf("Summary %q should contain 'search'", summary)
		}
		if !toolUIContainsStr(summary, "golang testing") {
			t.Errorf("Summary %q should contain query", summary)
		}
	})

	t.Run("web_fetch with url", func(t *testing.T) {
		params := map[string]interface{}{
			"url": "https://example.com/docs",
		}
		summary := gen.GenerateSummary(tools.ToolNameWebFetch, params, ToolStateRunning)

		if !toolUIContainsStr(summary, "fetch") {
			t.Errorf("Summary %q should contain 'fetch'", summary)
		}
		if !toolUIContainsStr(summary, "example.com") {
			t.Errorf("Summary %q should contain domain", summary)
		}
	})
}

// TestTodoSummaries tests summaries for todo operations
func TestTodoSummaries(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantInResp []string
	}{
		{
			name:       "list todos",
			params:     map[string]interface{}{"action": "list"},
			wantInResp: []string{"listing todos"},
		},
		{
			name:       "add todo",
			params:     map[string]interface{}{"action": "add", "text": "Write tests"},
			wantInResp: []string{"add:", "Write tests"},
		},
		{
			name:       "check todo",
			params:     map[string]interface{}{"action": "check", "id": "todo_1"},
			wantInResp: []string{"check", "todo_1"},
		},
		{
			name:       "delete todo",
			params:     map[string]interface{}{"action": "delete", "id": "todo_2"},
			wantInResp: []string{"delete", "todo_2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := gen.GenerateSummary(tools.ToolNameTodo, tt.params, ToolStateRunning)
			if summary == "" {
				t.Error("GenerateSummary returned empty string")
				return
			}

			for _, want := range tt.wantInResp {
				if !toolUIContainsStr(summary, want) {
					t.Errorf("Summary %q does not contain %q", summary, want)
				}
			}
		})
	}
}

// TestStatisticsDisplay tests the statistics display function
func TestStatisticsDisplay(t *testing.T) {
	tests := []struct {
		name     string
		metadata *tools.ExecutionMetadata
		wantIn   []string
		dontWant []string
	}{
		{
			name: "full metadata",
			metadata: &tools.ExecutionMetadata{
				DurationMs:      1500,
				OutputLineCount: 25,
				OutputSizeBytes: 1024,
			},
			wantIn: []string{"1.5s", "25 lines", "1.0 KB"},
		},
		{
			name: "fast execution",
			metadata: &tools.ExecutionMetadata{
				DurationMs:      50,
				OutputLineCount: 1,
				OutputSizeBytes: 100,
			},
			wantIn: []string{"50ms", "1 line", "100 B"},
		},
		{
			name: "failed execution",
			metadata: &tools.ExecutionMetadata{
				DurationMs: 100,
				ExitCode:   1,
			},
			wantIn: []string{"exit 1"},
		},
		{
			name:     "nil metadata",
			metadata: nil,
			wantIn:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := CreateStatisticsDisplay(tt.metadata)

			for _, want := range tt.wantIn {
				if !toolUIContainsStr(stats, want) {
					t.Errorf("Statistics %q does not contain %q", stats, want)
				}
			}

			for _, dont := range tt.dontWant {
				if toolUIContainsStr(stats, dont) {
					t.Errorf("Statistics %q should not contain %q", stats, dont)
				}
			}
		})
	}
}

// TestCompactSummaryConsistency tests that compact summaries are consistent
func TestCompactSummaryConsistency(t *testing.T) {
	gen := NewToolSummaryGenerator()

	// Test that the same parameters produce the same output
	params := map[string]interface{}{
		"path": "test.go",
		"line": 10,
	}

	summary1 := gen.GenerateCompactSummary(tools.ToolNameReadFile, params)
	summary2 := gen.GenerateCompactSummary(tools.ToolNameReadFile, params)

	if summary1 != summary2 {
		t.Errorf("Compact summaries should be consistent: %q vs %q", summary1, summary2)
	}
}

// TestLongPathShortening tests that long paths are properly shortened
func TestLongPathShortening(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		path     string
		maxLen   int
		checkLen bool
	}{
		{"/short/path.txt", 40, false},
		{"/very/long/path/that/exceeds/the/maximum/length/and/should/be/shortened/significantly.txt", 40, true},
		{"/home/user/projects/myproject/internal/subpackage/deeply/nested/file.go", 40, true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			shortened := gen.shortenPath(tt.path)
			if tt.checkLen && len(shortened) > 50 {
				t.Errorf("shortenPath() = %q (len=%d), should be shorter than 50", shortened, len(shortened))
			}
		})
	}
}

// TestLongURLShortening tests that long URLs are properly shortened
func TestLongURLShortening(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		url      string
		checkLen bool
	}{
		{"https://example.com/short", false},
		{"https://very-long-domain-name.example.com/path/to/some/resource/that/is/quite/long/and/needs/truncation", true},
		{"https://docs.example.com/api/v1/resources/items/12345/details?param=value&other=thing", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			shortened := gen.shortenURL(tt.url)
			if tt.checkLen && len(shortened) > 60 {
				t.Errorf("shortenURL() = %q (len=%d), should be shorter than 60", shortened, len(shortened))
			}
		})
	}
}

// Helper function for testing
func toolUIContainsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return len(substr) == 0
}
