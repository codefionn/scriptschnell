package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/provider"
)

// TestModelMenuItemV2 tests the modelMenuItemV2 implementation
func TestModelMenuItemV2(t *testing.T) {
	model := &provider.Model{
		ID:          "gpt-4",
		Name:        "GPT-4",
		Provider:    "openai",
		Description: "Most capable GPT-4 model",
	}

	item := modelMenuItemV2{
		model:            model,
		modelType:        "orchestration",
		showProviderName: false,
	}

	// Test Title without provider name
	expectedTitle := "GPT-4 (gpt-4)"
	if item.Title() != expectedTitle {
		t.Errorf("Expected title '%s', got '%s'", expectedTitle, item.Title())
	}

	// Test Title with provider name
	itemWithProvider := modelMenuItemV2{
		model:            model,
		modelType:        "orchestration",
		showProviderName: true,
	}
	expectedTitleWithProvider := "[openai] GPT-4 (gpt-4)"
	if itemWithProvider.Title() != expectedTitleWithProvider {
		t.Errorf("Expected title '%s', got '%s'", expectedTitleWithProvider, itemWithProvider.Title())
	}

	// Test Description
	if item.Description() != model.Description {
		t.Errorf("Expected description '%s', got '%s'", model.Description, item.Description())
	}

	// Test FilterValue
	filterValue := item.FilterValue()
	expectedSubstrings := []string{"GPT-4", "gpt-4", "capable"}
	for _, substr := range expectedSubstrings {
		if !contains(filterValue, substr) {
			t.Errorf("FilterValue should contain '%s', got '%s'", substr, filterValue)
		}
	}
}

// TestModelMenuItemV2Interface tests that modelMenuItemV2 implements MenuItem
func TestModelMenuItemV2Interface(t *testing.T) {
	var _ MenuItem = modelMenuItemV2{}
}

// TestNewModelsMenuV2 tests creation of model menu v2
func TestNewModelsMenuV2(t *testing.T) {
	// Create a temporary provider manager with some test models
	testModels := []*provider.Model{
		{
			ID:          "claude-3-5-sonnet-20241022",
			Name:        "Claude 3.5 Sonnet",
			Provider:    "anthropic",
			Description: "Most intelligent model",
		},
		{
			ID:          "gpt-4",
			Name:        "GPT-4",
			Provider:    "openai",
			Description: "Most capable GPT-4",
		},
	}

	// We need to create a provider manager for testing
	// For now, we'll test the structure without actual provider manager
	t.Run("menu structure", func(t *testing.T) {
		// Create empty menu to test structure
		config := DefaultMenuConfig()
		items := make([]MenuItem, 0)
		genericMenu := NewGenericMenu(items, config)

		menu := &ModelsMenuV2{
			GenericMenu: genericMenu,
			modelType:   "orchestration",
		}

		if menu.modelType != "orchestration" {
			t.Errorf("Expected modelType 'orchestration', got '%s'", menu.modelType)
		}

		if menu.GenericMenu == nil {
			t.Error("GenericMenu should not be nil")
		}
	})

	t.Run("model items", func(t *testing.T) {
		// Test creating menu items directly
		items := make([]MenuItem, len(testModels))
		for i, m := range testModels {
			items[i] = modelMenuItemV2{model: m, modelType: "orchestration", showProviderName: false}
		}

		if len(items) != 2 {
			t.Errorf("Expected 2 items, got %d", len(items))
		}

		// Verify first item without provider name
		if items[0].Title() != "Claude 3.5 Sonnet (claude-3-5-sonnet-20241022)" {
			t.Errorf("Unexpected title: %s", items[0].Title())
		}

		// Test with provider name
		itemsWithProvider := make([]MenuItem, len(testModels))
		for i, m := range testModels {
			itemsWithProvider[i] = modelMenuItemV2{model: m, modelType: "orchestration", showProviderName: true}
		}

		// Verify first item with provider name
		if itemsWithProvider[0].Title() != "[anthropic] Claude 3.5 Sonnet (claude-3-5-sonnet-20241022)" {
			t.Errorf("Unexpected title: %s", itemsWithProvider[0].Title())
		}
	})
}

// TestModelsMenuV2GetSelectedModel tests getting selected model
func TestModelsMenuV2GetSelectedModel(t *testing.T) {
	model := &provider.Model{
		ID:          "gpt-4",
		Name:        "GPT-4",
		Provider:    "openai",
		Description: "Test model",
	}

	items := []MenuItem{
		modelMenuItemV2{model: model, modelType: "orchestration", showProviderName: false},
	}

	config := DefaultMenuConfig()
	genericMenu := NewGenericMenu(items, config)

	menu := &ModelsMenuV2{
		GenericMenu: genericMenu,
		modelType:   "orchestration",
	}

	// Initially no selection
	if menu.GetSelectedModel() != nil {
		t.Error("Initially should have no selected model")
	}

	// Simulate selection
	keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
	menu.Update(keyMsg)

	// Should have selection
	selected := menu.GetSelectedModel()
	if selected == nil {
		t.Fatalf("Should have selected model after Enter")
	}

	if selected.ID != "gpt-4" {
		t.Errorf("Expected selected model ID 'gpt-4', got '%s'", selected.ID)
	}
}

// TestModelsMenuV2GetModelType tests getting model type
func TestModelsMenuV2GetModelType(t *testing.T) {
	config := DefaultMenuConfig()
	items := make([]MenuItem, 0)
	genericMenu := NewGenericMenu(items, config)

	t.Run("orchestration type", func(t *testing.T) {
		menu := &ModelsMenuV2{
			GenericMenu: genericMenu,
			modelType:   "orchestration",
		}

		if menu.GetModelType() != "orchestration" {
			t.Errorf("Expected 'orchestration', got '%s'", menu.GetModelType())
		}
	})

	t.Run("summarize type", func(t *testing.T) {
		menu := &ModelsMenuV2{
			GenericMenu: genericMenu,
			modelType:   "summarize",
		}

		if menu.GetModelType() != "summarize" {
			t.Errorf("Expected 'summarize', got '%s'", menu.GetModelType())
		}
	})
}

// TestModelsMenuV2Update tests update propagation
func TestModelsMenuV2Update(t *testing.T) {
	model := &provider.Model{
		ID:          "test-model",
		Name:        "Test Model",
		Provider:    "test",
		Description: "Test",
	}

	items := []MenuItem{
		modelMenuItemV2{model: model, modelType: "orchestration", showProviderName: false},
	}

	config := DefaultMenuConfig()
	config.DisableQuitKeys = false
	genericMenu := NewGenericMenu(items, config)

	menu := &ModelsMenuV2{
		GenericMenu: genericMenu,
		modelType:   "orchestration",
	}

	// Test window resize
	resizeMsg := tea.WindowSizeMsg{Width: 100, Height: 30}
	updatedModel, _ := menu.Update(resizeMsg)

	if _, ok := updatedModel.(*ModelsMenuV2); !ok {
		t.Error("Update should return *ModelsMenuV2")
	}

	// Test key message
	keyMsg := tea.KeyMsg{Type: tea.KeyEsc}
	updatedModel, cmd := menu.Update(keyMsg)

	if _, ok := updatedModel.(*ModelsMenuV2); !ok {
		t.Error("Update should return *ModelsMenuV2")
	}

	// Should have quit command
	if cmd == nil {
		t.Error("Should have command after Esc")
	}
}

// TestModelsMenuV2FilteringConfig tests that filtering is enabled
func TestModelsMenuV2FilteringConfig(t *testing.T) {
	// Create menu items
	models := []*provider.Model{
		{ID: "model-1", Name: "Model 1", Provider: "test", Description: "First"},
		{ID: "model-2", Name: "Model 2", Provider: "test", Description: "Second"},
	}

	items := make([]MenuItem, len(models))
	for i, m := range models {
		items[i] = modelMenuItemV2{model: m, modelType: "orchestration", showProviderName: false}
	}

	config := DefaultMenuConfig()
	config.StartFiltering = true
	genericMenu := NewGenericMenu(items, config)

	menu := &ModelsMenuV2{
		GenericMenu: genericMenu,
		modelType:   "orchestration",
	}

	// Verify filtering is configured
	if !menu.config.EnableFiltering {
		t.Error("Filtering should be enabled")
	}

	if !menu.config.StartFiltering {
		t.Error("Should start in filtering mode")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr))))
}
