package socketserver

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
	ack        chan struct{} // signals that the client displayed the dialog
}

// pendingQuestion tracks a question request waiting for user response
type pendingQuestion struct {
	question  string
	questions map[string]string // question_id -> question text
	multiMode bool
	response  chan string            // Single answer
	multiResp chan map[string]string // Multiple answers (question_id -> answer)
}

// MessageBroker handles the orchestration of LLM interactions for socket clients
type MessageBroker struct {
	session            *session.Session
	orchestrator       *orchestrator.Orchestrator
	cfg                *config.Config
	providerMgr        *provider.Manager
	secretsPassword    *securemem.String
	initialized        bool
	sessionStorage     *session.SessionStorage
	requireSandboxAuth bool

	pendingAuthMu sync.Mutex
	pendingAuths  map[string]*pendingAuthorization // authID -> pending auth
	authCounter   int

	sendMessage func(*BaseMessage) // callback to send messages to client

	pendingQuestionMu sync.Mutex
	pendingQuestions  map[string]*pendingQuestion // questionID -> pending question
	questionCounter   int
}

// NewMessageBroker creates a new message broker
func NewMessageBroker() *MessageBroker {
	mb := &MessageBroker{
		pendingAuths:     make(map[string]*pendingAuthorization),
		pendingQuestions: make(map[string]*pendingQuestion),
	}

	storage, err := session.NewSessionStorage()
	if err != nil {
		logger.Warn("Failed to initialize session storage: %v", err)
	} else {
		mb.sessionStorage = storage
	}

	return mb
}

// SetDependencies sets the provider manager and secrets password
func (mb *MessageBroker) SetDependencies(providerMgr *provider.Manager, secretsPassword *securemem.String, cfg *config.Config) {
	mb.providerMgr = providerMgr
	mb.secretsPassword = secretsPassword
	mb.cfg = cfg
}

// InitializeSession initializes a new session with the given configuration.
// If a session and orchestrator already exist (e.g. after loading a saved session), this is a no-op.
func (mb *MessageBroker) InitializeSession(
	cfg *config.Config,
	providerMgr *provider.Manager,
	secretsPassword *securemem.String,
	existingSession *session.Session,
) error {
	mb.cfg = cfg
	mb.providerMgr = providerMgr

	// Idempotent: if we already have a live session+orchestrator, skip re-init
	if mb.initialized && mb.session != nil && mb.orchestrator != nil {
		return nil
	}

	// Use existing session if provided, otherwise create new
	sess := existingSession
	if sess == nil {
		sess = session.NewSession(session.GenerateID(), cfg.WorkingDir)
	}
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
		nil,
		nil,
		mb.requireSandboxAuth,
	)
	if err != nil {
		filesystem.Close()
		return fmt.Errorf("failed to create orchestrator: %w", err)
	}

	mb.orchestrator = orch

	// Set up user interaction handler for socket mode so sandbox host functions
	// can prompt the user for authorization during WASM execution
	handler := &socketInteractionHandler{broker: mb}
	if err := orch.SetUserInteractionHandler(handler); err != nil {
		logger.Warn("Failed to set socket interaction handler: %v", err)
	}

	mb.initialized = true

	return nil
}

// SetMessageCallback sets the callback function for sending messages to the client
func (mb *MessageBroker) SetMessageCallback(callback func(*BaseMessage)) {
	mb.sendMessage = callback
}

// ProcessUserMessage processes a user message through the orchestrator
// This is the main entry point for prompt submission over the socket
func (mb *MessageBroker) ProcessUserMessage(ctx context.Context, message string, requestID string) error {
	if !mb.initialized {
		return fmt.Errorf("session not initialized")
	}

	if mb.sendMessage == nil {
		return fmt.Errorf("message callback not set")
	}

	// Send user message to client
	mb.sendMessage(NewRequest(
		MessageTypeChatMessage,
		requestID,
		map[string]interface{}{
			"role":    "user",
			"content": message,
		},
	))

	// Create auth callback - sends request to client and waits for response
	authCallback := func(toolName string, params map[string]interface{}, reason string) (bool, error) {
		return mb.handleAuthorization(ctx, toolName, params, reason, requestID)
	}

	// Create question callback for planning agent
	questionCallback := func(question string) (string, error) {
		answer, _, err := mb.handleQuestion(ctx, question, nil, false, requestID)
		return answer, err
	}

	// Set the question callback on the orchestrator
	mb.orchestrator.SetUserInputCallback(questionCallback)

	// Create tool call callback - sends tool call notification to client
	toolCallCallback := func(toolName, toolID string, parameters map[string]interface{}) error {
		// Extract description from parameters if present
		var description string
		if desc, ok := parameters["description"]; ok {
			if descStr, ok := desc.(string); ok {
				description = descStr
			}
		}

		// Send tool call message
		mb.sendMessage(NewRequest(
			MessageTypeToolCall,
			requestID,
			map[string]interface{}{
				"tool_id":     toolID,
				"tool_name":   toolName,
				"parameters":  parameters,
				"description": description,
			},
		))
		return nil
	}

	// Create tool result callback - sends tool result to client
	toolResultCallback := func(toolName, toolID, result, errorMsg string) error {
		data := map[string]interface{}{
			"tool_id": toolID,
		}
		if errorMsg != "" {
			data["error"] = errorMsg
		}
		if result != "" {
			data["result"] = result
		}

		mb.sendMessage(NewRequest(
			MessageTypeToolResult,
			requestID,
			data,
		))
		return nil
	}

	// Create progress callback - filters and forwards progress updates
	progressCallback := func(msg progress.Update) error {
		return mb.handleProgress(msg, requestID)
	}

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

	return err
}

// handleProgress handles progress updates from the orchestrator
func (mb *MessageBroker) handleProgress(msg progress.Update, requestID string) error {
	// Skip tool calling status messages for socket clients
	if strings.Contains(msg.Message, "Calling tool:") {
		return nil
	}
	// Skip "Thinking..." messages
	if msg.Message == "Thinking..." {
		return nil
	}

	// Verification agent messages should be sent even if ephemeral
	if msg.VerificationAgent && msg.Message != "" {
		logger.Debug("Progress (verification agent): %s", msg.Message)
		mb.sendMessage(NewRequest(
			MessageTypeProgress,
			requestID,
			map[string]interface{}{
				"message":    msg.Message,
				"is_compact": true,
			},
		))
		return nil
	}

	// Handle reasoning content (extended thinking)
	if msg.Reasoning != "" {
		logger.Debug("Progress (reasoning): %s", msg.Reasoning)
		mb.sendMessage(NewRequest(
			MessageTypeChatMessage,
			requestID,
			map[string]interface{}{
				"role":      "assistant",
				"content":   msg.Reasoning,
				"reasoning": true,
			},
		))
		return nil
	}

	// Only send non-ephemeral important messages
	if !msg.Ephemeral && msg.Message != "" {
		logger.Debug("Progress: %s", msg.Message)
		mb.sendMessage(NewRequest(
			MessageTypeProgress,
			requestID,
			map[string]interface{}{
				"message": msg.Message,
			},
		))
	}

	return nil
}

// Stop stops the current session operations
func (mb *MessageBroker) Stop() error {
	if mb.orchestrator != nil {
		mb.orchestrator.Stop()
	}
	return nil
}

// Close cleans up resources
func (mb *MessageBroker) Close() error {
	if mb.orchestrator != nil {
		return mb.orchestrator.Close()
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

// GetConfig returns the config
func (mb *MessageBroker) GetConfig() *config.Config {
	return mb.cfg
}

// SetRequireSandboxAuth sets whether sandbox authentication is required
func (mb *MessageBroker) SetRequireSandboxAuth(require bool) {
	mb.requireSandboxAuth = require
}

// socketInteractionHandler implements actor.UserInteractionHandler for socket mode.
// It delegates authorization requests to the broker's socket-based authorization mechanism.
type socketInteractionHandler struct {
	broker *MessageBroker
}

func (h *socketInteractionHandler) Mode() string { return "socket" }

func (h *socketInteractionHandler) SupportsInteraction(interactionType actor.InteractionType) bool {
	return interactionType == actor.InteractionTypeAuthorization
}

func (h *socketInteractionHandler) HandleInteraction(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error) {
	if req.InteractionType != actor.InteractionTypeAuthorization {
		return &actor.UserInteractionResponse{
			RequestID: req.RequestID,
			Approved:  false,
			Error:     fmt.Errorf("socket mode does not support %d interactions", req.InteractionType),
		}, nil
	}

	payload, ok := req.Payload.(*actor.AuthorizationPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload type for authorization: expected *AuthorizationPayload, got %T", req.Payload)
	}

	if h.broker.sendMessage == nil {
		return &actor.UserInteractionResponse{
			RequestID: req.RequestID,
			Approved:  false,
			Error:     fmt.Errorf("no active socket session to send authorization request"),
		}, nil
	}

	// Use empty requestID for authorization requests (they're not tied to a specific chat message)
	approved, err := h.broker.handleAuthorization(ctx, payload.ToolName, payload.Parameters, payload.Reason, "")
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

// handleAuthorization handles an authorization request by sending it to the client
// and waiting for the response. Uses a two-phase timeout:
// Phase 1 (30s): Wait for the client to acknowledge it displayed the dialog.
// Phase 2 (10min): Wait for the user's actual approve/deny response.
func (mb *MessageBroker) handleAuthorization(ctx context.Context, toolName string, params map[string]interface{}, reason string, requestID string) (bool, error) {
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

	// Send authorization request to client
	mb.sendMessage(NewRequest(
		MessageTypeAuthorizationRequest,
		requestID,
		map[string]interface{}{
			"auth_id":    authID,
			"tool_name":  toolName,
			"parameters": params,
			"reason":     reason,
		},
	))

	// Phase 1: Wait for ack (client confirmed dialog is displayed) or early response
	select {
	case approved := <-responseChan:
		// User responded before ack (fast user) — return immediately
		logger.Debug("Authorization response received (before ack) for %s: %v", authID, approved)
		cleanup()
		return approved, nil
	case <-ackChan:
		// Client confirmed dialog is displayed — proceed to phase 2
		logger.Debug("Authorization ack received for %s, waiting for user response", authID)
	case <-ctx.Done():
		logger.Debug("Authorization cancelled during ack wait for %s", authID)
		cleanup()
		return false, ctx.Err()
	case <-time.After(consts.Timeout30Seconds):
		logger.Error("Authorization ack timeout for %s — dialog was not displayed", authID)
		cleanup()
		return false, fmt.Errorf("authorization timed out: dialog was not displayed by the client within 30 seconds")
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

// HandleAuthorizationResponse handles a response from the client for an authorization request
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

// HandleAuthorizationAck handles an ack from the client confirming the authorization dialog was displayed
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
// It sends the question to the client and waits for a response
func (mb *MessageBroker) handleQuestion(ctx context.Context, question string, questions map[string]string, multiMode bool, requestID string) (string, map[string]string, error) {
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

	// Send question request to client
	data := map[string]interface{}{
		"question_id": questionID,
		"question":    question,
		"multi_mode":  multiMode,
	}
	if len(questions) > 0 {
		data["questions"] = questions
	}

	mb.sendMessage(NewRequest(
		MessageTypeQuestionRequest,
		requestID,
		data,
	))

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

// HandleQuestionResponse handles a response from the client for a question request
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

// IsInitialized returns whether the broker has been initialized
func (mb *MessageBroker) IsInitialized() bool {
	return mb.initialized
}

// ResetSession tears down the current orchestrator and session so the next
// chat message will trigger a fresh InitializeSession.
func (mb *MessageBroker) ResetSession() {
	if mb.orchestrator != nil {
		_ = mb.orchestrator.Close()
		mb.orchestrator = nil
	}
	mb.session = nil
	mb.initialized = false
}
