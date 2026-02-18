package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/provider"
)

const (
	defaultBaseURLPlaceholder = "Base URL (e.g., http://localhost:1234/v1)"
	defaultAPIKeyPlaceholder  = "API Key (leave empty for local servers)"
)

var (
	providerItemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	providerSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	providerTitleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	providerHelpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	providerErrorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	providerFormLabelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

type providerAction int

const (
	actionAddProvider providerAction = iota
	actionEditProvider
)

type providerFormMode int

const (
	formModeNone providerFormMode = iota
	formModeAdd
	formModeEdit
)

const (
	inputIDBaseURL    = "base_url"
	inputIDAPIKey     = "api_key"
	inputIDRequests   = "requests_per_minute"
	inputIDIntervalMS = "min_interval_ms"
	inputIDTokens     = "tokens_per_minute"
)

// providerSuccessMsg is sent when a provider is successfully added
type providerSuccessMsg struct {
	providerName string
	modelCount   int
}

type providerSettingsSavedMsg struct {
	providerName     string
	updatedCreds     bool
	rateLimitSummary string
}

type providerItem struct {
	title        string
	description  string
	action       providerAction
	providerType string
	displayName  string
}

func (i providerItem) FilterValue() string { return i.title }

type formInput struct {
	id    string
	label string
	input textinput.Model
}

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
	list              list.Model
	providerMgr       *provider.Manager
	width             int
	height            int
	quitting          bool
	inputMode         bool
	formMode          providerFormMode
	formInputs        []formInput
	focusIndex        int
	addingType        string // provider key (openai, anthropic, google, openrouter, mistral, cerebras, groq, kimi, openai-compatible)
	addingLabel       string // human-friendly provider label
	editProvider      string
	editProviderLabel string
	inputError        string
	result            string
}

func NewProviderMenu(providerMgr *provider.Manager, width, height int) ProviderMenuModel {
	items := buildProviderMenuItems(providerMgr)

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

	return ProviderMenuModel{
		list:        l,
		providerMgr: providerMgr,
		width:       width,
		height:      height,
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
		m.resetInputState()
		m.refreshItems()
		return m, nil

	case providerSettingsSavedMsg:
		var statusParts []string
		if msg.updatedCreds {
			statusParts = append(statusParts, "credentials")
		}
		if msg.rateLimitSummary != "" {
			statusParts = append(statusParts, fmt.Sprintf("rate limit %s", msg.rateLimitSummary))
		}
		if len(statusParts) == 0 {
			statusParts = append(statusParts, "no changes")
		}
		m.result = fmt.Sprintf("✓ Updated %s (%s)", msg.providerName, strings.Join(statusParts, ", "))
		m.inputMode = false
		m.resetInputState()
		m.refreshItems()
		return m, nil

	case ErrMsg:
		// Handle errors from provider operations
		m.result = fmt.Sprintf("❌ Error: %v", error(msg))
		m.inputMode = false
		m.resetInputState()
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
				m.enterAddProvider(i)
				return m, nil
			case actionEditProvider:
				m.enterEditProvider(i)
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
		m.resetInputState()
		m.refreshItems()
		return m, nil

	case providerSettingsSavedMsg:
		var statusParts []string
		if msg.updatedCreds {
			statusParts = append(statusParts, "credentials")
		}
		if msg.rateLimitSummary != "" {
			statusParts = append(statusParts, fmt.Sprintf("rate limit %s", msg.rateLimitSummary))
		}
		if len(statusParts) == 0 {
			statusParts = append(statusParts, "no changes")
		}
		m.result = fmt.Sprintf("✓ Updated %s (%s)", msg.providerName, strings.Join(statusParts, ", "))
		m.inputMode = false
		m.resetInputState()
		m.refreshItems()
		return m, nil

	case ErrMsg:
		// Handle errors from provider operations
		m.result = fmt.Sprintf("❌ Error: %v", error(msg))
		m.inputMode = false
		m.resetInputState()
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		if key != "enter" {
			m.inputError = ""
		}
		switch msg.String() {
		case "ctrl+c", "esc":
			m.inputMode = false
			m.resetInputState()
			return m, nil

		case "tab", "shift+tab", "up", "down":
			if len(m.formInputs) <= 1 {
				return m, nil
			}

			if msg.String() == "tab" || msg.String() == "down" {
				m.focusIndex++
			} else {
				m.focusIndex--
			}

			if m.focusIndex >= len(m.formInputs) {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = len(m.formInputs) - 1
			}

			m.focusInput(m.focusIndex)
			return m, nil

		case "enter":
			switch m.formMode {
			case formModeAdd:
				if m.addingType == "openai-compatible" {
					baseURL := strings.TrimSpace(m.getInputValue(inputIDBaseURL))
					apiKey := m.getInputValue(inputIDAPIKey)
					if baseURL == "" {
						m.inputError = "Base URL is required"
						return m, nil
					}
					m.inputError = ""
					return m, m.addProviderWithURL(m.addingType, m.addingLabel, apiKey, baseURL)
				}
				apiKey := m.getInputValue(inputIDAPIKey)
				m.inputError = ""
				return m, m.addProvider(m.addingType, m.addingLabel, apiKey)

			case formModeEdit:
				if m.editProvider == "" {
					return m, nil
				}
				rpm, err := parseOptionalPositiveInt(m.getInputValue(inputIDRequests), "requests per minute")
				if err != nil {
					m.inputError = err.Error()
					return m, nil
				}
				interval, err := parseOptionalPositiveInt(m.getInputValue(inputIDIntervalMS), "min interval (ms)")
				if err != nil {
					m.inputError = err.Error()
					return m, nil
				}
				tokensPerMinute, err := parseOptionalPositiveInt(m.getInputValue(inputIDTokens), "tokens per minute")
				if err != nil {
					m.inputError = err.Error()
					return m, nil
				}
				var cfg *provider.RateLimitConfig
				if rpm > 0 || interval > 0 || tokensPerMinute > 0 {
					cfg = &provider.RateLimitConfig{
						RequestsPerMinute: rpm,
						MinIntervalMillis: interval,
						TokensPerMinute:   tokensPerMinute,
					}
				}
				apiKey := m.getInputValue(inputIDAPIKey)
				baseURL := m.getInputValue(inputIDBaseURL)
				m.inputError = ""
				return m, m.saveProviderSettings(m.editProvider, m.editProviderLabel, apiKey, baseURL, cfg)
			default:
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	if len(m.formInputs) == 0 {
		return m, nil
	}

	if len(m.formInputs) == 1 {
		m.formInputs[0].input, cmd = m.formInputs[0].input.Update(msg)
	} else {
		m.formInputs[m.focusIndex].input, cmd = m.formInputs[m.focusIndex].input.Update(msg)
	}
	return m, cmd
}

func (m ProviderMenuModel) View() string {
	if m.quitting {
		return ""
	}

	if m.inputMode {
		sb := acquireBuilder()
		switch m.formMode {
		case formModeAdd:
			displayName := m.addingLabel
			if displayName == "" {
				displayName = friendlyProviderName(m.addingType)
			}
			sb.WriteString(providerTitleStyle.Render(fmt.Sprintf("Add %s Provider\n\n", displayName)))
		case formModeEdit:
			label := m.editProviderLabel
			if label == "" {
				label = friendlyProviderName(m.editProvider)
			}
			sb.WriteString(providerTitleStyle.Render(fmt.Sprintf("Configure %s\n\n", label)))
		default:
			releaseBuilder(sb)
			return ""
		}

		for i, fi := range m.formInputs {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(providerFormLabelStyle.Render(fi.label))
			sb.WriteString("\n")
			sb.WriteString(fi.input.View())
		}

		var help string
		if len(m.formInputs) > 1 {
			help = "Tab: Next field • Enter: Save • ESC: Cancel"
		} else {
			help = "Enter: Save • ESC: Cancel"
		}
		sb.WriteString(providerHelpStyle.Render(help))

		if m.inputError != "" {
			sb.WriteString("\n\n")
			sb.WriteString(providerErrorStyle.Render(m.inputError))
		}
		return builderString(sb)
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

		// Auto-configure default model if this is the first provider
		if err := m.providerMgr.ConfigureDefaultModelForProvider(ptype); err != nil {
			return ErrMsg(fmt.Errorf("failed to configure default model: %w", err))
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

		// Auto-configure default model if this is the first provider
		if err := m.providerMgr.ConfigureDefaultModelForProvider(ptype); err != nil {
			return ErrMsg(fmt.Errorf("failed to configure default model: %w", err))
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

func (m ProviderMenuModel) saveProviderSettings(providerName, providerLabel, apiKey, baseURL string, cfg *provider.RateLimitConfig) tea.Cmd {
	if m.providerMgr == nil {
		return func() tea.Msg {
			return ErrMsg(fmt.Errorf("provider manager not initialized"))
		}
	}
	name := providerName
	label := providerLabel
	var cfgCopy *provider.RateLimitConfig
	if cfg != nil {
		c := *cfg
		cfgCopy = &c
	}
	apiKey = strings.TrimSpace(apiKey)
	baseURL = strings.TrimSpace(baseURL)
	var prevCfg *provider.RateLimitConfig
	if p, ok := m.providerMgr.GetProvider(name); ok && p.RateLimit != nil {
		copyPrev := *p.RateLimit
		prevCfg = &copyPrev
	}
	rateLimitSummary := ""

	return func() tea.Msg {
		if apiKey != "" || baseURL != "" {
			if err := m.providerMgr.UpdateProviderConnection(name, apiKey, baseURL); err != nil {
				return ErrMsg(fmt.Errorf("failed to update API settings: %w", err))
			}
		}
		if err := m.providerMgr.UpdateProviderRateLimit(name, cfgCopy); err != nil {
			return ErrMsg(fmt.Errorf("failed to update rate limit: %w", err))
		}
		rateChanged := !rateLimitEqual(prevCfg, cfgCopy)
		if rateChanged {
			rateLimitSummary = formatRateLimitSummary(cfgCopy)
		}
		return providerSettingsSavedMsg{
			providerName:     label,
			updatedCreds:     apiKey != "" || baseURL != "",
			rateLimitSummary: rateLimitSummary,
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

func (m *ProviderMenuModel) enterAddProvider(item providerItem) {
	if item.providerType == "" {
		return
	}
	m.inputMode = true
	m.formMode = formModeAdd
	m.addingType = item.providerType
	m.addingLabel = item.displayName
	m.editProvider = ""
	m.editProviderLabel = ""
	m.inputError = ""
	m.formInputs = nil

	if item.providerType == "openai-compatible" {
		baseLabel := "Enter base URL:"
		base := newFormInput(inputIDBaseURL, baseLabel, defaultBaseURLPlaceholder, textinput.EchoNormal, "")
		apiLabel := "Enter API key (optional for local servers):"
		hints := provider.EnvVarHints(item.providerType)
		if len(hints) > 0 {
			apiLabel = fmt.Sprintf("Enter API key (optional, or leave empty to use %s):", formatEnvVarHints(hints))
		}
		api := newFormInput(inputIDAPIKey, apiLabel, defaultAPIKeyPlaceholder, textinput.EchoPassword, "")
		m.formInputs = []formInput{base, api}
	} else {
		label := "Enter your API key:"
		hints := provider.EnvVarHints(item.providerType)
		if len(hints) > 0 {
			label = fmt.Sprintf("Enter your API key (leave empty to use %s):", formatEnvVarHints(hints))
		}
		api := newFormInput(inputIDAPIKey, label, defaultAPIKeyPlaceholder, textinput.EchoPassword, "")
		m.formInputs = []formInput{api}
	}

	m.focusIndex = 0
	m.focusInput(m.focusIndex)
}

func (m *ProviderMenuModel) enterEditProvider(item providerItem) {
	if item.providerType == "" {
		return
	}
	providerName := item.providerType
	display := item.displayName
	if display == "" {
		display = friendlyProviderName(providerName)
	}

	var cfg *provider.RateLimitConfig
	var baseURL string
	if m.providerMgr != nil {
		if p, ok := m.providerMgr.GetProvider(providerName); ok {
			cfg = p.RateLimit
			baseURL = p.BaseURL
		}
	}
	m.enterEditMode(providerName, display, item.providerType, baseURL, cfg)
}

func (m *ProviderMenuModel) enterEditMode(providerName, displayName, providerType, baseURL string, cfg *provider.RateLimitConfig) {
	m.inputMode = true
	m.formMode = formModeEdit
	m.addingType = ""
	m.addingLabel = ""
	m.editProvider = providerName
	m.editProviderLabel = displayName
	m.inputError = ""
	m.formInputs = make([]formInput, 0, 4)

	if providerType == "openai-compatible" {
		base := newFormInput(
			inputIDBaseURL,
			"Base URL (leave blank to keep current):",
			defaultBaseURLPlaceholder,
			textinput.EchoNormal,
			baseURL,
		)
		m.formInputs = append(m.formInputs, base)
	}

	api := newFormInput(
		inputIDAPIKey,
		"API key (leave blank to keep current):",
		defaultAPIKeyPlaceholder,
		textinput.EchoPassword,
		"",
	)
	m.formInputs = append(m.formInputs, api)

	reqVal := ""
	if cfg != nil && cfg.RequestsPerMinute > 0 {
		reqVal = strconv.Itoa(cfg.RequestsPerMinute)
	}
	intervalVal := ""
	if cfg != nil && cfg.MinIntervalMillis > 0 {
		intervalVal = strconv.Itoa(cfg.MinIntervalMillis)
	}

	reqInput := newFormInput(
		inputIDRequests,
		"Requests per minute (blank = unlimited):",
		"",
		textinput.EchoNormal,
		reqVal,
	)
	intInput := newFormInput(
		inputIDIntervalMS,
		"Minimum interval in ms between requests (blank = unlimited):",
		"",
		textinput.EchoNormal,
		intervalVal,
	)
	tokensVal := ""
	if cfg != nil && cfg.TokensPerMinute > 0 {
		tokensVal = strconv.Itoa(cfg.TokensPerMinute)
	}
	tokensInput := newFormInput(
		inputIDTokens,
		"Tokens per minute (blank = unlimited):",
		"",
		textinput.EchoNormal,
		tokensVal,
	)

	m.formInputs = append(m.formInputs, reqInput, intInput, tokensInput)

	m.focusIndex = 0
	m.focusInput(m.focusIndex)
}

func (m *ProviderMenuModel) resetInputState() {
	m.addingType = ""
	m.addingLabel = ""
	m.editProvider = ""
	m.editProviderLabel = ""
	m.focusIndex = 0
	m.inputError = ""
	m.formMode = formModeNone
	m.formInputs = nil
}

func (m *ProviderMenuModel) refreshItems() {
	m.list.SetItems(buildProviderMenuItems(m.providerMgr))
}

func buildProviderMenuItems(mgr *provider.Manager) []list.Item {
	items := make([]list.Item, 0)
	if mgr != nil {
		providers := mgr.ListProviders()
		sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
		for _, p := range providers {
			label := friendlyProviderName(p.Name)
			items = append(items, providerItem{
				title:        fmt.Sprintf("Configure %s", label),
				description:  fmt.Sprintf("%s • %s", apiKeyStatusDescription(p), rateLimitDescription(p)),
				action:       actionEditProvider,
				providerType: p.Name,
				displayName:  label,
			})
		}
	}

	addItems := []providerItem{
		{
			title:        "Add OpenAI Provider",
			description:  "Configure OpenAI with API key",
			action:       actionAddProvider,
			providerType: "openai",
			displayName:  "OpenAI",
		},
		{
			title:        "Add Anthropic Provider",
			description:  "Configure Anthropic (Claude) with API key",
			action:       actionAddProvider,
			providerType: "anthropic",
			displayName:  "Anthropic",
		},
		{
			title:        "Add Google Provider",
			description:  "Configure Google Generative Language (Gemini) with API key",
			action:       actionAddProvider,
			providerType: "google",
			displayName:  "Google",
		},
		{
			title:        "Add OpenRouter Provider",
			description:  "Configure OpenRouter with API key",
			action:       actionAddProvider,
			providerType: "openrouter",
			displayName:  "OpenRouter",
		},
		{
			title:        "Add Mistral Provider",
			description:  "Configure Mistral AI with API key",
			action:       actionAddProvider,
			providerType: "mistral",
			displayName:  "Mistral",
		},
		{
			title:        "Add Cerebras Provider",
			description:  "Configure Cerebras Cloud AI with API key",
			action:       actionAddProvider,
			providerType: "cerebras",
			displayName:  "Cerebras",
		},
		{
			title:        "Add Groq Provider",
			description:  "Configure Groq's low-latency models with API key",
			action:       actionAddProvider,
			providerType: "groq",
			displayName:  "Groq",
		},
		{
			title:        "Add Kimi Provider",
			description:  "Configure Kimi (Moonshot AI) with API key",
			action:       actionAddProvider,
			providerType: "kimi",
			displayName:  "Kimi",
		},
		{
			title:        "Add OpenAI-Compatible Provider",
			description:  "Configure any OpenAI-compatible API (LM Studio, LocalAI, vLLM, etc.)",
			action:       actionAddProvider,
			providerType: "openai-compatible",
			displayName:  "OpenAI-Compatible",
		},
	}

	for _, it := range addItems {
		items = append(items, it)
	}
	return items
}

func friendlyProviderName(name string) string {
	switch name {
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "google":
		return "Google"
	case "openrouter":
		return "OpenRouter"
	case "mistral":
		return "Mistral"
	case "cerebras":
		return "Cerebras"
	case "groq":
		return "Groq"
	case "kimi":
		return "Kimi"
	case "ollama":
		return "Ollama"
	case "openai-compatible":
		return "OpenAI-Compatible"
	default:
		if name == "" {
			return "Provider"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func apiKeyStatusDescription(p *provider.Provider) string {
	if p == nil {
		return "API key: (unknown)"
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return "API key: unset"
	}
	return "API key: configured"
}

func rateLimitDescription(p *provider.Provider) string {
	if p == nil || p.RateLimit == nil || (p.RateLimit.RequestsPerMinute <= 0 && p.RateLimit.MinIntervalMillis <= 0 && p.RateLimit.TokensPerMinute <= 0) {
		return "Current: Unlimited (provider default)"
	}

	parts := make([]string, 0, 2)
	if p.RateLimit.RequestsPerMinute > 0 {
		parts = append(parts, fmt.Sprintf("%d req/min", p.RateLimit.RequestsPerMinute))
	}
	if p.RateLimit.MinIntervalMillis > 0 {
		parts = append(parts, fmt.Sprintf("%d ms gap", p.RateLimit.MinIntervalMillis))
	}
	if p.RateLimit.TokensPerMinute > 0 {
		parts = append(parts, fmt.Sprintf("%d tokens/min", p.RateLimit.TokensPerMinute))
	}
	return "Current: " + strings.Join(parts, ", ")
}

func formatRateLimitSummary(cfg *provider.RateLimitConfig) string {
	if cfg == nil || (cfg.RequestsPerMinute <= 0 && cfg.MinIntervalMillis <= 0 && cfg.TokensPerMinute <= 0) {
		return "unlimited"
	}

	parts := make([]string, 0, 2)
	if cfg.RequestsPerMinute > 0 {
		parts = append(parts, fmt.Sprintf("%d req/min", cfg.RequestsPerMinute))
	}
	if cfg.MinIntervalMillis > 0 {
		parts = append(parts, fmt.Sprintf("%d ms gap", cfg.MinIntervalMillis))
	}
	if cfg.TokensPerMinute > 0 {
		parts = append(parts, fmt.Sprintf("%d tokens/min", cfg.TokensPerMinute))
	}
	return strings.Join(parts, ", ")
}

func rateLimitEqual(a, b *provider.RateLimitConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.RequestsPerMinute == b.RequestsPerMinute &&
		a.MinIntervalMillis == b.MinIntervalMillis &&
		a.TokensPerMinute == b.TokensPerMinute
}

func parseOptionalPositiveInt(value, fieldName string) (int, error) {
	if value == "" {
		return 0, nil
	}

	num, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number", fieldName)
	}
	if num < 0 {
		return 0, fmt.Errorf("%s cannot be negative", fieldName)
	}
	return num, nil
}

func newFormInput(id, label, placeholder string, echo textinput.EchoMode, value string) formInput {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 200
	ti.Width = 60
	ti.EchoMode = echo
	ti.SetValue(value)
	return formInput{
		id:    id,
		label: label,
		input: ti,
	}
}

func (m *ProviderMenuModel) focusInput(idx int) {
	for i := range m.formInputs {
		if i == idx {
			m.formInputs[i].input.Focus()
		} else {
			m.formInputs[i].input.Blur()
		}
	}
}

func (m *ProviderMenuModel) getInputValue(id string) string {
	for _, fi := range m.formInputs {
		if fi.id == id {
			return strings.TrimSpace(fi.input.Value())
		}
	}
	return ""
}
