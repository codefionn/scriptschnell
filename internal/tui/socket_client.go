package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/socketclient"
)

// SocketClientWrapper wraps the socketclient.Client for TUI integration
// It provides a unified interface that mimics the local runtime behavior
type SocketClientWrapper struct {
	client       *socketclient.Client
	config       *config.Config
	socketPath   string      // Stored socket path for detection
	providerMgr  interface{} // provider.Manager interface
	connected    bool
	reconnecting bool

	// Session state
	currentSessionID string
	currentWorkspace string

	// Callback registration
	mu sync.RWMutex

	// Message handlers for streaming responses
	onChatMessage      func(msg socketclient.ChatMessage)
	onToolCall         func(msg socketclient.ToolCall)
	onToolResult       func(msg socketclient.ToolResult)
	onProgress         func(msg socketclient.ProgressData)
	onAuthorization    func(req socketclient.AuthorizationRequest)
	onQuestion         func(req socketclient.QuestionRequest)
	onSessionChanged   func(sessionID string)
	onWorkspaceChanged func(workspace string)

	// Completion tracking
	pendingPrompts map[string]chan struct{} // requestID -> completion channel
}

// NewSocketClientWrapper creates a new socket client wrapper
func NewSocketClientWrapper(cfg *config.Config) *SocketClientWrapper {
	socketPath := cfg.Socket.GetSocketPath()
	client, err := socketclient.NewClient(socketPath)
	if err != nil {
		panic(fmt.Sprintf("failed to create socket client: %v", err))
	}

	wrapper := &SocketClientWrapper{
		client:         client,
		config:         cfg,
		socketPath:     socketPath,
		connected:      false,
		pendingPrompts: make(map[string]chan struct{}),
	}

	// Set up client callbacks
	wrapper.setupClientCallbacks()

	return wrapper
}

// setupClientCallbacks configures all socket client callbacks
func (w *SocketClientWrapper) setupClientCallbacks() {
	// State change callback - handles connection state changes
	w.client.SetStateChangedCallback(func(state socketclient.ConnectionState, err error) {
		switch state {
		case socketclient.StateConnected:
			logger.Info("Socket client connected to server")
			w.mu.Lock()
			w.connected = true
			w.reconnecting = false
			w.mu.Unlock()
		case socketclient.StateDisconnected:
			logger.Warn("Socket client disconnected: %v", err)
			w.mu.Lock()
			w.connected = false
			w.mu.Unlock()
		case socketclient.StateReconnecting:
			logger.Info("Socket client is reconnecting...")
			w.mu.Lock()
			w.reconnecting = true
			w.mu.Unlock()
		}
	})

	// Reconnecting callback - provides detailed reconnection info
	w.client.SetReconnectingCallback(func(attempt int, maxAttempts int) {
		logger.Info("Socket client reconnecting... (attempt %d/%d)", attempt, maxAttempts)
	})

	// Connection lost callback
	w.client.SetConnectionLostCallback(func(err error) {
		logger.Warn("Socket client connection lost: %v", err)
	})

	// Chat message callback
	w.client.SetChatMessageCallback(func(msg socketclient.ChatMessage) {
		w.mu.RLock()
		handler := w.onChatMessage
		w.mu.RUnlock()
		if handler != nil {
			handler(msg)
		}
	})

	// Tool call callback
	w.client.SetToolCallCallback(func(msg socketclient.ToolCall) {
		w.mu.RLock()
		handler := w.onToolCall
		w.mu.RUnlock()
		if handler != nil {
			handler(msg)
		}
	})

	// Tool result callback
	w.client.SetToolResultCallback(func(msg socketclient.ToolResult) {
		w.mu.RLock()
		handler := w.onToolResult
		w.mu.RUnlock()
		if handler != nil {
			handler(msg)
		}
	})

	// Progress callback
	w.client.SetProgressCallback(func(msg socketclient.ProgressData) {
		w.mu.RLock()
		handler := w.onProgress
		w.mu.RUnlock()
		if handler != nil {
			handler(msg)
		}
	})

	// Authorization callback
	w.client.SetAuthorizationCallback(func(req socketclient.AuthorizationRequest) (bool, error) {
		w.mu.RLock()
		handler := w.onAuthorization
		w.mu.RUnlock()
		if handler != nil {
			handler(req)
		}
		// Return default values for now - actual authorization handled via callback
		return true, nil
	})

	// Question callback
	w.client.SetQuestionCallback(func(req socketclient.QuestionRequest) (map[string]string, error) {
		w.mu.RLock()
		handler := w.onQuestion
		w.mu.RUnlock()
		if handler != nil {
			handler(req)
		}
		// Return default values for now - actual question handling done via callback
		return nil, nil
	})

	// Completion callback
	w.client.SetCompletionCallback(func(requestID string, success bool, errorMsg string) {
		w.SignalCompletion(requestID)
	})
}

// Connect attempts to connect to the socket server
func (w *SocketClientWrapper) Connect(ctx context.Context) error {
	logger.Info("Attempting to connect to socket server at: %s", w.socketPath)
	return w.client.Connect(ctx)
}

// IsConnected returns whether the client is connected
func (w *SocketClientWrapper) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.connected
}

// IsReconnecting returns whether the client is currently reconnecting
func (w *SocketClientWrapper) IsReconnecting() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.reconnecting
}

// Disconnect closes the connection
func (w *SocketClientWrapper) Disconnect() {
	w.client.Disconnect()
}

// Close terminates the client
func (w *SocketClientWrapper) Close() {
	w.client.Close()
}

// SetChatMessageHandler sets the callback for chat messages
func (w *SocketClientWrapper) SetChatMessageHandler(handler func(msg socketclient.ChatMessage)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onChatMessage = handler
}

// SetToolCallHandler sets the callback for tool calls
func (w *SocketClientWrapper) SetToolCallHandler(handler func(msg socketclient.ToolCall)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onToolCall = handler
}

// SetToolResultHandler sets the callback for tool results
func (w *SocketClientWrapper) SetToolResultHandler(handler func(msg socketclient.ToolResult)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onToolResult = handler
}

// SetProgressHandler sets the callback for progress updates
func (w *SocketClientWrapper) SetProgressHandler(handler func(msg socketclient.ProgressData)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onProgress = handler
}

// SetAuthorizationHandler sets the callback for authorization requests
func (w *SocketClientWrapper) SetAuthorizationHandler(handler func(req socketclient.AuthorizationRequest)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onAuthorization = handler
}

// SetQuestionHandler sets the callback for question dialogs
func (w *SocketClientWrapper) SetQuestionHandler(handler func(req socketclient.QuestionRequest)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onQuestion = handler
}

// CreateSession creates a new session
func (w *SocketClientWrapper) CreateSession(ctx context.Context, workspace string, workingDir string) (string, error) {
	sessionID, err := w.client.CreateSession(ctx, workspace, "", workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	w.mu.Lock()
	w.currentSessionID = sessionID
	w.currentWorkspace = workspace
	w.mu.Unlock()

	logger.Info("Created socket session: %s", sessionID)
	return sessionID, nil
}

// AttachSession attaches to an existing session
func (w *SocketClientWrapper) AttachSession(ctx context.Context, sessionID string) error {
	err := w.client.AttachSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to attach to session: %w", err)
	}

	w.mu.Lock()
	w.currentSessionID = sessionID
	w.mu.Unlock()

	logger.Info("Attached to socket session: %s", sessionID)
	return nil
}

// DetachSession detaches from the current session
func (w *SocketClientWrapper) DetachSession(ctx context.Context) error {
	err := w.client.DetachSession(ctx)
	if err != nil {
		return fmt.Errorf("failed to detach from session: %w", err)
	}

	w.mu.Lock()
	w.currentSessionID = ""
	w.mu.Unlock()

	logger.Info("Detached from socket session")
	return nil
}

// GetCurrentSessionID returns the current session ID
func (w *SocketClientWrapper) GetCurrentSessionID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.currentSessionID
}

// GetCurrentWorkspace returns the current workspace
func (w *SocketClientWrapper) GetCurrentWorkspace() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.currentWorkspace
}

// SendChat sends a chat message and waits for completion
func (w *SocketClientWrapper) SendChat(ctx context.Context, prompt string) error {
	w.mu.RLock()
	sessionID := w.currentSessionID
	w.mu.RUnlock()

	if sessionID == "" {
		return fmt.Errorf("no active session")
	}

	// Register completion channel
	reqID := w.generateRequestID()
	completionCh := make(chan struct{}, 1)
	w.mu.Lock()
	w.pendingPrompts[reqID] = completionCh
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		delete(w.pendingPrompts, reqID)
		w.mu.Unlock()
	}()

	// Stream chat messages
	err := w.client.StreamChat(ctx, prompt, nil, func(msg socketclient.ChatMessage) {
		// Messages are delivered via OnChatMessage callback
	})
	if err != nil {
		return fmt.Errorf("failed to send chat: %w", err)
	}

	// Wait for completion signal
	select {
	case <-completionCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("chat completion timeout")
	}
}

// StopChat stops the current chat generation
func (w *SocketClientWrapper) StopChat(ctx context.Context) error {
	return w.client.StopChat(ctx)
}

// ClearChat clears the chat history
func (w *SocketClientWrapper) ClearChat(ctx context.Context) error {
	return w.client.ClearChat(ctx)
}

// ListSessions lists available sessions
func (w *SocketClientWrapper) ListSessions(ctx context.Context, workspace string) ([]socketclient.SessionInfo, error) {
	return w.client.ListSessions(ctx, workspace)
}

// SaveSession saves the current session
func (w *SocketClientWrapper) SaveSession(ctx context.Context, name string) error {
	return w.client.SaveSession(ctx, name)
}

// LoadSession loads a session
func (w *SocketClientWrapper) LoadSession(ctx context.Context, sessionID string) error {
	w.mu.RLock()
	workspace := w.currentWorkspace
	w.mu.RUnlock()

	err := w.client.LoadSession(ctx, sessionID, workspace)
	if err != nil {
		return fmt.Errorf("failed to load session: %w", err)
	}

	return nil
}

// DeleteSession deletes a session
func (w *SocketClientWrapper) DeleteSession(ctx context.Context, sessionID string) error {
	w.mu.RLock()
	workspace := w.currentWorkspace
	w.mu.RUnlock()

	return w.client.DeleteSession(ctx, sessionID, workspace)
}

// ListWorkspaces lists available workspaces
func (w *SocketClientWrapper) ListWorkspaces(ctx context.Context) ([]socketclient.WorkspaceInfo, error) {
	return w.client.ListWorkspaces(ctx)
}

// SetWorkspace sets the active workspace
func (w *SocketClientWrapper) SetWorkspace(ctx context.Context, workspace string) error {
	err := w.client.SetWorkspace(ctx, workspace)
	if err != nil {
		return fmt.Errorf("failed to set workspace: %w", err)
	}

	w.mu.Lock()
	w.currentWorkspace = workspace
	w.mu.Unlock()

	return nil
}

// SendAuthorizationResponse sends an authorization response
func (w *SocketClientWrapper) SendAuthorizationResponse(requestID string, approved bool) error {
	return w.client.SendAuthorizationResponse(requestID, approved)
}

// SendAuthorizationAck sends an authorization acknowledgment
func (w *SocketClientWrapper) SendAuthorizationAck(ctx context.Context, requestID string) error {
	return w.client.SendAuthorizationAck(requestID)
}

// SendQuestionResponse sends a question response
func (w *SocketClientWrapper) SendQuestionResponse(ctx context.Context, requestID string, answers map[string]string) error {
	return w.client.SendQuestionResponse(requestID, "", answers)
}

// SignalCompletion signals that a prompt has completed
func (w *SocketClientWrapper) SignalCompletion(requestID string) {
	w.mu.Lock()
	if ch, ok := w.pendingPrompts[requestID]; ok {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	w.mu.Unlock()
}

// generateRequestID generates a unique request ID
func (w *SocketClientWrapper) generateRequestID() string {
	return fmt.Sprintf("tui-%d", time.Now().UnixNano())
}

// GetSocketPath returns the socket path for detection
func (w *SocketClientWrapper) GetSocketPath() string {
	return w.socketPath
}
