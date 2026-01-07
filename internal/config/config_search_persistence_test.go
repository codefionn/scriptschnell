package config

import (
	"path/filepath"
	"testing"
)

// TestSearchProviderPersistenceAcrossModelChanges verifies that search settings
// are preserved when models are changed (which saves providers.json but not config.json)
func TestSearchProviderPersistenceAcrossModelChanges(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Step 1: Create config with search provider
	cfg := DefaultConfig()
	cfg.Search.Provider = "exa"
	cfg.Search.Exa.APIKey = "test-exa-key"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save initial config: %v", err)
	}

	// Step 2: Reload config (simulating what happens between operations)
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}

	// Verify search settings survived reload
	if reloaded.Search.Provider != "exa" {
		t.Errorf("Search.Provider after reload: got %q, want %q", reloaded.Search.Provider, "exa")
	}
	if reloaded.Search.Exa.APIKey == "" {
		t.Error("Search.Exa.APIKey was lost after reload")
	}

	// Step 3: Modify something unrelated and save again
	reloaded.Temperature = 0.9
	if err := reloaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save after temperature change: %v", err)
	}

	// Step 4: Reload and verify search settings still intact
	final, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload final config: %v", err)
	}

	if final.Search.Provider != "exa" {
		t.Errorf("Search.Provider after unrelated save: got %q, want %q", final.Search.Provider, "exa")
	}
	if final.Search.Exa.APIKey == "" {
		t.Error("Search.Exa.APIKey was lost after unrelated save")
	}
	if final.Temperature != 0.9 {
		t.Errorf("Temperature not preserved: got %f, want %f", final.Temperature, 0.9)
	}
}

// TestSearchProviderPersistenceAcrossTabStateSaves verifies that search settings
// are preserved when tab state is saved
func TestSearchProviderPersistenceAcrossTabStateSaves(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Step 1: Create config with search provider
	cfg := DefaultConfig()
	cfg.Search.Provider = "google_pse"
	cfg.Search.GooglePSE.APIKey = "test-google-key"
	cfg.Search.GooglePSE.CX = "test-cx-id"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save initial config: %v", err)
	}

	// Step 2: Load and modify tab state (simulating tab switching/creation)
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if loaded.OpenTabs == nil {
		loaded.OpenTabs = make(map[string]*WorkspaceTabState)
	}
	loaded.OpenTabs["/workspace1"] = &WorkspaceTabState{
		ActiveTabID: 1,
		TabIDs:      []int{1, 2, 3},
		TabNames:    map[int]string{1: "Tab 1", 2: "Tab 2", 3: "Tab 3"},
	}

	if err := loaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save after tab state change: %v", err)
	}

	// Step 3: Reload and verify both tab state and search settings
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload after tab save: %v", err)
	}

	// Verify tab state was saved
	if reloaded.OpenTabs == nil || reloaded.OpenTabs["/workspace1"] == nil {
		t.Fatal("Tab state was not saved")
	}
	if len(reloaded.OpenTabs["/workspace1"].TabIDs) != 3 {
		t.Errorf("Tab IDs not preserved: got %d tabs, want 3", len(reloaded.OpenTabs["/workspace1"].TabIDs))
	}

	// Verify search settings were preserved
	if reloaded.Search.Provider != "google_pse" {
		t.Errorf("Search.Provider after tab save: got %q, want %q", reloaded.Search.Provider, "google_pse")
	}
	if reloaded.Search.GooglePSE.APIKey == "" {
		t.Error("Search.GooglePSE.APIKey was lost after tab save")
	}
	if reloaded.Search.GooglePSE.CX != "test-cx-id" {
		t.Errorf("Search.GooglePSE.CX: got %q, want %q", reloaded.Search.GooglePSE.CX, "test-cx-id")
	}
}

// TestSearchProviderChangePreservesOtherSettings verifies that changing search
// provider preserves other search API keys
func TestSearchProviderChangePreservesOtherSettings(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Step 1: Set up Exa
	cfg := DefaultConfig()
	cfg.Search.Provider = "exa"
	cfg.Search.Exa.APIKey = "exa-key-123"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save with Exa: %v", err)
	}

	// Step 2: Load and switch to Google PSE
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	loaded.Search.Provider = "google_pse"
	loaded.Search.GooglePSE.APIKey = "google-key-456"
	loaded.Search.GooglePSE.CX = "cx-789"

	if err := loaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save with Google PSE: %v", err)
	}

	// Step 3: Reload and verify both providers' keys are preserved
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload: %v", err)
	}

	if reloaded.Search.Provider != "google_pse" {
		t.Errorf("Active provider: got %q, want %q", reloaded.Search.Provider, "google_pse")
	}

	// Exa key should still be there (even though it's not the active provider)
	if reloaded.Search.Exa.APIKey == "" {
		t.Error("Exa API key was lost when switching to Google PSE")
	}

	// Google PSE keys should be set
	if reloaded.Search.GooglePSE.APIKey == "" {
		t.Error("Google PSE API key was not saved")
	}
	if reloaded.Search.GooglePSE.CX != "cx-789" {
		t.Errorf("Google PSE CX: got %q, want %q", reloaded.Search.GooglePSE.CX, "cx-789")
	}
}

// TestSearchProviderPersistenceWithAuthorizations verifies that search settings
// are preserved when domain/command authorizations are added
func TestSearchProviderPersistenceWithAuthorizations(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Step 1: Set up search provider
	cfg := DefaultConfig()
	cfg.Search.Provider = "perplexity"
	cfg.Search.Perplexity.APIKey = "perplexity-key"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save initial config: %v", err)
	}

	// Step 2: Load and add authorizations
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	loaded.AuthorizeDomain("github.com")
	loaded.AuthorizeDomain("*.googleapis.com")
	loaded.AuthorizeCommand("git commit")
	loaded.AuthorizeCommand("npm install")

	if err := loaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save after authorizations: %v", err)
	}

	// Step 3: Reload and verify both authorizations and search settings
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload: %v", err)
	}

	// Verify authorizations were saved
	if !reloaded.IsDomainAuthorized("github.com") {
		t.Error("Domain authorization for github.com was not saved")
	}
	if !reloaded.IsCommandAuthorized("git commit") {
		t.Error("Command authorization for 'git commit' was not saved")
	}

	// Verify search settings were preserved
	if reloaded.Search.Provider != "perplexity" {
		t.Errorf("Search.Provider after authorizations: got %q, want %q", reloaded.Search.Provider, "perplexity")
	}
	if reloaded.Search.Perplexity.APIKey == "" {
		t.Error("Search.Perplexity.APIKey was lost after adding authorizations")
	}
}

// TestSearchProviderPersistenceWithMCPServers verifies that search settings
// are preserved when MCP servers are configured
func TestSearchProviderPersistenceWithMCPServers(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Step 1: Set up search provider
	cfg := DefaultConfig()
	cfg.Search.Provider = "exa"
	cfg.Search.Exa.APIKey = "exa-key"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save initial config: %v", err)
	}

	// Step 2: Load and add MCP server
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	if loaded.MCP.Servers == nil {
		loaded.MCP.Servers = make(map[string]*MCPServerConfig)
	}
	loaded.MCP.Servers["test-server"] = &MCPServerConfig{
		Type:        "command",
		Description: "Test MCP Server",
		Command: &MCPCommandConfig{
			Exec: []string{"test-command", "--arg"},
		},
	}

	if err := loaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save after MCP config: %v", err)
	}

	// Step 3: Reload and verify both MCP and search settings
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload: %v", err)
	}

	// Verify MCP server was saved
	if reloaded.MCP.Servers == nil || reloaded.MCP.Servers["test-server"] == nil {
		t.Fatal("MCP server was not saved")
	}

	// Verify search settings were preserved
	if reloaded.Search.Provider != "exa" {
		t.Errorf("Search.Provider after MCP config: got %q, want %q", reloaded.Search.Provider, "exa")
	}
	if reloaded.Search.Exa.APIKey == "" {
		t.Error("Search.Exa.APIKey was lost after adding MCP server")
	}
}

// TestSearchProviderEmptyAfterDefaultConfig verifies that a fresh config
// has proper default search settings
func TestSearchProviderEmptyAfterDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Default should have empty provider but initialized sub-configs
	if cfg.Search.Provider != "" {
		t.Errorf("Default Search.Provider should be empty, got %q", cfg.Search.Provider)
	}

	if cfg.Search.Exa.APIKey != "" {
		t.Errorf("Default Search.Exa.APIKey should be empty, got %q", cfg.Search.Exa.APIKey)
	}

	if cfg.Search.GooglePSE.APIKey != "" {
		t.Errorf("Default Search.GooglePSE.APIKey should be empty, got %q", cfg.Search.GooglePSE.APIKey)
	}

	if cfg.Search.Perplexity.APIKey != "" {
		t.Errorf("Default Search.Perplexity.APIKey should be empty, got %q", cfg.Search.Perplexity.APIKey)
	}
}

// TestSearchProviderPersistenceAfterMultipleSaveCycles simulates rapid
// save/load cycles that might happen during active TUI use
func TestSearchProviderPersistenceAfterMultipleSaveCycles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Initial setup
	cfg := DefaultConfig()
	cfg.Search.Provider = "exa"
	cfg.Search.Exa.APIKey = "test-key"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save initial config: %v", err)
	}

	// Simulate multiple operations
	for i := 0; i < 10; i++ {
		loaded, err := Load(configPath)
		if err != nil {
			t.Fatalf("Failed to load on iteration %d: %v", i, err)
		}

		// Verify search settings are intact
		if loaded.Search.Provider != "exa" {
			t.Errorf("Iteration %d: Search.Provider = %q, want %q", i, loaded.Search.Provider, "exa")
		}
		if loaded.Search.Exa.APIKey == "" {
			t.Errorf("Iteration %d: Search.Exa.APIKey was lost", i)
		}

		// Make some unrelated change
		loaded.Temperature = 0.5 + float64(i)*0.01

		if err := loaded.Save(configPath); err != nil {
			t.Fatalf("Failed to save on iteration %d: %v", i, err)
		}
	}

	// Final verification
	final, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load final config: %v", err)
	}

	if final.Search.Provider != "exa" {
		t.Errorf("Final: Search.Provider = %q, want %q", final.Search.Provider, "exa")
	}
	if final.Search.Exa.APIKey == "" {
		t.Error("Final: Search.Exa.APIKey was lost after multiple cycles")
	}
}

// TestSearchProviderDisabling verifies that disabling search (setting to empty)
// works correctly
func TestSearchProviderDisabling(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Step 1: Set up search provider
	cfg := DefaultConfig()
	cfg.Search.Provider = "exa"
	cfg.Search.Exa.APIKey = "test-key"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save with provider: %v", err)
	}

	// Step 2: Load and disable search
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	loaded.Search.Provider = "" // Explicitly disable

	if err := loaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save with disabled provider: %v", err)
	}

	// Step 3: Reload and verify provider is disabled but API key preserved
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload: %v", err)
	}

	if reloaded.Search.Provider != "" {
		t.Errorf("Search.Provider should be empty (disabled), got %q", reloaded.Search.Provider)
	}

	// API key should still be preserved in case user wants to re-enable
	if reloaded.Search.Exa.APIKey == "" {
		t.Error("Search.Exa.APIKey should be preserved even when provider is disabled")
	}
}

// TestSearchConfigWithSecretsEncryption verifies that search settings work
// correctly with secrets encryption enabled
func TestSearchConfigWithSecretsEncryption(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	password := "test-password-123"

	// Step 1: Create config with encryption
	cfg := DefaultConfig()
	if err := cfg.UpdateSecretsPassword(password); err != nil {
		t.Fatalf("Failed to set secrets password: %v", err)
	}

	cfg.Search.Provider = "exa"
	cfg.Search.Exa.APIKey = "secret-exa-key"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save encrypted config: %v", err)
	}

	// Step 2: Load without password (should work, just encrypted)
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load encrypted config: %v", err)
	}

	// Should have encrypted data (not readable yet)
	if err := loaded.ApplySecretsPassword(password); err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	// Now should be decrypted
	if loaded.Search.Provider != "exa" {
		t.Errorf("Search.Provider after decryption: got %q, want %q", loaded.Search.Provider, "exa")
	}
	if loaded.Search.Exa.APIKey != "secret-exa-key" {
		t.Errorf("Search.Exa.APIKey after decryption: got %q, want %q", loaded.Search.Exa.APIKey, "secret-exa-key")
	}

	// Step 3: Make unrelated change and save
	loaded.Temperature = 0.8
	if err := loaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save after decryption: %v", err)
	}

	// Step 4: Reload and verify search settings still encrypted and preserved
	final, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload encrypted config: %v", err)
	}

	if err := final.ApplySecretsPassword(password); err != nil {
		t.Fatalf("Failed to decrypt final: %v", err)
	}

	if final.Search.Provider != "exa" {
		t.Errorf("Final Search.Provider: got %q, want %q", final.Search.Provider, "exa")
	}
	if final.Search.Exa.APIKey != "secret-exa-key" {
		t.Errorf("Final Search.Exa.APIKey: got %q, want %q", final.Search.Exa.APIKey, "secret-exa-key")
	}
}

// TestSearchProviderWithContextDirectories verifies search settings persist
// when context directories are modified
func TestSearchProviderWithContextDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Step 1: Set up search and context dirs
	cfg := DefaultConfig()
	cfg.Search.Provider = "google_pse"
	cfg.Search.GooglePSE.APIKey = "google-key"
	cfg.AddContextDirectory("/workspace", "src")
	cfg.AddContextDirectory("/workspace", "tests")

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Step 2: Load and add more context dirs
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	loaded.AddContextDirectory("/workspace", "docs")
	loaded.AddContextDirectory("/other-workspace", "lib")

	if err := loaded.Save(configPath); err != nil {
		t.Fatalf("Failed to save after adding context dirs: %v", err)
	}

	// Step 3: Verify both context dirs and search settings
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to reload: %v", err)
	}

	// Check context dirs
	if len(reloaded.ContextDirectories) != 2 {
		t.Errorf("Expected 2 workspaces in ContextDirectories, got %d", len(reloaded.ContextDirectories))
	}

	// Check search settings
	if reloaded.Search.Provider != "google_pse" {
		t.Errorf("Search.Provider: got %q, want %q", reloaded.Search.Provider, "google_pse")
	}
	if reloaded.Search.GooglePSE.APIKey == "" {
		t.Error("Search.GooglePSE.APIKey was lost")
	}
}
