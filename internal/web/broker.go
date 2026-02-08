package web

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/orchestrator"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/securemem"
	"github.com/codefionn/scriptschnell/internal/session"
)

// pendingAuthorization tracks an authorization request waiting for user response
type pendingAuthorization struct {
	toolName   string
	parameters map[string]interface{}
	reason     string
	response   chan bool
	ack        chan struct{} // signals that the frontend displayed the dialog
}

// pendingQuestion tracks a question request waiting for user response
type pendingQuestion struct {
	question  string
	questions []QuestionItem
	multiMode bool
	response  chan string            // Single answer
	multiResp chan map[string]string // Multiple answers (question -> answer)
}

// MessageBroker handles the orchestration of LLM interactions for web clients
type MessageBroker struct {
	session           *session.Session
	orchestrator      *orchestrator.Orchestrator
	cfg               *config.Config
	providerMgr       *provider.Manager
	initialized       bool
	pendingAuthMu     sync.Mutex
	pendingAuths      map[string]*pendingAuthorization // authID -> pending auth
	authCounter       int
	sendMessage       func(*WebMessage) // callback to send messages to client
	pendingQuestionMu sync.Mutex
	pendingQuestions  map[string]*pendingQuestion // questionID -> pending question
	questionCounter   int
}

// NewMessageBroker creates a new message broker
func NewMessageBroker() *MessageBroker {
	return &MessageBroker{
		pendingAuths:     make(map[string]*pendingAuthorization),
		pendingQuestions: make(map[string]*pendingQuestion),
	}
}

// InitializeSession initializes a new session
func (mb *MessageBroker) InitializeSession(cfg *config.Config, providerMgr *provider.Manager, secretsPassword *securemem.String, requireSandboxAuth bool) error {
	mb.cfg = cfg
	mb.providerMgr = providerMgr

	// Create new session
	sess := session.NewSession(session.GenerateID(), cfg.WorkingDir)
	mb.session = sess

	// Create filesystem
	filesystem := fs.NewCachedFS(
		cfg.WorkingDir,
		time.Duration(cfg.CacheTTL)*time.Second,
		cfg.MaxCacheEntries,
	)

	// Create orchestrator
	orch, err := orchestrator.NewOrchestratorWithSharedResources(
		cfg,
		providerMgr,
		false, // cliMode
		filesystem,
		sess,
		nil, // sessionStorageRef
		nil, // domainBlockerRef
		requireSandboxAuth,
	)
	if err != nil {
		filesystem.Close()
		return fmt.Errorf("failed to create orchestrator: %w", err)
	}

	mb.orchestrator = orch

	// Set up user interaction handler for web mode so sandbox host functions
	// can prompt the user for authorization during WASM execution
	handler := &webInteractionHandler{broker: mb}
	if err := orch.SetUserInteractionHandler(handler); err != nil {
		logger.Warn("Failed to set web interaction handler: %v", err)
	}

	mb.initialized = true

	return nil
}

// ProcessUserMessage processes a user message through the orchestrator
func (mb *MessageBroker) ProcessUserMessage(ctx context.Context, message string, callback func(*WebMessage)) error {
	if !mb.initialized {
		return fmt.Errorf("session not initialized")
	}

	// Store the callback for authorization requests
	mb.sendMessage = callback

	// Send user message to callback
	callback(&WebMessage{
		Type:    MessageTypeChat,
		Role:    "user",
		Content: message,
	})

	// Track messages for callback
	pendingMessages := make(chan *session.Message, 100)
	pendingToolCalls := make(chan ToolCallMsg, 100)
	pendingToolResults := make(chan ToolResultMsg, 100)
	pendingErrors := make(chan error, 10)

	// Create auth callback - sends request to web client and waits for response
	authCallback := func(toolName string, params map[string]interface{}, reason string) (bool, error) {
		return mb.handleAuthorization(ctx, toolName, params, reason, callback)
	}

	// Create question callback for planning agent
	questionCallback := func(question string) (string, error) {
		answer, _, err := mb.handleQuestion(ctx, question, nil, false, callback)
		return answer, err
	}

	// Set the question callback on the orchestrator
	mb.orchestrator.SetUserInputCallback(questionCallback)

	// Create tool call callback - now sends compact interactions
	toolCallCallback := func(toolName, toolID string, parameters map[string]interface{}) error {
		// Extract description from parameters if present
		var description string
		if desc, ok := parameters["description"]; ok {
			if descStr, ok := desc.(string); ok {
				description = descStr
			}
		}
		// Send compact tool interaction message
		callback(
			&WebMessage{
				Type:       MessageTypeToolInteraction,
				ToolName:   toolName,
				ToolID:     toolID,
				Parameters: parameters,
				Status:     "calling",
				Compact:    true,
				Description: description,
			})
		return nil
	}

	// Create tool result callback - updates existing compact interaction
	toolResultCallback := func(toolName, toolID, result, errorMsg string) error {
		callback(
			&WebMessage{
				Type:    MessageTypeToolInteraction,
				ToolID:  toolID,
				Result:  result,
				Error:   errorMsg,
				Status:  "completed",
				Compact: true,
			})
		return nil
	}

	// Create progress callback - filter out tool call status messages
	progressCallback := func(msg progress.Update) error {
		// Skip tool calling status messages for web UI
		if strings.Contains(msg.Message, "Calling tool:") {
			return nil
		}
		// Skip "Thinking..." messages
		if msg.Message == "Thinking..." {
			return nil
		}
		// Only send non-ephemeral important messages
		if !msg.Ephemeral && msg.Message != "" {
			logger.Debug("Progress: %s", msg.Message)
			callback(&WebMessage{
				Type:    MessageTypeSystem,
				Content: msg.Message,
			})
		}
		return nil
	}

	// Start goroutine to send responses to callback
	// Note: Tool calls and results are now handled directly via callbacks
	// This goroutine only handles assistant messages and errors
	done := make(chan struct{})
	go func() {
		for {
			select {
			case msg := <-pendingMessages:
				if msg.Role == "assistant" {
					callback(&WebMessage{
						Type:    MessageTypeChat,
						Role:    "assistant",
						Content: msg.Content,
					})
				}
			case tc := <-pendingToolCalls:
				// Tool calls are now handled directly via callback
				_ = tc // Avoid unused variable error
				continue
			case tr := <-pendingToolResults:
				// Tool results are now handled directly via callback
				_ = tr // Avoid unused variable error
				continue
			case err := <-pendingErrors:
				callback(&WebMessage{
					Type:    MessageTypeError,
					Content: err.Error(),
				})
			case <-done:
				return
			}
		}
	}()

	// Process through orchestrator
	err := mb.orchestrator.ProcessPromptWithVerification(
		ctx,
		message,
		progressCallback,
		nil, // contextCallback
		authCallback,
		toolCallCallback,
		toolResultCallback,
		nil, // openRouterUsageCallback
	)

	// Signal completion
	close(done)

	// Wait for the response goroutine to finish
	// This prevents sending to closed channels
	select {
	case <-done:
	case <-time.After(consts.Timeout5Seconds):
		logger.Warn("Timeout waiting for response goroutine to finish")
	}

	if err != nil {
		pendingErrors <- err
		return err
	}

	return nil
}

// GetSession returns the current session
func (mb *MessageBroker) GetSession() *session.Session {
	return mb.session
}

// GetOrchestrator returns the current orchestrator
func (mb *MessageBroker) GetOrchestrator() *orchestrator.Orchestrator {
	return mb.orchestrator
}

// GetProviderManager returns the provider manager
func (mb *MessageBroker) GetProviderManager() *provider.Manager {
	return mb.providerMgr
}

// Stop stops the current session operations
func (mb *MessageBroker) Stop() error {
	if mb.orchestrator != nil {
		mb.orchestrator.Stop()
	}
	return nil
}

// GetConfig returns the config
func (mb *MessageBroker) GetConfig() *config.Config {
	return mb.cfg
}

// Close cleans up resources
func (mb *MessageBroker) Close() error {
	if mb.orchestrator != nil {
		return mb.orchestrator.Close()
	}
	return nil
}

// webInteractionHandler implements actor.UserInteractionHandler for web mode.
// It delegates authorization requests to the broker's existing web-based authorization mechanism.
type webInteractionHandler struct {
	broker *MessageBroker
}

func (h *webInteractionHandler) Mode() string { return "web" }

func (h *webInteractionHandler) SupportsInteraction(interactionType actor.InteractionType) bool {
	return interactionType == actor.InteractionTypeAuthorization
}

func (h *webInteractionHandler) HandleInteraction(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error) {
	if req.InteractionType != actor.InteractionTypeAuthorization {
		return &actor.UserInteractionResponse{
			RequestID: req.RequestID,
			Approved:  false,
			Error:     fmt.Errorf("web mode does not support %d interactions", req.InteractionType),
		}, nil
	}

	payload, ok := req.Payload.(*actor.AuthorizationPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload type for authorization: expected *AuthorizationPayload, got %T", req.Payload)
	}

	callback := h.broker.sendMessage
	if callback == nil {
		return &actor.UserInteractionResponse{
			RequestID: req.RequestID,
			Approved:  false,
			Error:     fmt.Errorf("no active web session to send authorization request"),
		}, nil
	}

	approved, err := h.broker.handleAuthorization(ctx, payload.ToolName, payload.Parameters, payload.Reason, callback)
	if err != nil {
		return &actor.UserInteractionResponse{
			RequestID: req.RequestID,
			Approved:  false,
			Error:     err,
		}, nil
	}

	return &actor.UserInteractionResponse{
		RequestID:    req.RequestID,
		Approved:     approved,
		Acknowledged: true,
	}, nil
}

// handleAuthorization handles an authorization request by sending it to the web client
// and waiting for the response. Uses a two-phase timeout:
// Phase 1 (30s): Wait for the frontend to acknowledge it displayed the dialog.
// Phase 2 (10min): Wait for the user's actual approve/deny response.
func (mb *MessageBroker) handleAuthorization(ctx context.Context, toolName string, params map[string]interface{}, reason string, callback func(*WebMessage)) (bool, error) {
	// Generate unique auth ID
	mb.pendingAuthMu.Lock()
	mb.authCounter++
	authID := fmt.Sprintf("auth-%d-%d", time.Now().Unix(), mb.authCounter)

	// Create response and ack channels
	responseChan := make(chan bool, 1)
	ackChan := make(chan struct{}, 1)

	// Store pending authorization
	mb.pendingAuths[authID] = &pendingAuthorization{
		toolName:   toolName,
		parameters: params,
		reason:     reason,
		response:   responseChan,
		ack:        ackChan,
	}
	mb.pendingAuthMu.Unlock()

	// Cleanup helper
	cleanup := func() {
		mb.pendingAuthMu.Lock()
		delete(mb.pendingAuths, authID)
		mb.pendingAuthMu.Unlock()
	}

	logger.Debug("Authorization request for tool %s (authID: %s)", toolName, authID)

	// Send authorization request to web client
	callback(&WebMessage{
		Type:       MessageTypeAuthorizationRequest,
		AuthID:     authID,
		ToolName:   toolName,
		Parameters: params,
		Reason:     reason,
	})

	// Phase 1: Wait for ack (frontend confirmed dialog is displayed) or early response
	select {
	case approved := <-responseChan:
		// User responded before ack (fast user) — return immediately
		logger.Debug("Authorization response received (before ack) for %s: %v", authID, approved)
		cleanup()
		return approved, nil
	case <-ackChan:
		// Frontend confirmed dialog is displayed — proceed to phase 2
		logger.Debug("Authorization ack received for %s, waiting for user response", authID)
	case <-ctx.Done():
		logger.Debug("Authorization cancelled during ack wait for %s", authID)
		cleanup()
		return false, ctx.Err()
	case <-time.After(consts.Timeout30Seconds):
		logger.Error("Authorization ack timeout for %s — dialog was not displayed", authID)
		cleanup()
		return false, fmt.Errorf("authorization timed out: dialog was not displayed by the frontend within 30 seconds")
	}

	// Phase 2: Wait for user's approve/deny response (generous timeout)
	select {
	case approved := <-responseChan:
		logger.Debug("Authorization response received for %s: %v", authID, approved)
		cleanup()
		return approved, nil
	case <-ctx.Done():
		logger.Debug("Authorization cancelled for %s", authID)
		cleanup()
		return false, ctx.Err()
	case <-time.After(consts.Timeout10Minutes):
		logger.Error("Authorization response timeout for %s", authID)
		cleanup()
		return false, fmt.Errorf("authorization timed out after 10 minutes waiting for user response")
	}
}

// HandleAuthorizationResponse handles a response from the web client for an authorization request
func (mb *MessageBroker) HandleAuthorizationResponse(authID string, approved bool) error {
	mb.pendingAuthMu.Lock()
	defer mb.pendingAuthMu.Unlock()

	auth, ok := mb.pendingAuths[authID]
	if !ok {
		return fmt.Errorf("no pending authorization with ID %s", authID)
	}

	// Send response (non-blocking since channel is buffered)
	select {
	case auth.response <- approved:
		logger.Debug("Authorization response sent for %s: %v", authID, approved)
	default:
		logger.Warn("Authorization response channel full for %s", authID)
	}

	return nil
}

// HandleAuthorizationAck handles an ack from the web client confirming the authorization dialog was displayed
func (mb *MessageBroker) HandleAuthorizationAck(authID string) error {
	mb.pendingAuthMu.Lock()
	defer mb.pendingAuthMu.Unlock()

	auth, ok := mb.pendingAuths[authID]
	if !ok {
		return fmt.Errorf("no pending authorization with ID %s", authID)
	}

	// Signal ack (non-blocking since channel is buffered)
	select {
	case auth.ack <- struct{}{}:
		logger.Debug("Authorization ack sent for %s", authID)
	default:
		logger.Warn("Authorization ack channel full for %s (already acked)", authID)
	}

	return nil
}

// handleQuestion handles a question request from the planning agent
// It sends the question to the web client and waits for a response
func (mb *MessageBroker) handleQuestion(ctx context.Context, question string, questions []QuestionItem, multiMode bool, callback func(*WebMessage)) (string, map[string]string, error) {
	// Generate unique question ID
	mb.pendingQuestionMu.Lock()
	mb.questionCounter++
	questionID := fmt.Sprintf("question-%d-%d", time.Now().Unix(), mb.questionCounter)

	// Create response channels
	responseChan := make(chan string, 1)
	multiResponseChan := make(chan map[string]string, 1)

	// Store pending question
	mb.pendingQuestions[questionID] = &pendingQuestion{
		question:  question,
		questions: questions,
		multiMode: multiMode,
		response:  responseChan,
		multiResp: multiResponseChan,
	}
	mb.pendingQuestionMu.Unlock()

	logger.Debug("Question request (questionID: %s, multiMode: %v)", questionID, multiMode)

	// Send question request to web client
	callback(&WebMessage{
		Type:       MessageTypeQuestionRequest,
		QuestionID: questionID,
		Question:   question,
		Questions:  questions,
		MultiMode:  multiMode,
	})

	// Wait for response with timeout
	select {
	case answer := <-responseChan:
		logger.Debug("Single question response received for %s: %s", questionID, answer)
		// Cleanup
		mb.pendingQuestionMu.Lock()
		delete(mb.pendingQuestions, questionID)
		mb.pendingQuestionMu.Unlock()
		return answer, nil, nil
	case multiResp := <-multiResponseChan:
		logger.Debug("Multi question response received for %s: %v", questionID, multiResp)
		// Cleanup
		mb.pendingQuestionMu.Lock()
		delete(mb.pendingQuestions, questionID)
		mb.pendingQuestionMu.Unlock()
		return "", multiResp, nil
	case <-ctx.Done():
		logger.Debug("Question cancelled for %s", questionID)
		// Cleanup
		mb.pendingQuestionMu.Lock()
		delete(mb.pendingQuestions, questionID)
		mb.pendingQuestionMu.Unlock()
		return "", nil, ctx.Err()
	case <-time.After(consts.Timeout5Minutes):
		logger.Error("Question timeout for %s", questionID)
		// Cleanup
		mb.pendingQuestionMu.Lock()
		delete(mb.pendingQuestions, questionID)
		mb.pendingQuestionMu.Unlock()
		return "", nil, fmt.Errorf("question timeout after 5 minutes")
	}
}

// HandleQuestionResponse handles a response from the web client for a question request
func (mb *MessageBroker) HandleQuestionResponse(questionID string, answer string, answers map[string]string) error {
	mb.pendingQuestionMu.Lock()
	defer mb.pendingQuestionMu.Unlock()

	q, ok := mb.pendingQuestions[questionID]
	if !ok {
		return fmt.Errorf("no pending question with ID %s", questionID)
	}

	// Send response based on mode
	if q.multiMode {
		// Multi-question mode
		select {
		case q.multiResp <- answers:
			logger.Debug("Multi question response sent for %s", questionID)
		default:
			logger.Warn("Multi question response channel full for %s", questionID)
		}
	} else {
		// Single question mode
		select {
		case q.response <- answer:
			logger.Debug("Single question response sent for %s: %s", questionID, answer)
		default:
			logger.Warn("Question response channel full for %s", questionID)
		}
	}

	return nil
}

// ToolCallMsg represents a tool call message
type ToolCallMsg struct {
	Name        string
	ID          string
	Params      map[string]interface{}
	Description string // Human-readable description of what the tool is doing
}

// ToolResultMsg represents a tool result message
type ToolResultMsg struct {
	ID     string
	Result interface{}
	Error  string
}
