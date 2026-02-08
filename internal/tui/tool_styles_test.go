package tui

import (
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
			name:       "shell command",
			toolName:   tools.ToolNameShell,
			parameters: map[string]interface{}{"command": "go test ./..."},
			want:       "go test ./...",
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
