package bash

import (
	"context"
	"time"
)

// ExecutionType defines how a command should be executed
type ExecutionType string

const (
	ExecutionInternal  ExecutionType = "internal"  // Execute internally without spawning process
	ExecutionExternal  ExecutionType = "external"  // Execute via external process
	ExecutionBuiltin   ExecutionType = "builtin"   // Shell builtin (cd, export, etc.)
	ExecutionVirtual   ExecutionType = "virtual"   // Virtual command (simulated in memory)
)

// CommandType categorizes the type of command
type CommandType string

const (
	CommandFilesystem  CommandType = "filesystem"  // File operations (cat, ls, cp, mv, etc.)
	CommandProcess     CommandType = "process"     // Process management (ps, kill, etc.)
	CommandNetwork     CommandType = "network"     // Network operations (curl, wget, etc.)
	CommandText        CommandType = "text"        // Text processing (grep, sed, awk, etc.)
	CommandArchive     CommandType = "archive"     // Archive operations (tar, zip, etc.)
	CommandSystem      CommandType = "system"      // System info (uname, date, etc.)
	CommandEnvironment CommandType = "environment" // Environment (env, export, etc.)
	CommandFlow        CommandType = "flow"        // Control flow (if, while, for, etc.)
	CommandUser        CommandType = "user"        // User operations (whoami, id, etc.)
	CommandMisc        CommandType = "misc"        // Miscellaneous
)

// CommandMetadata contains metadata for command routing and execution
type CommandMetadata struct {
	Name           string        `json:"name"`             // Command name
	CanonicalPath  string        `json:"canonical_path"`   // Full path to command (if external)
	ExecutionType  ExecutionType `json:"execution_type"`   // How to execute
	CommandType    CommandType   `json:"command_type"`     // Category of command
	SafeInternal   bool          `json:"safe_internal"`    // Can be safely executed internally
	RequiresSandbox bool         `json:"requires_sandbox"` // Needs sandbox environment
	SideEffects    bool          `json:"side_effects"`     // Has side effects (writes, network, etc.)
	MinArgs        int           `json:"min_args"`         // Minimum arguments required
	MaxArgs        int           `json:"max_args"`         // Maximum arguments (-1 = unlimited)
	Description    string        `json:"description"`      // Command description
	Examples       []string      `json:"examples"`         // Example usages
}

// ExecutionContext provides context for command execution
type ExecutionContext struct {
	Context       context.Context
	WorkingDir    string            `json:"working_dir"`
	Environment   map[string]string `json:"environment"`
	Stdin         string            `json:"stdin"`
	Variables     *Environment      `json:"variables"`
	Filesystem    VirtualFilesystem `json:"filesystem"`
	ParentPID     int               `json:"parent_pid"`
	Timeout       time.Duration     `json:"timeout"`
	Interactive   bool              `json:"interactive"`
	Background    bool              `json:"background"`
	Redirects     []*RedirectNode   `json:"redirects"`
}

// ExecutionResult contains the result of command execution
type ExecutionResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Error    error  `json:"error"`
	PID      int    `json:"pid"` // Process ID if external
}

// CommandHandler defines the interface for executing commands
type CommandHandler interface {
	// Execute runs the command with the given context and arguments
	Execute(ctx *ExecutionContext, args []string) *ExecutionResult
	
	// Metadata returns command metadata
	Metadata() *CommandMetadata
	
	// Validate checks if the command arguments are valid
	Validate(args []string) error
}

// CommandRouter routes commands to appropriate handlers
type CommandRouter struct {
	handlers      map[string]CommandHandler
	internalCmds  map[string]bool
	externalPaths []string
}

// NewCommandRouter creates a new command router
func NewCommandRouter() *CommandRouter {
	return &CommandRouter{
		handlers:      make(map[string]CommandHandler),
		internalCmds:  make(map[string]bool),
		externalPaths: []string{},
	}
}

// RegisterHandler registers a command handler
func (r *CommandRouter) RegisterHandler(name string, handler CommandHandler) {
	r.handlers[name] = handler
	if handler.Metadata().ExecutionType == ExecutionInternal || 
	   handler.Metadata().ExecutionType == ExecutionBuiltin ||
	   handler.Metadata().ExecutionType == ExecutionVirtual {
		r.internalCmds[name] = true
	}
}

// Route determines how to execute a command
func (r *CommandRouter) Route(cmd *CommandNode) (*CommandMetadata, CommandHandler) {
	name := cmd.Name
	
	// Check for registered handler
	if handler, ok := r.handlers[name]; ok {
		return handler.Metadata(), handler
	}
	
	// Default to external execution
	return &CommandMetadata{
		Name:           name,
		ExecutionType:  ExecutionExternal,
		CommandType:    CommandMisc,
		SafeInternal:   false,
		RequiresSandbox: true,
		SideEffects:    true,
		Description:    "External command",
	}, nil
}

// IsInternal checks if a command can be handled internally
func (r *CommandRouter) IsInternal(name string) bool {
	return r.internalCmds[name]
}

// GetHandler returns the handler for a command
func (r *CommandRouter) GetHandler(name string) (CommandHandler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}

// ListInternalCommands returns all internally handled commands
func (r *CommandRouter) ListInternalCommands() []string {
	cmds := make([]string, 0, len(r.internalCmds))
	for cmd := range r.internalCmds {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// InternalCommandHandler is a base implementation for internal commands
type InternalCommandHandler struct {
	metadata *CommandMetadata
	execute  func(ctx *ExecutionContext, args []string) *ExecutionResult
}

func NewInternalCommandHandler(metadata *CommandMetadata, 
	execute func(ctx *ExecutionContext, args []string) *ExecutionResult) *InternalCommandHandler {
	return &InternalCommandHandler{
		metadata: metadata,
		execute:  execute,
	}
}

func (h *InternalCommandHandler) Execute(ctx *ExecutionContext, args []string) *ExecutionResult {
	return h.execute(ctx, args)
}

func (h *InternalCommandHandler) Metadata() *CommandMetadata {
	return h.metadata
}

func (h *InternalCommandHandler) Validate(args []string) error {
	if len(args) < h.metadata.MinArgs {
		return &CommandError{
			Command: h.metadata.Name,
			Message: "insufficient arguments",
		}
	}
	if h.metadata.MaxArgs >= 0 && len(args) > h.metadata.MaxArgs {
		return &CommandError{
			Command: h.metadata.Name,
			Message: "too many arguments",
		}
	}
	return nil
}

// CommandError represents an error in command execution
type CommandError struct {
	Command string `json:"command"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *CommandError) Error() string {
	return e.Command + ": " + e.Message
}
