package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/statcode-ai/statcode-ai/internal/actor"
	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/llm"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/provider"
	"github.com/statcode-ai/statcode-ai/internal/session"
	"github.com/statcode-ai/statcode-ai/internal/tools"
)

// AuthorizationCallback is called when a tool requires user authorization
// It should return true if approved, false if denied
type AuthorizationCallback func(toolName string, params map[string]interface{}, reason string) (bool, error)

// Orchestrator manages the LLM interaction
type Orchestrator struct {
	fs                   fs.FileSystem
	session              *session.Session
	providerMgr          *provider.Manager
	toolRegistry         *tools.Registry
	orchestrationClient  llm.Client
	summarizeClient      llm.Client
	config               *config.Config
	workingDir           string
	ctx                  context.Context
	cancel               context.CancelFunc
	actorSystem          *actor.System
	authorizer           tools.Authorizer
	actorCancel          context.CancelFunc
	compactionMu         sync.Mutex
	compactionInProgress bool
	cliMode              bool
	todoClient           *tools.TodoActorClient
	todoActorCancel      context.CancelFunc
	currentStatusCb      StatusCallback
	statusCbMu           sync.Mutex
	errorJudge           *tools.ErrorJudgeActorClient
	errorJudgeCancel     context.CancelFunc
}

const (
	autoContinueTokenLimit   = 1000
	autoContinueMaxAttempts  = 3
	autoContinueJudgeTimeout = 15 * time.Second
	errorRetryMaxAttempts    = 5
)

func NewOrchestrator(cfg *config.Config, providerMgr *provider.Manager, cliMode bool) (*Orchestrator, error) {
	logger.Debug("Creating new orchestrator with working_dir=%s, cliMode=%v", cfg.WorkingDir, cliMode)
	ctx, cancel := context.WithCancel(context.Background())

	// Create filesystem
	filesystem := fs.NewCachedFS(
		cfg.WorkingDir,
		time.Duration(cfg.CacheTTL)*time.Second,
		cfg.MaxCacheEntries,
	)
	logger.Debug("Filesystem initialized with cache_ttl=%ds", cfg.CacheTTL)

	// Create session
	sess := session.NewSession("main", cfg.WorkingDir)
	logger.Debug("Session created: id=main")

	// Create orchestrator (without authorization actor yet)
	orch := &Orchestrator{
		fs:          filesystem,
		session:     sess,
		providerMgr: providerMgr,
		config:      cfg,
		workingDir:  cfg.WorkingDir,
		ctx:         ctx,
		cancel:      cancel,
		actorSystem: actor.NewSystem(),
		cliMode:     cliMode,
	}

	// Initialize clients first so we have summarize client for authorization actor
	if err := orch.initializeClients(); err != nil {
		// Non-fatal, user can configure later
		fmt.Printf("Warning: %v\n", err)
	}

	// Set up authorization actor with summarize client
	authorizationCtx, authorizationCancel := context.WithCancel(context.Background())
	authActor := tools.NewAuthorizationActor("authorization", filesystem, sess, orch.summarizeClient, nil)
	authRef, err := orch.actorSystem.Spawn(authorizationCtx, "authorization", authActor, 32)
	if err != nil {
		authorizationCancel()
		cancel()
		logger.Error("Failed to start authorization actor: %v", err)
		return nil, fmt.Errorf("failed to start authorization actor: %w", err)
	}
	logger.Debug("Authorization actor spawned")
	orch.authorizer = tools.NewAuthorizationActorClient(authRef)
	orch.actorCancel = authorizationCancel

	// Set up todo actor
	todoCtx, todoCancel := context.WithCancel(context.Background())
	todoActor := tools.NewTodoActor("todo")
	todoRef, err := orch.actorSystem.Spawn(todoCtx, "todo", todoActor, 16)
	if err != nil {
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start todo actor: %v", err)
		return nil, fmt.Errorf("failed to start todo actor: %w", err)
	}
	logger.Debug("Todo actor spawned")
	orch.todoClient = tools.NewTodoActorClient(todoRef)
	orch.todoActorCancel = todoCancel

	// Set up error judge actor with summarize client
	errorJudgeCtx, errorJudgeCancel := context.WithCancel(context.Background())
	errorJudgeActor := tools.NewErrorJudgeActor("error_judge", orch.summarizeClient)
	errorJudgeRef, err := orch.actorSystem.Spawn(errorJudgeCtx, "error_judge", errorJudgeActor, 8)
	if err != nil {
		errorJudgeCancel()
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start error judge actor: %v", err)
		return nil, fmt.Errorf("failed to start error judge actor: %w", err)
	}
	logger.Debug("Error judge actor spawned")
	orch.errorJudge = tools.NewErrorJudgeActorClient(errorJudgeRef)
	orch.errorJudgeCancel = errorJudgeCancel

	// Register tools
	orch.registerTools()

	return orch, nil
}

func (o *Orchestrator) initializeClients() error {
	orchModelID := o.providerMgr.GetOrchestrationModel()
	summModelID := o.providerMgr.GetSummarizeModel()

	if orchModelID != "" {
		client, err := o.providerMgr.CreateClient(orchModelID)
		if err != nil {
			return fmt.Errorf("failed to create orchestration client: %w", err)
		}
		o.orchestrationClient = client
	}

	var summarizeErr error
	if summModelID != "" {
		client, err := o.providerMgr.CreateClient(summModelID)
		if err != nil {
			summarizeErr = fmt.Errorf("failed to create summarize client: %w", err)
		} else {
			o.summarizeClient = client
		}
	}

	// Fallback to the orchestration client when no separate summarize client is configured.
	if o.summarizeClient == nil && o.orchestrationClient != nil {
		o.summarizeClient = o.orchestrationClient
	}

	return summarizeErr
}

func (o *Orchestrator) getSummarizeModelID() string {
	modelID := o.providerMgr.GetSummarizeModel()
	if modelID == "" {
		modelID = o.providerMgr.GetOrchestrationModel()
	}
	return modelID
}

func (o *Orchestrator) registerTools() {
	o.toolRegistry = tools.NewRegistry(o.authorizer)

	// Register read file tool
	o.toolRegistry.Register(o.chooseReadFileTool())

	// Register file creation tool
	o.toolRegistry.Register(tools.NewCreateFileTool(o.fs, o.session))

	// Register diff tool variant based on active model
	if o.shouldUseSimpleDiffTool() {
		o.toolRegistry.Register(tools.NewWriteFileSimpleDiffTool(o.fs, o.session))
	} else {
		o.toolRegistry.Register(tools.NewWriteFileDiffTool(o.fs, o.session))
	}

	// Register search files tool
	o.toolRegistry.Register(tools.NewSearchFilesTool(o.fs))

	// Register web search tool
	o.toolRegistry.Register(tools.NewWebSearchTool(o.config))

	// Register todo tool
	o.toolRegistry.Register(tools.NewTodoTool(o.todoClient))

	// Register shell tool
	o.toolRegistry.Register(tools.NewShellTool(o.session, o.workingDir))

	// Register status tool
	o.toolRegistry.Register(tools.NewStatusTool(o.session))

	// Register sandbox tool with filesystem and session for controlled access
	sandboxTool := tools.NewSandboxToolWithFS(o.workingDir, o.config.TempDir, o.fs, o.session)

	// Set up TinyGo status callback to use the current status callback during ProcessPrompt
	if sandboxTool.GetTinyGoManager() != nil {
		sandboxTool.GetTinyGoManager().SetStatusCallback(func(status string) {
			o.statusCbMu.Lock()
			cb := o.currentStatusCb
			o.statusCbMu.Unlock()

			if cb != nil {
				if err := cb(status); err != nil {
					logger.Warn("Failed to send TinyGo status update: %v", err)
				}
			}
		})
	}

	o.toolRegistry.Register(sandboxTool)

	// Register parallel execution tool
	o.toolRegistry.Register(tools.NewParallelTool(o.toolRegistry))

	// Register summarize file tool (if summarize client is available)
	if o.summarizeClient != nil {
		o.toolRegistry.Register(tools.NewSummarizeFileTool(o.fs, o.session, o.summarizeClient))
	}

	// Register tool summarize (meta-tool that wraps other tools with summarization)
	// NOTE: Register this last so it has access to all other tools
	if o.summarizeClient != nil {
		o.toolRegistry.Register(tools.NewToolSummarizeTool(o.toolRegistry, o.summarizeClient))
	}
}

func (o *Orchestrator) chooseReadFileTool() tools.Tool {
	return tools.NewReadFileNumberedTool(o.fs, o.session)
}

func (o *Orchestrator) shouldUseSimpleDiffTool() bool {
	return true
}

// StatusCallback is called to update the UI with processing status
type StatusCallback func(status string) error

// ContextUsageCallback receives free context percentage and total context window updates for UI display
type ContextUsageCallback func(freePercent int, contextWindow int) error

// ProcessPrompt processes a user prompt
func (o *Orchestrator) ProcessPrompt(ctx context.Context, prompt string, streamCallback func(string) error, statusCallback StatusCallback, contextCallback ContextUsageCallback, authCallback AuthorizationCallback) error {
	if o.orchestrationClient == nil {
		return fmt.Errorf("no orchestration model configured. Use /provider and /models commands to set up")
	}

	// Store status callback for use by tools (e.g., TinyGo download progress)
	o.statusCbMu.Lock()
	o.currentStatusCb = statusCallback
	o.statusCbMu.Unlock()

	defer func() {
		o.statusCbMu.Lock()
		o.currentStatusCb = nil
		o.statusCbMu.Unlock()
	}()

	// Add user message
	o.session.AddMessage(&session.Message{
		Role:    "user",
		Content: prompt,
	})

	// Build system prompt
	modelID := o.providerMgr.GetOrchestrationModel()
	promptBuilder := llm.NewPromptBuilder(o.fs, o.workingDir)
	systemPrompt, err := promptBuilder.BuildSystemPrompt(ctx, modelID, o.cliMode)
	if err != nil {
		return fmt.Errorf("failed to build system prompt: %w", err)
	}

	// Broadcast initial context usage after recording the user message
	o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

	// Tool execution loop
	maxIterations := 256 // Prevent infinite loops
	autoContinueAttempts := 0
	hitMaxIterations := false
	for iteration := 0; iteration < maxIterations; iteration++ {
		logger.Debug("ProcessPrompt iteration %d starting (max=%d)", iteration, maxIterations)

		// Convert session messages to LLM format
		sessionMessages := o.session.GetMessages()
		llmMessages := make([]*llm.Message, len(sessionMessages))
		for i, msg := range sessionMessages {
			llmMessages[i] = &llm.Message{
				Role:      msg.Role,
				Content:   msg.Content,
				ToolCalls: msg.ToolCalls,
				ToolID:    msg.ToolID,
				ToolName:  msg.ToolName,
			}
		}

		totalTokens, perMessageTokens, _ := estimateContextTokens(modelID, systemPrompt, sessionMessages)
		o.dispatchContextUsage(modelID, totalTokens, contextCallback)
		o.maybeCompactContext(modelID, systemPrompt, sessionMessages, perMessageTokens, totalTokens, streamCallback, contextCallback)

		// Get model's max output tokens from model metadata
		maxTokens := o.providerMgr.GetModelMaxOutputTokens(modelID)
		if maxTokens == 0 {
			// Fallback to config value if model doesn't specify max output tokens
			maxTokens = o.config.MaxTokens
			if maxTokens == 0 {
				maxTokens = 4096 // Ultimate fallback
			}
		}

		// Prepare request
		req := &llm.CompletionRequest{
			Messages:     llmMessages,
			Tools:        o.toolRegistry.ToJSONSchema(),
			Temperature:  o.config.Temperature,
			MaxTokens:    maxTokens,
			SystemPrompt: systemPrompt,
		}

		// Notify UI that we're waiting for LLM response
		if statusCallback != nil && len(llmMessages) > 1 {
			// Show that we're processing with the LLM
			if err := statusCallback("Thinking..."); err != nil {
				logger.Warn("Failed to send status update: %v", err)
			}
		}

		// Get completion with error retry logic
		response, err := o.completeWithRetry(ctx, req, statusCallback, streamCallback)
		if err != nil {
			return fmt.Errorf("completion failed: %w", err)
		}

		// Log the stop reason
		if response.StopReason != "" {
			logger.Info("LLM generation stop reason: %s", response.StopReason)
		} else {
			logger.Debug("LLM generation completed with no stop reason reported")
		}

		// Log response details for debugging
		logger.Debug("LLM response: content_length=%d, tool_calls=%d, stop_reason=%q",
			len(response.Content), len(response.ToolCalls), response.StopReason)

		// Add assistant response to session
		o.session.AddMessage(&session.Message{
			Role:      "assistant",
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		})
		o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

		// Stream the content to UI if present
		if response.Content != "" && streamCallback != nil {
			if err := streamCallback(response.Content); err != nil {
				return err
			}
		} else if len(response.ToolCalls) > 0 && response.Content == "" {
			// Log when we have tool calls but no content - this is normal but worth tracking
			logger.Debug("Response contains %d tool calls with no text content", len(response.ToolCalls))
		}

		// Check if response was truncated due to token limits
		isTruncated := response.StopReason == "length" || response.StopReason == "max_tokens"

		// Check if there are tool calls to execute
		if len(response.ToolCalls) == 0 {
			// No tool calls - check if we should auto-continue
			shouldContinue, judgeOutput := o.shouldAutoContinue(ctx, systemPrompt)
			logger.Debug("Auto-continue judge called (no tool calls): shouldContinue=%v, output=%q", shouldContinue, judgeOutput)

			if shouldContinue && autoContinueAttempts < autoContinueMaxAttempts {
				autoContinueAttempts++
				logger.Info("Auto-continue triggered (attempt %d/%d)", autoContinueAttempts, autoContinueMaxAttempts)
				if streamCallback != nil {
					if err := streamCallback("\n⏭ Auto-continue requested.\n"); err != nil {
						return err
					}
				}
				if statusCallback != nil {
					if err := statusCallback("Continuing..."); err != nil {
						logger.Warn("Failed to send status update: %v", err)
					}
				}
				o.session.AddMessage(&session.Message{
					Role:    "user",
					Content: "continue",
				})
				o.broadcastContextUsage(modelID, systemPrompt, contextCallback)
				continue
			}
			if judgeOutput != "" {
				logger.Debug("Auto-continue judge decision: %s", judgeOutput)
			}
			if statusCallback != nil {
				if err := statusCallback(""); err != nil {
					logger.Warn("Failed to clear status: %v", err)
				}
			}
			logger.Debug("Breaking out of loop: no tool calls and no auto-continue (iteration %d)", iteration)
			break
		}

		// Tool calls present - check if response was truncated AND we should auto-continue
		if isTruncated {
			shouldContinue, judgeOutput := o.shouldAutoContinue(ctx, systemPrompt)
			logger.Debug("Auto-continue judge called (truncated with tool calls): shouldContinue=%v, output=%q", shouldContinue, judgeOutput)

			if shouldContinue && autoContinueAttempts < autoContinueMaxAttempts {
				autoContinueAttempts++
				logger.Info("Auto-continue triggered due to truncation (attempt %d/%d)", autoContinueAttempts, autoContinueMaxAttempts)
				if streamCallback != nil {
					if err := streamCallback("\n⏭ Response truncated, auto-continuing...\n"); err != nil {
						return err
					}
				}
				if statusCallback != nil {
					if err := statusCallback("Continuing..."); err != nil {
						logger.Warn("Failed to send status update: %v", err)
					}
				}
				o.session.AddMessage(&session.Message{
					Role:    "user",
					Content: "continue",
				})
				o.broadcastContextUsage(modelID, systemPrompt, contextCallback)
				continue
			}
		}

		// Execute each tool call
		logger.Debug("Executing %d tool calls from iteration %d", len(response.ToolCalls), iteration)
		for _, toolCall := range response.ToolCalls {
			toolID, _ := toolCall["id"].(string)
			toolType, _ := toolCall["type"].(string)

			if toolType != "function" {
				continue
			}

			function, ok := toolCall["function"].(map[string]interface{})
			if !ok {
				continue
			}

			toolName, _ := function["name"].(string)
			argsJSON, _ := function["arguments"].(string)
			logger.Debug("Executing tool: %s (id=%s)", toolName, toolID)

			// Notify UI about tool call
			if statusCallback != nil {
				if err := statusCallback(fmt.Sprintf("Calling tool: %s", toolName)); err != nil {
					logger.Warn("Failed to send status update: %v", err)
				}
			}
			if streamCallback != nil {
				if err := streamCallback(fmt.Sprintf("\n🔧 Calling tool: %s\n", toolName)); err != nil {
					logger.Warn("Failed to stream tool call notification: %v", err)
				}
			}

			// Parse arguments
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				o.session.AddMessage(&session.Message{
					Role:    "tool",
					Content: fmt.Sprintf("Error parsing tool arguments: %v", err),
					ToolID:  toolID,
				})
				continue
			}

			// Execute tool
			toolCallObj := &tools.ToolCall{
				ID:         toolID,
				Name:       toolName,
				Parameters: args,
			}
			result := o.toolRegistry.Execute(ctx, toolCallObj)

			// Check if authorization is required
			if result.RequiresUserInput {
				// Ask user for approval
				approved := false
				if authCallback != nil {
					var err error
					approved, err = authCallback(toolName, args, result.AuthReason)
					if err != nil {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: fmt.Sprintf("Authorization error: %v", err),
						}
					} else if approved {
						// Re-execute with approval
						result = o.toolRegistry.ExecuteWithApproval(ctx, toolCallObj)
					} else {
						// User denied
						result = &tools.ToolResult{
							ID:    toolID,
							Error: "Operation denied by user",
						}
					}
				} else {
					// No callback provided, deny by default
					result = &tools.ToolResult{
						ID:    toolID,
						Error: "Authorization required but no approval mechanism available",
					}
				}
			}

			// Format result as string
			var toolResult string
			if result.Error != "" {
				toolResult = fmt.Sprintf("Error: %s", result.Error)
			} else {
				toolResult = fmt.Sprintf("%v", result.Result)
			}

			// Add tool result to session
			o.session.AddMessage(&session.Message{
				Role:     "tool",
				Content:  toolResult,
				ToolID:   toolID,
				ToolName: toolName,
			})
			o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

			// Notify UI about tool result
			if streamCallback != nil {
				truncated := toolResult
				if len(truncated) > 200 {
					truncated = truncated[:200] + "..."
				}
				if err := streamCallback(fmt.Sprintf("✓ Result: %s\n", truncated)); err != nil {
					logger.Warn("Failed to stream tool result: %v", err)
				}
			}
			logger.Debug("Tool execution complete: %s (id=%s)", toolName, toolID)
		}

		// Log that we're continuing the loop to get LLM's analysis of tool results
		logger.Debug("Tool execution complete, continuing loop to get LLM analysis (iteration %d)", iteration)

		// Check if this is the last iteration
		if iteration == maxIterations-1 {
			hitMaxIterations = true
		}
	}

	// Check if we exited due to hitting max iterations
	if hitMaxIterations {
		logger.Warn("ProcessPrompt reached maximum iteration limit (%d). This may indicate the LLM is stuck in a loop.", maxIterations)
		if streamCallback != nil {
			_ = streamCallback(fmt.Sprintf("\n⚠️  Reached maximum iteration limit (%d). Stopping to prevent infinite loop.\n", maxIterations))
		}
	}

	logger.Debug("ProcessPrompt completed")
	return nil
}

// completeWithRetry wraps LLM completion with error retry logic
func (o *Orchestrator) completeWithRetry(ctx context.Context, req *llm.CompletionRequest, statusCallback StatusCallback, streamCallback func(string) error) (*llm.CompletionResponse, error) {
	modelID := o.providerMgr.GetOrchestrationModel()

	for attempt := 1; attempt <= errorRetryMaxAttempts; attempt++ {
		// Try the completion
		response, err := o.orchestrationClient.CompleteWithRequest(ctx, req)

		// Success - return immediately
		if err == nil {
			return response, nil
		}

		// Log the error
		logger.Warn("LLM completion error (attempt %d/%d): %v", attempt, errorRetryMaxAttempts, err)

		// Last attempt - return the error
		if attempt >= errorRetryMaxAttempts {
			return nil, err
		}

		// Consult the error judge
		decision, judgeErr := o.consultErrorJudge(ctx, err, attempt, modelID)
		if judgeErr != nil {
			logger.Warn("Error judge consultation failed: %v", judgeErr)
			// Continue without retry on judge error
			return nil, err
		}

		// Check if we should retry
		if !decision.ShouldRetry {
			logger.Info("Error judge decided to halt: %s", decision.Reason)
			if streamCallback != nil {
				_ = streamCallback(fmt.Sprintf("\n⚠️  %s\n", decision.Reason))
			}
			return nil, err
		}

		// Notify user about retry
		logger.Info("Error judge decided to retry (attempt %d/%d, sleep %ds): %s",
			attempt, errorRetryMaxAttempts, decision.SleepSeconds, decision.Reason)

		if streamCallback != nil {
			_ = streamCallback(fmt.Sprintf("\n⏳ Retrying in %d seconds... (Attempt %d/%d: %s)\n",
				decision.SleepSeconds, attempt, errorRetryMaxAttempts, decision.Reason))
		}

		if statusCallback != nil {
			_ = statusCallback(fmt.Sprintf("Retrying in %ds...", decision.SleepSeconds))
		}

		// Sleep before retry
		if decision.SleepSeconds > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(decision.SleepSeconds) * time.Second):
				// Continue to retry
			}
		}

		// Update status to show we're retrying
		if statusCallback != nil {
			_ = statusCallback(fmt.Sprintf("Retrying (attempt %d/%d)...", attempt+1, errorRetryMaxAttempts))
		}
	}

	// Should never reach here, but just in case
	return nil, fmt.Errorf("max retry attempts exceeded")
}

// consultErrorJudge asks the error judge actor for a decision
func (o *Orchestrator) consultErrorJudge(ctx context.Context, err error, attemptNumber int, modelID string) (tools.ErrorJudgeDecision, error) {
	// If no error judge available, use heuristic fallback
	if o.errorJudge == nil {
		logger.Debug("No error judge available, using built-in heuristics")
		return o.heuristicErrorDecision(err, attemptNumber), nil
	}

	// Ask the error judge actor
	decision, judgeErr := o.errorJudge.Judge(ctx, err, attemptNumber, errorRetryMaxAttempts, modelID)
	if judgeErr != nil {
		// Fallback to heuristics if judge fails
		logger.Warn("Error judge failed, using heuristics: %v", judgeErr)
		return o.heuristicErrorDecision(err, attemptNumber), nil
	}

	return decision, nil
}

// heuristicErrorDecision provides a simple fallback when error judge is unavailable
func (o *Orchestrator) heuristicErrorDecision(err error, attemptNumber int) tools.ErrorJudgeDecision {
	errMsg := strings.ToLower(err.Error())

	// Rate limit errors - retry with exponential backoff
	if strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "429") {
		sleepSeconds := 5 * (1 << uint(attemptNumber-1)) // 5, 10, 20, 40, 80...
		if sleepSeconds > 60 {
			sleepSeconds = 60
		}
		return tools.ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: sleepSeconds,
			Reason:       "Rate limit error detected",
		}
	}

	// Temporary service errors - retry with moderate delay
	if strings.Contains(errMsg, "500") || strings.Contains(errMsg, "503") ||
	   strings.Contains(errMsg, "timeout") {
		return tools.ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: attemptNumber * 3,
			Reason:       "Temporary service error",
		}
	}

	// Network errors - retry with short delay
	if strings.Contains(errMsg, "connection") || strings.Contains(errMsg, "network") {
		return tools.ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: attemptNumber * 2,
			Reason:       "Network error",
		}
	}

	// Unknown error - try a couple times
	if attemptNumber < 3 {
		return tools.ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: attemptNumber * 2,
			Reason:       "Unknown error, retrying cautiously",
		}
	}

	// Don't retry after multiple attempts
	return tools.ErrorJudgeDecision{
		ShouldRetry:  false,
		SleepSeconds: 0,
		Reason:       "Error persisted after multiple attempts",
	}
}

func (o *Orchestrator) shouldAutoContinue(ctx context.Context, systemPrompt string) (bool, string) {
	if o.summarizeClient == nil {
		logger.Debug("Auto-continue skipped: no summarize client")
		return false, ""
	}

	modelID := o.getSummarizeModelID()
	if modelID == "" {
		logger.Debug("Auto-continue skipped: no summarize-capable model configured")
		return false, ""
	}

	messages := o.session.GetMessages()
	if len(messages) == 0 {
		logger.Debug("Auto-continue skipped: no messages in session")
		return false, ""
	}

	userPrompts := collectRecentUserPrompts(messages, 10)
	if len(userPrompts) == 0 {
		logger.Debug("Auto-continue skipped: no user prompts found")
		return false, ""
	}

	recentMessages := selectRecentMessagesByTokens(modelID, messages, autoContinueTokenLimit)
	if len(recentMessages) == 0 {
		recentMessages = messages
	}
	logger.Debug("Auto-continue judge analyzing %d recent messages (from %d total)", len(recentMessages), len(messages))

	prompt := buildAutoContinueJudgePrompt(userPrompts, recentMessages, systemPrompt)

	judgeCtx, cancel := context.WithTimeout(ctx, autoContinueJudgeTimeout)
	defer cancel()

	logger.Debug("Calling auto-continue judge with timeout %v", autoContinueJudgeTimeout)
	result, err := o.summarizeClient.Complete(judgeCtx, prompt)
	if err != nil {
		logger.Warn("Auto-continue judge failed: %v", err)
		return false, ""
	}

	decision := strings.TrimSpace(result)
	if decision == "" {
		logger.Warn("Auto-continue judge returned empty decision")
		return false, ""
	}

	upper := strings.ToUpper(decision)
	fields := strings.Fields(upper)
	head := upper
	if len(fields) > 0 {
		head = fields[0]
	}

	switch head {
	case "CONTINUE":
		logger.Debug("Auto-continue judge decided: CONTINUE (full response: %q)", decision)
		return true, decision
	case "STOP":
		logger.Debug("Auto-continue judge decided: STOP (full response: %q)", decision)
		return false, decision
	default:
		if strings.Contains(upper, "CONTINUE") && !strings.Contains(upper, "DO NOT CONTINUE") {
			logger.Debug("Auto-continue judge decided: CONTINUE (heuristic match, full response: %q)", decision)
			return true, decision
		}
		logger.Debug("Auto-continue judge decided: STOP (no match, full response: %q)", decision)
	}

	return false, decision
}

func collectRecentUserPrompts(messages []*session.Message, limit int) []string {
	if limit <= 0 {
		return nil
	}

	prompts := make([]string, 0, limit)
	for i := len(messages) - 1; i >= 0 && len(prompts) < limit; i-- {
		if !strings.EqualFold(messages[i].Role, "user") {
			continue
		}

		text := strings.TrimSpace(messages[i].Content)
		if text == "" {
			text = "(empty)"
		}

		prompts = append(prompts, text)
	}

	for i, j := 0, len(prompts)-1; i < j; i, j = i+1, j-1 {
		prompts[i], prompts[j] = prompts[j], prompts[i]
	}

	return prompts
}

func selectRecentMessagesByTokens(modelID string, messages []*session.Message, tokenLimit int) []*session.Message {
	if tokenLimit <= 0 || len(messages) == 0 {
		return nil
	}

	_, perMessageTokens, _ := estimateContextTokens(modelID, "", messages)

	total := 0
	start := len(messages)

	for i := len(messages) - 1; i >= 0; i-- {
		tokens := perMessageTokens[i]
		if tokens <= 0 {
			tokens = 1
		}

		if total+tokens > tokenLimit && start < len(messages) {
			break
		}

		total += tokens
		start = i

		if total >= tokenLimit {
			break
		}
	}

	return messages[start:]
}

func buildAutoContinueJudgePrompt(userPrompts []string, messages []*session.Message, systemPrompt string) string {
	var sb strings.Builder
	sb.WriteString("You are an auto-continue judge. Decide whether the assistant should keep generating its reply.\n")
	sb.WriteString("Respond with exactly one word: CONTINUE or STOP.\n")
	sb.WriteString("Choose CONTINUE when the assistant response appears incomplete, truncated, or when unresolved tasks remain. Choose STOP when the response is complete or further continuation is unnecessary.\n\n")

	trimmedSystemPrompt := strings.TrimSpace(systemPrompt)
	if trimmedSystemPrompt == "" {
		sb.WriteString("System prompt: (unavailable)\n")
	} else {
		sb.WriteString("System prompt (includes project context):\n")
		sb.WriteString(trimmedSystemPrompt)
		if !strings.HasSuffix(trimmedSystemPrompt, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	if len(userPrompts) > 0 {
		sb.WriteString("Recent user prompts (oldest to newest):\n")
		for i, prompt := range userPrompts {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, prompt))
		}
	} else {
		sb.WriteString("Recent user prompts: (none)\n")
	}

	sb.WriteString("\nConversation excerpt (most recent context, approx last ")
	sb.WriteString(fmt.Sprintf("%d tokens):\n", autoContinueTokenLimit))

	if len(messages) == 0 {
		sb.WriteString("(no messages)\n")
	} else {
		for _, msg := range messages {
			role := formatRoleLabel(msg)
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				content = "(no content)"
			}

			sb.WriteString(role)
			sb.WriteString(": ")
			sb.WriteString(content)
			sb.WriteString("\n---\n")
		}
	}

	sb.WriteString("\nReply with exactly one word: CONTINUE or STOP.")
	return sb.String()
}

func (o *Orchestrator) maybeCompactContext(modelID, systemPrompt string, sessionMessages []*session.Message, perMessageTokens []int, totalTokens int, streamCallback func(string) error, contextCallback ContextUsageCallback) {
	if len(sessionMessages) < 4 {
		return
	}

	contextWindow := o.getContextWindow(modelID)
	if contextWindow <= 0 {
		return
	}

	if totalTokens*100/contextWindow < 80 {
		return
	}

	prefixCount := selectCompactionPrefix(perMessageTokens, totalTokens)
	if prefixCount <= 0 {
		return
	}

	if prefixCount >= len(sessionMessages) {
		prefixCount = len(sessionMessages) - 1
	}

	// Ensure we keep at least two recent messages un-compacted
	if len(sessionMessages)-prefixCount < 2 {
		prefixCount = len(sessionMessages) - 2
		if prefixCount <= 0 {
			return
		}
	}

	messagesCopy := append([]*session.Message(nil), sessionMessages[:prefixCount]...)

	o.compactionMu.Lock()
	if o.compactionInProgress {
		o.compactionMu.Unlock()
		return
	}
	o.compactionInProgress = true
	o.compactionMu.Unlock()

	go o.compactContext(modelID, systemPrompt, contextCallback, messagesCopy, streamCallback)
}

func (o *Orchestrator) compactContext(modelID, systemPrompt string, contextCallback ContextUsageCallback, messages []*session.Message, streamCallback func(string) error) {
	defer func() {
		o.compactionMu.Lock()
		o.compactionInProgress = false
		o.compactionMu.Unlock()
	}()

	if len(messages) == 0 {
		return
	}

	summaryPrompt := buildSummaryPrompt(messages)
	summary := ""

	if o.summarizeClient != nil {
		summaryCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		result, err := o.summarizeClient.Complete(summaryCtx, summaryPrompt)
		if err != nil {
			fmt.Printf("Context compaction summary failed: %v\n", err)
		} else {
			summary = strings.TrimSpace(result)
		}
	}

	if summary == "" {
		summary = fallbackConversationSummary(messages)
	}

	if summary == "" {
		return
	}

	summaryContent := fmt.Sprintf("Summary of earlier context (auto-compacted):\n%s", summary)

	if !o.session.CompactWithSummary(messages, summaryContent) {
		// Session head changed before compaction could apply
		return
	}

	o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

	if streamCallback != nil {
		_ = streamCallback("\n🧹 Auto-compacted earlier context.\n")
	}
}

func (o *Orchestrator) dispatchContextUsage(modelID string, totalTokens int, callback ContextUsageCallback) {
	if callback == nil {
		return
	}

	window := o.getContextWindow(modelID)
	if window <= 0 {
		_ = callback(-1, 0)
		return
	}

	usedPercent := 0
	if totalTokens > 0 && window > 0 {
		usedPercent = (totalTokens * 100) / window
	}
	if usedPercent > 100 {
		usedPercent = 100
	}
	if usedPercent < 0 {
		usedPercent = 0
	}
	freePercent := 100 - usedPercent
	if freePercent < 0 {
		freePercent = 0
	}
	if freePercent > 100 {
		freePercent = 100
	}

	_ = callback(freePercent, window)
}

func (o *Orchestrator) broadcastContextUsage(modelID, systemPrompt string, callback ContextUsageCallback) {
	if callback == nil {
		return
	}

	messages := o.session.GetMessages()
	totalTokens, _, _ := estimateContextTokens(modelID, systemPrompt, messages)
	o.dispatchContextUsage(modelID, totalTokens, callback)
}

func (o *Orchestrator) getContextWindow(modelID string) int {
	if modelID == "" {
		return 0
	}

	if window := o.providerMgr.GetModelContextWindow(modelID); window > 0 {
		return window
	}

	return heuristicContextWindow(modelID)
}

func selectCompactionPrefix(perMessageTokens []int, totalTokens int) int {
	if len(perMessageTokens) == 0 {
		return 0
	}

	threshold := totalTokens * 40 / 100
	if threshold == 0 {
		threshold = perMessageTokens[0]
	}

	accum := 0
	for i, tokens := range perMessageTokens {
		if tokens <= 0 {
			tokens = 1
		}
		accum += tokens
		if accum >= threshold {
			return i + 1
		}
	}

	return len(perMessageTokens)
}

func buildSummaryPrompt(messages []*session.Message) string {
	var sb strings.Builder
	sb.WriteString("Summarize the earlier part of this conversation so the assistant can continue later without losing context.\n")
	sb.WriteString("Capture tasks in progress, key decisions, file operations, and remaining follow-ups.\n")
	sb.WriteString("Use concise bullet points; preserve critical commands, file paths, and TODOs.\n\n")
	sb.WriteString("Conversation history:\n")
	for _, msg := range messages {
		role := formatRoleLabel(msg)
		sb.WriteString(role)
		sb.WriteString(": ")
		sb.WriteString(strings.TrimSpace(msg.Content))
		sb.WriteString("\n---\n")
	}

	return sb.String()
}

func fallbackConversationSummary(messages []*session.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Key points retained:\n")
	for _, msg := range messages {
		role := formatRoleLabel(msg)
		sb.WriteString("- ")
		sb.WriteString(role)
		sb.WriteString(": ")
		sb.WriteString(condenseContent(msg.Content, 200))
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

func condenseContent(content string, limit int) string {
	if limit <= 0 {
		return ""
	}

	collapsed := strings.Join(strings.Fields(content), " ")
	if collapsed == "" {
		return "(no content)"
	}

	runes := []rune(collapsed)
	if len(runes) <= limit {
		return collapsed
	}

	if limit <= 3 {
		return string(runes[:limit])
	}

	return string(runes[:limit-3]) + "..."
}

func formatRoleLabel(msg *session.Message) string {
	role := strings.TrimSpace(msg.Role)
	if role == "" {
		role = "unknown"
	}
	runes := []rune(role)
	runes[0] = unicode.ToUpper(runes[0])
	label := string(runes)
	if msg.ToolName != "" {
		label += " (" + msg.ToolName + ")"
	}
	return label
}

func heuristicContextWindow(modelID string) int {
	model := strings.ToLower(modelID)
	if strings.HasPrefix(model, "claude-") {
		return 200000
	}
	if strings.Contains(model, "32k") {
		return 32768
	}
	if strings.Contains(model, "16k") {
		return 16384
	}
	if strings.HasPrefix(model, "gpt-3.5") {
		return 4096
	}
	if strings.Contains(model, "turbo") || strings.Contains(model, "4o") || strings.Contains(model, "o1") || strings.Contains(model, "o3") {
		return 128000
	}
	if strings.HasPrefix(model, "gpt-4") {
		return 8192
	}
	return 8192
}

// Stop stops the current generation
func (o *Orchestrator) Stop() {
	if o.cancel != nil {
		o.cancel()
		// Recreate context for next use
		o.ctx, o.cancel = context.WithCancel(context.Background())
	}
}

// Close closes the orchestrator
func (o *Orchestrator) Close() error {
	if o.cancel != nil {
		o.cancel()
	}

	if o.actorCancel != nil {
		o.actorCancel()
	}

	if o.todoActorCancel != nil {
		o.todoActorCancel()
	}

	if o.errorJudgeCancel != nil {
		o.errorJudgeCancel()
	}

	var firstErr error

	if o.actorSystem != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := o.actorSystem.StopAll(shutdownCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Close the filesystem watcher if it's a CachedFS
	if cachedFS, ok := o.fs.(*fs.CachedFS); ok {
		if err := cachedFS.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// GetCurrentModel returns the current orchestration model name
func (o *Orchestrator) GetCurrentModel() string {
	if o.orchestrationClient == nil {
		return "none"
	}
	return o.orchestrationClient.GetModelName()
}

// GetTodoClient returns the TodoActorClient for UI access
func (o *Orchestrator) GetTodoClient() *tools.TodoActorClient {
	return o.todoClient
}

// UpdateModels updates the LLM clients
func (o *Orchestrator) UpdateModels() error {
	return o.initializeClients()
}

// GetContextFile returns the context file used to prime the LLM, if available.
func (o *Orchestrator) GetContextFile() string {
	exists, err := o.fs.Exists(o.ctx, llm.AgentsFileName)
	if err != nil || !exists {
		return ""
	}
	return llm.AgentsFileName
}

func (o *Orchestrator) GetInitPrompt() string {
	return llm.InitPrompt()
}

// GetFilesystem returns the filesystem instance for use in TUI autocomplete
func (o *Orchestrator) GetFilesystem() fs.FileSystem {
	return o.fs
}

// GetWorkingDir returns the working directory
func (o *Orchestrator) GetWorkingDir() string {
	return o.workingDir
}

// ClearSession clears the current session, removing all messages and file tracking
func (o *Orchestrator) ClearSession() error {
	o.session.Clear()
	return nil
}
