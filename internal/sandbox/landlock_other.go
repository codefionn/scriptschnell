//go:build !linux

package sandbox

import (
	"os/exec"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// LandlockSandbox is a no-op implementation for non-Linux systems.
type LandlockSandbox struct {
	workspaceDir    string
	allowedPaths    []DirectoryPermission
	additionalPaths []DirectoryPermission
	customROPaths   []string
	customRWPaths   []string
}

// NewLandlockSandbox creates a new sandbox (no-op on non-Linux).
func NewLandlockSandbox(workspaceDir string, cfg *SandboxConfig) *LandlockSandbox {
	logger.Debug("Landlock sandboxing not available on this platform (non-Linux)")
	return &LandlockSandbox{
		workspaceDir:  workspaceDir,
		allowedPaths:  []DirectoryPermission{},
		customROPaths: []string{},
		customRWPaths: []string{},
	}
}

// AddAuthorizedPath adds a directory path (no-op on non-Linux).
func (s *LandlockSandbox) AddAuthorizedPath(path string, access AccessLevel) {
	s.additionalPaths = append(s.additionalPaths, DirectoryPermission{
		Path:   path,
		Access: access,
	})
}

// SetAdditionalPaths sets the additional authorized paths.
func (s *LandlockSandbox) SetAdditionalPaths(paths []DirectoryPermission) {
	s.additionalPaths = paths
}

// IsEnabled always returns false on non-Linux.
func (s *LandlockSandbox) IsEnabled() bool {
	return false
}

// Enable is a no-op on non-Linux.
func (s *LandlockSandbox) Enable() {}

// Disable is a no-op on non-Linux.
func (s *LandlockSandbox) Disable() {}

// WrapCommand is a no-op on non-Linux.
func (s *LandlockSandbox) WrapCommand(cmd *exec.Cmd) error {
	return nil
}

// Restrict is a no-op on non-Linux.
func (s *LandlockSandbox) Restrict() error {
	return nil
}

// GetAllowedPaths returns the allowed paths.
func (s *LandlockSandbox) GetAllowedPaths() []DirectoryPermission {
	result := make([]DirectoryPermission, 0, len(s.allowedPaths)+len(s.additionalPaths))
	result = append(result, s.allowedPaths...)
	result = append(result, s.additionalPaths...)
	return result
}

// GetWorkspaceDir returns the workspace directory.
func (s *LandlockSandbox) GetWorkspaceDir() string {
	return s.workspaceDir
}
