package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	authDialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("170")).
			Padding(1, 2).
			Width(80)

	authTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170")).
			MarginBottom(1)

	authReasonStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true).
			MarginBottom(1)

	authParamStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			MarginLeft(2)

	authChoiceItemStyle         = lipgloss.NewStyle().PaddingLeft(2)
	authChoiceSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("170")).Bold(true)
)

type authChoiceItem struct {
	label string
	value string
	desc  string
}

func (i authChoiceItem) FilterValue() string { return i.label }

type authChoiceDelegate struct{}

func (d authChoiceDelegate) Height() int                             { return 2 }
func (d authChoiceDelegate) Spacing() int                            { return 1 }
func (d authChoiceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d authChoiceDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(authChoiceItem)
	if !ok {
		return
	}

	var title string
	if index == m.Index() {
		title = authChoiceSelectedItemStyle.Render(fmt.Sprintf("▸ %s", item.label))
	} else {
		title = authChoiceItemStyle.Render(fmt.Sprintf("  %s", item.label))
	}

	desc := authChoiceItemStyle.Render(roleDescStyle.Render(item.desc))
	fmt.Fprintf(w, "%s\n%s", title, desc)
}

// AuthorizationRequest contains the details of an authorization request
type AuthorizationRequest struct {
	ToolName   string
	Parameters map[string]interface{}
	Reason     string
}

// AuthorizationDialog presents a dialog to approve or deny a tool execution
type AuthorizationDialog struct {
	request  AuthorizationRequest
	list     list.Model
	choice   bool
	approved bool
	quitting bool
	width    int
	height   int
}

// AuthorizationApprovedMsg is sent when user approves the authorization
type AuthorizationApprovedMsg struct {
	Approved bool
}

// NewAuthorizationDialog constructs a dialog for authorization approval
func NewAuthorizationDialog(req AuthorizationRequest) AuthorizationDialog {
	items := []list.Item{
		authChoiceItem{
			label: "Approve",
			value: "approve",
			desc:  "Allow this tool to execute with the specified parameters.",
		},
		authChoiceItem{
			label: "Deny",
			value: "deny",
			desc:  "Prevent this tool from executing.",
		},
	}

	const width = 80
	const height = 20

	l := list.New(items, authChoiceDelegate{}, width, height-4)
	l.Title = "Authorization Required"
	l.Styles.Title = authTitleStyle
	l.DisableQuitKeybindings()
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	// Default to Deny for safety
	l.Select(1)

	return AuthorizationDialog{
		request: req,
		list:    l,
		width:   width,
		height:  height,
	}
}

func (m AuthorizationDialog) Init() tea.Cmd {
	return nil
}

func (m AuthorizationDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width-4, msg.Height-12)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			// ESC or Ctrl+C means deny
			m.choice = true
			m.approved = false
			m.quitting = true
			return m, tea.Batch(
				tea.Quit,
				func() tea.Msg { return AuthorizationApprovedMsg{Approved: false} },
			)

		case "enter":
			if item, ok := m.list.SelectedItem().(authChoiceItem); ok {
				m.choice = true
				m.approved = item.value == "approve"
				m.quitting = true
				return m, tea.Batch(
					tea.Quit,
					func() tea.Msg { return AuthorizationApprovedMsg{Approved: m.approved} },
				)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m AuthorizationDialog) View() string {
	var sb strings.Builder

	// Title
	sb.WriteString(authTitleStyle.Render("⚠️  Authorization Required"))
	sb.WriteString("\n\n")

	// Tool name
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Tool: "))
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(m.request.ToolName))
	sb.WriteString("\n\n")

	// Parameters
	if len(m.request.Parameters) > 0 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Parameters:"))
		sb.WriteString("\n")
		for k, v := range m.request.Parameters {
			paramLine := fmt.Sprintf("  %s: %v", k, v)
			// Truncate very long values
			if len(paramLine) > 100 {
				paramLine = paramLine[:97] + "..."
			}
			sb.WriteString(authParamStyle.Render(paramLine))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Reason
	if m.request.Reason != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Reason:"))
		sb.WriteString("\n")
		sb.WriteString(authReasonStyle.Render(m.request.Reason))
		sb.WriteString("\n\n")
	}

	// Choice list
	sb.WriteString(m.list.View())
	sb.WriteString("\n")

	// Help
	help := roleDescStyle.Render("↑/↓: Navigate • Enter: Confirm • ESC: Deny and close")
	sb.WriteString(help)

	return authDialogStyle.Render(sb.String())
}

// GetApproved returns whether the user approved the authorization
func (m AuthorizationDialog) GetApproved() bool {
	return m.approved
}

// HasChoice returns whether the user made a choice
func (m AuthorizationDialog) HasChoice() bool {
	return m.choice
}
