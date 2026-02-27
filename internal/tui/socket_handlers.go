package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/socketclient"
)

// setupSocketModeCallbacks sets up all callbacks for socket mode operation
func (m *Model) setupSocketModeCallbacks() {
	if m.socketFactory == nil {
		return
	}

	// Chat message handler - converts socket messages to TUI messages
	m.socketFactory.SetChatMessageHandler(func(msg socketclient.ChatMessage) {
		if m.program == nil {
			return
		}

		// Find tab associated with this session
		tabIdx := -1
		for i, tab := range m.sessions {
			if tab.Session != nil && tab.Session.ID == msg.SessionID {
				tabIdx = i
				break
			}
		}

		if tabIdx < 0 {
			logger.Warn("Received chat message for unknown session: %s", msg.SessionID)
			return
		}

		// Convert socket message to TUI message format
		switch msg.Role {
		case "user":
			if tabIdx == m.activeSessionIdx {
				m.messages = append(m.messages, message{
					role:      "You",
					content:   msg.Content,
					timestamp: msg.Timestamp.Format(time.RFC3339),
				})
				m.updateViewport()
			} else {
				m.sessions[tabIdx].Messages = append(m.sessions[tabIdx].Messages, message{
					role:      "You",
					content:   msg.Content,
					timestamp: msg.Timestamp.Format(time.RFC3339),
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
	})

	// Tool call handler
	m.socketFactory.SetToolCallHandler(func(msg socketclient.ToolCall) {
		if m.program == nil {
			return
		}

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
			role:       "Tool",
			content:    msg.ToolName,
			toolName:   msg.ToolName,
			toolID:     msg.ToolID,
			toolState:  ToolStatePending,
			timestamp:  msg.Timestamp.Format(time.RFC3339),
		}

		// Add parameters as description
		if msg.Parameters != nil {
			paramsJSON := formatToolParams(msg.Parameters)
			toolMsg.description = fmt.Sprintf("Parameters: %s", paramsJSON)
		}

		m.addMessageForTab(tabIdx, "Tool", toolMsg.content)
	})

	// Tool result handler
	m.socketFactory.SetToolResultHandler(func(msg socketclient.ToolResult) {
		if m.program == nil {
			return
		}

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
				msgs[i].timestamp = msg.Timestamp.Format(time.RFC3339)
				break
			}
		}

		if tabIdx == m.activeSessionIdx {
			m.updateViewport()
		}
	})

	// Progress handler
	m.socketFactory.SetProgressHandler(func(msg socketclient.ProgressData) {
		if m.program == nil {
			return
		}

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
	})

	// Authorization handler - shows authorization dialog
	m.socketFactory.SetAuthorizationHandler(func(req socketclient.AuthorizationRequest) {
		if m.program == nil {
			return
		}

		// Find tab
		tabIdx := -1
		for i, tab := range m.sessions {
			if tab.Session != nil && tab.Session.ID == req.SessionID {
				tabIdx = i
				break
			}
		}

		if tabIdx < 0 {
			logger.Warn("Received authorization request for unknown session: %s", req.SessionID)
			// Auto-deny for unknown session
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m.socketFactory.SendAuthorizationResponse(ctx, req.AuthID, false)
			return
		}

		// Send acknowledgment
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.socketFactory.SendAuthorizationAck(ctx, req.AuthID); err != nil {
			logger.Warn("Failed to send authorization ack: %v", err)
		}

		// Store pending authorization
		m.authorizationMu.Lock()
		if m.pendingSocketAuthorizations == nil {
			m.pendingSocketAuthorizations = make(map[string]pendingSocketAuthorization)
		}
		m.pendingSocketAuthorizations[req.AuthID] = pendingSocketAuthorization{
			requestID: req.AuthID,
			toolName:  req.ToolName,
			params:    req.Parameters,
			reason:    req.Reason,
			tabID:     tabIdx,
			startTime: time.Now(),
			acked:     true,
		}
		m.authorizationCounter++

		// Create dialog
		tabName := fmt.Sprintf("Tab %d", tabIdx+1)
		m.authorizationDialog = NewAuthorizationDialog(&AuthorizationRequest{
			AuthID:       req.AuthID,
			TabID:        tabIdx,
			ToolName:     req.ToolName,
			Parameters:   req.Parameters,
			Reason:       req.Reason,
			ResponseChan: nil, // No channel in socket mode, use SendAuthorizationResponse
		}, tabName)

		m.authorizationDialogOpen = true
		m.SetOverlayActive(true)
		m.activeAuthorizationID = req.AuthID

		// Mark tab as waiting for auth
		m.sessions[tabIdx].WaitingForAuth = true

		m.authorizationMu.Unlock()

		logger.Debug("Showing socket authorization dialog for tool %s (requestID: %s)", req.ToolName, req.AuthID)
	})

	// Question handler - shows question dialog
	m.socketFactory.SetQuestionHandler(func(req socketclient.QuestionRequest) {
		if m.program == nil {
			return
		}

		// Find tab
		tabIdx := -1
		for i, tab := range m.sessions {
			if tab.Session != nil && tab.Session.ID == req.SessionID {
				tabIdx = i
				break
			}
		}

		if tabIdx < 0 {
			logger.Warn("Received question for unknown session: %s", req.SessionID)
			// Auto-cancel for unknown session
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m.socketFactory.SendQuestionResponse(ctx, req.QuestionID, nil)
			return
		}

		// Store pending question
		m.userInteractionMu.Lock()
		if m.pendingSocketQuestions == nil {
			m.pendingSocketQuestions = make(map[string]pendingSocketQuestion)
		}
		m.pendingSocketQuestions[req.QuestionID] = pendingSocketQuestion{
			requestID: req.QuestionID,
			questions: req.Questions,
			multiMode: req.MultiMode,
			tabID:     tabIdx,
			startTime: time.Now(),
		}

		// Create dialog based on mode
		if req.MultiMode && len(req.Questions) > 1 {
			// Multiple choice dialog - convert to QuestionWithOptions
			var questions []QuestionWithOptions
			for _, q := range req.Questions {
				questions = append(questions, QuestionWithOptions{
					Question: q,
					Options:  []string{},
				})
			}
			m.userQuestionDialog = NewUserQuestionDialog(questions)
		} else {
			// Single input dialog
			var question string
			for _, q := range req.Questions {
				question = q
				break
			}
			m.userQuestionDialog = NewUserInputDialog(question)
		}

		m.userQuestionDialogOpen = true
		m.activeUserInteractionID = req.QuestionID
		m.SetOverlayActive(true)

		m.userInteractionMu.Unlock()

		logger.Debug("Showing socket question dialog (requestID: %s, multiMode: %v)", req.QuestionID, req.MultiMode)
	})
}

// handleSocketAuthorizationResponse processes authorization dialog response for socket mode
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