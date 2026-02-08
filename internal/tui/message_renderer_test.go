package tui

import (
	"strings"
	"testing"
)

func createTestMessage(role string, content string) message {
	return message{
		role:          role,
		content:       content,
		timestamp:     "12:34:56",
		toolName:      "",
		toolID:        "",
		toolState:     ToolStateCompleted,
		toolType:      ToolTypeUnknown,
		isCollapsible: false,
		isCollapsed:   false,
		progress:      -1,
	}
}

func createTestToolMessage(toolName string, content string, state ToolState) message {
	return message{
		role:          "Tool",
		content:       content,
		timestamp:     "12:34:56",
		toolName:      toolName,
		toolID:        "tool-123",
		toolState:     state,
		toolType:      GetToolTypeFromName(toolName),
		isCollapsible: true,
		isCollapsed:   false,
		progress:      -1,
	}
}

func TestRenderMessage(t *testing.T) {
	msg := createTestMessage("You", "Hello")
	mr := NewMessageRenderer(80, 80)
	result := mr.RenderMessage(msg, 0)

	if !strings.Contains(result, "You") {
		t.Error("expected 'You' in output")
	}
}

func TestRenderHeader(t *testing.T) {
	msg := createTestMessage("You", "")
	mr := NewMessageRenderer(80, 80)
	result := mr.RenderHeader(msg)

	if !strings.Contains(result, "You") {
		t.Error("expected 'You' in header")
	}
}

func TestRenderContent(t *testing.T) {
	msg := createTestMessage("You", "Hello")
	mr := NewMessageRenderer(80, 80)
	result := mr.RenderContent(msg)

	if !strings.Contains(result, "Hello") {
		t.Error("expected 'Hello' in content")
	}
}

func TestRenderReasoning(t *testing.T) {
	mr := NewMessageRenderer(80, 80)
	result := mr.RenderReasoning("Thinking...")

	if !strings.Contains(result, "Thinking:") {
		t.Error("expected 'Thinking:'")

	}
	if !strings.Contains(result, "---") {
		t.Error("expected '---'")
	}
}

func TestSetWidth(t *testing.T) {
	mr := NewMessageRenderer(80, 80)
	mr.SetWidth(100, 90)

	if mr.contentWidth != 100 {
		t.Errorf("contentWidth = %d, want 100", mr.contentWidth)
	}
	if mr.renderWrapWidth != 90 {
		t.Errorf("renderWrapWidth = %d, want 90", mr.renderWrapWidth)
	}
}

func TestUpdateMessageProgress(t *testing.T) {
	mr := NewMessageRenderer(80, 80)
	msg := createTestToolMessage("read", "", ToolStatePending)

	mr.UpdateMessageProgress(&msg, 0.5, "processing...")
	if msg.progress != 0.5 {
		t.Errorf("progress = %f, want 0.5", msg.progress)
	}
	if msg.toolState != ToolStateRunning {
		t.Errorf("state = %v, want Running", msg.toolState)
	}
}

func TestToggleCollapse(t *testing.T) {
	mr := NewMessageRenderer(80, 80)
	msg := createTestToolMessage("read", "", ToolStateCompleted)
	msg.isCollapsible = true

	result := mr.ToggleCollapse(&msg)
	if !result {
		t.Error("expected true")
	}
	if !msg.isCollapsed {
		t.Error("expected collapsed true")
	}
}
