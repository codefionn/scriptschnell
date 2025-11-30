package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolSpec represents the static specification of a tool (name, description, parameters).
// This is used for LLM schema generation and does not require any runtime dependencies.
//
// Design rationale: Separating specification from execution allows:
// - Single spec instance shared across multiple registries (memory efficient)
// - Clear lifecycle: specs are immutable singletons, executors are runtime instances
// - Flexible dependency injection through factories
//
// Example:
//
//	type MyToolSpec struct{}
//	func (s *MyToolSpec) Name() string { return "my_tool" }
//	func (s *MyToolSpec) Description() string { return "Does something" }
//	func (s *MyToolSpec) Parameters() map[string]interface{} { return ... }
type ToolSpec interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
}

// ToolExecutor handles the actual execution of a tool with specific runtime dependencies.
//
// Example:
//
//	type MyToolExecutor struct {
//	    fs      fs.FileSystem
//	    session *session.Session
//	}
//	func (e *MyToolExecutor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
//	    // Use e.fs and e.session
//	}
type ToolExecutor interface {
	Execute(ctx context.Context, params map[string]interface{}) *ToolResult
}

// Tool represents an LLM tool (combines ToolSpec and ToolExecutor for convenience).
// This interface is maintained for backward compatibility with existing tools.
//
// Migration guide: New tools should use ToolSpec + ToolFactory pattern:
//
// Before (legacy):
//
//	type MyTool struct { deps ... }
//	func (t *MyTool) Name() string { ... }
//	func (t *MyTool) Description() string { ... }
//	func (t *MyTool) Parameters() map[string]interface{} { ... }
//	func (t *MyTool) Execute(ctx, params) *ToolResult { ... }
//	registry.Register(NewMyTool(deps))
//
// After (new pattern):
//
//	type MyToolSpec struct{}
//	func (s *MyToolSpec) Name() string { ... }
//	func (s *MyToolSpec) Description() string { ... }
//	func (s *MyToolSpec) Parameters() map[string]interface{} { ... }
//
//	type MyToolExecutor struct { deps ... }
//	func (e *MyToolExecutor) Execute(ctx, params) *ToolResult { ... }
//
//	func NewMyToolFactory(deps ...) ToolFactory {
//	    return func(reg *Registry) ToolExecutor {
//	        return &MyToolExecutor{deps: deps}
//	    }
//	}
//	registry.RegisterSpec(&MyToolSpec{}, NewMyToolFactory(deps))
type Tool interface {
	ToolSpec
	ToolExecutor
}

// ToolFactory creates tool executors with specific runtime dependencies.
// This allows the same tool spec to be instantiated with different dependencies.
//
// The factory receives the registry as a parameter, enabling tools like parallel_tools
// to access other registered tools.
//
// Example:
//
//	func NewMyToolFactory(fs fs.FileSystem, sess *session.Session) ToolFactory {
//	    return func(reg *Registry) ToolExecutor {
//	        return &MyToolExecutor{fs: fs, session: sess}
//	    }
//	}
type ToolFactory func(registry *Registry) ToolExecutor

// LegacyToolSpec wraps a legacy Tool as a ToolSpec for migration purposes
type LegacyToolSpec struct {
	tool Tool
}

func (s *LegacyToolSpec) Name() string                       { return s.tool.Name() }
func (s *LegacyToolSpec) Description() string                { return s.tool.Description() }
func (s *LegacyToolSpec) Parameters() map[string]interface{} { return s.tool.Parameters() }

// WrapLegacyTool creates a spec and factory from a legacy Tool
func WrapLegacyTool(tool Tool) (ToolSpec, ToolFactory) {
	spec := &LegacyToolSpec{tool: tool}
	factory := func(reg *Registry) ToolExecutor {
		return tool
	}
	return spec, factory
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

	// Dual response support: separate responses for LLM and UI
	// If UIResult is set, it will be used for UI display instead of Result
	// The LLM will always receive Result in its messages
	UIResult interface{} `json:"ui_result,omitempty"`
}

// ExecutionMetadata captures detailed information about tool execution
type ExecutionMetadata struct {
	// Timing information
	StartTime  *time.Time `json:"start_time,omitempty"`
	EndTime    *time.Time `json:"end_time,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`

	// Command/process information (for shell, sandbox, etc.)
	Command   string `json:"command,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	PID       int    `json:"pid,omitempty"`
	ProcessID string `json:"process_id,omitempty"` // For background jobs

	// Output statistics
	OutputSizeBytes int  `json:"output_size_bytes,omitempty"`
	OutputLineCount int  `json:"output_line_count,omitempty"`
	HasStderr       bool `json:"has_stderr,omitempty"`
	StderrSizeBytes int  `json:"stderr_size_bytes,omitempty"`
	StderrLineCount int  `json:"stderr_line_count,omitempty"`

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

// registryEntry holds either a Tool (legacy) or a ToolSpec + ToolExecutor pair
type registryEntry struct {
	// Legacy: full tool implementation
	tool Tool
	// New: separated spec and executor
	spec     ToolSpec
	executor ToolExecutor
}

func (e *registryEntry) getSpec() ToolSpec {
	if e.spec != nil {
		return e.spec
	}
	return e.tool
}

func (e *registryEntry) getExecutor() ToolExecutor {
	if e.executor != nil {
		return e.executor
	}
	return e.tool
}

// Registry manages available tools
type Registry struct {
	entries    map[string]*registryEntry
	authorizer Authorizer
}

// NewRegistry creates a new tool registry with an optional authorizer
func NewRegistry(authorizer Authorizer) *Registry {
	return &Registry{
		entries:    make(map[string]*registryEntry),
		authorizer: authorizer,
	}
}

// Register adds a tool to the registry (legacy method for backward compatibility)
func (r *Registry) Register(tool Tool) {
	r.entries[tool.Name()] = &registryEntry{tool: tool}
}

// RegisterSpec adds a tool spec with a factory to the registry
func (r *Registry) RegisterSpec(spec ToolSpec, factory ToolFactory) {
	executor := factory(r)
	r.entries[spec.Name()] = &registryEntry{
		spec:     spec,
		executor: executor,
	}
}

// RemoveByPrefix unregisters tools whose names share the provided prefix.
func (r *Registry) RemoveByPrefix(prefix string) {
	for name := range r.entries {
		if strings.HasPrefix(name, prefix) {
			delete(r.entries, name)
		}
	}
}

// Get retrieves a tool by name (legacy method - returns nil if tool uses new spec/executor pattern)
func (r *Registry) Get(name string) (Tool, bool) {
	entry, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	return entry.tool, entry.tool != nil
}

// GetExecutor retrieves a tool executor by name
func (r *Registry) GetExecutor(name string) (ToolExecutor, bool) {
	entry, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	return entry.getExecutor(), true
}

// List returns all registered tools (legacy method - only returns tools using old interface)
func (r *Registry) List() []Tool {
	result := make([]Tool, 0, len(r.entries))
	for _, entry := range r.entries {
		if entry.tool != nil {
			result = append(result, entry.tool)
		}
	}
	return result
}

// ListSpecs returns all registered tool specs
func (r *Registry) ListSpecs() []ToolSpec {
	result := make([]ToolSpec, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, entry.getSpec())
	}
	return result
}

// ExecuteWithApproval executes a tool call, bypassing authorization (used when user has manually approved)
func (r *Registry) ExecuteWithApproval(ctx context.Context, call *ToolCall) *ToolResult {
	entry, ok := r.entries[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	executor := entry.getExecutor()
	if executor == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool executor not available: " + call.Name,
		}
	}

	// Skip authorization check - user has already approved

	result := executor.Execute(ctx, call.Parameters)
	if result == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool returned nil result",
		}
	}

	result.ID = call.ID
	return result
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
	entry, ok := r.entries[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	executor := entry.getExecutor()
	if executor == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool executor not available: " + call.Name,
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

	result := executor.Execute(ctx, call.Parameters)
	if result == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool returned nil result",
		}
	}

	result.ID = call.ID
	return result
}

// ExecuteWithACPSupport executes a tool call with ACP callback support
func (r *Registry) ExecuteWithACPSupport(ctx context.Context, call *ToolCall, toolName string, statusCb func(string) error, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error) *ToolResult {
	entry, ok := r.entries[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	executor := entry.getExecutor()
	if executor == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool executor not available: " + call.Name,
		}
	}

	// Handle authorization (same as regular Execute)
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
				return &ToolResult{
					ID:                     call.ID,
					RequiresUserInput:      true,
					AuthReason:             decision.Reason,
					SuggestedCommandPrefix: decision.SuggestedCommandPrefix,
				}
			}
			return &ToolResult{
				ID:                     call.ID,
				Error:                  decision.Reason,
				SuggestedCommandPrefix: decision.SuggestedCommandPrefix,
			}
		}
	}

	// Try to use ACP-aware execution if the tool supports it
	if acpTool, ok := executor.(interface {
		ExecuteWithACPSupport(ctx context.Context, params map[string]interface{}, statusCb func(string) error, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error) *ToolResult
	}); ok && toolCallCb != nil && toolResultCb != nil {
		result := acpTool.ExecuteWithACPSupport(ctx, call.Parameters, statusCb, toolCallCb, toolResultCb)
		if result == nil {
			return &ToolResult{
				ID:    call.ID,
				Error: "tool returned nil result",
			}
		}
		result.ID = call.ID
		return result
	}

	// Fall back to regular execution
	result := executor.Execute(ctx, call.Parameters)
	if result == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool returned nil result",
		}
	}
	result.ID = call.ID
	return result
}

// ExecuteWithACPSupportAndApproval executes a tool call with ACP callbacks, bypassing authorization
func (r *Registry) ExecuteWithACPSupportAndApproval(ctx context.Context, call *ToolCall, toolName string, statusCb func(string) error, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error) *ToolResult {
	entry, ok := r.entries[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool not found: " + call.Name,
		}
	}

	executor := entry.getExecutor()
	if executor == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool executor not available: " + call.Name,
		}
	}

	// Try to use ACP-aware execution if the tool supports it
	if acpTool, ok := executor.(interface {
		ExecuteWithACPSupport(ctx context.Context, params map[string]interface{}, statusCb func(string) error, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error) *ToolResult
	}); ok && toolCallCb != nil && toolResultCb != nil {
		result := acpTool.ExecuteWithACPSupport(ctx, call.Parameters, statusCb, toolCallCb, toolResultCb)
		if result == nil {
			return &ToolResult{
				ID:    call.ID,
				Error: "tool returned nil result",
			}
		}
		result.ID = call.ID
		return result
	}

	// Fall back to regular execution
	result := executor.Execute(ctx, call.Parameters)
	if result == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool returned nil result",
		}
	}
	result.ID = call.ID
	return result
}

// ToJSONSchema converts tools to JSON schema format for LLM
func (r *Registry) ToJSONSchema() []map[string]interface{} {
	schemas := make([]map[string]interface{}, 0, len(r.entries))
	for _, entry := range r.entries {
		spec := entry.getSpec()
		if spec == nil {
			continue
		}
		schemas = append(schemas, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        spec.Name(),
				"description": spec.Description(),
				"parameters":  spec.Parameters(),
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
