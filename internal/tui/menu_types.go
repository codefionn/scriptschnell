package tui

// MenuType represents the type of menu to display
type MenuType int

const (
	// MenuTypeNone indicates no menu should be displayed
	MenuTypeNone MenuType = iota
	// MenuTypeProvider indicates the provider configuration menu
	MenuTypeProvider
	// MenuTypeModels indicates the model selection menu
	MenuTypeModels
	// MenuTypeSearch indicates the search configuration menu
	MenuTypeSearch
	// MenuTypeSettings indicates the main settings menu
	MenuTypeSettings
	// MenuTypeSecrets indicates the encryption/password menu
	MenuTypeSecrets
	// MenuTypeMCP indicates the MCP configuration menu
	MenuTypeMCP
	// MenuTypeClearSession indicates the session should be cleared
	MenuTypeClearSession
	// MenuTypeNewTab indicates a new tab should be created
	MenuTypeNewTab
)

// ModelRole represents the role a model can have
type ModelRole string

const (
	// ModelRoleOrchestration is for main conversation and tool calls
	ModelRoleOrchestration ModelRole = "orchestration"
	// ModelRoleSummarization is for file summarization tasks
	ModelRoleSummarization ModelRole = "summarize"
)

// MenuResult represents the result of a command that may trigger a menu
type MenuResult struct {
	// Type indicates which menu to display (if any)
	Type MenuType
	// Message is an optional message to display to the user
	Message string
	// ModelRole is used for MenuTypeModels to indicate which role to configure
	ModelRole ModelRole
	// TabName is used for MenuTypeNewTab to specify the tab name
	TabName string
}

// NewMenuResult creates a MenuResult with just a message (no menu)
func NewMenuResult(message string) MenuResult {
	return MenuResult{
		Type:    MenuTypeNone,
		Message: message,
	}
}

// NewProviderMenuResult creates a MenuResult for the provider menu
func NewProviderMenuResult() MenuResult {
	return MenuResult{
		Type: MenuTypeProvider,
	}
}

// NewModelsMenuResult creates a MenuResult for the models menu with a specific role
func NewModelsMenuResult(role ModelRole) MenuResult {
	return MenuResult{
		Type:      MenuTypeModels,
		ModelRole: role,
	}
}

// NewSearchMenuResult creates a MenuResult for the search configuration menu
func NewSearchMenuResult() MenuResult {
	return MenuResult{
		Type: MenuTypeSearch,
	}
}

// NewSecretsMenuResult creates a MenuResult for the secrets/password menu
func NewSecretsMenuResult() MenuResult {
	return MenuResult{
		Type: MenuTypeSecrets,
	}
}

// NewMCPMenuResult creates a MenuResult for the MCP configuration menu
func NewMCPMenuResult() MenuResult {
	return MenuResult{
		Type: MenuTypeMCP,
	}
}

// NewSettingsMenuResult creates a MenuResult for the main settings menu
func NewSettingsMenuResult() MenuResult {
	return MenuResult{
		Type: MenuTypeSettings,
	}
}

// NewClearSessionResult creates a MenuResult that clears the session
func NewClearSessionResult() MenuResult {
	return MenuResult{
		Type:    MenuTypeClearSession,
		Message: "Session and todos cleared. Starting fresh conversation.",
	}
}

// NewNewTabResult creates a MenuResult that creates a new tab
func NewNewTabResult(name string) MenuResult {
	return MenuResult{
		Type:    MenuTypeNewTab,
		TabName: name,
	}
}
