package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Tool represents an LLM tool
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// ToolCall represents a tool call from the LLM
type ToolCall struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Parameters map[string]interface{} `json:"parameters"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ID                     string      `json:"id"`
	Result                 interface{} `json:"result"`
	Error                  string      `json:"error,omitempty"`
	RequiresUserInput      bool        `json:"requires_user_input,omitempty"`      // If true, user approval is needed
	AuthReason             string      `json:"auth_reason,omitempty"`              // Reason for requiring authorization
	SuggestedCommandPrefix string      `json:"suggested_command_prefix,omitempty"` // Suggested prefix to remember for future use
	
	// Enhanced execution metadata for better summaries and diagnostics
	ExecutionMetadata *ExecutionMetadata `json:"execution_metadata,omitempty"`
}

// ExecutionMetadata captures detailed information about tool execution
type ExecutionMetadata struct {
	// Timing information
	StartTime   *time.Time `json:"start_time,omitempty"`
	EndTime     *time.Time `json:"end_time,omitempty"`
	DurationMs  int64      `json:"duration_ms,omitempty"`
	
	// Command/process information (for shell, sandbox, etc.)
	Command     string `json:"command,omitempty"`
	ExitCode    int    `json:"exit_code,omitempty"`
	PID         int    `json:"pid,omitempty"`
	ProcessID   string `json:"process_id,omitempty"` // For background jobs
	
	// Output statistics
	OutputSizeBytes int    `json:"output_size_bytes,omitempty"`
	OutputLineCount  int    `json:"output_line_count,omitempty"`
	HasStderr        bool   `json:"has_stderr,omitempty"`
	StderrSizeBytes  int    `json:"stderr_size_bytes,omitempty"`
	StderrLineCount  int    `json:"stderr_line_count,omitempty"`
	
	// Execution context
	WorkingDir      string `json:"working_dir,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
	WasTimedOut     bool   `json:"was_timed_out,omitempty"`
	WasBackgrounded bool   `json:"was_backgrounded,omitempty"`
	
	// Tool-specific metadata
	ToolType string                 `json:"tool_type,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
	
	// Error classification
	ErrorType    string `json:"error_type,omitempty"`    // "timeout", "permission", "not_found", "syntax", etc.
	ErrorContext string `json:"error_context,omitempty"` // Additional context for the error
}

// Registry manages available tools
type Registry struct {
	tools      map[string]Tool
	authorizer Authorizer
}

// NewRegistry creates a new tool registry with an optional authorizer
func NewRegistry(authorizer Authorizer) *Registry {
	return &Registry{
		tools:      make(map[string]Tool),
		authorizer: authorizer,
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// RemoveByPrefix unregisters tools whose names share the provided prefix.
func (r *Registry) RemoveByPrefix(prefix string) {
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
		}
	}
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// List returns all registered tools
func (r *Registry) List() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		result = append(result, tool)
	}
	return result
}

// ExecuteWithApproval executes a tool call, bypassing authorization (used when user has manually approved)
func (r *Registry) ExecuteWithApproval(ctx context.Context, call *ToolCall) *ToolResult {
	tool, ok := r.tools[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	// Skip authorization check - user has already approved

	result, err := tool.Execute(ctx, call.Parameters)
	if err != nil {
		return &ToolResult{
			ID:    call.ID,
			Error: err.Error(),
		}
	}

	return &ToolResult{
		ID:     call.ID,
		Result: result,
	}
}

// NewToolResultWithMetadata creates a ToolResult with execution metadata
func NewToolResultWithMetadata(id string, result interface{}, err error, metadata *ExecutionMetadata) *ToolResult {
	toolResult := &ToolResult{
		ID:                id,
		Result:            result,
		ExecutionMetadata: metadata,
	}
	
	if err != nil {
		toolResult.Error = err.Error()
		// Classify error type if metadata is available
		if metadata != nil {
			metadata.ErrorType = classifyError(err)
			metadata.ErrorContext = extractErrorContext(err)
		}
	}
	
	return toolResult
}

// EnhanceToolResult adds metadata to an existing ToolResult
func EnhanceToolResult(result *ToolResult, metadata *ExecutionMetadata) *ToolResult {
	if result == nil {
		return nil
	}
	result.ExecutionMetadata = metadata
	
	// If there's an error and no error classification yet, add it
	if result.Error != "" && metadata != nil && metadata.ErrorType == "" {
		metadata.ErrorType = classifyErrorString(result.Error)
	}
	
	return result
}

// classifyError attempts to categorize errors for better summaries
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	
	errStr := strings.ToLower(err.Error())
	
	switch {
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline"):
		return "timeout"
	case strings.Contains(errStr, "permission denied") || strings.Contains(errStr, "access denied"):
		return "permission"
	case strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file"):
		return "not_found"
	case strings.Contains(errStr, "syntax") || strings.Contains(errStr, "parse"):
		return "syntax"
	case strings.Contains(errStr, "network") || strings.Contains(errStr, "connection"):
		return "network"
	case strings.Contains(errStr, "exit status") || strings.Contains(errStr, "exit code"):
		return "process_exit"
	default:
		return "unknown"
	}
}

// classifyErrorString classifies errors from string messages
func classifyErrorString(errStr string) string {
	return classifyError(fmt.Errorf("%s", errStr))
}

// extractErrorContext pulls relevant context from error messages
func extractErrorContext(err error) string {
	if err == nil {
		return ""
	}
	
	errStr := err.Error()
	
	// Extract file paths from error messages
	if idx := strings.Index(errStr, ": "); idx > 0 {
		context := errStr[:idx]
		// If it looks like a file path, use it as context
		if strings.Contains(context, "/") || strings.Contains(context, "\\") {
			return context
		}
	}
	
	// Extract command names from shell errors
	if strings.Contains(strings.ToLower(errStr), "command") {
		words := strings.Fields(errStr)
		for i, word := range words {
			if strings.ToLower(word) == "command" && i+1 < len(words) {
				return strings.Trim(words[i+1], `"'`)
			}
		}
	}
	
	return ""
}

// CalculateOutputStats computes statistics for output content
func CalculateOutputStats(content string) (bytes int, lines int) {
	if content == "" {
		return 0, 0
	}
	bytes = len(content)
	lines = strings.Count(content, "\n") + 1
	return bytes, lines
}

// Execute executes a tool call
func (r *Registry) Execute(ctx context.Context, call *ToolCall) *ToolResult {
	tool, ok := r.tools[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	if r.authorizer != nil {
		decision, err := r.authorizer.Authorize(ctx, call.Name, call.Parameters)
		if err != nil {
			return &ToolResult{
				ID:    call.ID,
				Error: "authorization error: " + err.Error(),
			}
		}

		if decision != nil && !decision.Allowed {
			if decision.RequiresUserInput {
				// Signal that user approval is needed
				return &ToolResult{
					ID:                     call.ID,
					RequiresUserInput:      true,
					AuthReason:             decision.Reason,
					SuggestedCommandPrefix: decision.SuggestedCommandPrefix,
				}
			}
			// Hard denial (no user input option)
			return &ToolResult{
				ID:                     call.ID,
				Error:                  decision.Reason,
				SuggestedCommandPrefix: decision.SuggestedCommandPrefix,
			}
		}
	}

	result, err := tool.Execute(ctx, call.Parameters)
	if err != nil {
		return &ToolResult{
			ID:    call.ID,
			Error: err.Error(),
		}
	}

	return &ToolResult{
		ID:     call.ID,
		Result: result,
	}
}

// ToJSONSchema converts tools to JSON schema format for LLM
func (r *Registry) ToJSONSchema() []map[string]interface{} {
	schemas := make([]map[string]interface{}, 0, len(r.tools))
	for _, tool := range r.tools {
		schemas = append(schemas, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters":  tool.Parameters(),
			},
		})
	}
	return schemas
}

// Helper function to get string parameter
func GetStringParam(params map[string]interface{}, key string, defaultVal string) string {
	if val, ok := params[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

// Helper function to get int parameter
func GetIntParam(params map[string]interface{}, key string, defaultVal int) int {
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case json.Number:
			if i, err := v.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultVal
}

// Helper function to get bool parameter
func GetBoolParam(params map[string]interface{}, key string, defaultVal bool) bool {
	if val, ok := params[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return defaultVal
}
