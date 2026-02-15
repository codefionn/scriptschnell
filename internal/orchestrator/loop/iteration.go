package loop

import (
	"context"
	"errors"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/progress"
)

// IterationExecutor handles the execution of a single loop iteration,
// including LLM calls, tool execution, and response processing.
type IterationExecutor struct {
	deps *Dependencies
}

// NewIterationExecutor creates a new IterationExecutor
func NewIterationExecutor(deps *Dependencies) *IterationExecutor {
	return &IterationExecutor{deps: deps}
}

// Execute runs a single iteration of the orchestration loop.
func (e *IterationExecutor) Execute(ctx context.Context, state State) (*IterationOutcome, error) {
	outcome := &IterationOutcome{Result: Continue}

	// Get system prompt
	systemPrompt, err := e.deps.SystemPromptProvider.GetSystemPrompt(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get system prompt: %w", err)
	}

	// Get session messages
	sessionMessages := e.deps.Session.GetMessages()

	// Convert to LLM messages
	llmMessages := e.convertSessionMessages(sessionMessages)

	// Apply cache control if supported
	if e.deps.ContextManager != nil {
		e.applyCacheControl(llmMessages)
	}

	// Check if compaction is needed
	if e.deps.ContextManager != nil && e.deps.ContextManager.ShouldCompact(
		e.deps.SystemPromptProvider.GetModelID(),
		systemPrompt,
		sessionMessages,
	) {
		// Try to compact
		if state.ShouldAllowCompaction() {
			outcome.Result = CompactionNeeded
			return outcome, nil
		}
	}

	// Build completion request
	req := e.buildCompletionRequest(llmMessages, systemPrompt)

	// Execute LLM completion
	response, err := e.executeCompletion(ctx, req)
	if err != nil {
		outcome.Result = Error
		outcome.Error = err
		return outcome, err
	}

	outcome.Response = response
	outcome.Content = response.Content
	outcome.Reasoning = response.Reasoning
	outcome.ToolCalls = response.ToolCalls
	outcome.HasToolCalls = len(response.ToolCalls) > 0

	// Add assistant message to session
	assistantMsg := &SimpleMessage{
		Role:      "assistant",
		Content:   response.Content,
		Reasoning: response.Reasoning,
		ToolCalls: response.ToolCalls,
	}
	e.deps.Session.AddMessage(assistantMsg)

	// Check for loop detection
	if response.Content != "" {
		isLoop, pattern, count := state.RecordLoopDetection(response.Content)
		if isLoop {
			outcome.Result = BreakLoopDetected
			outcome.Metadata = map[string]interface{}{
				"loop_pattern": pattern,
				"loop_count":   count,
			}
			return outcome, nil
		}
	}

	// Handle no tool calls case - check for auto-continue
	if !outcome.HasToolCalls {
		// Check if response is incomplete and auto-continue is needed
		// This would be handled by the strategy in ShouldAutoContinue
		outcome.Result = Break
	} else {
		// Execute tool calls
		toolResult, err := e.executeToolCalls(ctx, response.ToolCalls)
		if err != nil {
			outcome.Result = Error
			outcome.Error = err
			return outcome, err
		}

		// Check if all tools executed successfully
		if toolResult != nil {
			// Tool execution results are already added to session by the tool executor
			outcome.Result = Continue
		}
	}

	return outcome, nil
}

// convertSessionMessages converts session messages to LLM messages
func (e *IterationExecutor) convertSessionMessages(sessionMessages []Message) []*llm.Message {
	llmMessages := make([]*llm.Message, len(sessionMessages))
	for i, msg := range sessionMessages {
		llmMessages[i] = &llm.Message{
			Role:      msg.GetRole(),
			Content:   msg.GetContent(),
			Reasoning: msg.GetReasoning(),
			ToolCalls: msg.GetToolCalls(),
			ToolID:    msg.GetToolID(),
			ToolName:  msg.GetToolName(),
		}
	}
	return llmMessages
}

// applyCacheControl applies cache control breakpoints to messages
func (e *IterationExecutor) applyCacheControl(messages []*llm.Message) {
	// Implementation depends on the specific caching strategy
	// This is a placeholder for cache control logic
	const (
		cacheControlTokenInterval  = 10000
		cacheControlMaxBreakpoints = 4
	)

	for _, msg := range messages {
		if msg != nil {
			msg.CacheControl = false
		}
	}

	if len(messages) == 0 {
		return
	}

	nextThreshold := cacheControlTokenInterval
	totalTokens := 0
	breakpoints := make([]int, 0, cacheControlMaxBreakpoints)

	for idx, msg := range messages {
		if msg == nil {
			continue
		}

		totalTokens += llm.EstimateTokenCountForMessage(msg)
		if totalTokens >= nextThreshold {
			breakpoints = append(breakpoints, idx)
			if len(breakpoints) == cacheControlMaxBreakpoints {
				break
			}
			nextThreshold *= 2
		}
	}

	for _, idx := range breakpoints {
		if idx >= 0 && idx < len(messages) {
			messages[idx].CacheControl = true
		}
	}
}

// buildCompletionRequest builds the LLM completion request
func (e *IterationExecutor) buildCompletionRequest(
	messages []*llm.Message,
	systemPrompt string,
) *llm.CompletionRequest {
	req := &llm.CompletionRequest{
		Messages:     messages,
		Tools:        e.deps.ToolRegistry.ToJSONSchema(),
		SystemPrompt: systemPrompt,
	}

	// Add previous response ID if available (for response ID tracking models)
	if e.deps.LLMClient != nil {
		if prevID := e.deps.LLMClient.GetLastResponseID(); prevID != "" {
			req.PreviousResponseID = prevID
		}
	}

	return req
}

// executeCompletion executes the LLM completion with retry logic
func (e *IterationExecutor) executeCompletion(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	const maxRetries = 3

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		response, err := e.deps.LLMClient.CompleteWithRequest(ctx, req)
		if err == nil {
			return response, nil
		}

		lastErr = err

		// Check for specific error types that shouldn't be retried
		if errors.Is(err, context.Canceled) {
			return nil, err
		}

		// On last attempt, return the error
		if attempt >= maxRetries {
			break
		}

		// Notify progress callback of retry
		if e.deps.ProgressCallback != nil {
			_ = e.deps.ProgressCallback(progress.Update{
				Message:   fmt.Sprintf("Retrying LLM call (attempt %d/%d)...", attempt, maxRetries),
				Mode:      progress.ReportJustStatus,
				Ephemeral: true,
			})
		}
	}

	return nil, fmt.Errorf("LLM completion failed after %d attempts: %w", maxRetries, lastErr)
}

// executeToolCalls executes the tool calls from an LLM response
func (e *IterationExecutor) executeToolCalls(ctx context.Context, toolCalls []map[string]interface{}) (*ToolExecutionResult, error) {
	// This is a placeholder - actual implementation would integrate with the tool registry
	// and handle tool execution with proper error handling and progress reporting

	for _, toolCall := range toolCalls {
		toolID, _ := toolCall["id"].(string)
		function, _ := toolCall["function"].(map[string]interface{})
		toolName, _ := function["name"].(string)

		// Notify progress
		if e.deps.ProgressCallback != nil {
			_ = e.deps.ProgressCallback(progress.Update{
				Message:   fmt.Sprintf("Executing tool: %s", toolName),
				Mode:      progress.ReportJustStatus,
				Ephemeral: true,
			})
		}

		// Tool execution would be handled here
		// This should integrate with the orchestrator's tool execution logic

		_ = toolID
	}

	return &ToolExecutionResult{Success: true}, nil
}

// ToolExecutionResult contains the result of tool execution
type ToolExecutionResult struct {
	Success bool
	Results []ToolResult
	Error   error
}

// ToolResult contains the result of a single tool execution
type ToolResult struct {
	ToolName string
	ToolID   string
	Result   interface{}
	Error    string
}

// ExecutionHooks provides hooks for customizing iteration execution
type ExecutionHooks struct {
	// OnBeforeIteration is called before each iteration
	OnBeforeIteration func(ctx context.Context, state State) error

	// OnAfterIteration is called after each iteration
	OnAfterIteration func(ctx context.Context, state State, outcome *IterationOutcome) error

	// OnBeforeLLMCall is called before the LLM completion
	OnBeforeLLMCall func(ctx context.Context, req *llm.CompletionRequest) error

	// OnAfterLLMCall is called after the LLM completion
	OnAfterLLMCall func(ctx context.Context, response *llm.CompletionResponse, err error) error

	// OnToolExecution is called for each tool execution
	OnToolExecution func(ctx context.Context, toolName, toolID string, params map[string]interface{}) error
}

// HookedIteration wraps an Iteration with execution hooks
type HookedIteration struct {
	base  Iteration
	hooks *ExecutionHooks
}

// NewHookedIteration creates a new HookedIteration
func NewHookedIteration(base Iteration, hooks *ExecutionHooks) *HookedIteration {
	return &HookedIteration{base: base, hooks: hooks}
}

// Execute runs the iteration with hooks
func (h *HookedIteration) Execute(ctx context.Context, state State) (*IterationOutcome, error) {
	if h.hooks != nil && h.hooks.OnBeforeIteration != nil {
		if err := h.hooks.OnBeforeIteration(ctx, state); err != nil {
			return nil, err
		}
	}

	outcome, err := h.base.Execute(ctx, state)

	if h.hooks != nil && h.hooks.OnAfterIteration != nil {
		if hookErr := h.hooks.OnAfterIteration(ctx, state, outcome); hookErr != nil {
			// Log hook error but don't override original error
			_ = hookErr
		}
	}

	return outcome, err
}

// MockIteration is a mock implementation of Iteration for testing
type MockIteration struct {
	MockOutcome *IterationOutcome
	MockError   error
	ExecuteFunc func(ctx context.Context, state State) (*IterationOutcome, error)

	// Tracking
	ExecuteCount int
	LastState    State
}

// Execute runs the mock iteration
func (m *MockIteration) Execute(ctx context.Context, state State) (*IterationOutcome, error) {
	m.ExecuteCount++
	m.LastState = state

	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, state)
	}

	return m.MockOutcome, m.MockError
}
