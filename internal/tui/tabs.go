package tui

import (
	"fmt"
	"os"
	"regexp"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

const maxTabs = 10

// Tab bar styles
var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("63"))

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	newTabButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Bold(true)
)

// handleNewTab creates a new session tab
func (m *Model) handleNewTab(name string) tea.Cmd {
	// Validate max tabs
	if len(m.sessions) >= maxTabs {
		m.AddSystemMessage(fmt.Sprintf("Maximum %d tabs allowed", maxTabs))
		return nil
	}

	// Validate name if provided
	if name != "" {
		if !isValidSessionName(name) {
			m.AddSystemMessage("Invalid session name: use only letters, numbers, hyphens, and underscores")
			return nil
		}

		// Check for duplicate names
		for _, ts := range m.sessions {
			if ts.Name == name {
				m.AddSystemMessage(fmt.Sprintf("Tab with name '%s' already exists", name))
				return nil
			}
		}
	}

	// Generate unique session ID
	m.sessionIDCounter++
	sessionID := fmt.Sprintf("tab-%d-%d", m.sessionIDCounter, time.Now().Unix())
	tabID := m.sessionIDCounter

	// Determine working directory and create worktree if named
	workingDir := m.workingDir
	worktreePath := ""

	if name != "" {
		// Try to create worktree for named session
		wtp, err := m.handleWorktreeCreation(name)
		if err == nil && wtp != "" {
			worktreePath = wtp
			workingDir = wtp
		}
	}

	// Create new session
	sess := session.NewSession(sessionID, workingDir)

	// Create TabSession wrapper
	tabSession := &TabSession{
		ID:           tabID,
		Session:      sess,
		Name:         name,
		WorktreePath: worktreePath,
		Messages:     []message{},
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}

	// Add to sessions list
	m.sessions = append(m.sessions, tabSession)

	// Switch to new tab
	newIdx := len(m.sessions) - 1
	return m.handleSwitchTab(newIdx)
}

// handleSwitchTab switches to a different tab
func (m *Model) handleSwitchTab(newIdx int) tea.Cmd {
	if newIdx < 0 || newIdx >= len(m.sessions) {
		return nil
	}

	if newIdx == m.activeSessionIdx {
		return nil // Already on this tab
	}

	// Save current tab's display state (including generation state)
	m.sessions[m.activeSessionIdx].Messages = m.messages
	m.sessions[m.activeSessionIdx].LastActiveAt = time.Now()

	// Switch to new tab
	m.activeSessionIdx = newIdx
	newTabSession := m.sessions[newIdx]

	// Switch orchestrator session (via callback if available)
	shouldSwitchSession := true
	if m.generating && m.validTabIndex(m.generationTabIdx) && newIdx != m.generationTabIdx {
		// Keep orchestrator pinned to the generating tab, apply switch after completion
		m.pendingSessionIdx = newIdx
		shouldSwitchSession = false
	} else {
		m.pendingSessionIdx = -1
	}

	if shouldSwitchSession && m.onSwitchSession != nil {
		if err := m.onSwitchSession(newTabSession.Session); err != nil {
			logger.Warn("Failed to switch orchestrator session: %v", err)
			m.AddSystemMessage(fmt.Sprintf("Error switching session: %v", err))
		}
	}

	// Restore tab's display state
	m.messages = newTabSession.Messages
	m.updateViewport()

	// Save tab state to config
	if err := m.saveTabState(); err != nil {
		logger.Warn("Failed to save tab state: %v", err)
	}

	return nil
}

// handleCloseTab closes a tab
func (m *Model) handleCloseTab(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.sessions) {
		return nil
	}

	if len(m.sessions) == 1 {
		m.AddSystemMessage("Cannot close last tab. Use /quit to exit or /clear to reset.")
		return nil
	}

	// Allow closing non-active tabs even during generation
	// Only block closing the active tab if generating
	if m.generating && idx == m.activeSessionIdx {
		m.AddSystemMessage("Cannot close active tab during generation. Switch to another tab first or press ESC to stop.")
		return nil
	}

	closingTab := m.sessions[idx]

	// Auto-save session if it has messages
	if len(closingTab.Session.GetMessages()) > 0 {
		logger.Info("Auto-saving session %s on tab close", closingTab.Session.ID)
		if m.onSaveSession != nil {
			if err := m.onSaveSession(closingTab.Session); err != nil {
				logger.Warn("Failed to auto-save session on tab close: %v", err)
				m.AddSystemMessage(fmt.Sprintf("Warning: Failed to auto-save session: %v", err))
			} else {
				logger.Info("Successfully auto-saved session %s on tab close", closingTab.Session.ID)
			}
		} else {
			logger.Warn("No session save callback available for tab close autosave")
		}
	}

	// Note: We do NOT delete the worktree (per user requirement)
	if closingTab.WorktreePath != "" {
		logger.Info("Keeping worktree: %s", closingTab.WorktreePath)
	}

	// Remove from sessions list
	m.sessions = append(m.sessions[:idx], m.sessions[idx+1:]...)

	// Adjust active index if needed
	if m.activeSessionIdx >= len(m.sessions) {
		m.activeSessionIdx = len(m.sessions) - 1
	} else if m.activeSessionIdx == idx && m.activeSessionIdx > 0 {
		m.activeSessionIdx--
	}

	// Save tab state
	if err := m.saveTabState(); err != nil {
		logger.Warn("Failed to save tab state after closing: %v", err)
	}

	// Switch to adjusted active tab
	return m.handleSwitchTab(m.activeSessionIdx)
}

// renderTabBar renders the tab bar UI
func (m *Model) renderTabBar() string {
	if len(m.sessions) <= 1 {
		return "" // Don't show tab bar for single session
	}

	var tabs []string
	for i, ts := range m.sessions {
		isActive := i == m.activeSessionIdx
		tabText := ts.DisplayName()

		// Add modified indicator
		if ts.HasMessages() {
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

// saveTabState saves the current tab state to config
func (m *Model) saveTabState() error {
	if m.config == nil {
		return fmt.Errorf("config is nil")
	}

	tabState := &config.WorkspaceTabState{
		ActiveTabID:   m.sessions[m.activeSessionIdx].ID,
		TabIDs:        make([]int, len(m.sessions)),
		TabNames:      make(map[int]string),
		WorktreePaths: make(map[int]string),
	}

	for i, ts := range m.sessions {
		tabState.TabIDs[i] = ts.ID
		if ts.Name != "" {
			tabState.TabNames[ts.ID] = ts.Name
		}
		if ts.WorktreePath != "" {
			tabState.WorktreePaths[ts.ID] = ts.WorktreePath
		}
	}

	if m.config.OpenTabs == nil {
		m.config.OpenTabs = make(map[string]*config.WorkspaceTabState)
	}
	m.config.OpenTabs[m.workingDir] = tabState

	return m.config.Save(config.GetConfigPath())
}

// restoreTabs restores tabs from saved config
func (m *Model) restoreTabs() error {
	if m.config == nil || m.config.OpenTabs == nil {
		return m.createDefaultTab()
	}

	tabState, ok := m.config.OpenTabs[m.workingDir]
	if !ok || len(tabState.TabIDs) == 0 {
		return m.createDefaultTab()
	}

	// Restore each tab
	for _, tabID := range tabState.TabIDs {
		name := tabState.TabNames[tabID]
		worktreePath := tabState.WorktreePaths[tabID]

		// Verify worktree still exists
		if worktreePath != "" {
			if _, err := os.Stat(worktreePath); err != nil {
				logger.Warn("Worktree no longer exists: %s", worktreePath)
				worktreePath = ""
			}
		}

		// Determine working directory
		workingDir := m.workingDir
		if worktreePath != "" {
			workingDir = worktreePath
		}

		// Create session (TODO: load from storage if exists)
		sessionID := fmt.Sprintf("tab-%d", tabID)
		sess := session.NewSession(sessionID, workingDir)

		tabSession := &TabSession{
			ID:           tabID,
			Session:      sess,
			Name:         name,
			WorktreePath: worktreePath,
			Messages:     []message{},
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
		}

		m.sessions = append(m.sessions, tabSession)

		// Update session counter
		if tabID >= m.sessionIDCounter {
			m.sessionIDCounter = tabID + 1
		}
	}

	// Set active tab
	for i, ts := range m.sessions {
		if ts.ID == tabState.ActiveTabID {
			m.activeSessionIdx = i
			break
		}
	}

	return nil
}

// createDefaultTab creates a single default tab
func (m *Model) createDefaultTab() error {
	logger.Info("Creating default tab with working directory: %s", m.workingDir)
	defaultSession := session.NewSession("tab-1", m.workingDir)

	defaultTab := &TabSession{
		ID:           1,
		Session:      defaultSession,
		Name:         "",
		WorktreePath: "",
		Messages:     []message{},
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}

	m.sessions = []*TabSession{defaultTab}
	m.activeSessionIdx = 0
	m.sessionIDCounter = 2

	logger.Info("Default tab created. Total sessions: %d", len(m.sessions))
	return nil
}

// isValidSessionName validates a session name
func isValidSessionName(name string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, name)
	return matched
}
