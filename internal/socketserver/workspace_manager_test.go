package socketserver

import (
	"context"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
)

func TestWorkspaceManager(t *testing.T) {
	// Test workspace manager creation
	wm, err := NewWorkspaceManager()
	if err != nil {
		t.Fatalf("Failed to create workspace manager: %v", err)
	}

	if wm == nil {
		t.Fatal("Workspace manager is nil")
	}

	// Test workspace count
	initialCount := wm.GetWorkspaceCount()
	if initialCount != 0 {
		t.Errorf("Expected 0 workspaces initially, got %d", initialCount)
	}
}

func TestWorkspaceGeneration(t *testing.T) {
	// Test workspace ID generation
	id1 := generateWorkspaceID("/tmp/test")
	id2 := generateWorkspaceID("/tmp/test")
	id3 := generateWorkspaceID("/tmp/other")

	if id1 != id2 {
		t.Errorf("Same path should generate same ID: %s != %s", id1, id2)
	}

	if id1 == id3 {
		t.Errorf("Different paths should generate different IDs: %s == %s", id1, id3)
	}
}

func TestWorkspaceInfo(t *testing.T) {
	now := time.Now()

	// Test WorkspaceInternalInfo creation
	ws := &WorkspaceInternalInfo{
		ID:               "test-id",
		Path:             "/tmp/test",
		Name:             "test",
		RepositoryRoot:   "/tmp/test",
		CurrentBranch:    "main",
		IsWorktree:       false,
		WorktreeName:     "",
		SessionCount:     0,
		LastAccessed:     now,
		CreatedAt:        now,
		ContextDirs:      []string{"/tmp/context"},
		LandlockRead:     []string{"/tmp/read"},
		LandlockWrite:    []string{"/tmp/write"},
		DomainsApproved:  map[string]bool{"example.com": true},
		CommandsApproved: map[string]bool{"git": true},
	}

	if ws.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", ws.ID)
	}

	if ws.Name != "test" {
		t.Errorf("Expected name 'test', got '%s'", ws.Name)
	}

	if ws.SessionCount != 0 {
		t.Errorf("Expected session count 0, got %d", ws.SessionCount)
	}
}

func TestServerWithWorkspaceManager(t *testing.T) {
	// Test server creation with workspace manager
	cfg := config.DefaultConfig()
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server == nil {
		t.Fatal("Server is nil")
	}

	if server.workspaceManager == nil {
		t.Fatal("Workspace manager is not initialized")
	}

	// Test workspace count
	count := server.workspaceManager.GetWorkspaceCount()
	if count < 0 {
		t.Errorf("Expected non-negative workspace count, got %d", count)
	}
}

func TestWorkspaceSessionCountUpdate(t *testing.T) {
	wm, err := NewWorkspaceManager()
	if err != nil {
		t.Fatalf("Failed to create workspace manager: %v", err)
	}

	// Register a workspace with a known ID before updating its session count
	ctx := context.Background()
	tmpDir := t.TempDir()
	ws0, err := wm.ResolveWorkspace(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to resolve workspace: %v", err)
	}
	workspaceID := ws0.ID

	// Test increment
	wm.UpdateWorkspaceSessionCount(workspaceID, 1)
	ws, exists := wm.GetWorkspace(workspaceID)
	if !exists {
		t.Fatal("Workspace should exist after count update")
	}
	if ws.SessionCount != 1 {
		t.Errorf("Expected session count 1, got %d", ws.SessionCount)
	}

	// Test decrement
	wm.UpdateWorkspaceSessionCount(workspaceID, -1)
	ws, exists = wm.GetWorkspace(workspaceID)
	if !exists {
		t.Fatal("Workspace should still exist after decrement")
	}
	if ws.SessionCount != 0 {
		t.Errorf("Expected session count 0, got %d", ws.SessionCount)
	}

	// Test that count doesn't go negative
	wm.UpdateWorkspaceSessionCount(workspaceID, -5)
	ws, exists = wm.GetWorkspace(workspaceID)
	if !exists {
		t.Fatal("Workspace should still exist")
	}
	if ws.SessionCount != 0 {
		t.Errorf("Expected session count 0 (no negative), got %d", ws.SessionCount)
	}
}

func TestWorkspaceSetters(t *testing.T) {
	wm, err := NewWorkspaceManager()
	if err != nil {
		t.Fatalf("Failed to create workspace manager: %v", err)
	}

	// Create a temp directory and use it as the workspace path
	ctx := context.Background()
	tmpDir := t.TempDir()
	ws, err := wm.ResolveWorkspace(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to resolve workspace: %v", err)
	}

	// Test setting context directories
	contextDirs := []string{"/tmp/context1", "/tmp/context2"}
	err = wm.SetWorkspaceContextDirs(ws.ID, contextDirs)
	if err != nil {
		t.Errorf("Failed to set context directories: %v", err)
	}

	// Verify
	updatedWs, exists := wm.GetWorkspace(ws.ID)
	if !exists {
		t.Fatal("Workspace should exist")
	}
	if len(updatedWs.ContextDirs) != 2 {
		t.Errorf("Expected 2 context directories, got %d", len(updatedWs.ContextDirs))
	}

	// Test setting landlock permissions
	readPaths := []string{"/tmp/read1"}
	writePaths := []string{"/tmp/write1"}
	err = wm.SetWorkspaceLandlockPermissions(ws.ID, readPaths, writePaths)
	if err != nil {
		t.Errorf("Failed to set landlock permissions: %v", err)
	}

	// Test approving domain
	err = wm.ApproveDomainForWorkspace(ws.ID, "example.com")
	if err != nil {
		t.Errorf("Failed to approve domain: %v", err)
	}

	// Test approving command
	err = wm.ApproveCommandForWorkspace(ws.ID, "git")
	if err != nil {
		t.Errorf("Failed to approve command: %v", err)
	}

	// Verify approvals
	updatedWs, exists = wm.GetWorkspace(ws.ID)
	if !exists {
		t.Fatal("Workspace should exist")
	}
	if !updatedWs.DomainsApproved["example.com"] {
		t.Error("Domain should be approved")
	}
	if !updatedWs.CommandsApproved["git"] {
		t.Error("Command should be approved")
	}
}
