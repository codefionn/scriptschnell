package socketclient

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Message represents a protocol message
type Message struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Error     *ErrorInfo      `json:"error,omitempty"`
}

// ErrorInfo contains error details
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// GetType returns the message type
func (m *Message) GetType() (string, bool) {
	if m.Type == "" {
		return "", false
	}
	return m.Type, true
}

// GetRequestID returns the request ID
func (m *Message) GetRequestID() (string, bool) {
	if m.RequestID == "" {
		return "", false
	}
	return m.RequestID, true
}

// NewMessage creates a new message
func NewMessage(msgType string, data interface{}) *Message {
	var rawData json.RawMessage
	if data != nil {
		bytes, err := json.Marshal(data)
		if err == nil {
			rawData = bytes
		}
	}

	return &Message{
		Type:      msgType,
		RequestID: uuid.New().String(),
		Data:      rawData,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// NewMessageWithRequestID creates a new message with a specific request ID
func NewMessageWithRequestID(msgType, requestID string, data interface{}) *Message {
	var rawData json.RawMessage
	if data != nil {
		bytes, err := json.Marshal(data)
		if err == nil {
			rawData = bytes
		}
	}

	return &Message{
		Type:      msgType,
		RequestID: requestID,
		Data:      rawData,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// ParseMessage parses a message from JSON bytes
func ParseMessage(data string) (*Message, error) {
	var msg Message
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// SendRequest sends a request and waits for a response
func (c *Client) SendRequest(msg *Message) (*Message, error) {
	// Generate request ID if not provided
	if msg.RequestID == "" {
		msg.RequestID = uuid.New().String()
	}

	// Create response channel
	respCh := make(chan *Message, 1)

	// Register pending request
	c.requestMu.Lock()
	c.pendingRequests[msg.RequestID] = respCh
	c.requestMu.Unlock()

	// Cleanup function
	defer func() {
		c.requestMu.Lock()
		delete(c.pendingRequests, msg.RequestID)
		c.requestMu.Unlock()
		close(respCh)
	}()

	// Send message
	select {
	case c.outgoing <- msg:
	case <-c.stopCh:
		return nil, NewSocketError("CONNECTION_CLOSED", "Client is closed", "")
	case <-time.After(c.config.WriteTimeout):
		return nil, NewSocketError("TIMEOUT", "Write timeout", "")
	}

	// Wait for response
	select {
	case resp := <-respCh:
		// Check for error response
		if resp.Error != nil {
			return nil, NewSocketError(resp.Error.Code, resp.Error.Message, resp.Error.Details)
		}
		return resp, nil
	case <-c.stopCh:
		return nil, NewSocketError("CONNECTION_CLOSED", "Client is closed", "")
	case <-time.After(c.config.RequestTimeout):
		return nil, NewSocketError("TIMEOUT", "Request timeout", "")
	}
}

// SendMessage sends a message without waiting for a response
func (c *Client) SendMessage(msg *Message) error {
	// Generate request ID if not provided
	if msg.RequestID == "" {
		msg.RequestID = uuid.New().String()
	}

	select {
	case c.outgoing <- msg:
		return nil
	case <-c.stopCh:
		return NewSocketError("CONNECTION_CLOSED", "Client is closed", "")
	case <-time.After(c.config.WriteTimeout):
		return NewSocketError("TIMEOUT", "Write timeout", "")
	}
}

// ChatMessage represents a chat message
type ChatMessage struct {
	SessionID  string    `json:"session_id,omitempty"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	StreamID   string    `json:"stream_id,omitempty"`
	ChunkIndex int       `json:"chunk_index,omitempty"`
	IsFinal    bool      `json:"is_final,omitempty"`
	Reasoning  string    `json:"reasoning,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// ToolCall represents a tool call notification
type ToolCall struct {
	SessionID   string                 `json:"session_id,omitempty"`
	ToolName    string                 `json:"tool_name"`
	ToolID      string                 `json:"tool_id"`
	Parameters  map[string]interface{} `json:"parameters"`
	Description string                 `json:"description,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
}

// ToolResult represents a tool execution result
type ToolResult struct {
	SessionID string    `json:"session_id,omitempty"`
	ToolID    string    `json:"tool_id"`
	Result    *string   `json:"result,omitempty"`
	Error     *string   `json:"error,omitempty"`
	Status    string    `json:"status,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ProgressData represents a progress update
type ProgressData struct {
	SessionID         string `json:"session_id,omitempty"`
	Message           string `json:"message"`
	Status            string `json:"status,omitempty"`
	ContextUsage      int    `json:"context_usage,omitempty"`
	Ephemeral         bool   `json:"ephemeral,omitempty"`
	VerificationAgent bool   `json:"verification_agent,omitempty"`
	IsCompact         bool   `json:"is_compact,omitempty"`
	Reasoning         string `json:"reasoning,omitempty"`
	Mode              string `json:"mode,omitempty"`
}

// AuthorizationRequest represents an authorization request
type AuthorizationRequest struct {
	SessionID  string                 `json:"session_id,omitempty"`
	AuthID     string                 `json:"auth_id"`
	ToolName   string                 `json:"tool_name"`
	Parameters map[string]interface{} `json:"parameters"`
	Reason     string                 `json:"reason"`
}

// AuthorizationAck represents an authorization acknowledgment
type AuthorizationAck struct {
	AuthID string `json:"auth_id"`
}

// AuthorizationResponse represents an authorization response
type AuthorizationResponse struct {
	AuthID   string `json:"auth_id"`
	Approved bool   `json:"approved"`
}

// QuestionRequest represents a question request
type QuestionRequest struct {
	SessionID  string            `json:"session_id,omitempty"`
	QuestionID string            `json:"question_id"`
	Question   string            `json:"question"`
	MultiMode  bool              `json:"multi_mode"`
	Questions  map[string]string `json:"questions,omitempty"`
}

// QuestionResponse represents a question response
type QuestionResponse struct {
	QuestionID string            `json:"question_id"`
	Answer     string            `json:"answer,omitempty"`
	Answers    map[string]string `json:"answers,omitempty"`
}

// SessionInfo represents session information
type SessionInfo struct {
	SessionID      string           `json:"session_id"`
	Workspace      string           `json:"workspace"`
	Title          string           `json:"title"`
	CreatedAt      string           `json:"created_at"`
	UpdatedAt      string           `json:"updated_at"`
	MessageCount   int              `json:"message_count"`
	Status         string           `json:"status"`
	TotalTokens    int64            `json:"total_tokens,omitempty"`
	CachedTokens   int64            `json:"cached_tokens,omitempty"`
	TotalCost      float64          `json:"total_cost,omitempty"`
	MessageHistory []MessageHistory `json:"message_history,omitempty"`
}

// MessageHistory represents a message in session history
type MessageHistory struct {
	Role      string    `json:"role"`
	Content   string    `json:"content,omitempty"`
	Reasoning string    `json:"reasoning,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WorkspaceInfo represents workspace information
type WorkspaceInfo struct {
	ID               string          `json:"id"`
	Path             string          `json:"path"`
	Name             string          `json:"name"`
	RepositoryRoot   string          `json:"repository_root"`
	CurrentBranch    string          `json:"current_branch"`
	IsWorktree       bool            `json:"is_worktree"`
	WorktreeName     string          `json:"worktree_name"`
	SessionCount     int             `json:"session_count"`
	LastAccessed     string          `json:"last_accessed"`
	CreatedAt        string          `json:"created_at"`
	ContextDirs      []string        `json:"context_dirs"`
	LandlockRead     []string        `json:"landlock_read"`
	LandlockWrite    []string        `json:"landlock_write"`
	DomainsApproved  map[string]bool `json:"domains_approved"`
	CommandsApproved map[string]bool `json:"commands_approved"`
}

// ConfigValue represents a configuration value
type ConfigValue struct {
	Value interface{} `json:"value"`
	Type  string      `json:"type"`
}
