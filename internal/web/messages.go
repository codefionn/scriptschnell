package web

import "time"

// Message types
const (
	MessageTypeChat             = "chat"
	MessageTypeToolCall         = "tool_call"
	MessageTypeToolResult       = "tool_result"
	MessageTypeError            = "error"
	MessageTypeSystem           = "system"
	MessageTypePlanningQuestion = "planning_question"
	MessageTypeClear            = "clear"
	MessageTypeGetConfig        = "get_config"
	MessageTypeConfig           = "config"
	MessageTypeToolInteraction  = "tool_interaction" // Compact tool call + result
)

// WebMessage represents a message sent over WebSocket
type WebMessage struct {
	Type       string                 `json:"type"`
	Role       string                 `json:"role,omitempty"`
	Content    string                 `json:"content,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolID     string                 `json:"tool_id,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Result     interface{}            `json:"result,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Timestamp  time.Time              `json:"timestamp,omitempty"`
	// For compact tool interactions
	Status     string                 `json:"status,omitempty"` // "calling", "completed", "error"
	Compact    bool                   `json:"compact,omitempty"`  // Whether this is a compact message
}

// SessionInfo represents session information
type SessionInfo struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"created_at"`
	MessageCount int       `json:"message_count"`
}

// ModelInfo represents model information
type ModelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// ConfigInfo represents configuration information
type ConfigInfo struct {
	WorkingDir string    `json:"working_dir"`
	Model      ModelInfo `json:"model"`
}

// AuthStatus represents authentication status
type AuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Token         string `json:"token,omitempty"`
}
