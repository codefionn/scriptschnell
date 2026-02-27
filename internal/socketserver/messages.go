package socketserver

import "time"

// Message type constants
const (
	// Handshake & Authentication
	MessageTypeAuthRequest  = "auth_request"
	MessageTypeAuthResponse = "auth_response"

	// Session Management
	MessageTypeSessionCreate            = "session_create"
	MessageTypeSessionCreateResponse    = "session_create_response"
	MessageTypeSessionAttach            = "session_attach"
	MessageTypeSessionDetach            = "session_detach"
	MessageTypeSessionList              = "session_list"
	MessageTypeSessionListResponse      = "session_list_response"
	MessageTypeSessionDelete            = "session_delete"

	// Chat & Generation
	MessageTypeChatSend    = "chat_send"
	MessageTypeChatStop    = "chat_stop"
	MessageTypeChatClear   = "chat_clear"
	MessageTypeChatMessage = "chat_message"

	// Tool Interactions
	MessageTypeToolCall    = "tool_call"
	MessageTypeToolResult  = "tool_result"
	MessageTypeToolCompact = "tool_compact"

	// Authorization
	MessageTypeAuthorizationRequest  = "authorization_request"
	MessageTypeAuthorizationAck      = "authorization_ack"
	MessageTypeAuthorizationResponse = "authorization_response"

	// Question Dialogs (Planning Agent)
	MessageTypeQuestionRequest  = "question_request"
	MessageTypeQuestionResponse = "question_response"

	// Progress Updates
	MessageTypeProgress = "progress"

	// Configuration
	MessageTypeConfigGet = "config_get"
	MessageTypeConfigSet = "config_set"

	// Workspace Management
	MessageTypeWorkspaceList       = "workspace_list"
	MessageTypeWorkspaceListResponse = "workspace_list_response"
	MessageTypeWorkspaceSet       = "workspace_set"

	// Session Persistence
	MessageTypeSessionSave = "session_save"
	MessageTypeSessionLoad = "session_load"

	// Connection Lifecycle
	MessageTypePing   = "ping"
	MessageTypePong   = "pong"
	MessageTypeClose  = "close"
	MessageTypeClosed = "closed"

	// Error
	MessageTypeError = "error"

	// Flow Control
	MessageTypeFlowControl = "flow_control"
)

// BaseMessage represents the base structure for all socket messages
type BaseMessage struct {
	Type      string                 `json:"type"`
	RequestID string                 `json:"request_id,omitempty"`
	Data      map[string]interface{} `json:"data"`
	Timestamp string                 `json:"timestamp,omitempty"`
	Error     *ErrorInfo             `json:"error,omitempty"`
}

// ErrorInfo contains error details
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// NewMessage creates a new message with the given type and data
func NewMessage(msgType string, data map[string]interface{}) *BaseMessage {
	return &BaseMessage{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// NewRequest creates a new message with a request ID
func NewRequest(msgType string, requestID string, data map[string]interface{}) *BaseMessage {
	return &BaseMessage{
		Type:      msgType,
		RequestID: requestID,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// NewResponse creates a response message for a given request
func NewResponse(msgType string, requestID string, data map[string]interface{}) *BaseMessage {
	return &BaseMessage{
		Type:      msgType,
		RequestID: requestID,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// NewError creates an error response
func NewError(requestID string, errCode string, message string, details string) *BaseMessage {
	return &BaseMessage{
		Type:      MessageTypeError,
		RequestID: requestID,
		Error: &ErrorInfo{
			Code:    errCode,
			Message: message,
			Details: details,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// AuthRequest data for authentication request
type AuthRequest struct {
	ClientType   string   `json:"client_type"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	Token        string   `json:"token,omitempty"`
}

// AuthResponse data for authentication response
type AuthResponse struct {
	Success           bool     `json:"success"`
	ConnectionID      string   `json:"connection_id"`
	ServerVersion     string   `json:"server_version"`
	ServerCapabilities []string `json:"server_capabilities"`
}

// SessionCreateRequest data for session creation
type SessionCreateRequest struct {
	WorkingDir string                 `json:"working_dir,omitempty"`
	Workspace  string                 `json:"workspace,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	Options    map[string]interface{} `json:"options,omitempty"`
}

// SessionCreateResponse data for session creation response
type SessionCreateResponse struct {
	SessionID  string `json:"session_id"`
	Status     string `json:"status"` // "created" or "attached"
	Workspace  string `json:"workspace"`
	WorkingDir string `json:"working_dir"`
}

// SessionAttachRequest data for attaching to a session
type SessionAttachRequest struct {
	SessionID string `json:"session_id"`
}

// SessionListRequest data for listing sessions
type SessionListRequest struct {
	Workspace string `json:"workspace,omitempty"`
}

// SessionInfo represents session information in list response
type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	Workspace    string    `json:"workspace"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	Status       string    `json:"status"` // "active" or "idle"
}

// SessionInfoResponse is the response format for session list (used by client.go)
type SessionInfoResponse struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	WorkingDir   string `json:"working_dir"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	MessageCount int    `json:"message_count"`
}

// SessionListResponse data for session list response
type SessionListResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// SessionDeleteRequest data for deleting a session
type SessionDeleteRequest struct {
	SessionID string `json:"session_id"`
	Workspace string `json:"workspace"`
}

// ChatSendRequest data for sending chat messages
type ChatSendRequest struct {
	Content string                 `json:"content"`
	Prompt  string                 `json:"prompt,omitempty"` // Alias for content
	Options map[string]interface{} `json:"options,omitempty"`
}

// ChatMessage data for streaming chat messages
type ChatMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	StreamID  string `json:"stream_id,omitempty"`
	ChunkIndex int    `json:"chunk_index,omitempty"`
	IsFinal   bool   `json:"is_final,omitempty"`
	Reasoning bool   `json:"reasoning,omitempty"` // Extended thinking content
}

// ToolCallRequest data for tool call notification
type ToolCallRequest struct {
	ToolName    string                 `json:"tool_name"`
	ToolID      string                 `json:"tool_id"`
	Parameters  map[string]interface{} `json:"parameters"`
	Description string                 `json:"description,omitempty"`
}

// ToolResultData data for tool execution result
type ToolResultData struct {
	ToolID string  `json:"tool_id"`
	Result *string `json:"result,omitempty"`
	Error  *string `json:"error,omitempty"`
	Status string  `json:"status,omitempty"` // "completed" or "failed"
}

// ToolCompact data for compact tool interaction
type ToolCompact struct {
	ToolID     string `json:"tool_id"`
	ToolName   string `json:"tool_name"`
	Status     string `json:"status"` // "calling", "completed", "error"
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	Description string `json:"description,omitempty"`
}

// AuthorizationRequestData data for authorization request
type AuthorizationRequestData struct {
	AuthID    string                 `json:"auth_id"`
	ToolName  string                 `json:"tool_name"`
	Parameters map[string]interface{} `json:"parameters"`
	Reason    string                 `json:"reason"`
}

// AuthorizationAck data for authorization acknowledgment
type AuthorizationAck struct {
	AuthID string `json:"auth_id"`
}

// AuthorizationResponseData data for authorization response
type AuthorizationResponseData struct {
	AuthID   string `json:"auth_id"`
	Approved bool   `json:"approved"`
	Ack      bool   `json:"ack,omitempty"`
}

// QuestionRequest data for question requests
type QuestionRequest struct {
	QuestionID  string        `json:"question_id"`
	Question    string        `json:"question"`
	MultiMode   bool          `json:"multi_mode"`
	Questions   []QuestionDef `json:"questions,omitempty"` // Only in multi_mode
}

// QuestionDef defines a question in multi-mode
type QuestionDef struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// QuestionRequestData data for question requests
type QuestionRequestData struct {
	QuestionID string            `json:"question_id"`
	Question   string            `json:"question"`
	MultiMode  bool              `json:"multi_mode"`
	Questions  map[string]string `json:"questions,omitempty"` // question_id -> question text
}

// QuestionResponseData data for question response
type QuestionResponseData struct {
	QuestionID string            `json:"question_id"`
	Answer     string            `json:"answer,omitempty"`
	Answers    map[string]string `json:"answers,omitempty"` // For multi-mode
}

// ProgressData data for progress updates
type ProgressData struct {
	Message            string `json:"message"`
	ContextUsage       int    `json:"context_usage,omitempty"`
	Ephemeral          bool   `json:"ephemeral,omitempty"`
	VerificationAgent  bool   `json:"verification_agent,omitempty"`
	IsCompact          bool   `json:"is_compact,omitempty"`
}

// ConfigGetRequest data for getting configuration
type ConfigGetRequest struct {
	Keys []string `json:"keys"`
}

// ConfigSetRequest data for setting configuration
type ConfigSetRequest struct {
	Values map[string]interface{} `json:"values"`
}

// WorkspaceInfo represents workspace information (for API responses)
type WorkspaceInfo struct {
	ID                string            `json:"id"`
	Path              string            `json:"path"`
	Name              string            `json:"name"`
	RepositoryRoot    string            `json:"repository_root"`
	CurrentBranch     string            `json:"current_branch"`
	IsWorktree        bool              `json:"is_worktree"`
	WorktreeName      string            `json:"worktree_name"`
	SessionCount      int               `json:"session_count"`
	LastAccessed      string            `json:"last_accessed"`
	CreatedAt         string            `json:"created_at"`
	ContextDirs       []string          `json:"context_dirs"`
	LandlockRead      []string          `json:"landlock_read"`
	LandlockWrite     []string          `json:"landlock_write"`
	DomainsApproved   map[string]bool   `json:"domains_approved"`
	CommandsApproved  map[string]bool   `json:"commands_approved"`
}

// WorkspaceListResponse data for workspace list response
type WorkspaceListResponse struct {
	Workspaces []WorkspaceInfo `json:"workspaces"`
}

// WorkspaceSetRequest data for setting workspace
type WorkspaceSetRequest struct {
	Workspace string `json:"workspace"`
}

// WorkspaceCreateRequest data for creating a workspace (e.g., git worktree)
type WorkspaceCreateRequest struct {
	BaseWorkspace string `json:"base_workspace"`
	Name          string `json:"name"`
}

// WorkspaceCreateResponse data for workspace creation response
type WorkspaceCreateResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
	IsWorktree  bool   `json:"is_worktree"`
}

// SessionSaveRequest data for saving session
type SessionSaveRequest struct {
	Name string `json:"name"`
}

// SessionLoadRequest data for loading session
type SessionLoadRequest struct {
	SessionID string `json:"session_id"`
	Workspace string `json:"workspace"`
}

// CloseRequest data for close request
type CloseRequest struct {
	Reason          string `json:"reason"`
	PreserveSession bool   `json:"preserve_session"`
}

// ClosedData data for closed notification
type ClosedData struct {
	Reason    string `json:"reason"`
	Reconnect bool   `json:"reconnect"`
}

// FlowControlData data for flow control
type FlowControlData struct {
	Pause bool `json:"pause"`
}

// Error codes
const (
	ErrorCodeAuthFailed           = "AUTH_FAILED"
	ErrorCodeAuthRequired         = "AUTH_REQUIRED"
	ErrorCodeInvalidRequest       = "INVALID_REQUEST"
	ErrorCodeSessionNotFound      = "SESSION_NOT_FOUND"
	ErrorCodeSessionExists        = "SESSION_EXISTS"
	ErrorCodeWorkspaceInvalid     = "WORKSPACE_INVALID"
	ErrorCodeWorkspaceAccessDenied = "WORKSPACE_ACCESS_DENIED"
	ErrorCodeOperationNotAllowed  = "OPERATION_NOT_ALLOWED"
	ErrorCodeInternalError        = "INTERNAL_ERROR"
	ErrorCodeTimeout              = "TIMEOUT"
	ErrorCodeNotImplemented       = "NOT_IMPLEMENTED"
)