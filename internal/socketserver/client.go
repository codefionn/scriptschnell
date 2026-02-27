package socketserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

// Client represents a connected socket client
type Client struct {
	// Connection identifier
	ID string

	// Socket connection
	conn net.Conn

	// Hub reference
	hub *Hub

	// Session manager reference
	sessionManager *SessionManager

	// Workspace manager reference
	workspaceManager *WorkspaceManager

	// Outbound message channel
	send chan *BaseMessage

	// Current session state
	SessionID string
	Workspace string
	messages  []BaseMessage // Message history for the session

	// Authentication state
	authenticated bool
	authMethod    string
	clientType    string

	// Control
	mu       sync.Mutex
	closed   bool
	stopOnce sync.Once
	stopChan chan struct{}

	// Broker for LLM interactions
	broker *MessageBroker

	// Dependencies - to be populated when orchestrator integration is added
	// orchestrator *orchestrator.Orchestrator
}

// NewClient creates a new client instance
func NewClient(id string, conn net.Conn, hub *Hub, sessionMgr *SessionManager, workspaceMgr *WorkspaceManager, broker *MessageBroker) *Client {
	return &Client{
		ID:               id,
		conn:             conn,
		hub:              hub,
		sessionManager:   sessionMgr,
		workspaceManager: workspaceMgr,
		broker:           broker,
		send:             make(chan *BaseMessage, 256),
		messages:         make([]BaseMessage, 0),
		stopChan:         make(chan struct{}),
		authenticated:    false,
	}
}

// Start begins reading from and writing to the client connection
func (c *Client) Start() {
	// Register with hub
	c.hub.RegisterClient(c)

	// Start read and write pumps
	go c.readPump()
	go c.writePump()

	logger.Info("Client %s started read/write pumps", c.ID)
}

// Stop gracefully stops the client
func (c *Client) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopChan)

		c.mu.Lock()
		if !c.closed {
			c.closed = true
		}
		c.mu.Unlock()

		// Unregister from hub
		c.hub.UnregisterClient(c)

		// Detach from session
		sessionID := c.GetSession()
		if c.sessionManager != nil {
			c.sessionManager.DetachClient(c.ID)
		}

		// Update workspace session count
		if c.workspaceManager != nil && sessionID != "" {
			workingDir := c.GetWorkspace()
			if ws, ok := c.workspaceManager.GetWorkspaceByPath(workingDir); ok {
				c.workspaceManager.UpdateWorkspaceSessionCount(ws.ID, -1)
			}
		}

		// Close connection
		if c.conn != nil {
			c.conn.Close()
		}

		// Close send channel
		close(c.send)

		logger.Info("Client %s stopped", c.ID)
	})
}

// Close is an alias for Stop
func (c *Client) Close() {
	c.Stop()
}

// readPump reads messages from the socket connection
func (c *Client) readPump() {
	defer c.Stop()

	reader := bufio.NewReader(c.conn)

	for {
		select {
		case <-c.stopChan:
			logger.Info("Client %s read pump stopping", c.ID)
			return
		default:
			// Set read deadline
			if err := c.conn.SetReadDeadline(time.Now().Add(consts.Timeout60Seconds)); err != nil {
				logger.Error("Failed to set read deadline for client %s: %v", c.ID, err)
				return
			}

			// Read until newline
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					logger.Info("Client %s disconnected (EOF)", c.ID)
				} else if errors.Is(err, net.ErrClosed) {
					logger.Info("Client %s connection closed", c.ID)
				} else {
					logger.Error("Error reading from client %s: %v", c.ID, err)
				}
				return
			}

			// Trim whitespace and skip empty lines
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Parse message
			var msg BaseMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				logger.Error("Failed to parse message from client %s: %v", c.ID, err)
				c.SendError("", ErrorCodeInvalidRequest, "Invalid JSON format", err.Error())
				continue
			}

			// Handle message
			if err := c.handleMessage(&msg); err != nil {
				logger.Error("Error handling message from client %s: %v", c.ID, err)
				c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to handle message", err.Error())
			}
		}
	}
}

// writePump writes messages to the socket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Stop()
	}()

	for {
		select {
		case <-c.stopChan:
			logger.Info("Client %s write pump stopping", c.ID)
			return

		case <-ticker.C:
			// Send periodic ping
			c.SendPing()

		case message, ok := <-c.send:
			if !ok {
				// Channel closed
				return
			}

			// Set write deadline
			if err := c.conn.SetWriteDeadline(time.Now().Add(consts.Timeout10Seconds)); err != nil {
				logger.Error("Failed to set write deadline for client %s: %v", c.ID, err)
				return
			}

			// Marshal message to JSON
			data, err := json.Marshal(message)
			if err != nil {
				logger.Error("Failed to marshal message for client %s: %v", c.ID, err)
				continue
			}

			// Write with newline delimiter
			if _, err := fmt.Fprintf(c.conn, "%s\n", data); err != nil {
				logger.Error("Failed to write message to client %s: %v", c.ID, err)
				return
			}
		}
	}
}

// handleMessage dispatches incoming messages to appropriate handlers
func (c *Client) handleMessage(msg *BaseMessage) error {
	logger.Debug("Client %s received message: %s", c.ID, msg.Type)

	// Check authentication for messages that require it
	if msg.Type != MessageTypeAuthRequest && !c.authenticated {
		return fmt.Errorf("client not authenticated")
	}

	switch msg.Type {
	case MessageTypeAuthRequest:
		return c.handleAuthRequest(msg)

	case MessageTypePing:
		return c.handlePing(msg)

	case MessageTypePong:
		// Ignore pong messages
		return nil

	case MessageTypeClose:
		return c.handleClose(msg)

	case MessageTypeSessionCreate:
		return c.handleSessionCreate(msg)

	case MessageTypeSessionAttach:
		return c.handleSessionAttach(msg)

	case MessageTypeSessionDetach:
		return c.handleSessionDetach(msg)

	case MessageTypeSessionList:
		return c.handleSessionList(msg)

	case MessageTypeSessionDelete:
		return c.handleSessionDelete(msg)

	case MessageTypeChatSend:
		return c.handleChatSend(msg)

	case MessageTypeChatStop:
		return c.handleChatStop(msg)

	case MessageTypeChatClear:
		return c.handleChatClear(msg)

	case MessageTypeConfigGet:
		return c.handleConfigGet(msg)

	case MessageTypeConfigSet:
		return c.handleConfigSet(msg)

	case MessageTypeWorkspaceList:
		return c.handleWorkspaceList(msg)

	case MessageTypeWorkspaceSet:
		return c.handleWorkspaceSet(msg)

	case MessageTypeSessionSave:
		return c.handleSessionSave(msg)

	case MessageTypeSessionLoad:
		return c.handleSessionLoad(msg)

	case MessageTypeAuthorizationResponse:
		return c.handleAuthorizationResponse(msg)

	case MessageTypeQuestionResponse:
		return c.handleQuestionResponse(msg)

	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// Send sends a message to the client
func (c *Client) Send(msg *BaseMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		logger.Warn("Attempted to send message to closed client %s", c.ID)
		return
	}

	select {
	case c.send <- msg:
	default:
		logger.Warn("Send buffer full for client %s, message dropped", c.ID)
	}
}

// SendError sends an error message to the client
func (c *Client) SendError(requestID string, code string, message string, details string) {
	c.Send(NewError(requestID, code, message, details))
}

// SendResponse sends a response message
func (c *Client) SendResponse(msgType string, requestID string, data map[string]interface{}) {
	c.Send(NewResponse(msgType, requestID, data))
}

// SendPing sends a ping message
func (c *Client) SendPing() {
	msg := NewMessage(MessageTypePing, nil)
	msg.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	c.Send(msg)
}

// SendPong sends a pong message
func (c *Client) SendPong() {
	msg := NewMessage(MessageTypePong, nil)
	msg.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	c.Send(msg)
}

// SendChatMessage sends a chat message to the client
func (c *Client) SendChatMessage(role string, content string, streamID string, chunkIndex int, isFinal bool) {
	msg := NewResponse(MessageTypeChatMessage, "", map[string]interface{}{
		"role":        role,
		"content":     content,
		"stream_id":   streamID,
		"chunk_index": chunkIndex,
		"is_final":    isFinal,
	})
	c.Send(msg)
}

// SendToolCall sends a tool call notification
func (c *Client) SendToolCall(toolID string, toolName string, parameters map[string]interface{}, description string) {
	msg := NewMessage(MessageTypeToolCall, map[string]interface{}{
		"tool_id":     toolID,
		"tool_name":   toolName,
		"parameters":  parameters,
		"description": description,
	})
	c.Send(msg)
}

// SendToolResult sends a tool execution result
func (c *Client) SendToolResult(toolID string, result string, errMsg string, status string) {
	msg := NewMessage(MessageTypeToolResult, map[string]interface{}{
		"tool_id": toolID,
		"result":  result,
		"error":   errMsg,
		"status":  status,
	})
	c.Send(msg)
}

// SendToolCompact sends a compact tool interaction message
func (c *Client) SendToolCompact(toolID string, toolName string, status string, result string, errMsg string, description string) {
	msg := NewMessage(MessageTypeToolCompact, map[string]interface{}{
		"tool_id":     toolID,
		"tool_name":   toolName,
		"status":      status,
		"result":      result,
		"error":       errMsg,
		"description": description,
	})
	c.Send(msg)
}

// SendProgress sends a progress update
func (c *Client) SendProgress(message string, contextUsage int, ephemeral bool, verificationAgent bool) {
	msg := NewMessage(MessageTypeProgress, map[string]interface{}{
		"message":            message,
		"context_usage":      contextUsage,
		"ephemeral":          ephemeral,
		"verification_agent": verificationAgent,
	})
	c.Send(msg)
}

// SendAuthorizationRequest sends an authorization request to the client
func (c *Client) SendAuthorizationRequest(authID string, toolName string, parameters map[string]interface{}, reason string) {
	msg := NewMessage(MessageTypeAuthorizationRequest, map[string]interface{}{
		"auth_id":    authID,
		"tool_name":  toolName,
		"parameters": parameters,
		"reason":     reason,
	})
	c.Send(msg)
}

// SendQuestionRequest sends a question request to the client
func (c *Client) SendQuestionRequest(questionID string, question string, multiMode bool, questions []QuestionDef) {
	data := map[string]interface{}{
		"question_id": questionID,
		"question":    question,
		"multi_mode":  multiMode,
	}
	if multiMode && len(questions) > 0 {
		data["questions"] = questions
	}
	msg := NewMessage(MessageTypeQuestionRequest, data)
	c.Send(msg)
}

// Authenticated returns whether the client is authenticated
func (c *Client) Authenticated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authenticated
}

// setAuthenticated sets the authentication state
func (c *Client) setAuthenticated(authenticated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authenticated = authenticated
}

// GetSession returns the current session ID
func (c *Client) GetSession() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.SessionID
}

// SetSession sets the current session ID and workspace
func (c *Client) SetSession(sessionID string, workspace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SessionID = sessionID
	c.Workspace = workspace
}

// GetWorkspace returns the current workspace path
func (c *Client) GetWorkspace() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Workspace
}

// SetWorkspace sets the current workspace path
func (c *Client) SetWorkspace(workspace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Workspace = workspace
}

// handleAuthRequest authenticates a client connection
func (c *Client) handleAuthRequest(msg *BaseMessage) error {
	// Parse request data
	var data AuthRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid auth request", err.Error())
		return nil
	}

	// Store client metadata
	c.clientType = data.ClientType
	if c.clientType == "" {
		c.clientType = "unknown"
	}

	// Determine authentication method (if required)
	// For now, we accept all clients - authentication is handled by file permissions
	// In the future, token-based or peer credential validation can be added here

	// Mark client as authenticated
	c.setAuthenticated(true)

	// Generate connection info
	connectionID := c.ID

	// Send successful auth response
	c.SendResponse(MessageTypeAuthResponse, msg.RequestID, map[string]interface{}{
		"success":             true,
		"connection_id":       connectionID,
		"server_version":      "1.0.0",
		"server_capabilities": []string{"sessions", "workspaces", "chat", "progress", "authorization", "questions"},
	})

	logger.Info("Frontend connected: client=%s type=%s addr=%s", c.ID, c.clientType, c.conn.RemoteAddr())
	return nil
}

func (c *Client) handlePing(msg *BaseMessage) error {
	c.SendPong()
	return nil
}

func (c *Client) handleClose(msg *BaseMessage) error {
	logger.Info("Client %s requested close", c.ID)
	c.Stop()
	return nil
}

func (c *Client) handleSessionCreate(msg *BaseMessage) error {
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	// Parse request data
	var data SessionCreateRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid session create request", err.Error())
		return nil
	}

	// Use client's workspace or override from request
	workingDir := data.WorkingDir
	if workingDir == "" {
		workingDir = c.Workspace
	}
	if workingDir == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Working directory not specified", "")
		return nil
	}

	// Resolve workspace and update tracking
	if c.workspaceManager != nil {
		ctx := context.Background()
		ws, err := c.workspaceManager.ResolveWorkspace(ctx, workingDir)
		if err != nil {
			c.SendError(msg.RequestID, ErrorCodeWorkspaceInvalid, "Failed to resolve workspace", err.Error())
			return nil
		}
		c.workspaceManager.UpdateWorkspaceSessionCount(ws.ID, 1)
	}

	// Create new session
	sessionID, sess, err := c.sessionManager.CreateSession(workingDir)
	if err != nil {
		// Rollback session count
		if c.workspaceManager != nil {
			_, cancel := context.WithTimeout(context.Background(), consts.Timeout30Seconds)
			defer cancel()
			if ws, ok := c.workspaceManager.GetWorkspaceByPath(workingDir); ok {
				c.workspaceManager.UpdateWorkspaceSessionCount(ws.ID, -1)
			}
		}
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to create session", err.Error())
		return nil
	}

	// Attach client to the new session
	if err := c.sessionManager.AttachClient(c.ID, sessionID); err != nil {
		// Rollback session count
		if c.workspaceManager != nil {
			_, cancel := context.WithTimeout(context.Background(), consts.Timeout30Seconds)
			defer cancel()
			if ws, ok := c.workspaceManager.GetWorkspaceByPath(workingDir); ok {
				c.workspaceManager.UpdateWorkspaceSessionCount(ws.ID, -1)
			}
		}
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to attach to session", err.Error())
		return nil
	}

	// Update client session state
	c.SetSession(sessionID, workingDir)
	c.SetWorkspace(workingDir)

	// Send response
	responseData := map[string]interface{}{
		"session_id":  sessionID,
		"working_dir": workingDir,
		"created_at":  sess.CreatedAt.Format(time.RFC3339),
	}
	c.SendResponse(MessageTypeSessionCreate, msg.RequestID, responseData)

	logger.Info("Client %s created session %s", c.ID, sessionID)
	return nil
}

func (c *Client) handleSessionAttach(msg *BaseMessage) error {
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	// Parse request data
	var data SessionAttachRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid session attach request", err.Error())
		return nil
	}

	if data.SessionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Session ID is required", "")
		return nil
	}

	// Attach client to session
	if err := c.sessionManager.AttachClient(c.ID, data.SessionID); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to attach to session", err.Error())
		return nil
	}

	// Get session info to update client state
	if sessInfo, exists := c.sessionManager.GetSessionInfo(data.SessionID); exists {
		c.SetSession(data.SessionID, sessInfo.WorkingDir)
	}

	// Send response
	c.SendResponse(MessageTypeSessionAttach, msg.RequestID, map[string]interface{}{
		"session_id": data.SessionID,
		"status":     "attached",
	})

	logger.Info("Client %s attached to session %s", c.ID, data.SessionID)
	return nil
}

func (c *Client) handleSessionDetach(msg *BaseMessage) error {
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	sessionID := c.GetSession()
	if sessionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Not attached to a session", "")
		return nil
	}

	// Detach client from session
	c.sessionManager.DetachClient(c.ID)

	// Clear client session state
	c.SetSession("", "")

	// Send response
	c.SendResponse(MessageTypeSessionDetach, msg.RequestID, map[string]interface{}{
		"session_id": sessionID,
		"status":     "detached",
	})

	logger.Info("Client %s detached from session %s", c.ID, sessionID)
	return nil
}

func (c *Client) handleSessionList(msg *BaseMessage) error {
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	// Parse request data
	var data SessionListRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid session list request", err.Error())
		return nil
	}

	workingDir := data.Workspace
	if workingDir == "" {
		workingDir = c.Workspace
	}

	if workingDir == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Working directory not specified", "")
		return nil
	}

	// List sessions
	sessions, err := c.sessionManager.ListSessions(workingDir)
	if err != nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to list sessions", err.Error())
		return nil
	}

	// Convert to response format
	sessionList := make([]SessionInfoResponse, 0, len(sessions))
	for _, s := range sessions {
		sessionList = append(sessionList, SessionInfoResponse{
			ID:           s.ID,
			Title:        s.Title,
			WorkingDir:   s.WorkingDir,
			CreatedAt:    s.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    s.UpdatedAt.Format(time.RFC3339),
			MessageCount: s.MessageCount,
		})
	}

	// Send response
	c.SendResponse(MessageTypeSessionList, msg.RequestID, map[string]interface{}{
		"sessions": sessionList,
	})

	return nil
}

func (c *Client) handleSessionDelete(msg *BaseMessage) error {
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	// Parse request data
	var data SessionDeleteRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid session delete request", err.Error())
		return nil
	}

	if data.SessionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Session ID is required", "")
		return nil
	}

	workingDir := data.Workspace
	if workingDir == "" {
		workingDir = c.Workspace
	}

	if workingDir == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Working directory not specified", "")
		return nil
	}

	// Delete session
	if err := c.sessionManager.DeleteSession(workingDir, data.SessionID); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to delete session", err.Error())
		return nil
	}

	// Send response
	c.SendResponse(MessageTypeSessionDelete, msg.RequestID, map[string]interface{}{
		"session_id": data.SessionID,
		"status":     "deleted",
	})

	logger.Info("Client %s deleted session %s", c.ID, data.SessionID)
	return nil
}

func (c *Client) handleChatSend(msg *BaseMessage) error {
	if c.broker == nil {
		return fmt.Errorf("broker not initialized")
	}

	// Check if client has a session
	sessionID := c.GetSession()
	if sessionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Not attached to a session", "")
		return nil
	}

	// Parse request data
	var data ChatSendRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid chat send request", err.Error())
		return nil
	}

	if data.Content == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Content is required", "")
		return nil
	}

	// Set message callback for streaming responses
	c.broker.SetMessageCallback(func(baseMsg *BaseMessage) {
		c.Send(baseMsg)
	})

	// Process message through broker
	ctx := context.Background()
	if err := c.broker.ProcessUserMessage(ctx, data.Content, msg.RequestID); err != nil {
		logger.Error("Error processing user message for client %s: %v", c.ID, err)
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to process message", err.Error())
		return nil
	}

	// Send completion response
	c.SendResponse(MessageTypeChatSend, msg.RequestID, map[string]interface{}{
		"status": "completed",
	})

	return nil
}

func (c *Client) handleChatStop(msg *BaseMessage) error {
	if c.broker == nil {
		return fmt.Errorf("broker not initialized")
	}

	// Stop the broker's current operation
	if err := c.broker.Stop(); err != nil {
		logger.Error("Error stopping broker for client %s: %v", c.ID, err)
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to stop operation", err.Error())
		return nil
	}

	// Send response
	c.SendResponse(MessageTypeChatStop, msg.RequestID, map[string]interface{}{
		"status": "stopped",
	})

	logger.Info("Client %s stopped chat operation", c.ID)
	return nil
}

func (c *Client) handleChatClear(msg *BaseMessage) error {
	if c.broker == nil {
		return fmt.Errorf("broker not initialized")
	}

	sessionID := c.GetSession()
	if sessionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Not attached to a session", "")
		return nil
	}

	// Get the current session and clear its message history
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	sess, ok := c.sessionManager.GetSession(sessionID)
	if !ok {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to get session", "Session not found")
		return nil
	}

	// Get messages to preserve system messages
	messages := sess.GetMessages()
	systemMessages := make([]*session.Message, 0)
	for _, msg := range messages {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		}
	}

	// Clear all session data
	sess.Clear()

	// Re-add system messages
	for _, msg := range systemMessages {
		sess.AddMessage(msg)
	}

	// Mark session as dirty for auto-save
	if c.sessionManager != nil {
		c.sessionManager.MarkSessionDirty(sessionID)
	}

	// Send response
	c.SendResponse(MessageTypeChatClear, msg.RequestID, map[string]interface{}{
		"status": "cleared",
	})

	logger.Info("Client %s cleared chat history for session %s", c.ID, sessionID)
	return nil
}

func (c *Client) handleConfigGet(msg *BaseMessage) error {
	// Parse request data
	var data ConfigGetRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid config get request", err.Error())
		return nil
	}

	// The broker has access to the config
	if c.broker == nil || !c.broker.IsInitialized() {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Session not initialized", "")
		return nil
	}

	cfg := c.broker.GetConfig()
	if cfg == nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Config not available", "")
		return nil
	}

	// Build response with requested keys
	result := make(map[string]interface{})

	// If no keys specified, return common ones
	keys := data.Keys
	if len(keys) == 0 {
		keys = []string{"model", "working_dir", "temperature", "max_tokens"}
	}

	for _, key := range keys {
		switch key {
		case "model":
			// Model is not in config, get it from session
			if session := c.broker.GetSession(); session != nil {
				provider, modelFamily := session.GetCurrentProvider()
				if provider != "" && modelFamily != "" {
					result["model"] = modelFamily
					result["provider"] = provider
				}
			}
		case "working_dir", "workingDir":
			result["working_dir"] = cfg.WorkingDir
		case "temperature":
			if cfg.Temperature != 0 {
				result["temperature"] = cfg.Temperature
			}
		case "max_tokens", "maxTokens":
			if cfg.MaxTokens != 0 {
				result["max_tokens"] = cfg.MaxTokens
			}
		case "log_level", "logLevel":
			result["log_level"] = cfg.LogLevel
		case "context_dirs", "contextDirs":
			result["context_dirs"] = cfg.ContextDirectories
		case "auto_save", "autoSave":
			result["auto_save"] = map[string]interface{}{
				"enabled":               cfg.AutoSave.Enabled,
				"save_interval_seconds": cfg.AutoSave.SaveIntervalSeconds,
			}
		case "socket":
			result["socket"] = map[string]interface{}{
				"path":            cfg.Socket.Path,
				"enabled":         cfg.Socket.Enabled,
				"permissions":     cfg.Socket.Permissions,
				"max_connections": cfg.Socket.MaxConnections,
			}
		default:
			// Unknown key, skip
		}
	}

	// Send response
	c.SendResponse(MessageTypeConfigGet, msg.RequestID, map[string]interface{}{
		"config": result,
	})

	return nil
}

func (c *Client) handleConfigSet(msg *BaseMessage) error {
	// Parse request data
	var data ConfigSetRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid config set request", err.Error())
		return nil
	}

	// The broker has access to the config
	if c.broker == nil || !c.broker.IsInitialized() {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Session not initialized", "")
		return nil
	}

	cfg := c.broker.GetConfig()
	if cfg == nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Config not available", "")
		return nil
	}

	// Update allowed config values (scoped to session level)
	updated := make(map[string]interface{})

	for key, value := range data.Values {
		switch key {
		case "model":
			// Model selection is handled by provider manager, not stored in config
			// This is not modifiable via config set
			logger.Warn("Client %s attempted to set model via config (not supported)", c.ID)
		case "temperature":
			if temp, ok := value.(float64); ok && temp > 0 {
				cfg.Temperature = temp
				updated["temperature"] = temp
			}
		case "max_tokens":
			if tokens, ok := value.(float64); ok && tokens > 0 {
				cfg.MaxTokens = int(tokens)
				updated["max_tokens"] = int(tokens)
			}
		case "auto_save":
			if autoSaveMap, ok := value.(map[string]interface{}); ok {
				if enabled, ok := autoSaveMap["enabled"].(bool); ok {
					cfg.AutoSave.Enabled = enabled
					updated["auto_save_enabled"] = enabled
				}
				if interval, ok := autoSaveMap["save_interval_seconds"].(float64); ok && interval > 0 {
					cfg.AutoSave.SaveIntervalSeconds = int(interval)
					updated["auto_save_interval"] = interval
				}
			}
		default:
			// Unmodifiable config value, skip
			logger.Warn("Client %s attempted to set unmodifiable config key: %s", c.ID, key)
		}
	}

	// Update the orchestrator if there is one
	if c.broker.GetOrchestrator() != nil {
		// The orchestrator will pick up the new config values on next generation
	}

	// Send response
	c.SendResponse(MessageTypeConfigSet, msg.RequestID, map[string]interface{}{
		"updated": updated,
		"status":  "updated",
	})

	logger.Info("Client %s updated config: %v", c.ID, updated)
	return nil
}

func (c *Client) handleWorkspaceList(msg *BaseMessage) error {
	if c.workspaceManager == nil {
		return fmt.Errorf("workspace manager not initialized")
	}

	// Get all workspaces
	workspaces := c.workspaceManager.ListWorkspaces()

	// Convert to API format
	workspaceList := make([]WorkspaceInfo, 0, len(workspaces))
	for _, ws := range workspaces {
		// Create maps for approved domains and commands
		domainsApproved := make(map[string]bool)
		for domain, approved := range ws.DomainsApproved {
			domainsApproved[domain] = approved
		}
		commandsApproved := make(map[string]bool)
		for cmd, approved := range ws.CommandsApproved {
			commandsApproved[cmd] = approved
		}

		workspaceList = append(workspaceList, WorkspaceInfo{
			ID:               ws.ID,
			Path:             ws.Path,
			Name:             ws.Name,
			RepositoryRoot:   ws.RepositoryRoot,
			CurrentBranch:    ws.CurrentBranch,
			IsWorktree:       ws.IsWorktree,
			WorktreeName:     ws.WorktreeName,
			SessionCount:     ws.SessionCount,
			LastAccessed:     ws.LastAccessed.Format(time.RFC3339),
			CreatedAt:        ws.CreatedAt.Format(time.RFC3339),
			ContextDirs:      ws.ContextDirs,
			LandlockRead:     ws.LandlockRead,
			LandlockWrite:    ws.LandlockWrite,
			DomainsApproved:  domainsApproved,
			CommandsApproved: commandsApproved,
		})
	}

	// Send response
	c.SendResponse(MessageTypeWorkspaceList, msg.RequestID, map[string]interface{}{
		"workspaces": workspaceList,
	})

	return nil
}

func (c *Client) handleWorkspaceSet(msg *BaseMessage) error {
	if c.workspaceManager == nil {
		return fmt.Errorf("workspace manager not initialized")
	}

	// Parse request data
	var data WorkspaceSetRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid workspace set request", err.Error())
		return nil
	}

	if data.Workspace == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Workspace path is required", "")
		return nil
	}

	// Resolve workspace
	ctx := context.Background()
	ws, err := c.workspaceManager.ResolveWorkspace(ctx, data.Workspace)
	if err != nil {
		c.SendError(msg.RequestID, ErrorCodeWorkspaceInvalid, "Invalid workspace path", err.Error())
		return nil
	}

	// Update client workspace
	c.SetWorkspace(data.Workspace)
	c.workspaceManager.UpdateWorkspaceAccess(ws.ID)

	// Send response
	c.SendResponse(MessageTypeWorkspaceSet, msg.RequestID, map[string]interface{}{
		"workspace_id": ws.ID,
		"path":         ws.Path,
		"name":         ws.Name,
		"status":       "set",
	})

	logger.Info("Client %s set workspace to %s (%s)", c.ID, ws.Name, ws.Path)
	return nil
}

func (c *Client) handleSessionSave(msg *BaseMessage) error {
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	sessionID := c.GetSession()
	if sessionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Not attached to a session", "")
		return nil
	}

	// Parse request data
	var data SessionSaveRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid session save request", err.Error())
		return nil
	}

	// Save session (name is optional)
	if err := c.sessionManager.SaveSession(sessionID, data.Name); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to save session", err.Error())
		return nil
	}

	// Get session info for response
	if sessInfo, exists := c.sessionManager.GetSessionInfo(sessionID); exists {
		c.SendResponse(MessageTypeSessionSave, msg.RequestID, map[string]interface{}{
			"session_id": sessionID,
			"title":      sessInfo.Title,
			"saved_at":   time.Now().Format(time.RFC3339),
			"status":     "saved",
		})
	} else {
		c.SendResponse(MessageTypeSessionSave, msg.RequestID, map[string]interface{}{
			"session_id": sessionID,
			"status":     "saved",
		})
	}

	logger.Info("Client %s saved session %s", c.ID, sessionID)
	return nil
}

func (c *Client) handleSessionLoad(msg *BaseMessage) error {
	if c.sessionManager == nil {
		return fmt.Errorf("session manager not initialized")
	}

	// Parse request data
	var data SessionLoadRequest
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid session load request", err.Error())
		return nil
	}

	if data.SessionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Session ID is required", "")
		return nil
	}

	workingDir := data.Workspace
	if workingDir == "" {
		workingDir = c.Workspace
	}

	if workingDir == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Working directory not specified", "")
		return nil
	}

	// Detach from current session if any
	if currentSessionID := c.GetSession(); currentSessionID != "" {
		c.sessionManager.DetachClient(c.ID)
	}

	// Load session
	sess, err := c.sessionManager.LoadSession(workingDir, data.SessionID)
	if err != nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to load session", err.Error())
		return nil
	}

	// Attach client to the loaded session
	if err := c.sessionManager.AttachClient(c.ID, data.SessionID); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to attach to session", err.Error())
		return nil
	}

	// Update client session state
	c.SetSession(data.SessionID, workingDir)

	// Get session messages for replay
	messages := sess.GetMessages()

	// Convert messages to response format
	messageList := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		msgData := map[string]interface{}{
			"role":      msg.Role,
			"content":   msg.Content,
			"timestamp": msg.Timestamp.Format(time.RFC3339),
		}
		if msg.Reasoning != "" {
			msgData["reasoning"] = msg.Reasoning
		}
		if len(msg.ToolCalls) > 0 {
			msgData["tool_calls"] = msg.ToolCalls
		}
		if msg.ToolID != "" {
			msgData["tool_id"] = msg.ToolID
		}
		if msg.ToolName != "" {
			msgData["tool_name"] = msg.ToolName
		}
		messageList = append(messageList, msgData)
	}

	// Send response
	c.SendResponse(MessageTypeSessionLoad, msg.RequestID, map[string]interface{}{
		"session_id":  data.SessionID,
		"working_dir": workingDir,
		"title":       sess.Title,
		"messages":    messageList,
		"status":      "loaded",
	})

	logger.Info("Client %s loaded session %s (%d messages)", c.ID, data.SessionID, len(messages))
	return nil
}

func (c *Client) handleAuthorizationResponse(msg *BaseMessage) error {
	if c.broker == nil {
		return fmt.Errorf("broker not initialized")
	}

	// Parse request data
	var data AuthorizationResponseData
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid authorization response", err.Error())
		return nil
	}

	if data.AuthID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Auth ID is required", "")
		return nil
	}

	// Handle ack if present
	if data.Ack {
		if err := c.broker.HandleAuthorizationAck(data.AuthID); err != nil {
			logger.Error("Error handling authorization ack for client %s: %v", c.ID, err)
			c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to handle ack", err.Error())
			return nil
		}
		return nil
	}

	// Handle response
	if err := c.broker.HandleAuthorizationResponse(data.AuthID, data.Approved); err != nil {
		logger.Error("Error handling authorization response for client %s: %v", c.ID, err)
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to handle authorization response", err.Error())
		return nil
	}

	logger.Debug("Client %s sent authorization response for %s: approved=%v", c.ID, data.AuthID, data.Approved)
	return nil
}

func (c *Client) handleQuestionResponse(msg *BaseMessage) error {
	if c.broker == nil {
		return fmt.Errorf("broker not initialized")
	}

	// Parse request data
	var data QuestionResponseData
	if err := parseData(msg.Data, &data); err != nil {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Invalid question response", err.Error())
		return nil
	}

	if data.QuestionID == "" {
		c.SendError(msg.RequestID, ErrorCodeInvalidRequest, "Question ID is required", "")
		return nil
	}

	// Handle response
	if err := c.broker.HandleQuestionResponse(data.QuestionID, data.Answer, data.Answers); err != nil {
		logger.Error("Error handling question response for client %s: %v", c.ID, err)
		c.SendError(msg.RequestID, ErrorCodeInternalError, "Failed to handle question response", err.Error())
		return nil
	}

	logger.Debug("Client %s sent question response for %s", c.ID, data.QuestionID)
	return nil
}

// parseData is a helper to parse message data into a struct
func parseData(data map[string]interface{}, v interface{}) error {
	// Use json.Marshal/Unmarshal for robust parsing
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}
	if err := json.Unmarshal(jsonBytes, v); err != nil {
		return fmt.Errorf("failed to unmarshal data: %w", err)
	}
	return nil
}
