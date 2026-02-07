package tui

import (
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/tools"
)

func TestIsParallelToolCall(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     bool
	}{
		{"parallel_tool_execution", tools.ToolNameParallel, true},
		{"read_file", tools.ToolNameReadFile, false},
		{"shell", tools.ToolNameShell, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsParallelToolCall(tt.toolName)
			if got != tt.want {
				t.Errorf("IsParallelToolCall(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestExtractParallelToolCalls(t *testing.T) {
	tests := []struct {
		name      string
		params    map[string]interface{}
		wantCount int
		wantFirst string
	}{
		{
			name: "valid tool calls",
			params: map[string]interface{}{
				"tool_calls": []interface{}{
					map[string]interface{}{
						"name":       "read_file",
						"parameters": map[string]interface{}{"path": "test.go"},
					},
					map[string]interface{}{
						"name":       "search_files",
						"parameters": map[string]interface{}{"pattern": "*.go"},
					},
				},
			},
			wantCount: 2,
			wantFirst: "read_file",
		},
		{
			name:      "no tool_calls",
			params:    map[string]interface{}{},
			wantCount: 0,
			wantFirst: "",
		},
		{
			name: "invalid tool_calls type",
			params: map[string]interface{}{
				"tool_calls": "not an array",
			},
			wantCount: 0,
			wantFirst: "",
		},
		{
			name: "empty tool_calls",
			params: map[string]interface{}{
				"tool_calls": []interface{}{},
			},
			wantCount: 0,
			wantFirst: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractParallelToolCalls(tt.params)
			if len(got) != tt.wantCount {
				t.Errorf("ExtractParallelToolCalls() returned %d calls, want %d", len(got), tt.wantCount)
			}
			if tt.wantCount > 0 && got[0].Name != tt.wantFirst {
				t.Errorf("ExtractParallelToolCalls() first call = %q, want %q", got[0].Name, tt.wantFirst)
			}
		})
	}
}

func TestToolGroupManager(t *testing.T) {
	gm := NewToolGroupManager()

	// Test CreateGroup
	group := gm.CreateGroup(GroupConfig{
		Name:     "Test Group",
		ToolType: ToolTypeParallel,
		TabIdx:   0,
	})

	if group == nil {
		t.Fatal("CreateGroup returned nil")
	}

	if group.Name != "Test Group" {
		t.Errorf("Group name = %q, want %q", group.Name, "Test Group")
	}

	if group.ToolType != ToolTypeParallel {
		t.Errorf("Group type = %v, want %v", group.ToolType, ToolTypeParallel)
	}

	// Test GetGroup
	retrieved, ok := gm.GetGroup(group.ID)
	if !ok {
		t.Error("GetGroup returned not found for existing group")
	}
	if retrieved.ID != group.ID {
		t.Error("GetGroup returned wrong group")
	}

	// Test AddMessageToGroup
	toolMsg := &ToolCallMessage{
		ToolName: "read_file",
		ToolID:   "test-1",
		State:    ToolStateRunning,
	}

	if !gm.AddMessageToGroup(group.ID, toolMsg) {
		t.Error("AddMessageToGroup returned false for valid group")
	}

	// Test UpdateGroupState
	gm.UpdateGroupState(group.ID)

	completed, total := group.GetProgress()
	if total != 1 {
		t.Errorf("Group total = %d, want 1", total)
	}
	if completed != 0 {
		t.Errorf("Group completed = %d, want 0 (tool still running)", completed)
	}

	// Test ToggleGroupExpansion
	// Group starts expanded, so ToggleGroupExpansion returns false (new collapsed state)
	newState := gm.ToggleGroupExpansion(group.ID)
	if newState {
		t.Error("ToggleGroupExpansion should return false (collapsed state)")
	}
	if group.IsExpanded() {
		t.Error("Group should be collapsed after toggle")
	}

	// Toggle again to expand
	newState = gm.ToggleGroupExpansion(group.ID)
	if !newState {
		t.Error("ToggleGroupExpansion should return true (expanded state)")
	}
	if !group.IsExpanded() {
		t.Error("Group should be expanded after second toggle")
	}

	// Test GetGroupsForTab
	groups := gm.GetGroupsForTab(0)
	if len(groups) != 1 {
		t.Errorf("GetGroupsForTab returned %d groups, want 1", len(groups))
	}

	// Test RemoveGroup
	gm.RemoveGroup(group.ID)
	_, ok = gm.GetGroup(group.ID)
	if ok {
		t.Error("GetGroup returned found after RemoveGroup")
	}
}

func TestToolGroupGetProgress(t *testing.T) {
	group := &ToolGroup{
		ID:   "test",
		Name: "Test",
		Messages: []*ToolCallMessage{
			{ToolName: "tool1", State: ToolStateCompleted},
			{ToolName: "tool2", State: ToolStateRunning},
			{ToolName: "tool3", State: ToolStateFailed},
			{ToolName: "tool4", State: ToolStatePending},
		},
	}

	completed, total := group.GetProgress()

	if total != 4 {
		t.Errorf("Total = %d, want 4", total)
	}
	if completed != 2 {
		t.Errorf("Completed = %d, want 2 (completed + failed)", completed)
	}
}

func TestGroupFormatter(t *testing.T) {
	gm := NewToolGroupManager()
	group := gm.CreateGroup(GroupConfig{
		Name:     "Parallel Execution",
		ToolType: ToolTypeParallel,
		TabIdx:   0,
	})

	// Add some tool messages
	gm.AddMessageToGroup(group.ID, &ToolCallMessage{
		ToolName: "read_file",
		ToolID:   "1",
		State:    ToolStateCompleted,
	})
	gm.AddMessageToGroup(group.ID, &ToolCallMessage{
		ToolName: "search_files",
		ToolID:   "2",
		State:    ToolStateRunning,
	})

	gm.UpdateGroupState(group.ID)

	gf := NewGroupFormatter()

	// Test FormatGroupHeader
	header := gf.FormatGroupHeader(group)
	if header == "" {
		t.Error("FormatGroupHeader returned empty string")
	}

	// Check that header contains expected elements
	if !containsAnyStr(header, []string{"Parallel", "⚡", "[1/2]"}) {
		t.Errorf("FormatGroupHeader missing expected elements: %q", header)
	}

	// Test FormatGroupSummary
	summary := gf.FormatGroupSummary(group)
	if summary == "" {
		t.Error("FormatGroupSummary returned empty string")
	}

	// Check that summary contains tool names
	if !containsAnyStr(summary, []string{"read_file", "search_files"}) {
		t.Errorf("FormatGroupSummary missing tool names: %q", summary)
	}
}

func containsAnyStr(s string, substrs []string) bool {
	for _, substr := range substrs {
		if containsStr(s, substr) {
			return true
		}
	}
	return false
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}
