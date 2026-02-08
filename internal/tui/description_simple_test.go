// Tests for description field display in TUI
// These tests verify that the description field is properly propagated and displayed

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTUIDescriptionMessageCreation(t *testing.T) {
	// Test that description field is properly set in the TUI message

	// Create a message with description
	msg := message{
		role:        "Tool",
		content:     "Executing go_sandbox",
		description: "Building and testing",
		toolName:    "go_sandbox",
	}

	// Verify description field is set
	assert.Equal(t, "Building and testing", msg.description)
}

func TestTUIDescriptionEmptyFallback(t *testing.T) {
	// Test that empty description doesn't break anything

	// Create a message without description
	msg := message{
		role:        "Tool",
		content:     "Executing go_sandbox",
		toolName:    "go_sandbox",
		description: "",
	}

	// Verify empty description is handled gracefully
	assert.Empty(t, msg.description)
}

func TestTUIDescriptionSpecialCharacters(t *testing.T) {
	// Test that descriptions with special characters work

	tests := []struct {
		name        string
		description string
	}{
		{"Unicode", "Building & testing"},
		{"Quote", `Testing "quoted" values`},
		{"Braces", "{special} values here"},
		{"Long", "test description that is longer than usual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := message{
				role:        "Tool",
				content:     "Executing tool",
				description: tt.description,
			}

			// Verify description is stored correctly
			assert.Equal(t, tt.description, msg.description)
		})
	}
}

func TestToolProgressStateWithDescription(t *testing.T) {
	// Test that ToolProgressState stores and handles description correctly
	tracker := NewToolProgressTracker()

	// Start a tool with description
	state := tracker.StartTool("test-id-1", "go_sandbox", "Building and testing changes")
	assert.NotNil(t, state)
	assert.Equal(t, "go_sandbox", state.ToolName)
	assert.Equal(t, "Building and testing changes", state.Description)

	// Start a tool without description
	state2 := tracker.StartTool("test-id-2", "read_file", "")
	assert.NotNil(t, state2)
	assert.Equal(t, "read_file", state2.ToolName)
	assert.Empty(t, state2.Description)
}

func TestFormatToolProgressWithDescription(t *testing.T) {
	// Test that FormatToolProgress includes description in output
	pf := NewProgressFormatter()

	// Create a state with description
	state := &ToolProgressState{
		ToolName:    "go_sandbox",
		Description: "Running tests for new feature",
		Status:      "compiling",
		StartTime:   time.Now(),
	}

	result := pf.FormatToolProgress(state, true)

	// Verify description is included in output
	assert.True(t, strings.Contains(result, "go_sandbox"))
	assert.True(t, strings.Contains(result, "Running tests for new feature"))
}

func TestFormatToolProgressWithoutDescription(t *testing.T) {
	// Test that FormatToolProgress works without description
	pf := NewProgressFormatter()

	// Create a state without description
	state := &ToolProgressState{
		ToolName:  "read_file",
		Status:    "reading",
		StartTime: time.Now(),
	}

	result := pf.FormatToolProgress(state, true)

	// Verify tool name is included but description placeholder is not
	assert.True(t, strings.Contains(result, "read_file"))
	// Should not contain empty parens
	assert.False(t, strings.Contains(result, "()"))
}

func TestToolCallMessageWithDescription(t *testing.T) {
	// Test that ToolCallMessage properly stores description
	msg := &ToolCallMessage{
		ToolName:    "go_sandbox",
		ToolID:      "test-123",
		ToolType:    ToolTypeGoSandbox,
		State:       ToolStateRunning,
		Description: "Building the project",
		Parameters: map[string]interface{}{
			"code": "package main\n\nfunc main() {}",
		},
	}

	assert.Equal(t, "go_sandbox", msg.ToolName)
	assert.Equal(t, "Building the project", msg.Description)
	assert.Equal(t, ToolStateRunning, msg.State)
}

func TestFormatCompactToolCallWithDescription(t *testing.T) {
	// Test that FormatCompactToolCall includes description
	ts := InitializeToolStyles()

	result := ts.FormatCompactToolCall(
		"go_sandbox",
		map[string]interface{}{"code": "test"},
		ToolStateRunning,
		"Running unit tests",
	)

	// Verify description is included
	assert.True(t, strings.Contains(result, "go_sandbox"))
	assert.True(t, strings.Contains(result, "Running unit tests"))
}

func TestFormatCompactToolCallWithoutDescription(t *testing.T) {
	// Test that FormatCompactToolCall works without description
	ts := InitializeToolStyles()

	result := ts.FormatCompactToolCall(
		"read_file",
		map[string]interface{}{"path": "/test/file.go"},
		ToolStateCompleted,
		"",
	)

	// Verify tool name is included
	assert.True(t, strings.Contains(result, "read_file"))
	// Should not crash or show empty parens
	assert.False(t, strings.Contains(result, "()"))
}
