package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/orchestrator/loop"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/session"
)

// sessionAdapter wraps *session.Session to implement loop.Session
type sessionAdapter struct {
	session *session.Session
}

// newSessionAdapter creates a new session adapter
func newSessionAdapter(sess *session.Session) loop.Session {
	return &sessionAdapter{session: sess}
}

// AddMessage adds a message to the session
func (a *sessionAdapter) AddMessage(msg loop.Message) {
	a.session.AddMessage(&session.Message{
		Role:      msg.GetRole(),
		Content:   msg.GetContent(),
		Reasoning: msg.GetReasoning(),
		ToolCalls: msg.GetToolCalls(),
		ToolID:    msg.GetToolID(),
		ToolName:  msg.GetToolName(),
	})
}

// GetMessages returns all messages in the session as loop.Message interfaces
func (a *sessionAdapter) GetMessages() []loop.Message {
	sessionMessages := a.session.GetMessages()
	messages := make([]loop.Message, len(sessionMessages))
	for i, msg := range sessionMessages {
		messages[i] = msg // *session.Message implements loop.Message via its methods
	}
	return messages
}

func selectLatestNativeMessage(nativeMsgs []interface{}) interface{} {
	for i := len(nativeMsgs) - 1; i >= 0; i-- {
		if m, ok := nativeMsgs[i].(map[string]interface{}); ok {
			if _, isSystem := m["_anthropic_system"]; isSystem {
				continue
			}
		}
		return nativeMsgs[i]
	}
	return nil
}

// Ensure sessionAdapter implements loop.Session
var _ loop.Session = (*sessionAdapter)(nil)

// orchestratorSystemPromptProvider implements loop.SystemPromptProvider
type orchestratorSystemPromptProvider struct {
	orch *Orchestrator
}

func newOrchestratorSystemPromptProvider(orch *Orchestrator) *orchestratorSystemPromptProvider {
	return &orchestratorSystemPromptProvider{orch: orch}
}

func (p *orchestratorSystemPromptProvider) GetSystemPrompt(ctx context.Context) (string, error) {
	modelID := p.orch.providerMgr.GetOrchestrationModel()
	return p.orch.getOrBuildSystemPrompt(ctx, modelID)
}

func (p *orchestratorSystemPromptProvider) GetModelID() string {
	return p.orch.providerMgr.GetOrchestrationModel()
}

// orchestratorContextManager implements loop.ContextManager
type orchestratorContextManager struct {
	orch *Orchestrator
}

func newOrchestratorContextManager(orch *Orchestrator) *orchestratorContextManager {
	return &orchestratorContextManager{orch: orch}
}

func (cm *orchestratorContextManager) EstimateTokens(modelID, systemPrompt string, messages []loop.Message) (total int, perMessage []int, err error) {
	// Convert loop.Message to *session.Message for the existing function
	sessionMessages := make([]*session.Message, len(messages))
	for i, msg := range messages {
		if m, ok := msg.(*session.Message); ok {
			sessionMessages[i] = m
		} else {
			// Fallback: create a new session.Message from the interface
			sessionMessages[i] = &session.Message{
				Role:      msg.GetRole(),
				Content:   msg.GetContent(),
				Reasoning: msg.GetReasoning(),
				ToolCalls: msg.GetToolCalls(),
				ToolID:    msg.GetToolID(),
				ToolName:  msg.GetToolName(),
			}
		}
	}
	total, perMessage, _ = estimateContextTokens(modelID, systemPrompt, sessionMessages)
	return total, perMessage, nil
}

func (cm *orchestratorContextManager) ShouldCompact(modelID, systemPrompt string, messages []loop.Message) bool {
	// Convert loop.Message to *session.Message for the existing function
	sessionMessages := make([]*session.Message, len(messages))
	for i, msg := range messages {
		if m, ok := msg.(*session.Message); ok {
			sessionMessages[i] = m
		} else {
			sessionMessages[i] = &session.Message{
				Role:      msg.GetRole(),
				Content:   msg.GetContent(),
				Reasoning: msg.GetReasoning(),
				ToolCalls: msg.GetToolCalls(),
				ToolID:    msg.GetToolID(),
				ToolName:  msg.GetToolName(),
			}
		}
	}
	// Use the orchestrator's existing compaction check
	totalTokens, _, _ := estimateContextTokens(modelID, systemPrompt, sessionMessages)
	modelLimit := cm.orch.getContextWindow(modelID)
	if modelLimit <= 0 {
		modelLimit = cm.orch.config.MaxTokens
		if modelLimit <= 0 {
			modelLimit = 8192
		}
	}
	// Trigger compaction at 90% usage
	return totalTokens > int(float64(modelLimit)*0.9)
}

func (cm *orchestratorContextManager) Compact(ctx context.Context, modelID, systemPrompt string, progressCb progress.Callback) error {
	sessionMessages := cm.orch.session.GetMessages()
	_, perMessageTokens, _ := estimateContextTokens(modelID, systemPrompt, sessionMessages)
	totalTokens, _, _ := estimateContextTokens(modelID, systemPrompt, sessionMessages)
	cm.orch.maybeCompactContext(modelID, systemPrompt, sessionMessages, perMessageTokens, totalTokens, progressCb, nil)
	return nil
}

// orchestratorToolRegistry wraps the orchestrator's tool registry for the loop
type orchestratorToolRegistry struct {
	orch *Orchestrator
}

func newOrchestratorToolRegistry(orch *Orchestrator) *orchestratorToolRegistry {
	return &orchestratorToolRegistry{orch: orch}
}

func (r *orchestratorToolRegistry) ToJSONSchema() []map[string]interface{} {
	if r.orch.toolRegistry == nil {
		return nil
	}
	return r.orch.toolRegistry.ToJSONSchema()
}

// orchestratorIteration wraps the orchestrator's iteration logic
type orchestratorIteration struct {
	orch               *Orchestrator
	progressCallback   progress.Callback
	authCallback       AuthorizationCallback
	toolCallCallback   ToolCallCallback
	toolResultCallback ToolResultCallback
	contextCallback    ContextUsageCallback
}

func newOrchestratorIteration(
	orch *Orchestrator,
	progressCallback progress.Callback,
	authCallback AuthorizationCallback,
	toolCallCallback ToolCallCallback,
	toolResultCallback ToolResultCallback,
	contextCallback ContextUsageCallback,
) *orchestratorIteration {
	return &orchestratorIteration{
		orch:               orch,
		progressCallback:   progressCallback,
		authCallback:       authCallback,
		toolCallCallback:   toolCallCallback,
		toolResultCallback: toolResultCallback,
		contextCallback:    contextCallback,
	}
}

func (i *orchestratorIteration) Execute(ctx context.Context, state loop.State) (*loop.IterationOutcome, error) {
	outcome := &loop.IterationOutcome{Result: loop.Continue}

	sendStatus := func(msg string) {
		dispatchProgress(i.progressCallback, progress.Update{
			Message:   msg,
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		})
	}

	sendStream := func(msg string, addNewLine bool) {
		dispatchProgress(i.progressCallback, progress.Update{
			Message:    msg,
			AddNewLine: addNewLine,
			Mode:       progress.ReportNoStatus,
		})
	}

	modelID := i.orch.providerMgr.GetOrchestrationModel()
	systemPrompt, err := i.orch.getOrBuildSystemPrompt(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("failed to build system prompt: %w", err)
	}

	// Get session messages
	sessionMessages := i.orch.session.GetMessages()

	// Convert to LLM messages
	llmMessages := make([]*llm.Message, len(sessionMessages))
	for idx, msg := range sessionMessages {
		llmMessages[idx] = &llm.Message{
			Role:              msg.Role,
			Content:           msg.Content,
			Reasoning:         msg.Reasoning,
			ToolCalls:         msg.ToolCalls,
			ToolID:            msg.ToolID,
			ToolName:          msg.ToolName,
			NativeFormat:      msg.NativeFormat,
			NativeProvider:    msg.NativeProvider,
			NativeModelFamily: msg.NativeModelFamily,
			NativeTimestamp:   msg.NativeTimestamp,
		}
	}

	// Apply cache control if enabled
	if i.orch.config != nil && i.orch.config.EnablePromptCache {
		markCacheControlBreakpoints(llmMessages, cacheControlTokenInterval, cacheControlMaxBreakpoints)
	}

	// Estimate context and compact if needed
	totalTokens, perMessageTokens, _ := estimateContextTokens(modelID, systemPrompt, sessionMessages)
	i.orch.dispatchContextUsage(modelID, totalTokens, i.contextCallback)

	// Check compaction
	if state.ShouldAllowCompaction() {
		i.orch.maybeCompactContext(modelID, systemPrompt, sessionMessages, perMessageTokens, totalTokens, i.progressCallback, i.contextCallback)
		if i.orch.compactionInProgress {
			state.RecordCompaction()
		}
	}

	// Get max tokens
	maxTokens := i.orch.providerMgr.GetModelMaxOutputTokens(modelID)
	if maxTokens == 0 {
		maxTokens = i.orch.config.MaxTokens
		if maxTokens == 0 {
			maxTokens = consts.DefaultMaxTokens
		}
	}

	// Prepare request
	var toolsJSON []map[string]interface{}
	if i.orch.toolRegistry != nil {
		toolsJSON = i.orch.toolRegistry.ToJSONSchema()
	}
	req := &llm.CompletionRequest{
		Messages:      llmMessages,
		Tools:         toolsJSON,
		Temperature:   i.orch.config.Temperature,
		MaxTokens:     maxTokens,
		SystemPrompt:  systemPrompt,
		EnableCaching: i.orch.config.EnablePromptCache,
		CacheTTL:      i.orch.config.PromptCacheTTL,
	}

	if i.orch.orchestrationClient != nil {
		if prevID := i.orch.orchestrationClient.GetLastResponseID(); prevID != "" {
			req.PreviousResponseID = prevID
		}
	}

	i.orch.applyModelSpecificDefaults(req, modelID)

	if i.progressCallback != nil && len(llmMessages) > 1 {
		sendStatus("Thinking...")
	}

	// Get completion with error retry logic
	var validationErr *toolCallValidationError
	var ctxSizeErr *contextSizeExceededError
	response, err := i.orch.completeWithRetry(ctx, req, i.progressCallback)
	if err != nil {
		if errors.As(err, &validationErr) {
			msg := fmt.Sprintf("Tool call validation failed for '%s': missing required parameter '%s'.", validationErr.toolName, validationErr.missingParam)
			sendStream(fmt.Sprintf("\n⚠️ %s\n", msg), false)
			sendStatus("Waiting for tool call validation fix...")
			i.orch.session.AddMessage(&session.Message{
				Role:    "user",
				Content: msg,
			})
			i.orch.broadcastContextUsage(modelID, systemPrompt, i.contextCallback)
			outcome.Result = loop.Continue
			return outcome, nil
		}
		if errors.As(err, &ctxSizeErr) {
			logger.Info("Context size exceeded, triggering forced compaction: %s", ctxSizeErr.reason)
			sendStatus("Compacting context...")
			i.orch.forceCompactContext(modelID, systemPrompt, i.orch.session.GetMessages(), i.progressCallback, i.contextCallback)
			i.orch.resetCompactionAttempts()
			outcome.Result = loop.CompactionNeeded
			return outcome, nil
		}
		outcome.Result = loop.Error
		outcome.Error = err
		return outcome, err
	}

	outcome.Response = response
	outcome.Content = response.Content
	outcome.Reasoning = response.Reasoning
	outcome.ToolCalls = response.ToolCalls
	outcome.HasToolCalls = len(response.ToolCalls) > 0

	// Detect provider/model changes for native format support
	provider, modelFamily, _ := i.orch.detectProviderChange(modelID)
	converter := llm.GetConverter(modelID)

	// Convert to native format if supported
	var nativeMsgs []interface{}
	var nativeErr error
	if converter != nil && converter.SupportsNativeStorage() {
		nativeMsgs, nativeErr = converter.ConvertToNative([]*llm.Message{{
			Role:      "assistant",
			Content:   response.Content,
			Reasoning: response.Reasoning,
			ToolCalls: response.ToolCalls,
		}}, "", i.orch.config.EnablePromptCache, i.orch.config.PromptCacheTTL)
	}

	// Add assistant message
	assistantMsg := &session.Message{
		Role:      "assistant",
		Content:   response.Content,
		Reasoning: response.Reasoning,
		ToolCalls: response.ToolCalls,
	}

	// Set native format if available
	if nativeErr == nil && len(nativeMsgs) > 0 {
		if nativeFormat := selectLatestNativeMessage(nativeMsgs); nativeFormat != nil {
			assistantMsg.NativeFormat = nativeFormat
			assistantMsg.NativeProvider = provider
			assistantMsg.NativeModelFamily = modelFamily
			assistantMsg.NativeTimestamp = time.Now()
		}
	}

	i.orch.session.AddMessage(assistantMsg)
	i.orch.broadcastContextUsage(modelID, systemPrompt, i.contextCallback)

	// Stream reasoning content to UI
	if response.Reasoning != "" {
		dispatchProgress(i.progressCallback, progress.Update{
			Reasoning: response.Reasoning,
			Mode:      progress.ReportNoStatus,
			Ephemeral: true,
		})
	}

	// Stream content to UI
	if response.Content != "" {
		sendStream(response.Content, false)
	}

	// Check for text loops
	if response.Content != "" {
		isLoop, pattern, count := state.RecordLoopDetection(response.Content)
		if isLoop {
			displayPattern := pattern
			if len(displayPattern) > 100 {
				displayPattern = displayPattern[:100] + "..."
			}
			sendStream(fmt.Sprintf("\n\n🔁 Loop detected! Pattern '%s' repeated %d times. Stopping.\n", displayPattern, count), false)
			outcome.Result = loop.BreakLoopDetected
			outcome.Metadata = map[string]interface{}{
				"loop_pattern": pattern,
				"loop_count":   count,
			}
			return outcome, nil
		}
	}

	// Handle no tool calls case
	if !outcome.HasToolCalls {
		outcome.Result = loop.Break
	} else {
		// Execute tool calls
		if err := i.orch.processToolCalls(ctx, response.ToolCalls, i.orch.session, i.progressCallback, i.authCallback, i.toolCallCallback, i.toolResultCallback, nil); err != nil {
			logger.Warn("Error processing tool calls: %v", err)
		}
		outcome.Result = loop.Continue
	}

	return outcome, nil
}

// buildLoopConfig builds loop configuration from orchestrator config
func (o *Orchestrator) buildLoopConfig() *loop.Config {
	// Start with defaults
	config := loop.DefaultConfig()

	// Apply settings from config.Loop if present
	if o.config != nil {
		loopCfg := o.config.Loop

		if loopCfg.MaxIterations > 0 {
			config.MaxIterations = loopCfg.MaxIterations
		}

		if loopCfg.MaxAutoContinueAttempts > 0 {
			config.MaxAutoContinueAttempts = loopCfg.MaxAutoContinueAttempts
		} else {
			// Use model-specific defaults
			config.MaxAutoContinueAttempts = o.getAutoContinueMaxAttempts()
		}

		config.EnableLoopDetection = loopCfg.EnableLoopDetection
		config.EnableAutoContinue = loopCfg.EnableAutoContinue

		// LLM judge settings
		config.EnableLLMAutoContinueJudge = loopCfg.EnableLLMAutoContinueJudge
		if loopCfg.LLMAutoContinueJudgeTimeout > 0 {
			config.LLMAutoContinueJudgeTimeout = time.Duration(loopCfg.LLMAutoContinueJudgeTimeout) * time.Second
		}
		if loopCfg.LLMAutoContinueJudgeTokenLimit > 0 {
			config.LLMAutoContinueJudgeTokenLimit = loopCfg.LLMAutoContinueJudgeTokenLimit
		}
	} else {
		// Use model-specific defaults
		config.MaxAutoContinueAttempts = o.getAutoContinueMaxAttempts()
	}

	return config
}

// createLoopStrategy creates a strategy based on configuration
func (o *Orchestrator) createLoopStrategy(config *loop.Config) loop.Strategy {
	strategyMode := "default"
	if o.config != nil && o.config.Loop.Strategy != "" {
		strategyMode = o.config.Loop.Strategy
	}

	factory := loop.NewStrategyFactory(config)

	// Check if we should use LLM judge strategy
	if strategyMode == "llm-judge" && config.EnableLLMAutoContinueJudge {
		// Use summarization client for LLM judge (faster/cheaper than orchestration model)
		llmClient := o.summarizeClient
		if llmClient == nil {
			// Fall back to orchestration client if summarization client is not available
			llmClient = o.orchestrationClient
		}

		modelID := o.providerMgr.GetSummarizeModel()
		if modelID == "" {
			modelID = o.providerMgr.GetOrchestrationModel()
		}

		// Create session adapter for the strategy
		session := newSessionAdapter(o.session)

		if llmClient != nil && modelID != "" {
			logger.Debug("Creating LLM judge strategy with model: %s", modelID)
			return factory.CreateWithLLMJudge(strategyMode, config, llmClient, modelID, session)
		}

		// Fall back to default if LLM client or model not available
		logger.Warn("LLM judge strategy requested but LLM client/model not available, falling back to default strategy")
		return factory.Create("default")
	}

	return factory.Create(strategyMode)
}

// initializeLoop creates and initializes the loop abstraction
func (o *Orchestrator) initializeLoop() error {
	// Build configuration from orchestrator settings
	o.loopConfig = o.buildLoopConfig()

	deps := &loop.Dependencies{
		LLMClient:            o.orchestrationClient,
		Session:              newSessionAdapter(o.session),
		ToolRegistry:         newOrchestratorToolRegistry(o),
		SystemPromptProvider: newOrchestratorSystemPromptProvider(o),
		ContextManager:       newOrchestratorContextManager(o),
		ProgressCallback:     o.currentProgressCb,
	}

	// Create strategy based on configuration
	strategy := o.createLoopStrategy(o.loopConfig)

	// The actual iteration will be created per-request with callbacks
	o.loop = loop.NewOrchestratorLoop(o.loopConfig, strategy, nil, deps)

	return nil
}

// SetLoopStrategy sets the loop strategy mode ("default", "conservative", "aggressive")
func (o *Orchestrator) SetLoopStrategy(strategy string) {
	if o.config != nil {
		o.config.Loop.Strategy = strategy
	}
}

// runOrchestrationLoopWithAbstraction uses the new loop abstraction
func (o *Orchestrator) runOrchestrationLoopWithAbstraction(
	ctx context.Context,
	progressCallback progress.Callback,
	contextCallback ContextUsageCallback,
	authCallback AuthorizationCallback,
	toolCallCallback ToolCallCallback,
	toolResultCallback ToolResultCallback,
) error {
	if o.loop == nil {
		if err := o.initializeLoop(); err != nil {
			return fmt.Errorf("failed to initialize loop: %w", err)
		}
	}

	// Create the iteration with the current callbacks
	iteration := newOrchestratorIteration(o, progressCallback, authCallback, toolCallCallback, toolResultCallback, contextCallback)

	// Set the iteration on the loop
	if _, ok := o.loop.(*loop.OrchestratorLoop); ok {
		// We need to create a new loop with the proper iteration
		strategy := o.createLoopStrategy(o.loopConfig)
		deps := &loop.Dependencies{
			LLMClient:            o.orchestrationClient,
			Session:              newSessionAdapter(o.session),
			ToolRegistry:         newOrchestratorToolRegistry(o),
			SystemPromptProvider: newOrchestratorSystemPromptProvider(o),
			ContextManager:       newOrchestratorContextManager(o),
			ProgressCallback:     progressCallback,
		}
		o.loop = loop.NewOrchestratorLoop(o.loopConfig, strategy, iteration, deps)
	}

	result, err := o.loop.Run(ctx, newSessionAdapter(o.session), progressCallback)
	if err != nil {
		return err
	}

	if result.HitIterationLimit {
		logger.Warn("Orchestration loop reached maximum iteration limit")
	}

	return nil
}
