package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// startPromptForTabSocket initiates generation for a specific tab using socket connection
func (m *Model) startPromptForTabSocket(tabIdx int, input string) tea.Cmd {
	if tabIdx < 0 || tabIdx >= len(m.sessions) {
		logger.Warn("startPromptForTabSocket: invalid tab index %d", tabIdx)
		return nil
	}

	if m.socketFactory == nil {
		logger.Error("SocketRuntimeFactory not initialized")
		return nil
	}

	tab := m.sessions[tabIdx]

	// Mark tab as generating
	m.setTabGenerating(tabIdx, true)
	m.addMessageForTab(tabIdx, "You", input)

	// Send prompt via socket
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		err := m.socketFactory.SendChat(ctx, tab, input)
		if err != nil {
			logger.Error("Failed to send chat via socket: %v", err)
			return TabGenerationCompleteMsg{
				TabID: tab.ID,
				Error: fmt.Errorf("socket chat failed: %w", err),
			}
		}

		return TabGenerationCompleteMsg{
			TabID: tab.ID,
		}
	}
}

// stopChatSocket stops the current chat in socket mode
func (m *Model) stopChatSocket() tea.Cmd {
	if m.socketFactory == nil {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := m.socketFactory.StopChat(ctx); err != nil {
			logger.Error("Failed to stop socket chat: %v", err)
		}

		return nil
	}
}

// clearChatSocket clears chat history in socket mode
func (m *Model) clearChatSocket(tabIdx int) tea.Cmd {
	if m.socketFactory == nil {
		return nil
	}

	if tabIdx < 0 || tabIdx >= len(m.sessions) {
		return nil
	}

	_ = m.sessions[tabIdx]

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := m.socketFactory.ClearChat(ctx); err != nil {
			logger.Error("Failed to clear socket chat: %v", err)
			return ErrMsg(fmt.Errorf("failed to clear chat: %w", err))
		}

		// Clear local messages
		if tabIdx == m.activeSessionIdx {
			m.messages = []message{}
		}
		m.sessions[tabIdx].Messages = []message{}

		return nil
	}
}

// handleTabGenerationCompleteSocket handles generation completion in socket mode
func (m *Model) handleTabGenerationCompleteSocket(msg TabGenerationCompleteMsg) tea.Cmd {
	tabIdx := m.findTabIndexByID(msg.TabID)
	if tabIdx < 0 {
		logger.Warn("Received TabGenerationCompleteMsg for unknown tab ID: %d", msg.TabID)
		return nil
	}

	// Mark tab as not generating
	m.setTabGenerating(tabIdx, false)

	// Process next queued prompt if any (use the socket-specific version)
	return m.processNextQueuedPromptForTabSocket(tabIdx)
}

// setTabGeneratingWithUI sets the generating state for a tab and updates UI
// This is a socket-specific wrapper that adds UI state updates
func (m *Model) setTabGeneratingWithUI(tabIdx int, generating bool) {
	// Call the base method to set generation state
	m.setTabGenerating(tabIdx, generating)

	// Update UI state if this is the active tab
	if tabIdx == m.activeSessionIdx {
		if generating {
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
	}
}

// processNextQueuedPromptForTabSocket processes the next queued prompt for a tab in socket mode
func (m *Model) processNextQueuedPromptForTabSocket(tabIdx int) tea.Cmd {
	if tabIdx < 0 || tabIdx >= len(m.sessions) {
		return nil
	}

	queue := m.queuedPrompts[tabIdx]
	if len(queue) == 0 {
		return nil
	}

	// Dequeue next prompt
	next := queue[0]
	m.queuedPrompts[tabIdx] = queue[1:]

	// Decrement count in session
	m.sessions[tabIdx].Session.DecrementQueuedUserPromptCount()

	logger.Info("Processing queued prompt for tab %d: %q", tabIdx, next)

	// Start prompt for this specific tab using socket mode
	return m.startPromptForTabSocket(tabIdx, next)
}
