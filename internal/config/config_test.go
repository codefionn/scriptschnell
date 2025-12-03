package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddContextDirectory(t *testing.T) {
	cfg := DefaultConfig()

	// Test adding a directory
	cfg.AddContextDirectory("/usr/share/doc")
	dirs := cfg.GetContextDirectories()
	if len(dirs) != 1 {
		t.Errorf("Expected 1 directory, got %d", len(dirs))
	}
	if dirs[0] != "/usr/share/doc" {
		t.Errorf("Expected /usr/share/doc, got %s", dirs[0])
	}

	// Test adding another directory
	cfg.AddContextDirectory("/home/user/docs")
	dirs = cfg.GetContextDirectories()
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories, got %d", len(dirs))
	}

	// Test adding duplicate directory (should not add)
	cfg.AddContextDirectory("/usr/share/doc")
	dirs = cfg.GetContextDirectories()
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories after duplicate add, got %d", len(dirs))
	}
}

func TestRemoveContextDirectory(t *testing.T) {
	cfg := DefaultConfig()

	// Add some directories
	cfg.AddContextDirectory("/usr/share/doc")
	cfg.AddContextDirectory("/home/user/docs")
	cfg.AddContextDirectory("/opt/docs")

	// Test removing existing directory
	removed := cfg.RemoveContextDirectory("/home/user/docs")
	if !removed {
		t.Error("Expected removal to return true")
	}
	dirs := cfg.GetContextDirectories()
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories after removal, got %d", len(dirs))
	}

	// Test removing non-existent directory
	removed = cfg.RemoveContextDirectory("/non/existent")
	if removed {
		t.Error("Expected removal of non-existent directory to return false")
	}

	// Verify remaining directories
	dirs = cfg.GetContextDirectories()
	if len(dirs) != 2 {
		t.Errorf("Expected 2 directories, got %d", len(dirs))
	}
}

func TestGetContextDirectories(t *testing.T) {
	cfg := DefaultConfig()

	// Test empty list
	dirs := cfg.GetContextDirectories()
	if len(dirs) != 0 {
		t.Errorf("Expected 0 directories, got %d", len(dirs))
	}

	// Add directories
	cfg.AddContextDirectory("/usr/share/doc")
	cfg.AddContextDirectory("/home/user/docs")

	// Test that we get a copy (not the original slice)
	dirs = cfg.GetContextDirectories()
	dirs[0] = "/modified"

	// Original should be unchanged
	actualDirs := cfg.GetContextDirectories()
	if actualDirs[0] == "/modified" {
		t.Error("GetContextDirectories should return a copy, not the original slice")
	}
}

func TestContextDirectoriesPersistence(t *testing.T) {
	// Create a temporary directory for test config
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	// Create config with context directories
	cfg := DefaultConfig()
	cfg.AddContextDirectory("/usr/share/doc")
	cfg.AddContextDirectory("/home/user/docs")

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
	dirs := loadedCfg.GetContextDirectories()
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

	// Test GetContextDirectories with nil
	dirs := cfg.GetContextDirectories()
	if dirs == nil {
		t.Error("GetContextDirectories should return empty slice, not nil")
	}
	if len(dirs) != 0 {
		t.Errorf("Expected empty slice, got %d items", len(dirs))
	}

	// Test AddContextDirectory with nil
	cfg.AddContextDirectory("/test")
	dirs = cfg.GetContextDirectories()
	if len(dirs) != 1 {
		t.Errorf("Expected 1 directory after add to nil, got %d", len(dirs))
	}

	// Test RemoveContextDirectory with nil
	cfg2 := &Config{}
	removed := cfg2.RemoveContextDirectory("/test")
	if removed {
		t.Error("Remove from nil should return false")
	}
}

func TestContextDirectoriesInLoadedConfig(t *testing.T) {
	// Create a temporary config file without context_directories field
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

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
	dirs := cfg.GetContextDirectories()
	if dirs == nil {
		t.Error("Context directories should be initialized to empty slice")
	}
	if len(dirs) != 0 {
		t.Errorf("Expected 0 context directories, got %d", len(dirs))
	}

	// Test that we can add to it
	cfg.AddContextDirectory("/test")
	dirs = cfg.GetContextDirectories()
	if len(dirs) != 1 {
		t.Errorf("Expected 1 directory after add, got %d", len(dirs))
	}
}
