package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/statcode-ai/statcode-ai/internal/provider"
)

var (
	providerItemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	providerSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	providerTitleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	providerHelpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type providerAction int

const (
	actionListProviders providerAction = iota
	actionAddProvider
)

// providerSuccessMsg is sent when a provider is successfully added
type providerSuccessMsg struct {
	providerName string
	modelCount   int
}

type providerItem struct {
	title        string
	description  string
	action       providerAction
	providerType string
	displayName  string
}

func (i providerItem) FilterValue() string { return i.title }

type providerItemDelegate struct{}

func (d providerItemDelegate) Height() int                             { return 2 }
func (d providerItemDelegate) Spacing() int                            { return 1 }
func (d providerItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d providerItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(providerItem)
	if !ok {
		return
	}

	var str string
	if index == m.Index() {
		str = providerSelectedItemStyle.Render(fmt.Sprintf("▸ %s", i.title))
	} else {
		str = providerItemStyle.Render(fmt.Sprintf("  %s", i.title))
	}

	desc := providerHelpStyle.Render(i.description)
	fmt.Fprintf(w, "%s\n%s", str, providerItemStyle.Render(desc))
}

type ProviderMenuModel struct {
	list        list.Model
	providerMgr *provider.Manager
	width       int
	height      int
	quitting    bool
	inputMode   bool
	inputs      []textinput.Model
	focusIndex  int
	addingType  string // provider key (openai, anthropic, google, openrouter, mistral, cerebras, groq, openai-compatible)
	addingLabel string // human-friendly provider label
	needsURL    bool   // true if provider needs a base URL (openai-compatible)
	result      string
}

func NewProviderMenu(providerMgr *provider.Manager, width, height int) ProviderMenuModel {
	items := []list.Item{
		providerItem{
			title:        "Add OpenAI Provider",
			description:  "Configure OpenAI with API key",
			action:       actionAddProvider,
			providerType: "openai",
			displayName:  "OpenAI",
		},
		providerItem{
			title:        "Add Anthropic Provider",
			description:  "Configure Anthropic (Claude) with API key",
			action:       actionAddProvider,
			providerType: "anthropic",
			displayName:  "Anthropic",
		},
		providerItem{
			title:        "Add Google Provider",
			description:  "Configure Google Generative Language (Gemini) with API key",
			action:       actionAddProvider,
			providerType: "google",
			displayName:  "Google",
		},
		providerItem{
			title:        "Add OpenRouter Provider",
			description:  "Configure OpenRouter with API key",
			action:       actionAddProvider,
			providerType: "openrouter",
			displayName:  "OpenRouter",
		},
		providerItem{
			title:        "Add Mistral Provider",
			description:  "Configure Mistral AI with API key",
			action:       actionAddProvider,
			providerType: "mistral",
			displayName:  "Mistral",
		},
		providerItem{
			title:        "Add Cerebras Provider",
			description:  "Configure Cerebras Cloud AI with API key",
			action:       actionAddProvider,
			providerType: "cerebras",
			displayName:  "Cerebras",
		},
		providerItem{
			title:        "Add Groq Provider",
			description:  "Configure Groq's low-latency models with API key",
			action:       actionAddProvider,
			providerType: "groq",
			displayName:  "Groq",
		},
		providerItem{
			title:        "Add OpenAI-Compatible Provider",
			description:  "Configure any OpenAI-compatible API (LM Studio, LocalAI, vLLM, etc.)",
			action:       actionAddProvider,
			providerType: "openai-compatible",
			displayName:  "OpenAI-Compatible",
		},
	}

	const defaultWidth = 80
	const defaultHeight = 20

	if width == 0 {
		width = defaultWidth
	}
	if height == 0 {
		height = defaultHeight
	}

	l := list.New(items, providerItemDelegate{}, width, height-4)
	l.Title = "Provider Management"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.Styles.Title = providerTitleStyle

	// Setup inputs for adding provider
	inputs := make([]textinput.Model, 2)

	// Input 0: Base URL (for openai-compatible provider)
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "Base URL (e.g., http://localhost:1234/v1)"
	inputs[0].CharLimit = 200
	inputs[0].Width = 60

	// Input 1: API Key
	inputs[1] = textinput.New()
	inputs[1].Placeholder = "API Key (leave empty for local servers)"
	inputs[1].CharLimit = 200
	inputs[1].Width = 60
	inputs[1].EchoMode = textinput.EchoPassword

	return ProviderMenuModel{
		list:        l,
		providerMgr: providerMgr,
		width:       width,
		height:      height,
		inputs:      inputs,
	}
}

func (m ProviderMenuModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m ProviderMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle input mode
	if m.inputMode {
		return m.updateInputMode(msg)
	}

	switch msg := msg.(type) {
	case providerSuccessMsg:
		// Handle successful provider addition
		m.result = fmt.Sprintf("✓ %s provider added with %d models from API", msg.providerName, msg.modelCount)
		m.inputMode = false
		m.inputs[0].SetValue("")
		return m, tea.Quit

	case ErrMsg:
		// Handle errors from provider operations
		m.result = fmt.Sprintf("❌ Error: %v", error(msg))
		m.inputMode = false
		m.inputs[0].SetValue("")
		return m, nil

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
			i, ok := m.list.SelectedItem().(providerItem)
			if !ok {
				return m, nil
			}

			switch i.action {
			case actionAddProvider:
				if i.providerType == "" {
					return m, nil
				}
				m.addingType = i.providerType
				m.addingLabel = i.displayName
				m.inputMode = true
				m.focusIndex = 0

				// OpenAI-compatible provider needs base URL
				if i.providerType == "openai-compatible" {
					m.needsURL = true
					m.inputs[0].Focus()
					m.inputs[1].Blur()
				} else {
					m.needsURL = false
					m.inputs[0].Blur()
					m.inputs[1].Focus()
				}
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ProviderMenuModel) updateInputMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case providerSuccessMsg:
		// Handle successful provider addition
		m.result = fmt.Sprintf("✓ %s provider added with %d models from API", msg.providerName, msg.modelCount)
		m.inputMode = false
		m.inputs[0].SetValue("")
		m.inputs[1].SetValue("")
		return m, tea.Quit

	case ErrMsg:
		// Handle errors from provider operations
		m.result = fmt.Sprintf("❌ Error: %v", error(msg))
		m.inputMode = false
		m.inputs[0].SetValue("")
		m.inputs[1].SetValue("")
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.inputMode = false
			m.inputs[0].SetValue("")
			m.inputs[1].SetValue("")
			return m, nil

		case "tab", "shift+tab", "up", "down":
			// Navigate between inputs (only for openai-compatible)
			if !m.needsURL {
				return m, nil
			}

			if msg.String() == "tab" || msg.String() == "down" {
				m.focusIndex++
			} else {
				m.focusIndex--
			}

			if m.focusIndex > 1 {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = 1
			}

			cmds := make([]tea.Cmd, 2)
			for i := 0; i < 2; i++ {
				if i == m.focusIndex {
					cmds[i] = m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}

			return m, tea.Batch(cmds...)

		case "enter":
			if m.needsURL {
				// OpenAI-compatible provider: need base URL
				baseURL := m.inputs[0].Value()
				apiKey := strings.TrimSpace(m.inputs[1].Value())

				if baseURL == "" {
					return m, nil // Base URL is required
				}

				return m, m.addProviderWithURL(m.addingType, m.addingLabel, apiKey, baseURL)
			} else {
				// Other providers: only need API key
				apiKey := strings.TrimSpace(m.inputs[1].Value())
				return m, m.addProvider(m.addingType, m.addingLabel, apiKey)
			}
		}
	}

	var cmd tea.Cmd
	if m.needsURL {
		// Update the focused input
		m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	} else {
		// Only update API key input for non-openai-compatible providers
		m.inputs[1], cmd = m.inputs[1].Update(msg)
	}
	return m, cmd
}

func (m ProviderMenuModel) View() string {
	if m.quitting {
		return ""
	}

	if m.inputMode {
		displayName := m.addingLabel
		if displayName == "" {
			displayName = m.addingType
		}

		var s string
		s += providerTitleStyle.Render(fmt.Sprintf("Add %s Provider\n\n", displayName))

		if m.needsURL {
			// OpenAI-compatible provider: show both base URL and API key inputs
			s += "Enter base URL:\n\n"
			s += m.inputs[0].View() + "\n\n"
			hints := provider.EnvVarHints(m.addingType)
			if len(hints) > 0 {
				s += fmt.Sprintf("Enter API key (optional for local servers, or leave empty to use %s):\n\n", formatEnvVarHints(hints))
			} else {
				s += "Enter API key (optional for local servers):\n\n"
			}
			s += m.inputs[1].View() + "\n\n"
			s += providerHelpStyle.Render("Tab: Next field • Enter: Submit • ESC: Cancel")
		} else {
			// Other providers: only show API key input
			hints := provider.EnvVarHints(m.addingType)
			if len(hints) > 0 {
				s += fmt.Sprintf("Enter your API key (leave empty to use %s):\n\n", formatEnvVarHints(hints))
			} else {
				s += "Enter your API key:\n\n"
			}
			s += m.inputs[1].View() + "\n\n"
			s += providerHelpStyle.Render("Enter: Submit • ESC: Cancel")
		}
		return s
	}

	if m.result != "" {
		return m.list.View() + "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(m.result) + "\n"
	}

	help := providerHelpStyle.Render("\n↑/↓: Navigate • Enter: Select • q/ESC: Cancel")
	return m.list.View() + help
}

func (m ProviderMenuModel) addProvider(providerType, providerLabel, apiKey string) tea.Cmd {
	ptype := providerType
	label := providerLabel
	return func() tea.Msg {
		// Use context.Background() since we don't have access to the main context
		ctx := context.Background()

		// Add provider and fetch models from API
		if err := m.providerMgr.AddProviderWithAPIListing(ctx, ptype, apiKey); err != nil {
			return ErrMsg(fmt.Errorf("failed to add provider: %w", err))
		}

		// Get the number of models fetched
		p, ok := m.providerMgr.GetProvider(ptype)
		modelCount := 0
		if ok {
			modelCount = len(p.Models)
		}

		display := label
		if display == "" {
			display = ptype
		}

		return providerSuccessMsg{
			providerName: display,
			modelCount:   modelCount,
		}
	}
}

func (m ProviderMenuModel) addProviderWithURL(providerType, providerLabel, apiKey, baseURL string) tea.Cmd {
	ptype := providerType
	label := providerLabel
	return func() tea.Msg {
		// Use context.Background() since we don't have access to the main context
		ctx := context.Background()

		// Add provider with base URL and fetch models from API
		if err := m.providerMgr.AddProviderWithAPIListingAndBaseURL(ctx, ptype, apiKey, baseURL); err != nil {
			return ErrMsg(fmt.Errorf("failed to add provider: %w", err))
		}

		// Get the number of models fetched
		p, ok := m.providerMgr.GetProvider(ptype)
		modelCount := 0
		if ok {
			modelCount = len(p.Models)
		}

		display := label
		if display == "" {
			display = ptype
		}

		return providerSuccessMsg{
			providerName: display,
			modelCount:   modelCount,
		}
	}
}

func formatEnvVarHints(vars []string) string {
	switch len(vars) {
	case 0:
		return ""
	case 1:
		return vars[0]
	case 2:
		return fmt.Sprintf("%s or %s", vars[0], vars[1])
	default:
		return fmt.Sprintf("%s, or %s", strings.Join(vars[:len(vars)-1], ", "), vars[len(vars)-1])
	}
}
