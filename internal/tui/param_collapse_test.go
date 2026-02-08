package tui

import (
	"strings"
	"testing"
)

func TestParamCollapseManager_ShouldAutoCollapse(t *testing.T) {
	config := DefaultParamCollapseConfig()
	manager := NewParamCollapseManager(config)

	tests := []struct {
		name     string
		params   map[string]interface{}
		expected bool
	}{
		{
			name:     "empty params",
			params:   map[string]interface{}{},
			expected: false,
		},
		{
			name:     "few params",
			params:   map[string]interface{}{"path": "test.txt", "line": 1},
			expected: false,
		},
		{
			name: "many params",
			params: map[string]interface{}{
				"path":     "test.txt",
				"line":     1,
				"column":   5,
				"context":  3,
				"encoding": "utf-8",
				"timeout":  30,
				"retries":  3,
			},
			expected: true,
		},
		{
			name: "large content",
			params: map[string]interface{}{
				"path":    "test.txt",
				"content": strings.Repeat("This is a very long content string that exceeds the total character limit. ", 20),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.ShouldAutoCollapse("test-tool-id", tt.params)
			if got != tt.expected {
				t.Errorf("ShouldAutoCollapse() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParamCollapseManager_ToggleCollapse(t *testing.T) {
	config := DefaultParamCollapseConfig()
	manager := NewParamCollapseManager(config)

	params := map[string]interface{}{
		"path": "test.txt",
		"line": 1,
	}

	// Get initial state (should be collapsed based on auto-collapse logic)
	state := manager.GetInitialState("test-tool", params)
	initialCollapsed := state.IsCollapsed

	// Toggle should flip the state
	newState := manager.ToggleCollapse("test-tool")
	if newState == initialCollapsed {
		t.Errorf("ToggleCollapse should flip the collapsed state: initial=%v, after toggle=%v", initialCollapsed, newState)
	}

	// Toggle again should flip back
	newState = manager.ToggleCollapse("test-tool")
	if newState != initialCollapsed {
		t.Errorf("Second toggle should restore original state: initial=%v, after second toggle=%v", initialCollapsed, newState)
	}
}

func TestParamCollapseManager_GetVisibleParams(t *testing.T) {
	config := DefaultParamCollapseConfig()
	config.MaxVisibleParams = 2
	manager := NewParamCollapseManager(config)

	params := map[string]interface{}{
		"path":     "test.txt", // Important
		"line":     1,
		"column":   5,
		"encoding": "utf-8",
	}

	// When collapsed, should only show important params + fill up to MaxVisibleParams
	manager.GetInitialState("test-tool", params)
	manager.SetCollapse("test-tool", true)

	visible := manager.GetVisibleParams("test-tool", params)

	// Path should always be visible (it's important)
	if _, ok := visible["path"]; !ok {
		t.Error("path should be visible (important param)")
	}

	// Should not exceed MaxVisibleParams
	if len(visible) > config.MaxVisibleParams {
		t.Errorf("visible params count %d should not exceed MaxVisibleParams %d", len(visible), config.MaxVisibleParams)
	}

	// When expanded, should show all
	manager.SetCollapse("test-tool", false)
	visible = manager.GetVisibleParams("test-tool", params)
	if len(visible) != len(params) {
		t.Errorf("expanded visible params %d should equal total params %d", len(visible), len(params))
	}
}

func TestParamCollapseManager_GetHiddenCount(t *testing.T) {
	config := DefaultParamCollapseConfig()
	config.MaxVisibleParams = 2
	manager := NewParamCollapseManager(config)

	params := map[string]interface{}{
		"path":     "test.txt",
		"line":     1,
		"column":   5,
		"encoding": "utf-8",
	}

	// When collapsed, some params should be hidden
	manager.GetInitialState("test-tool", params)
	manager.SetCollapse("test-tool", true)

	hidden := manager.GetHiddenCount("test-tool", params)
	if hidden <= 0 {
		t.Error("some params should be hidden when collapsed")
	}

	// When expanded, no params should be hidden
	manager.SetCollapse("test-tool", false)
	hidden = manager.GetHiddenCount("test-tool", params)
	if hidden != 0 {
		t.Errorf("no params should be hidden when expanded, got %d", hidden)
	}
}

func TestCollapseToggleLabel(t *testing.T) {
	tests := []struct {
		isCollapsed bool
		hiddenCount int
		expected    string
	}{
		{true, 5, "▶ show more"},
		{true, 0, "▶ expand"},
		{false, 0, "▼ collapse"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := CollapseToggleLabel(tt.isCollapsed, tt.hiddenCount)
			if got != tt.expected {
				t.Errorf("CollapseToggleLabel(%v, %d) = %q, want %q", tt.isCollapsed, tt.hiddenCount, got, tt.expected)
			}
		})
	}
}

func TestDefaultParamCollapseConfig(t *testing.T) {
	config := DefaultParamCollapseConfig()

	if config.MaxVisibleParams <= 0 {
		t.Error("MaxVisibleParams should be positive")
	}
	if config.MaxParamValueLength <= 0 {
		t.Error("MaxParamValueLength should be positive")
	}
	if config.MaxTotalContentChars <= 0 {
		t.Error("MaxTotalContentChars should be positive")
	}
	if config.ImportantParams == nil {
		t.Error("ImportantParams should not be nil")
	}

	// Check that common important params are set
	importantParams := []string{"path", "command", "url", "query", "action"}
	for _, param := range importantParams {
		if !config.ImportantParams[param] {
			t.Errorf("%q should be in ImportantParams", param)
		}
	}
}

func TestFormatParamValueSimple(t *testing.T) {
	tests := []struct {
		value    interface{}
		contains string
	}{
		{string("hello"), "hello"},
		{bool(true), "true"},
		{bool(false), "false"},
		{float64(42.5), "42.5"},
		{int(123), "123"},
		{[]interface{}{1, 2, 3}, "[3 items]"},
		{map[string]interface{}{"a": 1}, "{1 keys}"},
		{nil, "null"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := formatParamValueSimple(tt.value)
			if got == "" {
				t.Error("formatParamValueSimple should not return empty string")
			}
		})
	}
}
