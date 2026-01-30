package vcs

import (
	"context"
)

// MockVCS is a mock implementation of the VCS interface for testing.
type MockVCS struct {
	// RepositoryRootFunc is the mock implementation for RepositoryRoot
	RepositoryRootFunc func(ctx context.Context, dir string) (string, error)

	// IsIgnoredFunc is the mock implementation for IsIgnored
	IsIgnoredFunc func(ctx context.Context, absPath string) (bool, error)

	// CreateWorktreeFunc is the mock implementation for CreateWorktree
	CreateWorktreeFunc func(ctx context.Context, sessionName string) (string, error)
}

// RepositoryRoot calls the mock RepositoryRootFunc if set, otherwise returns empty string.
func (m *MockVCS) RepositoryRoot(ctx context.Context, dir string) (string, error) {
	if m.RepositoryRootFunc != nil {
		return m.RepositoryRootFunc(ctx, dir)
	}
	return "", nil
}

// IsIgnored calls the mock IsIgnoredFunc if set, otherwise returns false.
func (m *MockVCS) IsIgnored(ctx context.Context, absPath string) (bool, error) {
	if m.IsIgnoredFunc != nil {
		return m.IsIgnoredFunc(ctx, absPath)
	}
	return false, nil
}

// CreateWorktree calls the mock CreateWorktreeFunc if set, otherwise returns empty string.
func (m *MockVCS) CreateWorktree(ctx context.Context, sessionName string) (string, error) {
	if m.CreateWorktreeFunc != nil {
		return m.CreateWorktreeFunc(ctx, sessionName)
	}
	return "", nil
}
