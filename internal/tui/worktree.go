package tui

import (
	"context"
	"fmt"
)

// handleWorktreeCreation attempts to create a worktree for the given session name
// Returns the worktree path if successful, empty string with warning if failed
func (m *Model) handleWorktreeCreation(sessionName string) (worktreePath string, err error) {
	// Check if VCS is available
	if m.vcs == nil {
		// Not a VCS repository - session will use current directory
		m.AddSystemMessage(fmt.Sprintf(
			"Not in a git repository. Session '%s' will use current directory.",
			sessionName))
		return "", nil
	}

	// Try to create worktree
	worktreePath, err = m.vcs.CreateWorktree(context.Background(), sessionName)
	if err != nil {
		// Worktree creation failed - fall back to current directory
		m.AddSystemMessage(fmt.Sprintf(
			"Failed to create worktree: %v. Using current directory.", err))
		return "", nil
	}

	m.AddSystemMessage(fmt.Sprintf("Created git worktree: %s", worktreePath))
	return worktreePath, nil
}
