package orchestrator

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/session"
)

func TestSelectCompactionPrefix(t *testing.T) {
	tokens := []int{10, 10, 10, 10, 10}
	total := 60

	prefix := selectCompactionPrefix(tokens, total)
	if prefix != 3 {
		t.Fatalf("expected prefix 3, got %d", prefix)
	}
}

func TestSelectCompactionPrefixHandlesZeros(t *testing.T) {
	tokens := []int{0, 0, 5}
	total := 5

	prefix := selectCompactionPrefix(tokens, total)
	if prefix != 2 {
		t.Fatalf("expected prefix 2 when threshold met midway, got %d", prefix)
	}
}

func TestCondenseContent(t *testing.T) {
	value := condenseContent("  multiple\nlines of\ttext  ", 20)
	if value != "multiple lines of..." {
		t.Fatalf("unexpected condensed content: %q", value)
	}

	ellipsis := condenseContent("0123456789ABCDEFGHIJ", 10)
	if ellipsis != "0123456..." {
		t.Fatalf("expected ellipsis, got %q", ellipsis)
	}
}

func TestHeuristicContextWindow(t *testing.T) {
	if window := heuristicContextWindow("claude-3-sonnet-20240229"); window != 200000 {
		t.Fatalf("expected Claude heuristic 200000, got %d", window)
	}

	if window := heuristicContextWindow("gpt-3.5-turbo"); window != 4096 {
		t.Fatalf("expected GPT-3.5 heuristic 4096, got %d", window)
	}

	if window := heuristicContextWindow("devstral-small"); window != 128000 {
		t.Fatalf("expected Devstral heuristic 128000, got %d", window)
	}
}

func TestFormatRoleLabel(t *testing.T) {
	msg := &session.Message{Role: "assistant", ToolName: "shell", Content: "ran command"}

	label := formatRoleLabel(msg)
	if label != "Assistant (shell)" {
		t.Fatalf("unexpected label: %s", label)
	}
}

func TestSelectCompactionPrefix_EdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		perMessageTokens []int
		totalTokens      int
		expected         int
	}{
		{"empty_slice", []int{}, 0, 0},
		{"single_element", []int{100}, 100, 1},
		{"all_equal_10", []int{10, 10, 10, 10, 10, 10, 10, 10, 10, 10}, 100, 4},
		{"total_zero", []int{5, 5, 5}, 0, 1},
		{"large_first", []int{1000, 1, 1}, 1002, 1},
		{"all_zeros", []int{0, 0, 0, 0, 0}, 5, 2},
		{"threshold_exact", []int{20, 20}, 100, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectCompactionPrefix(tt.perMessageTokens, tt.totalTokens)
			if got != tt.expected {
				t.Fatalf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestCondenseContent_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		limit    int
		expected string
	}{
		{"empty_string", "", 10, "(no content)"},
		{"whitespace_only", "   \t\n  ", 10, "(no content)"},
		{"limit_zero", "hello", 0, ""},
		{"limit_negative", "hello", -5, ""},
		{"limit_1", "hello", 1, "h"},
		{"limit_2", "hello", 2, "he"},
		{"limit_3_short", "abc", 3, "abc"},
		{"limit_3_longer", "hello", 3, "hel"},
		{"exact_limit", "hello", 5, "hello"},
		{"multiline_collapse", "foo\nbar\nbaz", 100, "foo bar baz"},
		{"unicode_runes", "\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9", 4, "\u00e9..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := condenseContent(tt.content, tt.limit)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestFormatRoleLabel_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		msg      *session.Message
		expected string
	}{
		{"empty_role", &session.Message{Role: ""}, "Unknown"},
		{"no_tool_name", &session.Message{Role: "user"}, "User"},
		{"role_lowercase", &session.Message{Role: "assistant"}, "Assistant"},
		{"role_uppercase", &session.Message{Role: "ASSISTANT"}, "ASSISTANT"},
		{"role_tool_with_name", &session.Message{Role: "tool", ToolName: "read_file"}, "Tool (read_file)"},
		{"role_system", &session.Message{Role: "system"}, "System"},
		{"role_whitespace", &session.Message{Role: "  user  "}, "User"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRoleLabel(tt.msg)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestHeuristicContextWindow_MorePatterns(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		expected int
	}{
		{"claude_haiku", "claude-3-haiku-20240307", 200000},
		{"gpt4_plain", "gpt-4", 8192},
		{"gpt4_32k", "gpt-4-32k", 32768},
		{"gpt4_turbo", "gpt-4-turbo", 128000},
		{"gpt4o", "gpt-4o", 128000},
		{"gpt4o_mini", "gpt-4o-mini", 128000},
		{"o1_preview", "o1-preview", 128000},
		{"o3_mini", "o3-mini", 128000},
		{"gpt35_turbo_16k", "gpt-3.5-turbo-16k", 16384},
		{"devstral_large", "devstral-large", 128000},
		{"unknown_model", "my-custom-model", 8192},
		{"empty_model", "", 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := heuristicContextWindow(tt.modelID)
			if got != tt.expected {
				t.Fatalf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}
