// Package vcs provides a version control system abstraction layer.
// It defines interfaces for common VCS operations, allowing for pluggable implementations.
package vcs

import (
	"context"
)

// VCS represents a version control system.
// It provides methods for common VCS operations like finding repository roots,
// checking ignored files, creating worktrees, and getting current branch.
type VCS interface {
	// RepositoryRoot returns the root directory of the VCS repository
	// containing the given directory. Returns an error if not in a repository.
	RepositoryRoot(ctx context.Context, dir string) (string, error)

	// IsIgnored checks if a file/directory path is ignored by the VCS.
	// The path should be absolute. Returns false if not in a repository.
	IsIgnored(ctx context.Context, absPath string) (bool, error)

	// CreateWorktree creates a new worktree with the specified session name.
	// Returns the path to the created worktree.
	// Returns an error if worktree creation fails or not in a repository.
	CreateWorktree(ctx context.Context, sessionName string) (string, error)

	// CurrentBranch returns the name of the current branch.
	// Returns an empty string if not in a repository or on a detached HEAD.
	CurrentBranch(ctx context.Context) (string, error)
}
