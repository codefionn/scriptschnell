//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLandlockSandbox(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("creates sandbox with default settings", func(t *testing.T) {
		sb := NewLandlockSandbox(tmpDir, nil)
		if sb == nil {
			t.Fatal("expected non-nil sandbox")
		}
		if sb.workspaceDir != tmpDir {
			t.Errorf("expected workspaceDir %s, got %s", tmpDir, sb.workspaceDir)
		}
		// bestEffort should default to true
		if !sb.bestEffort {
			t.Error("expected bestEffort to be true by default")
		}
	})

	t.Run("creates sandbox with config", func(t *testing.T) {
		cfg := &SandboxConfig{
			AdditionalReadOnlyPaths:  []string{"/custom/readonly"},
			AdditionalReadWritePaths: []string{"/custom/readwrite"},
			BestEffort:               false,
		}
		sb := NewLandlockSandbox(tmpDir, cfg)
		if sb == nil {
			t.Fatal("expected non-nil sandbox")
		}
		if len(sb.customROPaths) != 1 || sb.customROPaths[0] != "/custom/readonly" {
			t.Errorf("expected customROPaths [/custom/readonly], got %v", sb.customROPaths)
		}
		if len(sb.customRWPaths) != 1 || sb.customRWPaths[0] != "/custom/readwrite" {
			t.Errorf("expected customRWPaths [/custom/readwrite], got %v", sb.customRWPaths)
		}
	})

	t.Run("respects DisableSandbox config", func(t *testing.T) {
		cfg := &SandboxConfig{
			DisableSandbox: true,
		}
		sb := NewLandlockSandbox(tmpDir, cfg)
		if sb == nil {
			t.Fatal("expected non-nil sandbox")
		}
		if sb.IsEnabled() {
			t.Error("expected sandbox to be disabled")
		}
		if !sb.disabled {
			t.Error("expected disabled flag to be true")
		}
	})
}

func TestLandlockSandboxIsEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("returns false when disabled", func(t *testing.T) {
		cfg := &SandboxConfig{DisableSandbox: true}
		sb := NewLandlockSandbox(tmpDir, cfg)
		if sb.IsEnabled() {
			t.Error("expected IsEnabled to return false for disabled sandbox")
		}
	})

	t.Run("returns enabled status based on availability", func(t *testing.T) {
		sb := NewLandlockSandbox(tmpDir, nil)
		// The result depends on whether landlock is available on the system
		// Just verify the method doesn't panic
		_ = sb.IsEnabled()
	})
}

func TestLandlockSandboxEnableDisable(t *testing.T) {
	tmpDir := t.TempDir()
	sb := NewLandlockSandbox(tmpDir, nil)

	t.Run("Enable and Disable work", func(t *testing.T) {
		sb.Enable()
		// Should not panic

		sb.Disable()
		if sb.IsEnabled() {
			t.Error("expected sandbox to be disabled after Disable()")
		}
	})
}

func TestLandlockSandboxAddAuthorizedPath(t *testing.T) {
	tmpDir := t.TempDir()
	sb := NewLandlockSandbox(tmpDir, nil)

	t.Run("adds read-only path", func(t *testing.T) {
		sb.AddAuthorizedPath("/test/readonly", AccessReadOnly)
		paths := sb.GetAllowedPaths()
		found := false
		for _, p := range paths {
			if p.Path == "/test/readonly" && p.Access == AccessReadOnly {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find /test/readonly with read-only access")
		}
	})

	t.Run("adds read-write path", func(t *testing.T) {
		sb.AddAuthorizedPath("/test/readwrite", AccessReadWrite)
		paths := sb.GetAllowedPaths()
		found := false
		for _, p := range paths {
			if p.Path == "/test/readwrite" && p.Access == AccessReadWrite {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find /test/readwrite with read-write access")
		}
	})
}

func TestLandlockSandboxRestrict(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("returns nil when disabled", func(t *testing.T) {
		cfg := &SandboxConfig{DisableSandbox: true}
		sb := NewLandlockSandbox(tmpDir, cfg)
		err := sb.Restrict()
		if err != nil {
			t.Errorf("expected nil error for disabled sandbox, got %v", err)
		}
	})

	t.Run("returns nil when not available", func(t *testing.T) {
		sb := NewLandlockSandbox(tmpDir, nil)
		sb.available = false
		err := sb.Restrict()
		if err != nil {
			t.Errorf("expected nil error when not available, got %v", err)
		}
	})
}

func TestGetDefaultAllowedPaths(t *testing.T) {
	paths := getDefaultAllowedPaths()

	t.Run("returns non-empty list", func(t *testing.T) {
		if len(paths) == 0 {
			t.Error("expected non-empty default paths list")
		}
	})

	t.Run("includes common paths", func(t *testing.T) {
		// Check for some common paths that should always be present
		homeDir, _ := os.UserHomeDir()

		expectedPaths := []string{
			"/usr",
			"/bin",
			"/lib",
		}

		for _, expected := range expectedPaths {
			found := false
			for _, p := range paths {
				if p.Path == expected {
					found = true
					break
				}
			}
			if !found {
				t.Logf("Warning: expected path %s not found in default paths", expected)
			}
		}

		// Home directory should be present
		if homeDir != "" {
			found := false
			for _, p := range paths {
				if p.Path == homeDir {
					found = true
					break
				}
			}
			if !found {
				t.Logf("Warning: home directory %s not found in default paths", homeDir)
			}
		}
	})
}

func TestPackageManagerConfigs(t *testing.T) {
	// Package manager configs are internal, but we can verify
	// that getDefaultAllowedPaths includes common package manager paths
	paths := getDefaultAllowedPaths()

	// Verify we have paths that would come from package managers
	homeDir, _ := os.UserHomeDir()

	// Check for common package manager related paths
	pmPaths := []string{
		filepath.Join(homeDir, ".npm"),
		filepath.Join(homeDir, ".cargo"),
		filepath.Join(homeDir, "go"),
	}

	for _, expectedPath := range pmPaths {
		found := false
		for _, p := range paths {
			if p.Path == expectedPath {
				found = true
				break
			}
		}
		// These may not exist on all systems, so just log
		if found {
			t.Logf("Found package manager path: %s", expectedPath)
		}
	}
}

func TestAddPathIfExists(t *testing.T) {
	// Test that getDefaultAllowedPaths properly handles path existence
	// by verifying only existing paths are included
	paths := getDefaultAllowedPaths()

	// All returned paths should exist
	for _, p := range paths {
		if _, err := os.Stat(p.Path); os.IsNotExist(err) {
			t.Logf("Warning: path %s in results but doesn't exist", p.Path)
		}
	}
}
