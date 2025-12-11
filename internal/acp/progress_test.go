package acp

import (
	"context"
	"strings"
	"testing"

	"github.com/coder/acp-go-sdk"
)

// TestToolProgressAccumulation tests that tool progress is properly accumulated
// and included in the final tool result
func TestToolProgressAccumulation(t *testing.T) {
	agent := &ScriptschnellAIAgent{}

	session := &statcodeSession{
		sessionID:     "test-progress-accumulation",
		promptCtx:     context.Background(),
		toolLocations: make(map[string][]acp.ToolCallLocation),
		toolParams:    make(map[string]map[string]interface{}),
		toolProgress:  make(map[string]*strings.Builder),
	}

	toolID := "test-tool-123"

	// Send multiple progress messages
	progress1 := "Starting operation...\n"
	progress2 := "Processing step 1...\n"
	progress3 := "Processing step 2...\n"

	// Accumulate progress messages
	err := agent.sendToolCallProgress(session, toolID, progress1)
	if err != nil {
		t.Fatalf("Failed to send first progress: %v", err)
	}

	err = agent.sendToolCallProgress(session, toolID, progress2)
	if err != nil {
		t.Fatalf("Failed to send second progress: %v", err)
	}

	err = agent.sendToolCallProgress(session, toolID, progress3)
	if err != nil {
		t.Fatalf("Failed to send third progress: %v", err)
	}

	// Verify progress was accumulated
	session.mu.Lock()
	progressBuilder, exists := session.toolProgress[toolID]
	if !exists {
		t.Fatal("No progress was accumulated")
	}
	accumulated := progressBuilder.String()
	session.mu.Unlock()

	expectedAccumulated := progress1 + progress2 + progress3
	if accumulated != expectedAccumulated {
		t.Errorf("Expected accumulated progress: %q, got: %q", expectedAccumulated, accumulated)
	}

	// Test that popToolContext returns the accumulated progress
	params, locations, progressText := agent.popToolContext(session, toolID)

	if params != nil || len(locations) != 0 {
		t.Error("Expected empty params and locations")
	}

	if progressText != expectedAccumulated {
		t.Errorf("Expected progress text: %q, got: %q", expectedAccumulated, progressText)
	}

	// Verify the progress was cleaned up
	session.mu.Lock()
	_, exists = session.toolProgress[toolID]
	if exists {
		t.Error("Progress was not cleaned up after popToolContext")
	}
	session.mu.Unlock()
}

// TestToolProgressWithFinalResult tests that accumulated progress is combined
// with the final tool result correctly
func TestToolProgressWithFinalResult(t *testing.T) {
	agent := &ScriptschnellAIAgent{}

	session := &statcodeSession{
		sessionID:     "test-progress-with-result",
		promptCtx:     context.Background(),
		toolLocations: make(map[string][]acp.ToolCallLocation),
		toolParams:    make(map[string]map[string]interface{}),
		toolProgress:  make(map[string]*strings.Builder),
	}

	toolID := "test-tool-with-result-123"

	// Store some mock tool parameters
	agent.rememberToolContext(session, toolID, map[string]interface{}{
		"command": "echo hello",
	}, []acp.ToolCallLocation{})

	// Send progress messages
	progressMsg := "Executing command...\nOutput: hello\n"
	err := agent.sendToolCallProgress(session, toolID, progressMsg)
	if err != nil {
		t.Fatalf("Failed to send progress: %v", err)
	}

	// Test the combination logic by manually calling popToolContext
	// with the final result to simulate what handleToolCallResult does
	params, locations, progressText := agent.popToolContext(session, toolID)
	finalResult := "\nCommand completed successfully."

	// Verify progress was accumulated correctly
	if progressText != progressMsg {
		t.Errorf("Expected progress: %q, got: %q", progressMsg, progressText)
	}

	// Simulate the combined result logic from handleToolCallResult
	combinedResult := finalResult
	if progressText != "" {
		if combinedResult != "" {
			combinedResult = progressText + combinedResult
		} else {
			combinedResult = progressText
		}
	}

	expectedCombined := progressMsg + finalResult
	if combinedResult != expectedCombined {
		t.Errorf("Expected combined result: %q, got: %q", expectedCombined, combinedResult)
	}

	// Verify params and locations are also retrieved correctly
	if params == nil {
		t.Error("Expected params to be retrieved")
	}
	if len(locations) != 0 {
		t.Error("Expected empty locations")
	}
}

// TestMultipleToolProgresses tests that progress accumulation works correctly
// for multiple tools running concurrently
func TestMultipleToolProgresses(t *testing.T) {
	agent := &ScriptschnellAIAgent{}

	session := &statcodeSession{
		sessionID:     "test-multiple-progresses",
		promptCtx:     context.Background(),
		toolLocations: make(map[string][]acp.ToolCallLocation),
		toolParams:    make(map[string]map[string]interface{}),
		toolProgress:  make(map[string]*strings.Builder),
	}

	toolID1 := "tool-1"
	toolID2 := "tool-2"

	// Send progress to both tools
	err := agent.sendToolCallProgress(session, toolID1, "Tool 1 progress A\n")
	if err != nil {
		t.Fatalf("Failed to send progress to tool 1: %v", err)
	}

	err = agent.sendToolCallProgress(session, toolID2, "Tool 2 progress X\n")
	if err != nil {
		t.Fatalf("Failed to send progress to tool 2: %v", err)
	}

	err = agent.sendToolCallProgress(session, toolID1, "Tool 1 progress B\n")
	if err != nil {
		t.Fatalf("Failed to send more progress to tool 1: %v", err)
	}

	// Verify each tool has its own accumulated progress
	_, _, progress1 := agent.popToolContext(session, toolID1)
	if progress1 != "Tool 1 progress A\nTool 1 progress B\n" {
		t.Errorf("Invalid progress for tool 1: %q", progress1)
	}

	_, _, progress2 := agent.popToolContext(session, toolID2)
	if progress2 != "Tool 2 progress X\n" {
		t.Errorf("Invalid progress for tool 2: %q", progress2)
	}
}
