package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	secretsTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	secretsLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("249"))
	secretsHelpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	secretsErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	secretsFormStyle  = lipgloss.NewStyle().Padding(1, 2)
)

// SecretsMenuModel collects a new encryption password from the user.
type SecretsMenuModel struct {
	inputs     []textinput.Model
	focused    int
	width      int
	height     int
	errMsg     string
	confirmed  bool
	password   string
	quitting   bool
	showHelper bool
}

// NewSecretsMenu creates a password form.
func NewSecretsMenu(width, height int) *SecretsMenuModel {
	const (
		defaultWidth  = 80
		defaultHeight = 16
	)
	if width == 0 {
		width = defaultWidth
	}
	if height == 0 {
		height = defaultHeight
	}

	model := &SecretsMenuModel{
		width:  width,
		height: height,
		inputs: make([]textinput.Model, 2),
	}

	model.inputs[0] = textinput.New()
	model.inputs[0].Placeholder = "Leave blank to reset password"
	model.inputs[0].EchoMode = textinput.EchoPassword
	model.inputs[0].Prompt = ""

	model.inputs[1] = textinput.New()
	model.inputs[1].Placeholder = "Re-enter password (blank to reset)"
	model.inputs[1].EchoMode = textinput.EchoPassword
	model.inputs[1].Prompt = ""

	model.setFocus(0)

	return model
}

// Init implements tea.Model.
func (m *SecretsMenuModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m *SecretsMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "tab", "shift+tab":
			if len(m.inputs) == 0 {
				return m, nil
			}
			delta := 1
			if msg.String() == "shift+tab" {
				delta = -1
			}
			m.focused = (m.focused + delta + len(m.inputs)) % len(m.inputs)
			m.setFocus(m.focused)
			return m, textinput.Blink
		case "enter":
			if m.focused < len(m.inputs)-1 {
				m.focused++
				m.setFocus(m.focused)
				return m, textinput.Blink
			}
			if err := m.validateInputs(); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.password = m.inputs[0].Value()
			m.confirmed = true
			m.quitting = true
			return m, tea.Quit
		}
	}

	var cmds []tea.Cmd
	for i := range m.inputs {
		cmd := m.updateInput(i, msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the menu.
func (m *SecretsMenuModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(secretsTitleStyle.Render("Set Encryption Password"))
	b.WriteString("\n")
	b.WriteString(secretsHelpStyle.Render("Password protects API keys at rest. Leave both fields blank to reset to the default (empty) password.\n"))
	b.WriteString("\n")

	labels := []string{"New Password", "Confirm Password"}
	for i, input := range m.inputs {
		b.WriteString(secretsLabelStyle.Render(labels[i]))
		b.WriteString("\n")
		b.WriteString(input.View())
		b.WriteString("\n\n")
	}

	if m.errMsg != "" {
		b.WriteString(secretsErrorStyle.Render(m.errMsg))
		b.WriteString("\n\n")
	}

	b.WriteString(secretsHelpStyle.Render("Tab: Next • Shift+Tab: Prev • Enter: Save • Esc: Cancel"))

	return secretsFormStyle.Render(b.String())
}

// Result returns the selected password and whether the user confirmed changes.
func (m *SecretsMenuModel) Result() (string, bool) {
	return m.password, m.confirmed
}

func (m *SecretsMenuModel) setFocus(index int) {
	for i := range m.inputs {
		if i == index {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

func (m *SecretsMenuModel) updateInput(index int, msg tea.Msg) tea.Cmd {
	if index != m.focused {
		return nil
	}
	var cmd tea.Cmd
	m.inputs[index], cmd = m.inputs[index].Update(msg)
	return cmd
}

func (m *SecretsMenuModel) validateInputs() error {
	first := m.inputs[0].Value()
	second := m.inputs[1].Value()
	if first != second {
		return fmt.Errorf("passwords do not match")
	}
	// No additional validation currently
	return nil
}
