package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	// activeTabStyle is the style for the currently active tab
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("63"))

	// inactiveTabStyle is the style for inactive tabs
	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	// newTabButtonStyle is the style for the "plus" button to add a new tab
	newTabButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Bold(true)
)

// renderTabBar renders the tab bar UI, including each session tab and the new tab button.
func (m *Model) renderTabBar() string {
	if len(m.sessions) <= 1 {
		return "" // Don't show tab bar for single session
	}

	var tabs []string
	for i, ts := range m.sessions {
		isActive := i == m.activeSessionIdx
		tabText := ts.DisplayName()

		// Add state indicators
		if ts.NeedsAuthorization() {
			// Yellow/orange dot for authorization required
			authDot := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(" ◉")
			tabText += authDot
		} else if ts.IsGenerating() {
			// Regular dot for generating
			tabText += " ●"
		} else if ts.HasMessages() {
			// Regular dot for has messages
			tabText += " ●"
		}

		// Apply styling
		if isActive {
			tabs = append(tabs, fmt.Sprintf(" %s ", activeTabStyle.Render(tabText)))
		} else {
			tabs = append(tabs, fmt.Sprintf(" %s ", inactiveTabStyle.Render(tabText)))
		}
	}

	// Add new tab button
	tabs = append(tabs, newTabButtonStyle.Render(" [+] "))

	return lipgloss.JoinHorizontal(lipgloss.Left, tabs...)
}
