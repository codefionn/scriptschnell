package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/config"
)

var (
	settingsItemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	settingsSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	settingsTitleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	settingsHelpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type settingsAction int

const (
	actionSelectProvider settingsAction = iota
	actionEditExaKey
	actionEditGoogleKey
	actionEditGoogleCX
	actionEditPerplexityKey
)

type settingsItem struct {
	title       string
	description string
	action      settingsAction
}

func (i settingsItem) FilterValue() string { return i.title }

type settingsItemDelegate struct{}

func (d settingsItemDelegate) Height() int                             { return 2 }
func (d settingsItemDelegate) Spacing() int                            { return 1 }
func (d settingsItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d settingsItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(settingsItem)
	if !ok {
		return
	}

	var str string
	if index == m.Index() {
		str = settingsSelectedItemStyle.Render(fmt.Sprintf("▸ %s", i.title))
	} else {
		str = settingsItemStyle.Render(fmt.Sprintf("  %s", i.title))
	}

	desc := settingsHelpStyle.Render(i.description)
	fmt.Fprintf(w, "%s\n%s", str, settingsItemStyle.Render(desc))
}

type SettingsMenuModel struct {
	list     list.Model
	cfg      *config.Config
	width    int
	height   int
	quitting bool

	// input mode for editing a single field
	inputMode bool
	input     textinput.Model
	fieldID   settingsAction

	// provider selection sub-menu
	providerMode bool
	providerList list.Model

	result string
}

func NewSettingsMenu(cfg *config.Config, width, height int) SettingsMenuModel {
	const defaultWidth = 80
	const defaultHeight = 20
	if width == 0 {
		width = defaultWidth
	}
	if height == 0 {
		height = defaultHeight
	}

	m := SettingsMenuModel{cfg: cfg, width: width, height: height}
	m.initLists()

	// input setup
	m.input = textinput.New()
	m.input.CharLimit = 200
	m.input.Width = 60
	m.input.Placeholder = "value"
	m.input.EchoMode = textinput.EchoNormal
	// For API keys, hide input; will be set per field

	return m
}

func (m *SettingsMenuModel) initLists() {
	items := []list.Item{
		settingsItem{title: m.providerTitle(), description: "Choose which provider to use for web search", action: actionSelectProvider},
		settingsItem{title: fmt.Sprintf("Exa API Key: %s", mask(m.cfg.Search.Exa.APIKey)), description: "Set or update your Exa API key", action: actionEditExaKey},
		settingsItem{title: fmt.Sprintf("Google PSE API Key: %s", mask(m.cfg.Search.GooglePSE.APIKey)), description: "Set Google Programmable Search Engine API key", action: actionEditGoogleKey},
		settingsItem{title: fmt.Sprintf("Google PSE CX: %s", emptyIf(m.cfg.Search.GooglePSE.CX, "(unset)")), description: "Set Google Programmable Search Engine CX (Search Engine ID)", action: actionEditGoogleCX},
		settingsItem{title: fmt.Sprintf("Perplexity API Key: %s", mask(m.cfg.Search.Perplexity.APIKey)), description: "Set or update your Perplexity API key", action: actionEditPerplexityKey},
	}

	l := list.New(items, settingsItemDelegate{}, m.width, m.height-4)
	l.Title = "Settings"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.Styles.Title = settingsTitleStyle
	m.list = l

	// Provider selection list
	provItems := []list.Item{
		settingsItem{title: providerDisplayName(""), description: "Disable external search", action: actionSelectProvider},
		settingsItem{title: providerDisplayName("exa"), description: "Use Exa AI Search API", action: actionSelectProvider},
		settingsItem{title: providerDisplayName("google_pse"), description: "Use Google Programmable Search Engine", action: actionSelectProvider},
		settingsItem{title: providerDisplayName("perplexity"), description: "Use Perplexity Search API", action: actionSelectProvider},
	}
	pl := list.New(provItems, settingsItemDelegate{}, m.width, m.height-4)
	pl.Title = "Select Search Provider"
	pl.SetShowStatusBar(false)
	pl.SetFilteringEnabled(false)
	pl.Styles.Title = settingsTitleStyle
	if idx := providerIndex(m.cfg.Search.Provider); idx >= 0 && idx < len(provItems) {
		pl.Select(idx)
	}
	m.providerList = pl
}

func (m SettingsMenuModel) providerTitle() string {
	current := m.cfg.Search.Provider
	if current == "" {
		return "Search Provider: (none)"
	}
	return fmt.Sprintf("Search Provider: %s", providerDisplayName(current))
}

func providerDisplayName(key string) string {
	switch key {
	case "exa":
		return "Exa"
	case "google_pse":
		return "Google PSE"
	case "perplexity":
		return "Perplexity"
	default:
		return "None"
	}
}

func providerIndex(key string) int {
	switch key {
	case "exa":
		return 1
	case "google_pse":
		return 2
	case "perplexity":
		return 3
	default:
		return 0
	}
}

func mask(s string) string {
	if s == "" {
		return "(unset)"
	}
	if len(s) <= 6 {
		return "******"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

func emptyIf(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (m SettingsMenuModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m SettingsMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.inputMode {
		return m.updateInputMode(msg)
	}
	if m.providerMode {
		return m.updateProviderMode(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 4)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			(&m).saveConfig()
			m.quitting = true
			return m, tea.Quit
		case "enter":
			i, ok := m.list.SelectedItem().(settingsItem)
			if !ok {
				return m, nil
			}
			switch i.action {
			case actionSelectProvider:
				m.result = ""
				m.providerMode = true
				return m, nil
			case actionEditExaKey:
				m.result = ""
				m.fieldID = actionEditExaKey
				m.inputMode = true
				m.input.Placeholder = "EXA_API_KEY"
				m.input.SetValue("")
				m.input.EchoMode = textinput.EchoPassword
				m.input.Focus()
				return m, nil
			case actionEditGoogleKey:
				m.result = ""
				m.fieldID = actionEditGoogleKey
				m.inputMode = true
				m.input.Placeholder = "GOOGLE_API_KEY"
				m.input.SetValue("")
				m.input.EchoMode = textinput.EchoPassword
				m.input.Focus()
				return m, nil
			case actionEditGoogleCX:
				m.result = ""
				m.fieldID = actionEditGoogleCX
				m.inputMode = true
				m.input.Placeholder = "GOOGLE_CX"
				m.input.SetValue("")
				m.input.EchoMode = textinput.EchoNormal
				m.input.Focus()
				return m, nil
			case actionEditPerplexityKey:
				m.result = ""
				m.fieldID = actionEditPerplexityKey
				m.inputMode = true
				m.input.Placeholder = "PERPLEXITY_API_KEY"
				m.input.SetValue("")
				m.input.EchoMode = textinput.EchoPassword
				m.input.Focus()
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m SettingsMenuModel) updateInputMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.inputMode = false
			return m, nil
		case "enter":
			v := m.input.Value()
			switch m.fieldID {
			case actionEditExaKey:
				m.cfg.Search.Exa.APIKey = v
			case actionEditGoogleKey:
				m.cfg.Search.GooglePSE.APIKey = v
			case actionEditGoogleCX:
				m.cfg.Search.GooglePSE.CX = v
			case actionEditPerplexityKey:
				m.cfg.Search.Perplexity.APIKey = v
			}
			m.saveConfig()
			// Refresh items to show masked values
			m.initLists()
			m.inputMode = false
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m SettingsMenuModel) updateProviderMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.providerMode = false
			return m, nil
		case "enter":
			i, ok := m.providerList.SelectedItem().(settingsItem)
			if !ok {
				return m, nil
			}
			// Determine provider from title
			title := i.title
			switch title {
			case providerDisplayName(""):
				m.cfg.Search.Provider = ""
			case providerDisplayName("exa"):
				m.cfg.Search.Provider = "exa"
			case providerDisplayName("google_pse"):
				m.cfg.Search.Provider = "google_pse"
			case providerDisplayName("perplexity"):
				m.cfg.Search.Provider = "perplexity"
			}
			m.saveConfig()
			m.providerMode = false
			m.initLists()
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.providerList.SetWidth(msg.Width)
		m.providerList.SetHeight(msg.Height - 4)
	}
	var cmd tea.Cmd
	m.providerList, cmd = m.providerList.Update(msg)
	return m, cmd
}

func (m SettingsMenuModel) View() string {
	if m.quitting {
		return ""
	}
	if m.inputMode {
		var s string
		s += settingsTitleStyle.Render("Edit Setting\n\n")
		s += m.input.View() + "\n\n"
		s += settingsHelpStyle.Render("Enter: Save • ESC: Cancel")
		return s
	}
	if m.providerMode {
		return m.providerList.View() + "\n" + settingsHelpStyle.Render("↑/↓: Navigate • Enter: Select • ESC: Back")
	}
	if m.result != "" {
		return m.list.View() + "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(m.result) + "\n"
	}
	help := settingsHelpStyle.Render("\n↑/↓: Navigate • Enter: Select • q/ESC: Close")
	return m.list.View() + help
}

// GetResult exposes the last status message after the menu closes
func (m SettingsMenuModel) GetResult() string {
	return m.result
}

func (m *SettingsMenuModel) saveConfig() {
	if err := m.cfg.Save(config.GetConfigPath()); err != nil {
		m.result = fmt.Sprintf("Failed to save: %v", err)
		return
	}
	m.result = "✓ Settings saved"
}
