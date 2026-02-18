package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	// contextStyle is the style for context-related information in the footer
	contextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	// contextUsageStyle is the style for the context usage percentage indicator
	contextUsageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				MarginLeft(2)

	// errorStyle is the style for error messages in the footer
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			MarginLeft(2)
)

// renderMainFooter renders the complex status bar at the bottom of the UI, including processing status,
// errors, and context/cost information.
func (m *Model) renderMainFooter() string {
	footerLeft := ""
	if m.processingStatus != "" {
		statusText := m.processingStatus
		if m.thinkingTokens > 0 {
			statusText = fmt.Sprintf("%s (%d tokens)", statusText, m.thinkingTokens)
		}
		if !m.animationsDisabled && m.spinnerActive {
			footerLeft = statusStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), statusText))
		} else {
			footerLeft = statusStyle.Render(fmt.Sprintf("⚙️  %s", statusText))
		}
	} else if m.isCurrentTabGenerating() {
		var generatingText string

		// Show thinking tokens only if we haven't received content yet (still in thinking phase)
		if m.thinkingTokens > 0 && !m.contentReceived {
			generatingText = fmt.Sprintf("Thinking... (%d thinking tokens)", m.thinkingTokens)
		} else {
			generatingText = "Generating..."
		}

		if !m.animationsDisabled && m.spinnerActive {
			footerLeft = statusStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), generatingText))
		} else {
			footerLeft = statusStyle.Render(fmt.Sprintf("⏳ %s", generatingText))
		}
	} else if m.err != nil {
		if m.errVisibleUntil.IsZero() || time.Now().Before(m.errVisibleUntil) {
			footerLeft = errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
		}
	}

	footerRight := contextStyle.Render(m.contextDisplay())

	return m.renderFooter(footerLeft, footerRight)
}

// renderFooter is a layout helper that places strings at the left and right ends of a line,
// expanding the space between them as needed.
func (m *Model) renderFooter(left, right string) string {
	width := m.contentWidth
	if width <= 0 {
		switch {
		case left == "":
			return right
		case right == "":
			return left
		default:
			return left + " " + right
		}
	}

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	space := width - leftWidth - rightWidth
	if space < 1 {
		space = 1
	}

	return left + strings.Repeat(" ", space) + right
}

// contextDisplay generates the status string for branch, context file, cost, and caching.
func (m *Model) contextDisplay() string {
	var parts []string

	// Get current session's accumulated usage
	var totalCost float64
	var totalTokens int
	var cachedTokens int
	var currentBranch string

	if m.activeSessionIdx >= 0 && m.activeSessionIdx < len(m.sessions) {
		sess := m.sessions[m.activeSessionIdx].Session
		if sess != nil {
			totalCost = sess.GetTotalCost()
			totalTokens = sess.GetTotalTokens()
			cachedTokens = sess.TotalCachedTokens + sess.TotalCacheReadTokens
			currentBranch = sess.GetCurrentBranch()
		}
	}

	// Add branch info if available
	if currentBranch != "" {
		parts = append(parts, fmt.Sprintf("Branch: %s", currentBranch))
	}

	// Determine caching state
	cacheActive := cachedTokens > 0

	// Add context file info
	if strings.TrimSpace(m.contextFile) == "" {
		parts = append(parts, "Context: (none)")
	} else {
		contextLabel := fmt.Sprintf("Context: %s", m.contextFile)
		if cacheActive {
			contextLabel += " (cache on)"
		}
		parts = append(parts, contextLabel)
	}

	// Add accumulated usage from session
	if totalCost > 0 {
		parts = append(parts, fmt.Sprintf("Cost: $%.6f", totalCost))
	}

	if cachedTokens > 0 && totalTokens > 0 {
		cachePercent := float64(cachedTokens) / float64(totalTokens) * 100
		parts = append(parts, fmt.Sprintf("Cached: %.0f%%", cachePercent))
	}

	return strings.Join(parts, " | ")
}

// renderContextUsage renders the context freedom percentage indicator.
func (m *Model) renderContextUsage() string {
	percent := m.contextFreePercent
	if percent < 0 {
		return contextUsageStyle.Render("Free context: unknown")
	}
	if percent > 100 {
		percent = 100
	}

	// Format context window size in K tokens
	if m.contextWindow > 0 {
		contextWindowK := m.contextWindow / 1000
		return contextUsageStyle.Render(fmt.Sprintf("Free context: %d%% (%dK)", percent, contextWindowK))
	}

	return contextUsageStyle.Render(fmt.Sprintf("Free context: %d%%", percent))
}
