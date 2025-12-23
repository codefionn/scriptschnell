package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/config"
)

func TestSearchProviderSelectionIsPersisted(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := config.DefaultConfig()

	menu := NewSettingsMenu(cfg, 80, 20)

	// Enter provider selection (default selection is "Search Provider")
	updated, _ := menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	menu = updated.(SettingsMenuModel)
	if !menu.providerMode {
		t.Fatalf("expected providerMode to be true after entering provider selection")
	}

	// Move to "Exa" and select.
	updated, _ = menu.Update(tea.KeyMsg{Type: tea.KeyDown})
	menu = updated.(SettingsMenuModel)
	updated, _ = menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	menu = updated.(SettingsMenuModel)

	if cfg.Search.Provider != "exa" {
		t.Fatalf("expected cfg.Search.Provider to be %q, got %q", "exa", cfg.Search.Provider)
	}

	loaded, err := config.Load(config.GetConfigPath())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if loaded.Search.Provider != "exa" {
		t.Fatalf("expected persisted Search.Provider to be %q, got %q", "exa", loaded.Search.Provider)
	}
}
