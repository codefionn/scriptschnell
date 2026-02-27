package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/socketclient"
)

// SocketRuntimeFactory is a socket-based alternative to RuntimeFactory
// It connects to a socket server and manages sessions remotely
type SocketRuntimeFactory struct {
	wrapper    *SocketClientWrapper
	config     *config.Config
	workingDir string
	mu         sync.RWMutex
	connected  bool
}

// NewSocketRuntimeFactory creates a new socket-based runtime factory
// If socketPath is non-empty, it overrides the path from config
func NewSocketRuntimeFactory(cfg *config.Config, socketPath string) (*SocketRuntimeFactory, error) {
	logger.Info("Creating SocketRuntimeFactory")

	// Apply socket path override if provided
	if socketPath != "" {
		cfg.Socket.Path = socketPath
		logger.Info("Socket path overridden to: %s", socketPath)
	}

	wrapper := NewSocketClientWrapper(cfg)

	factory := &SocketRuntimeFactory{
		wrapper:    wrapper,
		config:     cfg,
		workingDir: cfg.WorkingDir,
	}

	return factory, nil
}

// Connect attempts to connect to the socket server
func (sf *SocketRuntimeFactory) Connect(ctx context.Context) error {
	socketPath := sf.config.Socket.GetSocketPath()
	logger.Info("SocketRuntimeFactory: Connecting to socket server at: %s", socketPath)

	err := sf.wrapper.Connect(ctx)
	if err != nil {
		logger.Error("SocketRuntimeFactory: Failed to connect to socket server at %s: %v", socketPath, err)
		return fmt.Errorf("failed to connect to socket server at %s: %w", socketPath, err)
	}

	sf.mu.Lock()
	sf.connected = true
	sf.mu.Unlock()

	logger.Info("SocketRuntimeFactory: Connected to server")
	return nil
}

// IsConnected returns whether the factory is connected to the server
func (sf *SocketRuntimeFactory) IsConnected() bool {
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	return sf.connected
}

// GetWorkingDir returns the working directory
func (sf *SocketRuntimeFactory) GetWorkingDir() string {
	return sf.workingDir
}

// GetSharedFilesystem returns nil for socket mode (filesystem is on server)
func (sf *SocketRuntimeFactory) GetSharedFilesystem() interface{} {
	return nil
}

// GetSharedResources returns the shared resources (minimal for socket mode)
func (sf *SocketRuntimeFactory) GetSharedResources() interface{} {
	return nil
}

// CreateSocketSession creates a new session on the socket server
func (sf *SocketRuntimeFactory) CreateSocketSession(ctx context.Context, workspace string, workingDir string) (*TabSession, error) {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected to socket server")
	}

	sessionID, err := sf.wrapper.CreateSession(ctx, workspace, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket session: %w", err)
	}

	// Create local session object
	sess := session.NewSession(sessionID, workingDir)

	// Create TabSession wrapper
	tabSession := &TabSession{
		ID:                 generateTabID(),
		Session:            sess,
		Name:               "", // No name for auto-created sessions
		WorktreePath:       "",
		Messages:           []message{},
		CreatedAt:          time.Now(),
		LastActiveAt:       time.Now(),
		ContextFreePercent: 100,
		Runtime:            nil, // No local runtime in socket mode
		Generating:         false,
		WaitingForAuth:     false,
	}

	logger.Info("Created socket session: %s (tabID: %d)", sessionID, tabSession.ID)
	return tabSession, nil
}

// AttachSocketSession attaches to an existing session on the socket server
func (sf *SocketRuntimeFactory) AttachSocketSession(ctx context.Context, sessionID string, workingDir string) (*TabSession, error) {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected to socket server")
	}

	err := sf.wrapper.AttachSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to attach to socket session: %w", err)
	}

	// Create local session object
	sess := session.NewSession(sessionID, workingDir)

	tabSession := &TabSession{
		ID:                 generateTabID(),
		Session:            sess,
		Name:               "",
		WorktreePath:       "",
		Messages:           []message{},
		CreatedAt:          time.Now(),
		LastActiveAt:       time.Now(),
		ContextFreePercent: 100,
		Runtime:            nil,
		Generating:         false,
		WaitingForAuth:     false,
	}

	logger.Info("Attached to socket session: %s (tabID: %d)", sessionID, tabSession.ID)
	return tabSession, nil
}

// DetachSession detaches from a session
func (sf *SocketRuntimeFactory) DetachSession(ctx context.Context, tabSession *TabSession) error {
	if tabSession == nil {
		return nil
	}

	return sf.wrapper.DetachSession(ctx)
}

// SendChat sends a chat message via the socket
func (sf *SocketRuntimeFactory) SendChat(ctx context.Context, tabSession *TabSession, prompt string) error {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected to socket server")
	}

	if tabSession == nil || tabSession.Session == nil {
		return fmt.Errorf("invalid tab session")
	}

	return sf.wrapper.SendChat(ctx, prompt)
}

// StopChat stops the current chat
func (sf *SocketRuntimeFactory) StopChat(ctx context.Context) error {
	return sf.wrapper.StopChat(ctx)
}

// ClearChat clears the chat history
func (sf *SocketRuntimeFactory) ClearChat(ctx context.Context) error {
	return sf.wrapper.ClearChat(ctx)
}

// ListSessions lists available sessions
func (sf *SocketRuntimeFactory) ListSessions(ctx context.Context, workspace string) ([]socketclient.SessionInfo, error) {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected to socket server")
	}

	return sf.wrapper.ListSessions(ctx, workspace)
}

// SaveSession saves the current session
func (sf *SocketRuntimeFactory) SaveSession(ctx context.Context, tabSession *TabSession, name string) error {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected to socket server")
	}

	return sf.wrapper.SaveSession(ctx, name)
}

// LoadSession loads a session
func (sf *SocketRuntimeFactory) LoadSession(ctx context.Context, sessionID string, workingDir string) (*TabSession, error) {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected to socket server")
	}

	err := sf.wrapper.LoadSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	// Create local session object
	sess := session.NewSession(sessionID, workingDir)

	tabSession := &TabSession{
		ID:                 generateTabID(),
		Session:            sess,
		Name:               "",
		WorktreePath:       "",
		Messages:           []message{},
		CreatedAt:          time.Now(),
		LastActiveAt:       time.Now(),
		ContextFreePercent: 100,
		Runtime:            nil,
		Generating:         false,
		WaitingForAuth:     false,
	}

	return tabSession, nil
}

// DeleteSession deletes a session
func (sf *SocketRuntimeFactory) DeleteSession(ctx context.Context, sessionID string) error {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected to socket server")
	}

	return sf.wrapper.DeleteSession(ctx, sessionID)
}

// ListWorkspaces lists available workspaces
func (sf *SocketRuntimeFactory) ListWorkspaces(ctx context.Context) ([]socketclient.WorkspaceInfo, error) {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected to socket server")
	}

	return sf.wrapper.ListWorkspaces(ctx)
}

// SetWorkspace sets the active workspace
func (sf *SocketRuntimeFactory) SetWorkspace(ctx context.Context, workspace string) error {
	sf.mu.RLock()
	connected := sf.connected
	sf.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected to socket server")
	}

	return sf.wrapper.SetWorkspace(ctx, workspace)
}

// SendAuthorizationResponse sends an authorization response
func (sf *SocketRuntimeFactory) SendAuthorizationResponse(ctx context.Context, requestID string, approved bool) error {
	return sf.wrapper.SendAuthorizationResponse(requestID, approved)
}

// SendAuthorizationAck sends an authorization acknowledgment
func (sf *SocketRuntimeFactory) SendAuthorizationAck(ctx context.Context, requestID string) error {
	return sf.wrapper.SendAuthorizationAck(ctx, requestID)
}

// SendQuestionResponse sends a question response
func (sf *SocketRuntimeFactory) SendQuestionResponse(ctx context.Context, requestID string, answers map[string]string) error {
	return sf.wrapper.SendQuestionResponse(ctx, requestID, answers)
}

// SetChatMessageHandler sets the callback for chat messages
func (sf *SocketRuntimeFactory) SetChatMessageHandler(handler func(msg socketclient.ChatMessage)) {
	sf.wrapper.SetChatMessageHandler(handler)
}

// SetToolCallHandler sets the callback for tool calls
func (sf *SocketRuntimeFactory) SetToolCallHandler(handler func(msg socketclient.ToolCall)) {
	sf.wrapper.SetToolCallHandler(handler)
}

// SetToolResultHandler sets the callback for tool results
func (sf *SocketRuntimeFactory) SetToolResultHandler(handler func(msg socketclient.ToolResult)) {
	sf.wrapper.SetToolResultHandler(handler)
}

// SetProgressHandler sets the callback for progress updates
func (sf *SocketRuntimeFactory) SetProgressHandler(handler func(msg socketclient.ProgressData)) {
	sf.wrapper.SetProgressHandler(handler)
}

// SetAuthorizationHandler sets the callback for authorization requests
func (sf *SocketRuntimeFactory) SetAuthorizationHandler(handler func(req socketclient.AuthorizationRequest)) {
	sf.wrapper.SetAuthorizationHandler(handler)
}

// SetQuestionHandler sets the callback for question dialogs
func (sf *SocketRuntimeFactory) SetQuestionHandler(handler func(req socketclient.QuestionRequest)) {
	sf.wrapper.SetQuestionHandler(handler)
}

// Close closes the socket connection
func (sf *SocketRuntimeFactory) Close() {
	logger.Info("SocketRuntimeFactory: Closing connection")
	sf.wrapper.Close()
	sf.mu.Lock()
	sf.connected = false
	sf.mu.Unlock()
}

// GetWrapper returns the underlying socket client wrapper
func (sf *SocketRuntimeFactory) GetWrapper() *SocketClientWrapper {
	return sf.wrapper
}

// generateTabID generates a unique tab ID
func generateTabID() int {
	return int(time.Now().UnixNano() % 10000)
}

// IsReconnecting returns whether the client is currently reconnecting
func (sf *SocketRuntimeFactory) IsReconnecting() bool {
	return sf.wrapper.IsReconnecting()
}
