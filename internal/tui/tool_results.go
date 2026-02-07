package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/tools"
)

const (
	ansiESC        = 0x1b
	ansiBEL        = 0x07
	carriageReturn = '\r'
)

// stripANSISequences removes ANSI escape sequences (CSI, OSC, DCS, etc.)
// from the provided string. Unlike sanitizePromptInput, this function
// doesn't use state as tool output is complete, not streaming.
func stripANSISequences(input string) string {
	if input == "" {
		return input
	}

	var b strings.Builder
	b.Grow(len(input))

	i := 0
	for i < len(input) {
		c := input[i]

		// Check for ESC character
		if c == ansiESC {
			i++
			if i >= len(input) {
				// Trailing ESC, just ignore
				break
			}

			next := input[i]
			i++

			// Handle different escape sequence types
			switch next {
			case '[': // CSI sequences (like colors)
				// Skip until final byte (0x40-0x7e)
				for i < len(input) {
					if input[i] >= 0x40 && input[i] <= 0x7e {
						i++
						break
					}
					i++
				}

			case ']': // OSC sequences (like setting colors)
				// Skip until BEL or ST (ESC \)
				for i < len(input) {
					if input[i] == ansiBEL {
						i++
						break
					}
					if input[i] == ansiESC && i+1 < len(input) && input[i+1] == '\\' {
						i += 2 // Skip ST terminator
						break
					}
					i++
				}

			case 'P', 'X', '^', '_': // DCS, SOS, PM, APC sequences
				// Skip until ST (ESC \)
				for i < len(input) {
					if input[i] == ansiESC && i+1 < len(input) && input[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}

			default:
				// Unknown escape sequence, just continue
				// The next character will be handled in next iteration
			}
			continue
		}

		// Skip carriage return characters (often used with progress bars)
		if c == carriageReturn {
			i++
			continue
		}

		// Write the character if it's not part of a sequence
		b.WriteByte(c)
		i++
	}

	return b.String()
}

// CollapsibleState represents the expand/collapse state of a tool result
type CollapsibleState int

const (
	StateCollapsed CollapsibleState = iota
	StateExpanded
	StateAuto // Auto-collapse if content is long
)

// ResultFormatter handles formatting of tool results with rich display options
type ResultFormatter struct {
	ts *ToolStyles
}

// NewResultFormatter creates a new result formatter
func NewResultFormatter() *ResultFormatter {
	return &ResultFormatter{
		ts: GetToolStyles(),
	}
}

// FormatToolResult creates a fully formatted tool result with collapsible content
func (rf *ResultFormatter) FormatToolResult(toolName string, result string, errorMsg string, metadata *tools.ExecutionMetadata, state ToolState) string {
	if errorMsg != "" {
		return rf.formatErrorResult(toolName, errorMsg, state)
	}

	toolType := GetToolTypeFromName(toolName)

	switch toolType {
	case ToolTypeReadFile:
		return rf.formatReadFileResult(result, metadata, state)
	case ToolTypeCreateFile:
		return rf.formatCreateFileResult(result, metadata, state)
	case ToolTypeEditFile, ToolTypeReplaceFile:
		return rf.formatEditFileResult(result, metadata, state)
	case ToolTypeShell:
		return rf.formatShellResult(result, metadata, state)
	case ToolTypeGoSandbox:
		return rf.formatSandboxResult(result, metadata, state)
	case ToolTypeWebSearch, ToolTypeWebFetch:
		return rf.formatWebResult(result, metadata, state, toolType)
	default:
		return rf.formatGenericResult(result, metadata, state)
	}
}

// formatErrorResult formats a failed tool execution
func (rf *ResultFormatter) formatErrorResult(toolName string, errorMsg string, state ToolState) string {
	toolType := GetToolTypeFromName(toolName)
	icon := GetIconForToolType(toolType)
	toolStyle := rf.ts.GetToolTypeStyle(toolType)
	stateStyle := rf.ts.GetStateStyle(ToolStateFailed)

	indicator := stateStyle.Render(GetStateIndicator(ToolStateFailed))
	toolIcon := toolStyle.Render(icon)
	toolNameStyled := toolStyle.Render(toolName)
	stateLabel := stateStyle.Render("(Failed)")

	// Truncate long error messages
	maxErrorLen := 200
	displayError := errorMsg
	if len(errorMsg) > maxErrorLen {
		displayError = errorMsg[:maxErrorLen-3] + "..."
	}

	// Build the error message and strip ANSI codes
	errorStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateFailed)).Render(displayError)
	result := fmt.Sprintf("%s %s %s %s - %s",
		indicator, toolIcon, toolNameStyled, stateLabel, errorStyled)
	return stripANSISequences(result)
}

// formatReadFileResult formats read_file tool results
func (rf *ResultFormatter) formatReadFileResult(result string, metadata *tools.ExecutionMetadata, state ToolState) string {
	lines := strings.Count(result, "\n") + 1
	bytes := len(result)

	var duration time.Duration
	if metadata != nil && metadata.DurationMs > 0 {
		duration = time.Duration(metadata.DurationMs) * time.Millisecond
	}

	// Build metrics display
	metrics := rf.buildMetrics([]string{
		rf.formatLineCount(lines),
		rf.formatByteSize(bytes),
		rf.formatDuration(duration),
	})

	return rf.formatResultWithContent("read_file", ToolTypeReadFile, metrics, result, state, lines > 20)
}

// formatCreateFileResult formats create_file tool results
func (rf *ResultFormatter) formatCreateFileResult(result string, metadata *tools.ExecutionMetadata, state ToolState) string {
	lines := strings.Count(result, "\n") + 1

	var duration time.Duration
	if metadata != nil && metadata.DurationMs > 0 {
		duration = time.Duration(metadata.DurationMs) * time.Millisecond
	}

	metrics := rf.buildMetrics([]string{
		fmt.Sprintf("created %d lines", lines),
		rf.formatDuration(duration),
	})

	return rf.formatResultWithContent("create_file", ToolTypeCreateFile, metrics, result, state, lines > 10)
}

// formatEditFileResult formats edit/replace file results with enhanced diff display
func (rf *ResultFormatter) formatEditFileResult(result string, metadata *tools.ExecutionMetadata, state ToolState) string {
	// Count actual changes in the diff
	additions, deletions := countDiffChanges(result)

	var duration time.Duration
	if metadata != nil && metadata.DurationMs > 0 {
		duration = time.Duration(metadata.DurationMs) * time.Millisecond
	}

	// Build change summary with colors
	var changeParts []string
	if additions > 0 {
		addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateCompleted))
		changeParts = append(changeParts, addStyle.Render(fmt.Sprintf("+%d", additions)))
	}
	if deletions > 0 {
		delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateFailed))
		changeParts = append(changeParts, delStyle.Render(fmt.Sprintf("-%d", deletions)))
	}
	if len(changeParts) == 0 {
		changeParts = append(changeParts, "no changes")
	}

	metrics := rf.buildMetrics([]string{
		strings.Join(changeParts, "/"),
		rf.formatDuration(duration),
	})

	// Diff results should be collapsible if large
	shouldCollapse := len(result) > 500 || strings.Count(result, "\n") > 15

	// Format the diff with syntax highlighting
	formattedDiff := rf.formatDiffWithHighlighting(result)

	return rf.formatResultWithContent("edit_file", ToolTypeEditFile, metrics, formattedDiff, state, shouldCollapse)
}

// formatShellResult formats shell command results with special handling for output
func (rf *ResultFormatter) formatShellResult(result string, metadata *tools.ExecutionMetadata, state ToolState) string {
	lines := strings.Count(result, "\n") + 1

	var duration time.Duration
	var exitCode int
	if metadata != nil {
		duration = time.Duration(metadata.DurationMs) * time.Millisecond
		exitCode = metadata.ExitCode
	}

	// Build metrics
	var metricParts []string
	if lines > 0 {
		metricParts = append(metricParts, rf.formatLineCount(lines))
	}
	if duration > 0 {
		metricParts = append(metricParts, rf.formatDuration(duration))
	}
	if exitCode != 0 {
		metricParts = append(metricParts, lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorStateFailed)).
			Render(fmt.Sprintf("exit %d", exitCode)))
	}

	metrics := rf.buildMetrics(metricParts)

	// Shell output should be collapsible if large or if it's just status info
	shouldCollapse := len(result) > 300 || strings.Count(result, "\n") > 10 ||
		(exitCode == 0 && len(result) == 0)

	// Format shell output with special handling
	formattedOutput := rf.formatShellOutput(result)

	return rf.formatResultWithContent("shell", ToolTypeShell, metrics, formattedOutput, state, shouldCollapse)
}

// formatSandboxResult formats go_sandbox results with execution details
func (rf *ResultFormatter) formatSandboxResult(result string, metadata *tools.ExecutionMetadata, state ToolState) string {
	// Extract sandbox-specific metrics
	shellCalls := countSandboxShellCallsFromMetadata(metadata)
	httpCalls := countHTTPCallsFromMetadata(metadata)

	var duration time.Duration
	var exitCode int
	if metadata != nil {
		duration = time.Duration(metadata.DurationMs) * time.Millisecond
		exitCode = metadata.ExitCode
	}

	// Build metrics
	var metricParts []string
	if shellCalls > 0 {
		metricParts = append(metricParts, rf.formatShellCalls(shellCalls))
	}
	if httpCalls > 0 {
		metricParts = append(metricParts, rf.formatHTTPCalls(httpCalls))
	}
	if duration > 0 {
		metricParts = append(metricParts, rf.formatDuration(duration))
	}
	if exitCode != 0 {
		metricParts = append(metricParts, lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorStateFailed)).
			Render(fmt.Sprintf("exit %d", exitCode)))
	}

	if len(metricParts) == 0 {
		metricParts = append(metricParts, "executed")
	}

	metrics := rf.buildMetrics(metricParts)

	// Sandbox output is typically verbose, always offer collapse
	shouldCollapse := len(result) > 200

	// Strip ANSI escape sequences to prevent them from being displayed as literal text
	sanitizedResult := stripANSISequences(result)

	// Format with code block styling
	formattedOutput := fmt.Sprintf("```\n%s\n```", rf.truncateIfNeeded(sanitizedResult, 1000))

	return rf.formatResultWithContent("go_sandbox", ToolTypeGoSandbox, metrics, formattedOutput, state, shouldCollapse)
}

// formatWebResult formats web_search and web_fetch results
func (rf *ResultFormatter) formatWebResult(result string, metadata *tools.ExecutionMetadata, state ToolState, toolType ToolType) string {
	lines := strings.Count(result, "\n") + 1

	var duration time.Duration
	if metadata != nil && metadata.DurationMs > 0 {
		duration = time.Duration(metadata.DurationMs) * time.Millisecond
	}

	metrics := rf.buildMetrics([]string{
		rf.formatLineCount(lines),
		rf.formatDuration(duration),
	})

	shouldCollapse := len(result) > 400

	return rf.formatResultWithContent("web", toolType, metrics, result, state, shouldCollapse)
}

// formatGenericResult formats any other tool result
func (rf *ResultFormatter) formatGenericResult(result string, metadata *tools.ExecutionMetadata, state ToolState) string {
	lines := strings.Count(result, "\n") + 1

	var duration time.Duration
	if metadata != nil && metadata.DurationMs > 0 {
		duration = time.Duration(metadata.DurationMs) * time.Millisecond
	}

	metrics := rf.buildMetrics([]string{
		rf.formatLineCount(lines),
		rf.formatDuration(duration),
	})

	shouldCollapse := len(result) > 300

	return rf.formatResultWithContent("tool", ToolTypeUnknown, metrics, result, state, shouldCollapse)
}

// formatResultWithContent creates the final formatted result with header and optional collapse
func (rf *ResultFormatter) formatResultWithContent(toolName string, toolType ToolType, metrics string, content string, state ToolState, collapsible bool) string {
	icon := GetIconForToolType(toolType)
	toolStyle := rf.ts.GetToolTypeStyle(toolType)
	stateStyle := rf.ts.GetStateStyle(state)

	indicator := stateStyle.Render(GetStateIndicator(state))
	toolIcon := toolStyle.Render(icon)
	toolNameStyled := toolStyle.Render(toolName)
	stateLabel := stateStyle.Render(fmt.Sprintf("(%s)", GetStateLabel(state)))

	// Build header line and strip ANSI codes from it
	header := fmt.Sprintf("%s %s %s %s - %s",
		indicator, toolIcon, toolNameStyled, stateLabel, metrics)
	header = stripANSISequences(header)

	// If content is empty or already formatted, just return header
	if content == "" || isAlreadyFormatted(content) {
		return header
	}

	// For collapsible content, add a hint
	if collapsible {
		collapseHint := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Italic(true).
			Render("[content truncated, click to expand]")
		collapseHint = stripANSISequences(collapseHint)
		truncated := rf.truncateForPreview(content, 5)
		return fmt.Sprintf("%s\n%s\n%s", header, collapseHint, truncated)
	}

	// For short content, include it directly
	return fmt.Sprintf("%s\n%s", header, content)
}

// formatDiffWithHighlighting applies syntax highlighting to diff output
func (rf *ResultFormatter) formatDiffWithHighlighting(diff string) string {
	lines := strings.Split(diff, "\n")
	var formatted []string

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			// Addition
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateCompleted))
			formatted = append(formatted, style.Render(line))
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			// Deletion
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateFailed))
			formatted = append(formatted, style.Render(line))
		case strings.HasPrefix(line, "@@"):
			// Hunk header
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateWarning)).Bold(true)
			formatted = append(formatted, style.Render(line))
		case strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			// File header
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB")).Bold(true)
			formatted = append(formatted, style.Render(line))
		default:
			formatted = append(formatted, line)
		}
	}

	return "```diff\n" + strings.Join(formatted, "\n") + "\n```"
}

// formatShellOutput formats shell command output with special handling
func (rf *ResultFormatter) formatShellOutput(output string) string {
	if output == "" {
		return ""
	}

	// Strip ANSI escape sequences to prevent them from being displayed as literal text
	sanitizedOutput := stripANSISequences(output)

	// Check if output looks like a table (has aligned columns)
	if looksLikeTable(sanitizedOutput) {
		// Keep as-is but wrap in code block
		return fmt.Sprintf("```\n%s\n```", sanitizedOutput)
	}

	// For command output, just wrap in code block
	return fmt.Sprintf("```\n%s\n```", strings.TrimSpace(sanitizedOutput))
}

// looksLikeTable checks if output appears to be a formatted table
func looksLikeTable(output string) bool {
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return false
	}

	// Check for consistent spacing patterns (crude heuristic)
	spaceCounts := make(map[int]int)
	for _, line := range lines[:min(5, len(lines))] {
		count := strings.Count(line, "  ")
		spaceCounts[count]++
	}

	// If we see multiple spaces consistently, might be a table
	for count, freq := range spaceCounts {
		if count > 2 && freq > 1 {
			return true
		}
	}

	return false
}

// Helper formatting functions

func (rf *ResultFormatter) buildMetrics(parts []string) string {
	// Filter out empty parts
	var valid []string
	for _, p := range parts {
		if p != "" {
			valid = append(valid, p)
		}
	}

	if len(valid) == 0 {
		return rf.ts.StatsStyle.Render("done")
	}

	return rf.ts.StatsStyle.Render(strings.Join(valid, " • "))
}

func (rf *ResultFormatter) formatLineCount(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

func (rf *ResultFormatter) formatByteSize(bytes int) string {
	return formatBytes(bytes)
}

func (rf *ResultFormatter) formatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	return formatDuration(d.Milliseconds())
}

func (rf *ResultFormatter) formatShellCalls(n int) string {
	if n == 1 {
		return "1 shell call"
	}
	return fmt.Sprintf("%d shell calls", n)
}

func (rf *ResultFormatter) formatHTTPCalls(n int) string {
	if n == 1 {
		return "1 HTTP call"
	}
	return fmt.Sprintf("%d HTTP calls", n)
}

func (rf *ResultFormatter) truncateForPreview(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	preview := strings.Join(lines[:maxLines], "\n")
	return preview + "\n..."
}

func (rf *ResultFormatter) truncateIfNeeded(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen-3] + "..."
}

// Utility functions

// countDiffChanges counts additions and deletions in a diff
func countDiffChanges(diff string) (additions, deletions int) {
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			additions++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			deletions++
		}
	}
	return
}

// countSandboxShellCallsFromMetadata extracts shell call count from metadata
func countSandboxShellCallsFromMetadata(metadata *tools.ExecutionMetadata) int {
	if metadata == nil || metadata.Details == nil {
		return 0
	}

	// Try function_call_counts first
	if counts, ok := metadata.Details["function_call_counts"].(map[string]interface{}); ok {
		if count := intValue(counts["ExecuteCommand"]); count > 0 {
			return count
		}
	}

	// Fall back to counting in function_calls array
	if calls, ok := metadata.Details["function_calls"].([]interface{}); ok {
		count := 0
		for _, call := range calls {
			if callMap, ok := call.(map[string]interface{}); ok {
				if name, ok := callMap["name"].(string); ok && name == "ExecuteCommand" {
					count++
				}
			}
		}
		return count
	}

	return 0
}

// countHTTPCallsFromMetadata extracts HTTP call count from metadata
func countHTTPCallsFromMetadata(metadata *tools.ExecutionMetadata) int {
	if metadata == nil || metadata.Details == nil {
		return 0
	}

	// Try function_call_counts first
	if counts, ok := metadata.Details["function_call_counts"].(map[string]interface{}); ok {
		if count := intValue(counts["Fetch"]); count > 0 {
			return count
		}
	}

	// Fall back to counting in function_calls array
	if calls, ok := metadata.Details["function_calls"].([]interface{}); ok {
		count := 0
		for _, call := range calls {
			if callMap, ok := call.(map[string]interface{}); ok {
				if name, ok := callMap["name"].(string); ok && (name == "Fetch" || name == "HTTPRequest") {
					count++
				}
			}
		}
		return count
	}

	return 0
}

// min returns the minimum of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
