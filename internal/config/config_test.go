package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddContextDirectory(t *testing.T) {
	cfg := DefaultConfig()
	workspace := "/test/workspace"

	// Test adding a directory
	cfg.AddContextDirectory(workspace, "/usr/share/doc")
	dirs := cfg.GetContextDirectories(workspace)
	if len(dirs) != 1 {
		t.Errorf("Expected 1 directory, got %d", len(dirs))
	}
	if dirs[0] != "/usr/share/doc" {
		t.Errorf("Expected /usr/share/doc, got %s", dirs[0])
	}

	// Test adding another directory
	cfg.AddContextDirectory(workspace, "/home/user/docs")
	dirs = cfg.GetContextDirectories(workspace)
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories, got %d", len(dirs))
	}

	// Test adding duplicate directory (should not add)
	cfg.AddContextDirectory(workspace, "/usr/share/doc")
	dirs = cfg.GetContextDirectories(workspace)
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories after duplicate add, got %d", len(dirs))
	}
}

func TestRemoveContextDirectory(t *testing.T) {
	cfg := DefaultConfig()
	workspace := "/test/workspace"

	// Add some directories
	cfg.AddContextDirectory(workspace, "/usr/share/doc")
	cfg.AddContextDirectory(workspace, "/home/user/docs")
	cfg.AddContextDirectory(workspace, "/opt/docs")

	// Test removing existing directory
	removed := cfg.RemoveContextDirectory(workspace, "/home/user/docs")
	if !removed {
		t.Error("Expected removal to return true")
	}
	dirs := cfg.GetContextDirectories(workspace)
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories after removal, got %d", len(dirs))
	}

	// Test removing non-existent directory
	removed = cfg.RemoveContextDirectory(workspace, "/non/existent")
	if removed {
		t.Error("Expected removal of non-existent directory to return false")
	}

	// Verify remaining directories
	dirs = cfg.GetContextDirectories(workspace)
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories, got %d", len(dirs))
	}
}

func TestGetContextDirectories(t *testing.T) {
	cfg := DefaultConfig()
	workspace := "/test/workspace"

	// Test empty list
	dirs := cfg.GetContextDirectories(workspace)
	if len(dirs) != 0 {
		t.Errorf("Expected 0 directories, got %d", len(dirs))
	}

	// Add directories
	cfg.AddContextDirectory(workspace, "/usr/share/doc")
	cfg.AddContextDirectory(workspace, "/home/user/docs")

	// Test that we get a copy (not the original slice)
	dirs = cfg.GetContextDirectories(workspace)
	dirs[0] = "/modified"

	// Original should be unchanged
	actualDirs := cfg.GetContextDirectories(workspace)
	if actualDirs[0] == "/modified" {
		t.Error("GetContextDirectories should return a copy, not the original slice")
	}
}

func TestContextDirectoriesPersistence(t *testing.T) {
	// Create a temporary directory for test config
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	workspace := "/test/workspace"

	// Create config with context directories
	cfg := DefaultConfig()
	cfg.AddContextDirectory(workspace, "/usr/share/doc")
	cfg.AddContextDirectory(workspace, "/home/user/docs")

	// Save config
	err := cfg.Save(configPath)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load config
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify context directories were persisted
	dirs := loadedCfg.GetContextDirectories(workspace)
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories after load, got %d", len(dirs))
	}
	if dirs[0] != "/usr/share/doc" || dirs[1] != "/home/user/docs" {
		t.Errorf("Context directories not persisted correctly: %v", dirs)
	}
}

func TestContextDirectoriesNilHandling(t *testing.T) {
	// Test that nil handling works correctly
	cfg := &Config{}
	workspace := "/test/workspace"

	// Test GetContextDirectories with nil
	dirs := cfg.GetContextDirectories(workspace)
	if dirs == nil {
		t.Error("GetContextDirectories should return empty slice, not nil")
	}
	if len(dirs) != 0 {
		t.Errorf("Expected empty slice, got %d items", len(dirs))
	}

	// Test AddContextDirectory with nil
	cfg.AddContextDirectory(workspace, "/test")
	dirs = cfg.GetContextDirectories(workspace)
	if len(dirs) != 1 {
		t.Errorf("Expected 1 directory after add to nil, got %d", len(dirs))
	}

	// Test RemoveContextDirectory with nil
	cfg2 := &Config{}
	removed := cfg2.RemoveContextDirectory(workspace, "/test")
	if removed {
		t.Error("Remove from nil should return false")
	}
}

func TestContextDirectoriesInLoadedConfig(t *testing.T) {
	// Create a temporary config file without context_directories field
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	workspace := "/test/workspace"

	// Write minimal config JSON
	configJSON := `{
		"working_dir": ".",
		"cache_ttl_seconds": 300
	}`
	err := os.WriteFile(configPath, []byte(configJSON), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Load config
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify context_directories is initialized
	dirs := cfg.GetContextDirectories(workspace)
	if dirs == nil {
		t.Error("Context directories should be initialized to empty slice")
	}
	if len(dirs) != 0 {
		t.Errorf("Expected 0 context directories, got %d", len(dirs))
	}

	// Test that we can add to it
	cfg.AddContextDirectory(workspace, "/test")
	dirs = cfg.GetContextDirectories(workspace)
	if len(dirs) != 1 {
		t.Errorf("Expected 1 directory after add, got %d", len(dirs))
	}
}

func TestWorkspaceIsolation(t *testing.T) {
	cfg := DefaultConfig()
	workspace1 := "/workspace1"
	workspace2 := "/workspace2"

	// Add directories to workspace1
	cfg.AddContextDirectory(workspace1, "/usr/share/doc")
	cfg.AddContextDirectory(workspace1, "/home/user/docs")

	// Add directories to workspace2
	cfg.AddContextDirectory(workspace2, "/opt/docs")

	// Verify workspace1 has 2 directories
	dirs1 := cfg.GetContextDirectories(workspace1)
	if len(dirs1) != 2 {
		t.Errorf("Workspace1: Expected 2 directories, got %d", len(dirs1))
	}

	// Verify workspace2 has 1 directory
	dirs2 := cfg.GetContextDirectories(workspace2)
	if len(dirs2) != 1 {
		t.Errorf("Workspace2: Expected 1 directory, got %d", len(dirs2))
	}

	// Verify workspace3 (never used) has 0 directories
	dirs3 := cfg.GetContextDirectories("/workspace3")
	if len(dirs3) != 0 {
		t.Errorf("Workspace3: Expected 0 directories, got %d", len(dirs3))
	}

	// Remove directory from workspace1
	removed := cfg.RemoveContextDirectory(workspace1, "/usr/share/doc")
	if !removed {
		t.Error("Expected removal to return true")
	}

	// Verify workspace1 now has 1 directory
	dirs1 = cfg.GetContextDirectories(workspace1)
	if len(dirs1) != 1 {
		t.Errorf("Workspace1 after removal: Expected 1 directory, got %d", len(dirs1))
	}

	// Verify workspace2 is unaffected
	dirs2 = cfg.GetContextDirectories(workspace2)
	if len(dirs2) != 1 {
		t.Errorf("Workspace2 after removal from workspace1: Expected 1 directory, got %d", len(dirs2))
	}

	// Remove all directories from workspace1
	cfg.RemoveContextDirectory(workspace1, "/home/user/docs")

	// Verify workspace1 entry is removed from map
	if _, exists := cfg.ContextDirectories[workspace1]; exists {
		t.Error("Expected workspace1 to be removed from map when all directories removed")
	}
}
