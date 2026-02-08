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
	MessageTypeStop             = "stop"
	MessageTypeGetConfig        = "get_config"
	MessageTypeConfig           = "config"
	MessageTypeToolInteraction  = "tool_interaction" // Compact tool call + result

	// Authorization message types
	MessageTypeAuthorizationRequest  = "authorization_request"
	MessageTypeAuthorizationResponse = "authorization_response"
	MessageTypeAuthorizationAck      = "authorization_ack"

	// Question dialog message types
	MessageTypeQuestionRequest  = "question_request"
	MessageTypeQuestionResponse = "question_response"

	// Menu-related message types
	MessageTypeGetProviders    = "get_providers"
	MessageTypeProviders       = "providers"
	MessageTypeAddProvider     = "add_provider"
	MessageTypeUpdateProvider  = "update_provider"
	MessageTypeDeleteProvider  = "delete_provider"
	MessageTypeGetModels       = "get_models"
	MessageTypeModels          = "models"
	MessageTypeSetModel        = "set_model"
	MessageTypeGetSearchConfig = "get_search_config"
	MessageTypeSearchConfig    = "search_config"
	MessageTypeSetSearchConfig = "set_search_config"
	MessageTypeSetPassword     = "set_password"
	MessageTypePasswordStatus  = "password_status"
	MessageTypeGetMCPServers   = "get_mcp_servers"
	MessageTypeMCPServers      = "mcp_servers"
	MessageTypeAddMCPServer    = "add_mcp_server"
	MessageTypeToggleMCPServer = "toggle_mcp_server"
	MessageTypeDeleteMCPServer = "delete_mcp_server"
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
	Status      string `json:"status,omitempty"`      // "calling", "completed", "error"
	Compact     bool   `json:"compact,omitempty"`     // Whether this is a compact message
	Description string `json:"description,omitempty"` // Human-readable description of what the tool is doing

	// For authorization requests
	AuthID   string `json:"auth_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Approved *bool  `json:"approved,omitempty"` // pointer to distinguish false from not set

	// For question dialogs
	QuestionID string            `json:"question_id,omitempty"`
	Question   string            `json:"question,omitempty"`    // Single question text
	Questions  []QuestionItem    `json:"questions,omitempty"`   // Multiple questions
	Answer     string            `json:"answer,omitempty"`      // Single answer
	Answers    []string          `json:"answers,omitempty"`     // Multiple answers (array format)
	AnswersMap map[string]string `json:"answers_map,omitempty"` // Multiple answers (map format: question_id -> answer)
	MultiMode  bool              `json:"multi_mode,omitempty"`  // Whether this is a multi-question request
}

// QuestionItem represents a question with options for the question dialog
type QuestionItem struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// SessionInfo represents session information
type SessionInfo struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"created_at"`
	MessageCount int       `json:"message_count"`
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

// ProviderInfo represents provider information
type ProviderInfo struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	APIKey      string         `json:"api_key,omitempty"`
	BaseURL     string         `json:"base_url,omitempty"`
	ModelCount  int            `json:"model_count"`
	RateLimit   *RateLimitInfo `json:"rate_limit,omitempty"`
}

// RateLimitInfo represents rate limit information
type RateLimitInfo struct {
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`
	MinIntervalMillis int `json:"min_interval_millis,omitempty"`
	TokensPerMinute   int `json:"tokens_per_minute,omitempty"`
}

// ModelInfo represents model information
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Description string `json:"description,omitempty"`
}

// SearchConfigInfo represents search configuration information
type SearchConfigInfo struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key,omitempty"`
}

// MCPServerInfo represents MCP server information
type MCPServerInfo struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Description string                 `json:"description,omitempty"`
	Disabled    bool                   `json:"disabled"`
	Config      map[string]interface{} `json:"config,omitempty"`
}
