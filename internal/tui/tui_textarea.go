package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// defaultInputPlaceholder is the default placeholder text for the textarea
const defaultInputPlaceholder = "Type your prompt here... (@ for files, Alt+Enter or Ctrl+J for newline, Ctrl+X for commands)"

// renderTextarea renders the main input area and autocomplete suggestions
func (m *Model) renderTextarea() string {
	var sb strings.Builder

	// Textarea
	sb.WriteString(m.textarea.View())
	sb.WriteString("\n")

	// Autocomplete suggestions
	if len(m.suggestions) > 0 {
		sb.WriteString(m.renderSuggestions())
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderSuggestions renders the autocomplete suggestions list
func (m *Model) renderSuggestions() string {
	if len(m.suggestions) == 0 {
		return ""
	}

	var sb strings.Builder

	// Suggestion box style
	suggestionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		MarginLeft(2)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("170")).
		Background(lipgloss.Color("238")).
		Bold(true).
		MarginLeft(2)

	sb.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginLeft(2).
		Render("Suggestions (↑↓ to navigate, Tab/Shift+Tab to select, ESC to dismiss):"))
	sb.WriteString("\n")

	// Show max 5 suggestions at a time
	maxDisplay := 5
	start := 0
	if len(m.suggestions) > maxDisplay {
		// Center the selected item if possible
		start = m.selectedSuggIndex - maxDisplay/2
		if start < 0 {
			start = 0
		}
		if start+maxDisplay > len(m.suggestions) {
			start = len(m.suggestions) - maxDisplay
		}
	}

	end := start + maxDisplay
	if end > len(m.suggestions) {
		end = len(m.suggestions)
	}

	for i := start; i < end; i++ {
		suggestion := m.suggestions[i]
		if i == m.selectedSuggIndex {
			sb.WriteString(selectedStyle.Render(fmt.Sprintf("▶ %s", suggestion)))
		} else {
			sb.WriteString(suggestionStyle.Render(fmt.Sprintf("  %s", suggestion)))
		}
		sb.WriteString("\n")
	}

	// Show indicator if there are more suggestions
	if len(m.suggestions) > maxDisplay {
		more := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginLeft(2).
			Render(fmt.Sprintf("  ... (%d/%d)", m.selectedSuggIndex+1, len(m.suggestions)))
		sb.WriteString(more)
	}

	return sb.String()
}
