package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// MessageRenderer handles rendering of messages with consistent styling
type MessageRenderer struct {
	ts              *ToolStyles
	contentWidth    int
	renderWrapWidth int
	paramsRenderer  *ParamsRenderer
}

// NewMessageRenderer creates a new message renderer
func NewMessageRenderer(contentWidth, renderWrapWidth int) *MessageRenderer {
	return &MessageRenderer{
		ts:              GetToolStyles(),
		contentWidth:    contentWidth,
		renderWrapWidth: renderWrapWidth,
		paramsRenderer:  NewParamsRenderer(),
	}
}

// SetWidth updates the renderer width
func (mr *MessageRenderer) SetWidth(contentWidth, renderWrapWidth int) {
	mr.contentWidth = contentWidth
	mr.renderWrapWidth = renderWrapWidth
}

// RenderHeader creates a styled header for a message
func (mr *MessageRenderer) RenderHeader(msg message) string {
	timestampStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	timestampText := timestampStyle.Render(msg.timestamp)

	var roleText string

	// Special handling for verification agent messages
	if msg.isVerificationAgent {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("150")).
			Bold(true)
		roleText = style.Render("Verification agent")
	} else {
		switch msg.role {
		case "You":
			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color("86")).
				Bold(true)
			roleText = style.Render(msg.role)

		case "Assistant":
			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true)
			roleText = style.Render(msg.role)

		case "Tool":
			// Use tool-specific styling
			roleText = mr.renderToolHeader(msg)

		default:
			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
			roleText = style.Render(msg.role)
		}
	}

	// Calculate padding for right-aligned timestamp
	availableWidth := mr.contentWidth - 4
	if availableWidth < 20 {
		availableWidth = 20
	}

	roleWidth := lipgloss.Width(roleText)
	timestampWidth := lipgloss.Width(timestampText)

	padding := availableWidth - roleWidth - timestampWidth
	if padding < 1 {
		padding = 1
	}

	return roleText + strings.Repeat(" ", padding) + timestampText
}

// renderToolHeader creates a styled header for tool messages
func (mr *MessageRenderer) renderToolHeader(msg message) string {
	// If it's a grouped message, show group info
	if msg.groupID != "" && msg.groupTotal > 0 {
		return mr.renderGroupedToolHeader(msg)
	}

	// Regular tool header
	toolType := msg.toolType
	if toolType == ToolTypeUnknown {
		toolType = GetToolTypeFromName(msg.toolName)
	}

	toolStyle := mr.ts.GetToolTypeStyle(toolType)
	stateStyle := mr.ts.GetStateStyle(msg.toolState)

	icon := GetIconForToolType(toolType)
	indicator := GetStateIndicator(msg.toolState)

	// Build the role text with summary generator
	var parts []string
	parts = append(parts, stateStyle.Render(indicator))
	parts = append(parts, toolStyle.Render(fmt.Sprintf("%s %s", icon, msg.toolName)))

	// Add one-line tool summary
	summaryGen := NewToolSummaryGenerator()
	compactSummary := summaryGen.GenerateCompactSummary(msg.toolName, msg.parameters)
	if compactSummary != "" {
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Render(compactSummary))
	}

	// Add progress if available
	if msg.progress >= 0 && msg.progress < 1.0 {
		progressStr := fmt.Sprintf("[%3d%%]", int(msg.progress*100))
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorStateRunning)).
			Render(progressStr))
	}

	// Add status if present
	if msg.status != "" {
		statusStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)
		parts = append(parts, statusStyle.Render(msg.status))
	}

	// Add description if present and not already in compact summary
	// Skip for go_sandbox and other tools that include description in their summary
	if msg.description != "" && !isDescriptionInCompactSummary(msg.toolName) {
		descStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA")).
			Italic(true)
		parts = append(parts, descStyle.Render(msg.description))
	}

	// Add statistics for completed tools
	if msg.toolState == ToolStateCompleted || msg.toolState == ToolStateFailed {
		stats := CreateStatisticsDisplay(msg.executionMetadata)
		if stats != "" {
			statsStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#666666")).
				Faint(true)
			parts = append(parts, statsStyle.Render(fmt.Sprintf("[%s]", stats)))
		}
	}

	return strings.Join(parts, " ")
}

// renderGroupedToolHeader creates a header for grouped tool messages
func (mr *MessageRenderer) renderGroupedToolHeader(msg message) string {
	// For grouped messages, show a more compact header
	toolType := msg.toolType
	if toolType == ToolTypeUnknown {
		toolType = GetToolTypeFromName(msg.toolName)
	}

	toolStyle := mr.ts.GetToolTypeStyle(toolType)
	stateStyle := mr.ts.GetStateStyle(msg.toolState)

	icon := GetIconForToolType(toolType)
	indicator := GetStateIndicator(msg.toolState)

	// Show index in group if applicable
	var groupInfo string
	if msg.groupTotal > 1 {
		groupInfo = fmt.Sprintf("(%d/%d)", msg.groupIdx+1, msg.groupTotal)
	}

	parts := []string{
		stateStyle.Render(indicator),
		toolStyle.Render(fmt.Sprintf("%s %s", icon, msg.toolName)),
	}

	if groupInfo != "" {
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Render(groupInfo))
	}

	return "  " + strings.Join(parts, " ")
}

// RenderReasoning renders reasoning/thinking content
func (mr *MessageRenderer) RenderReasoning(reasoning string) string {
	var sb strings.Builder

	// Reasoning header
	reasoningHeader := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Bold(true).
		Render("Thinking:")
	sb.WriteString(reasoningHeader)
	sb.WriteString("\n")

	// Separator
	sb.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Render("---"))
	sb.WriteString("\n")

	// Content (will be rendered via markdown in main pipeline)
	sb.WriteString(reasoning)

	return sb.String()
}

// RenderContent renders message content based on role
func (mr *MessageRenderer) RenderContent(msg message) string {
	switch msg.role {
	case "You":
		return mr.renderUserContent(msg.content)

	case "Tool":
		return mr.renderToolContent(msg)

	case "Assistant":
		// Assistant content goes through markdown rendering
		return msg.content

	default:
		return msg.content
	}
}

// renderUserContent renders user message content
func (mr *MessageRenderer) renderUserContent(content string) string {
	// User content is wrapped for readability
	if mr.renderWrapWidth > 0 {
		// Note: Actual word wrapping happens in the main rendering pipeline
		return content
	}
	return content
}

// renderToolContent renders tool message content with special handling
func (mr *MessageRenderer) renderToolContent(msg message) string {
	var sb strings.Builder

	// Parameters are already shown in the tool header compact summary,
	// so we don't render them separately to avoid duplication

	// If content is collapsed, show collapsed indicator
	if msg.isCollapsed && msg.isCollapsible {
		collapseStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Italic(true)
		return sb.String() + collapseStyle.Render("[output collapsed - press Enter to expand]")
	}

	content := msg.content

	// If there's a full result and we're showing summary, handle accordingly
	if msg.summarized && msg.fullResult != "" && !msg.isCollapsed {
		// Show summary with option to expand
		summaryStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))
		expandHint := summaryStyle.Render("\n[Full output available - press Enter to toggle]")
		content = content + expandHint
	}

	sb.WriteString(content)
	return sb.String()
}

// RenderCompactToolCall creates a compact one-line representation
func (mr *MessageRenderer) RenderCompactToolCall(msg message) string {
	toolType := msg.toolType
	if toolType == ToolTypeUnknown {
		toolType = GetToolTypeFromName(msg.toolName)
	}

	icon := GetIconForToolType(toolType)
	indicator := GetStateIndicator(msg.toolState)

	toolStyle := mr.ts.GetToolTypeStyle(toolType)
	stateStyle := mr.ts.GetStateStyle(msg.toolState)

	indicatorStr := stateStyle.Render(indicator)
	toolIconStr := toolStyle.Render(icon)
	toolNameStr := toolStyle.Render(msg.toolName)

	return fmt.Sprintf("%s %s %s", indicatorStr, toolIconStr, toolNameStr)
}

// UpdateMessageProgress updates a message with progress information
func (mr *MessageRenderer) UpdateMessageProgress(msg *message, progress float64, status string) {
	msg.progress = progress
	if status != "" {
		msg.status = status
	}
	// Update state based on progress
	if progress >= 1.0 {
		msg.toolState = ToolStateCompleted
	} else if progress >= 0 {
		msg.toolState = ToolStateRunning
	}
}

// ToggleCollapse toggles the collapsed state of a message
func (mr *MessageRenderer) ToggleCollapse(msg *message) bool {
	if !msg.isCollapsible {
		return false
	}
	msg.isCollapsed = !msg.isCollapsed
	return msg.isCollapsed
}

// ToggleParams toggles the collapsed state of tool parameters
func (mr *MessageRenderer) ToggleParams(msg *message) bool {
	if len(msg.parameters) == 0 {
		return false
	}
	msg.paramsCollapsed = !msg.paramsCollapsed
	return msg.paramsCollapsed
}

// isDescriptionInCompactSummary returns true if the tool's compact summary
// already includes the description parameter
func isDescriptionInCompactSummary(toolName string) bool {
	switch toolName {
	case tools.ToolNameGoSandbox:
		return true
	}
	return false
}

// Helper function to create a summary of a tool result
func CreateToolSummary(toolName string, lines int, bytes int, duration int64) string {
	var parts []string

	if lines > 0 {
		if lines == 1 {
			parts = append(parts, "1 line")
		} else {
			parts = append(parts, fmt.Sprintf("%d lines", lines))
		}
	}

	if bytes > 0 {
		parts = append(parts, formatBytes(bytes))
	}

	if duration > 0 {
		parts = append(parts, formatDuration(duration))
	}

	if len(parts) == 0 {
		return "done"
	}

	return strings.Join(parts, " • ")
}

// CreateStatisticsDisplay creates a formatted statistics display from execution metadata
func CreateStatisticsDisplay(metadata *tools.ExecutionMetadata) string {
	if metadata == nil {
		return ""
	}

	var parts []string

	// Duration (always show if > 0)
	if metadata.DurationMs > 0 {
		parts = append(parts, formatDuration(metadata.DurationMs))
	}

	// Lines (from metadata or output_line_count)
	if metadata.OutputLineCount > 0 {
		if metadata.OutputLineCount == 1 {
			parts = append(parts, "1 line")
		} else {
			parts = append(parts, fmt.Sprintf("%d lines", metadata.OutputLineCount))
		}
	}

	// Bytes (from metadata or output_size_bytes)
	if metadata.OutputSizeBytes > 0 {
		parts = append(parts, formatBytes(metadata.OutputSizeBytes))
	}

	// Exit code (only show for failures)
	if metadata.ExitCode > 0 {
		parts = append(parts, fmt.Sprintf("exit %d", metadata.ExitCode))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, " • ")
}
