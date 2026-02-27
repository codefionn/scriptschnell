package socketserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/vcs"
)

// WorkspaceInternalInfo represents workspace metadata (internal use)
type WorkspaceInternalInfo struct {
	ID               string          `json:"id"`                // Unique workspace ID (hash of working dir)
	Path             string          `json:"path"`              // Working directory path
	Name             string          `json:"name"`              // Workspace name (from git repo or directory name)
	RepositoryRoot   string          `json:"repository_root"`   // Git repository root (empty if not in git repo)
	CurrentBranch    string          `json:"current_branch"`    // Current git branch (empty if not in git repo or detached)
	IsWorktree       bool            `json:"is_worktree"`       // Whether this is a git worktree
	WorktreeName     string          `json:"worktree_name"`     // Worktree name (if applicable)
	SessionCount     int             `json:"session_count"`     // Number of active sessions in this workspace
	LastAccessed     time.Time       `json:"last_accessed"`     // Last access time
	CreatedAt        time.Time       `json:"created_at"`        // When workspace was first registered
	ContextDirs      []string        `json:"context_dirs"`      // Context directories for this workspace
	LandlockRead     []string        `json:"landlock_read"`     // Landlock read-only paths
	LandlockWrite    []string        `json:"landlock_write"`    // Landlock read-write paths
	DomainsApproved  map[string]bool `json:"domains_approved"`  // Approved network domains
	CommandsApproved map[string]bool `json:"commands_approved"` // Approved command prefixes
}

// WorkspaceManager manages workspace lifecycle and state
type WorkspaceManager struct {
	mu sync.RWMutex

	// Workspace registry
	workspaces map[string]*WorkspaceInternalInfo // workspace ID -> WorkspaceInternalInfo

	// Path to workspace ID mapping (for quick lookup)
	pathToID map[string]string // working dir -> workspace ID

	// Configuration references
	configDir    string // Config directory for workspace-scoped settings
	configLoaded bool
}

// NewWorkspaceManager creates a new workspace manager
func NewWorkspaceManager() (*WorkspaceManager, error) {
	wm := &WorkspaceManager{
		workspaces: make(map[string]*WorkspaceInternalInfo),
		pathToID:   make(map[string]string),
	}

	return wm, nil
}

// ResolveWorkspace resolves a working directory to workspace info
// If the workspace doesn't exist, it's automatically registered
func (wm *WorkspaceManager) ResolveWorkspace(ctx context.Context, workingDir string) (*WorkspaceInternalInfo, error) {
	// Normalize the path
	absPath, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Check if directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("working directory does not exist: %s", absPath)
	}

	// Generate workspace ID (hash of the working dir)
	workspaceID := generateWorkspaceID(absPath)

	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Check if workspace already exists
	if ws, exists := wm.workspaces[workspaceID]; exists {
		// Update last accessed time
		ws.LastAccessed = time.Now()
		return ws, nil
	}

	// Create new workspace info
	ws, err := wm.createWorkspaceInfo(ctx, absPath, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace info: %w", err)
	}

	// Register workspace
	wm.workspaces[workspaceID] = ws
	wm.pathToID[absPath] = workspaceID

	logger.Info("Registered new workspace: %s at %s", ws.Name, absPath)

	return ws, nil
}

// GetWorkspace retrieves workspace info by ID
func (wm *WorkspaceManager) GetWorkspace(workspaceID string) (*WorkspaceInternalInfo, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	ws, exists := wm.workspaces[workspaceID]
	return ws, exists
}

// GetWorkspaceByPath retrieves workspace info by working directory path
func (wm *WorkspaceManager) GetWorkspaceByPath(path string) (*WorkspaceInternalInfo, bool) {
	// Normalize the path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, false
	}

	wm.mu.RLock()
	defer wm.mu.RUnlock()

	workspaceID, exists := wm.pathToID[absPath]
	if !exists {
		return nil, false
	}

	ws, exists := wm.workspaces[workspaceID]
	return ws, exists
}

// ListWorkspaces returns all registered workspaces
func (wm *WorkspaceManager) ListWorkspaces() []*WorkspaceInternalInfo {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	// Create a copy of the workspaces list
	workspaces := make([]*WorkspaceInternalInfo, 0, len(wm.workspaces))
	for _, ws := range wm.workspaces {
		wsCopy := *ws
		workspaces = append(workspaces, &wsCopy)
	}

	return workspaces
}

// CreateWorktree creates a new git worktree for a session
// Returns the path to the created worktree
func (wm *WorkspaceManager) CreateWorktree(ctx context.Context, baseWorkingDir, sessionName string) (*WorkspaceInternalInfo, error) {
	// Create git VCS instance
	git := vcs.NewGit(baseWorkingDir)

	// Create the worktree
	worktreePath, err := git.CreateWorktree(ctx, sessionName)
	if err != nil {
		return nil, fmt.Errorf("failed to create git worktree: %w", err)
	}

	// Resolve the worktree as a workspace
	ws, err := wm.ResolveWorkspace(ctx, worktreePath)
	if err != nil {
		// If workspace registration fails, try to clean up the worktree
		_ = os.RemoveAll(worktreePath)
		return nil, fmt.Errorf("failed to register worktree as workspace: %w", err)
	}

	// Mark as worktree
	ws.IsWorktree = true
	ws.WorktreeName = sessionName

	logger.Info("Created git worktree: %s at %s", sessionName, worktreePath)

	return ws, nil
}

// UpdateWorkspaceSessionCount updates the session count for a workspace
func (wm *WorkspaceManager) UpdateWorkspaceSessionCount(workspaceID string, delta int) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if ws, exists := wm.workspaces[workspaceID]; exists {
		ws.SessionCount += delta
		if ws.SessionCount < 0 {
			ws.SessionCount = 0
		}
	}
}

// UpdateWorkspaceAccess updates the last accessed time for a workspace
func (wm *WorkspaceManager) UpdateWorkspaceAccess(workspaceID string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if ws, exists := wm.workspaces[workspaceID]; exists {
		ws.LastAccessed = time.Now()
	}
}

// CleanupWorkspace cleans up a workspace (e.g., removes worktrees)
func (wm *WorkspaceManager) CleanupWorkspace(workspaceID string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	ws, exists := wm.workspaces[workspaceID]
	if !exists {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Only cleanup if this is a worktree and has no active sessions
	if ws.IsWorktree && ws.SessionCount == 0 {
		logger.Info("Cleaning up worktree: %s", ws.Path)
		if err := os.RemoveAll(ws.Path); err != nil {
			logger.Warn("Failed to cleanup worktree %s: %v", ws.Path, err)
			return fmt.Errorf("failed to cleanup worktree: %w", err)
		}

		// Remove from registry
		delete(wm.workspaces, workspaceID)
		delete(wm.pathToID, ws.Path)
	}

	return nil
}

// SetWorkspaceContextDirs sets the context directories for a workspace
func (wm *WorkspaceManager) SetWorkspaceContextDirs(workspaceID string, contextDirs []string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	ws, exists := wm.workspaces[workspaceID]
	if !exists {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	ws.ContextDirs = contextDirs
	return nil
}

// SetWorkspaceLandlockPermissions sets the landlock permissions for a workspace
func (wm *WorkspaceManager) SetWorkspaceLandlockPermissions(workspaceID string, readPaths, writePaths []string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	ws, exists := wm.workspaces[workspaceID]
	if !exists {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	ws.LandlockRead = readPaths
	ws.LandlockWrite = writePaths
	return nil
}

// ApproveDomainForWorkspace approves a network domain for a workspace
func (wm *WorkspaceManager) ApproveDomainForWorkspace(workspaceID, domain string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	ws, exists := wm.workspaces[workspaceID]
	if !exists {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	if ws.DomainsApproved == nil {
		ws.DomainsApproved = make(map[string]bool)
	}
	ws.DomainsApproved[domain] = true
	return nil
}

// ApproveCommandForWorkspace approves a command prefix for a workspace
func (wm *WorkspaceManager) ApproveCommandForWorkspace(workspaceID, command string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	ws, exists := wm.workspaces[workspaceID]
	if !exists {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	if ws.CommandsApproved == nil {
		ws.CommandsApproved = make(map[string]bool)
	}
	ws.CommandsApproved[command] = true
	return nil
}

// createWorkspaceInfo creates workspace info from a working directory
func (wm *WorkspaceManager) createWorkspaceInfo(ctx context.Context, workingDir, workspaceID string) (*WorkspaceInternalInfo, error) {
	now := time.Now()

	// Initialize workspace info
	ws := &WorkspaceInternalInfo{
		ID:               workspaceID,
		Path:             workingDir,
		Name:             filepath.Base(workingDir),
		SessionCount:     0,
		LastAccessed:     now,
		CreatedAt:        now,
		DomainsApproved:  make(map[string]bool),
		CommandsApproved: make(map[string]bool),
	}

	// Try to detect git repository
	git := vcs.NewGit(workingDir)
	if repoRoot, err := git.RepositoryRoot(ctx, workingDir); err == nil {
		ws.RepositoryRoot = repoRoot
		ws.Name = filepath.Base(repoRoot)

		// Get current branch
		if branch, err := git.CurrentBranch(ctx); err == nil {
			ws.CurrentBranch = branch
		}

		// Check if this is a worktree (working dir != repo root)
		if workingDir != repoRoot {
			ws.IsWorktree = true
			ws.WorktreeName = filepath.Base(workingDir)
		}
	}

	return ws, nil
}

// generateWorkspaceID generates a unique workspace ID from a working directory
func generateWorkspaceID(workingDir string) string {
	h := sha256.New()
	h.Write([]byte(workingDir))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// GetWorkspaceCount returns the number of registered workspaces
func (wm *WorkspaceManager) GetWorkspaceCount() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	return len(wm.workspaces)
}

// Shutdown gracefully shuts down the workspace manager
func (wm *WorkspaceManager) Shutdown() {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Cleanup worktrees (only if they have no active sessions)
	for _, ws := range wm.workspaces {
		if ws.IsWorktree && ws.SessionCount == 0 {
			logger.Info("Cleaning up worktree on shutdown: %s", ws.Path)
			_ = os.RemoveAll(ws.Path)
		}
	}

	// Clear registries
	wm.workspaces = make(map[string]*WorkspaceInternalInfo)
	wm.pathToID = make(map[string]string)

	logger.Info("Workspace manager shut down")
}
