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
	width    int
	height   int
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
			// Cancel - send EndUserQuestionsMsg to close dialog
			return d, func() tea.Msg { return EndUserQuestionsMsg{} }
		case tea.KeyEnter:
			// Submit - send EndUserQuestionsMsg to close dialog and return answer
			return d, func() tea.Msg { return EndUserQuestionsMsg{} }
		}
	case tea.WindowSizeMsg:
		// Handle window resize
		d.width = msg.Width
		d.height = msg.Height
		// Update textarea width to be responsive
		// Use most of the dialog width (85%) with max of 100 chars
		dialogWidth := min(max(80, d.width*90/100), 120)
		textareaWidth := min(dialogWidth-8, 100) // Account for padding and border
		d.textarea.SetWidth(textareaWidth)
		return d, cmd
	}

	d.textarea, cmd = d.textarea.Update(msg)
	return d, cmd
}

func (d UserInputDialog) View() string {
	if d.quitting {
		return ""
	}

	// Calculate responsive dialog width
	dialogWidth := min(max(80, d.width*90/100), 120)

	// Build content
	content := lipgloss.NewStyle().
		Foreground(lipgloss.Color("62")).
		Bold(true).
		Render("Question:\n\n") +
		lipgloss.NewStyle().
			Foreground(lipgloss.Color("200")).
			Render(d.question+"\n\n") +
		d.textarea.View() +
		"\n\n" +
		lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("Enter to submit, Esc to cancel")

	// Apply dialog box styling with responsive width
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(dialogWidth).
		Render(content)
}

func (d UserInputDialog) GetAnswer() string {
	return strings.TrimSpace(d.textarea.Value())
}
