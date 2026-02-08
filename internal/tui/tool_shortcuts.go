package tui

import (
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.design/x/clipboard"
)

// ToolShortcuts provides keyboard interaction for tool call messages
type ToolShortcuts struct {
	selectedToolIndex int // Currently selected tool message index (-1 if none)
	totalToolMessages int // Count of tool messages in current view
	mu                sync.RWMutex
}

// NewToolShortcuts creates a new tool shortcuts handler
func NewToolShortcuts() *ToolShortcuts {
	return &ToolShortcuts{
		selectedToolIndex: -1,
		totalToolMessages: 0,
	}
}

// ShortcutKey definitions for tool interactions
const (
	ShortcutExpandCollapse = "e"   // Toggle expand/collapse
	ShortcutCollapseAll    = "C"   // Collapse all tool results
	ShortcutCopyOutput     = "y"   // Yank (copy) output to clipboard
	ShortcutCopyFullResult = "Y"   // Copy full result to clipboard
	ShortcutNextTool       = "j"   // Next tool message
	ShortcutPrevTool       = "k"   // Previous tool message
	ShortcutFirstTool      = "g"   // First tool message
	ShortcutLastTool       = "G"   // Last tool message
	ShortcutClearSelection = "esc" // Clear tool selection
)

// ToolShortcutMsg is sent when a tool shortcut is triggered
type ToolShortcutMsg struct {
	Shortcut string
	ToolIdx  int // Index of the tool message, -1 if no specific tool selected
}

// ClipboardCopyMsg is sent when content is copied to clipboard
type ClipboardCopyMsg struct {
	Content string
	Success bool
	Error   string
}

// SetToolCount updates the count of tool messages
func (ts *ToolShortcuts) SetToolCount(count int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.totalToolMessages = count

	// Reset selection if out of bounds
	if ts.selectedToolIndex >= count {
		ts.selectedToolIndex = count - 1
	}
	if ts.selectedToolIndex < 0 && count > 0 {
		ts.selectedToolIndex = 0
	}
}

// GetSelectedIndex returns the currently selected tool index
func (ts *ToolShortcuts) GetSelectedIndex() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.selectedToolIndex
}

// SelectNext selects the next tool message
func (ts *ToolShortcuts) SelectNext() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.totalToolMessages == 0 {
		return false
	}

	if ts.selectedToolIndex < ts.totalToolMessages-1 {
		ts.selectedToolIndex++
		return true
	}
	return false
}

// SelectPrev selects the previous tool message
func (ts *ToolShortcuts) SelectPrev() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.totalToolMessages == 0 {
		return false
	}

	if ts.selectedToolIndex > 0 {
		ts.selectedToolIndex--
		return true
	}
	return false
}

// SelectFirst selects the first tool message
func (ts *ToolShortcuts) SelectFirst() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.totalToolMessages > 0 {
		ts.selectedToolIndex = 0
		return true
	}
	return false
}

// SelectLast selects the last tool message
func (ts *ToolShortcuts) SelectLast() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.totalToolMessages > 0 {
		ts.selectedToolIndex = ts.totalToolMessages - 1
		return true
	}
	return false
}

// ClearSelection clears the current selection
func (ts *ToolShortcuts) ClearSelection() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.selectedToolIndex = -1
}

// HasSelection returns true if a tool is currently selected
func (ts *ToolShortcuts) HasSelection() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.selectedToolIndex >= 0 && ts.selectedToolIndex < ts.totalToolMessages
}

// HandleKey processes keyboard shortcuts for tool interactions
// Returns true if the key was handled
func (ts *ToolShortcuts) HandleKey(key string, toolMode bool) (ToolShortcutMsg, bool) {
	// If in tool mode or the key is a tool-specific shortcut
	if toolMode || ts.HasSelection() {
		switch key {
		case ShortcutExpandCollapse:
			return ToolShortcutMsg{Shortcut: "expand_collapse", ToolIdx: ts.GetSelectedIndex()}, true
		case "ctrl+o", "alt+o":
			return ToolShortcutMsg{Shortcut: "expand_all", ToolIdx: -1}, true
		case ShortcutCollapseAll:
			return ToolShortcutMsg{Shortcut: "collapse_all", ToolIdx: -1}, true
		case ShortcutCopyOutput:
			return ToolShortcutMsg{Shortcut: "copy_output", ToolIdx: ts.GetSelectedIndex()}, true
		case ShortcutCopyFullResult:
			return ToolShortcutMsg{Shortcut: "copy_full", ToolIdx: ts.GetSelectedIndex()}, true
		case ShortcutNextTool:
			if ts.SelectNext() {
				return ToolShortcutMsg{Shortcut: "select", ToolIdx: ts.GetSelectedIndex()}, true
			}
			return ToolShortcutMsg{}, false
		case ShortcutPrevTool:
			if ts.SelectPrev() {
				return ToolShortcutMsg{Shortcut: "select", ToolIdx: ts.GetSelectedIndex()}, true
			}
			return ToolShortcutMsg{}, false
		case ShortcutFirstTool:
			if ts.SelectFirst() {
				return ToolShortcutMsg{Shortcut: "select", ToolIdx: ts.GetSelectedIndex()}, true
			}
			return ToolShortcutMsg{}, false
		case ShortcutLastTool:
			if ts.SelectLast() {
				return ToolShortcutMsg{Shortcut: "select", ToolIdx: ts.GetSelectedIndex()}, true
			}
			return ToolShortcutMsg{}, false
		case ShortcutClearSelection:
			ts.ClearSelection()
			return ToolShortcutMsg{Shortcut: "clear_selection", ToolIdx: -1}, true
		}
	}

	// Navigation shortcuts that work even without selection
	switch key {
	case ShortcutNextTool:
		if ts.SelectNext() {
			return ToolShortcutMsg{Shortcut: "select", ToolIdx: ts.GetSelectedIndex()}, true
		}
	case ShortcutPrevTool:
		if ts.SelectPrev() {
			return ToolShortcutMsg{Shortcut: "select", ToolIdx: ts.GetSelectedIndex()}, true
		}
	}

	return ToolShortcutMsg{}, false
}

// ToolShortcutHandler handles tool shortcut messages
type ToolShortcutHandler struct {
	shortcuts *ToolShortcuts
}

// NewToolShortcutHandler creates a new handler
func NewToolShortcutHandler() *ToolShortcutHandler {
	return &ToolShortcutHandler{
		shortcuts: NewToolShortcuts(),
	}
}

// GetShortcuts returns the shortcuts manager
func (tsh *ToolShortcutHandler) GetShortcuts() *ToolShortcuts {
	return tsh.shortcuts
}

// Handle processes tool shortcut messages
func (tsh *ToolShortcutHandler) Handle(msg ToolShortcutMsg, messages []message) tea.Cmd {
	switch msg.Shortcut {
	case "expand_collapse":
		return tsh.handleExpandCollapse(msg.ToolIdx, messages)
	case "expand_all":
		return tsh.handleExpandAll(messages)
	case "collapse_all":
		return tsh.handleCollapseAll(messages)
	case "copy_output":
		return tsh.handleCopyOutput(msg.ToolIdx, messages)
	case "copy_full":
		return tsh.handleCopyFullResult(msg.ToolIdx, messages)
	default:
		return nil
	}
}

func (tsh *ToolShortcutHandler) handleExpandCollapse(idx int, messages []message) tea.Cmd {
	if idx < 0 || idx >= len(messages) {
		return nil
	}

	msg := &messages[idx]
	if !msg.isCollapsible {
		return nil
	}

	// Toggle collapse state
	msg.isCollapsed = !msg.isCollapsed

	return func() tea.Msg {
		return ToolShortcutMsg{Shortcut: "refresh", ToolIdx: idx}
	}
}

func (tsh *ToolShortcutHandler) handleExpandAll(messages []message) tea.Cmd {
	for i := range messages {
		if messages[i].isCollapsible {
			messages[i].isCollapsed = false
		}
	}

	return func() tea.Msg {
		return ToolShortcutMsg{Shortcut: "refresh", ToolIdx: -1}
	}
}

func (tsh *ToolShortcutHandler) handleCollapseAll(messages []message) tea.Cmd {
	for i := range messages {
		if messages[i].isCollapsible {
			messages[i].isCollapsed = true
		}
	}

	return func() tea.Msg {
		return ToolShortcutMsg{Shortcut: "refresh", ToolIdx: -1}
	}
}

func (tsh *ToolShortcutHandler) handleCopyOutput(idx int, messages []message) tea.Cmd {
	if idx < 0 || idx >= len(messages) {
		return func() tea.Msg {
			return ClipboardCopyMsg{Success: false, Error: "No tool message selected"}
		}
	}

	msg := messages[idx]
	content := msg.content

	// If collapsed and has full result, copy the full result
	if msg.isCollapsed && msg.fullResult != "" {
		content = msg.fullResult
	}

	return copyToClipboard(content)
}

func (tsh *ToolShortcutHandler) handleCopyFullResult(idx int, messages []message) tea.Cmd {
	if idx < 0 || idx >= len(messages) {
		return func() tea.Msg {
			return ClipboardCopyMsg{Success: false, Error: "No tool message selected"}
		}
	}

	msg := messages[idx]
	content := msg.fullResult
	if content == "" {
		content = msg.content
	}

	return copyToClipboard(content)
}

// copyToClipboard copies content to system clipboard
func copyToClipboard(content string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.Init()
		if err != nil {
			return ClipboardCopyMsg{
				Success: false,
				Error:   fmt.Sprintf("Failed to initialize clipboard: %v", err),
			}
		}

		clipboard.Write(clipboard.FmtText, []byte(content))

		return ClipboardCopyMsg{
			Content: truncateForDisplay(content, 50),
			Success: true,
		}
	}
}

// truncateForDisplay truncates content for display in status messages
func truncateForDisplay(s string, maxLen int) string {
	lines := strings.Split(s, "\n")
	firstLine := lines[0]
	if len(firstLine) > maxLen {
		return firstLine[:maxLen] + "..."
	}
	return firstLine
}

// RenderHelp renders the keyboard shortcuts help
func RenderHelp() string {
	var sb strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170"))

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	sb.WriteString(headerStyle.Render("Tool Call Keyboard Shortcuts"))
	sb.WriteString("\n\n")

	shortcuts := []struct {
		key  string
		desc string
	}{
		{"e", "Expand/collapse selected tool result"},
		{"ctrl+o / alt+o", "Expand all tool results"},
		{"C", "Collapse all tool results"},
		{"y", "Copy tool output to clipboard"},
		{"Y", "Copy full result to clipboard"},
		{"j", "Select next tool message"},
		{"k", "Select previous tool message"},
		{"g", "Select first tool message"},
		{"G", "Select last tool message"},
		{"esc", "Clear tool selection"},
	}

	for _, sc := range shortcuts {
		sb.WriteString(keyStyle.Render(fmt.Sprintf("  %-10s", sc.key)))
		sb.WriteString(descStyle.Render(sc.desc))
		sb.WriteString("\n")
	}

	return sb.String()
}

// IsToolMessage returns true if the message is a tool message
func IsToolMessage(msg message) bool {
	return msg.role == "Tool"
}

// CountToolMessages counts tool messages in a slice
func CountToolMessages(messages []message) int {
	count := 0
	for _, msg := range messages {
		if IsToolMessage(msg) {
			count++
		}
	}
	return count
}

// GetToolMessageIndex returns the actual index of the nth tool message
func GetToolMessageIndex(messages []message, toolIdx int) int {
	count := 0
	for i, msg := range messages {
		if IsToolMessage(msg) {
			if count == toolIdx {
				return i
			}
			count++
		}
	}
	return -1
}
