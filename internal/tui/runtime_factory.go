package tui

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/orchestrator"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/session"
)

// SharedResources holds singleton resources shared across all tabs
type SharedResources struct {
	config               *config.Config
	providerMgr          *provider.Manager
	sessionStorageRef    *actor.ActorRef
	sessionStorageCancel context.CancelFunc
	sessionStorageSystem *actor.System // System that owns the session storage actor
	domainBlockerRef     *actor.ActorRef
	domainBlockerCancel  context.CancelFunc
	domainBlockerSystem  *actor.System // System that owns the domain blocker actor
	filesystem           fs.FileSystem // Shared CachedFS
}

// TabRuntime holds per-tab isolated resources
type TabRuntime struct {
	Orchestrator *orchestrator.Orchestrator
	ctx          context.Context
	cancel       context.CancelFunc
	tabID        int
}

// RuntimeFactory creates per-tab runtime instances
type RuntimeFactory struct {
	shared             *SharedResources
	runtimes           map[int]*TabRuntime // Map of tabID -> runtime
	mu                 sync.RWMutex
	workingDir         string
	cliMode            bool
	requireSandboxAuth bool
}

// NewRuntimeFactory creates a new factory with shared resources
func NewRuntimeFactory(cfg *config.Config, providerMgr *provider.Manager, workingDir string, cliMode bool) (*RuntimeFactory, error) {
	return NewRuntimeFactoryWithRequireSandboxAuth(cfg, providerMgr, workingDir, cliMode, false)
}

// NewRuntimeFactoryWithRequireSandboxAuth creates a factory with sandbox auth requirement
func NewRuntimeFactoryWithRequireSandboxAuth(cfg *config.Config, providerMgr *provider.Manager, workingDir string, cliMode bool, requireSandboxAuth bool) (*RuntimeFactory, error) {
	logger.Info("Creating RuntimeFactory for workspace: %s, requireSandboxAuth=%v", workingDir, requireSandboxAuth)

	// Create shared filesystem
	filesystem := fs.NewCachedFS(
		workingDir,
		time.Duration(cfg.CacheTTL)*time.Second,
		cfg.MaxCacheEntries,
	)

	// Create singleton session storage actor
	ctx, cancel := context.WithCancel(context.Background())
	actorSystem := actor.NewSystem()

	storageActor, err := actor.NewSessionStorageActorWithConfig("session-storage", func() *config.AutoSaveConfig {
		return &cfg.AutoSave
	})
	if err != nil {
		cancel()
		filesystem.Close()
		return nil, fmt.Errorf("failed to create session storage actor: %w", err)
	}

	storageRef, err := actorSystem.Spawn(ctx, "session-storage", storageActor, 16)
	if err != nil {
		cancel()
		filesystem.Close()
		return nil, fmt.Errorf("failed to spawn session storage actor: %w", err)
	}

	logger.Debug("Session storage actor spawned successfully")

	// Create singleton domain blocker actor
	domainBlockerCtx, domainBlockerCancel := context.WithCancel(context.Background())
	domainBlockerSystem := actor.NewSystem()
	domainBlockerConfig := actor.DomainBlockerConfig{
		BlocklistURL:    actor.DefaultRPZURL,
		RefreshInterval: consts.Duration6Hours,
		TTL:             consts.Duration24Hours,
		HTTPClient:      &http.Client{Timeout: consts.Timeout30Seconds},
	}
	domainBlockerActor := actor.NewDomainBlockerActor("domain-blocker", domainBlockerConfig)
	domainBlockerRef, err := domainBlockerSystem.Spawn(domainBlockerCtx, "domain-blocker", domainBlockerActor, 16)
	if err != nil {
		domainBlockerCancel()
		cancel()
		filesystem.Close()
		return nil, fmt.Errorf("failed to spawn domain blocker actor: %w", err)
	}

	logger.Debug("Domain blocker actor spawned successfully")

	shared := &SharedResources{
		config:               cfg,
		providerMgr:          providerMgr,
		sessionStorageRef:    storageRef,
		sessionStorageCancel: cancel,
		sessionStorageSystem: actorSystem,
		domainBlockerRef:     domainBlockerRef,
		domainBlockerCancel:  domainBlockerCancel,
		domainBlockerSystem:  domainBlockerSystem,
		filesystem:           filesystem,
	}

	return &RuntimeFactory{
		shared:             shared,
		runtimes:           make(map[int]*TabRuntime),
		workingDir:         workingDir,
		cliMode:            cliMode,
		requireSandboxAuth: requireSandboxAuth,
	}, nil
}

// CreateTabRuntime creates a new orchestrator runtime for a tab
func (rf *RuntimeFactory) CreateTabRuntime(tabID int, sess *session.Session) (*TabRuntime, error) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Check if runtime already exists
	if existing, ok := rf.runtimes[tabID]; ok {
		logger.Debug("Runtime for tab %d already exists, returning existing", tabID)
		return existing, nil
	}

	logger.Info("Creating runtime for tab %d (session: %s)", tabID, sess.ID)

	// Create per-tab context
	ctx, cancel := context.WithCancel(context.Background())

	// Create per-tab orchestrator with shared resources
	orch, err := orchestrator.NewOrchestratorWithSharedResources(
		rf.shared.config,
		rf.shared.providerMgr,
		rf.cliMode,
		rf.shared.filesystem,        // Shared FS
		sess,                        // Tab's session
		rf.shared.sessionStorageRef, // Shared session storage
		rf.shared.domainBlockerRef,  // Shared domain blocker
		rf.requireSandboxAuth,       // requireSandboxAuth (passed from CLI flag)
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create orchestrator for tab %d: %w", tabID, err)
	}

	runtime := &TabRuntime{
		Orchestrator: orch,
		ctx:          ctx,
		cancel:       cancel,
		tabID:        tabID,
	}

	rf.runtimes[tabID] = runtime
	logger.Debug("Runtime created successfully for tab %d", tabID)
	return runtime, nil
}

// GetTabRuntime retrieves an existing runtime
func (rf *RuntimeFactory) GetTabRuntime(tabID int) (*TabRuntime, bool) {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	runtime, ok := rf.runtimes[tabID]
	return runtime, ok
}

// DestroyTabRuntime cleans up a tab's runtime
func (rf *RuntimeFactory) DestroyTabRuntime(tabID int) error {
	rf.mu.Lock()
	runtime, ok := rf.runtimes[tabID]
	if !ok {
		rf.mu.Unlock()
		logger.Warn("No runtime found for tab %d", tabID)
		return fmt.Errorf("no runtime found for tab %d", tabID)
	}
	delete(rf.runtimes, tabID)
	rf.mu.Unlock()

	logger.Info("Destroying runtime for tab %d", tabID)

	// Cancel context and close orchestrator
	runtime.cancel()
	if err := runtime.Orchestrator.Close(); err != nil {
		logger.Warn("Failed to close orchestrator for tab %d: %v", tabID, err)
		return fmt.Errorf("failed to close orchestrator: %w", err)
	}

	logger.Debug("Runtime destroyed successfully for tab %d", tabID)
	return nil
}

// Close shuts down all resources
func (rf *RuntimeFactory) Close() error {
	logger.Info("Closing RuntimeFactory")

	rf.mu.Lock()
	runtimes := rf.runtimes
	rf.runtimes = make(map[int]*TabRuntime)
	rf.mu.Unlock()

	// Close all tab runtimes
	for tabID, runtime := range runtimes {
		logger.Debug("Closing runtime for tab %d", tabID)
		runtime.cancel()
		if err := runtime.Orchestrator.Close(); err != nil {
			logger.Warn("Failed to close orchestrator for tab %d: %v", tabID, err)
		}
	}

	// Close shared resources
	logger.Debug("Closing shared domain blocker actor")
	if rf.shared.domainBlockerRef != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := rf.shared.domainBlockerSystem.StopAll(shutdownCtx); err != nil {
			logger.Error("Error stopping domain blocker system: %v", err)
		}
		cancel()
	}

	rf.shared.domainBlockerCancel()

	logger.Debug("Closing shared session storage actor")
	if rf.shared.sessionStorageRef != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := rf.shared.sessionStorageSystem.StopAll(shutdownCtx); err != nil {
			logger.Error("Error stopping session storage system: %v", err)
		}
		cancel()
	}

	rf.shared.sessionStorageCancel()

	// Close shared filesystem
	logger.Debug("Closing shared filesystem")
	if cfs, ok := rf.shared.filesystem.(*fs.CachedFS); ok {
		cfs.Close()
	}

	logger.Info("RuntimeFactory closed successfully")
	return nil
}

// GetSharedFilesystem returns the shared filesystem instance
func (rf *RuntimeFactory) GetSharedFilesystem() fs.FileSystem {
	return rf.shared.filesystem
}

// GetWorkingDir returns the working directory
func (rf *RuntimeFactory) GetWorkingDir() string {
	return rf.workingDir
}

// GetConfig returns the shared config instance (use thread-safe methods!)
func (rf *RuntimeFactory) GetConfig() *config.Config {
	return rf.shared.config
}

// GetProviderManager returns the shared provider manager
func (rf *RuntimeFactory) GetProviderManager() *provider.Manager {
	return rf.shared.providerMgr
}

// GetSessionStorageRef returns the shared session storage actor reference
func (rf *RuntimeFactory) GetSessionStorageRef() *actor.ActorRef {
	return rf.shared.sessionStorageRef
}
