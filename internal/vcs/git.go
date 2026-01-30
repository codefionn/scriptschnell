package vcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Git implements the VCS interface for Git repositories.
type Git struct {
	workingDir string
	// repoRootOnce ensures we only look up the repo root once
	repoRootOnce sync.Once
	repoRoot     string // cached repo root, accessed via getRepoRoot
	repoRootErr  error  // error from repo root lookup

	// ignoreCache caches git ignore results
	ignoreCache map[string]bool
	ignoreMutex sync.RWMutex
}

// NewGit creates a new Git VCS instance for the given working directory.
// The working directory should be within a Git repository.
func NewGit(workingDir string) *Git {
	return &Git{
		workingDir:  workingDir,
		ignoreCache: make(map[string]bool),
	}
}

// getRepoRoot returns the cached repository root, looking it up if necessary.
func (g *Git) getRepoRoot(ctx context.Context) (string, error) {
	g.repoRootOnce.Do(func() {
		g.repoRoot, g.repoRootErr = g.RepositoryRoot(ctx, g.workingDir)
	})
	return g.repoRoot, g.repoRootErr
}

// RepositoryRoot returns the root directory of the Git repository
// containing the current working directory.
func (g *Git) RepositoryRoot(ctx context.Context, dir string) (string, error) {
	if dir == "" {
		dir = g.workingDir
	}

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// IsIgnored checks if a file/directory path is ignored by Git.
// The path should be absolute. Returns false if not in a repository.
func (g *Git) IsIgnored(ctx context.Context, absPath string) (bool, error) {
	// Get repository root (cached)
	repoRoot, err := g.getRepoRoot(ctx)
	if err != nil {
		return false, nil // Not in a repo, so not ignored
	}

	// Convert absolute path to path relative to repo root
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return false, nil // Path outside repo, not ignored
	}

	// Check cache
	g.ignoreMutex.RLock()
	ignored, ok := g.ignoreCache[relPath]
	g.ignoreMutex.RUnlock()
	if ok {
		return ignored, nil
	}

	// Run git check-ignore
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "check-ignore", "--quiet", "--", relPath)
	err = cmd.Run()
	ignored = err == nil

	// Update cache
	g.ignoreMutex.Lock()
	g.ignoreCache[relPath] = ignored
	g.ignoreMutex.Unlock()

	return ignored, nil
}

// CreateWorktree creates a new git worktree with the specified session name.
// Returns the path to the created worktree.
// The worktree is created in the parent directory of the repository root,
// with the naming pattern: {repoName}-{sessionName}.
func (g *Git) CreateWorktree(ctx context.Context, sessionName string) (string, error) {
	// Get repository root
	repoRoot, err := g.getRepoRoot(ctx)
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}

	repoName := getRepoName(repoRoot)
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
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "add", worktreePath, "-b", branchName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create worktree: %s", string(output))
	}

	return worktreePath, nil
}

// getRepoName extracts the repository name from the repository root path.
func getRepoName(repoRoot string) string {
	return filepath.Base(repoRoot)
}

// CurrentBranch returns the name of the current branch.
// Returns an empty string if not in a repository or on a detached HEAD.
func (g *Git) CurrentBranch(ctx context.Context) (string, error) {
	// Get repository root first to check if we're in a repo
	repoRoot, err := g.getRepoRoot(ctx)
	if err != nil {
		return "", nil // Not in a repo, return empty string
	}

	// Get the current branch name
	// git rev-parse --abbrev-ref HEAD returns the branch name, or HEAD if detached
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", nil
	}

	branch := strings.TrimSpace(string(output))

	// If we're in detached HEAD state, git will return "HEAD"
	// In that case, we return empty string to indicate no branch
	if branch == "HEAD" {
		return "", nil
	}

	return branch, nil
}
