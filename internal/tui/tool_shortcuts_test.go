package tui

import (
	"fmt"
	"testing"
)

func TestToolShortcuts(t *testing.T) {
	ts := NewToolShortcuts()

	// Test initial state
	if ts.HasSelection() {
		t.Error("HasSelection should be false initially")
	}
	if ts.GetSelectedIndex() != -1 {
		t.Errorf("GetSelectedIndex = %d, want -1", ts.GetSelectedIndex())
	}

	// Test SetToolCount
	ts.SetToolCount(5)
	if ts.totalToolMessages != 5 {
		t.Errorf("totalToolMessages = %d, want 5", ts.totalToolMessages)
	}

	// Test SelectFirst
	if !ts.SelectFirst() {
		t.Error("SelectFirst should return true when tools exist")
	}
	if ts.GetSelectedIndex() != 0 {
		t.Errorf("GetSelectedIndex = %d, want 0", ts.GetSelectedIndex())
	}

	// Test SelectNext
	if !ts.SelectNext() {
		t.Error("SelectNext should return true when more tools exist")
	}
	if ts.GetSelectedIndex() != 1 {
		t.Errorf("GetSelectedIndex = %d, want 1", ts.GetSelectedIndex())
	}

	// Test SelectPrev
	if !ts.SelectPrev() {
		t.Error("SelectPrev should return true when not at first tool")
	}
	if ts.GetSelectedIndex() != 0 {
		t.Errorf("GetSelectedIndex = %d, want 0", ts.GetSelectedIndex())
	}

	// Test SelectPrev at first tool
	if ts.SelectPrev() {
		t.Error("SelectPrev should return false at first tool")
	}

	// Test SelectLast
	if !ts.SelectLast() {
		t.Error("SelectLast should return true when tools exist")
	}
	if ts.GetSelectedIndex() != 4 {
		t.Errorf("GetSelectedIndex = %d, want 4", ts.GetSelectedIndex())
	}

	// Test SelectNext at last tool
	if ts.SelectNext() {
		t.Error("SelectNext should return false at last tool")
	}

	// Test ClearSelection
	ts.ClearSelection()
	if ts.HasSelection() {
		t.Error("HasSelection should be false after ClearSelection")
	}
}

func TestToolShortcutsHandleKey(t *testing.T) {
	ts := NewToolShortcuts()
	ts.SetToolCount(3)

	tests := []struct {
		name         string
		key          string
		toolMode     bool
		wantHandled  bool
		wantShortcut string
		setupFn      func(*ToolShortcuts) // Optional setup function
	}{
		{"expand in tool mode", "e", true, true, "expand_collapse", nil},
		{"expand all", "E", true, true, "expand_all", nil},
		{"collapse all", "C", true, true, "collapse_all", nil},
		{"copy output", "y", true, true, "copy_output", nil},
		{"copy full", "Y", true, true, "copy_full", nil},
		{"next tool", "j", true, true, "select", nil},
		{"prev tool after selection", "k", true, true, "select", func(ts *ToolShortcuts) { ts.SelectNext(); ts.SelectNext() }},
		{"first tool", "g", true, true, "select", nil},
		{"last tool", "G", true, true, "select", nil},
		{"clear selection", "esc", true, true, "clear_selection", nil},
		// Note: When not in tool mode and no selection, shortcuts should not be handled
		// This test verifies that regular keys pass through when tool mode is disabled
		{"unknown key", "x", true, false, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset selection for each test
			ts.ClearSelection()
			ts.SetToolCount(3)

			// Run setup if provided
			if tt.setupFn != nil {
				tt.setupFn(ts)
			}

			msg, handled := ts.HandleKey(tt.key, tt.toolMode)
			if handled != tt.wantHandled {
				t.Errorf("HandleKey() handled = %v, want %v", handled, tt.wantHandled)
			}
			if handled && msg.Shortcut != tt.wantShortcut {
				t.Errorf("HandleKey() shortcut = %q, want %q", msg.Shortcut, tt.wantShortcut)
			}
		})
	}
}

func TestIsToolMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  message
		want bool
	}{
		{"tool message", message{role: "Tool"}, true},
		{"user message", message{role: "You"}, false},
		{"assistant message", message{role: "Assistant"}, false},
		{"system message", message{role: "System"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsToolMessage(tt.msg)
			if got != tt.want {
				t.Errorf("IsToolMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCountToolMessages(t *testing.T) {
	messages := []message{
		{role: "You"},
		{role: "Tool"},
		{role: "Assistant"},
		{role: "Tool"},
		{role: "Tool"},
		{role: "System"},
	}

	got := CountToolMessages(messages)
	if got != 3 {
		t.Errorf("CountToolMessages() = %d, want 3", got)
	}
}

func TestGetToolMessageIndex(t *testing.T) {
	messages := []message{
		{role: "You"},
		{role: "Tool"}, // toolIdx 0
		{role: "Assistant"},
		{role: "Tool"}, // toolIdx 1
		{role: "Tool"}, // toolIdx 2
	}

	tests := []struct {
		toolIdx int
		want    int
	}{
		{0, 1},
		{1, 3},
		{2, 4},
		{3, -1}, // out of range
		{-1, -1},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("toolIdx_%d", tt.toolIdx), func(t *testing.T) {
			got := GetToolMessageIndex(messages, tt.toolIdx)
			if got != tt.want {
				t.Errorf("GetToolMessageIndex(%d) = %d, want %d", tt.toolIdx, got, tt.want)
			}
		})
	}
}

func TestTruncateForDisplay(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short line", "hello", 10, "hello"},
		{"needs truncation", "hello world this is long", 10, "hello worl..."},
		{"multiline first", "first line\nsecond line", 20, "first line"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForDisplay(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForDisplay() = %q, want %q", got, tt.want)
			}
		})
	}
}
