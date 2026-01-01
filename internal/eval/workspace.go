package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WorkspaceManager handles temporary workspace creation and cleanup
type WorkspaceManager struct {
	baseDir string
}

// NewWorkspaceManager creates a workspace manager
func NewWorkspaceManager(baseDir string) (*WorkspaceManager, error) {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace base dir: %w", err)
	}

	return &WorkspaceManager{baseDir: baseDir}, nil
}

// CreateWorkspace creates a temporary workspace for an eval run
func (wm *WorkspaceManager) CreateWorkspace(runID int64) (string, error) {
	workspaceDir := filepath.Join(wm.baseDir, fmt.Sprintf("run-%d", runID))

	// Clean existing workspace if it exists
	if err := os.RemoveAll(workspaceDir); err != nil {
		return "", fmt.Errorf("failed to clean existing workspace: %w", err)
	}

	// Create fresh workspace
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create workspace: %w", err)
	}

	// Create .logs subdirectory for scriptschnell logs
	logsDir := filepath.Join(workspaceDir, ".logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create logs dir: %w", err)
	}

	return workspaceDir, nil
}

// CleanupWorkspace removes a workspace after eval completes
func (wm *WorkspaceManager) CleanupWorkspace(workspaceDir string) error {
	// Don't cleanup on error - keep for debugging
	// Could add a retention policy later
	return nil
}

// WaitForSignal waits for .build_done or .build_failed signal file
func (wm *WorkspaceManager) WaitForSignal(workspaceDir string, timeout time.Duration) error {
	donePath := filepath.Join(workspaceDir, ".build_done")
	failedPath := filepath.Join(workspaceDir, ".build_failed")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	deadline := time.Now().Add(timeout)

	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(donePath); err == nil {
				return nil // Success
			}
			if _, err := os.Stat(failedPath); err == nil {
				return fmt.Errorf("build failed (signal file detected)")
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for build signal")
			}
		}
	}
}
