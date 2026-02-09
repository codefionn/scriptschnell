//go:build linux

package sandbox

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
)

func TestNewManager(t *testing.T) {
	t.Run("creates manager with nil config", func(t *testing.T) {
		mgr := NewManager(nil, "/workspace")
		if mgr == nil {
			t.Fatal("expected non-nil manager")
		}
		if mgr.workspaceDir != "/workspace" {
			t.Errorf("expected workspaceDir /workspace, got %s", mgr.workspaceDir)
		}
	})

	t.Run("creates manager with config", func(t *testing.T) {
		cfg := &config.Config{
			Sandbox: config.SandboxConfig{
				AdditionalReadOnlyPaths:  []string{"/readonly"},
				AdditionalReadWritePaths: []string{"/readwrite"},
				BestEffort:               true,
			},
		}
		mgr := NewManager(cfg, "/workspace")
		if mgr == nil {
			t.Fatal("expected non-nil manager")
		}
	})
}

func TestManagerSetAuthorizationCallback(t *testing.T) {
	mgr := NewManager(nil, "/workspace")

	t.Run("sets callback", func(t *testing.T) {
		called := false
		mgr.SetAuthorizationCallback(func(req RequestedDirectory) AuthorizationDecision {
			called = true
			return DecisionApprovedSession
		})

		if mgr.authCallback == nil {
			t.Error("expected authCallback to be set")
		}

		// Trigger the callback
		req := RequestedDirectory{Path: "/test", Access: AccessReadOnly}
		decision := mgr.authCallback(req)
		if !called {
			t.Error("expected callback to be called")
		}
		if decision != DecisionApprovedSession {
			t.Errorf("expected DecisionApprovedSession, got %v", decision)
		}
	})
}

func TestManagerRequestPathAccess(t *testing.T) {
	t.Run("denies by default when no callback set", func(t *testing.T) {
		mgr := NewManager(nil, "/workspace")
		decision := mgr.RequestPathAccess("/test", AccessReadOnly, "test")
		if decision != DecisionDenied {
			t.Errorf("expected DecisionDenied, got %v", decision)
		}
	})

	t.Run("approves via callback for session", func(t *testing.T) {
		mgr := NewManager(nil, "/workspace")
		mgr.SetAuthorizationCallback(func(req RequestedDirectory) AuthorizationDecision {
			if req.Path == "/test" {
				return DecisionApprovedSession
			}
			return DecisionDenied
		})

		decision := mgr.RequestPathAccess("/test", AccessReadOnly, "test")
		if decision != DecisionApprovedSession {
			t.Errorf("expected DecisionApprovedSession, got %v", decision)
		}

		// Verify path is in session paths
		sessionPaths := mgr.GetSessionPaths()
		found := false
		for _, p := range sessionPaths {
			if p.Path == "/test" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected /test to be in session paths")
		}
	})
}

func TestManagerGetSessionPaths(t *testing.T) {
	mgr := NewManager(nil, "/workspace")
	mgr.SetAuthorizationCallback(func(req RequestedDirectory) AuthorizationDecision {
		return DecisionApprovedSession
	})

	t.Run("returns copy of session paths", func(t *testing.T) {
		mgr.RequestPathAccess("/path1", AccessReadOnly, "test")
		mgr.RequestPathAccess("/path2", AccessReadWrite, "test")

		paths := mgr.GetSessionPaths()
		if len(paths) != 2 {
			t.Errorf("expected 2 session paths, got %d", len(paths))
		}

		// Modify returned slice should not affect original
		paths[0].Path = "/modified"
		originalPaths := mgr.GetSessionPaths()
		if originalPaths[0].Path == "/modified" {
			t.Error("expected GetSessionPaths to return a copy")
		}
	})
}

func TestManagerApplyDecision(t *testing.T) {
	t.Run("applies session approval", func(t *testing.T) {
		cfg := &config.Config{Sandbox: config.SandboxConfig{}}
		mgr := NewManager(cfg, "/workspace")

		mgr.ApplyDecision("/test", AccessReadOnly, DecisionApprovedSession)

		paths := mgr.GetSessionPaths()
		found := false
		for _, p := range paths {
			if p.Path == "/test" && p.Access == AccessReadOnly {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected /test to be in session paths after ApplyDecision")
		}
	})

	t.Run("applies workspace approval and persists to config", func(t *testing.T) {
		cfg := &config.Config{Sandbox: config.SandboxConfig{}}
		mgr := NewManager(cfg, "/workspace")

		mgr.ApplyDecision("/persisted", AccessReadWrite, DecisionApprovedWorkspace)

		// Verify it's persisted in config
		if !cfg.IsLandlockApproved("/workspace", "/persisted", "readwrite") {
			t.Error("expected /persisted to be in config approvals")
		}
	})

	t.Run("ignores denied decision", func(t *testing.T) {
		cfg := &config.Config{Sandbox: config.SandboxConfig{}}
		mgr := NewManager(cfg, "/workspace")

		mgr.ApplyDecision("/denied", AccessReadOnly, DecisionDenied)

		paths := mgr.GetSessionPaths()
		for _, p := range paths {
			if p.Path == "/denied" {
				t.Error("expected /denied not to be in session paths")
			}
		}
	})
}

func TestManagerLoadWorkspaceApprovals(t *testing.T) {
	t.Run("loads approvals from config", func(t *testing.T) {
		cfg := &config.Config{
			Sandbox: config.SandboxConfig{},
		}
		cfg.AddLandlockApproval("/workspace", "/approved/path", "read")
		cfg.AddLandlockApproval("/workspace", "/approved/rw", "readwrite")

		mgr := NewManager(cfg, "/workspace")
		mgr.LoadWorkspaceApprovals()

		// Verify paths are loaded into sandbox
		paths := mgr.GetAllowedPaths()
		foundRead := false
		foundRW := false
		for _, p := range paths {
			if p.Path == "/approved/path" && p.Access == AccessReadOnly {
				foundRead = true
			}
			if p.Path == "/approved/rw" && p.Access == AccessReadWrite {
				foundRW = true
			}
		}

		if !foundRead {
			t.Error("expected /approved/path to be loaded")
		}
		if !foundRW {
			t.Error("expected /approved/rw to be loaded")
		}
	})

	t.Run("handles nil config", func(t *testing.T) {
		mgr := NewManager(nil, "/workspace")
		// Should not panic
		mgr.LoadWorkspaceApprovals()
	})
}

func TestManagerIsEnabled(t *testing.T) {
	t.Run("reflects sandbox status", func(t *testing.T) {
		mgr := NewManager(nil, "/workspace")
		// Should match sandbox's enabled status
		if mgr.IsEnabled() != mgr.sandbox.IsEnabled() {
			t.Error("expected Manager.IsEnabled to match sandbox.IsEnabled")
		}
	})
}

func TestManagerEnableDisable(t *testing.T) {
	mgr := NewManager(nil, "/workspace")

	t.Run("Disable works", func(t *testing.T) {
		mgr.Disable()
		if mgr.IsEnabled() {
			t.Error("expected manager to be disabled")
		}
	})
}

func TestManagerRequestMultiplePaths(t *testing.T) {
	mgr := NewManager(nil, "/workspace")
	mgr.SetAuthorizationCallback(func(req RequestedDirectory) AuthorizationDecision {
		return DecisionApprovedSession
	})

	requests := []RequestedDirectory{
		{Path: "/path1", Access: AccessReadOnly, Description: "test"},
		{Path: "/path2", Access: AccessReadWrite, Description: "test"},
	}

	results := mgr.RequestMultiplePaths(requests)

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	if results["/path1"] != DecisionApprovedSession {
		t.Errorf("expected /path1 to be approved for session")
	}

	if results["/path2"] != DecisionApprovedSession {
		t.Errorf("expected /path2 to be approved for session")
	}
}
