package tui

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/tools"
)

func TestToolSummaryGenerator_GenerateCompactSummary(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		name     string
		toolName string
		params   map[string]interface{}
		wantLen  bool // Whether we expect a non-empty result
		contains string
	}{
		{
			name:     "read_file with path",
			toolName: tools.ToolNameReadFile,
			params:   map[string]interface{}{"path": "/home/user/project/file.go"},
			wantLen:  true,
			contains: "path:",
		},
		{
			name:     "create_file with path",
			toolName: tools.ToolNameCreateFile,
			params:   map[string]interface{}{"path": "newfile.txt", "content": "hello"},
			wantLen:  true,
			contains: "path:",
		},
		{
			name:     "shell with command",
			toolName: tools.ToolNameShell,
			params:   map[string]interface{}{"command": []interface{}{"ls", "-la"}},
			wantLen:  true,
			contains: "cmd:",
		},
		{
			name:     "web_search with queries",
			toolName: tools.ToolNameWebSearch,
			params:   map[string]interface{}{"queries": []interface{}{"golang test"}},
			wantLen:  true,
			contains: "query:",
		},
		{
			name:     "web_fetch with url",
			toolName: tools.ToolNameWebFetch,
			params:   map[string]interface{}{"url": "https://example.com"},
			wantLen:  true,
			contains: "url:",
		},
		{
			name:     "go_sandbox with description",
			toolName: tools.ToolNameGoSandbox,
			params:   map[string]interface{}{"description": "Run tests"},
			wantLen:  true,
			contains: "desc:",
		},
		{
			name:     "empty params",
			toolName: tools.ToolNameReadFile,
			params:   map[string]interface{}{},
			wantLen:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.GenerateCompactSummary(tt.toolName, tt.params)
			if tt.wantLen && got == "" {
				t.Error("GenerateCompactSummary() returned empty string, expected non-empty")
			}
			if !tt.wantLen && got != "" {
				t.Errorf("GenerateCompactSummary() = %q, expected empty", got)
			}
			if tt.contains != "" && got != "" {
				if !toolSummaryContainsStr(got, tt.contains) {
					t.Errorf("GenerateCompactSummary() = %q, should contain %q", got, tt.contains)
				}
			}
		})
	}
}

func TestToolSummaryGenerator_GenerateSummary(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		name     string
		toolName string
		params   map[string]interface{}
		state    ToolState
		contains string
	}{
		{
			name:     "read_file running",
			toolName: tools.ToolNameReadFile,
			params:   map[string]interface{}{"path": "test.go"},
			state:    ToolStateRunning,
			contains: "read",
		},
		{
			name:     "shell command",
			toolName: tools.ToolNameShell,
			params:   map[string]interface{}{"command": []interface{}{"echo", "hello"}},
			state:    ToolStateRunning,
			contains: "echo",
		},
		{
			name:     "web_search queries",
			toolName: tools.ToolNameWebSearch,
			params:   map[string]interface{}{"queries": []interface{}{"test query"}},
			state:    ToolStateCompleted,
			contains: "search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.GenerateSummary(tt.toolName, tt.params, tt.state)
			if got == "" {
				t.Error("GenerateSummary() returned empty string")
			}
			if !toolSummaryContainsStr(got, tt.contains) {
				t.Errorf("GenerateSummary() = %q, should contain %q", got, tt.contains)
			}
		})
	}
}

func TestToolSummaryGenerator_ShortenPath(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		path     string
		maxLen   int
		checkLen bool
	}{
		{"/short/path.txt", 40, false},
		{"/very/long/path/that/exceeds/the/maximum/length/and/should/be/shortened.txt", 40, true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := gen.shortenPath(tt.path)
			if tt.checkLen && len(got) > 50 { // Allow some buffer
				t.Errorf("shortenPath() = %q (len=%d), should be shorter", got, len(got))
			}
		})
	}
}

func TestToolSummaryGenerator_ShortenURL(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		url      string
		maxLen   int
		checkLen bool
	}{
		{"https://example.com/short", 50, false},
		{"https://very-long-domain-name.example.com/path/to/some/resource/that/is/quite/long", 50, true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := gen.shortenURL(tt.url)
			if tt.checkLen && len(got) > 60 { // Allow some buffer
				t.Errorf("shortenURL() = %q (len=%d), should be shorter", got, len(got))
			}
		})
	}
}

func TestToolSummaryGenerator_TruncateString(t *testing.T) {
	gen := NewToolSummaryGenerator()

	tests := []struct {
		s       string
		maxLen  int
		wantLen int
	}{
		{"short", 10, 5},
		{"this is a very long string that needs truncation", 20, 20},
		{"exact", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			got := gen.truncateString(tt.s, tt.maxLen)
			if len(got) != tt.wantLen {
				t.Errorf("truncateString() = %q (len=%d), want len=%d", got, len(got), tt.wantLen)
			}
		})
	}
}

func TestToolSummaryGenerator_ExtractFunctionCalls(t *testing.T) {
	gen := NewToolSummaryGenerator()

	code := `
package main

func main() {
	ExecuteCommand([]string{"ls", "-la"}, "")
	Fetch("GET", "https://example.com", "")
	ReadFile("test.txt", 0, 0)
}
`

	got := gen.extractFunctionCalls(code)

	expected := []string{"ExecuteCommand", "Fetch", "ReadFile"}
	for _, exp := range expected {
		found := false
		for _, fn := range got {
			if fn == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("extractFunctionCalls() missing expected function %q, got %v", exp, got)
		}
	}
}

// Helper function
func toolSummaryContainsStr(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > 0 && toolSummaryContainsStrHelper(s, substr))
}

func toolSummaryContainsStrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
