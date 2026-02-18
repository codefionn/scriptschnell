// Package loop provides an abstraction for the orchestrator loop,
// enabling testability, modularity, and customizable execution strategies.
package loop

import (
	"context"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/progress"
)

// Message represents a conversation message within the loop abstraction.
// This interface abstracts the underlying message implementation.
type Message interface {
	// GetRole returns the role of the message ("user", "assistant", "tool", "system")
	GetRole() string

	// GetContent returns the message content
	GetContent() string

	// GetReasoning returns the reasoning/thinking content (for extended thinking models)
	GetReasoning() string

	// GetToolCalls returns the tool calls in the message
	GetToolCalls() []map[string]interface{}

	// GetToolID returns the tool ID for tool responses
	GetToolID() string

	// GetToolName returns the tool name for tool responses
	GetToolName() string
}

// Session represents a conversation session within the loop abstraction.
// This interface abstracts the underlying session implementation.
type Session interface {
	// AddMessage adds a message to the session
	AddMessage(msg Message)

	// GetMessages returns all messages in the session
	GetMessages() []Message
}

// State manages the iteration state for the orchestration loop.
// It tracks iteration counts, limits, and loop detection state.
type State interface {
	// Iteration returns the current iteration count (0-based)
	Iteration() int

	// Increment advances the iteration counter and returns the new count
	Increment() int

	// MaxIterations returns the maximum number of iterations allowed
	MaxIterations() int

	// HasReachedLimit returns true if the maximum iteration limit has been reached
	HasReachedLimit() bool

	// AutoContinueAttempts returns the current number of auto-continue attempts
	AutoContinueAttempts() int

	// IncrementAutoContinue increments the auto-continue counter and returns the new count
	IncrementAutoContinue() int

	// MaxAutoContinueAttempts returns the maximum number of auto-continue attempts allowed
	MaxAutoContinueAttempts() int

	// HasReachedAutoContinueLimit returns true if auto-continue limit has been reached
	HasReachedAutoContinueLimit() bool

	// ResetAutoContinue resets the auto-continue counter to zero
	ResetAutoContinue()

	// RecordLoopDetection records a potential loop pattern for detection
	RecordLoopDetection(text string) (isLoop bool, pattern string, count int)

	// ResetLoopDetection resets the loop detector state
	ResetLoopDetection()

	// CompactionAttempts returns the number of compaction attempts for current request
	CompactionAttempts() int

	// IncrementCompactionAttempts increments the compaction counter
	IncrementCompactionAttempts() int

	// ResetCompactionAttempts resets compaction attempts to zero
	ResetCompactionAttempts()

	// ConsecutiveCompactions returns the number of consecutive compactions
	ConsecutiveCompactions() int

	// RecordCompaction records a compaction event with timestamp
	RecordCompaction()

	// ShouldAllowCompaction returns true if compaction should be allowed based on recent history
	ShouldAllowCompaction() bool

	// LastCompactionTime returns the timestamp of the last compaction
	LastCompactionTime() time.Time
}

// IterationResult represents the outcome of a single loop iteration
type IterationResult int

const (
	// Continue indicates the loop should continue to the next iteration
	Continue IterationResult = iota

	// Break indicates the loop should stop normally
	Break

	// BreakWithAutoContinue indicates the loop should stop but with auto-continue triggered
	BreakWithAutoContinue

	// BreakMaxIterations indicates the loop stopped due to hitting the iteration limit
	BreakMaxIterations

	// BreakLoopDetected indicates the loop stopped due to detecting a repetitive pattern
	BreakLoopDetected

	// Error indicates an error occurred during iteration
	Error

	// CompactionNeeded indicates the loop needs to compact context before continuing
	CompactionNeeded
)

// String returns a human-readable description of the iteration result
func (r IterationResult) String() string {
	switch r {
	case Continue:
		return "continue"
	case Break:
		return "break"
	case BreakWithAutoContinue:
		return "break_with_auto_continue"
	case BreakMaxIterations:
		return "break_max_iterations"
	case BreakLoopDetected:
		return "break_loop_detected"
	case Error:
		return "error"
	case CompactionNeeded:
		return "compaction_needed"
	default:
		return "unknown"
	}
}

// Iteration executes a single iteration of the orchestration loop.
// It handles the LLM call, tool execution, and response processing.
type Iteration interface {
	// Execute runs a single iteration of the loop.
	// Returns the result and any error that occurred.
	Execute(ctx context.Context, state State) (*IterationOutcome, error)
}

// IterationOutcome contains the detailed results of a single iteration
type IterationOutcome struct {
	// Result indicates the overall outcome type
	Result IterationResult

	// Response contains the LLM response (if successful)
	Response *llm.CompletionResponse

	// Error contains any error that occurred (if Result is Error)
	Error error

	// Content contains the assistant's response content
	Content string

	// Reasoning contains the assistant's reasoning content (if available)
	Reasoning string

	// ToolCalls contains the tool calls requested by the assistant
	ToolCalls []map[string]interface{}

	// HasToolCalls returns true if this iteration included tool calls
	HasToolCalls bool

	// AutoContinueTriggered indicates whether auto-continue was triggered
	AutoContinueTriggered bool

	// ContextUsagePercent contains the current context usage percentage
	ContextUsagePercent int

	// Metadata contains additional iteration-specific data
	Metadata map[string]interface{}
}

// Strategy determines when and how the loop should continue or terminate.
// It encapsulates the decision logic for loop control flow.
type Strategy interface {
	// ShouldContinue determines if the loop should continue after an iteration.
	// Returns true if another iteration should be executed.
	ShouldContinue(state State, outcome *IterationOutcome) bool

	// ShouldAutoContinue determines if auto-continue should be triggered.
	// Called when the assistant's response appears incomplete.
	ShouldAutoContinue(state State, content string) bool

	// GetResult returns the final LoopResult based on the loop's termination state.
	GetResult(state State, lastOutcome *IterationOutcome, terminatedEarly bool) *Result
}

// Result represents the final outcome of the orchestration loop
type Result struct {
	// Success indicates whether the loop completed successfully
	Success bool

	// TerminationReason describes why the loop terminated
	TerminationReason string

	// IterationsExecuted is the total number of iterations run
	IterationsExecuted int

	// AutoContinueAttempts is the number of auto-continue attempts made
	AutoContinueAttempts int

	// Error contains any error that caused termination (if applicable)
	Error error

	// HitIterationLimit is true if the loop hit the max iteration limit
	HitIterationLimit bool

	// LoopDetected is true if a repetitive loop pattern was detected
	LoopDetected bool

	// Metadata contains additional loop-specific information
	Metadata map[string]interface{}
}

// Config contains configuration options for the loop
type Config struct {
	// MaxIterations is the maximum number of iterations (default: 256)
	MaxIterations int

	// MaxAutoContinueAttempts is the max auto-continue attempts (default varies by model)
	MaxAutoContinueAttempts int

	// EnableLoopDetection enables repetitive pattern detection (default: true)
	EnableLoopDetection bool

	// EnableAutoContinue enables automatic continuation on incomplete responses (default: true)
	EnableAutoContinue bool

	// ContextCompactionThresholdPercent triggers compaction at this usage percent
	ContextCompactionThresholdPercent int

	// MaxConsecutiveCompactions limits consecutive compactions (default: 2)
	MaxConsecutiveCompactions int

	// EnableLLMAutoContinueJudge enables LLM-based auto-continue decisions (default: false)
	// When enabled, uses an LLM to intelligently decide whether to continue incomplete responses
	EnableLLMAutoContinueJudge bool

	// LLMAutoContinueJudgeTimeout is the max time to wait for LLM judge (default: 15s)
	LLMAutoContinueJudgeTimeout time.Duration

	// LLMAutoContinueJudgeTokenLimit limits context sent to LLM judge (default: 1000)
	LLMAutoContinueJudgeTokenLimit int
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		MaxIterations:                     256,
		MaxAutoContinueAttempts:           10,
		EnableLoopDetection:               true,
		EnableAutoContinue:                true,
		ContextCompactionThresholdPercent: 90,
		MaxConsecutiveCompactions:         2,
		EnableLLMAutoContinueJudge:        false,
		LLMAutoContinueJudgeTimeout:       15 * time.Second,
		LLMAutoContinueJudgeTokenLimit:    1000,
	}
}

// Loop is the main orchestration loop interface.
// It orchestrates the execution of multiple iterations using the provided
// strategy and iteration implementations.
type Loop interface {
	// Run executes the orchestration loop until completion or termination.
	// Returns the final result of the loop execution.
	Run(ctx context.Context, session Session, progressCb progress.Callback) (*Result, error)

	// RunIteration executes a single iteration and returns the outcome.
	// Useful for step-by-step execution or testing.
	RunIteration(ctx context.Context, state State) (*IterationOutcome, error)

	// GetState returns the current loop state (for inspection/testing)
	GetState() State
}

// Dependencies contains the external dependencies required by the loop
type Dependencies struct {
	// LLMClient is the orchestration LLM client
	LLMClient llm.Client

	// Session is the conversation session
	Session Session

	// ToolRegistry provides access to available tools
	ToolRegistry ToolRegistry

	// SystemPromptProvider provides the system prompt for each iteration
	SystemPromptProvider SystemPromptProvider

	// ContextManager handles context estimation and compaction (optional)
	ContextManager ContextManager

	// ProgressCallback sends progress updates to the UI
	ProgressCallback progress.Callback
}

// ToolRegistry defines the interface for accessing tools
type ToolRegistry interface {
	// ToJSONSchema returns the tools as JSON schema for LLM consumption
	ToJSONSchema() []map[string]interface{}
}

// SystemPromptProvider provides the system prompt for LLM requests
type SystemPromptProvider interface {
	// GetSystemPrompt returns the current system prompt
	GetSystemPrompt(ctx context.Context) (string, error)

	// GetModelID returns the current model ID
	GetModelID() string
}

// ContextManager handles context window management and compaction
type ContextManager interface {
	// EstimateTokens estimates the current token usage
	EstimateTokens(modelID, systemPrompt string, messages []Message) (total int, perMessage []int, err error)

	// ShouldCompact determines if context compaction is needed
	ShouldCompact(modelID, systemPrompt string, messages []Message) bool

	// Compact performs context compaction
	Compact(ctx context.Context, modelID, systemPrompt string, progressCb progress.Callback) error
}

// SimpleMessage is a basic implementation of the Message interface.
// It can be used by any component that needs to create messages compatible with the loop abstraction.
type SimpleMessage struct {
	Role      string
	Content   string
	Reasoning string
	ToolCalls []map[string]interface{}
	ToolID    string
	ToolName  string
}

// GetRole returns the role of the message
func (m *SimpleMessage) GetRole() string { return m.Role }

// GetContent returns the message content
func (m *SimpleMessage) GetContent() string { return m.Content }

// GetReasoning returns the reasoning content
func (m *SimpleMessage) GetReasoning() string { return m.Reasoning }

// GetToolCalls returns the tool calls
func (m *SimpleMessage) GetToolCalls() []map[string]interface{} { return m.ToolCalls }

// GetToolID returns the tool ID
func (m *SimpleMessage) GetToolID() string { return m.ToolID }

// GetToolName returns the tool name
func (m *SimpleMessage) GetToolName() string { return m.ToolName }

// Ensure SimpleMessage implements Message interface
var _ Message = (*SimpleMessage)(nil)
