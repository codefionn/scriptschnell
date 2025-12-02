package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/provider"
)

var (
	settingsMainItemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	settingsMainSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	settingsMainTitleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	settingsMainHelpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type settingsMainAction int

const (
	actionConfigureProviders settingsMainAction = iota
	actionConfigureModels
	actionConfigureSearch
	actionConfigureSecrets
	actionConfigureMCP
)

type settingsMainItem struct {
	title       string
	description string
	action      settingsMainAction
}

func (i settingsMainItem) FilterValue() string { return i.title }

type settingsMainItemDelegate struct{}

func (d settingsMainItemDelegate) Height() int                             { return 2 }
func (d settingsMainItemDelegate) Spacing() int                            { return 1 }
func (d settingsMainItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d settingsMainItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(settingsMainItem)
	if !ok {
		return
	}

	var str string
	if index == m.Index() {
		str = settingsMainSelectedItemStyle.Render(fmt.Sprintf("▸ %s", i.title))
	} else {
		str = settingsMainItemStyle.Render(fmt.Sprintf("  %s", i.title))
	}

	desc := settingsMainHelpStyle.Render(i.description)
	fmt.Fprintf(w, "%s\n%s", str, settingsMainItemStyle.Render(desc))
}

type SettingsMainMenuModel struct {
	list        list.Model
	cfg         *config.Config
	providerMgr *provider.Manager
	width       int
	height      int
	quitting    bool
	result      MenuResult
}

func NewSettingsMainMenu(cfg *config.Config, providerMgr *provider.Manager, width, height int) SettingsMainMenuModel {
	const defaultWidth = 80
	const defaultHeight = 20
	if width == 0 {
		width = defaultWidth
	}
	if height == 0 {
		height = defaultHeight
	}

	items := []list.Item{
		settingsMainItem{
			title:       "Configure Providers",
			description: "Add, remove, or test LLM provider API keys (OpenAI, Anthropic, Google, OpenRouter)",
			action:      actionConfigureProviders,
		},
		settingsMainItem{
			title:       "Configure Models",
			description: "Select orchestration and summarization models",
			action:      actionConfigureModels,
		},
		settingsMainItem{
			title:       "Configure Search",
			description: "Set up web search provider (Exa, Google PSE, Perplexity)",
			action:      actionConfigureSearch,
		},
		settingsMainItem{
			title:       "Set Encryption Password",
			description: "Protect API keys with a custom password (blank = default)",
			action:      actionConfigureSecrets,
		},
		settingsMainItem{
			title:       "Configure MCP Servers",
			description: "Manage custom Model Context Protocol servers (add, enable/disable, remove)",
			action:      actionConfigureMCP,
		},
	}

	l := list.New(items, settingsMainItemDelegate{}, width, height-4)
	l.Title = "Settings"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = settingsMainTitleStyle

	return SettingsMainMenuModel{
		list:        l,
		cfg:         cfg,
		providerMgr: providerMgr,
		width:       width,
		height:      height,
	}
}

func (m SettingsMainMenuModel) Init() tea.Cmd {
	return nil
}

func (m SettingsMainMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 4)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			i, ok := m.list.SelectedItem().(settingsMainItem)
			if !ok {
				return m, nil
			}
			switch i.action {
			case actionConfigureProviders:
				m.result = NewProviderMenuResult()
				m.quitting = true
				return m, tea.Quit
			case actionConfigureModels:
				m.result = NewModelsMenuResult(ModelRoleOrchestration)
				m.quitting = true
				return m, tea.Quit
			case actionConfigureSearch:
				m.result = NewSearchMenuResult()
				m.quitting = true
				return m, tea.Quit
			case actionConfigureSecrets:
				m.result = NewSecretsMenuResult()
				m.quitting = true
				return m, tea.Quit
			case actionConfigureMCP:
				m.result = NewMCPMenuResult()
				m.quitting = true
				return m, tea.Quit
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m SettingsMainMenuModel) View() string {
	if m.quitting {
		return ""
	}
	help := settingsMainHelpStyle.Render("\n↑/↓: Navigate • Enter: Select • q/ESC: Close")
	return m.list.View() + help
}

// GetResult returns the result after the menu closes (deprecated, use GetMenuResult)
func (m SettingsMainMenuModel) GetResult() string {
	// For backwards compatibility, return empty string
	return ""
}

// GetMenuResult returns the MenuResult after the menu closes
func (m SettingsMainMenuModel) GetMenuResult() MenuResult {
	return m.result
}
