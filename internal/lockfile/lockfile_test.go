package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockfile_AcquireRelease(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")
	lock := New(lockPath)

	// Acquire lock
	if err := lock.TryAcquire(); err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	if !lock.Locked() {
		t.Error("Lock should be locked")
	}

	if lock.PID() != os.Getpid() {
		t.Errorf("Expected PID %d, got %d", os.Getpid(), lock.PID())
	}

	// Release lock
	if err := lock.Release(); err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}

	if lock.Locked() {
		t.Error("Lock should not be locked after release")
	}

	// Should be able to acquire again
	if err := lock.TryAcquire(); err != nil {
		t.Fatalf("Failed to acquire lock after release: %v", err)
	}

	// Clean up
	lock.Release()
}

func TestLockfile_AlreadyLocked(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// First lock
	lock1 := New(lockPath)
	if err := lock1.TryAcquire(); err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}
	defer lock1.Release()

	// Second lock should fail
	lock2 := New(lockPath)
	if err := lock2.TryAcquire(); err == nil {
		t.Error("Expected error when acquiring already held lock")
		defer lock2.Release()
	} else if !errors.Is(err, ErrLocked) {
		t.Errorf("Expected ErrLocked, got: %v", err)
	}
}

func TestLockfile_Stale(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a fake lockfile with a non-existent PID
	pid := 99999
	timestamp := time.Now().Format(time.RFC3339)
	content := fmt.Sprintf("%d\n%s\n", pid, timestamp)
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create fake lockfile: %v", err)
	}

	// Should be able to acquire the lock (stale)
	lock := New(lockPath)
	if err := lock.TryAcquire(); err != nil {
		t.Fatalf("Failed to acquire stale lock: %v", err)
	}
	defer lock.Release()

	if !lock.Locked() {
		t.Error("Lock should be locked")
	}
}

func TestLockfile_StaleByTime(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a lockfile with an old timestamp (2 hours ago)
	pid := os.Getpid()
	timestamp := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	content := fmt.Sprintf("%d\n%s\n", pid, timestamp)
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create old lockfile: %v", err)
	}

	// Should be able to acquire the lock (stale by time)
	lock := New(lockPath)
	if err := lock.TryAcquire(); err != nil {
		t.Fatalf("Failed to acquire old lock: %v", err)
	}
	defer lock.Release()
}

func TestLockfile_ReleaseNotLocked(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")
	lock := New(lockPath)

	// Releasing an unlocked lock should be a no-op
	if err := lock.Release(); err != nil {
		t.Errorf("Expected no error when releasing unlocked lock, got: %v", err)
	}
}