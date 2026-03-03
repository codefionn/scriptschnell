package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/socketclient"
)

// SocketChatMessageMsg is sent when a chat message is received from the socket server
type SocketChatMessageMsg struct {
	SessionID string
	Role      string
	Content   string
	Reasoning string
}

// SocketToolCallMsg is sent when a tool call is received from the socket server
type SocketToolCallMsg struct {
	SessionID   string
	ToolID      string
	ToolName    string
	Parameters  map[string]interface{}
	Description string
}

// SocketToolResultMsg is sent when a tool result is received from the socket server
type SocketToolResultMsg struct {
	SessionID string
	ToolID    string
	Result    *string
	Error     *string
	Status    string
}

// SocketProgressMsg is sent when a progress update is received from the socket server
type SocketProgressMsg struct {
	SessionID string
	Message   string
	Status    string
	IsCompact bool
}

// SocketAuthorizationRequestMsg is sent when an authorization request is received
type SocketAuthorizationRequestMsg struct {
	SessionID  string
	AuthID     string
	ToolName   string
	Parameters map[string]interface{}
	Reason     string
}

// SocketQuestionRequestMsg is sent when a question request is received
type SocketQuestionRequestMsg struct {
	SessionID  string
	QuestionID string
	Question   string
	MultiMode  bool
	Questions  map[string]string
}

// setupSocketModeCallbacks sets up all callbacks for socket mode operation
//
//nolint:unused // Reserved for future socket mode implementation
func (m *Model) setupSocketModeCallbacks() {
	if m.socketFactory == nil {
		return
	}

	// Chat message handler - converts socket messages to TUI messages
	m.socketFactory.SetChatMessageHandler(func(msg socketclient.ChatMessage) {
		if m.program == nil {
			return
		}
		m.program.Send(SocketChatMessageMsg{
			SessionID: msg.SessionID,
			Role:      msg.Role,
			Content:   msg.Content,
			Reasoning: msg.Reasoning,
		})
	})

	// Tool call handler
	m.socketFactory.SetToolCallHandler(func(msg socketclient.ToolCall) {
		if m.program == nil {
			return
		}
		m.program.Send(SocketToolCallMsg{
			SessionID:   msg.SessionID,
			ToolID:      msg.ToolID,
			ToolName:    msg.ToolName,
			Parameters:  msg.Parameters,
			Description: msg.Description,
		})
	})

	// Tool result handler
	m.socketFactory.SetToolResultHandler(func(msg socketclient.ToolResult) {
		if m.program == nil {
			return
		}
		m.program.Send(SocketToolResultMsg{
			SessionID: msg.SessionID,
			ToolID:    msg.ToolID,
			Result:    msg.Result,
			Error:     msg.Error,
			Status:    msg.Status,
		})
	})

	// Progress handler
	m.socketFactory.SetProgressHandler(func(msg socketclient.ProgressData) {
		if m.program == nil {
			return
		}
		m.program.Send(SocketProgressMsg{
			SessionID: msg.SessionID,
			Message:   msg.Message,
			Status:    msg.Status,
			IsCompact: msg.IsCompact,
		})
	})

	// Authorization handler - shows authorization dialog
	m.socketFactory.SetAuthorizationHandler(func(req socketclient.AuthorizationRequest) {
		if m.program == nil {
			return
		}
		m.program.Send(SocketAuthorizationRequestMsg{
			SessionID:  req.SessionID,
			AuthID:     req.AuthID,
			ToolName:   req.ToolName,
			Parameters: req.Parameters,
			Reason:     req.Reason,
		})
	})

	// Question handler - shows question dialog
	m.socketFactory.SetQuestionHandler(func(req socketclient.QuestionRequest) {
		if m.program == nil {
			return
		}
		m.program.Send(SocketQuestionRequestMsg{
			SessionID:  req.SessionID,
			QuestionID: req.QuestionID,
			Question:   req.Question,
			MultiMode:  req.MultiMode,
			Questions:  req.Questions,
		})
	})
}

// handleSocketChatMessage processes a chat message from the socket server
func (m *Model) handleSocketChatMessage(msg SocketChatMessageMsg) {
	logger.Debug("[TUI] handleSocketChatMessage: session=%s role=%s content_len=%d", msg.SessionID, msg.Role, len(msg.Content))

	// Find tab associated with this session
	tabIdx := -1
	for i, tab := range m.sessions {
		if tab.Session != nil && tab.Session.ID == msg.SessionID {
			tabIdx = i
			break
		}
	}

	if tabIdx < 0 {
		logger.Warn("[TUI] Received chat message for unknown session: %s (have %d tabs)", msg.SessionID, len(m.sessions))
		for i, tab := range m.sessions {
			if tab.Session != nil {
				logger.Warn("[TUI] Tab %d has session %s", i, tab.Session.ID)
			} else {
				logger.Warn("[TUI] Tab %d has nil session", i)
			}
		}
		return
	}

	logger.Debug("[TUI] Found tab %d for session %s", tabIdx, msg.SessionID)

	// Convert socket message to TUI message format
	switch msg.Role {
	case "user":
		if tabIdx == m.activeSessionIdx {
			m.messages = append(m.messages, message{
				role:      "You",
				content:   msg.Content,
				timestamp: time.Now().Format(time.RFC3339),
			})
			m.updateViewport()
		} else {
			m.sessions[tabIdx].Messages = append(m.sessions[tabIdx].Messages, message{
				role:      "You",
				content:   msg.Content,
				timestamp: time.Now().Format(time.RFC3339),
			})
		}

	case "assistant":
		if tabIdx == m.activeSessionIdx {
			m.contentReceived = true
			m.appendAssistantChunkForTab(tabIdx, msg.Content)
		} else {
			m.appendAssistantChunkForTab(tabIdx, msg.Content)
		}

		// Handle reasoning content
		if msg.Reasoning != "" {
			msgs := m.sessions[tabIdx].Messages
			if len(msgs) > 0 {
				lastMsg := &msgs[len(msgs)-1]
				if lastMsg.role == "Assistant" {
					lastMsg.reasoning = msg.Reasoning
					if tabIdx == m.activeSessionIdx {
						m.viewportDirty = true
					}
				}
			}
		}
	}
}

// handleSocketToolCall processes a tool call from the socket server
func (m *Model) handleSocketToolCall(msg SocketToolCallMsg) {
	// Find tab
	tabIdx := -1
	for i, tab := range m.sessions {
		if tab.Session != nil && tab.Session.ID == msg.SessionID {
			tabIdx = i
			break
		}
	}

	if tabIdx < 0 {
		return
	}

	// Convert socket tool call to TUI format
	toolMsg := message{
		role:      "Tool",
		content:   msg.ToolName,
		toolName:  msg.ToolName,
		toolID:    msg.ToolID,
		toolState: ToolStatePending,
		timestamp: time.Now().Format(time.RFC3339),
	}

	// Add parameters as description
	if msg.Parameters != nil {
		paramsJSON := formatToolParams(msg.Parameters)
		toolMsg.description = fmt.Sprintf("Parameters: %s", paramsJSON)
	}

	m.addMessageForTab(tabIdx, "Tool", toolMsg.content)
}

// handleSocketToolResult processes a tool result from the socket server
func (m *Model) handleSocketToolResult(msg SocketToolResultMsg) {
	// Find tab
	tabIdx := -1
	for i, tab := range m.sessions {
		if tab.Session != nil && tab.Session.ID == msg.SessionID {
			tabIdx = i
			break
		}
	}

	if tabIdx < 0 {
		return
	}

	// Update the most recent tool message with the result
	msgs := m.sessions[tabIdx].Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].role == "Tool" && msgs[i].toolID == msg.ToolID {
			msgs[i].toolState = ToolStateCompleted
			if msg.Result != nil {
				msgs[i].fullResult = *msg.Result
			}
			if msg.Error != nil && *msg.Error != "" {
				msgs[i].toolState = ToolStateFailed
				msgs[i].content = fmt.Sprintf("%s (failed)", msgs[i].toolName)
			}
			msgs[i].timestamp = time.Now().Format(time.RFC3339)
			break
		}
	}

	if tabIdx == m.activeSessionIdx {
		m.updateViewport()
	}
}

// handleSocketProgress processes a progress update from the socket server
func (m *Model) handleSocketProgress(msg SocketProgressMsg) {
	// Find tab
	tabIdx := -1
	for i, tab := range m.sessions {
		if tab.Session != nil && tab.Session.ID == msg.SessionID {
			tabIdx = i
			break
		}
	}

	if tabIdx < 0 {
		return
	}

	// Handle status updates
	if msg.Status != "" {
		if tabIdx == m.activeSessionIdx {
			m.processingStatus = msg.Status
		}
	}

	// Handle compact progress messages (verification agent)
	if msg.IsCompact {
		m.updateOrCreateVerificationMessage(tabIdx, msg.Message)
	}
}

// handleSocketAuthorizationRequest processes an authorization request from the socket server
func (m *Model) handleSocketAuthorizationRequest(msg SocketAuthorizationRequestMsg) {
	// Find tab
	tabIdx := -1
	for i, tab := range m.sessions {
		if tab.Session != nil && tab.Session.ID == msg.SessionID {
			tabIdx = i
			break
		}
	}

	if tabIdx < 0 {
		logger.Warn("Received authorization request for unknown session: %s", msg.SessionID)
		// Auto-deny for unknown session
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.socketFactory.SendAuthorizationResponse(ctx, msg.AuthID, false)
		return
	}

	// Send acknowledgment
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.socketFactory.SendAuthorizationAck(ctx, msg.AuthID); err != nil {
		logger.Warn("Failed to send authorization ack: %v", err)
	}

	// Store pending authorization
	m.authorizationMu.Lock()
	if m.pendingSocketAuthorizations == nil {
		m.pendingSocketAuthorizations = make(map[string]pendingSocketAuthorization)
	}
	m.pendingSocketAuthorizations[msg.AuthID] = pendingSocketAuthorization{
		requestID: msg.AuthID,
		toolName:  msg.ToolName,
		params:    msg.Parameters,
		reason:    msg.Reason,
		tabID:     tabIdx,
		startTime: time.Now(),
		acked:     true,
	}
	m.authorizationCounter++

	// Create dialog
	tabName := fmt.Sprintf("Tab %d", tabIdx+1)
	m.authorizationDialog = NewAuthorizationDialog(
		&AuthorizationRequest{
			AuthID:       msg.AuthID,
			TabID:        tabIdx,
			ToolName:     msg.ToolName,
			Parameters:   msg.Parameters,
			Reason:       msg.Reason,
			ResponseChan: nil,
		}, tabName)

	m.authorizationDialogOpen = true
	m.SetOverlayActive(true)
	m.activeAuthorizationID = msg.AuthID

	// Mark tab as waiting for auth
	m.sessions[tabIdx].WaitingForAuth = true

	m.authorizationMu.Unlock()

	logger.Debug("Showing socket authorization dialog for tool %s (requestID: %s)", msg.ToolName, msg.AuthID)
}

// handleSocketQuestionRequest processes a question request from the socket server
func (m *Model) handleSocketQuestionRequest(msg SocketQuestionRequestMsg) {
	// Find tab
	tabIdx := -1
	for i, tab := range m.sessions {
		if tab.Session != nil && tab.Session.ID == msg.SessionID {
			tabIdx = i
			break
		}
	}

	if tabIdx < 0 {
		logger.Warn("Received question for unknown session: %s", msg.SessionID)
		// Auto-cancel for unknown session
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.socketFactory.SendQuestionResponse(ctx, msg.QuestionID, nil)
		return
	}

	// Store pending question
	m.userInteractionMu.Lock()
	if m.pendingSocketQuestions == nil {
		m.pendingSocketQuestions = make(map[string]pendingSocketQuestion)
	}
	m.pendingSocketQuestions[msg.QuestionID] = pendingSocketQuestion{
		requestID: msg.QuestionID,
		questions: msg.Questions,
		multiMode: msg.MultiMode,
		tabID:     tabIdx,
		startTime: time.Now(),
	}

	// Create dialog based on mode
	if msg.MultiMode && len(msg.Questions) > 1 {
		// Multiple choice dialog - convert to QuestionWithOptions
		var questions []QuestionWithOptions
		for _, q := range msg.Questions {
			questions = append(questions, QuestionWithOptions{
				Question: q,
				Options:  []string{},
			})
		}
		m.userQuestionDialog = NewUserQuestionDialog(questions)
	} else {
		// Single input dialog
		m.userQuestionDialog = NewUserInputDialog(msg.Question)
	}

	m.userQuestionDialogOpen = true
	m.activeUserInteractionID = msg.QuestionID
	m.SetOverlayActive(true)

	m.userInteractionMu.Unlock()

	logger.Debug("Showing socket question dialog (requestID: %s, multiMode: %v)", msg.QuestionID, msg.MultiMode)
}

// handleSocketAuthorizationResponse processes authorization dialog response for socket mode
//
//nolint:unused // Reserved for future socket mode implementation
func (m *Model) handleSocketAuthorizationResponse(requestID string, approved bool) error {
	m.authorizationMu.Lock()
	defer m.authorizationMu.Unlock()

	// Find pending authorization
	pending, ok := m.pendingSocketAuthorizations[requestID]
	if !ok {
		return fmt.Errorf("no pending authorization for requestID: %s", requestID)
	}

	// Send response to server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.socketFactory.SendAuthorizationResponse(ctx, requestID, approved); err != nil {
		logger.Error("Failed to send authorization response: %v", err)
		return err
	}

	logger.Debug("Sent socket authorization response (requestID: %s, approved: %v)", requestID, approved)

	// Clear tab's waiting state
	if pending.tabID >= 0 && pending.tabID < len(m.sessions) {
		m.sessions[pending.tabID].WaitingForAuth = false
	}

	// Clean up
	delete(m.pendingSocketAuthorizations, requestID)
	return nil
}

// handleSocketQuestionResponse processes question dialog response for socket mode
//
//nolint:unused // Reserved for future socket mode implementation
func (m *Model) handleSocketQuestionResponse(requestID string, answers map[string]string) error {
	m.userInteractionMu.Lock()
	defer m.userInteractionMu.Unlock()

	// Find pending question
	_, ok := m.pendingSocketQuestions[requestID]
	if !ok {
		return fmt.Errorf("no pending question for requestID: %s", requestID)
	}

	// Send response to server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.socketFactory.SendQuestionResponse(ctx, requestID, answers); err != nil {
		logger.Error("Failed to send question response: %v", err)
		return err
	}

	logger.Debug("Sent socket question response (requestID: %s, answers: %d)", requestID, len(answers))

	// Clean up
	delete(m.pendingSocketQuestions, requestID)
	return nil
}

// formatToolParams formats tool parameters for display
func formatToolParams(params map[string]interface{}) string {
	if len(params) == 0 {
		return "(none)"
	}

	var result string
	for k, v := range params {
		if result != "" {
			result += ", "
		}
		result += fmt.Sprintf("%s=%v", k, v)
	}
	return result
}
