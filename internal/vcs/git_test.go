package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a temporary git repository for testing.
// Returns the repo path and a cleanup function.
func setupTestRepo(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "git-test-repo-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize git repository
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

// runGitCmd runs a git command in the specified directory.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Git command %v failed: %v\nOutput: %s", args, err, string(output))
	}
}

func TestGit_RepositoryRoot(t *testing.T) {
	ctx := context.Background()

	t.Run("returns repository root when inside repo", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		git := NewGit(repoDir)
		root, err := git.RepositoryRoot(ctx, repoDir)
		if err != nil {
			t.Fatalf("RepositoryRoot failed: %v", err)
		}

		// The returned root should match the actual repo directory
		if root != repoDir {
			t.Errorf("Expected root %q, got %q", repoDir, root)
		}
	})

	t.Run("returns repository root from subdirectory", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		// Create a subdirectory
		subDir := filepath.Join(repoDir, "subdir", "nested")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("Failed to create subdirectory: %v", err)
		}

		git := NewGit(subDir)
		root, err := git.RepositoryRoot(ctx, subDir)
		if err != nil {
			t.Fatalf("RepositoryRoot failed: %v", err)
		}

		// Should return repo root, not the subdirectory
		if root != repoDir {
			t.Errorf("Expected root %q, got %q", repoDir, root)
		}
	})

	t.Run("returns error when outside repo", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "git-test-no-repo-")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		git := NewGit(tmpDir)
		_, err = git.RepositoryRoot(ctx, tmpDir)
		if err == nil {
			t.Error("Expected error when outside repo, got nil")
		}
	})
}

func TestGit_IsIgnored(t *testing.T) {
	ctx := context.Background()

	t.Run("returns false for non-ignored file", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		// Create a regular file
		filePath := filepath.Join(repoDir, "test.txt")
		if err := os.WriteFile(filePath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		git := NewGit(repoDir)
		ignored, err := git.IsIgnored(ctx, filePath)
		if err != nil {
			t.Fatalf("IsIgnored failed: %v", err)
		}

		if ignored {
			t.Error("Expected file to not be ignored")
		}
	})

	t.Run("returns true for gitignored file", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		// Create .gitignore file
		gitignorePath := filepath.Join(repoDir, ".gitignore")
		if err := os.WriteFile(gitignorePath, []byte("*.log\n"), 0644); err != nil {
			t.Fatalf("Failed to create .gitignore: %v", err)
		}

		// Create a .log file
		logFile := filepath.Join(repoDir, "debug.log")
		if err := os.WriteFile(logFile, []byte("debug content"), 0644); err != nil {
			t.Fatalf("Failed to create log file: %v", err)
		}

		git := NewGit(repoDir)
		ignored, err := git.IsIgnored(ctx, logFile)
		if err != nil {
			t.Fatalf("IsIgnored failed: %v", err)
		}

		if !ignored {
			t.Error("Expected log file to be ignored")
		}
	})

	t.Run("verifies cache is populated", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		// Create .gitignore file
		gitignorePath := filepath.Join(repoDir, ".gitignore")
		if err := os.WriteFile(gitignorePath, []byte("*.cache\n"), 0644); err != nil {
			t.Fatalf("Failed to create .gitignore: %v", err)
		}
		runGitCmd(t, repoDir, "add", ".gitignore")
		runGitCmd(t, repoDir, "commit", "-m", "Add .gitignore")

		// Create a .cache file
		cacheFile := filepath.Join(repoDir, "test.cache")
		if err := os.WriteFile(cacheFile, []byte("cache content"), 0644); err != nil {
			t.Fatalf("Failed to create cache file: %v", err)
		}

		git := NewGit(repoDir)

		// Call IsIgnored to populate cache
		ignored, err := git.IsIgnored(ctx, cacheFile)
		if err != nil {
			t.Fatalf("IsIgnored failed: %v", err)
		}

		// Verify cache is not empty (it should contain at least one entry)
		git.ignoreMutex.RLock()
		cacheSize := len(git.ignoreCache)
		git.ignoreMutex.RUnlock()

		if cacheSize == 0 {
			t.Error("Expected cache to be populated, but it's empty")
		}

		// The file should be ignored
		if !ignored {
			t.Error("Expected cache file to be ignored")
		}
	})

	t.Run("returns false for file outside repo", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		// Create a file outside the repo
		tmpDir, err := os.MkdirTemp("", "git-test-outside-")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		outsideFile := filepath.Join(tmpDir, "outside.txt")
		if err := os.WriteFile(outsideFile, []byte("outside content"), 0644); err != nil {
			t.Fatalf("Failed to create outside file: %v", err)
		}

		git := NewGit(repoDir)
		ignored, err := git.IsIgnored(ctx, outsideFile)
		if err != nil {
			t.Fatalf("IsIgnored failed: %v", err)
		}

		if ignored {
			t.Error("Expected file outside repo to not be ignored")
		}
	})

	t.Run("handles patterns in subdirectories", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		// Create a subdirectory with .gitignore
		subDir := filepath.Join(repoDir, "config")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("Failed to create subdirectory: %v", err)
		}

		subGitignore := filepath.Join(subDir, ".gitignore")
		if err := os.WriteFile(subGitignore, []byte("*.secret\n"), 0644); err != nil {
			t.Fatalf("Failed to create subdirectory .gitignore: %v", err)
		}

		// Create a secret file in the subdirectory
		secretFile := filepath.Join(subDir, "password.secret")
		if err := os.WriteFile(secretFile, []byte("secret"), 0644); err != nil {
			t.Fatalf("Failed to create secret file: %v", err)
		}

		git := NewGit(repoDir)
		ignored, err := git.IsIgnored(ctx, secretFile)
		if err != nil {
			t.Fatalf("IsIgnored failed: %v", err)
		}

		if !ignored {
			t.Error("Expected secret file to be ignored by subdirectory .gitignore")
		}
	})
}

func TestGit_CreateWorktree(t *testing.T) {
	ctx := context.Background()

	t.Run("creates worktree successfully", func(t *testing.T) {
		// Create parent directory for the repo and worktree
		parentDir, err := os.MkdirTemp("", "git-worktree-test-")
		if err != nil {
			t.Fatalf("Failed to create parent dir: %v", err)
		}
		defer os.RemoveAll(parentDir)

		repoDir := filepath.Join(parentDir, "main-repo")
		if err := os.Mkdir(repoDir, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		// Initialize git repository
		runGitCmd(t, repoDir, "init")
		runGitCmd(t, repoDir, "config", "user.name", "Test User")
		runGitCmd(t, repoDir, "config", "user.email", "test@example.com")

		// Create initial commit (required for worktree)
		initialFile := filepath.Join(repoDir, "README.md")
		if err := os.WriteFile(initialFile, []byte("# Test Repo\n"), 0644); err != nil {
			t.Fatalf("Failed to create initial file: %v", err)
		}
		runGitCmd(t, repoDir, "add", ".")
		runGitCmd(t, repoDir, "commit", "-m", "Initial commit")

		git := NewGit(repoDir)
		sessionName := "test-session"

		worktreePath, err := git.CreateWorktree(ctx, sessionName)
		if err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}

		// Verify worktree was created
		expectedWorktreePath := filepath.Join(parentDir, "main-repo-test-session")
		if worktreePath != expectedWorktreePath {
			t.Errorf("Expected worktree path %q, got %q", expectedWorktreePath, worktreePath)
		}

		// Verify worktree directory exists
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Errorf("Worktree directory does not exist: %s", worktreePath)
		}

		// Verify worktree is a valid git worktree
		cmd := exec.Command("git", "-C", repoDir, "worktree", "list")
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git worktree list failed: %v", err)
		}

		if !strings.Contains(string(output), worktreePath) {
			t.Errorf("Worktree not found in git worktree list. Output: %s", string(output))
		}
	})

	t.Run("returns error when not in a repo", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "git-test-no-repo-")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		git := NewGit(tmpDir)
		_, err = git.CreateWorktree(ctx, "test-session")
		if err == nil {
			t.Error("Expected error when not in a repo, got nil")
		}
	})
}

func TestGit_Caching(t *testing.T) {
	ctx := context.Background()

	t.Run("RepositoryRoot is cached", func(t *testing.T) {
		repoDir, cleanup := setupTestRepo(t)
		defer cleanup()

		git := NewGit(repoDir)

		// First call
		root1, err1 := git.RepositoryRoot(ctx, repoDir)
		if err1 != nil {
			t.Fatalf("First RepositoryRoot failed: %v", err1)
		}

		// Second call - should use cache
		root2, err2 := git.RepositoryRoot(ctx, repoDir)
		if err2 != nil {
			t.Fatalf("Second RepositoryRoot failed: %v", err2)
		}

		if root1 != root2 {
			t.Error("Cached repository root should match first result")
		}
	})
}

func TestNewGit(t *testing.T) {
	t.Run("creates Git instance with working dir", func(t *testing.T) {
		workingDir := "/some/path"
		git := NewGit(workingDir)

		if git == nil {
			t.Fatal("NewGit returned nil")
		}

		if git.workingDir != workingDir {
			t.Errorf("Expected workingDir %q, got %q", workingDir, git.workingDir)
		}

		if git.ignoreCache == nil {
			t.Error("ignoreCache should be initialized")
		}
	})
}
