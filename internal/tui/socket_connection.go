package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// SocketConnectionMsg is sent when socket connection state changes
type SocketConnectionMsg struct {
	Connected    bool
	Reconnecting bool
	Error        error
}

// SocketChatCompletedMsg is sent when a chat operation completes
type SocketChatCompletedMsg struct {
	SessionID string
	Error     error
}

// handleSocketConnection manages socket connection lifecycle
func (m *Model) handleSocketConnection() tea.Cmd {
	if m.socketFactory == nil {
		return nil
	}

	// Check connection state
	if m.socketFactory.IsConnected() {
		m.AddSystemMessage("Connected to socket server")
		return nil
	}

	// Attempt to connect
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := m.socketFactory.Connect(ctx); err != nil {
			logger.Warn("Failed to connect to socket server: %v", err)
			return SocketConnectionMsg{
				Connected:    false,
				Reconnecting: false,
				Error:        err,
			}
		}

		// Set up callbacks after successful connection
		m.setupSocketModeCallbacks()

		return SocketConnectionMsg{
			Connected:    true,
			Reconnecting: false,
		}
	}
}

// handleSocketDisconnection handles socket disconnection
func (m *Model) handleSocketDisconnection(err error) tea.Cmd {
	logger.Warn("Socket disconnected: %v", err)

	// Update footer to show disconnected state
	if err != nil {
		m.processingStatus = fmt.Sprintf("Disconnected: %v", err)
	} else {
		m.processingStatus = "Disconnected from socket server"
	}

	// Stop all generating tabs
	for i, tab := range m.sessions {
		if tab.IsGenerating() {
			m.sessions[i].SetGenerating(false)
			logger.Debug("Stopped generation for tab %d due to socket disconnection", tab.ID)
		}
	}

	// Clear spinner
	m.spinnerActive = false

	// Auto-reconnect is handled by the socket client
	// This just updates the UI state
	return nil
}

// handleSocketReconnecting handles socket reconnection attempts
func (m *Model) handleSocketReconnecting(attempt int, maxAttempts int) tea.Cmd {
	m.processingStatus = fmt.Sprintf("Reconnecting... (%d/%d)", attempt, maxAttempts)
	m.spinnerActive = true
	logger.Info("Socket reconnecting... (attempt %d/%d)", attempt, maxAttempts)
	return nil
}

// handleSocketReconnected handles successful reconnection
func (m *Model) handleSocketReconnected() tea.Cmd {
	logger.Info("Socket reconnected successfully")
	m.AddSystemMessage("Reconnected to socket server")

	// Clear reconnection status
	m.processingStatus = ""
	m.spinnerActive = false

	// Re-setup callbacks
	m.setupSocketModeCallbacks()

	return nil
}

// handleSocketAuthorizationResponseMsg processes authorization responses in socket mode
func (m *Model) handleSocketAuthorizationResponseMsg(msg SocketAuthorizationResponseMsg) tea.Cmd {
	if m.useSocketMode && m.socketFactory != nil {
		if err := m.handleSocketAuthorizationResponse(msg.RequestID, msg.Approved); err != nil {
			logger.Error("Failed to handle socket authorization response: %v", err)
		}
	}
	return nil
}

// handleSocketQuestionResponseMsg processes question responses in socket mode
func (m *Model) handleSocketQuestionResponseMsg(msg SocketQuestionResponseMsg) tea.Cmd {
	if m.useSocketMode && m.socketFactory != nil {
		if err := m.handleSocketQuestionResponse(msg.RequestID, msg.Answers); err != nil {
			logger.Error("Failed to handle socket question response: %v", err)
		}
	}
	return nil
}

// SocketAuthorizationResponseMsg is sent when user responds to an authorization dialog in socket mode
type SocketAuthorizationResponseMsg struct {
	RequestID string
	Approved  bool
}

// SocketQuestionResponseMsg is sent when user responds to a question dialog in socket mode
type SocketQuestionResponseMsg struct {
	RequestID string
	Answers   map[string]string
}

// sendSocketAuthorizationResponse sends authorization response to the socket server
func (m *Model) sendSocketAuthorizationResponse(requestID string, approved bool) {
	if m.program != nil {
		m.program.Send(SocketAuthorizationResponseMsg{
			RequestID: requestID,
			Approved:  approved,
		})
	}
}

// sendSocketQuestionResponse sends question response to the socket server
func (m *Model) sendSocketQuestionResponse(requestID string, answers map[string]string) {
	if m.program != nil {
		m.program.Send(SocketQuestionResponseMsg{
			RequestID: requestID,
			Answers:   answers,
		})
	}
}

// updateAuthorizationDialogForSocketMode updates authorization dialog handling for socket mode
func (m *Model) updateAuthorizationDialogForSocketMode(authID string, approved bool) {
	if m.useSocketMode && m.socketFactory != nil {
		// Send response via socket
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := m.socketFactory.SendAuthorizationResponse(ctx, authID, approved); err != nil {
			logger.Error("Failed to send socket authorization response: %v", err)
			m.AddSystemMessage(fmt.Sprintf("Failed to send authorization: %v", err))
		}
	}
}

// updateQuestionDialogForSocketMode updates question dialog handling for socket mode
func (m *Model) updateQuestionDialogForSocketMode(requestID string, answers map[string]string) {
	if m.useSocketMode && m.socketFactory != nil {
		// Send response via socket
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := m.socketFactory.SendQuestionResponse(ctx, requestID, answers); err != nil {
			logger.Error("Failed to send socket question response: %v", err)
			m.AddSystemMessage(fmt.Sprintf("Failed to send question response: %v", err))
		}
	}
}

// checkSocketConnectionStatus checks if socket is still connected
func (m *Model) checkSocketConnectionStatus() tea.Cmd {
	if !m.useSocketMode || m.socketFactory == nil {
		return nil
	}

	return func() tea.Msg {
		return SocketConnectionMsg{
			Connected:    m.socketFactory.IsConnected(),
			Reconnecting: m.socketFactory.IsReconnecting(),
		}
	}
}

// getSocketStatusIndicator returns a status indicator for socket connection
func (m *Model) getSocketStatusIndicator() string {
	if !m.useSocketMode {
		return ""
	}

	if m.socketFactory == nil {
		return "[SOCKET ERROR]"
	}

	if m.socketFactory.IsConnected() {
		return "[SOCKET]"
	}

	if m.socketFactory.IsReconnecting() {
		return "[SOCKET RECONNECTING...]"
	}

	return "[SOCKET DISCONNECTED]"
}
