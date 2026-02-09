//go:build !linux

package sandbox

import (
	"testing"
)

func TestNewLandlockSandbox_NonLinux(t *testing.T) {
	t.Run("creates no-op sandbox", func(t *testing.T) {
		sb := NewLandlockSandbox("/workspace", nil)
		if sb == nil {
			t.Fatal("expected non-nil sandbox")
		}
	})

	t.Run("sandbox is not enabled on non-Linux", func(t *testing.T) {
		sb := NewLandlockSandbox("/workspace", nil)
		if sb.IsEnabled() {
			t.Error("expected sandbox to not be enabled on non-Linux")
		}
	})

	t.Run("Restrict returns nil on non-Linux", func(t *testing.T) {
		sb := NewLandlockSandbox("/workspace", nil)
		err := sb.Restrict()
		if err != nil {
			t.Errorf("expected nil error on non-Linux, got %v", err)
		}
	})

	t.Run("AddAuthorizedPath does not panic on non-Linux", func(t *testing.T) {
		sb := NewLandlockSandbox("/workspace", nil)
		// Should not panic
		sb.AddAuthorizedPath("/test", AccessReadOnly)
		sb.AddAuthorizedPath("/test", AccessReadWrite)
	})

	t.Run("GetAllowedPaths returns empty on non-Linux", func(t *testing.T) {
		sb := NewLandlockSandbox("/workspace", nil)
		paths := sb.GetAllowedPaths()
		if len(paths) != 0 {
			t.Errorf("expected empty paths on non-Linux, got %d", len(paths))
		}
	})

	t.Run("Enable/Disable work without error on non-Linux", func(t *testing.T) {
		sb := NewLandlockSandbox("/workspace", nil)
		sb.Enable()
		sb.Disable()
		// Should not panic
	})
}

func TestManager_NonLinux(t *testing.T) {
	t.Run("manager works with no-op sandbox", func(t *testing.T) {
		mgr := NewManager(nil, "/workspace")
		if mgr == nil {
			t.Fatal("expected non-nil manager")
		}

		// Manager should still work, even if sandbox doesn't
		mgr.SetAuthorizationCallback(func(req RequestedDirectory) AuthorizationDecision {
			return DecisionApprovedSession
		})

		decision := mgr.RequestPathAccess("/test", AccessReadOnly, "test")
		if decision != DecisionApprovedSession {
			t.Errorf("expected DecisionApprovedSession, got %v", decision)
		}
	})
}
