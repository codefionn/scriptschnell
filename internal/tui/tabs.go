package tui

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

const maxTabs = 10

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
		ID:                 tabID,
		Session:            sess,
		Name:               name,
		WorktreePath:       worktreePath,
		Messages:           []message{},
		CreatedAt:          time.Now(),
		LastActiveAt:       time.Now(),
		ContextFreePercent: 100,
		Runtime:            nil,   // Lazy-loaded on first prompt
		Generating:         false, // Not generating initially
		WaitingForAuth:     false, // Not waiting for auth initially
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

	// Save current tab's display state
	m.sessions[m.activeSessionIdx].Messages = m.messages
	m.sessions[m.activeSessionIdx].LastActiveAt = time.Now()

	// Switch to new tab (just UI, no orchestrator switching!)
	m.activeSessionIdx = newIdx
	newTabSession := m.sessions[newIdx]
	m.messages = newTabSession.Messages
	m.openRouterUsage = newTabSession.OpenRouterUsage
	m.thinkingTokens = newTabSession.ThinkingTokens
	m.contextFreePercent = newTabSession.ContextFreePercent
	m.contextWindow = newTabSession.ContextWindow
	m.updateViewport()

	// Update context file if new tab has a runtime
	if newTabSession.Runtime != nil {
		m.contextFile = newTabSession.Runtime.Orchestrator.GetExtendedContextFile()
	}
	m.updateTodoClientForTab(newIdx)

	// Update processing status/spinner based on new tab's state
	if newTabSession.IsGenerating() {
		m.processingStatus = "Generating..."
		if !m.animationsDisabled {
			m.spinnerActive = true
		}
	} else {
		m.processingStatus = ""
		if !m.animationsDisabled {
			m.spinnerActive = false
		}
	}

	// Save tab state to config
	if err := m.saveTabState(); err != nil {
		logger.Warn("Failed to save tab state: %v", err)
	}

	logger.Debug("Switched to tab %d (ID: %d, generating: %v)", newIdx, newTabSession.ID, newTabSession.IsGenerating())
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

	closingTab := m.sessions[idx]

	// Don't allow closing if this tab is generating
	if closingTab.IsGenerating() {
		m.AddSystemMessage("Cannot close tab during generation. Press ESC to stop first.")
		logger.Warn("Attempted to close generating tab %d", closingTab.ID)
		return nil
	}

	// Auto-save session if it has messages
	if len(closingTab.Session.GetMessages()) > 0 {
		logger.Info("Auto-saving session %s on tab close", closingTab.Session.ID)
		// Use tab's runtime to save if available
		if closingTab.Runtime != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := closingTab.Runtime.Orchestrator.SaveCurrentSession(ctx); err != nil {
				logger.Warn("Failed to auto-save session on tab close: %v", err)
			} else {
				logger.Info("Successfully auto-saved session %s on tab close", closingTab.Session.ID)
			}
		} else if m.onSaveSession != nil {
			if err := m.onSaveSession(closingTab.Session); err != nil {
				logger.Warn("Failed to auto-save session on tab close: %v", err)
				m.AddSystemMessage(fmt.Sprintf("Warning: Failed to auto-save session: %v", err))
			} else {
				logger.Info("Successfully auto-saved session %s on tab close", closingTab.Session.ID)
			}
		}
	}

	// Cleanup runtime via factory
	if m.factory != nil && closingTab.Runtime != nil {
		if err := m.factory.DestroyTabRuntime(closingTab.ID); err != nil {
			logger.Warn("Failed to destroy runtime for tab %d: %v", closingTab.ID, err)
		} else {
			logger.Info("Successfully destroyed runtime for tab %d", closingTab.ID)
		}
	}

	// Note: We do NOT delete the worktree (per user requirement)
	if closingTab.WorktreePath != "" {
		logger.Info("Keeping worktree: %s", closingTab.WorktreePath)
	}

	// Remove from sessions list
	m.sessions = append(m.sessions[:idx], m.sessions[idx+1:]...)

	// Clean up queued prompts for this tab
	delete(m.queuedPrompts, idx)

	// Clean up concurrent generation tracking
	m.concurrentGensMu.Lock()
	delete(m.concurrentGens, closingTab.ID)
	m.concurrentGensMu.Unlock()

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

	logger.Info("Closed tab %d (ID: %d)", idx, closingTab.ID)

	// Switch to adjusted active tab
	return m.handleSwitchTab(m.activeSessionIdx)
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

	// Use thread-safe method to set tab state
	m.config.SetOpenTabState(m.workingDir, tabState)

	return m.config.Save(config.GetConfigPath())
}

// restoreTabs restores tabs from saved config
func (m *Model) restoreTabs() error {
	if m.config == nil {
		return m.createDefaultTab()
	}

	// Use thread-safe method to get tab state
	tabState, ok := m.config.GetOpenTabState(m.workingDir)
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
			ID:                 tabID,
			Session:            sess,
			Name:               name,
			WorktreePath:       worktreePath,
			Messages:           []message{},
			CreatedAt:          time.Now(),
			LastActiveAt:       time.Now(),
			ContextFreePercent: 100,
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
		ID:                 1,
		Session:            defaultSession,
		Name:               "",
		WorktreePath:       "",
		Messages:           []message{},
		CreatedAt:          time.Now(),
		LastActiveAt:       time.Now(),
		ContextFreePercent: 100,
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
