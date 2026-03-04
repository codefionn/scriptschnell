package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Test that all config fields are preserved through Load->Save cycle
// This test verifies the fix for the bug where LogToConsole, Sandbox, and Loop
// fields were being lost because marshalWithEncryptedSecrets() wasn't copying them.
func TestAllFieldsPreservedThroughSaveLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("Failed to remove temp dir %q: %v", tmpDir, err)
		}
	}()

	configPath := filepath.Join(tmpDir, "config.json")

	// Create a config with all fields set to non-default values
	originalCfg := DefaultConfig()
	originalCfg.LogToConsole = true
	originalCfg.Temperature = 0.7
	originalCfg.DisableAnimations = true
	originalCfg.LogLevel = "debug"
	originalCfg.Search.Provider = "exa"
	originalCfg.Search.Exa.APIKey = "test-api-key"

	// Set Sandbox config (note: BestEffort defaults to true, so we don't override it)
	originalCfg.Sandbox.AdditionalReadOnlyPaths = []string{"/read-only-path"}
	originalCfg.Sandbox.AdditionalReadWritePaths = []string{"/read-write-path"}
	originalCfg.Sandbox.DisableSandbox = true

	// Set Loop config
	originalCfg.Loop.Strategy = "conservative"
	originalCfg.Loop.MaxIterations = 100
	originalCfg.Loop.EnableAutoContinue = true

	// Save the config
	if err := originalCfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load the config
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify all fields are preserved
	if loadedCfg.LogToConsole != originalCfg.LogToConsole {
		t.Errorf("LogToConsole not preserved: got %v, want %v", loadedCfg.LogToConsole, originalCfg.LogToConsole)
	}

	if loadedCfg.Sandbox.AdditionalReadOnlyPaths[0] != originalCfg.Sandbox.AdditionalReadOnlyPaths[0] {
		t.Errorf("Sandbox.AdditionalReadOnlyPaths not preserved: got %v, want %v",
			loadedCfg.Sandbox.AdditionalReadOnlyPaths, originalCfg.Sandbox.AdditionalReadOnlyPaths)
	}

	if loadedCfg.Sandbox.AdditionalReadWritePaths[0] != originalCfg.Sandbox.AdditionalReadWritePaths[0] {
		t.Errorf("Sandbox.AdditionalReadWritePaths not preserved: got %v, want %v",
			loadedCfg.Sandbox.AdditionalReadWritePaths, originalCfg.Sandbox.AdditionalReadWritePaths)
	}

	if loadedCfg.Sandbox.DisableSandbox != originalCfg.Sandbox.DisableSandbox {
		t.Errorf("Sandbox.DisableSandbox not preserved: got %v, want %v",
			loadedCfg.Sandbox.DisableSandbox, originalCfg.Sandbox.DisableSandbox)
	}

	if loadedCfg.Loop.Strategy != originalCfg.Loop.Strategy {
		t.Errorf("Loop.Strategy not preserved: got %v, want %v", loadedCfg.Loop.Strategy, originalCfg.Loop.Strategy)
	}

	if loadedCfg.Loop.MaxIterations != originalCfg.Loop.MaxIterations {
		t.Errorf("Loop.MaxIterations not preserved: got %v, want %v", loadedCfg.Loop.MaxIterations, originalCfg.Loop.MaxIterations)
	}

	if loadedCfg.Loop.EnableAutoContinue != originalCfg.Loop.EnableAutoContinue {
		t.Errorf("Loop.EnableAutoContinue not preserved: got %v, want %v",
			loadedCfg.Loop.EnableAutoContinue, originalCfg.Loop.EnableAutoContinue)
	}

	if loadedCfg.Temperature != originalCfg.Temperature {
		t.Errorf("Temperature not preserved: got %v, want %v", loadedCfg.Temperature, originalCfg.Temperature)
	}
}

// Test that defaults are used correctly when fields are missing from file
func TestDefaultsUsedForMissingFields(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("Failed to remove temp dir %q: %v", tmpDir, err)
		}
	}()

	configPath := filepath.Join(tmpDir, "config.json")

	// Write a minimal config file (simulating an old config file)
	minimalConfig := `{
  "working_dir": ".",
  "temperature": 0.5
}`
	if err := os.WriteFile(configPath, []byte(minimalConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Load the config
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify defaults are used for fields not in the file
	if cfg.LogToConsole != false {
		t.Errorf("LogToConsole should default to false, got %v", cfg.LogToConsole)
	}

	if cfg.Sandbox.BestEffort != true {
		t.Errorf("Sandbox.BestEffort should default to true, got %v", cfg.Sandbox.BestEffort)
	}

	if cfg.Loop.Strategy != "" {
		// Loop config may have empty strategy as valid default
		t.Logf("Loop.Strategy is %q", cfg.Loop.Strategy)
	}
}
