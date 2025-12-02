package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/statcode-ai/scriptschnell/internal/provider"
)

// modelMenuItemV2 implements MenuItem for model selection
type modelMenuItemV2 struct {
	model            *provider.Model
	modelType        string
	showProviderName bool
}

func (m modelMenuItemV2) Title() string {
	if m.showProviderName {
		return fmt.Sprintf("[%s] %s (%s)", m.model.Provider, m.model.Name, m.model.ID)
	}
	return fmt.Sprintf("%s (%s)", m.model.Name, m.model.ID)
}

func (m modelMenuItemV2) Description() string {
	return m.model.Description
}

func (m modelMenuItemV2) FilterValue() string {
	return fmt.Sprintf("%s %s %s", m.model.Name, m.model.ID, m.model.Description)
}

// ModelsMenuV2 wraps GenericMenu for model selection
type ModelsMenuV2 struct {
	*GenericMenu
	modelType string
}

// NewModelsMenuV2 creates a new model selection menu using the generic component
func NewModelsMenuV2(providerMgr *provider.Manager, modelType string, width, height int) *ModelsMenuV2 {
	models := providerMgr.ListAllModels()

	// Check if there are multiple providers
	providerCount := len(providerMgr.ListProviders())
	showProviderName := providerCount > 1

	// Convert to MenuItem
	items := make([]MenuItem, len(models))
	for i, m := range models {
		items[i] = modelMenuItemV2{
			model:            m,
			modelType:        modelType,
			showProviderName: showProviderName,
		}
	}

	// Configure menu
	config := DefaultMenuConfig()
	config.Title = fmt.Sprintf("Select %s Model", modelType)
	config.Width = width
	config.Height = height
	config.EnableFiltering = true
	config.StartFiltering = true
	config.ShowStatusBar = true
	config.DisableQuitKeys = false
	config.HelpText = "Type to search • ↑/↓: Navigate • Enter: Select • Ctrl+L: Clear filter • Ctrl+Q/Ctrl+C/Esc: Cancel"

	menu := NewGenericMenu(items, config)

	return &ModelsMenuV2{
		GenericMenu: menu,
		modelType:   modelType,
	}
}

// Update overrides GenericMenu.Update to handle model-specific messages
func (m *ModelsMenuV2) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updatedModel, cmd := m.GenericMenu.Update(msg)

	// Type assertion to maintain *ModelsMenuV2 type
	if genericMenu, ok := updatedModel.(*GenericMenu); ok {
		m.GenericMenu = genericMenu
		return m, cmd
	}

	return updatedModel, cmd
}

// GetSelectedModel returns the selected model
func (m *ModelsMenuV2) GetSelectedModel() *provider.Model {
	selected := m.GetSelectedItem()
	if selected == nil {
		return nil
	}

	if modelItem, ok := selected.(modelMenuItemV2); ok {
		return modelItem.model
	}

	return nil
}

// GetModelType returns the model type (orchestration or summarize)
func (m *ModelsMenuV2) GetModelType() string {
	return m.modelType
}
