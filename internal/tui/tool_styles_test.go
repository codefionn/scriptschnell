package tui

import (
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/tools"
)

func TestGetToolTypeFromName(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     ToolType
	}{
		{"read_file", "read_file", ToolTypeReadFile},
		{"create_file", "create_file", ToolTypeCreateFile},
		{"edit_file", "edit_file", ToolTypeEditFile},
		{"replace_file", "replace_file", ToolTypeReplaceFile},
		{"shell", "shell", ToolTypeShell},
		{"go_sandbox", "go_sandbox", ToolTypeGoSandbox},
		{"web_search", "web_search", ToolTypeWebSearch},
		{"web_fetch", "web_fetch", ToolTypeWebFetch},
		{"todo", "todo", ToolTypeTodo},
		{"status_program", "status_program", ToolTypeStatus},
		{"stop_program", "stop_program", ToolTypeStopProgram},
		{"parallel_tool_execution", "parallel_tool_execution", ToolTypeParallel},
		{"unknown_tool", "unknown_tool", ToolTypeUnknown},
		{"read_file prefix", "read_file_numbered", ToolTypeReadFile},
		{"shell prefix", "shell_command", ToolTypeShell},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetToolTypeFromName(tt.toolName)
			if got != tt.want {
				t.Errorf("GetToolTypeFromName(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestGetIconForToolType(t *testing.T) {
	tests := []struct {
		toolType ToolType
		want     string
	}{
		{ToolTypeReadFile, IconReadFile},
		{ToolTypeCreateFile, IconCreateFile},
		{ToolTypeEditFile, IconEditFile},
		{ToolTypeShell, IconShell},
		{ToolTypeGoSandbox, IconGoSandbox},
		{ToolTypeUnknown, IconUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.toolType.String(), func(t *testing.T) {
			got := GetIconForToolType(tt.toolType)
			if got != tt.want {
				t.Errorf("GetIconForToolType(%v) = %q, want %q", tt.toolType, got, tt.want)
			}
		})
	}
}

func TestGetStateIndicator(t *testing.T) {
	tests := []struct {
		state ToolState
		want  string
	}{
		{ToolStatePending, IndicatorPending},
		{ToolStateRunning, IndicatorRunning},
		{ToolStateCompleted, IndicatorCompleted},
		{ToolStateFailed, IndicatorFailed},
		{ToolStateWarning, IndicatorWarning},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			got := GetStateIndicator(tt.state)
			if got != tt.want {
				t.Errorf("GetStateIndicator(%v) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestGetStateLabel(t *testing.T) {
	tests := []struct {
		state ToolState
		want  string
	}{
		{ToolStatePending, "Pending"},
		{ToolStateRunning, "Running"},
		{ToolStateCompleted, "Completed"},
		{ToolStateFailed, "Failed"},
		{ToolStateWarning, "Warning"},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			got := GetStateLabel(tt.state)
			if got != tt.want {
				t.Errorf("GetStateLabel(%v) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestToolStylesInitialization(t *testing.T) {
	ts := InitializeToolStyles()

	if ts == nil {
		t.Fatal("InitializeToolStyles() returned nil")
	}

	// Test state styles
	if ts.PendingStyle.GetForeground() == nil {
		t.Error("PendingStyle has no foreground color")
	}
	if ts.RunningStyle.GetForeground() == nil {
		t.Error("RunningStyle has no foreground color")
	}
	if ts.CompletedStyle.GetForeground() == nil {
		t.Error("CompletedStyle has no foreground color")
	}
	if ts.FailedStyle.GetForeground() == nil {
		t.Error("FailedStyle has no foreground color")
	}

	// Test tool type styles
	for toolType := range ts.ToolTypeStyles {
		style := ts.GetToolTypeStyle(toolType)
		if style.GetForeground() == nil {
			t.Errorf("ToolTypeStyle for %v has no foreground color", toolType)
		}
	}
}

func TestFormatCompactToolCall(t *testing.T) {
	ts := InitializeToolStyles()
	tests := []struct {
		name       string
		toolName   string
		parameters map[string]interface{}
		state      ToolState
		wantEmpty  bool
	}{
		{
			name:       "read_file with path",
			toolName:   tools.ToolNameReadFile,
			parameters: map[string]interface{}{"path": "test.go"},
			state:      ToolStateRunning,
			wantEmpty:  false,
		},
		{
			name:       "shell with command",
			toolName:   tools.ToolNameShell,
			parameters: map[string]interface{}{"command": "ls -la"},
			state:      ToolStateRunning,
			wantEmpty:  false,
		},
		{
			name:       "go_sandbox without params",
			toolName:   tools.ToolNameGoSandbox,
			parameters: map[string]interface{}{},
			state:      ToolStateRunning,
			wantEmpty:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ts.FormatCompactToolCall(tt.toolName, tt.parameters, tt.state, "")
			if got == "" && !tt.wantEmpty {
				t.Errorf("FormatCompactToolCall() returned empty string, want non-empty")
			}
			if got != "" && tt.wantEmpty {
				t.Errorf("FormatCompactToolCall() returned %q, want empty", got)
			}
		})
	}
}

func TestExtractPrimaryParameter(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		parameters map[string]interface{}
		want       string
	}{
		{
			name:       "read_file path",
			toolName:   tools.ToolNameReadFile,
			parameters: map[string]interface{}{"path": "internal/test.go"},
			want:       "internal/test.go",
		},
		{
			name:       "shell command string",
			toolName:   tools.ToolNameShell,
			parameters: map[string]interface{}{"command": "go test ./..."},
			want:       "go test ./...",
		},
		{
			name:       "shell command array",
			toolName:   tools.ToolNameShell,
			parameters: map[string]interface{}{"command": []interface{}{"ls", "-la", "/tmp"}},
			want:       "ls -la /tmp",
		},
		{
			name:       "create_file path",
			toolName:   tools.ToolNameCreateFile,
			parameters: map[string]interface{}{"path": "new_file.go"},
			want:       "new_file.go",
		},
		{
			name:       "go_sandbox without params",
			toolName:   tools.ToolNameGoSandbox,
			parameters: map[string]interface{}{},
			want:       "Go code execution",
		},
		{
			name:       "go_sandbox with description",
			toolName:   tools.ToolNameGoSandbox,
			parameters: map[string]interface{}{"description": "Build and test"},
			want:       "Build and test",
		},
		{
			name:       "web_search with query",
			toolName:   tools.ToolNameWebSearch,
			parameters: map[string]interface{}{"query": "golang testing"},
			want:       "golang testing",
		},
		{
			name:       "web_fetch with url",
			toolName:   tools.ToolNameWebFetch,
			parameters: map[string]interface{}{"url": "https://example.com"},
			want:       "https://example.com",
		},
		{
			name:       "todo with action",
			toolName:   tools.ToolNameTodo,
			parameters: map[string]interface{}{"action": "add"},
			want:       "add",
		},
		{
			name:       "parallel_tool_execution",
			toolName:   tools.ToolNameParallel,
			parameters: map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{}, map[string]interface{}{}}},
			want:       "2 tools",
		},
		{
			name:       "search_files pattern",
			toolName:   tools.ToolNameSearchFiles,
			parameters: map[string]interface{}{"pattern": "*.go"},
			want:       "*.go",
		},
		{
			name:       "search_file_content pattern",
			toolName:   tools.ToolNameSearchFileContent,
			parameters: map[string]interface{}{"pattern": "func main"},
			want:       "func main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPrimaryParameter(tt.toolName, tt.parameters)
			if got != tt.want {
				t.Errorf("extractPrimaryParameter() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractSecondaryParameters(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		parameters map[string]interface{}
		wantKeys   []string // Keys that should be present
	}{
		{
			name:       "read_file with line range",
			toolName:   tools.ToolNameReadFile,
			parameters: map[string]interface{}{"path": "test.go", "from_line": 10.0, "to_line": 50.0},
			wantKeys:   []string{"lines"},
		},
		{
			name:       "shell with timeout and background",
			toolName:   tools.ToolNameShell,
			parameters: map[string]interface{}{"command": "ls", "timeout": 30.0, "background": true},
			wantKeys:   []string{"timeout", "mode"},
		},
		{
			name:       "go_sandbox with libraries",
			toolName:   tools.ToolNameGoSandbox,
			parameters: map[string]interface{}{"code": "...", "libraries": []interface{}{"lib1", "lib2"}, "timeout": 60.0},
			wantKeys:   []string{"libs", "timeout"},
		},
		{
			name:       "todo with text and status",
			toolName:   tools.ToolNameTodo,
			parameters: map[string]interface{}{"action": "add", "text": "Write tests", "status": "pending", "priority": "high"},
			wantKeys:   []string{"text", "status", "priority"},
		},
		{
			name:       "search_file_content with context",
			toolName:   tools.ToolNameSearchFileContent,
			parameters: map[string]interface{}{"pattern": "func", "glob": "*.go", "context": 3.0},
			wantKeys:   []string{"glob", "context"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSecondaryParameters(tt.toolName, tt.parameters)
			for _, key := range tt.wantKeys {
				if _, ok := got[key]; !ok {
					t.Errorf("extractSecondaryParameters() missing key %q, got %v", key, got)
				}
			}
		})
	}
}

func TestTruncatePathSmart(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		maxLen int
		want   string // Optional exact match; if empty, just check length
	}{
		{"short path", "test.go", 50, "test.go"},
		{"medium path", "internal/tui/test.go", 30, "internal/tui/test.go"},
		{"long path - preserve filename", "/very/long/directory/path/to/file/test.go", 25, ""}, // Just check length
		{"very long filename", "/path/to/very_long_filename_that_exceeds_limit.go", 20, ""},    // Just check length
		{"path with parent", "/a/b/c/d/test.go", 20, ""},                                       // Just check length
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncatePathSmart(tt.path, tt.maxLen)
			if len(got) > tt.maxLen {
				t.Errorf("truncatePathSmart() result too long: %q (max %d)", got, tt.maxLen)
			}
			// For specific expected values
			if tt.want != "" && got != tt.want {
				t.Errorf("truncatePathSmart(%q, %d) = %q, want %q", tt.path, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestTruncateCommandSmart(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		maxLen int
		want   string
	}{
		{"short command", "ls", 50, "ls"},
		{"command with args", "go test ./internal/tui -v", 30, "go test ./internal/tui -v"},
		{"long command - truncate args", "go test ./internal/very/long/path/to/package -v -count=1 -cover", 30, "go test ... +4 more"},
		{"very long binary", "/very/long/path/to/binary", 15, "/very/long..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateCommandSmart(tt.cmd, tt.maxLen)
			if len(got) > tt.maxLen {
				t.Errorf("truncateCommandSmart() result too long: %q (max %d)", got, tt.maxLen)
			}
		})
	}
}

func TestTruncateURLSmart(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		maxLen int
		check  func(string) bool
	}{
		{
			name:   "short url",
			url:    "https://example.com",
			maxLen: 50,
			check:  func(s string) bool { return s == "https://example.com" },
		},
		{
			name:   "url with path",
			url:    "https://example.com/path/to/page",
			maxLen: 30,
			check:  func(s string) bool { return len(s) <= 30 },
		},
		{
			name:   "long url preserve domain",
			url:    "https://example.com/very/long/path/to/some/deeply/nested/page.html",
			maxLen: 35,
			check:  func(s string) bool { return strings.Contains(s, "example.com") && len(s) <= 35 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateURLSmart(tt.url, tt.maxLen)
			if len(got) > tt.maxLen {
				t.Errorf("truncateURLSmart() result too long: %q (max %d)", got, tt.maxLen)
			}
			if !tt.check(got) {
				t.Errorf("truncateURLSmart(%q, %d) = %q, check failed", tt.url, tt.maxLen, got)
			}
		})
	}
}

func TestTruncateStringSmart(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		check  func(string) bool
	}{
		{
			name:   "short string",
			s:      "hello world",
			maxLen: 20,
			check:  func(s string) bool { return s == "hello world" },
		},
		{
			name:   "truncate at word boundary",
			s:      "this is a long string with many words",
			maxLen: 20,
			check:  func(s string) bool { return strings.HasSuffix(s, "...") && len(s) <= 20 },
		},
		{
			name:   "no word boundary",
			s:      "abcdefghijklmnopqrstuvwxyz",
			maxLen: 15,
			check:  func(s string) bool { return s == "abcdefghijkl..." },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStringSmart(tt.s, tt.maxLen)
			if len(got) > tt.maxLen {
				t.Errorf("truncateStringSmart() result too long: %q (max %d)", got, tt.maxLen)
			}
			if !tt.check(got) {
				t.Errorf("truncateStringSmart(%q, %d) = %q, check failed", tt.s, tt.maxLen, got)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"small max", "hello", 2, "he"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}
