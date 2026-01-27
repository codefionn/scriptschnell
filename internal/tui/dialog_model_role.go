package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	roleItemStyle         = lipgloss.NewStyle().PaddingLeft(2)
	roleSelectedItemStyle = lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("170"))
	roleDescStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	roleHeaderStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
)

type roleItem struct {
	label string
	value string
	desc  string
}

func (i roleItem) FilterValue() string { return i.label }

type roleItemDelegate struct{}

func (d roleItemDelegate) Height() int                             { return 2 }
func (d roleItemDelegate) Spacing() int                            { return 1 }
func (d roleItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d roleItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(roleItem)
	if !ok {
		return
	}

	var title string
	if index == m.Index() {
		title = roleSelectedItemStyle.Render(fmt.Sprintf("▸ %s", item.label))
	} else {
		title = roleItemStyle.Render(fmt.Sprintf("  %s", item.label))
	}

	desc := roleItemStyle.Render(roleDescStyle.Render(item.desc))
	fmt.Fprintf(w, "%s\n%s", title, desc)
}

// ModelRoleDialog presents a dialog to choose how to register a model.
type ModelRoleDialog struct {
	list     list.Model
	choice   string
	quitting bool
}

type modelRoleSelectedMsg struct {
	role string
}

// NewModelRoleDialog constructs a dialog with orchestration and summarization choices.
func NewModelRoleDialog(modelName, defaultRole string) ModelRoleDialog {
	items := []list.Item{
		roleItem{
			label: fmt.Sprintf("Set %s as orchestration model", modelName),
			value: "orchestration",
			desc:  "Use for primary conversations, reasoning, and tool calls.",
		},
		roleItem{
			label: fmt.Sprintf("Set %s as summarization model", modelName),
			value: "summarize",
			desc:  "Use for fast file summaries and quick context refreshes.",
		},
		roleItem{
			label: fmt.Sprintf("Set %s as planning model", modelName),
			value: "planning",
			desc:  "Use for creating detailed plans and asking clarification questions.",
		},
		roleItem{
			label: fmt.Sprintf("Set %s as safety model", modelName),
			value: "safety",
			desc:  "Use for safety-critical tasks, falls back to summarize model.",
		},
	}

	const width = 80
	const height = 12

	l := list.New(items, roleItemDelegate{}, width, height-4)
	l.Title = "Choose model role"
	l.Styles.Title = roleHeaderStyle
	l.DisableQuitKeybindings()
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	switch defaultRole {
	case "summarize":
		l.Select(1)
	case "planning":
		l.Select(2)
	default:
		l.Select(0)
	}

	return ModelRoleDialog{list: l}
}

func (m ModelRoleDialog) Init() tea.Cmd {
	return nil
}

func (m ModelRoleDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			if item, ok := m.list.SelectedItem().(roleItem); ok {
				m.choice = item.value
				m.quitting = true
				return m, tea.Batch(
					tea.Quit,
					func() tea.Msg { return modelRoleSelectedMsg{role: item.value} },
				)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ModelRoleDialog) View() string {
	help := roleDescStyle.Render("\n↑/↓: Navigate • Enter: Confirm • esc: Cancel")
	return m.list.View() + help
}

// GetChoice returns the selected role, or "" if none chosen.
func (m ModelRoleDialog) GetChoice() string {
	return m.choice
}
