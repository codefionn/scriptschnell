package acp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
)

// TestACPFileSystemFallback ensures the filesystem falls back to local reads outside the working dir.
func TestACPFileSystemFallback(t *testing.T) {
	agent := newTestAgent(t)
	acpFS := NewACPFileSystem(agent.conn, "test-session", agent.config.WorkingDir)

	var _ fs.FileSystem = acpFS

	ctx := context.Background()

	// Create a file outside the working directory so fallback is used.
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "sample.txt")
	want := []byte("hello")
	if err := os.WriteFile(outsidePath, want, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	data, err := acpFS.ReadFile(ctx, outsidePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != string(want) {
		t.Fatalf("ReadFile = %q, want %q", data, want)
	}

	stat, err := acpFS.Stat(ctx, outsidePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if stat.Path != outsidePath {
		t.Errorf("Stat path = %s, want %s", stat.Path, outsidePath)
	}

	exists, err := acpFS.Exists(ctx, filepath.Join(outsideDir, "missing.txt"))
	if err != nil {
		t.Fatalf("Exists returned error: %v", err)
	}
	if exists {
		t.Errorf("Exists returned true for missing file")
	}
}
