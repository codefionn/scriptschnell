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

	// Build compact header: indicator + icon + toolName + primaryParam
	var parts []string
	parts = append(parts, stateStyle.Render(indicator))
	parts = append(parts, toolStyle.Render(fmt.Sprintf("%s %s", icon, msg.toolName)))

	// Add truncated primary parameter inline
	primaryParam := extractPrimaryParameter(msg.toolName, msg.parameters)
	if primaryParam != "" {
		// Truncate to fit within reasonable header width
		maxParamLen := 40
		if mr.contentWidth > 60 && mr.contentWidth < 100 {
			maxParamLen = mr.contentWidth - 40
		}
		if len(primaryParam) > maxParamLen {
			primaryParam = truncateStringSmart(primaryParam, maxParamLen)
		}
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Render(primaryParam))
	}

	// Only show progress for running tools (compact: just percentage)
	if msg.toolState == ToolStateRunning && msg.progress >= 0 && msg.progress < 1.0 {
		progressStr := fmt.Sprintf("%d%%", int(msg.progress*100))
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorStateRunning)).
			Render(progressStr))
	}

	return strings.Join(parts, " ")
}

// renderGroupedToolHeader creates a header for grouped tool messages
// Compact format: "  ✓ 📖 read_file .../path/to/file.go"
func (mr *MessageRenderer) renderGroupedToolHeader(msg message) string {
	toolType := msg.toolType
	if toolType == ToolTypeUnknown {
		toolType = GetToolTypeFromName(msg.toolName)
	}

	toolStyle := mr.ts.GetToolTypeStyle(toolType)
	stateStyle := mr.ts.GetStateStyle(msg.toolState)

	icon := GetIconForToolType(toolType)
	indicator := GetStateIndicator(msg.toolState)

	parts := []string{
		stateStyle.Render(indicator),
		toolStyle.Render(fmt.Sprintf("%s %s", icon, msg.toolName)),
	}

	// Add truncated primary parameter inline
	primaryParam := extractPrimaryParameter(msg.toolName, msg.parameters)
	if primaryParam != "" {
		maxParamLen := 30
		if mr.contentWidth > 60 && mr.contentWidth < 100 {
			maxParamLen = mr.contentWidth - 45
		}
		if len(primaryParam) > maxParamLen {
			primaryParam = truncateStringSmart(primaryParam, maxParamLen)
		}
		parts = append(parts, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Render(primaryParam))
	}

	return "  " + strings.Join(parts, " ")
}

// RenderReasoning renders reasoning/thinking content
func (mr *MessageRenderer) RenderReasoning(reasoning string) string {
	sb := acquireBuilder()

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

	return builderString(sb)
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
	sb := acquireBuilder()

	// Parameters are already shown in the tool header compact summary,
	// so we don't render them separately to avoid duplication

	// If content is collapsed, show preview with line count indicator
	if msg.isCollapsed && msg.isCollapsible {
		content := msg.content
		if msg.fullResult != "" {
			content = msg.fullResult
		}

		// Show preview (first 2-3 lines)
		preview := mr.getContentPreview(content, 3)
		lines := strings.Count(content, "\n") + 1
		if content == "" {
			lines = 0
		}

		if lines <= 3 {
			// Small content - just show it all
			releaseBuilder(sb)
			return preview
		}

		// Show preview with indicator for more content
		moreLines := lines - strings.Count(preview, "\n") - 1
		if moreLines > 0 {
			indicatorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#666666")).
				Italic(true)
			preview = preview + "\n" + indicatorStyle.Render(fmt.Sprintf("... (%d more lines)", moreLines))
		}
		releaseBuilder(sb)
		return preview
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
	return builderString(sb)
}

// getContentPreview returns the first N lines of content as a preview
func (mr *MessageRenderer) getContentPreview(content string, maxLines int) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	// Return first maxLines lines
	preview := strings.Join(lines[:maxLines], "\n")
	return preview
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
