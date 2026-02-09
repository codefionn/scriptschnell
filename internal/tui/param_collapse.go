package tui

import (
	"fmt"
	"strings"
	"sync"
)

// ParamCollapseConfig holds configuration for parameter collapsing
type ParamCollapseConfig struct {
	// Thresholds for auto-collapsing
	MaxVisibleParams     int // Maximum params to show before collapsing (default: 4)
	MaxParamValueLength  int // Maximum value length before truncating (default: 60)
	MaxTotalContentChars int // Maximum total content chars before collapsing (default: 300)

	// Importance weights for parameters
	ImportantParams map[string]bool // Parameters that should always be visible
}

// DefaultParamCollapseConfig returns the default configuration
func DefaultParamCollapseConfig() *ParamCollapseConfig {
	return &ParamCollapseConfig{
		MaxVisibleParams:     4,
		MaxParamValueLength:  60,
		MaxTotalContentChars: 300,
		ImportantParams: map[string]bool{
			// File operations
			"path": true,
			// Shell operations
			"command": true,
			// Web operations
			"url":     true,
			"query":   true,
			"queries": true,
			// Sandbox
			"code":        true,
			"description": true,
			// Todo
			"action": true,
			"text":   true,
			// Search
			"pattern": true,
			// General
			"job_id": true,
		},
	}
}

// ParamCollapseState tracks the collapse state for a single tool interaction
type ParamCollapseState struct {
	ToolID       string
	IsCollapsed  bool
	VisibleCount int // How many params are currently visible
	TotalCount   int // Total number of params
}

// ParamCollapseManager manages parameter collapse states across the application
type ParamCollapseManager struct {
	mu     sync.RWMutex
	states map[string]*ParamCollapseState // keyed by toolID
	config *ParamCollapseConfig
}

// NewParamCollapseManager creates a new collapse manager
func NewParamCollapseManager(config *ParamCollapseConfig) *ParamCollapseManager {
	if config == nil {
		config = DefaultParamCollapseConfig()
	}
	return &ParamCollapseManager{
		states: make(map[string]*ParamCollapseState),
		config: config,
	}
}

// ShouldAutoCollapse determines if parameters should be automatically collapsed
func (m *ParamCollapseManager) ShouldAutoCollapse(toolID string, params map[string]interface{}) bool {
	if len(params) == 0 {
		return false
	}

	// Check parameter count
	if len(params) > m.config.MaxVisibleParams+2 { // Allow a bit of buffer
		return true
	}

	// Check total content size
	totalChars := 0
	for key, value := range params {
		totalChars += len(key)
		totalChars += len(formatParamValueSimple(value))
	}
	return totalChars > m.config.MaxTotalContentChars
}

// GetInitialState creates initial collapse state for a tool
func (m *ParamCollapseManager) GetInitialState(toolID string, params map[string]interface{}) *ParamCollapseState {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if state already exists
	if state, exists := m.states[toolID]; exists {
		return state
	}

	// Create new state
	state := &ParamCollapseState{
		ToolID:       toolID,
		IsCollapsed:  m.ShouldAutoCollapse(toolID, params),
		VisibleCount: m.calculateVisibleCount(params),
		TotalCount:   len(params),
	}

	m.states[toolID] = state
	return state
}

// ToggleCollapse toggles the collapse state for a tool
func (m *ParamCollapseManager) ToggleCollapse(toolID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state, exists := m.states[toolID]; exists {
		state.IsCollapsed = !state.IsCollapsed
		if !state.IsCollapsed {
			state.VisibleCount = state.TotalCount
		} else {
			state.VisibleCount = paramMin(m.config.MaxVisibleParams, state.TotalCount)
		}
		return state.IsCollapsed
	}
	return false
}

// SetCollapse sets the collapse state for a tool
func (m *ParamCollapseManager) SetCollapse(toolID string, collapsed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state, exists := m.states[toolID]; exists {
		state.IsCollapsed = collapsed
		if !collapsed {
			state.VisibleCount = state.TotalCount
		} else {
			state.VisibleCount = paramMin(m.config.MaxVisibleParams, state.TotalCount)
		}
	}
}

// GetState returns the current collapse state for a tool
func (m *ParamCollapseManager) GetState(toolID string) *ParamCollapseState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[toolID]
}

// IsCollapsed returns whether a tool's parameters are collapsed
func (m *ParamCollapseManager) IsCollapsed(toolID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if state, exists := m.states[toolID]; exists {
		return state.IsCollapsed
	}
	return false
}

// GetVisibleParams returns the parameters that should be visible for a tool
func (m *ParamCollapseManager) GetVisibleParams(toolID string, params map[string]interface{}) map[string]interface{} {
	state := m.GetState(toolID)
	if state == nil {
		state = m.GetInitialState(toolID, params)
	}

	if !state.IsCollapsed {
		return params
	}

	// When collapsed, show important params first, then fill with others
	visible := make(map[string]interface{})
	importantCount := 0
	otherCount := 0

	// First pass: add important params
	for key, value := range params {
		if m.config.ImportantParams[key] && importantCount < m.config.MaxVisibleParams {
			visible[key] = value
			importantCount++
		}
	}

	// Second pass: add other params up to limit
	for key, value := range params {
		if !m.config.ImportantParams[key] && importantCount+otherCount < m.config.MaxVisibleParams {
			visible[key] = value
			otherCount++
		}
	}

	return visible
}

// GetHiddenCount returns the number of hidden parameters
func (m *ParamCollapseManager) GetHiddenCount(toolID string, params map[string]interface{}) int {
	state := m.GetState(toolID)
	if state == nil {
		state = m.GetInitialState(toolID, params)
	}

	if !state.IsCollapsed {
		return 0
	}

	return state.TotalCount - len(m.GetVisibleParams(toolID, params))
}

// ClearState removes state for a tool (cleanup)
func (m *ParamCollapseManager) ClearState(toolID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, toolID)
}

// ClearAll removes all states (cleanup)
func (m *ParamCollapseManager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = make(map[string]*ParamCollapseState)
}

// calculateVisibleCount calculates how many params should be visible
func (m *ParamCollapseManager) calculateVisibleCount(params map[string]interface{}) int {
	importantCount := 0
	for key := range params {
		if m.config.ImportantParams[key] {
			importantCount++
		}
	}
	return paramMin(m.config.MaxVisibleParams, paramMax(importantCount, len(params)))
}

// CollapseToggleLabel returns the label for the toggle button
func CollapseToggleLabel(isCollapsed bool, hiddenCount int) string {
	if isCollapsed {
		if hiddenCount > 0 {
			return "▶ show more"
		}
		return "▶ expand"
	}
	return "▼ collapse"
}

// formatParamValueSimple formats a parameter value as a simple string (without styling)
func formatParamValueSimple(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
	case int:
		return fmt.Sprintf("%d", v)
	case []interface{}:
		return fmt.Sprintf("[%d items]", len(v))
	case map[string]interface{}:
		return fmt.Sprintf("{%d keys}", len(v))
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// Helper functions for min/max - note: min is also defined in tool_results.go
// Using a local copy to avoid import cycles
func paramMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func paramMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
