package socketclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectionState represents the current state of the socket connection
type ConnectionState int

const (
	// StateDisconnected indicates the client is not connected
	StateDisconnected ConnectionState = iota
	// StateConnecting indicates the client is attempting to connect
	StateConnecting
	// StateConnected indicates the client is connected and authenticated
	StateConnected
	// StateReconnecting indicates the client is attempting to reconnect
	StateReconnecting
	// StateClosed indicates the client has been closed
	StateClosed
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// SocketError represents an error from the socket server
type SocketError struct {
	Code    string
	Message string
	Details string
}

func (e *SocketError) Error() string {
	msg := fmt.Sprintf("[%s] %s", e.Code, e.Message)
	if e.Details != "" {
		msg += ": " + e.Details
	}
	return msg
}

// NewSocketError creates a new SocketError
func NewSocketError(code, message, details string) *SocketError {
	return &SocketError{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// Config holds client configuration
type Config struct {
	// SocketPath is the path to the Unix socket
	SocketPath string
	// ClientType identifies the type of client (e.g., "tui", "cli", "web")
	ClientType string
	// ClientVersion is the version of the client
	ClientVersion string
	// Capabilities is a list of client capabilities
	Capabilities []string
	// AuthToken is an optional authentication token
	AuthToken string
	// ConnectTimeout is the timeout for initial connection
	ConnectTimeout time.Duration
	// ReconnectEnabled enables automatic reconnection
	ReconnectEnabled bool
	// MaxReconnectAttempts is the maximum number of reconnection attempts
	MaxReconnectAttempts int
	// ReconnectDelay is the initial delay between reconnection attempts
	ReconnectDelay time.Duration
	// ReconnectMaxDelay is the maximum delay between reconnection attempts
	ReconnectMaxDelay time.Duration
	// RequestTimeout is the default timeout for requests
	RequestTimeout time.Duration
	// ReadTimeout is the timeout for reading messages
	ReadTimeout time.Duration
	// WriteTimeout is the timeout for writing messages
	WriteTimeout time.Duration
	// PingInterval is the interval for sending ping messages
	PingInterval time.Duration
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		SocketPath:           "~/.scriptschnell.sock",
		ClientType:           "client",
		ClientVersion:        "1.0.0",
		Capabilities:         []string{"chat", "sessions", "workspaces"},
		ConnectTimeout:       10 * time.Second,
		ReconnectEnabled:     true,
		MaxReconnectAttempts: 10,
		ReconnectDelay:       2 * time.Second,
		ReconnectMaxDelay:    30 * time.Second,
		RequestTimeout:       30 * time.Second,
		ReadTimeout:          60 * time.Second,
		WriteTimeout:         10 * time.Second,
		PingInterval:         54 * time.Second,
	}
}

// Client represents a socket client
type Client struct {
	config *Config

	// Connection
	conn         net.Conn
	connMu       sync.RWMutex
	state        atomic.Int32 // ConnectionState
	connectCtx   context.Context
	connectCancel context.CancelFunc
	connectMu    sync.Mutex

	// Message I/O
	outgoing chan *Message
	incoming chan *Message

	// Request tracking
	pendingRequests map[string]chan *Message
	requestMu       sync.RWMutex

	// Callbacks
	chatMessageCallback        func(ChatMessage)
	toolCallCallback           func(ToolCall)
	toolResultCallback         func(ToolResult)
	progressCallback           func(ProgressData)
	authorizationCallback      func(AuthorizationRequest) (bool, error)
	questionCallback           func(QuestionRequest) (map[string]string, error)
	completionCallback         func(requestID string, success bool, errorMsg string)
	stateChangedCallback       func(ConnectionState, error)
	reconnectingCallback       func(attempt int, maxAttempts int)
	connectionLostCallback     func(error)

	// Session tracking
	currentSessionID atomic.Value // string
	currentWorkspace atomic.Value // string

	// Reconnection
	reconnectAttempts int
	reconnectMu       sync.Mutex

	// Lifecycle
	wg     sync.WaitGroup
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewClient creates a new socket client
func NewClient(socketPath string) (*Client, error) {
	config := DefaultConfig()
	config.SocketPath = socketPath
	return NewClientWithConfig(config)
}

// NewClientWithConfig creates a new socket client with custom configuration
func NewClientWithConfig(config *Config) (*Client, error) {
	if config.SocketPath == "" {
		return nil, errors.New("socket path is required")
	}

	client := &Client{
		config:          config,
		outgoing:        make(chan *Message, 256),
		incoming:        make(chan *Message, 256),
		pendingRequests: make(map[string]chan *Message),
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}

	// Set initial state
	client.state.Store(int32(StateDisconnected))

	// Initialize session tracking
	client.currentSessionID.Store("")
	client.currentWorkspace.Store("")

	return client, nil
}

// expandPath expands ~ to the home directory
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

// Connect connects to the socket server
func (c *Client) Connect(ctx context.Context) error {
	return c.connect(ctx, false)
}

// connect performs the actual connection logic
func (c *Client) connect(ctx context.Context, isReconnect bool) error {
	// Check if already connected
	if c.getState() != StateDisconnected && !isReconnect {
		return errors.New("already connected")
	}

	// Set connecting state
	c.setState(StateConnecting)

	// Store context for cancellation
	c.connectMu.Lock()
	c.connectCtx, c.connectCancel = context.WithCancel(ctx)
	c.connectMu.Unlock()

	// Start with timeout
	_, cancel := context.WithTimeout(ctx, c.config.ConnectTimeout)
	defer cancel()

	// Attempt connection
	socketPath := expandPath(c.config.SocketPath)
	conn, err := net.DialTimeout("unix", socketPath, c.config.ConnectTimeout)
	if err != nil {
		c.setState(StateDisconnected)
		return fmt.Errorf("failed to connect to socket %s: %w", socketPath, err)
	}

	// Store connection
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	// Start message pumps
	c.wg.Add(2)
	go c.readPump()
	go c.writePump()

	// Send authentication request
	authReq := map[string]interface{}{
		"client_type":   c.config.ClientType,
		"version":       c.config.ClientVersion,
		"capabilities":  c.config.Capabilities,
	}

	if c.config.AuthToken != "" {
		authReq["token"] = c.config.AuthToken
	}

	authMsg := NewMessage("auth_request", authReq)

	// Wait for authentication response
	resp, err := c.SendRequest(authMsg)
	if err != nil {
		c.Close()
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Check if authentication was successful
	if respType, ok := resp.GetType(); ok && respType != "auth_response" {
		c.Close()
		return fmt.Errorf("expected auth_response, got %s", respType)
	}

	if resp.Error != nil {
		c.Close()
		return NewSocketError(resp.Error.Code, resp.Error.Message, resp.Error.Details)
	}

	// Parse auth response
	var authRespData struct {
		Success           bool     `json:"success"`
		ConnectionID      string   `json:"connection_id"`
		ServerVersion     string   `json:"server_version"`
		ServerCapabilities []string `json:"server_capabilities"`
	}

	if err := json.Unmarshal(resp.Data, &authRespData); err != nil {
		c.Close()
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	if !authRespData.Success {
		c.Close()
		return errors.New("authentication rejected by server")
	}

	// Set connected state
	c.setState(StateConnected)
	c.reconnectAttempts = 0

	// Start ping ticker
	c.wg.Add(1)
	go c.pingPong()

	return nil
}

// getState returns the current connection state
func (c *Client) getState() ConnectionState {
	return ConnectionState(c.state.Load())
}

// setState sets the connection state and notifies callback
func (c *Client) setState(state ConnectionState) {
	oldState := c.getState()
	c.state.Store(int32(state))

	if c.stateChangedCallback != nil && oldState != state {
		c.stateChangedCallback(state, nil)
	}
}

// Disconnect disconnects from the socket server gracefully
func (c *Client) Disconnect() error {
	// Send close message if connected
	if c.getState() == StateConnected {
		closeMsg := NewMessage("close", map[string]interface{}{
			"reason":           "client_disconnect",
			"preserve_session": true,
		})
		c.outgoing <- closeMsg
	}

	// Wait a bit for message to be sent
	time.Sleep(100 * time.Millisecond)

	return c.Close()
}

// Close immediately closes the connection without sending close message
func (c *Client) Close() error {
	// Check if already closed
	if c.getState() == StateClosed {
		return nil
	}

	// Set closed state
	c.setState(StateClosed)

	// Cancel connection context
	c.connectMu.Lock()
	if c.connectCancel != nil {
		c.connectCancel()
	}
	c.connectMu.Unlock()

	// Close connection
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()

	// Stop message pumps
	close(c.stopCh)

	// Close channels
	close(c.outgoing)
	close(c.incoming)

	// Wait for goroutines to finish
	c.wg.Wait()

	// Clear pending requests
	c.requestMu.Lock()
	for _, ch := range c.pendingRequests {
		close(ch)
	}
	c.pendingRequests = make(map[string]chan *Message)
	c.requestMu.Unlock()

	return nil
}

// IsConnected returns true if the client is connected
func (c *Client) IsConnected() bool {
	return c.getState() == StateConnected
}

// GetState returns the current connection state
func (c *Client) GetState() ConnectionState {
	return c.getState()
}

// readPump reads messages from the connection
func (c *Client) readPump() {
	defer c.wg.Done()

	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return
	}

	reader := bufio.NewReader(conn)

	for {
		select {
		case <-c.stopCh:
			return
		default:
			// Set read deadline
			conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))

			// Read message (newline-delimited JSON)
			line, err := reader.ReadString('\n')
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					c.handleConnectionError(err)
				}
				return
			}

			// Parse message
			msg, err := ParseMessage(line)
			if err != nil {
				// Send error message back to server
				continue
			}

			// Route message
			c.routeMessage(msg)
		}
	}
}

// writePump writes messages to the connection
func (c *Client) writePump() {
	defer c.wg.Done()

	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return
	}

	for {
		select {
		case <-c.stopCh:
			return
		case msg, ok := <-c.outgoing:
			if !ok {
				return
			}

			// Set write deadline
			conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))

			// Serialize message
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			// Write message with newline
			if _, err := conn.Write(append(data, '\n')); err != nil {
				c.handleConnectionError(err)
				return
			}
		}
	}
}

// routeMessage routes incoming messages to the appropriate handler
func (c *Client) routeMessage(msg *Message) {
	// Check if this is a response to a request
	if requestID, ok := msg.GetRequestID(); ok && requestID != "" {
		c.requestMu.RLock()
		ch, exists := c.pendingRequests[requestID]
		c.requestMu.RUnlock()

		if exists {
			ch <- msg
			return
		}
	}

	// Route message to callbacks
	msgType, _ := msg.GetType()

	switch msgType {
	case "chat_message":
		if c.chatMessageCallback != nil {
			var chatMsg ChatMessage
			if err := json.Unmarshal(msg.Data, &chatMsg); err == nil {
				c.chatMessageCallback(chatMsg)
			}
		}
	case "tool_call":
		if c.toolCallCallback != nil {
			var toolCall ToolCall
			if err := json.Unmarshal(msg.Data, &toolCall); err == nil {
				c.toolCallCallback(toolCall)
			}
		}
	case "tool_result":
		if c.toolResultCallback != nil {
			var toolResult ToolResult
			if err := json.Unmarshal(msg.Data, &toolResult); err == nil {
				c.toolResultCallback(toolResult)
			}
		}
	case "progress":
		if c.progressCallback != nil {
			var progress ProgressData
			if err := json.Unmarshal(msg.Data, &progress); err == nil {
				c.progressCallback(progress)
			}
		}
	case "authorization_request":
		c.handleAuthorizationRequest(msg)
	case "question_request":
		c.handleQuestionRequest(msg)
	case "closed":
		c.handleServerClosed(msg)
	case "pong":
		// Handled by ping/pong ticker
	case "error":
		// Error messages are typically responses to requests
		if c.stateChangedCallback != nil && msg.Error != nil {
			c.stateChangedCallback(c.getState(), NewSocketError(msg.Error.Code, msg.Error.Message, msg.Error.Details))
		}
	}
}

// pingPong sends periodic ping messages
func (c *Client) pingPong() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			if c.getState() != StateConnected {
				continue
			}

			pingMsg := NewMessage("ping", nil)
			c.outgoing <- pingMsg
		}
	}
}

// handleConnectionError handles connection errors
func (c *Client) handleConnectionError(err error) {
	// Set disconnected state
	oldState := c.getState()
	c.setState(StateDisconnected)

	// Notify callback
	if c.connectionLostCallback != nil {
		c.connectionLostCallback(err)
	}

	// Attempt reconnection if enabled and not explicitly closed
	if c.config.ReconnectEnabled && oldState != StateClosed {
		c.attemptReconnect()
	}
}

// attemptReconnect attempts to reconnect to the server
func (c *Client) attemptReconnect() {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	// Check if we've exceeded max attempts
	if c.reconnectAttempts >= c.config.MaxReconnectAttempts {
		return
	}

	// Calculate delay with exponential backoff
	delay := c.config.ReconnectDelay * time.Duration(1<<uint(c.reconnectAttempts))
	if delay > c.config.ReconnectMaxDelay {
		delay = c.config.ReconnectMaxDelay
	}

	// Notify callback
	if c.reconnectingCallback != nil {
		c.reconnectingCallback(c.reconnectAttempts+1, c.config.MaxReconnectAttempts)
	}

	// Wait before attempting reconnection
	time.Sleep(delay)

	// Increment attempts
	c.reconnectAttempts++

	// Attempt reconnection
	c.setState(StateReconnecting)

	// Save current session and workspace
	sessionID := c.GetCurrentSessionID()
	workspace := c.GetCurrentWorkspace()

	ctx, cancel := context.WithTimeout(context.Background(), c.config.ConnectTimeout)
	defer cancel()

	err := c.connect(ctx, true)
	if err != nil {
		// Reconnection failed, will retry
		return
	}

	// Reconnection successful, restore session
	if sessionID != "" {
		// Attempt to reattach to session
		attachMsg := NewMessage("session_attach", map[string]interface{}{
			"session_id": sessionID,
		})
		_, err = c.SendRequest(attachMsg)
		if err != nil {
			// Failed to reattach, but connection is alive
			c.currentSessionID.Store("")
		}
	}

	// Set workspace
	if workspace != "" {
		setMsg := NewMessage("workspace_set", map[string]interface{}{
			"workspace": workspace,
		})
		_, err = c.SendRequest(setMsg)
		if err != nil {
			c.currentWorkspace.Store("")
		}
	}

	// Reset reconnect attempts
	c.reconnectAttempts = 0
}

// handleServerClosed handles server-side close notification
func (c *Client) handleServerClosed(msg *Message) {
	var closedData struct {
		Reason    string `json:"reason"`
		Reconnect bool   `json:"reconnect"`
	}

	if err := json.Unmarshal(msg.Data, &closedData); err != nil {
		closedData.Reason = "unknown"
		closedData.Reconnect = false
	}

	// Disable reconnection if server doesn't want it
	if !closedData.Reconnect {
		c.config.ReconnectEnabled = false
	}

	// Handle connection error
	c.handleConnectionError(NewSocketError("SERVER_CLOSED", closedData.Reason, ""))
}

// handleAuthorizationRequest handles an authorization request from the server
func (c *Client) handleAuthorizationRequest(msg *Message) {
	var authReq AuthorizationRequest
	if err := json.Unmarshal(msg.Data, &authReq); err != nil {
		return
	}

	// Send acknowledgment
	ackMsg := NewMessage("authorization_response", map[string]interface{}{
		"auth_id": authReq.AuthID,
		"ack":     true,
	})
	c.SendMessage(ackMsg)

	// Check if callback is set
	if c.authorizationCallback == nil {
		// Auto-deny if no callback
		respMsg := NewMessage("authorization_response", map[string]interface{}{
			"auth_id":  authReq.AuthID,
			"approved": false,
		})
		c.SendMessage(respMsg)
		return
	}

	// Call callback in goroutine to avoid blocking
	go func() {
		approved, err := c.authorizationCallback(authReq)

		respData := map[string]interface{}{
			"auth_id": authReq.AuthID,
		}

		if err != nil {
			respData["approved"] = false
		} else {
			respData["approved"] = approved
		}

		respMsg := NewMessage("authorization_response", respData)
		c.SendMessage(respMsg)
	}()
}

// handleQuestionRequest handles a question request from the server
func (c *Client) handleQuestionRequest(msg *Message) {
	var qReq QuestionRequest
	if err := json.Unmarshal(msg.Data, &qReq); err != nil {
		return
	}

	// Check if callback is set
	if c.questionCallback == nil {
		// Auto-empty response if no callback
		respMsg := NewMessage("question_response", map[string]interface{}{
			"question_id": qReq.QuestionID,
			"answer":     "",
		})
		c.SendMessage(respMsg)
		return
	}

	// Call callback in goroutine to avoid blocking
	go func() {
		answers, err := c.questionCallback(qReq)

		respData := map[string]interface{}{
			"question_id": qReq.QuestionID,
		}

		if err != nil {
			// Send empty answer on error
			respData["answer"] = ""
		} else if qReq.MultiMode {
			respData["answers"] = answers
		} else {
			if ans, ok := answers["answer"]; ok {
				respData["answer"] = ans
			} else {
				respData["answer"] = ""
			}
		}

		respMsg := NewMessage("question_response", respData)
		c.SendMessage(respMsg)
	}()
}

// GetCurrentSessionID returns the current session ID
func (c *Client) GetCurrentSessionID() string {
	if v := c.currentSessionID.Load(); v != nil {
		s, ok := v.(string)
		if ok {
			return s
		}
	}
	return ""
}

// GetCurrentWorkspace returns the current workspace
func (c *Client) GetCurrentWorkspace() string {
	if v := c.currentWorkspace.Load(); v != nil {
		s, ok := v.(string)
		if ok {
			return s
		}
	}
	return ""
}

// SetChatMessageCallback sets the callback for chat messages
func (c *Client) SetChatMessageCallback(fn func(ChatMessage)) {
	c.chatMessageCallback = fn
}

// SetToolCallCallback sets the callback for tool calls
func (c *Client) SetToolCallCallback(fn func(ToolCall)) {
	c.toolCallCallback = fn
}

// SetToolResultCallback sets the callback for tool results
func (c *Client) SetToolResultCallback(fn func(ToolResult)) {
	c.toolResultCallback = fn
}

// SetProgressCallback sets the callback for progress updates
func (c *Client) SetProgressCallback(fn func(ProgressData)) {
	c.progressCallback = fn
}

// SetAuthorizationCallback sets the callback for authorization requests
func (c *Client) SetAuthorizationCallback(fn func(AuthorizationRequest) (bool, error)) {
	c.authorizationCallback = fn
}

// SetQuestionCallback sets the callback for question requests
func (c *Client) SetQuestionCallback(fn func(QuestionRequest) (map[string]string, error)) {
	c.questionCallback = fn
}

// SetStateChangedCallback sets the callback for connection state changes
func (c *Client) SetStateChangedCallback(fn func(ConnectionState, error)) {
	c.stateChangedCallback = fn
}

// SetReconnectingCallback sets the callback for reconnection attempts
func (c *Client) SetReconnectingCallback(fn func(attempt int, maxAttempts int)) {
	c.reconnectingCallback = fn
}

// SetConnectionLostCallback sets the callback for connection loss events
func (c *Client) SetConnectionLostCallback(fn func(error)) {
	c.connectionLostCallback = fn
}

// SetCompletionCallback sets the callback for completion notifications
func (c *Client) SetCompletionCallback(fn func(requestID string, success bool, errorMsg string)) {
	c.completionCallback = fn
}

// SetReconnectEnabled enables or disables automatic reconnection
func (c *Client) SetReconnectEnabled(enabled bool) {
	c.config.ReconnectEnabled = enabled
}

// SetMaxReconnectAttempts sets the maximum number of reconnection attempts
func (c *Client) SetMaxReconnectAttempts(attempts int) {
	c.config.MaxReconnectAttempts = attempts
}

// SetReconnectDelay sets the initial delay between reconnection attempts
func (c *Client) SetReconnectDelay(delay time.Duration) {
	c.config.ReconnectDelay = delay
}

// SetReconnectMaxDelay sets the maximum delay between reconnection attempts
func (c *Client) SetReconnectMaxDelay(delay time.Duration) {
	c.config.ReconnectMaxDelay = delay
}

// SetRequestTimeout sets the default timeout for requests
func (c *Client) SetRequestTimeout(timeout time.Duration) {
	c.config.RequestTimeout = timeout
}

// GetReconnectAttempts returns the current number of reconnection attempts
func (c *Client) GetReconnectAttempts() int {
	return c.reconnectAttempts
}