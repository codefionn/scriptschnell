package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// getGitRepoRoot returns the root directory of the git repository
// Returns empty string and error if not in a git repo
func (m *Model) getGitRepoRoot(workingDir string) (string, error) {
	cmd := exec.Command("git", "-C", workingDir, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

// getGitRepoName extracts the repository name from the path
func getGitRepoName(repoRoot string) string {
	return filepath.Base(repoRoot)
}

// createWorktree creates a new git worktree with the specified session name
// Returns the worktree path and any error encountered
func (m *Model) createWorktree(sessionName string) (string, error) {
	// Get repository root
	repoRoot, err := m.getGitRepoRoot(m.workingDir)
	if err != nil {
		return "", err
	}

	repoName := getGitRepoName(repoRoot)
	worktreeName := fmt.Sprintf("%s-%s", repoName, sessionName)

	// Worktree path: parent directory of repo
	parentDir := filepath.Dir(repoRoot)
	worktreePath := filepath.Join(parentDir, worktreeName)

	// Check if worktree path already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path already exists: %s", worktreePath)
	}

	// Create branch name
	branchName := fmt.Sprintf("session/%s", sessionName)

	// Create worktree with new branch
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "add", worktreePath, "-b", branchName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create worktree: %s", string(output))
	}

	return worktreePath, nil
}

// handleWorktreeCreation attempts to create a worktree for the given session name
// Returns the worktree path if successful, empty string with warning if failed
func (m *Model) handleWorktreeCreation(sessionName string) (worktreePath string, err error) {
	// Check if in git repo
	_, err = m.getGitRepoRoot(m.workingDir)
	if err != nil {
		// Not a git repo - session will use current directory
		m.AddSystemMessage(fmt.Sprintf(
			"Not in a git repository. Session '%s' will use current directory.",
			sessionName))
		return "", nil
	}

	// Try to create worktree
	worktreePath, err = m.createWorktree(sessionName)
	if err != nil {
		// Worktree creation failed - fall back to current directory
		m.AddSystemMessage(fmt.Sprintf(
			"Failed to create worktree: %v. Using current directory.", err))
		return "", nil
	}

	m.AddSystemMessage(fmt.Sprintf("Created git worktree: %s", worktreePath))
	return worktreePath, nil
}
