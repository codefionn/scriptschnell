package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

type viewportRefreshMsg struct {
	token int
}

const (
	// resizeViewportDebounce is the time to wait before refreshing the viewport after a resize
	resizeViewportDebounce = 75 * time.Millisecond
)

var (
	// todoPanelStyle is the style for the TODO list panel on the right
	todoPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("241")).
			Padding(0, 1).
			Width(todoPanelWidth)

	// todoTitleStyle is the style for the title of the TODO panel
	todoTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)

	// todoItemStyle is the style for a single item in the TODO panel
	todoItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	// todoCompletedStyle is the style for completed TODO items
	todoCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Strikethrough(true)

	// todoInProgressStyle is the style for TODO items currently in progress
	todoInProgressStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true)

	// todoEmptyStyle is the style used when the TODO list is empty
	todoEmptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	// todoErrorStyle is the style used for errors in the TODO panel
	todoErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
)

// scheduleViewportRefresh schedules a viewport refresh message with debouncing
func (m *Model) scheduleViewportRefresh() tea.Cmd {
	m.viewportRefreshToken++
	token := m.viewportRefreshToken
	return tea.Tick(resizeViewportDebounce, func(time.Time) tea.Msg {
		return viewportRefreshMsg{token: token}
	})
}

// updateViewport refreshes the content of the main message viewport
func (m *Model) updateViewport() {
	// Create message renderer with current dimensions
	renderer := NewMessageRenderer(m.contentWidth, m.renderWrapWidth)

	rendered := acquireBuilder()

	for i, msg := range m.messages {
		// Skip header for Assistant messages for more compact display
		if msg.role != "Assistant" {
			// Render message header
			header := renderer.RenderHeader(msg)
			if i > 0 {
				rendered.WriteString("\n\n")
			}
			rendered.WriteString(header)
			rendered.WriteString("\n")
		} else if i > 0 {
			// Assistant messages still need spacing between them
			rendered.WriteString("\n\n")
		}

		// Render reasoning content if present (for extended thinking models)
		if msg.reasoning != "" {
			reasoningHeader := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Bold(true).
				Render("Thinking:")
			rendered.WriteString(reasoningHeader)
			rendered.WriteString("\n")

			// Render reasoning as markdown with a subtle indent
			if m.renderer != nil {
				if mdRendered, err := m.renderer.Render(msg.reasoning); err == nil {
					renderedContent := strings.TrimRight(mdRendered, "\n")
					// Add a subtle separator line before reasoning
					rendered.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("---"))
					rendered.WriteString("\n")
					rendered.WriteString(renderedContent)
				} else {
					rendered.WriteString(msg.reasoning)
				}
			} else {
				rendered.WriteString(msg.reasoning)
			}
			rendered.WriteString("\n\n")
		}

		// Render content with markdown for Assistant and Tool messages
		if (msg.role == "Assistant" || msg.role == "Tool") && m.renderer != nil && msg.content != "" {
			// Render markdown with Glamour, which handles syntax highlighting internally via Chroma
			var renderedContent string
			if mdRendered, err := m.renderer.Render(msg.content); err == nil {
				renderedContent = strings.TrimRight(mdRendered, "\n")
			} else {
				// Fallback to plain text if rendering fails
				renderedContent = msg.content
			}

			rendered.WriteString(renderedContent)
		} else {
			// Plain text for user and system messages
			if msg.role == "You" {
				// Wrap user prompts to improve readability
				wrappedContent := wordwrap.String(msg.content, m.renderWrapWidth)
				rendered.WriteString(wrappedContent)
			} else {
				rendered.WriteString(msg.content)
			}
		}
	}

	// Check if we should auto-scroll (user is at or near bottom)
	shouldScroll := m.viewport.AtBottom() || m.isCurrentTabGenerating()

	m.viewport.SetContent(builderString(rendered))

	// Auto-scroll to bottom when generating or if user was already at bottom
	if shouldScroll {
		m.viewport.GotoBottom()
	}

	m.lastUpdateHeight = len(m.messages)
	m.viewportDirty = false
}

// renderViewport renders the main message viewport
func (m *Model) renderViewport() string {
	return m.viewport.View()
}

// renderTodoPanel renders the TODO list panel on the right side of the UI
func (m *Model) renderTodoPanel() string {
	// Always refresh todo content to ensure it's up to date
	m.refreshTodoContent()

	// If viewport is not initialized or content fits, show content directly
	if m.todoViewport.Height == 0 || m.todoContentHeight <= m.todoViewport.Height {
		return todoPanelStyle.Render(strings.TrimRight(m.todoContent, "\n"))
	}

	// Use viewport for scrollable content
	viewportContent := m.todoViewport.View()

	// Add scroll indicators
	var scrollIndicator string
	if m.todoViewport.AtTop() && m.todoViewport.AtBottom() {
		scrollIndicator = ""
	} else if m.todoViewport.AtTop() {
		scrollIndicator = "\n⬇"
	} else if m.todoViewport.AtBottom() {
		scrollIndicator = "⬆\n"
	} else {
		scrollIndicator = "⬆\n⬇"
	}

	content := viewportContent
	if scrollIndicator != "" {
		content += scrollIndicator
	}

	return todoPanelStyle.Render(strings.TrimRight(content, "\n"))
}
