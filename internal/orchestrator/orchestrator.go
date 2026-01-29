package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/features"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
	"github.com/codefionn/scriptschnell/internal/mcp"
	"github.com/codefionn/scriptschnell/internal/planning"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/safety"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/summarizer"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// AuthorizationCallback is called when a tool requires user authorization
// It should return true if approved, false if denied
type AuthorizationCallback func(toolName string, params map[string]interface{}, reason string) (bool, error)

// ToolCallCallback is called when a tool is being executed
type ToolCallCallback func(toolName, toolID string, parameters map[string]interface{}) error

// ToolResultCallback is called when a tool execution completes
type ToolResultCallback func(toolName, toolID, result, errorMsg string) error

type ProgressCallback = progress.Callback
type ProgressUpdate = progress.Update

// UserInputCallback is called when the planning agent needs user input
type UserInputCallback = planning.UserInputCallback

// Orchestrator manages the LLM interaction
type Orchestrator struct {
	fs                     fs.FileSystem
	session                *session.Session
	providerMgr            *provider.Manager
	toolRegistry           *tools.Registry
	orchestrationClient    llm.Client
	summarizeClient        llm.Client
	planningClient         llm.Client
	safetyEvaluator        *safety.Evaluator
	config                 *config.Config
	workingDir             string
	ctx                    context.Context
	cancel                 context.CancelFunc
	actorSystem            *actor.System
	authorizer             tools.Authorizer
	actorCancel            context.CancelFunc
	compactionMu           sync.Mutex
	compactionInProgress   bool
	cliMode                bool
	todoClient             *tools.TodoActorClient
	todoActor              tools.TodoActorInterface
	todoActorCancel        context.CancelFunc
	currentProgressCb      progress.Callback
	progressCbMu           sync.Mutex
	errorJudge             *tools.ErrorJudgeActorClient
	errorJudgeCancel       context.CancelFunc
	toolExecutor           *tools.ToolExecutorActorClient
	toolExecutorCancel     context.CancelFunc
	shellActorClient       *actor.ShellActorClient
	shellActorCancel       context.CancelFunc
	domainBlockerRef       *actor.ActorRef
	domainBlockerCancel    context.CancelFunc
	domainBlockerClient    *actor.DomainBlockerClient
	sessionStorageRef      *actor.ActorRef
	sessionStorageCancel   context.CancelFunc
	activeShellMu          sync.Mutex
	activeShellChan        chan struct{}
	loopDetector           *loopdetector.LoopDetector
	mcpManager             *mcp.Manager
	toolSelectionDirty     bool
	activeMCPServers       []string
	activeMCPMu            sync.RWMutex
	preconnectMu           sync.Mutex
	preconnectInFlight     bool
	lastPreconnectAttempt  time.Time
	preconnectCompleted    bool
	clientInitMu           sync.Mutex
	cachedSystemPrompt     string
	systemPromptMu         sync.RWMutex
	healthManager          *actor.SessionHealthManager
	planningAgent          *planning.PlanningAgent
	planningAgentCancel    context.CancelFunc
	featureFlags           *features.FeatureFlags
	compactionAttemptCount int // Tracks compaction attempts for current request
	compactionAttemptMu    sync.Mutex
	userInputCb            UserInputCallback
	userInteractionRef     *actor.ActorRef
	userInteractionCancel  context.CancelFunc
	userInteractionClient  *actor.UserInteractionClient
	userInteractionTabID   int
	userInteractionTabIDMu sync.Mutex
}

const (
	defaultAutoContinueMaxAttempts  = 3
	kimiK2AutoContinueMaxAttempts   = 32
	minimaxAutoContinueMaxAttempts  = 32
	deepSeekAutoContinueMaxAttempts = 12
	errorRetryMaxAttempts           = 5
	preconnectThrottle              = 2 * time.Second
)

// Multi-stage compaction configuration
const (
	// Compaction threshold: trigger compaction when context usage exceeds this percentage
	compactionThresholdPercent = 90
	// Re-compaction threshold: after first compaction, if still above this, retry with forceful prompt
	recompactionThresholdPercent = 80
	// Maximum number of compaction attempts for a single request
	maxCompactionAttempts = 3
	// Max summary bytes for each compaction attempt (decreases with each attempt for more aggressive summarization)
	compactionMaxBytesAttempt1 = 16_384 // 16KB
	compactionMaxBytesAttempt2 = 8_192  // 8KB
	compactionMaxBytesAttempt3 = 4_096  // 4KB
)

// Compaction prompts for each attempt
var (
	// Standard compaction prompt - balanced approach
	compactionPromptStandard = "Summarize the earlier part of this conversation so the assistant can continue later without losing context. Capture tasks in progress, key decisions, file operations, and remaining follow-ups. Use concise bullet points; preserve critical commands, file paths, and TODOs."

	// Forceful compaction prompt - maximum brevity
	compactionPromptForceful = "EXTREMELY CONCISE SUMMARY REQUIRED. Summarize this conversation in the fewest words possible while preserving only the ESSENTIAL information needed to continue work. Focus on: current task, completed actions, and next steps only. Use ultra-compact bullet points with no explanations or background. Omit all non-critical details. Target: under 50 words per major topic."

	// Extreme compaction prompt - minimal viable summary
	compactionPromptExtreme = "MINIMAL SUMMARY - ABSOLUTE BREVITY. Create the smallest possible summary preserving only: (1) current primary task, (2) any completed file operations with paths, (3) immediate next step. Use single words or short phrases only. No sentences. No explanations. No background. Maximum 5 bullet points total. Each bullet maximum 10 words."
)

var toolCallValidationRegex = regexp.MustCompile(`(?i)tool call validation failed.*?missing required parameter ['"]?([^'"]+)['"]?.*?in ['"]?([^'"]+)['"]?(?: function call)?`)
var toolNotInRequestRegex = regexp.MustCompile(`(?i)tool call validation failed.*?attempted to call tool ['"]?([^'"]+)['"]?.*?which was not in request\.tools`)

// @fileReferenceRegex matches @filename pattern for file inclusion
// Supports: @filename, @filename.ext, @path/to/file, @path/to/file.ext
var fileReferenceRegex = regexp.MustCompile(`@([\w\-./]+)`)

type toolCallValidationError struct {
	toolName     string
	missingParam string
	inner        error
}

func (e *toolCallValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.inner != nil {
		return fmt.Sprintf("tool call validation failed for %s: missing %s: %v", e.toolName, e.missingParam, e.inner)
	}
	return fmt.Sprintf("tool call validation failed for %s: missing %s", e.toolName, e.missingParam)
}

func (e *toolCallValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.inner
}

// contextSizeExceededError indicates that the context size was exceeded and compaction should be triggered
type contextSizeExceededError struct {
	inner  error
	reason string
}

func (e *contextSizeExceededError) Error() string {
	if e == nil {
		return ""
	}
	if e.inner != nil {
		return fmt.Sprintf("context size exceeded: %s: %v", e.reason, e.inner)
	}
	return fmt.Sprintf("context size exceeded: %s", e.reason)
}

func (e *contextSizeExceededError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.inner
}

type toolExecutionFunc func(ctx context.Context, call *tools.ToolCall, toolName string, progressCb progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback, approved bool) (*tools.ToolResult, error)

func NewOrchestrator(cfg *config.Config, providerMgr *provider.Manager, cliMode bool) (*Orchestrator, error) {
	return NewOrchestratorWithFS(cfg, providerMgr, cliMode, nil)
}

// NewOrchestratorWithTodoActor creates a new orchestrator with a custom todo actor
func NewOrchestratorWithTodoActor(cfg *config.Config, providerMgr *provider.Manager, cliMode bool, customTodoActor tools.TodoActorInterface) (*Orchestrator, error) {
	return NewOrchestratorWithFSAndTodoActor(cfg, providerMgr, cliMode, nil, customTodoActor)
}

func NewOrchestratorWithFS(cfg *config.Config, providerMgr *provider.Manager, cliMode bool, customFS fs.FileSystem) (*Orchestrator, error) {
	return NewOrchestratorWithFSAndTodoActor(cfg, providerMgr, cliMode, customFS, nil)
}

// NewOrchestratorWithFSAndTodoActor creates a new orchestrator with custom filesystem and todo actor
func NewOrchestratorWithFSAndTodoActor(cfg *config.Config, providerMgr *provider.Manager, cliMode bool, customFS fs.FileSystem, customTodoActor tools.TodoActorInterface) (*Orchestrator, error) {
	logger.Debug("Creating new orchestrator with working_dir=%s, cliMode=%v", cfg.WorkingDir, cliMode)
	ctx, cancel := context.WithCancel(context.Background())

	// Create filesystem (use custom if provided, otherwise create default)
	var filesystem fs.FileSystem
	if customFS != nil {
		filesystem = customFS
		logger.Debug("Using custom filesystem")
	} else {
		filesystem = fs.NewCachedFS(
			cfg.WorkingDir,
			time.Duration(cfg.CacheTTL)*time.Second,
			cfg.MaxCacheEntries,
		)
		logger.Debug("Filesystem initialized with cache_ttl=%ds", cfg.CacheTTL)
	}

	// Create session
	sess := session.NewSession(session.GenerateID(), cfg.WorkingDir)
	logger.Debug("Session created: id=%s", sess.ID)

	// Create orchestrator (without authorization actor yet)
	orch := &Orchestrator{
		fs:            filesystem,
		session:       sess,
		providerMgr:   providerMgr,
		config:        cfg,
		workingDir:    cfg.WorkingDir,
		ctx:           ctx,
		cancel:        cancel,
		actorSystem:   actor.NewSystem(),
		cliMode:       cliMode,
		loopDetector:  loopdetector.NewLoopDetector(),
		featureFlags:  features.NewFeatureFlags(),
		healthManager: actor.NewSessionHealthManager(actor.NewSystem(), ""),
	}
	orch.mcpManager = mcp.NewManager(cfg, cfg.WorkingDir, providerMgr)

	// Initialize clients first so we have summarize client for authorization actor
	if err := orch.initializeClients(); err != nil {
		// Non-fatal, user can configure later
		logger.Warn("Failed to initialize clients: %v", err)
	}

	// Initialize safety evaluator
	if orch.summarizeClient != nil {
		orch.safetyEvaluator = safety.NewEvaluator(orch.summarizeClient)
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
	baseAuthorizer := tools.NewAuthorizationActorClient(authRef)

	// Create secret detector and wrap authorizer for secret-based authorization
	if features.Enabled["secret_based_auth"] {
		orch.authorizer = tools.NewSecretAwareAuthorizer(baseAuthorizer, authActor)
	} else {
		orch.authorizer = baseAuthorizer
	}
	orch.actorCancel = authorizationCancel

	// Set up todo actor
	todoCtx, todoCancel := context.WithCancel(context.Background())
	var todoActor actor.Actor
	if customTodoActor != nil {
		todoActor = customTodoActor
		logger.Debug("Using custom todo actor")
	} else {
		todoActor = tools.NewTodoActor("todo")
		logger.Debug("Using default todo actor")
	}
	todoRef, err := orch.actorSystem.SpawnWithOptions(todoCtx, "todo", todoActor, 16, actor.WithSequentialProcessing())
	if err != nil {
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start todo actor: %v", err)
		return nil, fmt.Errorf("failed to start todo actor: %w", err)
	}
	logger.Debug("Todo actor spawned")
	orch.todoClient = tools.NewTodoActorClient(todoRef)
	if customTodoActor != nil {
		orch.todoActor = customTodoActor
	} else {
		// Cast the default todo actor to the interface
		orch.todoActor = todoActor.(tools.TodoActorInterface)
	}
	orch.todoActorCancel = todoCancel

	// Set up shell actor for shell execution
	shellCtx, shellCancel := context.WithCancel(context.Background())
	shellActor := actor.NewShellActor("shell", sess)
	shellRef, err := orch.actorSystem.Spawn(shellCtx, "shell", shellActor, 64)
	if err != nil {
		shellCancel()
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start shell actor: %v", err)
		return nil, fmt.Errorf("failed to start shell actor: %w", err)
	}
	logger.Debug("Shell actor spawned")
	orch.shellActorClient = actor.NewShellActorClient(shellRef)
	orch.shellActorCancel = shellCancel

	// Set up domain blocker actor
	domainBlockerCtx, domainBlockerCancel := context.WithCancel(context.Background())
	domainBlockerConfig := actor.DomainBlockerConfig{
		BlocklistURL:    actor.DefaultRPZURL,
		RefreshInterval: 6 * time.Hour,  // Refresh every 6 hours
		TTL:             24 * time.Hour, // Blocklist expires after 24 hours
		HTTPClient:      &http.Client{Timeout: 30 * time.Second},
	}
	domainBlockerActor := actor.NewDomainBlockerActor("domain_blocker", domainBlockerConfig)
	domainBlockerRef, err := orch.actorSystem.Spawn(domainBlockerCtx, "domain_blocker", domainBlockerActor, 16)
	if err != nil {
		domainBlockerCancel()
		shellCancel()
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start domain blocker actor: %v", err)
		return nil, fmt.Errorf("failed to start domain blocker actor: %w", err)
	}
	logger.Debug("Domain blocker actor spawned")
	orch.domainBlockerRef = domainBlockerRef
	orch.domainBlockerCancel = domainBlockerCancel
	orch.domainBlockerClient = actor.NewDomainBlockerClient(domainBlockerRef)

	// Set up session storage actor
	configFunc := func() *config.AutoSaveConfig {
		return &orch.config.AutoSave
	}
	sessionStorageActor, err := actor.NewSessionStorageActorWithConfig("session_storage", configFunc)
	if err != nil {
		shellCancel()
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to create session storage actor: %v", err)
		return nil, fmt.Errorf("failed to create session storage actor: %w", err)
	}
	// Create managed context for session storage actor (like other actors)
	sessionStorageCtx, sessionStorageCancel := context.WithCancel(context.Background())
	sessionStorageRef, err := orch.actorSystem.Spawn(sessionStorageCtx, "session_storage", sessionStorageActor, 16)
	if err != nil {
		sessionStorageCancel()
		shellCancel()
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start session storage actor: %v", err)
		return nil, fmt.Errorf("failed to start session storage actor: %w", err)
	}
	logger.Debug("Session storage actor spawned")
	orch.sessionStorageRef = sessionStorageRef
	orch.sessionStorageCancel = sessionStorageCancel

	// Start autosave for the initial session if enabled
	if orch.config.AutoSave.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		sessionName := actor.GenerateSessionName("")
		if err := actor.StartAutoSaveViaActor(ctx, orch.sessionStorageRef, orch.session, sessionName); err != nil {
			logger.Warn("Failed to start autosave for initial session: %v", err)
		} else {
			logger.Info("Started autosave for session %s", orch.session.ID)
		}
		cancel()
	}

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

// NewOrchestratorWithSharedResources creates an orchestrator with shared resources
// Used by RuntimeFactory for per-tab orchestrators with shared session storage, domain blocker, and filesystem
func NewOrchestratorWithSharedResources(
	cfg *config.Config,
	providerMgr *provider.Manager,
	cliMode bool,
	sharedFS fs.FileSystem,
	sess *session.Session,
	sharedSessionStorage *actor.ActorRef,
	sharedDomainBlocker *actor.ActorRef,
) (*Orchestrator, error) {
	logger.Debug("Creating orchestrator with shared resources for session %s", sess.ID)
	ctx, cancel := context.WithCancel(context.Background())

	// Use provided shared filesystem
	filesystem := sharedFS
	logger.Debug("Using shared filesystem")

	// Create orchestrator (without authorization actor yet)
	orch := &Orchestrator{
		fs:            filesystem,
		session:       sess, // Use provided session instead of creating new one
		providerMgr:   providerMgr,
		config:        cfg,
		workingDir:    sess.WorkingDir,
		ctx:           ctx,
		cancel:        cancel,
		actorSystem:   actor.NewSystem(),
		cliMode:       cliMode,
		loopDetector:  loopdetector.NewLoopDetector(),
		featureFlags:  features.NewFeatureFlags(),
		healthManager: actor.NewSessionHealthManager(actor.NewSystem(), ""),
	}
	orch.mcpManager = mcp.NewManager(cfg, sess.WorkingDir, providerMgr)

	// Initialize clients first so we have summarize client for authorization actor
	if err := orch.initializeClients(); err != nil {
		// Non-fatal, user can configure later
		logger.Warn("Failed to initialize clients: %v", err)
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
	baseAuthorizer := tools.NewAuthorizationActorClient(authRef)

	// Create secret detector and wrap authorizer for secret-based authorization
	if features.Enabled["secret_based_auth"] {
		orch.authorizer = tools.NewSecretAwareAuthorizer(baseAuthorizer, authActor)
	} else {
		orch.authorizer = baseAuthorizer
	}
	orch.actorCancel = authorizationCancel

	// Set up todo actor
	todoCtx, todoCancel := context.WithCancel(context.Background())
	var todoActor actor.Actor = tools.NewTodoActor("todo")
	logger.Debug("Using default todo actor")
	todoRef, err := orch.actorSystem.SpawnWithOptions(todoCtx, "todo", todoActor, 16, actor.WithSequentialProcessing())
	if err != nil {
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start todo actor: %v", err)
		return nil, fmt.Errorf("failed to start todo actor: %w", err)
	}
	logger.Debug("Todo actor spawned")
	orch.todoClient = tools.NewTodoActorClient(todoRef)
	orch.todoActor = todoActor.(tools.TodoActorInterface)
	orch.todoActorCancel = todoCancel

	// Set up shell actor for shell execution
	shellCtx, shellCancel := context.WithCancel(context.Background())
	shellActor := actor.NewShellActor("shell", sess)
	shellRef, err := orch.actorSystem.Spawn(shellCtx, "shell", shellActor, 64)
	if err != nil {
		shellCancel()
		todoCancel()
		authorizationCancel()
		cancel()
		logger.Error("Failed to start shell actor: %v", err)
		return nil, fmt.Errorf("failed to start shell actor: %w", err)
	}
	logger.Debug("Shell actor spawned")
	orch.shellActorClient = actor.NewShellActorClient(shellRef)
	orch.shellActorCancel = shellCancel

	// Use shared domain blocker actor - DO NOT create new one
	orch.domainBlockerRef = sharedDomainBlocker
	orch.domainBlockerCancel = nil // Don't own the cancel function - shared lifecycle
	orch.domainBlockerClient = actor.NewDomainBlockerClient(sharedDomainBlocker)
	logger.Debug("Using shared domain blocker actor")

	// Use shared session storage actor - DO NOT create new one
	orch.sessionStorageRef = sharedSessionStorage
	orch.sessionStorageCancel = nil // Don't own the cancel function - shared lifecycle
	logger.Debug("Using shared session storage actor")

	// Start autosave for this session if enabled
	if orch.config.AutoSave.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		sessionName := actor.GenerateSessionName("")
		if err := actor.StartAutoSaveViaActor(ctx, orch.sessionStorageRef, orch.session, sessionName); err != nil {
			logger.Warn("Failed to start autosave for session %s: %v", orch.session.ID, err)
		} else {
			logger.Info("Started autosave for session %s", orch.session.ID)
		}
		cancel()
	}

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

	logger.Info("Orchestrator created successfully with shared resources for session %s", sess.ID)
	return orch, nil
}

func (o *Orchestrator) initializeClients() error {
	o.clientInitMu.Lock()
	defer o.clientInitMu.Unlock()

	orchModelID := o.providerMgr.GetOrchestrationModel()
	summModelID := o.providerMgr.GetSummarizeModel()
	planModelID := o.providerMgr.GetPlanningModel()

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

	var planningErr error
	if planModelID != "" {
		client, err := o.providerMgr.CreateClient(planModelID)
		if err != nil {
			planningErr = fmt.Errorf("failed to create planning client: %w", err)
		} else {
			o.planningClient = client
		}
	}

	// Fallbacks when no separate clients are configured
	if o.summarizeClient == nil && o.orchestrationClient != nil {
		o.summarizeClient = o.orchestrationClient
	}
	if o.planningClient == nil && o.summarizeClient != nil {
		o.planningClient = o.summarizeClient
	} else if o.planningClient == nil && o.orchestrationClient != nil {
		o.planningClient = o.orchestrationClient
	}

	// Initialize planning agent if planning client is available
	if o.planningClient != nil && o.planningAgent == nil {
		o.initializePlanningAgent()
	}

	// Return first error encountered
	if summarizeErr != nil {
		return summarizeErr
	}
	return planningErr
}

func (o *Orchestrator) getSummarizeModelID() string {
	modelID := o.providerMgr.GetSummarizeModel()
	if modelID == "" {
		modelID = o.providerMgr.GetOrchestrationModel()
	}
	return modelID
}

// expandFileReferences expands @file references in the prompt by including file content directly.
// Files smaller than 10% of the context window are included directly.
// Returns the expanded prompt with @file references replaced by their content.
func (o *Orchestrator) expandFileReferences(ctx context.Context, prompt string) string {
	// Find all @file references in the prompt
	matches := fileReferenceRegex.FindAllStringSubmatch(prompt, -1)
	if len(matches) == 0 {
		return prompt
	}

	// Get the context window size for the current model
	modelID := o.providerMgr.GetOrchestrationModel()
	contextWindow := o.getContextWindow(modelID)
	if contextWindow <= 0 {
		contextWindow = 8192 // Default fallback
	}

	// Calculate 10% of context window as the threshold for direct inclusion
	thresholdTokens := contextWindow / 10
	thresholdBytes := thresholdTokens * 4 // Approximate 4 chars per token

	// Track files we've already expanded to avoid duplicates
	expandedFiles := make(map[string]bool)

	// Build the expanded prompt by replacing @file references
	expandedPrompt := prompt
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		filePath := match[1]
		if expandedFiles[filePath] {
			continue
		}

		// Try to read the file
		content, err := o.fs.ReadFile(ctx, filePath)
		if err != nil {
			logger.Debug("@file expansion: could not read file %s: %v", filePath, err)
			// Keep the @file reference as-is if file doesn't exist or can't be read
			expandedFiles[filePath] = true
			continue
		}

		// Check if file is small enough to include directly
		fileSize := len(content)
		if fileSize <= thresholdBytes {
			// Include file content directly (only replace first occurrence to handle duplicates)
			fileMarker := fmt.Sprintf("@%s", filePath)
			fileInsertion := fmt.Sprintf("@%s\n---\n%s\n---", filePath, string(content))
			expandedPrompt = strings.Replace(expandedPrompt, fileMarker, fileInsertion, 1)
			logger.Debug("@file expansion: included %s (%d bytes, below threshold of %d bytes)", filePath, fileSize, thresholdBytes)
		} else {
			// File too large - keep the @file reference so LLM can use read_file tool
			logger.Debug("@file expansion: skipping %s (%d bytes exceeds threshold of %d bytes)", filePath, fileSize, thresholdBytes)
		}

		expandedFiles[filePath] = true
	}

	return expandedPrompt
}

// detectProviderChange checks if the provider/model family has changed
func (o *Orchestrator) detectProviderChange(modelID string) (provider, modelFamily string, changed bool) {
	converter := llm.GetConverter(modelID)
	if converter == nil {
		return "", "", false
	}

	provider = converter.GetProviderName()
	modelFamily = converter.GetModelFamily(modelID)

	changed = o.session.NeedsConversion(provider, modelFamily)

	return provider, modelFamily, changed
}

// convertSessionMessages converts all session messages to native format for the current provider
func (o *Orchestrator) convertSessionMessages(modelID, provider, modelFamily string) error {
	converter := llm.GetConverter(modelID)
	if converter == nil || !converter.SupportsNativeStorage() {
		return nil
	}

	messages := o.session.GetMessages()
	if len(messages) == 0 {
		return nil
	}

	// Convert unified → native
	unifiedMsgs := make([]*llm.Message, len(messages))
	for i, msg := range messages {
		unifiedMsgs[i] = &llm.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			ToolCalls: msg.ToolCalls,
			ToolID:    msg.ToolID,
			ToolName:  msg.ToolName,
		}
	}

	// Get system prompt for conversion context
	systemPrompt, err := o.getOrBuildSystemPrompt(context.Background(), modelID)
	if err != nil {
		systemPrompt = "" // Fall back to empty system prompt
	}

	nativeMessages, err := converter.ConvertToNative(unifiedMsgs, systemPrompt, o.config.EnablePromptCache, o.config.PromptCacheTTL)
	if err != nil {
		return fmt.Errorf("failed to convert messages to native format: %w", err)
	}

	// Update session messages with native format
	for i, msg := range messages {
		if i < len(nativeMessages) {
			msg.NativeFormat = nativeMessages[i]
			msg.NativeProvider = provider
			msg.NativeModelFamily = modelFamily
			msg.NativeTimestamp = time.Now()
		}
	}

	return nil
}

// initializePlanningAgent creates and initializes the planning agent
func (o *Orchestrator) initializePlanningAgent() {
	if o.planningClient == nil {
		logger.Debug("Cannot initialize planning agent: no planning client available")
		return
	}

	_, planningCancel := context.WithCancel(context.Background())
	investigator := NewCodebaseInvestigatorAgent(o)
	o.planningAgent = planning.NewPlanningAgent("planning", o.fs, o.session, o.planningClient, investigator)
	// Add web_fetch to planning tools (with authorization and summarize model if available)
	o.planningAgent.SetExternalTools([]planning.PlanningTool{
		planning.NewRealToolAdapter(
			&tools.WebFetchToolSpec{},
			tools.NewWebFetchTool(nil, o.summarizeClient, o.authorizer, secretdetect.NewDetector(), o.featureFlags),
		),
	})
	o.planningAgentCancel = planningCancel

	logger.Debug("Planning agent initialized")
}

const (
	cacheControlTokenInterval  = 10000
	cacheControlMaxBreakpoints = 4
)

func markCacheControlBreakpoints(messages []*llm.Message, intervalTokens, maxBreakpoints int) {
	for _, msg := range messages {
		if msg != nil {
			msg.CacheControl = false
		}
	}

	if intervalTokens <= 0 || maxBreakpoints <= 0 || len(messages) == 0 {
		return
	}

	nextThreshold := intervalTokens
	totalTokens := 0
	breakpoints := make([]int, 0, maxBreakpoints)

	for idx, msg := range messages {
		if msg == nil {
			continue
		}

		totalTokens += llm.EstimateTokenCountForMessage(msg)
		if totalTokens >= nextThreshold {
			breakpoints = append(breakpoints, idx)
			if len(breakpoints) == maxBreakpoints {
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

// toolSpec holds tool registration information
// Uses ToolSpec (static descriptor) + ToolFactory (executor factory) pattern
type toolSpec struct {
	spec     tools.ToolSpec
	factory  tools.ToolFactory
	critical bool
	isMCP    bool
	mcpKey   string
}

func (o *Orchestrator) rebuildTools(applyFilter bool) []error {
	var errs []error

	specs := make([]toolSpec, 0, 16)
	addSpec := func(spec tools.ToolSpec, critical bool, factory tools.ToolFactory, isMCP bool, mcpKey string) {
		if spec == nil || factory == nil {
			return
		}
		// Check if tool is enabled via feature flags (critical tools always enabled)
		if !critical && !o.featureFlags.IsToolEnabled(spec.Name()) {
			return
		}
		specs = append(specs, toolSpec{
			spec:     spec,
			critical: critical,
			factory:  factory,
			isMCP:    isMCP,
			mcpKey:   mcpKey,
		})
	}
	// Helper for legacy tools that haven't been migrated yet
	addLegacyTool := func(tool tools.Tool, critical bool, isMCP bool, mcpKey string) {
		if tool == nil {
			return
		}
		spec, factory := tools.WrapLegacyTool(tool)
		addSpec(spec, critical, factory, isMCP, mcpKey)
	}

	modelFamily := llm.DetectModelFamily(o.providerMgr.GetOrchestrationModel())

	// Core filesystem tools - using new pattern for migrated tools
	readFileSpec, readFileFactory := o.getReadFileToolSpec(modelFamily, o.session)
	addSpec(readFileSpec, true, readFileFactory, false, "")

	addSpec(
		&tools.CreateFileToolSpec{},
		true,
		tools.NewCreateFileToolFactory(o.fs, o.session),
		false,
		"",
	)
	if o.shouldUseReplaceFileTool(modelFamily) {
		addSpec(
			&tools.ReplaceFileToolSpec{},
			true,
			tools.NewReplaceFileToolFactory(o.fs, o.session),
			false,
			"",
		)
	}
	if o.shouldUseNonDiffUpdateTool(modelFamily) {
		addSpec(&tools.WriteFileJSONToolSpec{}, true, tools.NewWriteFileJSONToolFactory(o.fs, o.session), false, "")
	} else if o.shouldUseSimpleSingleDiffTool(modelFamily) {
		addSpec(&tools.WriteFileReplaceSingleToolSpec{}, true, tools.NewWriteFileReplaceSingleToolFactory(o.fs, o.session), false, "")
	} else if o.shouldUseSimpleDiffTool(modelFamily) {
		addSpec(&tools.WriteFileReplaceToolSpec{}, true, tools.NewWriteFileReplaceToolFactory(o.fs, o.session), false, "")
	} else {
		addSpec(
			&tools.WriteFileDiffToolSpec{},
			true,
			tools.NewWriteFileDiffToolFactory(o.fs, o.session),
			false,
			"",
		)
	}

	// Discovery / search tools
	addSpec(&tools.SearchFilesToolSpec{}, false, tools.NewSearchFilesToolFactory(o.fs), false, "")
	addSpec(&tools.SearchFileContentToolSpec{}, false, tools.NewSearchFileContentToolFactory(o.fs), false, "")
	addSpec(&tools.CodebaseInvestigatorToolSpec{}, false, tools.NewCodebaseInvestigatorToolFactory(NewCodebaseInvestigatorAgent(o)), false, "")
	addSpec(&tools.WebSearchToolSpec{}, false, tools.NewWebSearchToolFactory(o.config), false, "")
	addSpec(&tools.WebFetchToolSpec{}, false, tools.NewWebFetchToolFactory(nil, o.summarizeClient, o.authorizer, secretdetect.NewDetector(), o.featureFlags), false, "")

	// Context directory tools
	addSpec(&tools.SearchContextFilesToolSpec{}, false, tools.NewSearchContextFilesToolFactory(o.fs, o.config, o.session), false, "")
	addSpec(&tools.GrepContextFilesToolSpec{}, false, tools.NewGrepContextFilesToolFactory(o.fs, o.config, o.session), false, "")
	addSpec(&tools.ReadContextFileToolSpec{}, false, tools.NewReadContextFileToolFactory(o.fs, o.config, o.session), false, "")

	// Task management
	addSpec(&tools.TodoToolSpec{}, false, tools.NewTodoToolFactory(o.todoClient), false, "")

	// Shell tooling
	if o.shouldUseShellTool(modelFamily) {
		addSpec(&tools.ShellToolSpec{}, true, tools.NewShellToolFactory(o.session, o.workingDir), false, "")
		addSpec(&tools.LsToolSpec{}, true, tools.NewLsToolFactory(o.workingDir), false, "")
	}

	addSpec(&tools.StatusProgramToolSpec{}, true, tools.NewStatusProgramToolFactory(o.session), false, "")
	addSpec(&tools.WaitProgramToolSpec{}, true, tools.NewWaitProgramToolFactory(o.session), false, "")
	addSpec(&tools.StopProgramToolSpec{}, true, tools.NewStopProgramToolFactory(o.session), false, "")

	// Sandbox tool with TinyGo status forwarding - needs custom factory for configuration
	sandboxSpec, _ := tools.WrapLegacyTool(tools.NewSandboxToolWithFS(o.workingDir, o.config.TempDir, o.fs, o.session, o.shellActorClient))
	sandboxFactory := func(_ *tools.Registry) tools.ToolExecutor {
		instance := tools.NewSandboxToolWithFS(o.workingDir, o.config.TempDir, o.fs, o.session, o.shellActorClient)
		o.configureSandboxTool(instance)
		return instance
	}
	addSpec(sandboxSpec, true, sandboxFactory, false, "")

	// Parallel execution - needs registry access
	// Disabled for certain models (e.g., zai-glm) that don't handle parallel tools well
	if o.shouldUseParallelTool(modelFamily) {
		parallelSpec, _ := tools.WrapLegacyTool(tools.NewParallelTool(nil))
		parallelFactory := func(reg *tools.Registry) tools.ToolExecutor {
			return tools.NewParallelTool(reg)
		}
		addSpec(parallelSpec, false, parallelFactory, false, "")
	}

	// Summarization-related tools
	if o.summarizeClient != nil {
		addSpec(&tools.SummarizeFileToolSpec{}, false, tools.NewSummarizeFileToolFactory(o.fs, o.session, o.summarizeClient), false, "")

		summarizeSpec, _ := tools.WrapLegacyTool(tools.NewToolSummarizeTool(nil, o.summarizeClient))
		summarizeFactory := func(reg *tools.Registry) tools.ToolExecutor {
			return tools.NewToolSummarizeTool(reg, o.summarizeClient)
		}
		addSpec(summarizeSpec, false, summarizeFactory, false, "")
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
			addLegacyTool(t, false, true, mcpKey)
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
			filterMap := make(map[string]struct{}, len(filteredMCP))
			for i := range filteredMCP {
				filterMap[filteredMCP[i].spec.Name()] = struct{}{}
			}

			filteredSpecs = make([]toolSpec, 0, len(specs))
			for _, spec := range specs {
				if spec.isMCP {
					if _, ok := filterMap[spec.spec.Name()]; ok {
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

	registry := tools.NewRegistryWithSecrets(o.authorizer, secretdetect.NewDetector())
	o.toolRegistry = registry

	var activeMCPSanitized []string
	seenMCP := make(map[string]struct{})

	for _, spec := range filteredSpecs {
		// Use RegisterSpec with the new pattern
		registry.RegisterSpec(spec.spec, spec.factory)

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

func (o *Orchestrator) getReadFileTool(modelFamily llm.ModelFamily, sess *session.Session) tools.Tool {
	if o.shouldUseNumberedReadFileTool(modelFamily) {
		// Create a tool that combines spec and executor for legacy compatibility
		spec := &tools.ReadFileNumberedSpec{}
		executor := tools.NewReadFileNumberedFactory(o.fs, sess)(nil)
		return &combinedTool{spec: spec, executor: executor}
	}
	// Create a tool that combines spec and executor for legacy compatibility
	spec := &tools.ReadFileToolSpec{}
	executor := tools.NewReadFileToolFactory(o.fs, sess)(nil)
	return &combinedTool{spec: spec, executor: executor}
}

// combinedTool wraps a spec and executor to implement the Tool interface for backward compatibility
type combinedTool struct {
	spec     tools.ToolSpec
	executor tools.ToolExecutor
}

func (t *combinedTool) Name() string        { return t.spec.Name() }
func (t *combinedTool) Description() string { return t.spec.Description() }
func (t *combinedTool) Parameters() map[string]interface{} {
	return t.spec.Parameters()
}
func (t *combinedTool) Execute(ctx context.Context, params map[string]interface{}) *tools.ToolResult {
	return t.executor.Execute(ctx, params)
}

func (o *Orchestrator) getReadFileToolSpec(modelFamily llm.ModelFamily, sess *session.Session) (tools.ToolSpec, tools.ToolFactory) {
	if o.shouldUseNumberedReadFileTool(modelFamily) {
		// read_file_numbered has been migrated to new pattern
		return &tools.ReadFileNumberedSpec{}, tools.NewReadFileNumberedFactory(o.fs, sess)
	}
	// read_file has been migrated to new pattern
	return &tools.ReadFileToolSpec{}, tools.NewReadFileToolFactory(o.fs, sess)
}

func (o *Orchestrator) shouldUseShellTool(modelFamily llm.ModelFamily) bool {
	return false
}

func (o *Orchestrator) shouldUseNumberedReadFileTool(modelFamily llm.ModelFamily) bool {
	return false
}

func (o *Orchestrator) shouldUseReplaceFileTool(modelFamily llm.ModelFamily) bool {
	return modelFamily != llm.FamilyZaiGLM
}

func (o *Orchestrator) shouldUseNonDiffUpdateTool(modelFamily llm.ModelFamily) bool {
	return false
}

func (o *Orchestrator) shouldUseSimpleDiffTool(modelFamily llm.ModelFamily) bool {
	return !o.shouldUseNonDiffUpdateTool(modelFamily) && !o.shouldUseSimpleSingleDiffTool(modelFamily)
}

func (o *Orchestrator) shouldUseSimpleSingleDiffTool(modelFamily llm.ModelFamily) bool {
	return !o.shouldUseNonDiffUpdateTool(modelFamily) &&
		modelFamily == llm.FamilyCodestral
}

func (o *Orchestrator) shouldUseParallelTool(modelFamily llm.ModelFamily) bool {
	// Disable parallel tool for zai-glm models
	return modelFamily != llm.FamilyZaiGLM
}

func (o *Orchestrator) applyModelSpecificDefaults(req *llm.CompletionRequest, modelID string) {
	if req == nil {
		return
	}

	// Detect model family from model ID
	modelFamily := llm.DetectModelFamily(modelID)
	normalizedID := strings.ToLower(strings.TrimSpace(modelID))

	// Apply model-specific parameter defaults
	switch modelFamily {
	case llm.FamilyGPT5:
		req.Temperature = 1.0

	case llm.FamilyZaiGLM:
		// ZAI GLM (Cerebras) defaults
		// Always use temperature=1.0 and top_p=0.95 for optimal performance
		// These are the recommended defaults from Cerebras documentation
		req.Temperature = 1.0
		req.TopP = 0.95

		// Set clear_thinking to false ONLY for zai-glm-4.7 to preserve reasoning traces
		// (recommended for agentic/coding workflows)
		if strings.Contains(normalizedID, "zai-glm-4.7") && req.ClearThinking == nil {
			clearThinking := false
			req.ClearThinking = &clearThinking
		}
	}
}

func (o *Orchestrator) configureSandboxTool(sandboxTool *tools.SandboxTool) {
	if sandboxTool == nil {
		return
	}
	if o.authorizer != nil {
		sandboxTool.SetAuthorizer(o.authorizer)
	}
	sandboxTool.SetSummarizeClient(o.summarizeClient)
	// Set output compaction configuration
	sandboxTool.SetCompactionConfig(o.config.SandboxOutputCompaction)
	// Set context window from orchestration model for compaction decisions
	if o.orchestrationClient != nil {
		modelID := o.orchestrationClient.GetModelName()
		contextWindow := o.providerMgr.GetModelContextWindow(modelID)
		if contextWindow > 0 {
			sandboxTool.SetContextWindow(contextWindow)
		} else {
			// Fallback to maxTokens from config if model info not available
			sandboxTool.SetContextWindow(o.config.MaxTokens)
		}
	} else if o.config.MaxTokens > 0 {
		sandboxTool.SetContextWindow(o.config.MaxTokens)
	}
	// Set secret detector and feature flags for fetch requests
	sandboxTool.SetSecretDetector(secretdetect.NewDetector())
	sandboxTool.SetFeatureFlags(o.featureFlags)
	sandboxTool.SetProgressCallback(o.GetCurrentProgressCallback())
	if sandboxTool.GetTinyGoManager() != nil {
		sandboxTool.GetTinyGoManager().SetStatusCallback(func(status string) {
			dispatchProgress(o.GetCurrentProgressCallback(), progress.Update{
				Message:   status,
				Mode:      progress.ReportJustStatus,
				Ephemeral: true,
			})
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

	tmpCfg := &config.Config{
		WorkingDir:         o.config.WorkingDir,
		CacheTTL:           o.config.CacheTTL,
		MaxCacheEntries:    o.config.MaxCacheEntries,
		DefaultTimeout:     o.config.DefaultTimeout,
		TempDir:            o.config.TempDir,
		Temperature:        o.config.Temperature,
		MaxTokens:          o.config.MaxTokens,
		ProviderConfigPath: o.config.ProviderConfigPath,
		DisableAnimations:  o.config.DisableAnimations,
		LogLevel:           o.config.LogLevel,
		LogPath:            o.config.LogPath,
		AuthorizedDomains:  o.config.AuthorizedDomains,
		AuthorizedCommands: o.config.AuthorizedCommands,
		Search:             o.config.Search,
		MCP: config.MCPConfig{
			Servers: map[string]*config.MCPServerConfig{
				serverName: server,
			},
		},
		Secrets:           o.config.Secrets,
		EnablePromptCache: o.config.EnablePromptCache,
		AutoSave:          o.config.AutoSave,
	}

	manager := mcp.NewManager(tmpCfg, o.workingDir, o.providerMgr)
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

// getAutoContinueMaxAttempts returns the appropriate auto-continue limit based on model family
func (o *Orchestrator) getAutoContinueMaxAttempts() int {
	modelID := o.providerMgr.GetOrchestrationModel()
	modelFamily := llm.DetectModelFamily(modelID)

	if modelFamily == llm.FamilyKimi {
		return kimiK2AutoContinueMaxAttempts
	}
	if modelFamily == llm.FamilyDeepSeek {
		return deepSeekAutoContinueMaxAttempts
	}
	if modelFamily == llm.FamilyMiniMax {
		return minimaxAutoContinueMaxAttempts
	}
	return defaultAutoContinueMaxAttempts
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

// ContextUsageCallback receives free context percentage and total context window updates for UI display
type ContextUsageCallback func(freePercent int, contextWindow int) error

// OpenRouterUsageCallback receives OpenRouter usage data for cost tracking in UI display
type OpenRouterUsageCallback func(usage map[string]interface{}) error

// GetCurrentProgressCallback returns the active progress callback, if any.
func (o *Orchestrator) GetCurrentProgressCallback() progress.Callback {
	o.progressCbMu.Lock()
	defer o.progressCbMu.Unlock()
	return o.currentProgressCb
}

func dispatchProgress(cb progress.Callback, update progress.Update) {
	if cb == nil {
		return
	}
	if update.Message == "" && !update.ShouldStatus() {
		return
	}
	if err := progress.Dispatch(cb, update); err != nil {
		logger.Debug("progress callback error: %v", err)
	}
}

// planningToolAdapter bridges a standard tool into the planning tool interface.
type planningToolAdapter struct {
	tool tools.Tool
}

func (a *planningToolAdapter) Name() string {
	if a.tool == nil {
		return ""
	}
	return a.tool.Name()
}

func (a *planningToolAdapter) Description() string {
	if a.tool == nil {
		return "Unavailable planning tool"
	}
	return a.tool.Description()
}

func (a *planningToolAdapter) Parameters() map[string]interface{} {
	if a.tool == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	return a.tool.Parameters()
}

func (a *planningToolAdapter) Execute(ctx context.Context, params map[string]interface{}) *planning.PlanningToolResult {
	if a.tool == nil {
		return &planning.PlanningToolResult{Error: "tool is not available"}
	}
	res := a.tool.Execute(ctx, params)
	if res == nil {
		return &planning.PlanningToolResult{Error: "tool returned nil result"}
	}
	if res.Error != "" {
		return &planning.PlanningToolResult{Error: res.Error}
	}
	return &planning.PlanningToolResult{Result: res.Result}
}

// buildReadOnlyPlanningTools returns planning-safe MCP tools filtered to read-only capabilities.
func (o *Orchestrator) buildReadOnlyPlanningTools(allowedMCPs []string) ([]planning.PlanningTool, []error) {
	if o == nil || o.config == nil {
		return nil, nil
	}

	var adapters []planning.PlanningTool
	var allErrs []error

	// Add Web Search tool if configured
	if o.config.Search.Provider != "" {
		webSearchTool := tools.NewWebSearchTool(o.config)
		adapters = append(adapters, &planningToolAdapter{tool: webSearchTool})
	}

	if o.mcpManager != nil {
		mcpTools, errs := o.mcpManager.BuildTools()
		if len(errs) > 0 {
			allErrs = append(allErrs, errs...)
		}

		for _, tool := range mcpTools {
			if tool == nil {
				continue
			}
			serverKey := extractMCPSanitizedServer(tool.Name())
			if serverKey == "" {
				continue
			}

			serverName := o.lookupServerBySanitizedKey(serverKey)
			if serverName == "" {
				continue
			}

			// Filter by allowedMCPs if specified (non-nil)
			if allowedMCPs != nil {
				allowed := false
				for _, allowedName := range allowedMCPs {
					if allowedName == serverName {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}

			serverCfg := o.config.MCP.Servers[serverName]
			if !o.isReadOnlyMCPTool(serverName, serverCfg, tool) {
				continue
			}

			adapters = append(adapters, &planningToolAdapter{tool: tool})
		}
	}

	return adapters, allErrs
}

// isReadOnlyMCPTool determines whether a given MCP tool should be exposed to the planning agent.
func (o *Orchestrator) isReadOnlyMCPTool(serverName string, serverCfg *config.MCPServerConfig, tool tools.Tool) bool {
	if serverCfg == nil || tool == nil {
		return false
	}

	// Explicit metadata override
	if flag, ok := parseMetadataBool(serverCfg.Metadata, "read_only"); ok {
		return flag
	}

	switch strings.ToLower(serverCfg.Type) {
	case "openapi":
		if openapiTool, ok := tool.(*tools.OpenAPITool); ok {
			method := strings.ToUpper(openapiTool.HTTPMethod())
			return method == "GET" || method == "HEAD" || method == "OPTIONS"
		}
	case "openai":
		// Calling a hosted model is effectively read-only
		return true
	case "command":
		// Command MCPs are only allowed when explicitly marked read-only
		return false
	default:
		return false
	}

	return false
}

func parseMetadataBool(metadata map[string]string, key string) (bool, bool) {
	if len(metadata) == 0 {
		return false, false
	}
	raw, ok := metadata[key]
	if !ok {
		return false, false
	}

	val := strings.ToLower(strings.TrimSpace(raw))
	switch val {
	case "1", "true", "yes", "y", "on":
		return true, true
	case "0", "false", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

// classifyPromptSimplicity uses the summarization model (when available) to decide if planning is needed.
func (o *Orchestrator) classifyPromptSimplicity(ctx context.Context, prompt string) (bool, string) {
	if strings.TrimSpace(prompt) == "" {
		return true, "empty prompt"
	}

	if o.summarizeClient != nil {
		req := &llm.CompletionRequest{
			Messages: []*llm.Message{
				{
					Role: "system",
					Content: "Decide if the user's request is simple (single small change, no research) or complex. " +
						"Respond ONLY with JSON: {\"simple\":true|false,\"reason\":\"short reason\"}.",
				},
				{
					Role:    "user",
					Content: prompt,
				},
			},
			Temperature: 0,
			MaxTokens:   128,
		}

		resp, err := o.summarizeClient.CompleteWithRequest(ctx, req)
		if err == nil && resp != nil {
			if simple, reason, ok := parseSimplicityResponse(resp.Content); ok {
				return simple, reason
			}
		} else {
			logger.Warn("Prompt simplicity classification error: %v with response: %v", err, resp)
		}
	}

	return heuristicPromptSimplicity(prompt)
}

func parseSimplicityResponse(content string) (bool, string, bool) {
	type payload struct {
		Simple interface{} `json:"simple"`
		Reason string      `json:"reason"`
	}

	tryParse := func(raw string) (bool, string, bool) {
		var p payload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return false, "", false
		}
		switch v := p.Simple.(type) {
		case bool:
			return v, strings.TrimSpace(p.Reason), true
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true", "yes", "simple":
				return true, strings.TrimSpace(p.Reason), true
			case "false", "no", "complex":
				return false, strings.TrimSpace(p.Reason), true
			}
		}
		return false, strings.TrimSpace(p.Reason), false
	}

	if simple, reason, ok := tryParse(content); ok {
		return simple, reason, true
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return tryParse(content[start : end+1])
	}

	trimmed := strings.ToLower(strings.TrimSpace(content))
	switch {
	case strings.HasPrefix(trimmed, "simple"):
		return true, content, true
	case strings.HasPrefix(trimmed, "complex"), strings.HasPrefix(trimmed, "not simple"):
		return false, content, true
	default:
		return false, "", false
	}
}

func heuristicPromptSimplicity(prompt string) (bool, string) {
	words := strings.Fields(prompt)
	if len(words) <= 4 && strings.Count(prompt, "\n") == 0 {
		return true, "short single-line prompt"
	}

	lower := strings.ToLower(prompt)
	complexKeywords := []string{"migrate", "architecture", "multi-step", "refactor", "design", "plan", "roadmap", "debug multiple"}
	for _, kw := range complexKeywords {
		if strings.Contains(lower, kw) {
			return false, fmt.Sprintf("contains complex keyword '%s'", kw)
		}
	}

	if strings.Count(prompt, "\n") > 3 || len(words) > 60 {
		return false, "long prompt suggests complexity"
	}

	return false, "no complexity indicators found => using planner"
}

// convertPlanningBoardToSession converts a planning.PlanningBoard to session.PlanningBoard
func convertPlanningBoardToSession(planningBoard *planning.PlanningBoard) *session.PlanningBoard {
	if planningBoard == nil {
		return nil
	}

	sessionBoard := &session.PlanningBoard{
		Description:  planningBoard.Description,
		PrimaryTasks: make([]session.PlanningTask, len(planningBoard.PrimaryTasks)),
	}

	for i, task := range planningBoard.PrimaryTasks {
		sessionBoard.PrimaryTasks[i] = convertPlanningTaskToSession(&task)
	}

	return sessionBoard
}

// convertPlanningTaskToSession converts a planning.PlanningTask to session.PlanningTask
func convertPlanningTaskToSession(planningTask *planning.PlanningTask) session.PlanningTask {
	return session.PlanningTask{
		ID:          planningTask.ID,
		Text:        planningTask.Text,
		Priority:    planningTask.Priority,
		Status:      planningTask.Status,
		Description: planningTask.Description,
		Subtasks:    convertPlanningTasksToSession(planningTask.Subtasks),
	}
}

// convertPlanningTasksToSession converts a slice of planning.PlanningTask to session.PlanningTask
func convertPlanningTasksToSession(planningTasks []planning.PlanningTask) []session.PlanningTask {
	if planningTasks == nil {
		return nil
	}
	sessionTasks := make([]session.PlanningTask, len(planningTasks))
	for i, task := range planningTasks {
		sessionTasks[i] = convertPlanningTaskToSession(&task)
	}
	return sessionTasks
}

func formatPlanForDisplay(mode planning.PlanningMode, plan []string, board *planning.PlanningBoard) string {
	if mode == planning.PlanningModeBoard && board != nil {
		// Board mode: display hierarchical tasks
		var sb strings.Builder
		sb.WriteString("\nPlanning Board (read-only):\n")
		if board.Description != "" {
			sb.WriteString(fmt.Sprintf("\nDescription: %s\n", board.Description))
		}
		sb.WriteString("\nPrimary Tasks:\n")
		for i, task := range board.PrimaryTasks {
			sb.WriteString(fmt.Sprintf("%d. %s", i+1, task.Text))
			if task.Priority != "" && task.Priority != "medium" {
				sb.WriteString(fmt.Sprintf(" [%s]", task.Priority))
			}
			sb.WriteString("\n")
			if task.Description != "" {
				sb.WriteString(fmt.Sprintf("   Description: %s\n", task.Description))
			}
			if len(task.Subtasks) > 0 {
				sb.WriteString("   Subtasks:\n")
				for j, subtask := range task.Subtasks {
					sb.WriteString(fmt.Sprintf("   %c. %s", 'a'+j, subtask.Text))
					if subtask.Priority != "" && subtask.Priority != "medium" {
						sb.WriteString(fmt.Sprintf(" [%s]", subtask.Priority))
					}
					if subtask.Status != "" && subtask.Status != "pending" {
						sb.WriteString(fmt.Sprintf(" (%s)", subtask.Status))
					}
					sb.WriteString("\n")
				}
			}
		}
		return strings.TrimRight(sb.String(), "\n")
	}

	// Simple mode: display flat list
	if len(plan) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\nPlanning pass (read-only):\n")
	for i, step := range plan {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(step)))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// isInitPrompt checks if the given prompt is the init command prompt
func (o *Orchestrator) isInitPrompt(prompt string) bool {
	// Look for the unique signature of the init prompt
	return strings.Contains(prompt, "Your ONLY task is to analyze this codebase and CREATE an AGENTS.md file using the create_file tool") &&
		strings.Contains(prompt, "## NON-NEGOTIABLE REQUIREMENTS") &&
		strings.Contains(prompt, "**YOU MUST END THIS CONVERSATION BY CALLING create_file WITH path=\"AGENTS.md\"**")
}

// runPlanningPhaseIfNeeded executes a planning pass before entering the main orchestration loop.
func (o *Orchestrator) runPlanningPhaseIfNeeded(ctx context.Context, prompt string, progressCallback progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback) error {
	// Check if this is an init prompt - if so, skip planning since it has specific instructions
	if o.isInitPrompt(prompt) {
		logger.Debug("Skipping planning phase: detected /init command prompt")
		return nil
	}

	// Check if planning is enabled via feature flags
	if !o.featureFlags.IsPlanningEnabled() {
		logger.Debug("Planning phase disabled via feature flags")
		return nil
	}

	// Only allow planning on the first user message of a session
	if o.session != nil && o.session.UserMessageCount() > 1 {
		logger.Debug("Skipping planning phase: only allowed on first user message")
		return nil
	}

	if o.planningClient == nil {
		return nil
	}
	if o.planningAgent == nil {
		o.initializePlanningAgent()
	}
	if o.planningAgent == nil {
		return nil
	}

	decision, err := o.decidePlanningConfiguration(ctx, prompt)
	if err != nil {
		logger.Warn("Planning decision error: %v", err)
		return nil
	}

	if !decision.ShouldRun {
		logger.Debug("Skipping planning phase: %s", decision.Reason)
		return nil
	}

	logger.Info("Planning phase triggered: %s", decision.Reason)

	statusMsg := fmt.Sprintf("Running planning pass (%s)...", decision.Reason)

	dispatchProgress(progressCallback, progress.Update{
		Message:   statusMsg,
		Mode:      progress.ReportJustStatus,
		Ephemeral: true,
	})

	extraTools, errs := o.buildReadOnlyPlanningTools(decision.AllowedMCPs)
	for _, err := range errs {
		if err != nil {
			logger.Warn("Planning MCP tool build warning: %v", err)
		}
	}
	o.planningAgent.SetExternalTools(extraTools)

	allowQuestions := !o.cliMode
	maxQuestions := 0
	if allowQuestions {
		maxQuestions = 3
	}

	req := &planning.PlanningRequest{
		Objective:      prompt,
		AllowQuestions: allowQuestions,
		MaxQuestions:   maxQuestions,
	}

	var questionsAsked []string
	var userInputCb UserInputCallback
	if o.userInputCb != nil {
		userInputCb = func(question string) (string, error) {
			questionsAsked = append(questionsAsked, question)
			return o.userInputCb(question)
		}
	} else {
		// Fallback for when no callback is set
		userInputCb = func(question string) (string, error) {
			questionsAsked = append(questionsAsked, question)
			return fmt.Sprintf("Question noted: %s (Please answer this question)", question), nil
		}
	}

	// Set planning active before starting
	o.session.SetPlanningActive(true, req.Objective)
	defer o.session.SetPlanningActive(false, "")

	// Create wrapper callbacks to distinguish planning tools
	planningToolCallCb := func(toolName, toolID string, parameters map[string]interface{}) error {
		if toolCallCb != nil {
			return toolCallCb("Planning: "+toolName, toolID, parameters)
		}
		return nil
	}

	planningToolResultCb := func(toolName, toolID, result, errorMsg string) error {
		if toolResultCb != nil {
			return toolResultCb("Planning: "+toolName, toolID, result, errorMsg)
		}
		return nil
	}

	response, err := o.planningAgent.PlanWithProgress(ctx, req, userInputCb, progressCallback, planningToolCallCb, planningToolResultCb)
	if err != nil {
		return fmt.Errorf("planning failed: %w", err)
	}

	// Handle the planning response based on mode
	if response.Mode == planning.PlanningModeBoard && response.Board != nil {
		// Board mode: store the board and display it
		boardJSON, _ := json.Marshal(response.Board)
		o.session.AddMessage(&session.Message{
			Role:    "system",
			Content: fmt.Sprintf("Planning agent generated board for '%s': %s", prompt, string(boardJSON)),
		})

		// Store the board in session for orchestrator to use
		o.session.SetPlanningBoard(convertPlanningBoardToSession(response.Board))

		dispatchProgress(progressCallback, progress.Update{
			Message:    "\n\n" + formatPlanForDisplay(response.Mode, response.Plan, response.Board) + "\n",
			AddNewLine: false,
			Mode:       progress.ReportNoStatus,
		})
	} else if len(response.Plan) > 0 {
		// Simple mode: store and display the plan
		planJSON, _ := json.Marshal(response.Plan)
		o.session.AddMessage(&session.Message{
			Role:    "system",
			Content: fmt.Sprintf("Planning agent generated plan for '%s': %s", prompt, string(planJSON)),
		})

		dispatchProgress(progressCallback, progress.Update{
			Message:    "\n\n" + formatPlanForDisplay(response.Mode, response.Plan, response.Board) + "\n",
			AddNewLine: false,
			Mode:       progress.ReportNoStatus,
		})
	}

	if response.NeedsInput && len(response.Questions) > 0 {
		o.session.AddMessage(&session.Message{
			Role:    "system",
			Content: fmt.Sprintf("Planning agent needs user input: %s", strings.Join(response.Questions, "; ")),
		})
	}

	if len(questionsAsked) > 0 {
		logger.Debug("Planning questions captured for user: %v", questionsAsked)
	}

	dispatchProgress(progressCallback, progress.Update{
		Message:   "",
		Mode:      progress.ReportJustStatus,
		Ephemeral: true,
	})

	return nil
}

// ProcessPrompt processes a user prompt
func (o *Orchestrator) ProcessPrompt(ctx context.Context, prompt string, progressCallback progress.Callback, contextCallback ContextUsageCallback, authCallback AuthorizationCallback, toolCallCallback ToolCallCallback, toolResultCallback ToolResultCallback, openRouterUsageCallback OpenRouterUsageCallback) error {
	combinedCtx, cancel := combineContexts(ctx, o.ctx)
	if cancel != nil {
		defer cancel()
	}
	ctx = combinedCtx

	if o.orchestrationClient == nil {
		return fmt.Errorf("no orchestration model configured. Use /provider and /models commands to set up")
	}

	sendStatus := func(msg string) {
		dispatchProgress(progressCallback, progress.Update{
			Message:   msg,
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		})
	}

	sendStream := func(msg string, addNewLine bool) {
		dispatchProgress(progressCallback, progress.Update{
			Message:    msg,
			AddNewLine: addNewLine,
			Mode:       progress.ReportNoStatus,
		})
	}

	// Store progress callback for use by tools (e.g., TinyGo download progress)
	o.progressCbMu.Lock()
	o.currentProgressCb = progressCallback
	o.progressCbMu.Unlock()

	defer func() {
		o.progressCbMu.Lock()
		o.currentProgressCb = nil
		o.progressCbMu.Unlock()
	}()

	// Expand @file references in the prompt before adding to session
	expandedPrompt := o.expandFileReferences(ctx, prompt)

	// Add user message
	logger.Debug("ProcessPrompt: Adding user message with prompt (len=%d): %q", len(expandedPrompt), expandedPrompt)
	o.session.AddMessage(&session.Message{
		Role:    "user",
		Content: expandedPrompt,
	})
	logger.Debug("ProcessPrompt: Session now has %d messages", len(o.session.GetMessages()))

	// Auto-save the session if enabled
	if o.config.AutoSave.Enabled {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := o.SaveCurrentSession(ctx); err != nil {
				logger.Warn("Failed to auto-save session: %v", err)
			}
		}()
	}

	// Reset loop detector for new prompt
	o.loopDetector.Reset()
	logger.Debug("Loop detector reset for new prompt")

	if err := o.runPlanningPhaseIfNeeded(ctx, prompt, progressCallback, toolCallCallback, toolResultCallback); err != nil {
		logger.Warn("Pre-loop planning failed: %v", err)
	}

	// Get or build system prompt (cached for the session)
	modelID := o.providerMgr.GetOrchestrationModel()
	systemPrompt, err := o.getOrBuildSystemPrompt(ctx, modelID)
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

	// Detect provider/model changes and convert messages if needed
	provider, modelFamily, providerChanged := o.detectProviderChange(modelID)
	if providerChanged && provider != "" {
		logger.Info("Provider changed to %s (%s), converting message history", provider, modelFamily)
		if err := o.convertSessionMessages(modelID, provider, modelFamily); err != nil {
			logger.Warn("Failed to convert messages to native format: %v", err)
		} else {
			o.session.SetCurrentProvider(provider, modelFamily)
		}
	}

	// Tool execution loop
	maxIterations := 256 // Prevent infinite loops
	autoContinueAttempts := 0
	hitMaxIterations := false
	for iteration := 0; iteration < maxIterations; iteration++ {
		logger.Debug("ProcessPrompt iteration %d starting (max=%d)", iteration, maxIterations)

		// Convert session messages to LLM format (preserving native format)
		sessionMessages := o.session.GetMessages()
		requestMessages := sessionMessages
		if len(requestMessages) == 0 {
			requestMessages = []*session.Message{
				{
					Role:    "system",
					Content: systemPrompt,
				},
				{
					Role:    "user",
					Content: prompt,
				},
			}
		}

		llmMessages := make([]*llm.Message, len(requestMessages))
		for i, msg := range requestMessages {
			llmMessages[i] = &llm.Message{
				Role:              msg.Role,
				Content:           msg.Content,
				ToolCalls:         msg.ToolCalls,
				ToolID:            msg.ToolID,
				ToolName:          msg.ToolName,
				NativeFormat:      msg.NativeFormat,
				NativeProvider:    msg.NativeProvider,
				NativeModelFamily: msg.NativeModelFamily,
				NativeTimestamp:   msg.NativeTimestamp,
			}
		}

		if o.config != nil && o.config.EnablePromptCache {
			markCacheControlBreakpoints(llmMessages, cacheControlTokenInterval, cacheControlMaxBreakpoints)
		}

		totalTokens, perMessageTokens, _ := estimateContextTokens(modelID, systemPrompt, requestMessages)
		o.dispatchContextUsage(modelID, totalTokens, contextCallback)
		o.maybeCompactContext(modelID, systemPrompt, sessionMessages, perMessageTokens, totalTokens, progressCallback, contextCallback)

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
			Messages:      llmMessages,
			Tools:         o.toolRegistry.ToJSONSchema(),
			Temperature:   o.config.Temperature,
			MaxTokens:     maxTokens,
			SystemPrompt:  systemPrompt,
			EnableCaching: o.config.EnablePromptCache,
			CacheTTL:      o.config.PromptCacheTTL,
		}

		// Set previous_response_id for clients that support it (e.g., OpenRouter) for better prompt caching
		if prevID := o.orchestrationClient.GetLastResponseID(); prevID != "" {
			req.PreviousResponseID = prevID
			logger.Debug("Setting previous_response_id=%s for request", prevID)
		}

		// Apply model-specific defaults
		o.applyModelSpecificDefaults(req, modelID)

		// Notify UI that we're waiting for LLM response
		if progressCallback != nil && len(llmMessages) > 1 {
			sendStatus("Thinking...")
		}

		// Get completion with error retry logic
		var validationErr *toolCallValidationError
		var ctxSizeErr *contextSizeExceededError
		response, err := o.completeWithRetry(ctx, req, progressCallback)
		if err != nil {
			if errors.As(err, &validationErr) {
				msg := fmt.Sprintf("Tool call validation failed for '%s': missing required parameter '%s'. This client cannot populate the value automatically, so please include it in your next tool call.",
					validationErr.toolName, validationErr.missingParam)
				logger.Warn("Tool call validation error: %v", validationErr)
				o.session.AddMessage(&session.Message{
					Role:    "user",
					Content: msg,
				})
				o.broadcastContextUsage(modelID, systemPrompt, contextCallback)
				sendStream(fmt.Sprintf("\n⚠️ %s\n", msg), false)
				sendStatus("Waiting for tool call validation fix...")
				continue
			}
			if errors.As(err, &ctxSizeErr) {
				// Context size exceeded - trigger forced compaction and retry
				logger.Info("Context size exceeded, triggering forced compaction: %s", ctxSizeErr.reason)
				sendStatus("Compacting context...")
				o.forceCompactContext(modelID, systemPrompt, sessionMessages, progressCallback, contextCallback)

				// Check if compaction was effective by estimating context size after compaction
				currentMessages := o.session.GetMessages()
				// Add system prompt to estimate properly
				fullMessages := make([]*session.Message, len(currentMessages)+1)
				fullMessages[0] = &session.Message{Role: "system", Content: systemPrompt}
				copy(fullMessages[1:], currentMessages)
				newTotalTokens, _, _ := estimateContextTokens(modelID, "", fullMessages)
				contextWindow := o.getContextWindow(modelID)

				// If still exceeding re-compaction threshold, try again with more forceful prompt
				if contextWindow > 0 && (newTotalTokens*100/contextWindow) >= recompactionThresholdPercent {
					currentAttempt := o.getCurrentCompactionAttempt()
					if currentAttempt < maxCompactionAttempts {
						logger.Info("Compaction attempt %d insufficient (%d/%d tokens, %d%%), triggering re-compaction...",
							currentAttempt, newTotalTokens, contextWindow, newTotalTokens*100/contextWindow)
						sendStatus(fmt.Sprintf("Re-compacting (attempt %d/%d)...", currentAttempt+1, maxCompactionAttempts))
						o.forceCompactContext(modelID, systemPrompt, o.session.GetMessages(), progressCallback, contextCallback)
					} else {
						logger.Warn("Reached max compaction attempts (%d), context may still be too large", maxCompactionAttempts)
					}
				}

				// Reset compaction attempt counter after finishing
				o.resetCompactionAttempts()

				// Continue to retry with compacted context
				continue
			}
			return fmt.Errorf("completion failed: %w", err)
		}

		// Ensure tool calls always have IDs; some providers omit them intermittently.
		response.ToolCalls = llm.NormalizeToolCallIDs(response.ToolCalls)

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
		assistantMsg := &session.Message{
			Role:      "assistant",
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		}

		// Process OpenRouter usage data if available
		if response.Usage != nil {
			logger.Debug("LLM response contains usage data, processing OpenRouter usage")
			o.processOpenRouterUsage(response.Usage, openRouterUsageCallback)
			// Accumulate usage in session
			o.session.AccumulateUsage(response.Usage)
		} else {
			logger.Debug("LLM response contains no usage data")
		}

		// Convert to native format immediately if supported
		converter := llm.GetConverter(modelID)
		if converter != nil && converter.SupportsNativeStorage() {
			nativeMsgs, err := converter.ConvertToNative(
				[]*llm.Message{{
					Role:      "assistant",
					Content:   response.Content,
					ToolCalls: response.ToolCalls,
				}},
				"",
				o.config.EnablePromptCache,
				o.config.PromptCacheTTL,
			)

			if err == nil && len(nativeMsgs) > 0 {
				assistantMsg.NativeFormat = nativeMsgs[len(nativeMsgs)-1] // Last message is the assistant message
				assistantMsg.NativeProvider = provider
				assistantMsg.NativeModelFamily = modelFamily
				assistantMsg.NativeTimestamp = time.Now()
			} else if err != nil {
				logger.Warn("Failed to convert assistant message to native format: %v", err)
			}
		}

		o.session.AddMessage(assistantMsg)
		o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

		// Auto-save the session if enabled
		if o.config.AutoSave.Enabled {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := o.SaveCurrentSession(ctx); err != nil {
					logger.Warn("Failed to auto-save session: %v", err)
				}
			}()
		}

		// Stream the content to UI if present
		if response.Content != "" {
			sendStream(response.Content, false)
		} else if len(response.ToolCalls) > 0 && response.Content == "" {
			// Log when we have tool calls but no content - this is normal but worth tracking
			logger.Debug("Response contains %d tool calls with no text content", len(response.ToolCalls))
		}

		// Check for text loops in the response
		if response.Content != "" {
			isLoop, pattern, count := o.loopDetector.AddText(response.Content)
			if isLoop {
				logger.Warn("Text loop detected at iteration %d: pattern repeated %d times", iteration, count)
				// Show a truncated version of the pattern to the user
				displayPattern := pattern
				if len(displayPattern) > 100 {
					displayPattern = displayPattern[:100] + "..."
				}
				sendStream(fmt.Sprintf("\n\n🔁 Loop detected! The LLM is repeating the same text pattern %d times.\nPattern: %s\nStopping generation to prevent infinite loop.\n", count, displayPattern), false)
				logger.Debug("Breaking out of loop due to text repetition (iteration %d)", iteration)
				break
			}
		}

		// Check if response was truncated due to token limits
		isTruncated := response.StopReason == "length" || response.StopReason == "max_tokens"

		// Check if content ends with incomplete indication (colon or colon with newline)
		contentEndsIncomplete := false
		if response.Content != "" {
			trimmedContent := strings.TrimSpace(response.Content)
			contentEndsIncomplete = strings.HasSuffix(trimmedContent, ":") || strings.HasSuffix(trimmedContent, ":\n")
		}

		// Check if there are tool calls to execute
		if len(response.ToolCalls) == 0 {
			// No tool calls - check if we should auto-continue
			shouldContinue, judgeOutput := o.shouldAutoContinue(ctx, systemPrompt)

			// Also consider auto-continue if content ends with incomplete indicators
			if contentEndsIncomplete && !shouldContinue {
				logger.Debug("Auto-continue triggered by incomplete content ending (colon)")
				shouldContinue = true
				judgeOutput = "Auto-continue triggered by incomplete content ending (colon)"
			}

			logger.Debug("Auto-continue judge called (no tool calls): shouldContinue=%v, output=%q", shouldContinue, judgeOutput)

			if shouldContinue && autoContinueAttempts < o.getAutoContinueMaxAttempts() {
				autoContinueAttempts++
				logger.Info("Auto-continue triggered (attempt %d/%d)", autoContinueAttempts, o.getAutoContinueMaxAttempts())
				sendStream("\n⏭ Auto-continue requested.\n", false)
				sendStatus("Continuing...")
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
			sendStatus("")
			logger.Debug("Breaking out of loop: no tool calls and no auto-continue (iteration %d)", iteration)
			break
		}

		// Tool calls present - DO NOT auto-continue here
		// We need to execute the tools first and add tool result messages
		// Otherwise we'd create invalid message history: assistant[tool_calls] -> user[continue]
		// The auto-continue decision will be made in the next iteration after tool results are added
		if isTruncated || contentEndsIncomplete {
			logger.Debug("Response truncated with tool calls present (iteration %d). Will execute tools first, then auto-continue decision in next iteration.", iteration)
		}

		// Execute each tool call
		logger.Debug("Executing %d tool calls from iteration %d", len(response.ToolCalls), iteration)

		if err := o.processToolCalls(ctx, response.ToolCalls, o.session, progressCallback, authCallback, toolCallCallback, toolResultCallback, nil); err != nil {
			logger.Warn("Error processing tool calls: %v", err)
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
		sendStream(fmt.Sprintf("\n⚠️  Reached maximum iteration limit (%d). Stopping to prevent infinite loop.\n", maxIterations), false)
	}

	logger.Debug("ProcessPrompt completed")
	return nil
}

const maxVerificationRetries = 3

// ProcessPromptWithVerification wraps ProcessPrompt with automatic verification retry.
// If verification fails, it feeds the failures back to the LLM and retries up to 3 times.
// This is now the main entry point - ProcessPrompt handles orchestration, this handles verification retry.
func (o *Orchestrator) ProcessPromptWithVerification(
	ctx context.Context,
	prompt string,
	progressCallback progress.Callback,
	contextCallback ContextUsageCallback,
	authCallback AuthorizationCallback,
	toolCallCallback ToolCallCallback,
	toolResultCallback ToolResultCallback,
	openRouterUsageCallback OpenRouterUsageCallback,
) error {
	// Track queued prompt count to detect new prompts during verification
	initialQueuedCount := 0 // Will be read from session

	for attempt := 1; attempt <= maxVerificationRetries; attempt++ {
		// Mark verification attempt in session
		o.session.StartVerificationAttempt()
		initialQueuedCount = o.session.GetQueuedUserPromptCount()

		// Run main orchestration loop
		err := o.ProcessPrompt(ctx, prompt, progressCallback, contextCallback, authCallback, toolCallCallback, toolResultCallback, openRouterUsageCallback)
		if err != nil {
			o.session.ResetVerification()
			return err
		}

		userMsgCountAfterPrompt := o.session.UserMessageCount()

		// After orchestration completes, run verification
		result, err := o.runVerificationPhaseIfNeeded(ctx, prompt, progressCallback)
		if err != nil {
			logger.Warn("Verification error on attempt %d: %v", attempt, err)
			// Continue despite error - don't fail the whole operation
		}

		// If verification skipped (nil) or passed, we're done
		if result == nil || result.Success {
			o.session.ResetVerification()
			return nil
		}

		// Verification failed - check if we should retry
		if o.session.HasNewUserPromptOrQueued(userMsgCountAfterPrompt, initialQueuedCount) {
			logger.Info("New user prompt detected, stopping verification retry")
			o.session.ResetVerification()
			return nil
		}

		// Check if we've hit max attempts
		if attempt >= maxVerificationRetries {
			logger.Info("Max verification attempts (%d) reached", maxVerificationRetries)
			dispatchProgress(progressCallback, progress.Update{
				Message: fmt.Sprintf("\n⚠️  Maximum verification attempts (%d) reached. Please review failures and fix manually if needed.\n", maxVerificationRetries),
				Mode:    progress.ReportNoStatus,
			})
			o.session.ResetVerification()
			return nil
		}

		// Feed failure back to LLM for next attempt
		agent := NewVerificationAgent(o)
		feedbackMsg := agent.formatVerificationFailureFeedback(result, attempt)
		if strings.TrimSpace(feedbackMsg) == "" {
			feedbackMsg = "Verification failed, but no summary or errors were provided. Please review the verification output and fix any issues."
		}

		dispatchProgress(progressCallback, progress.Update{
			Message: fmt.Sprintf("\n🔄 Verification attempt %d/%d failed. Requesting fixes from LLM...\n\n", attempt, maxVerificationRetries),
			Mode:    progress.ReportNoStatus,
		})

		// Loop will call ProcessPrompt again with feedback as the next user prompt
		prompt = feedbackMsg
	}

	o.session.ResetVerification()
	return nil
}

// runVerificationPhaseIfNeeded runs verification after the main orchestration loop completes.
// It checks if files were modified and runs build/lint/test to verify the implementation.
// Returns the verification result and any error encountered.
func (o *Orchestrator) runVerificationPhaseIfNeeded(ctx context.Context, prompt string, progressCallback progress.Callback) (*VerificationResult, error) {
	// Check if verification is enabled
	if !o.featureFlags.IsToolEnabled("verification_agent") {
		logger.Debug("Verification phase disabled via feature flags")
		return nil, nil
	}

	// Check if summarize client is available
	if o.summarizeClient == nil {
		logger.Debug("Verification phase skipped: no summarize client")
		return nil, nil
	}

	// Get list of modified files from session
	filesModified := o.session.GetModifiedFiles()
	if len(filesModified) == 0 {
		logger.Debug("Verification phase skipped: no files modified")
		return nil, nil
	}

	// Collect user prompts from session (excluding "continue" messages)
	userPrompts := o.collectUserPrompts(10) // Last 10 user prompts
	if len(userPrompts) == 0 {
		userPrompts = []string{prompt}
	}

	// Get planning questions and answers from session
	questionsAnswered := o.session.GetPlanningQuestionsAnswered()

	// Create verification agent
	agent := NewVerificationAgent(o)

	// Run verification
	result, err := agent.Verify(ctx, userPrompts, filesModified, questionsAnswered, progressCallback)
	if err != nil {
		return nil, err
	}

	// Report results
	agent.reportResults(result, progressCallback)

	return result, nil
}

// collectUserPrompts collects recent user prompts from the session.
func (o *Orchestrator) collectUserPrompts(limit int) []string {
	messages := o.session.GetMessages()
	prompts := make([]string, 0, limit)

	for i := len(messages) - 1; i >= 0 && len(prompts) < limit; i-- {
		msg := messages[i]
		if msg.Role == "user" && msg.Content != "continue" && !strings.HasPrefix(msg.Content, "continue") {
			prompts = append(prompts, msg.Content)
		}
	}

	// Reverse to chronological order
	for i, j := 0, len(prompts)-1; i < j; i, j = i+1, j-1 {
		prompts[i], prompts[j] = prompts[j], prompts[i]
	}

	return prompts
}

func (o *Orchestrator) executeToolWithMode(ctx context.Context, call *tools.ToolCall, toolName string, progressCb progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback, approved bool) (*tools.ToolResult, error) {
	return o.ExecuteTool(ctx, call, toolName, progressCb, toolCallCb, toolResultCb, approved)
}

func (o *Orchestrator) processToolCalls(
	ctx context.Context,
	toolCalls []map[string]interface{},
	sess *session.Session,
	progressCb progress.Callback,
	authCb AuthorizationCallback,
	toolCallCb ToolCallCallback,
	toolResultCb ToolResultCallback,
	execFn toolExecutionFunc,
) error {
	execFunc := execFn
	if execFunc == nil {
		execFunc = o.executeToolWithMode
	}

	type toolCallResult struct {
		idx      int
		message  *session.Message
		toolName string
		toolID   string
		uiResult string
		errorMsg string
		metadata *tools.ExecutionMetadata
	}

	var (
		wg      sync.WaitGroup
		results = make([]*toolCallResult, len(toolCalls))
		authMu  sync.Mutex // serialize user auth prompts to avoid overlapping requests
	)

	for i, toolCall := range toolCalls {
		toolID, _ := toolCall["id"].(string)
		toolType, _ := toolCall["type"].(string)

		if toolType != "function" {
			errorMsg := fmt.Sprintf("Invalid tool type: %s", toolType)
			results[i] = &toolCallResult{
				idx: i,
				message: &session.Message{
					Role:    "tool",
					Content: errorMsg,
					ToolID:  toolID,
				},
				toolName: "unknown",
				toolID:   toolID,
				uiResult: errorMsg,
				errorMsg: errorMsg,
			}
			// Notify UI about tool result
			if toolResultCb != nil {
				if err := toolResultCb("unknown", toolID, errorMsg, errorMsg); err != nil {
					logger.Warn("Failed to send tool result message: %v", err)
				}
			}
			continue
		}

		function, ok := toolCall["function"].(map[string]interface{})
		if !ok {
			errorMsg := "Invalid function format in tool call"
			results[i] = &toolCallResult{
				idx: i,
				message: &session.Message{
					Role:    "tool",
					Content: errorMsg,
					ToolID:  toolID,
				},
				toolName: "unknown",
				toolID:   toolID,
				uiResult: errorMsg,
				errorMsg: errorMsg,
			}
			// Notify UI about tool result
			if toolResultCb != nil {
				if err := toolResultCb("unknown", toolID, errorMsg, errorMsg); err != nil {
					logger.Warn("Failed to send tool result message: %v", err)
				}
			}
			continue
		}

		toolName, _ := function["name"].(string)
		argsJSON, _ := function["arguments"].(string)
		logger.Debug("Executing tool: %s (id=%s)", toolName, toolID)

		// Notify UI about tool call
		dispatchProgress(progressCb, progress.Update{
			Message:   fmt.Sprintf("Calling tool: %s", toolName),
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		})

		// Parse arguments
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			results[i] = &toolCallResult{
				idx: i,
				message: &session.Message{
					Role:    "tool",
					Content: fmt.Sprintf("Error parsing tool arguments: %v", err),
					ToolID:  toolID,
				},
				toolName: toolName,
				toolID:   toolID,
				uiResult: fmt.Sprintf("Error parsing tool arguments: %v", err),
				errorMsg: fmt.Sprintf("Error parsing tool arguments: %v", err),
			}
			continue
		}

		toolCallObj := &tools.ToolCall{
			ID:         toolID,
			Name:       toolName,
			Parameters: args,
		}

		// Notify UI about tool call details
		if toolCallCb != nil {
			if err := toolCallCb(toolName, toolID, args); err != nil {
				logger.Warn("Failed to send tool call message: %v", err)
			}
		}

		wg.Add(1)
		go func(idx int, toolName, toolID string, args map[string]interface{}, callObj *tools.ToolCall) {
			defer wg.Done()

			// Execute the tool and capture the result
			var result *tools.ToolResult
			var execErr error

			result, execErr = execFunc(ctx, callObj, toolName, progressCb, toolCallCb, toolResultCb, false)
			if execErr != nil {
				result = &tools.ToolResult{
					ID:    toolID,
					Error: execErr.Error(),
				}
			}

			// Check if authorization is required
			if result.RequiresUserInput {
				// Ask user for approval - prefer userInteractionClient, fall back to authCb
				approved := false
				suggestedPrefix := result.SuggestedCommandPrefix
				tabID := o.GetUserInteractionTabID()

				if o.userInteractionClient != nil {
					// Use new actor-based authorization
					authMu.Lock()
					resp, err := o.userInteractionClient.RequestAuthorization(
						ctx,
						toolName,
						args,
						result.AuthReason,
						suggestedPrefix,
						tabID,
					)
					authMu.Unlock()

					if err != nil {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: fmt.Sprintf("Authorization error: %v", err),
						}
					} else if resp.TimedOut {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: "Authorization timed out - denied by default",
						}
					} else if resp.Cancelled {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: "Authorization cancelled by user",
						}
					} else if resp.Approved {
						approved = true
					} else {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: "Operation denied by user",
						}
					}
				} else if authCb != nil {
					// Fall back to legacy callback
					var err error
					authMu.Lock()
					approved, err = authCb(toolName, args, result.AuthReason)
					authMu.Unlock()
					if err != nil {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: fmt.Sprintf("Authorization error: %v", err),
						}
					} else if !approved {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: "Operation denied by user",
						}
					}
				} else {
					// No authorization mechanism available, deny by default
					result = &tools.ToolResult{
						ID:    toolID,
						Error: "Authorization required but no approval mechanism available",
					}
				}

				// If approved, persist command prefix and re-execute
				if approved {
					if suggestedPrefix != "" {
						sess.AuthorizeCommand(suggestedPrefix)
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

					result, execErr = execFunc(ctx, callObj, toolName, progressCb, toolCallCb, toolResultCb, true)
					if execErr != nil {
						result = &tools.ToolResult{
							ID:    toolID,
							Error: execErr.Error(),
						}
					}
				}
			}

			// Format result as string for LLM and UI
			var toolResult string // For LLM
			var uiResult string   // For UI display
			var executionMetadata *tools.ExecutionMetadata

			if result.Error != "" {
				toolResult = fmt.Sprintf("Error: %s", result.Error)
				uiResult = toolResult
			} else {
				// Extract execution metadata if present
				if resultMap, ok := result.Result.(map[string]interface{}); ok {
					if metadata, hasMetadata := resultMap["_execution_metadata"]; hasMetadata {
						if metadataObj, ok := metadata.(*tools.ExecutionMetadata); ok {
							executionMetadata = metadataObj
						}
					}

					if jsonBytes, err := json.Marshal(resultMap); err == nil {
						toolResult = string(jsonBytes)
					} else {
						toolResult = fmt.Sprintf("%v", result.Result)
					}
				} else {
					toolResult = fmt.Sprintf("%v", result.Result)
				}

				// Use UIResult for UI if available, otherwise use the same result as LLM
				if result.UIResult != nil {
					if uiStr, ok := result.UIResult.(string); ok {
						uiResult = uiStr
					} else {
						uiResult = fmt.Sprintf("%v", result.UIResult)
					}
				} else {
					uiResult = toolResult
				}
			}

			results[idx] = &toolCallResult{
				idx: idx,
				message: &session.Message{
					Role:     "tool",
					Content:  toolResult,
					ToolID:   toolID,
					ToolName: toolName,
				},
				toolName: toolName,
				toolID:   toolID,
				uiResult: uiResult,
				errorMsg: result.Error,
				metadata: executionMetadata,
			}
		}(i, toolName, toolID, args, toolCallObj)
	}

	wg.Wait()

	for _, res := range results {
		if res == nil {
			continue
		}

		// Notify UI about tool result (using UI-specific format)
		if toolResultCb != nil {
			if err := o.enhancedToolResultCallback(toolResultCb, res.toolName, res.toolID, res.uiResult, res.errorMsg, res.metadata); err != nil {
				logger.Warn("Failed to send tool result message: %v", err)
			}
		}

		// Add tool result to session (using LLM format)
		sess.AddMessage(res.message)
	}

	return nil
}

func parseToolCallValidationError(err error) *toolCallValidationError {
	if err == nil {
		return nil
	}

	errMsg := err.Error()

	// Case 1: Missing required parameter in tool call
	matches := toolCallValidationRegex.FindStringSubmatch(errMsg)
	if len(matches) >= 3 {
		missing := strings.Trim(matches[1], `"' `)
		toolName := strings.Trim(matches[2], `"' `)
		if missing != "" && toolName != "" {
			return &toolCallValidationError{
				toolName:     toolName,
				missingParam: missing,
				inner:        err,
			}
		}
	}

	// Case 2: Tool not available in request.tools (like shell tool)
	matches = toolNotInRequestRegex.FindStringSubmatch(errMsg)
	if len(matches) >= 2 {
		toolName := strings.Trim(matches[1], `"' `)
		if toolName != "" {
			return &toolCallValidationError{
				toolName:     toolName,
				missingParam: fmt.Sprintf("tool '%s' is not available (not in request.tools)", toolName),
				inner:        err,
			}
		}
	}

	return nil
}

// completeWithRetry wraps LLM completion with error retry logic
func (o *Orchestrator) completeWithRetry(ctx context.Context, req *llm.CompletionRequest, progressCallback progress.Callback) (*llm.CompletionResponse, error) {
	modelID := o.providerMgr.GetOrchestrationModel()

	sendStatus := func(msg string) {
		dispatchProgress(progressCallback, progress.Update{
			Message:   msg,
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		})
	}

	sendStream := func(msg string) {
		dispatchProgress(progressCallback, progress.Update{
			Message: msg,
			Mode:    progress.ReportNoStatus,
		})
	}

	for attempt := 1; attempt <= errorRetryMaxAttempts; attempt++ {
		// Try the completion
		response, err := o.orchestrationClient.CompleteWithRequest(ctx, req)

		// Success - return immediately
		if err == nil {
			return response, nil
		}

		// Log the error
		logger.Warn("LLM completion error (attempt %d/%d): %v", attempt, errorRetryMaxAttempts, err)

		if validationErr := parseToolCallValidationError(err); validationErr != nil {
			return nil, validationErr
		}

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
			sendStream(fmt.Sprintf("\n⚠️  %s\n", decision.Reason))
			return nil, err
		}

		// Check if compaction should be triggered
		if decision.TriggerCompaction {
			logger.Info("Error judge decided to trigger compaction: %s", decision.Reason)
			sendStream(fmt.Sprintf("\n🧹 %s - triggering context compaction...\n", decision.Reason))
			return nil, &contextSizeExceededError{inner: err, reason: decision.Reason}
		}

		// Notify user about retry
		logger.Info("Error judge decided to retry (attempt %d/%d, sleep %ds): %s",
			attempt, errorRetryMaxAttempts, decision.SleepSeconds, decision.Reason)

		sendStream(fmt.Sprintf("\n⏳ Retrying in %d seconds... (Attempt %d/%d: %s)\n",
			decision.SleepSeconds, attempt, errorRetryMaxAttempts, decision.Reason))

		sendStatus(fmt.Sprintf("Retrying in %ds...", decision.SleepSeconds))

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
		sendStatus(fmt.Sprintf("Retrying (attempt %d/%d)...", attempt+1, errorRetryMaxAttempts))
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

func (o *Orchestrator) maybeCompactContext(modelID, systemPrompt string, sessionMessages []*session.Message, perMessageTokens []int, totalTokens int, progressCallback progress.Callback, contextCallback ContextUsageCallback) {
	if len(sessionMessages) < 4 {
		return
	}

	contextWindow := o.getContextWindow(modelID)
	if contextWindow <= 0 {
		return
	}

	if totalTokens*100/contextWindow < compactionThresholdPercent {
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

	prefixCount = adjustCompactionBoundaryForTools(sessionMessages, prefixCount)
	if prefixCount <= 0 {
		return
	}

	messagesCopy := append([]*session.Message(nil), sessionMessages[:prefixCount]...)

	o.compactionMu.Lock()
	if o.compactionInProgress {
		o.compactionMu.Unlock()
		return
	}
	o.compactionInProgress = true
	o.compactionMu.Unlock()

	go o.compactContext(modelID, systemPrompt, contextCallback, messagesCopy, progressCallback)
}

func (o *Orchestrator) compactContext(modelID, systemPrompt string, contextCallback ContextUsageCallback, messages []*session.Message, progressCallback progress.Callback) {
	o.compactContextWithAttempt(modelID, systemPrompt, contextCallback, messages, progressCallback, 1)
}

// compactContextWithAttempt performs compaction with a specific attempt number,
// using increasingly forceful prompts for subsequent attempts.
func (o *Orchestrator) compactContextWithAttempt(modelID, systemPrompt string, contextCallback ContextUsageCallback, messages []*session.Message, progressCallback progress.Callback, attemptNumber int) {
	defer func() {
		o.compactionMu.Lock()
		o.compactionInProgress = false
		o.compactionMu.Unlock()
	}()

	if len(messages) == 0 {
		return
	}

	// Clamp attempt number to valid range
	if attemptNumber < 1 {
		attemptNumber = 1
	}
	if attemptNumber > maxCompactionAttempts {
		attemptNumber = maxCompactionAttempts
	}

	contextWindow := o.getContextWindow(modelID)
	_, perMessageTokens, _ := estimateContextTokens(modelID, "", messages)
	latestUserPrompt := findLatestUserPrompt(o.session.GetMessages())

	summary := ""

	// Determine prompt and max bytes based on attempt number
	var basePrompt string
	var maxBytes int
	var attemptDesc string

	switch attemptNumber {
	case 1:
		basePrompt = compactionPromptStandard
		maxBytes = compactionMaxBytesAttempt1
		attemptDesc = "standard"
	case 2:
		basePrompt = compactionPromptForceful
		maxBytes = compactionMaxBytesAttempt2
		attemptDesc = "forceful"
	case 3:
		basePrompt = compactionPromptExtreme
		maxBytes = compactionMaxBytesAttempt3
		attemptDesc = "extreme"
	default:
		basePrompt = compactionPromptExtreme
		maxBytes = compactionMaxBytesAttempt3
		attemptDesc = "extreme"
	}

	logger.Info("compaction: attempt %d/%d using %s prompt (max %d bytes)", attemptNumber, maxCompactionAttempts, attemptDesc, maxBytes)

	// Use the abstracted chunked summarizer
	if o.summarizeClient != nil {
		// Build conversation content for summarization
		conversationContent := buildConversationContent(messages)

		// Create chunked summarizer
		chunkedSummarizer := summarizer.NewChunkedSummarizer(o.summarizeClient)

		// Summarize with automatic chunking
		result, err := chunkedSummarizer.Summarize(context.Background(), conversationContent, summarizer.SummarizeOptions{
			BasePrompt: basePrompt,
			MaxBytes:   maxBytes,
			ProgressCallback: func(status string) {
				logger.Debug("compaction[%d]: %s", attemptNumber, status)
			},
		})

		if err != nil {
			logger.Warn("compaction[%d]: summary failed: %v", attemptNumber, err)
			summary = fallbackConversationSummary(messages)
		} else {
			summary = strings.TrimSpace(result.Summary)
			logger.Info("compaction[%d]: summarized %d messages using %d chunks, %d total tokens, %d chars output",
				attemptNumber, len(messages), result.ChunksUsed, result.TotalTokens, len(summary))
		}
	} else {
		summary = fallbackConversationSummary(messages)
	}

	if summary == "" {
		summary = fallbackConversationSummary(messages)
	}

	if summary == "" {
		return
	}

	// Update summary content to indicate attempt number if it's a retry
	var summaryLabel string
	if attemptNumber == 1 {
		summaryLabel = "auto-compacted"
	} else {
		summaryLabel = fmt.Sprintf("auto-compacted (attempt %d/%d)", attemptNumber, maxCompactionAttempts)
	}

	summaryContent := fmt.Sprintf("Summary of earlier context (%s):\n%s", summaryLabel, summary)
	userSection := buildUserCompactionSection(messages, perMessageTokens, contextWindow, latestUserPrompt)
	if userSection != "" {
		summaryContent = fmt.Sprintf("%s\n\n%s", summaryContent, userSection)
	}

	if !o.session.CompactWithSummary(messages, summaryContent) {
		// Session head changed before compaction could apply
		return
	}

	o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

	// Update progress message based on attempt number
	var progressMsg string
	if attemptNumber == 1 {
		progressMsg = "\n🧹 Auto-compacted earlier context.\n"
	} else {
		progressMsg = fmt.Sprintf("\n🧹 Auto-compacted earlier context (attempt %d/%d).\n", attemptNumber, maxCompactionAttempts)
	}

	dispatchProgress(progressCallback, progress.Update{
		Message: progressMsg,
	})
}

// forceCompactContext runs compaction synchronously when context size is exceeded.
// It uses multi-stage compaction with increasingly forceful prompts if needed.
func (o *Orchestrator) forceCompactContext(modelID, systemPrompt string, sessionMessages []*session.Message, progressCallback progress.Callback, contextCallback ContextUsageCallback) {
	if len(sessionMessages) < 4 {
		logger.Debug("forceCompactContext: not enough messages to compact (%d)", len(sessionMessages))
		return
	}

	// Wait for any in-progress compaction to finish
	o.compactionMu.Lock()
	if o.compactionInProgress {
		o.compactionMu.Unlock()
		logger.Debug("forceCompactContext: compaction already in progress, waiting...")
		// Wait a bit and check again
		time.Sleep(100 * time.Millisecond)
		o.compactionMu.Lock()
		for o.compactionInProgress {
			o.compactionMu.Unlock()
			time.Sleep(100 * time.Millisecond)
			o.compactionMu.Lock()
		}
	}
	o.compactionInProgress = true
	o.compactionMu.Unlock()

	defer func() {
		o.compactionMu.Lock()
		o.compactionInProgress = false
		o.compactionMu.Unlock()
	}()

	// Compact more aggressively - target 60% of messages
	prefixCount := len(sessionMessages) * 60 / 100
	if prefixCount < 2 {
		prefixCount = 2
	}

	// Ensure we keep at least two recent messages un-compacted
	if len(sessionMessages)-prefixCount < 2 {
		prefixCount = len(sessionMessages) - 2
		if prefixCount <= 0 {
			logger.Debug("forceCompactContext: cannot compact, too few messages")
			return
		}
	}

	_, perMessageTokens, _ := estimateContextTokens(modelID, "", sessionMessages)
	prefixCount = adjustCompactionBoundaryForTools(sessionMessages, prefixCount)
	if prefixCount <= 0 {
		logger.Debug("forceCompactContext: no messages to compact after boundary adjustment")
		return
	}

	messagesCopy := append([]*session.Message(nil), sessionMessages[:prefixCount]...)
	logger.Info("forceCompactContext: compacting %d messages", len(messagesCopy))

	// Run compaction synchronously with attempt-based retry
	contextWindow := o.getContextWindow(modelID)
	latestUserPrompt := findLatestUserPrompt(o.session.GetMessages())

	summary := ""

	// Use increasing forceful prompts based on attempt count
	o.compactionAttemptMu.Lock()
	attemptNum := o.compactionAttemptCount + 1
	o.compactionAttemptMu.Unlock()

	if o.summarizeClient != nil {
		conversationContent := buildConversationContent(messagesCopy)
		chunkedSummarizer := summarizer.NewChunkedSummarizer(o.summarizeClient)

		// Determine prompt and max bytes based on attempt number
		var basePrompt string
		var maxBytes int
		var attemptDesc string

		switch attemptNum {
		case 1:
			basePrompt = compactionPromptStandard
			maxBytes = compactionMaxBytesAttempt1
			attemptDesc = "standard"
		case 2:
			basePrompt = compactionPromptForceful
			maxBytes = compactionMaxBytesAttempt2
			attemptDesc = "forceful"
		case 3:
			basePrompt = compactionPromptExtreme
			maxBytes = compactionMaxBytesAttempt3
			attemptDesc = "extreme"
		default:
			basePrompt = compactionPromptExtreme
			maxBytes = compactionMaxBytesAttempt3
			attemptDesc = "extreme"
		}

		logger.Info("forceCompactContext: attempt %d/%d using %s prompt (max %d bytes)", attemptNum, maxCompactionAttempts, attemptDesc, maxBytes)

		result, err := chunkedSummarizer.Summarize(context.Background(), conversationContent, summarizer.SummarizeOptions{
			BasePrompt: basePrompt,
			MaxBytes:   maxBytes,
			ProgressCallback: func(status string) {
				logger.Debug("forceCompactContext[%d]: %s", attemptNum, status)
			},
		})

		if err != nil {
			logger.Warn("forceCompactContext[%d]: summary failed: %v", attemptNum, err)
			summary = fallbackConversationSummary(messagesCopy)
		} else {
			summary = strings.TrimSpace(result.Summary)
			logger.Info("forceCompactContext[%d]: summarized %d messages using %d chunks, %d total tokens, %d chars output",
				attemptNum, len(messagesCopy), result.ChunksUsed, result.TotalTokens, len(summary))
		}
	}

	if summary == "" {
		summary = fallbackConversationSummary(messagesCopy)
	}

	if summary == "" {
		logger.Debug("forceCompactContext: no summary generated")
		return
	}

	// Update compaction attempt counter
	o.compactionAttemptMu.Lock()
	o.compactionAttemptCount = attemptNum
	o.compactionAttemptMu.Unlock()

	// Update summary content to indicate attempt number if it's a retry
	var summaryLabel string
	if attemptNum == 1 {
		summaryLabel = "force-compacted due to context size limit"
	} else {
		summaryLabel = fmt.Sprintf("force-compacted (attempt %d/%d) due to context size limit", attemptNum, maxCompactionAttempts)
	}

	summaryContent := fmt.Sprintf("Summary of earlier context (%s):\n%s", summaryLabel, summary)
	userSection := buildUserCompactionSection(messagesCopy, perMessageTokens, contextWindow, latestUserPrompt)
	if userSection != "" {
		summaryContent = fmt.Sprintf("%s\n\n%s", summaryContent, userSection)
	}

	if !o.session.CompactWithSummary(messagesCopy, summaryContent) {
		logger.Debug("forceCompactContext: session head changed, compaction not applied")
		return
	}

	o.broadcastContextUsage(modelID, systemPrompt, contextCallback)

	// Update progress message based on attempt number
	var progressMsg string
	if attemptNum == 1 {
		progressMsg = "\n🧹 Force-compacted context due to size limit.\n"
	} else {
		progressMsg = fmt.Sprintf("\n🧹 Force-compacted context (attempt %d/%d) due to size limit.\n", attemptNum, maxCompactionAttempts)
	}

	dispatchProgress(progressCallback, progress.Update{
		Message: progressMsg,
	})

	logger.Info("forceCompactContext: completed successfully (attempt %d)", attemptNum)
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

	// Fallback to default context window
	return 8192
}

// getCurrentCompactionAttempt returns the current compaction attempt number
func (o *Orchestrator) getCurrentCompactionAttempt() int {
	o.compactionAttemptMu.Lock()
	defer o.compactionAttemptMu.Unlock()
	return o.compactionAttemptCount
}

// resetCompactionAttempts resets the compaction attempt counter
func (o *Orchestrator) resetCompactionAttempts() {
	o.compactionAttemptMu.Lock()
	o.compactionAttemptCount = 0
	o.compactionAttemptMu.Unlock()
}

// processOpenRouterUsage processes and sends OpenRouter usage data to the callback
func (o *Orchestrator) processOpenRouterUsage(usage map[string]interface{}, callback OpenRouterUsageCallback) {
	if callback == nil {
		logger.Debug("OpenRouter usage callback is nil, skipping usage processing")
		return
	}

	if usage != nil {
		logger.Debug("Processing OpenRouter usage data: %v", usage)
	} else {
		logger.Debug("OpenRouter usage data is nil, still calling callback")
	}

	_ = callback(usage)
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

// adjustCompactionBoundaryForTools ensures we never compact one half of a tool
// exchange and leave the other half dangling in the un-compacted tail.
func adjustCompactionBoundaryForTools(messages []*session.Message, prefixCount int) int {
	if prefixCount <= 0 || prefixCount >= len(messages) {
		return prefixCount
	}

	minUncompacted := 2
	maxPrefix := len(messages) - minUncompacted
	if maxPrefix < 0 {
		return prefixCount
	}
	if prefixCount > maxPrefix {
		prefixCount = maxPrefix
	}

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg == nil || !strings.EqualFold(msg.Role, "assistant") || len(msg.ToolCalls) == 0 {
			continue
		}

		start := i
		end := i
		for end+1 < len(messages) && strings.EqualFold(messages[end+1].Role, "tool") {
			end++
		}

		if start < prefixCount && prefixCount <= end {
			forward := end + 1
			if forward > maxPrefix {
				prefixCount = start
			} else {
				prefixCount = forward
			}
			// Restart scan because the boundary moved and may intersect earlier blocks.
			i = -1
		}
	}

	return prefixCount
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

// buildConversationContent builds the conversation content for summarization
func buildConversationContent(messages []*session.Message) string {
	var sb strings.Builder
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
	if strings.Contains(model, "turbo") || strings.Contains(model, "4o") || strings.Contains(model, "o1") || strings.Contains(model, "o3") || strings.Contains(model, "devstral") {
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

	if o.planningAgentCancel != nil {
		o.planningAgentCancel()
	}

	if o.healthManager != nil {
		o.healthManager.Stop()
	}

	if o.shellActorCancel != nil {
		o.shellActorCancel()
	}

	// Note: domainBlockerCancel is now managed by RuntimeFactory as a shared resource
	// and should NOT be cancelled here - it's lifecycle is managed externally

	var firstErr error

	// Final save and stop autosave for the current session BEFORE cancelling the actor
	if o.session != nil && o.sessionStorageRef != nil && o.config.AutoSave.Enabled {
		// Try to save the session one final time
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := o.SaveCurrentSession(saveCtx); err != nil {
			logger.Warn("Failed to save session on shutdown: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		saveCancel()

		// Stop the autosave process
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := actor.StopAutoSaveViaActor(stopCtx, o.sessionStorageRef); err != nil {
			logger.Warn("Failed to stop autosave on shutdown: %v", err)
		}
		stopCancel()
	}

	// Now cancel the session storage actor after we've finished using it
	if o.sessionStorageCancel != nil {
		o.sessionStorageCancel()
	}

	if o.planningAgent != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := o.planningAgent.Close(shutdownCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

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

	// Note: Could store metadata for TUI access when we extend the callback interface
	// This could be done via a side channel, context, or enhanced callback signature
	// Current implementation uses progress callbacks for most metadata needs

	return nil
}

// ExecuteTool executes a tool call with optional callbacks; approved bypasses authorization.
func (o *Orchestrator) ExecuteTool(ctx context.Context, toolCall *tools.ToolCall, toolName string, progressCallback progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback, approved bool) (*tools.ToolResult, error) {
	ctx, cleanup := o.prepareShellExecutionContext(ctx, toolCall, toolName)
	if cleanup != nil {
		defer cleanup()
	}

	var progressFn progress.Callback
	if progressCallback != nil {
		progressFn = progressCallback
	}

	if o.toolExecutor != nil {
		return o.toolExecutor.ExecuteWithCallbacks(ctx, toolCall, toolName, progressFn, toolCallCb, toolResultCb, approved)
	}

	result := o.toolRegistry.ExecuteWithCallbacks(ctx, toolCall, toolName, progressFn, toolCallCb, toolResultCb, approved)
	return result, nil
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

// GetTodoActor returns the TodoActorInterface for ACP access
func (o *Orchestrator) GetTodoActor() tools.TodoActorInterface {
	return o.todoActor
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

// GetFeatureFlags returns the feature flags manager
func (o *Orchestrator) GetFeatureFlags() *features.FeatureFlags {
	return o.featureFlags
}

// ClearSession clears the current session, removing all messages, file tracking, and todos
func (o *Orchestrator) ClearSession() error {
	o.session.Clear()

	if o.todoClient != nil {
		if err := o.todoClient.Clear(); err != nil {
			return fmt.Errorf("failed to clear todos: %w", err)
		}
	}

	// Clear cached system prompt so it gets regenerated for the new session
	o.systemPromptMu.Lock()
	o.cachedSystemPrompt = ""
	o.systemPromptMu.Unlock()
	logger.Debug("System prompt cache cleared for new session")

	return nil
}

// getOrBuildSystemPrompt returns the cached system prompt or builds a new one if not cached
func (o *Orchestrator) getOrBuildSystemPrompt(ctx context.Context, modelID string) (string, error) {
	// Try to read from cache first
	o.systemPromptMu.RLock()
	if o.cachedSystemPrompt != "" {
		cached := o.cachedSystemPrompt
		o.systemPromptMu.RUnlock()
		logger.Debug("Using cached system prompt (%d chars)", len(cached))
		return cached, nil
	}
	o.systemPromptMu.RUnlock()

	// Need to build it - acquire write lock
	o.systemPromptMu.Lock()
	defer o.systemPromptMu.Unlock()

	// Check again in case another goroutine built it while we waited for the lock
	if o.cachedSystemPrompt != "" {
		logger.Debug("Using cached system prompt built by another goroutine (%d chars)", len(o.cachedSystemPrompt))
		return o.cachedSystemPrompt, nil
	}

	// Build the system prompt
	logger.Debug("Building new system prompt for session")
	promptBuilder := llm.NewPromptBuilder(o.fs, o.workingDir, o.config)
	systemPrompt, err := promptBuilder.BuildSystemPrompt(ctx, modelID, o.cliMode, o.toolRegistry.ToJSONSchema())
	if err != nil {
		return "", err
	}

	// Cache it
	o.cachedSystemPrompt = systemPrompt
	logger.Info("System prompt built and cached for session (%d chars)", len(systemPrompt))

	return systemPrompt, nil
}

// GetSession returns the current session
func (o *Orchestrator) GetSession() *session.Session {
	return o.session
}

// SetSession replaces the current session with a new one
func (o *Orchestrator) SetSession(newSession *session.Session) {
	// Stop autosave for the current session if any
	if o.session != nil && o.sessionStorageRef != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := actor.StopAutoSaveViaActor(ctx, o.sessionStorageRef); err != nil {
			logger.Warn("Failed to stop autosave for previous session: %v", err)
		}
	}

	// Ensure the session has the same working directory
	newSession.WorkingDir = o.workingDir

	// Replace the session
	o.session = newSession

	// Start autosave for the new session if enabled
	if o.sessionStorageRef != nil && o.config.AutoSave.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		sessionName := actor.GenerateSessionName("")
		if err := actor.StartAutoSaveViaActor(ctx, o.sessionStorageRef, o.session, sessionName); err != nil {
			logger.Warn("Failed to start autosave for new session: %v", err)
		} else {
			logger.Info("Started autosave for session %s", o.session.ID)
		}
	}

	// Rebuild tools to ensure they use the new session
	// This is important so tools like read_file respect the new session's file tracking
	err := o.rebuildSessionTools()
	if err != nil {
		logger.Warn("Failed to rebuild session tools after session replacement: %v", err)
	}
}

// SaveCurrentSession saves the current session to persistent storage
func (o *Orchestrator) SaveCurrentSession(ctx context.Context) error {
	if o.session == nil {
		return fmt.Errorf("no active session to save")
	}
	if o.sessionStorageRef == nil {
		return fmt.Errorf("session storage not available")
	}

	sessionName := actor.GenerateSessionName("")

	err := actor.SaveSessionViaActor(ctx, o.sessionStorageRef, o.session, sessionName)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	logger.Debug("Session saved successfully: %s", o.session.ID)
	return nil
}

// SetUserInputCallback sets the callback for user input during planning
// Deprecated: Use SetUserInteractionHandler instead
func (o *Orchestrator) SetUserInputCallback(callback UserInputCallback) {
	o.userInputCb = callback
}

// SetUserInteractionHandler sets the handler for user interactions.
// This replaces the callback-based authorization and user input mechanisms
// with a proper actor-based system.
func (o *Orchestrator) SetUserInteractionHandler(handler actor.UserInteractionHandler) error {
	// Stop existing actor if any
	if o.userInteractionCancel != nil {
		o.userInteractionCancel()
		o.userInteractionCancel = nil
	}
	if o.userInteractionRef != nil {
		_ = o.userInteractionRef.Stop(context.Background())
		o.userInteractionRef = nil
	}
	o.userInteractionClient = nil

	if handler == nil {
		logger.Debug("UserInteractionHandler cleared")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	userInteractionActor := actor.NewUserInteractionActor("user_interaction", handler)
	ref, err := o.actorSystem.Spawn(ctx, "user_interaction", userInteractionActor, 16)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to spawn user interaction actor: %w", err)
	}

	o.userInteractionRef = ref
	o.userInteractionCancel = cancel
	o.userInteractionClient = actor.NewUserInteractionClient(ref)

	logger.Info("UserInteractionHandler set with mode: %s", handler.Mode())
	return nil
}

// GetUserInteractionClient returns the user interaction client if available
func (o *Orchestrator) GetUserInteractionClient() *actor.UserInteractionClient {
	return o.userInteractionClient
}

// SetUserInteractionTabID sets the tab ID used for user interaction requests
func (o *Orchestrator) SetUserInteractionTabID(tabID int) {
	o.userInteractionTabIDMu.Lock()
	defer o.userInteractionTabIDMu.Unlock()
	o.userInteractionTabID = tabID
}

// GetUserInteractionTabID returns the current tab ID for user interaction requests
func (o *Orchestrator) GetUserInteractionTabID() int {
	o.userInteractionTabIDMu.Lock()
	defer o.userInteractionTabIDMu.Unlock()
	return o.userInteractionTabID
}

// GenerateSessionTitle generates an auto-title for the current session based on
// the first user message and session context. This should be called before saving.
func (o *Orchestrator) GenerateSessionTitle(ctx context.Context) error {
	// Don't regenerate if title already exists
	if o.session.GetTitle() != "" {
		logger.Debug("Session already has title: %s", o.session.GetTitle())
		return nil
	}

	// Get the first user message
	messages := o.session.GetMessages()
	var userPrompt string
	for _, msg := range messages {
		if msg.Role == "user" {
			userPrompt = msg.Content
			break
		}
	}

	if userPrompt == "" {
		logger.Debug("No user messages found, skipping title generation")
		return nil
	}

	// Create title generator
	titleGen := session.NewTitleGenerator(o.summarizeClient)

	// Get workspace files (we'll use files that were read as context)
	filesRead := o.session.FilesRead
	workspaceFiles := session.ExtractFilesList(filesRead)

	// Generate the title
	title, err := titleGen.GenerateTitle(ctx, userPrompt, workspaceFiles, filesRead)
	if err != nil {
		logger.Warn("Failed to generate session title: %v", err)
		return err
	}

	// Set the title on the session
	o.session.SetTitle(title)
	logger.Info("Generated session title: %s", title)

	return nil
}

// rebuildSessionTools rebuilds tools that depend on the session
func (o *Orchestrator) rebuildSessionTools() error {
	// Rebuild all tools to ensure they use the new session
	errs := o.rebuildTools(true)
	if len(errs) > 0 {
		return fmt.Errorf("some tools failed to rebuild: %v", errs)
	}
	return nil
}

// GetActor returns an actor reference by name
func (o *Orchestrator) GetActor(name string) (*actor.ActorRef, bool) {
	return o.actorSystem.Get(name)
}
