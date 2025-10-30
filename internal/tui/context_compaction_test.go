package tui

import (
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/session"
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
}

func TestFormatRoleLabel(t *testing.T) {
	msg := &session.Message{Role: "assistant", ToolName: "shell", Content: "ran command"}

	label := formatRoleLabel(msg)
	if label != "Assistant (shell)" {
		t.Fatalf("unexpected label: %s", label)
	}
}
