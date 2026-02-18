package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	// titleStyle is the style for the application title in the header
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170")).
			MarginLeft(2)

	// statusStyle is the style for status indicators (model, generating, etc.)
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginLeft(2)
)

// renderHeader renders the application header, including the title and current model
func (m *Model) renderHeader() string {
	sb := acquireBuilder()

	// Title
	title := titleStyle.Render("scriptschnell - AI-Powered Coding Assistant")
	status := statusStyle.Render(fmt.Sprintf("Model: %s", m.currentModel))

	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(status)
	sb.WriteString("\n")

	return builderString(sb)
}
