// Package sandbox provides filesystem sandboxing capabilities using Linux Landlock.
package sandbox

import (
	"sync"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// AuthorizationCallback is a function that requests user authorization
// for a directory access request. It returns the user's decision.
// This callback should be non-blocking - it should return immediately
// with DecisionDenied if the request is queued for user input.
type AuthorizationCallback func(request RequestedDirectory) AuthorizationDecision

// AsyncAuthorizationCallback is a function that handles authorization
// asynchronously. It's called in a goroutine and should send the
// decision to the provided channel.
type AsyncAuthorizationCallback func(request RequestedDirectory, responseChan chan<- AuthorizationDecision)

// Manager manages the Landlock sandbox for the application.
type Manager struct {
	mu                sync.RWMutex
	sandbox           *LandlockSandbox
	config            *config.Config
	workspaceDir      string
	authCallback      AuthorizationCallback
	asyncAuthCallback AsyncAuthorizationCallback
	sessionPaths      []DirectoryPermission // Paths approved for this session only
}

// NewManager creates a new sandbox manager.
func NewManager(cfg *config.Config, workspaceDir string) *Manager {
	// Convert config.SandboxConfig to sandbox.SandboxConfig
	var sandboxCfg *SandboxConfig
	if cfg != nil {
		sandboxCfg = &SandboxConfig{
			AdditionalReadOnlyPaths:  cfg.Sandbox.AdditionalReadOnlyPaths,
			AdditionalReadWritePaths: cfg.Sandbox.AdditionalReadWritePaths,
			DisableSandbox:           cfg.Sandbox.DisableSandbox,
			BestEffort:               cfg.Sandbox.BestEffort,
		}
	}

	return &Manager{
		sandbox:      NewLandlockSandbox(workspaceDir, sandboxCfg),
		config:       cfg,
		workspaceDir: workspaceDir,
		sessionPaths: make([]DirectoryPermission, 0),
	}
}

// SetAuthorizationCallback sets the callback function for requesting authorization.
func (m *Manager) SetAuthorizationCallback(cb AuthorizationCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authCallback = cb
}

// SetAsyncAuthorizationCallback sets the async callback function for requesting authorization.
// The async callback is preferred if both are set.
func (m *Manager) SetAsyncAuthorizationCallback(cb AsyncAuthorizationCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asyncAuthCallback = cb
}

// RequestPathAccess requests access to an additional directory path.
// It checks workspace-approved paths first, then prompts for authorization if needed.
func (m *Manager) RequestPathAccess(path string, access AccessLevel, description string) AuthorizationDecision {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Convert access level to string for config lookup
	accessStr := "read"
	if access == AccessReadWrite {
		accessStr = "readwrite"
	}

	// 1. Check if already approved in workspace config
	if m.config != nil && m.config.IsLandlockApproved(m.workspaceDir, path, accessStr) {
		logger.Debug("Path %s already approved in workspace config (access: %s)", path, accessStr)
		m.sandbox.AddAuthorizedPath(path, access)
		return DecisionApprovedWorkspace
	}

	// 2. Check if already approved for this session
	for _, p := range m.sessionPaths {
		if p.Path == path {
			// If we need readwrite but only have read, need to re-authorize
			if access == AccessReadWrite && p.Access == AccessReadOnly {
				break // Need to re-authorize
			}
			logger.Debug("Path %s already approved for this session (access: %v)", path, p.Access)
			return DecisionApprovedSession
		}
	}

	// 3. Request authorization from user
	if m.authCallback != nil {
		logger.Info("Requesting directory access: path=%s, access=%s, reason=%s", path, accessStr, description)
		request := RequestedDirectory{
			Path:        path,
			Access:      access,
			Description: description,
		}
		decision := m.authCallback(request)

		switch decision {
		case DecisionApprovedWorkspace:
			logger.Info("Directory access approved for workspace: %s (%s)", path, accessStr)
			// Persist to workspace config
			if m.config != nil {
				m.config.AddLandlockApproval(m.workspaceDir, path, accessStr)
			}
			m.sandbox.AddAuthorizedPath(path, access)
			m.sessionPaths = append(m.sessionPaths, DirectoryPermission{Path: path, Access: access})
		case DecisionApprovedSession:
			logger.Info("Directory access approved for session: %s (%s)", path, accessStr)
			m.sandbox.AddAuthorizedPath(path, access)
			m.sessionPaths = append(m.sessionPaths, DirectoryPermission{Path: path, Access: access})
		case DecisionDenied:
			logger.Info("Directory access denied: %s (%s)", path, accessStr)
		}

		return decision
	}

	// No callback set, deny by default
	logger.Warn("No authorization callback set, denying path access: %s", path)
	return DecisionDenied
}

// RequestMultiplePaths requests access to multiple directory paths.
// Returns a map of path -> decision for each requested path.
func (m *Manager) RequestMultiplePaths(requests []RequestedDirectory) map[string]AuthorizationDecision {
	results := make(map[string]AuthorizationDecision)
	for _, req := range requests {
		results[req.Path] = m.RequestPathAccess(req.Path, req.Access, req.Description)
	}
	return results
}

// LoadWorkspaceApprovals loads previously approved paths from the workspace config.
func (m *Manager) LoadWorkspaceApprovals() {
	if m.config == nil {
		return
	}

	approvals := m.config.GetLandlockApprovals(m.workspaceDir)
	if len(approvals) > 0 {
		logger.Debug("Loading %d previously approved paths for workspace", len(approvals))
	}
	for _, approval := range approvals {
		access := AccessReadOnly
		if approval.AccessLevel == "readwrite" {
			access = AccessReadWrite
		}
		m.sandbox.AddAuthorizedPath(approval.Path, access)
		logger.Debug("Loaded workspace approval: %s (%s)", approval.Path, approval.AccessLevel)
	}
}

// GetSandbox returns the underlying sandbox instance.
func (m *Manager) GetSandbox() *LandlockSandbox {
	return m.sandbox
}

// IsEnabled returns whether sandboxing is enabled.
func (m *Manager) IsEnabled() bool {
	return m.sandbox.IsEnabled()
}

// Enable enables the sandbox.
func (m *Manager) Enable() {
	m.sandbox.Enable()
}

// Disable disables the sandbox.
func (m *Manager) Disable() {
	m.sandbox.Disable()
}

// GetAllowedPaths returns all currently allowed paths.
func (m *Manager) GetAllowedPaths() []DirectoryPermission {
	return m.sandbox.GetAllowedPaths()
}

// GetSessionPaths returns paths approved for this session only.
func (m *Manager) GetSessionPaths() []DirectoryPermission {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]DirectoryPermission, len(m.sessionPaths))
	copy(result, m.sessionPaths)
	return result
}

// ApplyDecision applies an authorization decision from the TUI.
// This is called when the user responds to the directory access dialog.
func (m *Manager) ApplyDecision(path string, access AccessLevel, decision AuthorizationDecision) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Convert access level to string for config lookup
	accessStr := "read"
	if access == AccessReadWrite {
		accessStr = "readwrite"
	}

	switch decision {
	case DecisionApprovedWorkspace:
		// Persist to workspace config
		if m.config != nil {
			m.config.AddLandlockApproval(m.workspaceDir, path, accessStr)
		}
		m.sandbox.AddAuthorizedPath(path, access)
		m.sessionPaths = append(m.sessionPaths, DirectoryPermission{Path: path, Access: access})
	case DecisionApprovedSession:
		m.sandbox.AddAuthorizedPath(path, access)
		m.sessionPaths = append(m.sessionPaths, DirectoryPermission{Path: path, Access: access})
	case DecisionDenied:
		// Do nothing
	}
}
