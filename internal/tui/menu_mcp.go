package tui

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/config"
)

var (
	mcpItemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	mcpSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	mcpTitleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	mcpHelpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type mcpAction int

const (
	mcpActionAddOpenAPI mcpAction = iota
	mcpActionAddCommand
	mcpActionAddOpenAI
	mcpActionToggle
)

type mcpItem struct {
	title       string
	description string
	action      mcpAction
	serverName  string
	serverType  string
}

func (i mcpItem) FilterValue() string { return i.title }

type mcpItemDelegate struct{}

func (d mcpItemDelegate) Height() int                             { return 2 }
func (d mcpItemDelegate) Spacing() int                            { return 1 }
func (d mcpItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d mcpItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(mcpItem)
	if !ok {
		return
	}

	var str string
	if index == m.Index() {
		str = mcpSelectedItemStyle.Render(fmt.Sprintf("▸ %s", i.title))
	} else {
		str = mcpItemStyle.Render(fmt.Sprintf("  %s", i.title))
	}

	desc := mcpHelpStyle.Render(i.description)
	fmt.Fprintf(w, "%s\n%s", str, mcpItemStyle.Render(desc))
}

type mcpInputField struct {
	label       string
	placeholder string
	optional    bool
}

// MCPPersistFunc is called after a mutation to save config, refresh tools, and optionally validate a server.
type MCPPersistFunc func(serverName string, validate bool) (string, error)

// MCPMenuModel provides an interactive menu for managing MCP servers.
type MCPMenuModel struct {
	list      list.Model
	cfg       *config.Config
	width     int
	height    int
	quitting  bool
	status    string
	persistFn MCPPersistFunc
	result    string

	// input mode state
	inputMode    bool
	inputs       []textinput.Model
	inputFields  []mcpInputField
	inputIndex   int
	activeAction mcpAction
}

func NewMCPMenu(cfg *config.Config, width, height int, persist MCPPersistFunc) *MCPMenuModel {
	const defaultWidth = 80
	const defaultHeight = 20
	if width == 0 {
		width = defaultWidth
	}
	if height == 0 {
		height = defaultHeight
	}

	model := &MCPMenuModel{
		cfg:       cfg,
		width:     width,
		height:    height,
		persistFn: persist,
	}

	model.list = list.New(nil, mcpItemDelegate{}, width, height-4)
	model.list.Title = "MCP Server Management"
	model.list.SetShowStatusBar(true)
	model.list.SetFilteringEnabled(false)
	model.list.Styles.Title = mcpTitleStyle

	model.refreshList()
	return model
}

func (m MCPMenuModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *MCPMenuModel) refreshList() {
	items := []list.Item{
		mcpItem{
			title:       "Add OpenAPI MCP Server",
			description: "Load an OpenAPI document and expose its operations as tools",
			action:      mcpActionAddOpenAPI,
		},
		mcpItem{
			title:       "Add Command MCP Server",
			description: "Wrap a local command line program as a tool",
			action:      mcpActionAddCommand,
		},
		mcpItem{
			title:       "Add OpenAI MCP Server",
			description: "Call an OpenAI or compatible model through a dedicated tool",
			action:      mcpActionAddOpenAI,
		},
	}

	if m.cfg.MCP.Servers == nil {
		m.cfg.MCP.Servers = make(map[string]*config.MCPServerConfig)
	}

	names := make([]string, 0, len(m.cfg.MCP.Servers))
	for name := range m.cfg.MCP.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		server := m.cfg.MCP.Servers[name]
		status := "enabled"
		if server.Disabled {
			status = "disabled"
		}
		desc := fmt.Sprintf("Type: %s • Status: %s", server.Type, status)
		if server.Description != "" {
			desc = fmt.Sprintf("%s • %s", desc, server.Description)
		}

		items = append(items, mcpItem{
			title:       fmt.Sprintf("%s (%s)", name, status),
			description: desc,
			action:      mcpActionToggle,
			serverName:  name,
			serverType:  server.Type,
		})
	}

	m.list.SetItems(items)
}

func (m *MCPMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.inputMode {
		return m.updateInputMode(msg)
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
			m.quitting = true
			return m, tea.Quit
		case "enter":
			return m.handleEnter()
		case "d", "backspace", "delete":
			return m.handleDelete()
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *MCPMenuModel) View() string {
	if m.quitting {
		return ""
	}

	if m.inputMode {
		b := acquireBuilder()
		b.WriteString(mcpTitleStyle.Render("Configure MCP Server"))
		b.WriteString("\n\n")
		for i, field := range m.inputFields {
			label := field.label
			if field.optional {
				label += " (optional)"
			}
			b.WriteString(fmt.Sprintf("%s:\n", label))
			b.WriteString(m.inputs[i].View())
			b.WriteString("\n\n")
		}
		help := mcpHelpStyle.Render("Tab/Shift+Tab: Next field • Enter: Confirm • ESC: Cancel")
		b.WriteString(help)
		return builderString(b)
	}

	help := mcpHelpStyle.Render("\n↑/↓: Navigate • Enter: Select/Toggle • d: Delete • q/ESC: Close")
	status := ""
	if m.status != "" {
		status = "\n" + mcpHelpStyle.Render(m.status)
	}
	return m.list.View() + help + status
}

func (m *MCPMenuModel) GetResult() string {
	return m.result
}

// --- list handlers ---

func (m *MCPMenuModel) handleEnter() (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(mcpItem)
	if !ok {
		return m, nil
	}

	switch item.action {
	case mcpActionAddOpenAPI:
		focusCmd := m.startAddOpenAPI()
		return m, tea.Batch(textinput.Blink, focusCmd)
	case mcpActionAddCommand:
		focusCmd := m.startAddCommand()
		return m, tea.Batch(textinput.Blink, focusCmd)
	case mcpActionAddOpenAI:
		focusCmd := m.startAddOpenAI()
		return m, tea.Batch(textinput.Blink, focusCmd)
	case mcpActionToggle:
		m.toggleServer(item.serverName)
		return m, nil
	default:
		return m, nil
	}
}

func (m *MCPMenuModel) handleDelete() (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(mcpItem)
	if !ok || item.action != mcpActionToggle {
		return m, nil
	}

	server := m.cfg.MCP.Servers[item.serverName]
	delete(m.cfg.MCP.Servers, item.serverName)
	message, err := m.persist(item.serverName, false)
	if err != nil {
		if server != nil {
			m.cfg.MCP.Servers[item.serverName] = server
		}
		m.status = fmt.Sprintf("Error removing server: %v", err)
	} else {
		if message != "" {
			m.status = message
		} else {
			m.status = fmt.Sprintf("Removed MCP server '%s'", item.serverName)
		}
		m.result = m.status
	}
	m.refreshList()
	return m, nil
}

func (m *MCPMenuModel) toggleServer(name string) {
	server, ok := m.cfg.MCP.Servers[name]
	if !ok {
		return
	}

	server.Disabled = !server.Disabled
	message, err := m.persist(name, false)
	if err != nil {
		server.Disabled = !server.Disabled // revert
		m.status = fmt.Sprintf("Error updating server: %v", err)
		return
	}
	if message != "" {
		m.status = message
	} else {
		state := "Enabled"
		if server.Disabled {
			state = "Disabled"
		}
		m.status = fmt.Sprintf("%s MCP server '%s'", state, name)
	}
	m.result = m.status
	m.refreshList()
}

// --- input mode handlers ---

func (m *MCPMenuModel) updateInputMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.inputMode = false
			m.inputs = nil
			m.inputFields = nil
			m.status = "Cancelled"
			return m, nil
		case "tab", "shift+tab":
			if msg.String() == "tab" {
				return m, m.focusInput((m.inputIndex + 1) % len(m.inputs))
			}
			return m, m.focusInput((m.inputIndex - 1 + len(m.inputs)) % len(m.inputs))
		case "enter":
			if m.inputIndex < len(m.inputs)-1 {
				return m, m.focusInput(m.inputIndex + 1)
			}
			if err := m.finalizeAdd(); err != nil {
				m.status = fmt.Sprintf("Error: %v", err)
				return m, nil
			}
			m.inputMode = false
			m.inputs = nil
			m.inputFields = nil
			m.refreshList()
			return m, nil
		}
	}

	if len(m.inputs) == 0 {
		return m, nil
	}

	var cmd tea.Cmd
	m.inputs[m.inputIndex], cmd = m.inputs[m.inputIndex].Update(msg)
	return m, cmd
}

func (m *MCPMenuModel) focusInput(index int) tea.Cmd {
	if len(m.inputs) == 0 {
		return nil
	}

	cmds := make([]tea.Cmd, 0, len(m.inputs))
	for i := range m.inputs {
		ti := m.inputs[i]
		if i == index {
			if cmd := ti.Focus(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		} else {
			ti.Blur()
		}
		m.inputs[i] = ti
	}
	m.inputIndex = index
	return tea.Batch(cmds...)
}

func (m *MCPMenuModel) startAddOpenAPI() tea.Cmd {
	m.inputMode = true
	m.activeAction = mcpActionAddOpenAPI
	m.inputFields = []mcpInputField{
		{label: "Server name", placeholder: "Unique identifier (e.g., weather)", optional: false},
		{label: "OpenAPI spec path or URL", placeholder: "./specs/api.yaml or https://...", optional: false},
		{label: "Service URL", placeholder: "https://api.example.com", optional: false},
		{label: "Bearer token", placeholder: "Optional static token", optional: true},
		{label: "Bearer token env var", placeholder: "e.g., API_TOKEN", optional: true},
	}
	return m.prepareInputs()
}

func (m *MCPMenuModel) startAddCommand() tea.Cmd {
	m.inputMode = true
	m.activeAction = mcpActionAddCommand
	m.inputFields = []mcpInputField{
		{label: "Server name", placeholder: "Unique identifier (e.g., formatter)", optional: false},
		{label: "Command", placeholder: "Executable and args (e.g., gofmt -w)", optional: false},
		{label: "Working directory", placeholder: "Optional", optional: true},
		{label: "Timeout seconds", placeholder: "e.g., 60 (optional)", optional: true},
	}
	return m.prepareInputs()
}

func (m *MCPMenuModel) startAddOpenAI() tea.Cmd {
	m.inputMode = true
	m.activeAction = mcpActionAddOpenAI
	m.inputFields = []mcpInputField{
		{label: "Server name", placeholder: "Unique identifier (e.g., reasoning)", optional: false},
		{label: "Model ID", placeholder: "Leave blank to use current orchestration model", optional: true},
		{label: "API Key", placeholder: "(optional, leave blank to rely on env)", optional: true},
		{label: "API Key Env Var", placeholder: "e.g., OPENAI_API_KEY (optional)", optional: true},
		{label: "Base URL", placeholder: "(optional for OpenAI-compatible)", optional: true},
	}
	return m.prepareInputs()
}

func (m *MCPMenuModel) prepareInputs() tea.Cmd {
	m.inputs = make([]textinput.Model, len(m.inputFields))
	for i, field := range m.inputFields {
		ti := textinput.New()
		ti.Placeholder = field.placeholder
		ti.CharLimit = 256
		ti.Width = 60
		if strings.Contains(strings.ToLower(field.label), "key") {
			ti.EchoMode = textinput.EchoPassword
		}
		m.inputs[i] = ti
	}
	return m.focusInput(0)
}

func (m *MCPMenuModel) finalizeAdd() error {
	values := make([]string, len(m.inputs))
	for i, input := range m.inputs {
		values[i] = strings.TrimSpace(input.Value())
		if values[i] == "" && !m.inputFields[i].optional {
			return fmt.Errorf("%s is required", m.inputFields[i].label)
		}
	}

	if m.cfg.MCP.Servers == nil {
		m.cfg.MCP.Servers = make(map[string]*config.MCPServerConfig)
	}

	name := values[0]
	if _, exists := m.cfg.MCP.Servers[name]; exists {
		return fmt.Errorf("server with name '%s' already exists", name)
	}

	switch m.activeAction {
	case mcpActionAddOpenAPI:
		m.cfg.MCP.Servers[name] = &config.MCPServerConfig{
			Type:        "openapi",
			Description: "",
			OpenAPI: &config.MCPOpenAPIConfig{
				SpecPath:        values[1],
				URL:             values[2],
				AuthBearerToken: values[3],
				AuthBearerEnv:   values[4],
			},
		}
		m.status = fmt.Sprintf("Added OpenAPI MCP server '%s'", name)
	case mcpActionAddCommand:
		cmdArgs := strings.Fields(values[1])
		if len(cmdArgs) == 0 {
			return fmt.Errorf("command must not be empty")
		}
		timeout := 60
		if values[3] != "" {
			if parsed, err := strconv.Atoi(values[3]); err == nil && parsed > 0 {
				timeout = parsed
			} else {
				return fmt.Errorf("invalid timeout value")
			}
		}
		m.cfg.MCP.Servers[name] = &config.MCPServerConfig{
			Type:        "command",
			Description: "",
			Command: &config.MCPCommandConfig{
				Exec:           cmdArgs,
				WorkingDir:     values[2],
				TimeoutSeconds: timeout,
			},
		}
		m.status = fmt.Sprintf("Added command MCP server '%s'", name)
	case mcpActionAddOpenAI:
		m.cfg.MCP.Servers[name] = &config.MCPServerConfig{
			Type:        "openai",
			Description: "",
			OpenAI: &config.MCPOpenAIConfig{
				Model:        values[1],
				APIKey:       values[2],
				APIKeyEnvVar: values[3],
				BaseURL:      values[4],
			},
		}
		m.status = fmt.Sprintf("Added OpenAI MCP server '%s'", name)
	default:
		return fmt.Errorf("unsupported action")
	}

	message, err := m.persist(name, true)
	if err != nil {
		delete(m.cfg.MCP.Servers, name)
		return err
	}

	if message != "" {
		m.status = message
	}
	m.result = m.status
	return nil
}

func (m *MCPMenuModel) persist(name string, validate bool) (string, error) {
	if m.persistFn == nil {
		return "", nil
	}
	return m.persistFn(name, validate)
}
