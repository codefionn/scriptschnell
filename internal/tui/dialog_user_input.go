package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

// UserInputDialog presents a single question to the user for text input
type UserInputDialog struct {
	question string
	textarea textarea.Model
	quitting bool
}

// NewUserInputDialog creates a dialog for single question user input
func NewUserInputDialog(question string) UserInputDialog {
	ta := textarea.New()
	ta.Placeholder = "Type your answer here..."
	ta.Focus()
	ta.Prompt = "│ "
	ta.CharLimit = 1000
	ta.SetWidth(60)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false) // Single line input

	return UserInputDialog{
		question: question,
		textarea: ta,
		quitting: false,
	}
}

func (d UserInputDialog) Init() tea.Cmd {
	return textarea.Blink
}

func (d UserInputDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			d.quitting = true
			return d, tea.Quit
		case tea.KeyEnter:
			d.quitting = true
			return d, tea.Quit
		}
	}

	d.textarea, cmd = d.textarea.Update(msg)
	return d, cmd
}

func (d UserInputDialog) View() string {
	if d.quitting {
		return ""
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Render(
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("62")).
				Bold(true).
				Render("Question:\n\n")+
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("200")).
					Render(d.question+"\n\n")+
				d.textarea.View()+
				"\n\n"+
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("240")).
					Render("Enter to submit, Esc to cancel"),
		) + "\n"
}

func (d UserInputDialog) GetAnswer() string {
	return strings.TrimSpace(d.textarea.Value())
}
