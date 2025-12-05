package features

import (
	"sync"
	"testing"
)

func TestNewFeatureFlags(t *testing.T) {
	flags := NewFeatureFlags()
	if flags == nil {
		t.Fatal("NewFeatureFlags returned nil")
	}

	// Planning should be enabled by default
	if !flags.IsPlanningEnabled() {
		t.Error("Planning should be enabled by default")
	}

	// All tools should be enabled by default
	if !flags.IsToolEnabled("read_file") {
		t.Error("read_file should be enabled by default")
	}
	if !flags.IsToolEnabled("unknown_tool") {
		t.Error("Unknown tools should be enabled by default")
	}
}

func TestIsToolEnabled_DefaultState(t *testing.T) {
	flags := NewFeatureFlags()

	// All tools should be enabled by default
	tools := []string{"read_file", "create_file", "shell", "web_search", "todo"}
	for _, tool := range tools {
		if !flags.IsToolEnabled(tool) {
			t.Errorf("Tool %s should be enabled by default", tool)
		}
	}
}

func TestEnableDisableTool(t *testing.T) {
	flags := NewFeatureFlags()

	// All tools enabled by default
	if !flags.IsToolEnabled("read_file") {
		t.Error("read_file should be enabled by default")
	}
	if !flags.IsToolEnabled("create_file") {
		t.Error("create_file should be enabled by default")
	}

	// Disable a specific tool
	flags.DisableTool("read_file")
	if flags.IsToolEnabled("read_file") {
		t.Error("read_file should be disabled after DisableTool")
	}

	// Other tools remain enabled
	if !flags.IsToolEnabled("create_file") {
		t.Error("create_file should still be enabled")
	}

	// Enable the tool again
	flags.EnableTool("read_file")
	if !flags.IsToolEnabled("read_file") {
		t.Error("read_file should be enabled after EnableTool")
	}

	// Disable another tool
	flags.DisableTool("create_file")
	if flags.IsToolEnabled("create_file") {
		t.Error("create_file should be disabled")
	}
}

func TestEnableAllTools(t *testing.T) {
	flags := NewFeatureFlags()

	// Disable some tools
	flags.DisableTool("read_file")
	flags.DisableTool("create_file")
	flags.DisableTool("shell")

	if flags.IsToolEnabled("read_file") {
		t.Error("read_file should be disabled")
	}
	if flags.IsToolEnabled("create_file") {
		t.Error("create_file should be disabled")
	}

	// Reset to all enabled
	flags.EnableAllTools()

	if !flags.IsToolEnabled("read_file") {
		t.Error("read_file should be enabled after EnableAllTools")
	}
	if !flags.IsToolEnabled("create_file") {
		t.Error("create_file should be enabled after EnableAllTools")
	}
	if !flags.IsToolEnabled("shell") {
		t.Error("shell should be enabled after EnableAllTools")
	}
}

func TestPlanningEnabled(t *testing.T) {
	flags := NewFeatureFlags()

	// Default should be true
	if !flags.IsPlanningEnabled() {
		t.Error("Planning should be enabled by default")
	}

	// Disable planning
	flags.SetPlanningEnabled(false)
	if flags.IsPlanningEnabled() {
		t.Error("Planning should be disabled after SetPlanningEnabled(false)")
	}

	// Enable planning
	flags.SetPlanningEnabled(true)
	if !flags.IsPlanningEnabled() {
		t.Error("Planning should be enabled after SetPlanningEnabled(true)")
	}
}

func TestUnknownTool(t *testing.T) {
	flags := NewFeatureFlags()

	// Unknown tools should default to enabled
	if !flags.IsToolEnabled("unknown_tool_xyz") {
		t.Error("Unknown tools should be enabled by default")
	}

	// Disabling unknown tool should be a no-op (doesn't change the flag)
	flags.DisableTool("unknown_tool_xyz")
	// Should still be enabled (no field to set)
	if !flags.IsToolEnabled("unknown_tool_xyz") {
		t.Error("Unknown tool should still be enabled (no-op)")
	}
}

func TestIndividualToolFlags(t *testing.T) {
	flags := NewFeatureFlags()

	// Test each known tool individually
	tools := []string{
		"read_file",
		"create_file",
		"write_file_diff",
		"write_file_replace",
		"write_file_json",
		"shell",
		"status_program",
		"wait_program",
		"stop_program",
		"parallel_tool_execution",
		"go_sandbox",
		"web_search",
		"tool_summarize",
		"todo",
		"search_files",
		"search_file_content",
		"codebase_investigator",
		"search_context_files",
		"grep_context_files",
		"read_context_file",
	}

	for _, tool := range tools {
		// Default enabled
		if !flags.IsToolEnabled(tool) {
			t.Errorf("Tool %s should be enabled by default", tool)
		}

		// Disable
		flags.DisableTool(tool)
		if flags.IsToolEnabled(tool) {
			t.Errorf("Tool %s should be disabled after DisableTool", tool)
		}

		// Enable
		flags.EnableTool(tool)
		if !flags.IsToolEnabled(tool) {
			t.Errorf("Tool %s should be enabled after EnableTool", tool)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	flags := NewFeatureFlags()
	var wg sync.WaitGroup

	// Test concurrent reads and writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			flags.EnableTool("read_file")
			flags.IsToolEnabled("read_file")
			flags.DisableTool("read_file")
			flags.IsToolEnabled("create_file")
			flags.SetPlanningEnabled(true)
			flags.IsPlanningEnabled()
		}()
	}

	wg.Wait()
	// If we get here without deadlock or race condition, test passes
}

func TestMixedToolStates(t *testing.T) {
	flags := NewFeatureFlags()

	// Disable some, enable others explicitly, leave some default
	flags.DisableTool("read_file")
	flags.DisableTool("create_file")
	flags.EnableTool("shell") // Explicitly enable (sets to true)

	if flags.IsToolEnabled("read_file") {
		t.Error("read_file should be disabled")
	}
	if flags.IsToolEnabled("create_file") {
		t.Error("create_file should be disabled")
	}
	if !flags.IsToolEnabled("shell") {
		t.Error("shell should be enabled")
	}
	if !flags.IsToolEnabled("web_search") {
		t.Error("web_search should be enabled (default)")
	}
}

func TestPublicFields(t *testing.T) {
	flags := NewFeatureFlags()

	// Test that public fields are accessible
	if !flags.PlanningEnabled {
		t.Error("PlanningEnabled should be true by default")
	}
	if !flags.ReadFile {
		t.Error("ReadFile should be true by default")
	}

	// Direct field access
	flags.ReadFile = false
	if flags.ReadFile {
		t.Error("ReadFile should be false after direct assignment")
	}

	// Method access should reflect direct changes (no lock needed for read-only test)
	if flags.IsToolEnabled("read_file") {
		t.Error("IsToolEnabled should return false for read_file")
	}
}
