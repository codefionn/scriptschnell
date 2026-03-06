package tui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// ToolGroup represents a group of related tool calls (e.g., parallel execution)
type ToolGroup struct {
	ID          string             // Unique group identifier
	Name        string             // Display name for the group
	ToolType    ToolType           // Type of group (parallel, sequential, etc.)
	State       ToolState          // Overall group state
	Messages    []*ToolCallMessage // Tool calls in this group
	CreatedAt   time.Time
	CompletedAt *time.Time
	isExpanded  bool // UI state: expanded or collapsed
	mu          sync.RWMutex
}

// ToolGroupManager manages tool groups across tabs
type ToolGroupManager struct {
	groups    map[string]*ToolGroup // groupID -> group
	tabGroups map[int][]string      // tabIdx -> []groupID
	mu        sync.RWMutex
}

// NewToolGroupManager creates a new tool group manager
func NewToolGroupManager() *ToolGroupManager {
	return &ToolGroupManager{
		groups:    make(map[string]*ToolGroup),
		tabGroups: make(map[int][]string),
	}
}

// GroupConfig configures how a new group should be created
type GroupConfig struct {
	Name     string
	ToolType ToolType
	TabIdx   int
}

// CreateGroup creates a new tool group
func (gm *ToolGroupManager) CreateGroup(config GroupConfig) *ToolGroup {
	groupID := generateGroupID()

	group := &ToolGroup{
		ID:         groupID,
		Name:       config.Name,
		ToolType:   config.ToolType,
		State:      ToolStateRunning,
		Messages:   make([]*ToolCallMessage, 0),
		CreatedAt:  time.Now(),
		isExpanded: false, // Start collapsed for compactness
	}

	gm.mu.Lock()
	defer gm.mu.Unlock()

	gm.groups[groupID] = group
	gm.tabGroups[config.TabIdx] = append(gm.tabGroups[config.TabIdx], groupID)

	return group
}

// GetGroup retrieves a group by ID
func (gm *ToolGroupManager) GetGroup(groupID string) (*ToolGroup, bool) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	group, ok := gm.groups[groupID]
	return group, ok
}

// AddMessageToGroup adds a tool call message to a group
func (gm *ToolGroupManager) AddMessageToGroup(groupID string, msg *ToolCallMessage) bool {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	group, ok := gm.groups[groupID]
	if !ok {
		return false
	}

	group.mu.Lock()
	defer group.mu.Unlock()

	group.Messages = append(group.Messages, msg)
	return true
}

// UpdateGroupState updates the overall state of a group based on its messages
func (gm *ToolGroupManager) UpdateGroupState(groupID string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	group, ok := gm.groups[groupID]
	if !ok {
		return
	}

	group.mu.Lock()
	defer group.mu.Unlock()

	// Calculate aggregate state
	hasRunning := false
	hasFailed := false
	allCompleted := true

	for _, msg := range group.Messages {
		switch msg.State {
		case ToolStateRunning, ToolStatePending:
			hasRunning = true
			allCompleted = false
		case ToolStateFailed:
			hasFailed = true
			allCompleted = false
		case ToolStateCompleted:
			// Continue checking
		default:
			allCompleted = false
		}
	}

	if len(group.Messages) == 0 {
		group.State = ToolStatePending
	} else if hasRunning {
		group.State = ToolStateRunning
	} else if hasFailed {
		group.State = ToolStateFailed
	} else if allCompleted {
		group.State = ToolStateCompleted
		now := time.Now()
		group.CompletedAt = &now
	}
}

// GetGroupsForTab returns all groups for a specific tab
func (gm *ToolGroupManager) GetGroupsForTab(tabIdx int) []*ToolGroup {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	groupIDs, ok := gm.tabGroups[tabIdx]
	if !ok {
		return nil
	}

	groups := make([]*ToolGroup, 0, len(groupIDs))
	for _, id := range groupIDs {
		if group, ok := gm.groups[id]; ok {
			groups = append(groups, group)
		}
	}

	return groups
}

// ToggleGroupExpansion toggles the expanded state of a group
func (gm *ToolGroupManager) ToggleGroupExpansion(groupID string) bool {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	group, ok := gm.groups[groupID]
	if !ok {
		return false
	}

	group.mu.Lock()
	defer group.mu.Unlock()

	group.isExpanded = !group.isExpanded
	return group.isExpanded
}

// SetGroupExpansion sets the expanded state of a group
func (gm *ToolGroupManager) SetGroupExpansion(groupID string, expanded bool) bool {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	group, ok := gm.groups[groupID]
	if !ok {
		return false
	}

	group.mu.Lock()
	defer group.mu.Unlock()

	group.isExpanded = expanded
	return true
}

// RemoveGroup removes a group (cleanup)
func (gm *ToolGroupManager) RemoveGroup(groupID string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	delete(gm.groups, groupID)

	// Remove from tab groups
	for tabIdx, groupIDs := range gm.tabGroups {
		var newIDs []string
		for _, id := range groupIDs {
			if id != groupID {
				newIDs = append(newIDs, id)
			}
		}
		gm.tabGroups[tabIdx] = newIDs
	}
}

// IsParallelToolCall checks if a tool call is the parallel tool
func IsParallelToolCall(toolName string) bool {
	return toolName == tools.ToolNameParallel
}

// ExtractParallelToolCalls extracts individual tool calls from parallel tool parameters
func ExtractParallelToolCalls(params map[string]interface{}) []ParallelToolCall {
	toolCallsRaw, ok := params["tool_calls"].([]interface{})
	if !ok {
		return nil
	}

	var calls []ParallelToolCall
	for _, tc := range toolCallsRaw {
		callMap, ok := tc.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := callMap["name"].(string)
		params, _ := callMap["parameters"].(map[string]interface{})

		if name != "" {
			calls = append(calls, ParallelToolCall{
				Name:       name,
				Parameters: params,
			})
		}
	}

	return calls
}

// ParallelToolCall represents a single tool call within a parallel execution
type ParallelToolCall struct {
	Name       string
	Parameters map[string]interface{}
}

// GroupFormatter formats tool groups for display
type GroupFormatter struct {
	ts *ToolStyles
}

// NewGroupFormatter creates a new group formatter
func NewGroupFormatter() *GroupFormatter {
	return &GroupFormatter{
		ts: GetToolStyles(),
	}
}

// FormatGroupHeader formats the header for a tool group
// Compact format: state icon + group icon + [N tools] + progress
func (gf *GroupFormatter) FormatGroupHeader(group *ToolGroup) string {
	group.mu.RLock()
	defer group.mu.RUnlock()

	// Get icon and style based on group type
	icon := gf.getGroupIcon(group.ToolType)
	stateIndicator := GetStateIndicator(group.State)

	// Build progress indicator
	completed, total := group.GetProgress()

	// Expansion indicator
	expandIndicator := "▶"
	if group.isExpanded {
		expandIndicator = "▼"
	}

	// Compact format: "▼ ◐ ⚡ [3 tools] 2/3"
	var parts []string
	parts = append(parts, expandIndicator, stateIndicator, icon)

	if total > 0 {
		parts = append(parts, fmt.Sprintf("[%d tools]", total))
		if completed < total {
			parts = append(parts, fmt.Sprintf("%d/%d", completed, total))
		}
	}

	return strings.Join(parts, " ")
}

// FormatGroupSummary creates a compact summary of all tools in a group
func (gf *GroupFormatter) FormatGroupSummary(group *ToolGroup) string {
	group.mu.RLock()
	defer group.mu.RUnlock()

	if len(group.Messages) == 0 {
		return ""
	}

	var summaries []string
	for _, msg := range group.Messages {
		summary := gf.formatToolCallSummary(msg)
		if summary != "" {
			summaries = append(summaries, summary)
		}
	}

	if len(summaries) == 0 {
		return ""
	}

	return strings.Join(summaries, "\n")
}

// formatToolCallSummary creates a one-line summary of a tool call
// Compact format: "  ✓ 📖 read_file .../path/to/file.go"
func (gf *GroupFormatter) formatToolCallSummary(msg *ToolCallMessage) string {
	toolType := GetToolTypeFromName(msg.ToolName)
	icon := GetIconForToolType(toolType)

	// Extract primary parameter
	primaryParam := extractPrimaryParameter(msg.ToolName, msg.Parameters)

	// Truncate parameter for compactness
	if len(primaryParam) > 35 {
		primaryParam = truncateStringSmart(primaryParam, 35)
	}

	var result string
	if primaryParam != "" {
		result = fmt.Sprintf("  %s %s %s %s", GetStateIndicator(msg.State), icon, msg.ToolName, primaryParam)
	} else {
		result = fmt.Sprintf("  %s %s %s", GetStateIndicator(msg.State), icon, msg.ToolName)
	}

	return result
}

// getGroupIcon returns the appropriate icon for a group type
func (gf *GroupFormatter) getGroupIcon(toolType ToolType) string {
	switch toolType {
	case ToolTypeParallel:
		return "⚡"
	default:
		return "📦"
	}
}

// Helper methods for ToolGroup

// GetProgress returns the completion progress of the group
func (g *ToolGroup) GetProgress() (completed, total int) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	total = len(g.Messages)
	for _, msg := range g.Messages {
		if msg.State == ToolStateCompleted || msg.State == ToolStateFailed {
			completed++
		}
	}
	return
}

// IsExpanded returns whether the group is expanded
func (g *ToolGroup) IsExpanded() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.isExpanded
}

// GetDuration returns the duration of the group execution
func (g *ToolGroup) GetDuration() time.Duration {
	g.mu.RLock()
	defer g.mu.RUnlock()

	end := g.CompletedAt
	if end == nil {
		end = &[]time.Time{time.Now()}[0]
	}

	return end.Sub(g.CreatedAt)
}

// Group rendering styles

// GroupContainerStyle returns the style for a group container
func GroupContainerStyle(expanded bool) lipgloss.Style {
	border := lipgloss.RoundedBorder()
	if !expanded {
		border = lipgloss.HiddenBorder()
	}

	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(lipgloss.Color(ColorParallel)).
		Padding(0, 1).
		Margin(1, 0)
}

// GroupHeaderStyle returns the style for a group header
func GroupHeaderStyle(state ToolState) lipgloss.Style {
	var color string
	switch state {
	case ToolStateRunning:
		color = ColorStateRunning
	case ToolStateCompleted:
		color = ColorStateCompleted
	case ToolStateFailed:
		color = ColorStateFailed
	default:
		color = ColorStatePending
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
}

// generateGroupID generates a unique group ID
var groupIDCounter int64

func generateGroupID() string {
	id := atomic.AddInt64(&groupIDCounter, 1)
	return fmt.Sprintf("group-%d-%d", time.Now().UnixNano(), id)
}
