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
	domainDialogStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("214")).
				Padding(1, 2)

	domainTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("214")).
				MarginBottom(1)

	domainReasonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Italic(true).
				MarginBottom(1)

	domainChoiceItemStyle         = lipgloss.NewStyle().PaddingLeft(2)
	domainChoiceSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("214")).Bold(true)
)

const (
	domainDialogDefaultWidth  = 80
	domainDialogDefaultHeight = 24
	domainDialogListPadding   = 4
	domainDialogHeightPadding = 14
)

type domainChoiceItem struct {
	label string
	value string
	desc  string
}

func (i domainChoiceItem) FilterValue() string { return i.label }

type domainChoiceDelegate struct{}

func (d domainChoiceDelegate) Height() int                             { return 2 }
func (d domainChoiceDelegate) Spacing() int                            { return 1 }
func (d domainChoiceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d domainChoiceDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(domainChoiceItem)
	if !ok {
		return
	}

	var title string
	if index == m.Index() {
		title = domainChoiceSelectedItemStyle.Render(fmt.Sprintf("▸ %s", item.label))
	} else {
		title = domainChoiceItemStyle.Render(fmt.Sprintf("  %s", item.label))
	}

	desc := domainChoiceItemStyle.Render(roleDescStyle.Render(item.desc))
	fmt.Fprintf(w, "%s\n%s", title, desc)
}

// DomainAuthorizationRequest contains the details of a domain authorization request
type DomainAuthorizationRequest struct {
	Domain string
	Code   string // The code snippet that uses this domain
}

// DomainAuthorizationDialog presents a dialog to approve or deny domain access
type DomainAuthorizationDialog struct {
	request  DomainAuthorizationRequest
	list     list.Model
	choice   string // "approve", "permanent", "deny", or ""
	quitting bool
	width    int
	height   int
}

// DomainAuthorizationChoiceMsg is sent when user makes a choice
type DomainAuthorizationChoiceMsg struct {
	Domain    string
	Choice    string // "approve" (session), "permanent", or "deny"
	Approved  bool
	Permanent bool
}

func (m DomainAuthorizationDialog) dialogWidth() int {
	if m.width > 0 {
		return min(domainDialogDefaultWidth, m.width)
	}
	return domainDialogDefaultWidth
}

func (m DomainAuthorizationDialog) listSize() (int, int) {
	width := max(10, m.dialogWidth()-domainDialogListPadding)

	height := m.height
	if height <= 0 {
		height = domainDialogDefaultHeight
	}

	return width, max(5, height-domainDialogHeightPadding)
}

// NewDomainAuthorizationDialog constructs a dialog for domain authorization
func NewDomainAuthorizationDialog(req DomainAuthorizationRequest) DomainAuthorizationDialog {
	items := []list.Item{
		domainChoiceItem{
			label: "Yes (this session)",
			value: "approve",
			desc:  "Allow access to this domain for the current session only.",
		},
		domainChoiceItem{
			label: "Yes (permanently)",
			value: "permanent",
			desc:  "Allow access to this domain permanently (saved to config).",
		},
		domainChoiceItem{
			label: "No",
			value: "deny",
			desc:  "Deny access to this domain.",
		},
	}

	dialog := DomainAuthorizationDialog{
		request: req,
		width:   domainDialogDefaultWidth,
		height:  domainDialogDefaultHeight,
	}

	listWidth, listHeight := dialog.listSize()

	l := list.New(items, domainChoiceDelegate{}, listWidth, listHeight)
	l.Title = "Network Access Authorization"
	l.Styles.Title = domainTitleStyle
	l.DisableQuitKeybindings()
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	// Default to Deny for safety
	l.Select(2)

	dialog.list = l

	return dialog
}

func (m DomainAuthorizationDialog) Init() tea.Cmd {
	return nil
}

func (m DomainAuthorizationDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listWidth, listHeight := m.listSize()
		m.list.SetSize(listWidth, listHeight)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			// ESC or Ctrl+C means deny
			m.choice = "deny"
			m.quitting = true
			return m, tea.Batch(
				tea.Quit,
				func() tea.Msg {
					return DomainAuthorizationChoiceMsg{
						Domain:    m.request.Domain,
						Choice:    "deny",
						Approved:  false,
						Permanent: false,
					}
				},
			)

		case "enter":
			if item, ok := m.list.SelectedItem().(domainChoiceItem); ok {
				m.choice = item.value
				m.quitting = true

				approved := item.value == "approve" || item.value == "permanent"
				permanent := item.value == "permanent"

				return m, tea.Batch(
					tea.Quit,
					func() tea.Msg {
						return DomainAuthorizationChoiceMsg{
							Domain:    m.request.Domain,
							Choice:    item.value,
							Approved:  approved,
							Permanent: permanent,
						}
					},
				)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m DomainAuthorizationDialog) View() string {
	var sb strings.Builder

	// Title
	sb.WriteString(domainTitleStyle.Render("🌐 Network Access Authorization"))
	sb.WriteString("\n\n")

	// Domain
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Domain: "))
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(m.request.Domain))
	sb.WriteString("\n\n")

	// Reason
	sb.WriteString(domainReasonStyle.Render("The sandbox code is attempting to access this domain over the network."))
	sb.WriteString("\n\n")

	// Code snippet (truncated if too long)
	if m.request.Code != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Code snippet:"))
		sb.WriteString("\n")
		codeSnippet := m.request.Code
		if len(codeSnippet) > 300 {
			codeSnippet = codeSnippet[:297] + "..."
		}
		codeStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("242")).
			Background(lipgloss.Color("235")).
			Padding(0, 1).
			MarginLeft(2)
		sb.WriteString(codeStyle.Render(codeSnippet))
		sb.WriteString("\n\n")
	}

	// Question
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Allow network access to this domain?"))
	sb.WriteString("\n\n")

	// Choice list
	sb.WriteString(m.list.View())
	sb.WriteString("\n")

	// Help
	help := roleDescStyle.Render("↑/↓: Navigate • Enter: Confirm • ESC: Deny and close")
	sb.WriteString(help)

	dialogWidth := m.dialogWidth()
	return domainDialogStyle.Width(dialogWidth).Render(sb.String())
}

// GetChoice returns the user's choice
func (m DomainAuthorizationDialog) GetChoice() string {
	return m.choice
}

// HasChoice returns whether the user made a choice
func (m DomainAuthorizationDialog) HasChoice() bool {
	return m.choice != ""
}
