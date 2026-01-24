package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// TUIInteractionHandler implements UserInteractionHandler for TUI mode
type TUIInteractionHandler struct {
	program        *tea.Program
	pendingDialogs map[string]*pendingTUIDialog
	mu             sync.Mutex

	// Configuration
	dialogTimeout time.Duration
}

type pendingTUIDialog struct {
	requestID    string
	responseChan chan *actor.UserInteractionResponse
	timer        *time.Timer
	displayed    bool // Confirmed dialog was shown
}

// NewTUIInteractionHandler creates a new TUI interaction handler
func NewTUIInteractionHandler(program *tea.Program) *TUIInteractionHandler {
	return &TUIInteractionHandler{
		program:        program,
		pendingDialogs: make(map[string]*pendingTUIDialog),
		dialogTimeout:  2 * time.Minute,
	}
}

// Mode returns the handler mode name
func (h *TUIInteractionHandler) Mode() string {
	return "tui"
}

// SupportsInteraction indicates whether this handler can handle the given type.
// TUI supports all interaction types.
func (h *TUIInteractionHandler) SupportsInteraction(interactionType actor.InteractionType) bool {
	return true
}

// validatePayload validates that the payload type matches the interaction type
func (h *TUIInteractionHandler) validatePayload(req *actor.UserInteractionRequest) bool {
	switch req.InteractionType {
	case actor.InteractionTypeAuthorization:
		_, ok := req.Payload.(*actor.AuthorizationPayload)
		return ok
	case actor.InteractionTypePlanningQuestion, actor.InteractionTypeUserInputSingle:
		_, ok1 := req.Payload.(*actor.UserInputSinglePayload)
		_, ok2 := req.Payload.(*actor.PlanningQuestionPayload)
		return ok1 || ok2
	case actor.InteractionTypeUserInputMultiple:
		_, ok := req.Payload.(*actor.UserInputMultiplePayload)
		return ok
	default:
		return false
	}
}

// HandleInteraction processes a user interaction request
func (h *TUIInteractionHandler) HandleInteraction(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error) {
	// For testing purposes, allow nil program but skip dialog display
	if h.program == nil {
		// In test mode, we can still process responses but won't display dialogs
		// This allows testing response handling logic without a real TUI program
		return h.handleInteractionWithoutProgram(ctx, req)
	}

	h.mu.Lock()

	// Create response channel
	respChan := make(chan *actor.UserInteractionResponse, 1)

	// Set up timeout
	timer := time.AfterFunc(h.dialogTimeout, func() {
		h.handleDialogTimeout(req.RequestID)
	})

	h.pendingDialogs[req.RequestID] = &pendingTUIDialog{
		requestID:    req.RequestID,
		responseChan: respChan,
		timer:        timer,
		displayed:    false,
	}
	h.mu.Unlock()

	// Send display message to TUI
	msg := h.createDisplayMessage(req)
	if msg != nil {
		h.program.Send(msg)
	} else {
		// Cleanup and return error
		h.cleanupPending(req.RequestID)
		return nil, fmt.Errorf("failed to create display message for interaction type %s", req.InteractionType)
	}

	// Wait for response with context cancellation
	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		h.cleanupPending(req.RequestID)
		return &actor.UserInteractionResponse{
			RequestID: req.RequestID,
			Cancelled: true,
			Error:     ctx.Err(),
		}, nil
	}
}

// handleInteractionWithoutProgram handles interactions when no TUI program is available (test mode)
func (h *TUIInteractionHandler) handleInteractionWithoutProgram(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error) {
	// Validate payload type based on interaction type
	if !h.validatePayload(req) {
		return nil, fmt.Errorf("invalid payload type for interaction type %s", req.InteractionType)
	}

	h.mu.Lock()

	// Create response channel
	respChan := make(chan *actor.UserInteractionResponse, 1)

	// Set up timeout
	timer := time.AfterFunc(h.dialogTimeout, func() {
		h.handleDialogTimeout(req.RequestID)
	})

	h.pendingDialogs[req.RequestID] = &pendingTUIDialog{
		requestID:    req.RequestID,
		responseChan: respChan,
		timer:        timer,
		displayed:    false,
	}
	h.mu.Unlock()

	// Wait for response with context cancellation
	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		h.cleanupPending(req.RequestID)
		return &actor.UserInteractionResponse{
			RequestID: req.RequestID,
			Cancelled: true,
			Error:     ctx.Err(),
		}, nil
	}
}

// createDisplayMessage creates the appropriate TUI message for the request type
func (h *TUIInteractionHandler) createDisplayMessage(req *actor.UserInteractionRequest) tea.Msg {
	switch req.InteractionType {
	case actor.InteractionTypeAuthorization:
		payload, ok := req.Payload.(*actor.AuthorizationPayload)
		if !ok {
			logger.Error("TUIInteractionHandler: invalid payload for authorization")
			return nil
		}
		// Create an AuthorizationRequest compatible with existing TUI handling
		// Note: We don't set ResponseChan since we use our own response mechanism
		return TUIAuthorizationRequestMsg{
			RequestID:  req.RequestID,
			TabID:      req.TabID,
			ToolName:   payload.ToolName,
			Parameters: payload.Parameters,
			Reason:     payload.Reason,
		}
	case actor.InteractionTypePlanningQuestion, actor.InteractionTypeUserInputSingle:
		payload, ok := req.Payload.(*actor.UserInputSinglePayload)
		if !ok {
			// Try planning question payload
			planPayload, planOk := req.Payload.(*actor.PlanningQuestionPayload)
			if !planOk {
				logger.Error("TUIInteractionHandler: invalid payload for user input")
				return nil
			}
			return TUIUserInputRequestMsg{
				RequestID: req.RequestID,
				TabID:     req.TabID,
				Question:  planPayload.Question,
			}
		}
		return TUIUserInputRequestMsg{
			RequestID: req.RequestID,
			TabID:     req.TabID,
			Question:  payload.Question,
		}
	case actor.InteractionTypeUserInputMultiple:
		payload, ok := req.Payload.(*actor.UserInputMultiplePayload)
		if !ok {
			logger.Error("TUIInteractionHandler: invalid payload for multiple questions")
			return nil
		}
		return TUIMultipleQuestionsRequestMsg{
			RequestID:       req.RequestID,
			TabID:           req.TabID,
			Questions:       payload.FormattedQuestions,
			ParsedQuestions: payload.ParsedQuestions,
		}
	default:
		logger.Error("TUIInteractionHandler: unknown interaction type: %v", req.InteractionType)
		return nil
	}
}

// HandleDialogDisplayed is called by TUI when dialog is shown (for acknowledgment)
func (h *TUIInteractionHandler) HandleDialogDisplayed(requestID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if pending, exists := h.pendingDialogs[requestID]; exists {
		pending.displayed = true
		logger.Debug("TUIInteractionHandler: dialog displayed for request %s", requestID)
	}
}

// HandleAuthorizationResponse is called by TUI when user responds to authorization dialog
func (h *TUIInteractionHandler) HandleAuthorizationResponse(requestID string, approved bool) {
	h.handleResponse(requestID, &actor.UserInteractionResponse{
		RequestID:    requestID,
		Approved:     approved,
		Acknowledged: true,
	})
}

// HandleUserInputResponse is called by TUI when user provides text input
func (h *TUIInteractionHandler) HandleUserInputResponse(requestID string, answer string, cancelled bool) {
	h.handleResponse(requestID, &actor.UserInteractionResponse{
		RequestID:    requestID,
		Answer:       answer,
		Cancelled:    cancelled,
		Acknowledged: true,
	})
}

// HandleMultipleAnswersResponse is called by TUI when user answers multiple questions
func (h *TUIInteractionHandler) HandleMultipleAnswersResponse(requestID string, answers map[string]string, cancelled bool) {
	h.handleResponse(requestID, &actor.UserInteractionResponse{
		RequestID:    requestID,
		Answers:      answers,
		Cancelled:    cancelled,
		Acknowledged: true,
	})
}

// handleResponse sends a response for a pending request
func (h *TUIInteractionHandler) handleResponse(requestID string, resp *actor.UserInteractionResponse) {
	h.mu.Lock()
	pending, exists := h.pendingDialogs[requestID]
	if !exists {
		h.mu.Unlock()
		logger.Warn("TUIInteractionHandler: no pending dialog for request %s", requestID)
		return
	}

	pending.timer.Stop()
	delete(h.pendingDialogs, requestID)
	h.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			logger.Warn("TUIInteractionHandler: response channel closed for request %s", requestID)
		}
	}()

	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()

	// Use a short timeout for sending response.
	// This gives the receiver a brief moment to be ready while avoiding indefinite blocking.
	select {
	case pending.responseChan <- resp:
		logger.Debug("TUIInteractionHandler: response sent for request %s", requestID)
	case <-timer.C:
		logger.Error("TUIInteractionHandler: timeout sending response for request %s after 1 second - receiver may have timed out or been cancelled", requestID)
	}
}

// handleDialogTimeout is called when a dialog times out
func (h *TUIInteractionHandler) handleDialogTimeout(requestID string) {
	h.mu.Lock()
	pending, exists := h.pendingDialogs[requestID]
	if !exists {
		h.mu.Unlock()
		return
	}

	delete(h.pendingDialogs, requestID)
	h.mu.Unlock()

	logger.Warn("TUIInteractionHandler: dialog timed out for request %s", requestID)

	defer func() {
		if r := recover(); r != nil {
			logger.Warn("TUIInteractionHandler: response channel closed for request %s", requestID)
		}
	}()

	select {
	case pending.responseChan <- &actor.UserInteractionResponse{
		RequestID: requestID,
		TimedOut:  true,
		Error:     fmt.Errorf("dialog timed out after %v", h.dialogTimeout),
	}:
	default:
	}
}

// cleanupPending cleans up a pending dialog
func (h *TUIInteractionHandler) cleanupPending(requestID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if pending, exists := h.pendingDialogs[requestID]; exists {
		pending.timer.Stop()
		delete(h.pendingDialogs, requestID)
	}
}

// SetProgram updates the program reference (useful for initialization)
func (h *TUIInteractionHandler) SetProgram(program *tea.Program) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.program = program
}

// TUIAuthorizationRequestMsg is sent to TUI to display an authorization dialog via the handler
type TUIAuthorizationRequestMsg struct {
	RequestID  string
	TabID      int
	ToolName   string
	Parameters map[string]interface{}
	Reason     string
}

// TUIUserInputRequestMsg is sent to TUI to display a user input dialog via the handler
type TUIUserInputRequestMsg struct {
	RequestID string
	TabID     int
	Question  string
}

// TUIMultipleQuestionsRequestMsg is sent to TUI to display multiple questions dialog via the handler
type TUIMultipleQuestionsRequestMsg struct {
	RequestID       string
	TabID           int
	Questions       string
	ParsedQuestions []actor.QuestionWithOptions
}
