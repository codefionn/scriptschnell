package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// ToolState represents the execution state of a tool call
type ToolState int

const (
	ToolStatePending ToolState = iota
	ToolStateRunning
	ToolStateCompleted
	ToolStateFailed
	ToolStateWarning
)

// String returns the string representation of a ToolState
func (ts ToolState) String() string {
	switch ts {
	case ToolStatePending:
		return "pending"
	case ToolStateRunning:
		return "running"
	case ToolStateCompleted:
		return "completed"
	case ToolStateFailed:
		return "failed"
	case ToolStateWarning:
		return "warning"
	default:
		return "unknown"
	}
}

// ToolType represents the category of a tool for styling purposes
type ToolType int

const (
	ToolTypeReadFile ToolType = iota
	ToolTypeCreateFile
	ToolTypeEditFile
	ToolTypeReplaceFile
	ToolTypeShell
	ToolTypeGoSandbox
	ToolTypeWebSearch
	ToolTypeWebFetch
	ToolTypeTodo
	ToolTypeStatus
	ToolTypeStopProgram
	ToolTypeParallel
	ToolTypeSummarize
	ToolTypeCommand
	ToolTypeUnknown
)

// String returns the string representation of a ToolType
func (tt ToolType) String() string {
	switch tt {
	case ToolTypeReadFile:
		return "read_file"
	case ToolTypeCreateFile:
		return "create_file"
	case ToolTypeEditFile:
		return "edit_file"
	case ToolTypeReplaceFile:
		return "replace_file"
	case ToolTypeShell:
		return "shell"
	case ToolTypeGoSandbox:
		return "go_sandbox"
	case ToolTypeWebSearch:
		return "web_search"
	case ToolTypeWebFetch:
		return "web_fetch"
	case ToolTypeTodo:
		return "todo"
	case ToolTypeStatus:
		return "status"
	case ToolTypeStopProgram:
		return "stop_program"
	case ToolTypeParallel:
		return "parallel"
	case ToolTypeSummarize:
		return "summarize"
	case ToolTypeCommand:
		return "command"
	default:
		return "unknown"
	}
}

// Color definitions for tool types (lipgloss color codes)
const (
	ColorReadFile    = "#6B8EEF" // Blue
	ColorCreateFile  = "#50C878" // Green
	ColorEditFile    = "#FFD700" // Yellow/Gold
	ColorReplaceFile = "#FFA500" // Orange
	ColorShell       = "#FF8C00" // Dark Orange
	ColorGoSandbox   = "#9370DB" // Purple
	ColorWebSearch   = "#00CED1" // Cyan
	ColorWebFetch    = "#20B2AA" // Light Sea Green
	ColorTodo        = "#FF6B9D" // Pink/Magenta
	ColorStatus      = "#87CEEB" // Sky Blue
	ColorStopProgram = "#DC143C" // Crimson
	ColorParallel    = "#DDA0DD" // Plum
	ColorSummarize   = "#98FB98" // Pale Green
	ColorCommand     = "#F0E68C" // Khaki
	ColorUnknown     = "#A9A9A9" // Dark Grey

	// State colors
	ColorStatePending   = "#808080" // Grey
	ColorStateRunning   = "#00BFFF" // Deep Sky Blue
	ColorStateCompleted = "#32CD32" // Lime Green
	ColorStateFailed    = "#FF4444" // Red
	ColorStateWarning   = "#FFD700" // Gold
)

// Icons for tool types
const (
	IconReadFile    = "📖"
	IconCreateFile  = "📝"
	IconEditFile    = "✏️"
	IconReplaceFile = "🔄"
	IconShell       = "💻"
	IconGoSandbox   = "🔧"
	IconWebSearch   = "🔍"
	IconWebFetch    = "🌐"
	IconTodo        = "✅"
	IconStatus      = "📊"
	IconStopProgram = "🛑"
	IconParallel    = "⚡"
	IconSummarize   = "📋"
	IconCommand     = "⚙️"
	IconUnknown     = "🔹"

	// State indicators
	IndicatorPending   = "○"
	IndicatorRunning   = "◐"
	IndicatorCompleted = "✓"
	IndicatorFailed    = "✗"
	IndicatorWarning   = "⚠"
)

// ToolStyles holds all styling components for tool calls
type ToolStyles struct {
	// State-based styles
	PendingStyle   lipgloss.Style
	RunningStyle   lipgloss.Style
	CompletedStyle lipgloss.Style
	FailedStyle    lipgloss.Style
	WarningStyle   lipgloss.Style

	// Tool type styles (colors)
	ToolTypeStyles map[ToolType]lipgloss.Style

	// Component styles
	HeaderStyle      lipgloss.Style
	PathStyle        lipgloss.Style
	StatsStyle       lipgloss.Style
	TimestampStyle   lipgloss.Style
	GroupHeaderStyle lipgloss.Style
	CollapsedStyle   lipgloss.Style
	ExpandedStyle    lipgloss.Style

	// Container styles
	ToolCallBoxStyle   lipgloss.Style
	ToolResultBoxStyle lipgloss.Style

	// Spinner for running state
	Spinner spinner.Model
}

// ToolCallMessage extends the base message with tool-specific metadata
type ToolCallMessage struct {
	ToolName      string
	ToolID        string
	ToolType      ToolType
	State         ToolState
	Parameters    map[string]interface{}
	Result        string
	Error         string
	ExecutionTime time.Duration
	Timestamp     time.Time
	Description   string // Human-readable description of what the tool is doing

	// UI state
	IsCollapsible bool
	IsCollapsed   bool
	IsGrouped     bool
	GroupID       string
	GroupIndex    int
	GroupTotal    int
}

// Global tool styles instance
var toolStyles *ToolStyles

// InitializeToolStyles creates and configures the tool styling system
func InitializeToolStyles() *ToolStyles {
	if toolStyles != nil {
		return toolStyles
	}

	ts := &ToolStyles{
		ToolTypeStyles: make(map[ToolType]lipgloss.Style),
	}

	// State-based styles
	ts.PendingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorStatePending)).
		Bold(true)

	ts.RunningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorStateRunning)).
		Bold(true)

	ts.CompletedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorStateCompleted)).
		Bold(true)

	ts.FailedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorStateFailed)).
		Bold(true)

	ts.WarningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorStateWarning)).
		Bold(true)

	// Tool type styles
	ts.ToolTypeStyles[ToolTypeReadFile] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorReadFile)).Bold(true)
	ts.ToolTypeStyles[ToolTypeCreateFile] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCreateFile)).Bold(true)
	ts.ToolTypeStyles[ToolTypeEditFile] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorEditFile)).Bold(true)
	ts.ToolTypeStyles[ToolTypeReplaceFile] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorReplaceFile)).Bold(true)
	ts.ToolTypeStyles[ToolTypeShell] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorShell)).Bold(true)
	ts.ToolTypeStyles[ToolTypeGoSandbox] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGoSandbox)).Bold(true)
	ts.ToolTypeStyles[ToolTypeWebSearch] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorWebSearch)).Bold(true)
	ts.ToolTypeStyles[ToolTypeWebFetch] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorWebFetch)).Bold(true)
	ts.ToolTypeStyles[ToolTypeTodo] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTodo)).Bold(true)
	ts.ToolTypeStyles[ToolTypeStatus] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorStatus)).Bold(true)
	ts.ToolTypeStyles[ToolTypeStopProgram] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorStopProgram)).Bold(true)
	ts.ToolTypeStyles[ToolTypeParallel] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorParallel)).Bold(true)
	ts.ToolTypeStyles[ToolTypeSummarize] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorSummarize)).Bold(true)
	ts.ToolTypeStyles[ToolTypeCommand] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCommand)).Bold(true)
	ts.ToolTypeStyles[ToolTypeUnknown] = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorUnknown)).Bold(true)

	// Component styles
	ts.HeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1)

	ts.PathStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C0C0C0")).
		Background(lipgloss.Color("#2A2A2A")).
		Padding(0, 1).
		MarginTop(1).
		MarginBottom(1)

	ts.StatsStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#808080")).
		Italic(true)

	ts.TimestampStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666"))

	ts.GroupHeaderStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorParallel)).
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorParallel)).
		Padding(0, 1)

	ts.CollapsedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666"))

	ts.ExpandedStyle = lipgloss.NewStyle()

	// Container styles
	ts.ToolCallBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#444444")).
		Padding(0, 1).
		Margin(1, 0)

	ts.ToolResultBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#444444")).
		Padding(1).
		Margin(1, 0)

	// Initialize spinner for running state
	ts.Spinner = spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateRunning))),
	)

	toolStyles = ts
	return ts
}

// GetToolStyles returns the global tool styles instance
func GetToolStyles() *ToolStyles {
	if toolStyles == nil {
		return InitializeToolStyles()
	}
	return toolStyles
}

// GetToolTypeFromName determines the ToolType from a tool name
func GetToolTypeFromName(name string) ToolType {
	switch name {
	case tools.ToolNameReadFile, tools.ToolNameReadFileSummarized:
		return ToolTypeReadFile
	case tools.ToolNameCreateFile:
		return ToolTypeCreateFile
	case tools.ToolNameEditFile:
		return ToolTypeEditFile
	case tools.ToolNameReplaceFile:
		return ToolTypeReplaceFile
	case tools.ToolNameShell:
		return ToolTypeShell
	case tools.ToolNameGoSandbox:
		return ToolTypeGoSandbox
	case tools.ToolNameWebSearch:
		return ToolTypeWebSearch
	case tools.ToolNameWebFetch:
		return ToolTypeWebFetch
	case tools.ToolNameStatusProgram:
		return ToolTypeStatus
	case tools.ToolNameStopProgram:
		return ToolTypeStopProgram
	case tools.ToolNameParallel:
		return ToolTypeParallel
	default:
		// Check for prefixes
		if strings.HasPrefix(name, "read_file") {
			return ToolTypeReadFile
		}
		if strings.HasPrefix(name, "create_file") {
			return ToolTypeCreateFile
		}
		if strings.HasPrefix(name, "edit_file") || strings.HasPrefix(name, "write_file") {
			return ToolTypeEditFile
		}
		if strings.HasPrefix(name, "shell") {
			return ToolTypeShell
		}
		if strings.HasPrefix(name, "go_sandbox") {
			return ToolTypeGoSandbox
		}
		if strings.HasPrefix(name, "todo") {
			return ToolTypeTodo
		}
		return ToolTypeUnknown
	}
}

// GetIconForToolType returns the icon for a tool type
func GetIconForToolType(tt ToolType) string {
	switch tt {
	case ToolTypeReadFile:
		return IconReadFile
	case ToolTypeCreateFile:
		return IconCreateFile
	case ToolTypeEditFile:
		return IconEditFile
	case ToolTypeReplaceFile:
		return IconReplaceFile
	case ToolTypeShell:
		return IconShell
	case ToolTypeGoSandbox:
		return IconGoSandbox
	case ToolTypeWebSearch:
		return IconWebSearch
	case ToolTypeWebFetch:
		return IconWebFetch
	case ToolTypeTodo:
		return IconTodo
	case ToolTypeStatus:
		return IconStatus
	case ToolTypeStopProgram:
		return IconStopProgram
	case ToolTypeParallel:
		return IconParallel
	case ToolTypeSummarize:
		return IconSummarize
	case ToolTypeCommand:
		return IconCommand
	default:
		return IconUnknown
	}
}

// GetStateIndicator returns the indicator character for a state
func GetStateIndicator(state ToolState) string {
	switch state {
	case ToolStatePending:
		return IndicatorPending
	case ToolStateRunning:
		return IndicatorRunning
	case ToolStateCompleted:
		return IndicatorCompleted
	case ToolStateFailed:
		return IndicatorFailed
	case ToolStateWarning:
		return IndicatorWarning
	default:
		return IndicatorPending
	}
}

// GetStateStyle returns the style for a state
func (ts *ToolStyles) GetStateStyle(state ToolState) lipgloss.Style {
	switch state {
	case ToolStatePending:
		return ts.PendingStyle
	case ToolStateRunning:
		return ts.RunningStyle
	case ToolStateCompleted:
		return ts.CompletedStyle
	case ToolStateFailed:
		return ts.FailedStyle
	case ToolStateWarning:
		return ts.WarningStyle
	default:
		return ts.PendingStyle
	}
}

// GetStateLabel returns a human-readable label for a state
func GetStateLabel(state ToolState) string {
	switch state {
	case ToolStatePending:
		return "Pending"
	case ToolStateRunning:
		return "Running"
	case ToolStateCompleted:
		return "Completed"
	case ToolStateFailed:
		return "Failed"
	case ToolStateWarning:
		return "Warning"
	default:
		return "Unknown"
	}
}

// GetToolTypeStyle returns the style for a tool type
func (ts *ToolStyles) GetToolTypeStyle(tt ToolType) lipgloss.Style {
	if style, ok := ts.ToolTypeStyles[tt]; ok {
		return style
	}
	return ts.ToolTypeStyles[ToolTypeUnknown]
}

// FormatToolCallHeader creates a styled header for a tool call
func (ts *ToolStyles) FormatToolCallHeader(toolName string, state ToolState, timestamp time.Time) string {
	toolType := GetToolTypeFromName(toolName)
	icon := GetIconForToolType(toolType)
	indicator := GetStateIndicator(state)
	stateStyle := ts.GetStateStyle(state)
	toolStyle := ts.GetToolTypeStyle(toolType)

	// Format: [indicator] Icon tool_name (State) - timestamp
	stateLabel := GetStateLabel(state)
	if state == ToolStateRunning {
		stateLabel = "Running..."
	}

	indicatorStr := stateStyle.Render(indicator)
	toolNameStr := toolStyle.Render(fmt.Sprintf("%s %s", icon, toolName))
	stateStr := stateStyle.Render(fmt.Sprintf("(%s)", stateLabel))
	timestampStr := ts.TimestampStyle.Render(timestamp.Format("15:04:05"))

	return fmt.Sprintf("%s %s %s - %s", indicatorStr, toolNameStr, stateStr, timestampStr)
}

// FormatToolPath formats a file path for display
func (ts *ToolStyles) FormatToolPath(path string) string {
	if path == "" {
		return ""
	}
	return ts.PathStyle.Render(path)
}

// FormatToolStats creates a styled statistics line
func (ts *ToolStyles) FormatToolStats(lines int, bytes int, duration time.Duration) string {
	var parts []string

	if lines > 0 {
		parts = append(parts, fmt.Sprintf("%d lines", lines))
	}
	if bytes > 0 {
		parts = append(parts, formatBytes(bytes))
	}
	if duration > 0 {
		parts = append(parts, formatDuration(duration.Milliseconds()))
	}

	if len(parts) == 0 {
		return ""
	}

	stats := strings.Join(parts, " • ")
	return ts.StatsStyle.Render(stats)
}

// FormatCompactToolCall creates a compact one-line representation with enhanced styling
func (ts *ToolStyles) FormatCompactToolCall(toolName string, parameters map[string]interface{}, state ToolState, description string) string {
	toolType := GetToolTypeFromName(toolName)
	icon := GetIconForToolType(toolType)
	indicator := GetStateIndicator(state)
	stateStyle := ts.GetStateStyle(state)
	toolStyle := ts.GetToolTypeStyle(toolType)

	// Get the primary parameter (path for files, command for shell, etc.)
	primaryParam := extractPrimaryParameter(toolName, parameters)

	indicatorStr := stateStyle.Render(indicator)
	toolIconStr := toolStyle.Render(icon)

	var result string
	if primaryParam != "" {
		result = fmt.Sprintf("%s %s %s `%s`", indicatorStr, toolIconStr, toolName, primaryParam)
	} else {
		result = fmt.Sprintf("%s %s %s", indicatorStr, toolIconStr, toolName)
	}

	// Append description if provided (useful for go_sandbox)
	if description != "" {
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A0A0A0")).Italic(true)
		result += " " + descStyle.Render(fmt.Sprintf("(%s)", description))
	}

	return result
}

// FormatGroupHeader creates a header for a group of parallel tool calls
func (ts *ToolStyles) FormatGroupHeader(groupID string, completed, total int) string {
	progress := fmt.Sprintf("[%d/%d]", completed, total)
	return ts.GroupHeaderStyle.Render(fmt.Sprintf("%s Parallel Execution %s", IconParallel, progress))
}

// UpdateSpinner updates the spinner state and returns a command
func (ts *ToolStyles) UpdateSpinner(msg tea.Msg) (spinner.Model, tea.Cmd) {
	return ts.Spinner.Update(msg)
}

// RenderRunningIndicator renders an animated indicator for running tools
func (ts *ToolStyles) RenderRunningIndicator() string {
	return ts.RunningStyle.Render("◐")
}

// extractPrimaryParameter extracts the most relevant parameter for display
// This function returns the single most important parameter for each tool type
func extractPrimaryParameter(toolName string, parameters map[string]interface{}) string {
	// Handle "Planning: " prefix
	toolName = strings.TrimPrefix(toolName, "Planning: ")

	switch toolName {
	// File operations - path is primary
	case tools.ToolNameReadFile, tools.ToolNameReadFileSummarized:
		if path, ok := parameters["path"].(string); ok {
			return truncatePathSmart(path, 50)
		}
	case tools.ToolNameCreateFile, tools.ToolNameEditFile, tools.ToolNameReplaceFile:
		if path, ok := parameters["path"].(string); ok {
			return truncatePathSmart(path, 50)
		}

	// Shell/Command operations - command is primary
	case tools.ToolNameShell:
		if command, ok := parameters["command"].(string); ok {
			return truncateCommandSmart(command, 60)
		}
		if command, ok := parameters["command"].([]interface{}); ok && len(command) > 0 {
			// Join array elements into a command string
			cmdParts := make([]string, 0, len(command))
			for _, part := range command {
				if str, ok := part.(string); ok {
					cmdParts = append(cmdParts, str)
				}
			}
			if len(cmdParts) > 0 {
				return truncateCommandSmart(strings.Join(cmdParts, " "), 60)
			}
		}

	// Web operations - query/URL is primary
	case tools.ToolNameWebSearch:
		if queries, ok := parameters["queries"].([]interface{}); ok && len(queries) > 0 {
			if first, ok := queries[0].(string); ok {
				return truncateStringSmart(first, 40)
			}
		}
		if query, ok := parameters["query"].(string); ok {
			return truncateStringSmart(query, 40)
		}
	case tools.ToolNameWebFetch:
		if url, ok := parameters["url"].(string); ok {
			return truncateURLSmart(url, 50)
		}

	// Sandbox - description or summary
	case tools.ToolNameGoSandbox:
		if desc, ok := parameters["description"].(string); ok && desc != "" {
			return truncateStringSmart(desc, 40)
		}
		return "Go code execution"

	// Todo operations - action is primary
	case tools.ToolNameTodo:
		if action, ok := parameters["action"].(string); ok {
			return action
		}

	// Program control - job_id is primary
	case tools.ToolNameStatusProgram:
		if jobID, ok := parameters["job_id"].(string); ok {
			return truncateStringSmart(jobID, 20)
		}
	case tools.ToolNameStopProgram:
		if jobID, ok := parameters["job_id"].(string); ok {
			return truncateStringSmart(jobID, 20)
		}
	case tools.ToolNameWaitProgram:
		if jobID, ok := parameters["job_id"].(string); ok {
			return truncateStringSmart(jobID, 20)
		}

	// Search operations - pattern is primary
	case tools.ToolNameSearchFiles:
		if pattern, ok := parameters["pattern"].(string); ok {
			return truncateStringSmart(pattern, 40)
		}
	case tools.ToolNameSearchFileContent:
		if pattern, ok := parameters["pattern"].(string); ok {
			return truncateStringSmart(pattern, 40)
		}

	// Summarize - target tool is primary
	case tools.ToolNameToolSummarize:
		if toolName, ok := parameters["tool_name"].(string); ok {
			return toolName
		}

	// Codebase investigator - objectives are primary
	case tools.ToolNameCodebaseInvestigator:
		if objectives, ok := parameters["objectives"].([]interface{}); ok && len(objectives) > 0 {
			if first, ok := objectives[0].(string); ok {
				return truncateStringSmart(first, 40)
			}
		}

	// Directory listing - path is primary
	case tools.ToolNameLs:
		if path, ok := parameters["path"].(string); ok {
			return truncatePathSmart(path, 40)
		}

	// Context files - pattern is primary
	case tools.ToolNameSearchContextFiles, tools.ToolNameGrepContextFiles:
		if pattern, ok := parameters["pattern"].(string); ok {
			return truncateStringSmart(pattern, 40)
		}
	case tools.ToolNameReadContextFile:
		if path, ok := parameters["path"].(string); ok {
			return truncatePathSmart(path, 50)
		}
	case tools.ToolNameAddContextDirectory:
		if directory, ok := parameters["directory"].(string); ok {
			return truncatePathSmart(directory, 40)
		}

	// Parallel execution - count
	case tools.ToolNameParallel:
		if toolCalls, ok := parameters["tool_calls"].([]interface{}); ok {
			return fmt.Sprintf("%d tools", len(toolCalls))
		}
	}

	return ""
}

// extractSecondaryParameters extracts additional context parameters for display
// Returns a map of parameter names to their truncated values
func extractSecondaryParameters(toolName string, parameters map[string]interface{}) map[string]string {
	// Handle "Planning: " prefix
	toolName = strings.TrimPrefix(toolName, "Planning: ")

	secondary := make(map[string]string)

	switch toolName {
	// File operations - show line ranges, search patterns
	case tools.ToolNameReadFile, tools.ToolNameReadFileSummarized:
		if from, ok := parameters["from_line"].(float64); ok {
			if to, ok := parameters["to_line"].(float64); ok {
				secondary["lines"] = fmt.Sprintf("%.0f-%.0f", from, to)
			} else {
				secondary["from"] = fmt.Sprintf("%.0f", from)
			}
		}
		if sections, ok := parameters["sections"].([]interface{}); ok && len(sections) > 0 {
			secondary["sections"] = fmt.Sprintf("%d", len(sections))
		}
		if prompt, ok := parameters["summarize_prompt"].(string); ok && prompt != "" {
			secondary["summarize"] = truncateStringSmart(prompt, 30)
		}

	case tools.ToolNameEditFile:
		if edits, ok := parameters["edits"].([]interface{}); ok {
			secondary["edits"] = fmt.Sprintf("%d", len(edits))
		}

	case tools.ToolNameCreateFile, tools.ToolNameReplaceFile:
		if content, ok := parameters["content"].(string); ok {
			lines := strings.Count(content, "\n") + 1
			secondary["size"] = fmt.Sprintf("%d lines", lines)
		}

	// Shell - show timeout, background status
	case tools.ToolNameShell:
		if timeout, ok := parameters["timeout"].(float64); ok && timeout > 0 {
			secondary["timeout"] = fmt.Sprintf("%.0fs", timeout)
		}
		if bg, ok := parameters["background"].(bool); ok && bg {
			secondary["mode"] = "background"
		}
		if stdin, ok := parameters["stdin"].(string); ok && stdin != "" {
			secondary["stdin"] = truncateStringSmart(stdin, 20)
		}

	// Web search - show number of results
	case tools.ToolNameWebSearch:
		if numResults, ok := parameters["num_results"].(float64); ok {
			secondary["limit"] = fmt.Sprintf("%.0f", numResults)
		}
		if queries, ok := parameters["queries"].([]interface{}); ok && len(queries) > 1 {
			secondary["queries"] = fmt.Sprintf("%d", len(queries))
		}

	// Web fetch - show summarize prompt
	case tools.ToolNameWebFetch:
		if prompt, ok := parameters["summarize_prompt"].(string); ok && prompt != "" {
			secondary["summarize"] = truncateStringSmart(prompt, 30)
		}

	// Sandbox - show libraries, timeout
	case tools.ToolNameGoSandbox:
		if libs, ok := parameters["libraries"].([]interface{}); ok && len(libs) > 0 {
			secondary["libs"] = fmt.Sprintf("%d", len(libs))
		}
		if timeout, ok := parameters["timeout"].(float64); ok && timeout > 0 {
			secondary["timeout"] = fmt.Sprintf("%.0fs", timeout)
		}
		if bg, ok := parameters["background"].(bool); ok && bg {
			secondary["mode"] = "background"
		}
		if workingDir, ok := parameters["working_dir"].(string); ok && workingDir != "" {
			secondary["dir"] = truncatePathSmart(workingDir, 20)
		}

	// Todo - show item details
	case tools.ToolNameTodo:
		if text, ok := parameters["text"].(string); ok && text != "" {
			secondary["text"] = truncateStringSmart(text, 30)
		}
		if status, ok := parameters["status"].(string); ok {
			secondary["status"] = status
		}
		if priority, ok := parameters["priority"].(string); ok {
			secondary["priority"] = priority
		}

	// Search - show context, glob patterns
	case tools.ToolNameSearchFiles:
		if contentRegex, ok := parameters["content_regex"].(string); ok {
			secondary["content"] = truncateStringSmart(contentRegex, 25)
		}
		if path, ok := parameters["path"].(string); ok && path != "" {
			secondary["path"] = truncatePathSmart(path, 25)
		}

	case tools.ToolNameSearchFileContent:
		if glob, ok := parameters["glob"].(string); ok {
			secondary["glob"] = glob
		}
		if path, ok := parameters["path"].(string); ok && path != "." {
			secondary["path"] = truncatePathSmart(path, 25)
		}
		if context, ok := parameters["context"].(float64); ok && context > 0 {
			secondary["context"] = fmt.Sprintf("%.0f", context)
		}

	// Tool summarize - show goal
	case tools.ToolNameToolSummarize:
		if goal, ok := parameters["summary_goal"].(string); ok {
			secondary["goal"] = truncateStringSmart(goal, 40)
		}

	// Parallel tools - show tool count
	case tools.ToolNameParallel:
		if toolCalls, ok := parameters["tool_calls"].([]interface{}); ok {
			secondary["total"] = fmt.Sprintf("%d", len(toolCalls))
		}

	// Context files
	case tools.ToolNameSearchContextFiles:
		if contentRegex, ok := parameters["content_regex"].(string); ok {
			secondary["content"] = truncateStringSmart(contentRegex, 25)
		}
	case tools.ToolNameGrepContextFiles:
		if filePattern, ok := parameters["file_pattern"].(string); ok {
			secondary["glob"] = filePattern
		}
		if context, ok := parameters["context_lines"].(float64); ok && context > 0 {
			secondary["context"] = fmt.Sprintf("%.0f", context)
		}
	case tools.ToolNameAddContextDirectory:
		if reason, ok := parameters["reason"].(string); ok && reason != "" {
			secondary["reason"] = truncateStringSmart(reason, 30)
		}
	}

	return secondary
}

// truncatePathSmart truncates a file path with intelligent placement
// Preserves the filename and important directory structure
func truncatePathSmart(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}

	// If maxLen is too small, just do simple truncation
	if maxLen < 10 {
		return truncateString(path, maxLen)
	}

	// Try to preserve the filename
	parts := strings.Split(path, "/")
	filename := parts[len(parts)-1]

	// If filename alone fits, show ".../filename"
	if len(filename) <= maxLen-4 {
		return ".../" + filename
	}

	// If filename is too long, truncate it with ellipsis
	if len(filename) > maxLen-3 {
		return filename[:maxLen-3] + "..."
	}

	// Try to show some directory context
	if len(parts) > 2 {
		// Show ".../parent/filename"
		parent := parts[len(parts)-2]
		shortPath := ".../" + parent + "/" + filename
		if len(shortPath) <= maxLen {
			return shortPath
		}
	}

	// Fallback: truncate middle with ellipsis
	return path[:maxLen-3] + "..."
}

// truncateCommandSmart truncates a shell command intelligently
// Preserves the command name and first few arguments
func truncateCommandSmart(cmd string, maxLen int) string {
	if len(cmd) <= maxLen {
		return cmd
	}

	if maxLen < 10 {
		return truncateString(cmd, maxLen)
	}

	// Split into parts
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return truncateString(cmd, maxLen)
	}

	// Always show the command itself
	command := parts[0]

	// If command alone is too long
	if len(command) > maxLen-3 {
		return command[:maxLen-3] + "..."
	}

	// Try to fit as many arguments as possible
	result := command
	for i, part := range parts[1:] {
		testResult := result + " " + part
		if len(testResult) > maxLen-3 {
			// Can't fit this argument
			remaining := len(parts) - i - 1
			if remaining > 0 {
				return result + fmt.Sprintf(" ... +%d more", remaining)
			}
			return result + " ..."
		}
		result = testResult
	}

	return result
}

// truncateURLSmart truncates a URL intelligently
// Preserves the domain and path structure
func truncateURLSmart(url string, maxLen int) string {
	if len(url) <= maxLen {
		return url
	}

	if maxLen < 15 {
		return truncateString(url, maxLen)
	}

	// Find the protocol separator
	protoEnd := strings.Index(url, "://")
	if protoEnd == -1 {
		// No protocol, just truncate
		return truncateString(url, maxLen)
	}

	protocol := url[:protoEnd+3] // Include ://
	remainder := url[protoEnd+3:]

	// Find the path start
	pathStart := strings.Index(remainder, "/")
	if pathStart == -1 {
		// No path, just domain
		if len(url) > maxLen {
			return url[:maxLen-3] + "..."
		}
		return url
	}

	domain := remainder[:pathStart]
	path := remainder[pathStart:]

	// Try: protocol + domain + "/..." + end of path
	if len(path) > 10 {
		// Get last segment of path
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash > 0 {
			endPart := path[lastSlash:]
			shortURL := protocol + domain + "/..." + endPart
			if len(shortURL) <= maxLen {
				return shortURL
			}
		}
	}

	// Fallback: protocol + truncated domain/path
	available := maxLen - len(protocol) - 3 // Reserve space for "..."
	if available > 0 && len(remainder) > available {
		return protocol + remainder[:available] + "..."
	}

	return truncateString(url, maxLen)
}

// truncateStringSmart truncates a string with ellipsis at word boundary when possible
func truncateStringSmart(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	if maxLen < 5 {
		return s[:maxLen]
	}

	// Look for a word boundary near the truncation point
	truncAt := maxLen - 3

	// Check for space near truncation point (within last 10 chars)
	start := truncAt - 10
	if start < 0 {
		start = 0
	}

	// Find last space before truncation point
	lastSpace := strings.LastIndex(s[start:truncAt], " ")
	if lastSpace > 0 {
		// Truncate at word boundary
		return s[:start+lastSpace] + "..."
	}

	// No good word boundary, just truncate
	return s[:truncAt] + "..."
}

// truncateString truncates a string with ellipsis (legacy, simple version)
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
