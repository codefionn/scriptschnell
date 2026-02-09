package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DirectoryAccessRequest contains the details of a directory access request
type DirectoryAccessRequest struct {
	Path        string
	AccessLevel string // "read" or "readwrite"
	Description string
}

// DirectoryAccessChoiceMsg is sent when user makes a choice
type DirectoryAccessChoiceMsg struct {
	Path    string
	Choice  string // "session", "workspace", or "deny"
	Request DirectoryAccessRequest
}

// dirAccessChoiceItem represents a choice in the list
type dirAccessChoiceItem struct {
	name        string
	description string
	value       string
}

func (i dirAccessChoiceItem) FilterValue() string {
	return i.name
}

func (i dirAccessChoiceItem) Title() string {
	return i.name
}

func (i dirAccessChoiceItem) Description() string {
	return i.description
}

// dirAccessChoiceDelegate renders choice items
type dirAccessChoiceDelegate struct{}

func (d dirAccessChoiceDelegate) Height() int                             { return 2 }
func (d dirAccessChoiceDelegate) Spacing() int                            { return 1 }
func (d dirAccessChoiceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d dirAccessChoiceDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(dirAccessChoiceItem)
	if !ok {
		return
	}

	var title string
	if index == m.Index() {
		title = authChoiceSelectedItemStyle.Render(fmt.Sprintf("> %s", i.name))
	} else {
		title = authChoiceItemStyle.Render(fmt.Sprintf("  %s", i.name))
	}

	desc := authChoiceItemStyle.Render(roleDescStyle.Render(i.description))
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, desc)
}

var (
	dirAccessTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("62")).
				Padding(0, 1)

	dirAccessPathStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86"))

	dirAccessDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	dirAccessBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62")).
				Padding(1, 2)
)

const dirAccessDialogDefaultWidth = 70

// DirectoryAccessDialog presents a dialog to approve or deny directory access
type DirectoryAccessDialog struct {
	request  DirectoryAccessRequest
	list     list.Model
	choice   string // "session", "workspace", or "deny"
	quitting bool
	width    int
	height   int
}

// NewDirectoryAccessDialog constructs a dialog for directory access authorization
func NewDirectoryAccessDialog(req DirectoryAccessRequest) DirectoryAccessDialog {
	items := []list.Item{
		dirAccessChoiceItem{
			name:        "✓ Approve for this session",
			description: "Allow access only during this session",
			value:       "session",
		},
		dirAccessChoiceItem{
			name:        "✓ Approve for this workspace",
			description: "Save approval for future sessions in this workspace",
			value:       "workspace",
		},
		dirAccessChoiceItem{
			name:        "✗ Deny access",
			description: "Block access to this directory",
			value:       "deny",
		},
	}

	listWidth, listHeight := dirAccessListSize(dirAccessDialogDefaultWidth)

	l := list.New(items, dirAccessChoiceDelegate{}, listWidth, listHeight)
	l.Title = "Directory Access Request"
	l.Styles.Title = dirAccessTitleStyle
	l.DisableQuitKeybindings()
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	return DirectoryAccessDialog{
		request: req,
		list:    l,
		width:   dirAccessDialogDefaultWidth,
	}
}

func (m DirectoryAccessDialog) dialogWidth() int {
	if m.width > 0 {
		return min(dirAccessDialogDefaultWidth, m.width)
	}
	return dirAccessDialogDefaultWidth
}

func dirAccessListSize(width int) (int, int) {
	listWidth := max(10, width-dirAccessDialogDefaultWidth)
	return listWidth, 5
}

func (m DirectoryAccessDialog) listSize() (int, int) {
	return dirAccessListSize(m.dialogWidth())
}

func (m DirectoryAccessDialog) Init() tea.Cmd {
	return nil
}

func (m DirectoryAccessDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listWidth, listHeight := m.listSize()
		m.list.SetSize(listWidth, listHeight)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			// ESC or Ctrl+C means deny
			m.choice = "deny"
			m.quitting = true
			return m, func() tea.Msg {
				return DirectoryAccessChoiceMsg{
					Path:    m.request.Path,
					Choice:  "deny",
					Request: m.request,
				}
			}

		case "enter":
			if item, ok := m.list.SelectedItem().(dirAccessChoiceItem); ok {
				m.choice = item.value
				m.quitting = true
				return m, func() tea.Msg {
					return DirectoryAccessChoiceMsg{
						Path:    m.request.Path,
						Choice:  item.value,
						Request: m.request,
					}
				}
			}

		case "s", "S":
			// Quick approve for session
			m.choice = "session"
			m.quitting = true
			return m, func() tea.Msg {
				return DirectoryAccessChoiceMsg{
					Path:    m.request.Path,
					Choice:  "session",
					Request: m.request,
				}
			}

		case "w", "W":
			// Quick approve for workspace
			m.choice = "workspace"
			m.quitting = true
			return m, func() tea.Msg {
				return DirectoryAccessChoiceMsg{
					Path:    m.request.Path,
					Choice:  "workspace",
					Request: m.request,
				}
			}

		case "d", "D":
			// Quick deny
			m.choice = "deny"
			m.quitting = true
			return m, func() tea.Msg {
				return DirectoryAccessChoiceMsg{
					Path:    m.request.Path,
					Choice:  "deny",
					Request: m.request,
				}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m DirectoryAccessDialog) View() string {
	var sb strings.Builder

	// Title
	sb.WriteString(dirAccessTitleStyle.Render("📁 Directory Access Request"))
	sb.WriteString("\n\n")

	// Path
	sb.WriteString("Path: ")
	sb.WriteString(dirAccessPathStyle.Render(m.request.Path))
	sb.WriteString("\n")

	// Access level
	accessDesc := "Read-only access"
	if m.request.AccessLevel == "readwrite" {
		accessDesc = "Read and write access"
	}
	sb.WriteString("Access: ")
	sb.WriteString(dirAccessPathStyle.Render(accessDesc))
	sb.WriteString("\n")

	// Description if provided
	if m.request.Description != "" {
		sb.WriteString("\nReason: ")
		sb.WriteString(dirAccessDescStyle.Render(m.request.Description))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// List of choices
	sb.WriteString(m.list.View())

	sb.WriteString("\n\n")
	sb.WriteString(dirAccessDescStyle.Render("Shortcuts: [S] Session  [W] Workspace  [D] Deny  [Esc] Cancel"))

	return dirAccessBoxStyle.Render(sb.String())
}

// GetChoice returns the user's choice
func (m DirectoryAccessDialog) GetChoice() string {
	return m.choice
}

// HasChoice returns whether the user made a choice
func (m DirectoryAccessDialog) HasChoice() bool {
	return m.choice != ""
}

// GetRequest returns the original request
func (m DirectoryAccessDialog) GetRequest() DirectoryAccessRequest {
	return m.request
}
