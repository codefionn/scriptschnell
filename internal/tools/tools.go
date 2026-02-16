package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
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

	// Adaptive timeout metrics (for go_sandbox)
	AdaptiveTimeoutOriginalSeconds int     `json:"adaptive_timeout_original_seconds,omitempty"` // Original configured timeout
	AdaptiveTimeoutExtensions      int     `json:"adaptive_timeout_extensions,omitempty"`       // Number of times timeout was extended
	AdaptiveTimeoutTotalSeconds    float64 `json:"adaptive_timeout_total_seconds,omitempty"`    // Total timeout after extensions
	AdaptiveTimeoutMaxExtensions   int     `json:"adaptive_timeout_max_extensions,omitempty"`   // Maximum allowed extensions

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
	spec      ToolSpec
	executor  ToolExecutor
	exclusive bool
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

func requiresExclusive(spec ToolSpec) bool {
	if ex, ok := spec.(exclusiveToolSpec); ok {
		return ex.RequiresExclusiveExecution()
	}
	return false
}

// ManualSuggestion represents a manual override for tool suggestions
type ManualSuggestion struct {
	// SuggestedTools is a list of tool names to suggest instead
	SuggestedTools []string
	// Reason explains why the original tool shouldn't be used
	Reason string
	// MatchPattern determines how to match (exact, prefix, contains)
	MatchPattern string
}

// Registry manages available tools
type Registry struct {
	entries           map[string]*registryEntry
	authorizer        Authorizer
	manualSuggestions map[string]*ManualSuggestion
	writeMu           sync.Mutex
	secretDetector    secretdetect.Detector
}

// NewRegistry creates a new tool registry with an optional authorizer
func NewRegistry(authorizer Authorizer) *Registry {
	r := &Registry{
		entries:           make(map[string]*registryEntry),
		authorizer:        authorizer,
		manualSuggestions: make(map[string]*ManualSuggestion),
	}
	r.initializeDefaultSuggestions()
	return r
}

// NewRegistryWithSecrets creates a new tool registry with authorizer and secret detector
func NewRegistryWithSecrets(authorizer Authorizer, detector secretdetect.Detector) *Registry {
	r := &Registry{
		entries:           make(map[string]*registryEntry),
		authorizer:        authorizer,
		manualSuggestions: make(map[string]*ManualSuggestion),
		secretDetector:    detector,
	}
	r.initializeDefaultSuggestions()
	return r
}

// SetSecretDetector sets the secret detector for the registry
func (r *Registry) SetSecretDetector(detector secretdetect.Detector) {
	r.secretDetector = detector
}

type exclusiveToolSpec interface {
	RequiresExclusiveExecution() bool
}

// executeWithWriteLock serializes tool executions that mutate files to avoid concurrent writes.
func (r *Registry) executeWithWriteLock(exclusive bool, fn func() *ToolResult) *ToolResult {
	if exclusive {
		r.writeMu.Lock()
		defer r.writeMu.Unlock()
	}
	return fn()
}

// initializeDefaultSuggestions populates the registry with common manual suggestions
func (r *Registry) initializeDefaultSuggestions() {
	// Shell-related commands - suggest using go_sandbox instead
	r.AddManualSuggestion("shell", &ManualSuggestion{
		SuggestedTools: []string{"go_sandbox"},
		Reason:         "For executing code, use 'go_sandbox' which provides a safe, sandboxed environment. Use 'shell' only for system commands like 'ls', 'git', 'go build', etc.",
		MatchPattern:   "exact",
	})

	// Common variations of shell commands
	r.AddManualSuggestion("bash", &ManualSuggestion{
		SuggestedTools: []string{"shell", "go_sandbox"},
		Reason:         "This tool doesn't exist. Use 'shell' for system commands or 'go_sandbox' for executing code safely",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("sh", &ManualSuggestion{
		SuggestedTools: []string{"shell", "go_sandbox"},
		Reason:         "This tool doesn't exist. Use 'shell' for system commands or 'go_sandbox' for executing code safely",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("exec", &ManualSuggestion{
		SuggestedTools: []string{"shell", "go_sandbox"},
		Reason:         "This tool doesn't exist. Use 'shell' for system commands or 'go_sandbox' for safe code execution",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("execute", &ManualSuggestion{
		SuggestedTools: []string{"shell", "go_sandbox"},
		Reason:         "This tool doesn't exist. Use 'shell' for system commands or 'go_sandbox' for safe code execution",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("run", &ManualSuggestion{
		SuggestedTools: []string{"shell", "go_sandbox"},
		Reason:         "This tool doesn't exist. Use 'shell' for system commands or 'go_sandbox' for safe code execution",
		MatchPattern:   "exact",
	})

	// Python execution
	r.AddManualSuggestion("python", &ManualSuggestion{
		SuggestedTools: []string{"go_sandbox"},
		Reason:         "Python execution is not directly supported. Use 'go_sandbox' for safe code execution in Go, or 'shell' to run python commands",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("python3", &ManualSuggestion{
		SuggestedTools: []string{"go_sandbox"},
		Reason:         "Python execution is not directly supported. Use 'go_sandbox' for safe code execution in Go, or 'shell' to run python commands",
		MatchPattern:   "exact",
	})

	// File operations - guide to correct tools
	r.AddManualSuggestion("edit_file", &ManualSuggestion{
		SuggestedTools: []string{"edit_file"},
		Reason:         "To modify files, use 'edit_file' which applies unified diff patches to existing files",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("modify_file", &ManualSuggestion{
		SuggestedTools: []string{"edit_file"},
		Reason:         "To modify files, use 'write_file_diff' which applies unified diff patches to existing files",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("update_file", &ManualSuggestion{
		SuggestedTools: []string{"edit_file"},
		Reason:         "To update files, use 'write_file_diff' which applies unified diff patches to existing files",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("patch_file", &ManualSuggestion{
		SuggestedTools: []string{"edit_file"},
		Reason:         "To patch files, use 'write_file_diff' which applies unified diff patches",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("write_file", &ManualSuggestion{
		SuggestedTools: []string{"create_file", "edit_file"},
		Reason:         "Use 'create_file' for new files, 'replace_file' to replace entire file content, or 'write_file_diff' to modify existing files with diffs",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("overwrite_file", &ManualSuggestion{
		SuggestedTools: []string{"replace_file"},
		Reason:         "Use 'replace_file' to replace the entire content of an existing file",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("rewrite_file", &ManualSuggestion{
		SuggestedTools: []string{"replace_file"},
		Reason:         "Use 'replace_file' to replace the entire content of an existing file",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("delete_file", &ManualSuggestion{
		SuggestedTools: []string{"shell"},
		Reason:         "File deletion is not directly supported. Use 'shell' with 'rm' command if necessary",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("remove_file", &ManualSuggestion{
		SuggestedTools: []string{"shell"},
		Reason:         "File removal is not directly supported. Use 'shell' with 'rm' command if necessary",
		MatchPattern:   "exact",
	})

	// List/browse files
	r.AddManualSuggestion("list_files", &ManualSuggestion{
		SuggestedTools: []string{"shell"},
		Reason:         "Use 'shell' with commands like 'ls', 'find', or 'tree' to list files",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("ls", &ManualSuggestion{
		SuggestedTools: []string{"shell"},
		Reason:         "Use 'shell' to run 'ls' and other directory listing commands",
		MatchPattern:   "exact",
	})

	// Summarization
	r.AddManualSuggestion("summarize_file", &ManualSuggestion{
		SuggestedTools: []string{"read_file_summarized"},
		Reason:         "Use 'read_file_summarized' to get AI-powered summaries of large files",
		MatchPattern:   "exact",
	})

	// Task management
	r.AddManualSuggestion("add_todo", &ManualSuggestion{
		SuggestedTools: []string{"todo"},
		Reason:         "Use 'todo' to manage todo items",
		MatchPattern:   "exact",
	})

	r.AddManualSuggestion("task", &ManualSuggestion{
		SuggestedTools: []string{"todo"},
		Reason:         "Use 'todo' to manage tasks and todo items",
		MatchPattern:   "exact",
	})
}

// AddManualSuggestion adds a manual suggestion for a specific tool name pattern
func (r *Registry) AddManualSuggestion(pattern string, suggestion *ManualSuggestion) {
	r.manualSuggestions[strings.ToLower(pattern)] = suggestion
}

// Register adds a tool to the registry (legacy method for backward compatibility)
func (r *Registry) Register(tool Tool) {
	r.entries[tool.Name()] = &registryEntry{
		tool:      tool,
		exclusive: requiresExclusive(tool),
	}
}

// RegisterSpec adds a tool spec with a factory to the registry
func (r *Registry) RegisterSpec(spec ToolSpec, factory ToolFactory) {
	executor := factory(r)
	r.entries[spec.Name()] = &registryEntry{
		spec:      spec,
		executor:  executor,
		exclusive: requiresExclusive(spec),
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
			Error: r.FormatToolNotFoundError(call.Name),
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

	result := r.executeWithWriteLock(entry.exclusive, func() *ToolResult {
		return executor.Execute(ctx, call.Parameters)
	})
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

// Execute executes a tool call
func (r *Registry) Execute(ctx context.Context, call *ToolCall) *ToolResult {
	entry, ok := r.entries[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: r.FormatToolNotFoundError(call.Name),
		}
	}

	executor := entry.getExecutor()
	if executor == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool executor not available: " + call.Name,
		}
	}

	// Secret-based authorization: Scan parameters for secrets before normal authorization
	if r.secretDetector != nil && shouldScanTool(call.Name) {
		paramStr := paramsToString(call.Parameters)
		if secrets := extractSecrets(r.secretDetector, paramStr); len(secrets) > 0 {
			// If we have secrets detected and an authorizer that supports secret judgment
			if secretAwareAuthorizer, ok := r.authorizer.(*SecretAwareAuthorizer); ok {
				decision, err := secretAwareAuthorizer.AuthorizeWithSecrets(ctx, call.Name, call.Parameters, secrets)
				if err != nil {
					return &ToolResult{
						ID:    call.ID,
						Error: "secret-based authorization error: " + err.Error(),
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

	result := r.executeWithWriteLock(entry.exclusive, func() *ToolResult {
		return executor.Execute(ctx, call.Parameters)
	})
	if result == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool returned nil result",
		}
	}

	result.ID = call.ID
	return result
}

// ExecuteWithCallbacks executes a tool call with optional callbacks and optional authorization skipping.
func (r *Registry) ExecuteWithCallbacks(ctx context.Context, call *ToolCall, toolName string, progressCb progress.Callback, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error, skipAuthorization bool) *ToolResult {
	entry, ok := r.entries[call.Name]
	if !ok {
		return &ToolResult{
			ID:    call.ID,
			Error: r.FormatToolNotFoundError(call.Name),
		}
	}

	executor := entry.getExecutor()
	if executor == nil {
		return &ToolResult{
			ID:    call.ID,
			Error: "tool executor not available: " + call.Name,
		}
	}

	if !skipAuthorization && r.authorizer != nil {
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

	// Allow tools to consume callbacks if they support it
	if cbTool, ok := executor.(interface {
		ExecuteWithCallbacks(ctx context.Context, params map[string]interface{}, progressCb progress.Callback, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error) *ToolResult
	}); ok {
		result := r.executeWithWriteLock(entry.exclusive, func() *ToolResult {
			return cbTool.ExecuteWithCallbacks(ctx, call.Parameters, progressCb, toolCallCb, toolResultCb)
		})
		if result == nil {
			return &ToolResult{
				ID:    call.ID,
				Error: "tool returned nil result",
			}
		}
		result.ID = call.ID
		return result
	}

	// Fallback to regular execution
	result := r.executeWithWriteLock(entry.exclusive, func() *ToolResult {
		return executor.Execute(ctx, call.Parameters)
	})
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

// levenshteinDistance computes the Levenshtein distance between two strings.
// This measures the minimum number of single-character edits (insertions, deletions, or substitutions)
// required to change one string into the other.
func levenshteinDistance(s1, s2 string) int {
	len1, len2 := len(s1), len(s2)

	// Early exit for identical strings
	if s1 == s2 {
		return 0
	}

	// Early exit for empty strings
	if len1 == 0 {
		return len2
	}
	if len2 == 0 {
		return len1
	}

	// Create distance matrix (we only need two rows for optimization)
	prevRow := make([]int, len2+1)
	currRow := make([]int, len2+1)

	// Initialize first row
	for j := 0; j <= len2; j++ {
		prevRow[j] = j
	}

	// Compute distances
	for i := 1; i <= len1; i++ {
		currRow[0] = i
		for j := 1; j <= len2; j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}

			// Minimum of deletion, insertion, substitution
			currRow[j] = min(
				prevRow[j]+1,      // deletion
				currRow[j-1]+1,    // insertion
				prevRow[j-1]+cost, // substitution
			)
		}
		prevRow, currRow = currRow, prevRow
	}

	return prevRow[len2]
}

// min returns the minimum of three integers
func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// toolSuggestion represents a tool name with its similarity score
type toolSuggestion struct {
	name     string
	distance int
}

// findSimilarTools finds tool names similar to the given name using Levenshtein distance.
// Returns up to maxSuggestions tools, sorted by similarity (closest first).
// Only includes tools with distance <= maxDistance.
func (r *Registry) findSimilarTools(targetName string, maxSuggestions int, maxDistance int) []string {
	if len(r.entries) == 0 {
		return nil
	}

	suggestions := make([]toolSuggestion, 0, len(r.entries))
	targetLower := strings.ToLower(targetName)

	// Compute distances for all registered tools
	for name := range r.entries {
		nameLower := strings.ToLower(name)
		distance := levenshteinDistance(targetLower, nameLower)

		// Only include if within max distance threshold
		if distance <= maxDistance {
			suggestions = append(suggestions, toolSuggestion{
				name:     name,
				distance: distance,
			})
		}
	}

	// Sort by distance (closest first)
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].distance < suggestions[j].distance
	})

	// Return up to maxSuggestions
	limit := maxSuggestions
	if limit > len(suggestions) {
		limit = len(suggestions)
	}

	result := make([]string, limit)
	for i := 0; i < limit; i++ {
		result[i] = suggestions[i].name
	}

	return result
}

// FormatToolNotFoundError creates a detailed error message when a tool is not found,
// including suggestions for similar tool names if available.
func (r *Registry) FormatToolNotFoundError(toolName string) string {
	baseError := "tool not found: " + toolName

	// First, check for manual suggestions (these take priority over automatic similarity matching)
	if manualSugg := r.findManualSuggestion(toolName); manualSugg != nil {
		return r.formatManualSuggestion(baseError, manualSugg)
	}

	// Fall back to automatic similarity matching
	similar := r.findSimilarTools(toolName, 3, 5)

	if len(similar) == 0 {
		return baseError
	}

	// Format suggestions
	if len(similar) == 1 {
		return fmt.Sprintf("%s. Did you mean '%s'?", baseError, similar[0])
	}

	// Multiple suggestions
	suggestions := strings.Join(similar, "', '")
	return fmt.Sprintf("%s. Did you mean one of: '%s'?", baseError, suggestions)
}

// findManualSuggestion looks up a manual suggestion for the given tool name
func (r *Registry) findManualSuggestion(toolName string) *ManualSuggestion {
	nameLower := strings.ToLower(toolName)

	// Check exact match first
	if sugg, ok := r.manualSuggestions[nameLower]; ok {
		return sugg
	}

	// Check for pattern matches (prefix, contains, etc.)
	for pattern, sugg := range r.manualSuggestions {
		switch sugg.MatchPattern {
		case "prefix":
			if strings.HasPrefix(nameLower, pattern) {
				return sugg
			}
		case "suffix":
			if strings.HasSuffix(nameLower, pattern) {
				return sugg
			}
		case "contains":
			if strings.Contains(nameLower, pattern) {
				return sugg
			}
		}
	}

	return nil
}

// formatManualSuggestion formats a manual suggestion into an error message
func (r *Registry) formatManualSuggestion(baseError string, sugg *ManualSuggestion) string {
	var msg strings.Builder
	msg.WriteString(baseError)

	// Add the reason if provided
	if sugg.Reason != "" {
		msg.WriteString(". ")
		msg.WriteString(sugg.Reason)
	}

	// Add tool suggestions
	if len(sugg.SuggestedTools) > 0 {
		// Filter out tools that don't exist in the registry
		validTools := make([]string, 0, len(sugg.SuggestedTools))
		for _, toolName := range sugg.SuggestedTools {
			if _, exists := r.entries[toolName]; exists {
				validTools = append(validTools, toolName)
			}
		}

		if len(validTools) > 0 {
			// Always add a period before the suggestion
			msg.WriteString(". ")

			if len(validTools) == 1 {
				msg.WriteString(fmt.Sprintf("Consider using '%s'.", validTools[0]))
			} else {
				toolsList := strings.Join(validTools, "', '")
				msg.WriteString(fmt.Sprintf("Consider using one of: '%s'.", toolsList))
			}
		}
	}

	return msg.String()
}
