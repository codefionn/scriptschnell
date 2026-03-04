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
	actionConfigureActiveProvider settingsAction = iota
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
	_, _ = fmt.Fprintf(w, "%s\n%s", str, settingsItemStyle.Render(desc))
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
	inputStep int
	inputFlow []searchProviderInputField

	// provider selection sub-menu
	providerMode    bool
	providerList    list.Model
	pendingProvider string

	result string
}

type searchProviderInputField struct {
	provider     string
	fieldKey     string
	label        string
	placeholder  string
	passwordMode bool
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
		settingsItem{
			title:       m.providerTitle(),
			description: m.providerDescription(),
			action:      actionConfigureActiveProvider,
		},
	}

	l := list.New(items, settingsItemDelegate{}, m.width, m.height-4)
	l.Title = "Settings"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.Styles.Title = settingsTitleStyle
	m.list = l

	// Provider selection list
	provItems := []list.Item{
		settingsItem{title: providerDisplayName(""), description: "Disable external search", action: actionConfigureActiveProvider},
		settingsItem{title: providerDisplayName("exa"), description: "Use Exa AI Search API", action: actionConfigureActiveProvider},
		settingsItem{title: providerDisplayName("google_pse"), description: "Use Google Programmable Search Engine", action: actionConfigureActiveProvider},
		settingsItem{title: providerDisplayName("perplexity"), description: "Use Perplexity Search API", action: actionConfigureActiveProvider},
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
	return fmt.Sprintf("Search Provider: %s (%s)", providerDisplayName(current), m.providerCredentialStatus(current))
}

func (m SettingsMenuModel) providerDescription() string {
	return "Choose provider first, then update credentials and make it active"
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
			case actionConfigureActiveProvider:
				m.result = ""
				m.providerMode = true
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
			m.inputFlow = nil
			m.inputStep = 0
			m.pendingProvider = ""
			return m, nil
		case "enter":
			v := m.input.Value()

			if len(m.inputFlow) == 0 || m.inputStep >= len(m.inputFlow) {
				m.inputMode = false
				return m, nil
			}

			field := m.inputFlow[m.inputStep]
			trimmed := v
			switch field.provider {
			case "exa":
				if field.fieldKey == "api_key" && trimmed != "" {
					m.cfg.Search.Exa.APIKey = trimmed
				}
			case "google_pse":
				switch field.fieldKey {
				case "api_key":
					if trimmed != "" {
						m.cfg.Search.GooglePSE.APIKey = trimmed
					}
				case "cx":
					if trimmed != "" {
						m.cfg.Search.GooglePSE.CX = trimmed
					}
				}
			case "perplexity":
				if field.fieldKey == "api_key" && trimmed != "" {
					m.cfg.Search.Perplexity.APIKey = trimmed
				}
			}

			m.inputStep++
			if m.inputStep >= len(m.inputFlow) {
				m.cfg.Search.Provider = m.pendingProvider
				m.saveConfig()
				m.initLists()
				m.inputMode = false
				m.inputFlow = nil
				m.inputStep = 0
				m.pendingProvider = ""
				return m, nil
			}

			m.prepareInputForCurrentStep()
			return m, textinput.Blink
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
				m.pendingProvider = ""
				m.inputFlow = nil
				m.inputStep = 0
				m.inputMode = false
				m.providerMode = false
				m.saveConfig()
				m.initLists()
				return m, nil
			case providerDisplayName("exa"):
				m.pendingProvider = "exa"
			case providerDisplayName("google_pse"):
				m.pendingProvider = "google_pse"
			case providerDisplayName("perplexity"):
				m.pendingProvider = "perplexity"
			}
			m.providerMode = false
			m.startProviderCredentialsFlow()
			return m, textinput.Blink
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
		s += settingsTitleStyle.Render("Configure Search Provider\n\n")
		if current := m.currentInputField(); current != nil {
			step := fmt.Sprintf("Step %d/%d: %s\n", m.inputStep+1, len(m.inputFlow), current.label)
			s += settingsHelpStyle.Render(step)
		}
		s += m.input.View() + "\n\n"
		s += settingsHelpStyle.Render("Enter: Next/Save • ESC: Cancel (empty keeps existing value)")
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

func (m *SettingsMenuModel) startProviderCredentialsFlow() {
	if m.pendingProvider == "" {
		m.inputMode = false
		m.inputFlow = nil
		m.inputStep = 0
		return
	}

	m.result = ""
	m.inputFlow = providerInputFlow(m.pendingProvider)
	m.inputStep = 0
	m.inputMode = true
	m.prepareInputForCurrentStep()
}

func (m *SettingsMenuModel) prepareInputForCurrentStep() {
	current := m.currentInputField()
	if current == nil {
		m.inputMode = false
		return
	}

	m.input.Placeholder = current.placeholder
	m.input.SetValue("")
	if current.passwordMode {
		m.input.EchoMode = textinput.EchoPassword
	} else {
		m.input.EchoMode = textinput.EchoNormal
	}
	m.input.Focus()
}

func (m SettingsMenuModel) currentInputField() *searchProviderInputField {
	if len(m.inputFlow) == 0 || m.inputStep < 0 || m.inputStep >= len(m.inputFlow) {
		return nil
	}
	field := m.inputFlow[m.inputStep]
	return &field
}

func providerInputFlow(provider string) []searchProviderInputField {
	switch provider {
	case "exa":
		return []searchProviderInputField{
			{
				provider:     "exa",
				fieldKey:     "api_key",
				label:        "Exa API Key",
				placeholder:  "EXA_API_KEY",
				passwordMode: true,
			},
		}
	case "google_pse":
		return []searchProviderInputField{
			{
				provider:     "google_pse",
				fieldKey:     "api_key",
				label:        "Google PSE API Key",
				placeholder:  "GOOGLE_API_KEY",
				passwordMode: true,
			},
			{
				provider:     "google_pse",
				fieldKey:     "cx",
				label:        "Google PSE CX (Search Engine ID)",
				placeholder:  "GOOGLE_CX",
				passwordMode: false,
			},
		}
	case "perplexity":
		return []searchProviderInputField{
			{
				provider:     "perplexity",
				fieldKey:     "api_key",
				label:        "Perplexity API Key",
				placeholder:  "PERPLEXITY_API_KEY",
				passwordMode: true,
			},
		}
	default:
		return nil
	}
}

func (m SettingsMenuModel) providerCredentialStatus(provider string) string {
	switch provider {
	case "exa":
		return credentialStatus(m.cfg.Search.Exa.APIKey)
	case "google_pse":
		if m.cfg.Search.GooglePSE.APIKey != "" && m.cfg.Search.GooglePSE.CX != "" {
			return "configured"
		}
		if m.cfg.Search.GooglePSE.APIKey == "" && m.cfg.Search.GooglePSE.CX == "" {
			return "unset"
		}
		return "partial"
	case "perplexity":
		return credentialStatus(m.cfg.Search.Perplexity.APIKey)
	default:
		return "unset"
	}
}

func credentialStatus(value string) string {
	if value == "" {
		return "unset"
	}
	return "configured"
}
