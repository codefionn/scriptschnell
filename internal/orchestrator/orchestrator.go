package orchestrator

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
	"github.com/statcode-ai/statcode-ai/internal/mcp"
	"github.com/statcode-ai/statcode-ai/internal/provider"
	"github.com/statcode-ai/statcode-ai/internal/session"
	"github.com/statcode-ai/statcode-ai/internal/tools"
)

// AuthorizationCallback is called when a tool requires user authorization
// It should return true if approved, false if denied
type AuthorizationCallback func(toolName string, params map[string]interface{}, reason string) (bool, error)

// ToolCallCallback is called when a tool is being executed
type ToolCallCallback func(toolName, toolID string, parameters map[string]interface{}) error

// ToolResultCallback is called when a tool execution completes
type ToolResultCallback func(toolName, toolID, result, errorMsg string) error

// Orchestrator manages the LLM interaction
type Orchestrator struct {
	fs                    fs.FileSystem
	session               *session.Session
	providerMgr           *provider.Manager
	toolRegistry          *tools.Registry
	orchestrationClient   llm.Client
	summarizeClient       llm.Client
	config                *config.Config
	workingDir            string
	ctx                   context.Context
	cancel                context.CancelFunc
	actorSystem           *actor.System
	authorizer            tools.Authorizer
	actorCancel           context.CancelFunc
	compactionMu          sync.Mutex
	compactionInProgress  bool
	cliMode               bool
	todoClient            *tools.TodoActorClient
	todoActorCancel       context.CancelFunc
	currentStatusCb       StatusCallback
	statusCbMu            sync.Mutex
	errorJudge            *tools.ErrorJudgeActorClient
	errorJudgeCancel      context.CancelFunc
	toolExecutor          *tools.ToolExecutorActorClient
	toolExecutorCancel    context.CancelFunc
	activeShellMu         sync.Mutex
	activeShellChan       chan struct{}
	loopDetector          *LoopDetector
	mcpManager            *mcp.Manager
	toolSelectionDirty    bool
	activeMCPServers      []string
	activeMCPMu           sync.RWMutex
	preconnectMu          sync.Mutex
	preconnectInFlight    bool
	lastPreconnectAttempt time.Time
	preconnectCompleted   bool
	clientInitMu          sync.Mutex
}

const (
	autoContinueMaxAttempts = 3
	errorRetryMaxAttempts   = 5
	preconnectThrottle      = 2 * time.Second
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
		fs:           filesystem,
		session:      sess,
		providerMgr:  providerMgr,
		config:       cfg,
		workingDir:   cfg.WorkingDir,
		ctx:          ctx,
		cancel:       cancel,
		actorSystem:  actor.NewSystem(),
		cliMode:      cliMode,
		loopDetector: NewLoopDetector(),
	}
	orch.mcpManager = mcp.NewManager(cfg, cfg.WorkingDir, providerMgr)

	// Initialize clients first so we have summarize client for authorization actor
	if err := orch.initializeClients(); err != nil {
		// Non-fatal, user can configure later
		fmt.Printf("Warning: %v\n", err)
	}

	// Set up authorization actor with summarize client
	authorizationCtx, authorizationCancel := context.WithCancel(context.Background())
	allowedCommandPrefixes := make([]string, 0, len(cfg.AuthorizedCommands))
	for prefix, enabled := range cfg.AuthorizedCommands {
		if enabled {
			allowedCommandPrefixes = append(allowedCommandPrefixes, prefix)
		}
	}

	allowedDomainPatterns := make([]string, 0, len(cfg.AuthorizedDomains))
	for domain, enabled := range cfg.AuthorizedDomains {
		if enabled {
			allowedDomainPatterns = append(allowedDomainPatterns, domain)
		}
	}

	authOpts := &tools.AuthorizationOptions{
		AllowedCommands: allowedCommandPrefixes,
		AllowedDomains:  allowedDomainPatterns,
	}

	authActor := tools.NewAuthorizationActor("authorization", filesystem, sess, orch.summarizeClient, authOpts)
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
	if toolErrs := orch.rebuildTools(false); len(toolErrs) > 0 {
		for _, terr := range toolErrs {
			if terr != nil {
				logger.Warn("Tool registration warning: %v", terr)
			}
		}
	}

	// Set up tool executor actor
	toolExecutorCtx, toolExecutorCancel := context.WithCancel(context.Background())
	toolExecutorActor := tools.NewToolExecutorActor("tool_executor", orch.toolRegistry)
	toolExecutorRef, err := orch.actorSystem.Spawn(toolExecutorCtx, "tool_executor", toolExecutorActor, 32)
	if err != nil {
		toolExecutorCancel()
		errorJudgeCancel()
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start tool executor actor: %v", err)
		return nil, fmt.Errorf("failed to start tool executor actor: %w", err)
	}
	logger.Debug("Tool executor actor spawned")
	orch.toolExecutor = tools.NewToolExecutorActorClient(toolExecutorRef)
	orch.toolExecutorCancel = toolExecutorCancel

	return orch, nil
}

func (o *Orchestrator) initializeClients() error {
	o.clientInitMu.Lock()
	defer o.clientInitMu.Unlock()

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

type toolSpec struct {
	template tools.Tool
	critical bool
	factory  func(reg *tools.Registry) tools.Tool
	isMCP    bool
	mcpKey   string
}

func (o *Orchestrator) rebuildTools(applyFilter bool) []error {
	var errs []error

	specs := make([]toolSpec, 0, 16)
	addSpec := func(template tools.Tool, critical bool, factory func(reg *tools.Registry) tools.Tool, isMCP bool, mcpKey string) {
		if template == nil || factory == nil {
			return
		}
		specs = append(specs, toolSpec{
			template: template,
			critical: critical,
			factory:  factory,
			isMCP:    isMCP,
			mcpKey:   mcpKey,
		})
	}

	modelFamily := llm.DetectModelFamily(o.providerMgr.GetOrchestrationModel())

	// Core filesystem tools
	if o.shouldUseNumberedReadFileTool(modelFamily) {
		addSpec(
			tools.NewReadFileNumberedTool(o.fs, o.session),
			true,
			func(_ *tools.Registry) tools.Tool { return tools.NewReadFileNumberedTool(o.fs, o.session) },
			false,
			"",
		)
	} else {
		addSpec(
			tools.NewReadFileTool(o.fs, o.session),
			true,
			func(_ *tools.Registry) tools.Tool { return tools.NewReadFileTool(o.fs, o.session) },
			false,
			"",
		)
	}
	addSpec(
		tools.NewCreateFileTool(o.fs, o.session),
		true,
		func(_ *tools.Registry) tools.Tool { return tools.NewCreateFileTool(o.fs, o.session) },
		false,
		"",
	)
	if o.shouldUseSimpleDiffTool(modelFamily) {
		addSpec(
			tools.NewWriteFileSimpleDiffTool(o.fs, o.session),
			true,
			func(_ *tools.Registry) tools.Tool { return tools.NewWriteFileSimpleDiffTool(o.fs, o.session) },
			false,
			"",
		)
	} else {
		addSpec(
			tools.NewWriteFileDiffTool(o.fs, o.session),
			true,
			func(_ *tools.Registry) tools.Tool { return tools.NewWriteFileDiffTool(o.fs, o.session) },
			false,
			"",
		)
	}

	// Discovery / search tools
	addSpec(
		tools.NewSearchFilesTool(o.fs),
		false,
		func(_ *tools.Registry) tools.Tool { return tools.NewSearchFilesTool(o.fs) },
		false,
		"",
	)
	addSpec(
		tools.NewWebSearchTool(o.config),
		false,
		func(_ *tools.Registry) tools.Tool { return tools.NewWebSearchTool(o.config) },
		false,
		"",
	)

	// Task management
	addSpec(
		tools.NewTodoTool(o.todoClient),
		false,
		func(_ *tools.Registry) tools.Tool { return tools.NewTodoTool(o.todoClient) },
		false,
		"",
	)

	// Shell tooling
	if o.shouldUseShellTool(modelFamily) {
		addSpec(
			tools.NewShellTool(o.session, o.workingDir),
			true,
			func(_ *tools.Registry) tools.Tool { return tools.NewShellTool(o.session, o.workingDir) },
			false,
			"",
		)
	}

	addSpec(
		tools.NewStatusProgramTool(o.session),
		true,
		func(_ *tools.Registry) tools.Tool { return tools.NewStatusProgramTool(o.session) },
		false,
		"",
	)
	addSpec(
		tools.NewWaitProgramTool(o.session),
		true,
		func(_ *tools.Registry) tools.Tool { return tools.NewWaitProgramTool(o.session) },
		false,
		"",
	)
	addSpec(
		tools.NewStopProgramTool(o.session),
		true,
		func(_ *tools.Registry) tools.Tool { return tools.NewStopProgramTool(o.session) },
		false,
		"",
	)

	// Sandbox tool with TinyGo status forwarding
	sandboxTemplate := tools.NewSandboxToolWithFS(o.workingDir, o.config.TempDir, o.fs, o.session)
	o.configureSandboxTool(sandboxTemplate)
	addSpec(
		sandboxTemplate,
		true,
		func(_ *tools.Registry) tools.Tool {
			instance := tools.NewSandboxToolWithFS(o.workingDir, o.config.TempDir, o.fs, o.session)
			o.configureSandboxTool(instance)
			return instance
		},
		false,
		"",
	)

	// Parallel execution
	addSpec(
		tools.NewParallelTool(nil),
		false,
		func(reg *tools.Registry) tools.Tool { return tools.NewParallelTool(reg) },
		false,
		"",
	)

	// Summarization-related tools
	if o.summarizeClient != nil {
		addSpec(
			tools.NewSummarizeFileTool(o.fs, o.session, o.summarizeClient),
			false,
			func(_ *tools.Registry) tools.Tool {
				return tools.NewSummarizeFileTool(o.fs, o.session, o.summarizeClient)
			},
			false,
			"",
		)
		addSpec(
			tools.NewToolSummarizeTool(nil, o.summarizeClient),
			false,
			func(reg *tools.Registry) tools.Tool { return tools.NewToolSummarizeTool(reg, o.summarizeClient) },
			false,
			"",
		)
	}

	// MCP-derived tools
	if o.mcpManager != nil {
		mcpTools, mcpErrs := o.mcpManager.BuildTools()
		for _, err := range mcpErrs {
			if err != nil {
				errs = append(errs, err)
			}
		}
		for _, tool := range mcpTools {
			t := tool
			mcpKey := extractMCPSanitizedServer(t.Name())
			addSpec(
				t,
				false,
				func(_ *tools.Registry) tools.Tool { return t },
				true,
				mcpKey,
			)
		}
	}

	filteredSpecs := specs
	if applyFilter {
		var (
			filterErr error
			mcpSpecs  []toolSpec
		)
		for _, spec := range specs {
			if spec.isMCP {
				mcpSpecs = append(mcpSpecs, spec)
			}
		}

		filteredMCP := mcpSpecs
		if len(mcpSpecs) > 0 {
			filteredMCP, filterErr = o.filterToolSpecs(mcpSpecs)
		}
		if filterErr != nil {
			errs = append(errs, filterErr)
		}

		if len(mcpSpecs) > 0 {
			filterMap := make(map[tools.Tool]struct{}, len(filteredMCP))
			for i := range filteredMCP {
				filterMap[filteredMCP[i].template] = struct{}{}
			}

			filteredSpecs = make([]toolSpec, 0, len(specs))
			for _, spec := range specs {
				if spec.isMCP {
					if _, ok := filterMap[spec.template]; ok {
						filteredSpecs = append(filteredSpecs, spec)
					}
					continue
				}
				filteredSpecs = append(filteredSpecs, spec)
			}
		} else {
			// No external MCP tools configured; nothing to filter.
			filteredSpecs = specs
		}
	} else {
		o.toolSelectionDirty = true
		o.setActiveMCPServers(nil)
	}

	registry := tools.NewRegistry(o.authorizer)
	o.toolRegistry = registry

	var activeMCPSanitized []string
	seenMCP := make(map[string]struct{})

	for _, spec := range filteredSpecs {
		toolInstance := spec.factory(registry)
		if toolInstance == nil {
			continue
		}
		registry.Register(toolInstance)

		if applyFilter && spec.isMCP && spec.mcpKey != "" {
			if _, exists := seenMCP[spec.mcpKey]; !exists {
				activeMCPSanitized = append(activeMCPSanitized, spec.mcpKey)
				seenMCP[spec.mcpKey] = struct{}{}
			}
		}
	}

	if o.toolExecutor != nil {
		if err := o.toolExecutor.SetRegistry(registry); err != nil {
			logger.Warn("Failed to update tool executor registry: %v", err)
		}
	}

	if applyFilter {
		o.setActiveMCPServers(o.resolveMCPServerNames(activeMCPSanitized))
		o.toolSelectionDirty = false
	}

	return errs
}

func (o *Orchestrator) shouldUseShellTool(modelFamily llm.ModelFamily) bool {
	return false
}

func (o *Orchestrator) shouldUseNumberedReadFileTool(modelFamily llm.ModelFamily) bool {
	return modelFamily != llm.FamilyZaiGLM
}

func (o *Orchestrator) shouldUseSimpleDiffTool(modelFamily llm.ModelFamily) bool {
	return true
}

func (o *Orchestrator) configureSandboxTool(sandboxTool *tools.SandboxTool) {
	if sandboxTool == nil {
		return
	}
	sandboxTool.SetSummarizeClient(o.summarizeClient)
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
}

// RefreshMCPTools rebuilds and registers tools derived from MCP server configuration.
func (o *Orchestrator) RefreshMCPTools() []error {
	return o.rebuildTools(false)
}

// TestMCPServer attempts to build tools for a specific MCP server to validate configuration.
func (o *Orchestrator) TestMCPServer(serverName string) error {
	if o.mcpManager == nil {
		return fmt.Errorf("mcp manager not initialized")
	}
	server, ok := o.config.MCP.Servers[serverName]
	if !ok {
		return fmt.Errorf("mcp server not found: %s", serverName)
	}

	tmpCfg := *o.config
	tmpCfg.MCP.Servers = map[string]*config.MCPServerConfig{
		serverName: server,
	}

	manager := mcp.NewManager(&tmpCfg, o.workingDir, o.providerMgr)
	tools, errs := manager.BuildTools()
	if len(errs) > 0 {
		messages := make([]string, 0, len(errs))
		for _, err := range errs {
			if err != nil {
				messages = append(messages, err.Error())
			}
		}
		if len(messages) > 0 {
			return fmt.Errorf("%s", strings.Join(messages, "; "))
		}
	}
	if len(tools) == 0 {
		return fmt.Errorf("no tools produced for MCP server: %s", serverName)
	}
	return nil
}

func (o *Orchestrator) setActiveMCPServers(servers []string) {
	o.activeMCPMu.Lock()
	defer o.activeMCPMu.Unlock()
	if servers == nil {
		o.activeMCPServers = nil
		return
	}
	o.activeMCPServers = append([]string(nil), servers...)
}

func (o *Orchestrator) resolveMCPServerNames(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	resolved := make([]string, 0, len(keys))
	seen := make(map[string]struct{})
	for _, key := range keys {
		if key == "" {
			continue
		}
		name := o.lookupServerBySanitizedKey(key)
		if name == "" {
			name = key
		}
		if _, exists := seen[name]; exists {
			continue
		}
		resolved = append(resolved, name)
		seen[name] = struct{}{}
	}
	return resolved
}

func (o *Orchestrator) lookupServerBySanitizedKey(key string) string {
	if o.config == nil {
		return ""
	}
	for name := range o.config.MCP.Servers {
		if sanitizeMCPIdentifier(name) == key {
			return name
		}
	}
	return ""
}

func (o *Orchestrator) GetActiveMCPServers() []string {
	o.activeMCPMu.RLock()
	defer o.activeMCPMu.RUnlock()
	if len(o.activeMCPServers) == 0 {
		return nil
	}
	return append([]string(nil), o.activeMCPServers...)
}

func extractMCPSanitizedServer(name string) string {
	if !strings.HasPrefix(name, "mcp_") {
		return ""
	}
	parts := strings.SplitN(name, "_", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func sanitizeMCPIdentifier(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	prevUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// StatusCallback is called to update the UI with processing status
type StatusCallback func(status string) error

// ContextUsageCallback receives free context percentage and total context window updates for UI display
type ContextUsageCallback func(freePercent int, contextWindow int) error

// ProcessPrompt processes a user prompt
func (o *Orchestrator) ProcessPrompt(ctx context.Context, prompt string, streamCallback func(string) error, statusCallback StatusCallback, contextCallback ContextUsageCallback, authCallback AuthorizationCallback, toolCallCallback ToolCallCallback, toolResultCallback ToolResultCallback) error {
	combinedCtx, cancel := combineContexts(ctx, o.ctx)
	if cancel != nil {
		defer cancel()
	}
	ctx = combinedCtx

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

	// Reset loop detector for new prompt
	o.loopDetector.Reset()
	logger.Debug("Loop detector reset for new prompt")

	// Build system prompt
	modelID := o.providerMgr.GetOrchestrationModel()
	promptBuilder := llm.NewPromptBuilder(o.fs, o.workingDir, o.config)
	systemPrompt, err := promptBuilder.BuildSystemPrompt(ctx, modelID, o.cliMode)
	if err != nil {
		return fmt.Errorf("failed to build system prompt: %w", err)
	}

	// Broadcast initial context usage after recording the user message
	o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

	if o.toolSelectionDirty {
		if errs := o.rebuildTools(true); len(errs) > 0 {
			for _, err := range errs {
				if err != nil {
					logger.Warn("Tool selection refresh warning: %v", err)
					if msg := err.Error(); strings.Contains(msg, "unexpected response format") {
						val, ok := err.(interface{ Response() string })
						if ok && val.Response() != "" {
							logger.Warn("Tool selection summarizer response: %s", val.Response())
						}
					}
				}
			}
		}
	}

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

		// Check for text loops in the response
		if response.Content != "" {
			isLoop, pattern, count := o.loopDetector.AddText(response.Content)
			if isLoop {
				logger.Warn("Text loop detected at iteration %d: pattern repeated %d times", iteration, count)
				if streamCallback != nil {
					// Show a truncated version of the pattern to the user
					displayPattern := pattern
					if len(displayPattern) > 100 {
						displayPattern = displayPattern[:100] + "..."
					}
					_ = streamCallback(fmt.Sprintf("\n\n🔁 Loop detected! The LLM is repeating the same text pattern %d times.\nPattern: %s\nStopping generation to prevent infinite loop.\n", count, displayPattern))
				}
				logger.Debug("Breaking out of loop due to text repetition (iteration %d)", iteration)
				break
			}
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

			toolCallObj := &tools.ToolCall{
				ID:         toolID,
				Name:       toolName,
				Parameters: args,
			}

			// Notify UI about tool call details
			if toolCallCallback != nil {
				if err := toolCallCallback(toolName, toolID, args); err != nil {
					logger.Warn("Failed to send tool call message: %v", err)
				}
			}

			result, execErr := o.executeTool(ctx, toolCallObj, toolName, statusCallback)
			if execErr != nil {
				result = &tools.ToolResult{
					ID:    toolID,
					Error: execErr.Error(),
				}
			}

			// Check if authorization is required
			if result.RequiresUserInput {
				// Ask user for approval
				approved := false
				if authCallback != nil {
					var err error
					suggestedPrefix := result.SuggestedCommandPrefix
					approved, err = authCallback(toolName, args, result.AuthReason)
					if err != nil {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: fmt.Sprintf("Authorization error: %v", err),
						}
					} else if approved {
						if suggestedPrefix != "" {
							o.session.AuthorizeCommand(suggestedPrefix)
							logger.Info("Authorized command prefix for session: %q", suggestedPrefix)
							if o.config != nil && !o.config.IsCommandAuthorized(suggestedPrefix) {
								o.config.AuthorizeCommand(suggestedPrefix)
								if err := o.config.Save(config.GetConfigPath()); err != nil {
									logger.Warn("Failed to persist authorized command prefix %q: %v", suggestedPrefix, err)
								} else {
									logger.Info("Persisted authorized command prefix %q to config", suggestedPrefix)
								}
							}
						}

						result, execErr = o.executeToolWithApproval(ctx, toolCallObj, toolName, statusCallback)
						if execErr != nil {
							result = &tools.ToolResult{
								ID:    toolID,
								Error: execErr.Error(),
							}
						}
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
			var executionMetadata *tools.ExecutionMetadata

			if result.Error != "" {
				toolResult = fmt.Sprintf("Error: %s", result.Error)
			} else {
				toolResult = fmt.Sprintf("%v", result.Result)

				// Extract execution metadata if present in the result
				if resultMap, ok := result.Result.(map[string]interface{}); ok {
					if metadata, hasMetadata := resultMap["_execution_metadata"]; hasMetadata {
						if metadataObj, ok := metadata.(*tools.ExecutionMetadata); ok {
							executionMetadata = metadataObj
						}
					}
				}
			}

			// Add tool result to session
			// Notify UI about tool result
			if toolResultCallback != nil {
				// Create enhanced callback that includes metadata
				if err := o.enhancedToolResultCallback(toolResultCallback, toolName, toolID, toolResult, result.Error, executionMetadata); err != nil {
					logger.Warn("Failed to send tool result message: %v", err)
				}
			}
			o.session.AddMessage(&session.Message{
				Role:     "tool",
				Content:  toolResult,
				ToolID:   toolID,
				ToolName: toolName,
			})
			o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

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

func (o *Orchestrator) maybeCompactContext(modelID, systemPrompt string, sessionMessages []*session.Message, perMessageTokens []int, totalTokens int, streamCallback func(string) error, contextCallback ContextUsageCallback) {
	if len(sessionMessages) < 4 {
		return
	}

	contextWindow := o.getContextWindow(modelID)
	if contextWindow <= 0 {
		return
	}

	if totalTokens*100/contextWindow < 90 {
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

	contextWindow := o.getContextWindow(modelID)
	_, perMessageTokens, _ := estimateContextTokens(modelID, "", messages)
	latestUserPrompt := findLatestUserPrompt(o.session.GetMessages())

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
	userSection := buildUserCompactionSection(messages, perMessageTokens, contextWindow, latestUserPrompt)
	if userSection != "" {
		summaryContent = fmt.Sprintf("%s\n\n%s", summaryContent, userSection)
	}

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

func buildUserCompactionSection(messages []*session.Message, perMessageTokens []int, contextWindow int, latestUserPrompt string) string {
	trimmedLatest := strings.TrimSpace(latestUserPrompt)
	userPrompts := make([]string, 0)

	userTokens := 0
	totalTokens := 0
	tokenSliceMatches := len(perMessageTokens) == len(messages)

	for i, msg := range messages {
		token := 1
		if tokenSliceMatches {
			token = perMessageTokens[i]
			if token <= 0 {
				token = 1
			}
		}
		totalTokens += token

		if strings.EqualFold(msg.Role, "user") {
			content := strings.TrimSpace(msg.Content)
			if content != "" {
				userPrompts = append(userPrompts, content)
				userTokens += token
			}
		}
	}

	if len(userPrompts) == 0 && trimmedLatest == "" {
		return ""
	}

	windowTokens := contextWindow
	if windowTokens <= 0 {
		windowTokens = totalTokens
	}
	if windowTokens <= 0 {
		windowTokens = 1
	}

	userPercent := (userTokens * 100) / windowTokens

	var sb strings.Builder
	if len(userPrompts) > 0 {
		sb.WriteString("User prompt compaction:\n")
		if userPercent < 5 {
			sb.WriteString("Older prompts (<5% of context) unified verbatim:\n")
			sb.WriteString(strings.Join(userPrompts, "\n---\n"))
		} else {
			sb.WriteString("Older prompts (>=5% of context) condensed summary:\n")
			sb.WriteString(compactUserPrompts(userPrompts))
		}
		if trimmedLatest != "" {
			sb.WriteString("\n\n")
		}
	}

	if trimmedLatest != "" {
		sb.WriteString("Latest user prompt (preserve and continue):\n")
		sb.WriteString(trimmedLatest)
		sb.WriteString("\nContinue to implement this.")
	}

	return strings.TrimSpace(sb.String())
}

func compactUserPrompts(prompts []string) string {
	if len(prompts) == 0 {
		return ""
	}

	if len(prompts) == 1 {
		return condenseContent(prompts[0], 400)
	}

	var sb strings.Builder
	for i, prompt := range prompts {
		sb.WriteString(fmt.Sprintf("- #%d: %s\n", i+1, condenseContent(prompt, 200)))
	}

	return strings.TrimSpace(sb.String())
}

func findLatestUserPrompt(messages []*session.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
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

// combineContexts returns a cancelable context that is cancelled when either input context is done.
func combineContexts(primary, secondary context.Context) (context.Context, context.CancelFunc) {
	switch {
	case primary == nil && secondary == nil:
		return context.WithCancel(context.Background())
	case primary == nil:
		return context.WithCancel(secondary)
	case secondary == nil:
		return context.WithCancel(primary)
	default:
		combined, cancel := context.WithCancel(primary)
		go func() {
			select {
			case <-combined.Done():
			case <-secondary.Done():
				cancel()
			}
		}()
		return combined, cancel
	}
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

	if o.toolExecutorCancel != nil {
		o.toolExecutorCancel()
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

// enhancedToolResultCallback forwards tool results with metadata to the UI
func (o *Orchestrator) enhancedToolResultCallback(callback ToolResultCallback, toolName, toolID, result, errorMsg string, metadata *tools.ExecutionMetadata) error {
	// For now, call the original callback
	// In the future, we can extend the callback interface to include metadata
	if err := callback(toolName, toolID, result, errorMsg); err != nil {
		return err
	}

	// TODO: Store metadata for TUI access when we extend the callback interface
	// This could be done via a side channel, context, or enhanced callback signature

	return nil
}

func (o *Orchestrator) executeTool(ctx context.Context, toolCall *tools.ToolCall, toolName string, statusCallback StatusCallback) (*tools.ToolResult, error) {
	ctx, cleanup := o.prepareShellExecutionContext(ctx, toolCall, toolName)
	if cleanup != nil {
		defer cleanup()
	}

	if o.toolExecutor != nil {
		var cb func(string) error
		if statusCallback != nil {
			cb = statusCallback
		}
		return o.toolExecutor.Execute(ctx, toolCall, toolName, cb)
	}
	// Fallback to direct execution if tool executor is unavailable
	return o.toolRegistry.Execute(ctx, toolCall), nil
}

func (o *Orchestrator) executeToolWithApproval(ctx context.Context, toolCall *tools.ToolCall, toolName string, statusCallback StatusCallback) (*tools.ToolResult, error) {
	ctx, cleanup := o.prepareShellExecutionContext(ctx, toolCall, toolName)
	if cleanup != nil {
		defer cleanup()
	}

	if o.toolExecutor != nil {
		var cb func(string) error
		if statusCallback != nil {
			cb = statusCallback
		}
		return o.toolExecutor.ExecuteWithApproval(ctx, toolCall, toolName, cb)
	}
	// Fallback to direct execution if tool executor is unavailable
	return o.toolRegistry.ExecuteWithApproval(ctx, toolCall), nil
}

func (o *Orchestrator) prepareShellExecutionContext(ctx context.Context, toolCall *tools.ToolCall, toolName string) (context.Context, func()) {
	if toolName != "shell" || toolCall == nil {
		return ctx, nil
	}

	if tools.GetBoolParam(toolCall.Parameters, "background", false) {
		return ctx, nil
	}

	ch := make(chan struct{}, 1)
	o.setActiveShellChannel(ch)
	newCtx := tools.ContextWithShellBackground(ctx, ch)
	return newCtx, func() {
		o.clearActiveShellChannel(ch)
	}
}

func (o *Orchestrator) setActiveShellChannel(ch chan struct{}) {
	o.activeShellMu.Lock()
	o.activeShellChan = ch
	o.activeShellMu.Unlock()
}

func (o *Orchestrator) clearActiveShellChannel(ch chan struct{}) {
	o.activeShellMu.Lock()
	if o.activeShellChan == ch {
		o.activeShellChan = nil
	}
	o.activeShellMu.Unlock()
}

// BackgroundCurrentShellJob requests that the currently running foreground shell command continue in the background.
func (o *Orchestrator) BackgroundCurrentShellJob() error {
	o.activeShellMu.Lock()
	ch := o.activeShellChan
	o.activeShellMu.Unlock()

	if ch == nil {
		return fmt.Errorf("no active foreground shell command to background")
	}

	select {
	case ch <- struct{}{}:
		return nil
	default:
		return fmt.Errorf("shell command is already transitioning to background")
	}
}

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
	o.preconnectMu.Lock()
	o.preconnectCompleted = false
	o.preconnectMu.Unlock()
	return o.initializeClients()
}

// TriggerPreconnect initializes missing LLM clients and warms provider TLS connections in the background.
// It is safe to call frequently; the work is throttled and only runs until a successful warmup completes.
func (o *Orchestrator) TriggerPreconnect() {
	if o == nil {
		return
	}

	o.preconnectMu.Lock()
	if o.preconnectCompleted {
		o.preconnectMu.Unlock()
		return
	}
	if o.preconnectInFlight {
		o.preconnectMu.Unlock()
		return
	}
	if time.Since(o.lastPreconnectAttempt) < preconnectThrottle {
		o.preconnectMu.Unlock()
		return
	}
	needClients := o.orchestrationClient == nil || o.summarizeClient == nil
	o.preconnectInFlight = true
	o.lastPreconnectAttempt = time.Now()
	o.preconnectMu.Unlock()

	go func() {
		if needClients {
			if err := o.initializeClients(); err != nil {
				logger.Debug("LLM preconnect attempt failed: %v", err)
			}
		}
		modelIDs := []string{
			o.providerMgr.GetOrchestrationModel(),
			o.getSummarizeModelID(),
		}
		attempted, warmed := o.providerMgr.WarmConnections(o.ctx, modelIDs...)
		o.preconnectMu.Lock()
		o.preconnectInFlight = false
		if warmed {
			o.preconnectCompleted = true
		} else if !attempted && !needClients {
			o.lastPreconnectAttempt = time.Time{}
		}
		o.preconnectMu.Unlock()
	}()
}

// GetContextFile returns the context file used to prime the LLM, if available.
func (o *Orchestrator) GetContextFile() string {
	exists, err := o.fs.Exists(o.ctx, llm.AgentsFileName)
	if err != nil || !exists {
		return ""
	}
	return llm.AgentsFileName
}

// GetExtendedContextFile returns the standard context file if present, otherwise falls back to README variants.
func (o *Orchestrator) GetExtendedContextFile() string {
	if path := o.GetContextFile(); path != "" {
		return path
	}

	candidates := []string{"README.md", "README.txt", "README"}
	for _, candidate := range candidates {
		exists, err := o.fs.Exists(o.ctx, candidate)
		if err != nil {
			logger.Debug("extended context check failed for %s: %v", candidate, err)
			continue
		}
		if exists {
			return candidate
		}
	}
	return ""
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
