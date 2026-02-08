package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/progress"
)

// ToolProgressState represents the progress state of a running tool
type ToolProgressState struct {
	ToolID      string
	ToolName    string
	Description string // Human-readable description of what the tool is doing
	StartTime   time.Time
	LastUpdate  time.Time
	Progress    float64 // 0.0 to 1.0, -1 for indeterminate
	Status      string  // Current status message
	Output      strings.Builder
	OutputLines int
	IsStreaming bool
	IsComplete  bool
	Error       string
	mu          sync.RWMutex
}

// ToolProgressMsg is sent when a tool's progress updates
type ToolProgressMsg struct {
	TabID       int
	ToolID      string
	ToolName    string
	Description string  // Human-readable description of what the tool is doing
	Progress    float64 // -1 for indeterminate
	Status      string
	Output      string // New output chunk (if streaming)
}

// ToolProgressCompleteMsg is sent when a tool completes
type ToolProgressCompleteMsg struct {
	TabID    int
	ToolID   string
	Duration time.Duration
	Error    string
}

// ToolProgressTracker tracks progress for multiple running tools
type ToolProgressTracker struct {
	tools map[string]*ToolProgressState // toolID -> state
	mu    sync.RWMutex
}

// NewToolProgressTracker creates a new progress tracker
func NewToolProgressTracker() *ToolProgressTracker {
	return &ToolProgressTracker{
		tools: make(map[string]*ToolProgressState),
	}
}

// StartTool begins tracking a new tool
func (tpt *ToolProgressTracker) StartTool(toolID, toolName, description string) *ToolProgressState {
	tpt.mu.Lock()
	defer tpt.mu.Unlock()

	now := time.Now()
	state := &ToolProgressState{
		ToolID:      toolID,
		ToolName:    toolName,
		Description: description,
		StartTime:   now,
		LastUpdate:  now,
		Progress:    -1, // Indeterminate by default
		Status:      "starting...",
		IsStreaming: false,
		IsComplete:  false,
	}

	tpt.tools[toolID] = state
	return state
}

// GetTool retrieves a tool's progress state
func (tpt *ToolProgressTracker) GetTool(toolID string) (*ToolProgressState, bool) {
	tpt.mu.RLock()
	defer tpt.mu.RUnlock()

	state, ok := tpt.tools[toolID]
	return state, ok
}

// UpdateProgress updates a tool's progress
func (tpt *ToolProgressTracker) UpdateProgress(toolID string, progress float64, status string) bool {
	tpt.mu.Lock()
	defer tpt.mu.Unlock()

	state, ok := tpt.tools[toolID]
	if !ok {
		return false
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.Progress = progress
	if status != "" {
		state.Status = status
	}
	state.LastUpdate = time.Now()

	return true
}

// AppendOutput adds streaming output to a tool
func (tpt *ToolProgressTracker) AppendOutput(toolID string, output string) bool {
	tpt.mu.Lock()
	defer tpt.mu.Unlock()

	state, ok := tpt.tools[toolID]
	if !ok {
		return false
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.Output.WriteString(output)
	state.OutputLines = strings.Count(state.Output.String(), "\n")
	state.IsStreaming = true
	state.LastUpdate = time.Now()

	return true
}

// CompleteTool marks a tool as complete
func (tpt *ToolProgressTracker) CompleteTool(toolID string, err error) bool {
	tpt.mu.Lock()
	defer tpt.mu.Unlock()

	state, ok := tpt.tools[toolID]
	if !ok {
		return false
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.IsComplete = true
	state.Progress = 1.0
	state.Status = "complete"
	if err != nil {
		state.Error = err.Error()
		state.Status = "failed"
	}

	return true
}

// RemoveTool removes a tool from tracking (cleanup)
func (tpt *ToolProgressTracker) RemoveTool(toolID string) {
	tpt.mu.Lock()
	defer tpt.mu.Unlock()

	delete(tpt.tools, toolID)
}

// GetActiveTools returns all active (non-complete) tools
func (tpt *ToolProgressTracker) GetActiveTools() []*ToolProgressState {
	tpt.mu.RLock()
	defer tpt.mu.RUnlock()

	var active []*ToolProgressState
	for _, state := range tpt.tools {
		state.mu.RLock()
		isComplete := state.IsComplete
		state.mu.RUnlock()

		if !isComplete {
			active = append(active, state)
		}
	}

	return active
}

// ProgressFormatter formats progress information for display
type ProgressFormatter struct {
	ts *ToolStyles
}

// NewProgressFormatter creates a new progress formatter
func NewProgressFormatter() *ProgressFormatter {
	return &ProgressFormatter{
		ts: GetToolStyles(),
	}
}

// FormatProgressBar creates a progress bar string
func (pf *ProgressFormatter) FormatProgressBar(progress float64, width int) string {
	if progress < 0 {
		// Indeterminate - use animated dots
		return pf.formatIndeterminateProgress(width)
	}

	if width < 3 {
		width = 10
	}

	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}

	empty := width - filled

	barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateRunning))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))

	filledBar := barStyle.Render(strings.Repeat("█", filled))
	emptyBar := emptyStyle.Render(strings.Repeat("░", empty))

	percent := int(progress * 100)
	percentStr := fmt.Sprintf(" %3d%%", percent)

	return filledBar + emptyBar + percentStr
}

// formatIndeterminateProgress creates an indeterminate progress indicator
func (pf *ProgressFormatter) formatIndeterminateProgress(width int) string {
	if width < 3 {
		width = 10
	}

	// Simple animated bracket style
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateRunning))
	return style.Render("◐ " + strings.Repeat("░", width-3) + " ⟳")
}

// FormatToolProgress creates a formatted progress display for a tool
func (pf *ProgressFormatter) FormatToolProgress(state *ToolProgressState, compact bool) string {
	state.mu.RLock()
	defer state.mu.RUnlock()

	toolType := GetToolTypeFromName(state.ToolName)
	icon := GetIconForToolType(toolType)
	toolStyle := pf.ts.GetToolTypeStyle(toolType)

	// Build the progress line
	var parts []string

	// Icon and tool name
	parts = append(parts, toolStyle.Render(fmt.Sprintf("%s %s", icon, state.ToolName)))

	// Description (if available, for tools like go_sandbox)
	if state.Description != "" {
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A0A0A0")).Italic(true)
		parts = append(parts, descStyle.Render(fmt.Sprintf("(%s)", state.Description)))
	}

	// Status
	if state.Status != "" {
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		parts = append(parts, statusStyle.Render(state.Status))
	}

	// Progress bar or indicator
	if !state.IsComplete {
		if compact {
			// Just show spinner
			parts = append(parts, pf.ts.RenderRunningIndicator())
		} else {
			// Show full progress bar
			parts = append(parts, pf.FormatProgressBar(state.Progress, 15))
		}
	} else if state.Error != "" {
		parts = append(parts, pf.ts.FailedStyle.Render("✗"))
	} else {
		parts = append(parts, pf.ts.CompletedStyle.Render("✓"))
	}

	// Duration
	duration := time.Since(state.StartTime)
	if duration > time.Second {
		parts = append(parts, pf.ts.StatsStyle.Render(formatDuration(duration.Milliseconds())))
	}

	return strings.Join(parts, " ")
}

// FormatActiveToolsList creates a summary of all active tools
func (pf *ProgressFormatter) FormatActiveToolsList(states []*ToolProgressState, maxItems int) string {
	if len(states) == 0 {
		return ""
	}

	if maxItems <= 0 {
		maxItems = 3
	}

	var lines []string
	for i, state := range states {
		if i >= maxItems {
			remaining := len(states) - maxItems
			if remaining > 0 {
				lines = append(lines, fmt.Sprintf("... and %d more", remaining))
			}
			break
		}

		lines = append(lines, pf.FormatToolProgress(state, true))
	}

	return strings.Join(lines, "\n")
}

// CreateProgressCallback creates a progress callback for tool execution
func (tpt *ToolProgressTracker) CreateProgressCallback(tabID int, toolID, toolName, description string, program *tea.Program) progress.Callback {
	// Start tracking this tool
	tpt.StartTool(toolID, toolName, description)

	return func(update progress.Update) error {
		if program == nil {
			return nil
		}

		normalized := progress.Normalize(update)

		// Update the stored state
		if normalized.ShouldStatus() {
			tpt.UpdateProgress(toolID, -1, normalized.Message)
		}

		if normalized.ShouldStream() {
			tpt.AppendOutput(toolID, normalized.Message)
		}

		// Send message to TUI
		program.Send(ToolProgressMsg{
			TabID:    tabID,
			ToolID:   toolID,
			ToolName: toolName,
			Progress: -1, // Indeterminate for now
			Status:   normalized.Message,
			Output:   normalized.Message,
		})

		return nil
	}
}

// CompleteAndNotify marks a tool complete and sends notification
func (tpt *ToolProgressTracker) CompleteAndNotify(tabID int, toolID string, err error, program *tea.Program) {
	tpt.CompleteTool(toolID, err)

	if program == nil {
		return
	}

	state, ok := tpt.GetTool(toolID)
	if !ok {
		return
	}

	var duration time.Duration
	state.mu.RLock()
	duration = time.Since(state.StartTime)
	state.mu.RUnlock()

	var errStr string
	if err != nil {
		errStr = err.Error()
	}

	program.Send(ToolProgressCompleteMsg{
		TabID:    tabID,
		ToolID:   toolID,
		Duration: duration,
		Error:    errStr,
	})
}

// ProgressUpdateCmd creates a command that periodically updates progress
func (tpt *ToolProgressTracker) ProgressUpdateCmd(tabID int, toolID string, program *tea.Program) tea.Cmd {
	return func() tea.Msg {
		state, ok := tpt.GetTool(toolID)
		if !ok {
			return nil
		}

		state.mu.RLock()
		isComplete := state.IsComplete
		toolName := state.ToolName
		status := state.Status
		state.mu.RUnlock()

		if isComplete {
			return nil
		}

		return ToolProgressMsg{
			TabID:    tabID,
			ToolID:   toolID,
			ToolName: toolName,
			Progress: -1,
			Status:   status,
		}
	}
}

// SpinnerFrames for animated progress
var SpinnerFrames = []string{"◐", "◓", "◑", "◒"}

// GetSpinnerFrame returns the current spinner frame based on time
func GetSpinnerFrame(elapsed time.Duration) string {
	frame := int(elapsed.Milliseconds()/250) % len(SpinnerFrames)
	return SpinnerFrames[frame]
}

// AnimatedSpinnerStyle returns a styled animated spinner
func AnimatedSpinnerStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorStateRunning))
}
