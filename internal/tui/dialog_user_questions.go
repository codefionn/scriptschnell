package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	// Dialog box styling - responsive width will be set dynamically
	dialogBoxBaseStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(1, 2).
				Background(lipgloss.Color("235"))

	dialogTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205")).
				Padding(0, 1)
)

// UserQuestionDialog presents questions to the user with multiple choice options
type UserQuestionDialog struct {
	questions []QuestionWithOptions
	list      list.Model
	answers   []string
	current   int
	width     int
	height    int
	mu        sync.Mutex // Protects concurrent access to answers
}

type QuestionWithOptions struct {
	Question string
	Options  []string
}

type questionItem struct {
	question QuestionWithOptions
	index    int
}

func (i questionItem) Title() string { return i.question.Question }
func (i questionItem) Description() string {
	var parts []string
	for i, opt := range i.question.Options {
		if i < 3 {
			parts = append(parts, fmt.Sprintf("%c) %s", 'a'+i, opt))
		}
	}
	return strings.Join(parts, "  ")
}
func (i questionItem) FilterValue() string { return i.question.Question }

// NewUserQuestionDialog creates a dialog for user questions
func NewUserQuestionDialog(questions []QuestionWithOptions) *UserQuestionDialog {
	items := make([]list.Item, len(questions))
	for i, q := range questions {
		items[i] = questionItem{question: q, index: i}
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "" // No title, we render our own
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowTitle(false)

	return &UserQuestionDialog{
		questions: questions,
		list:      l,
		answers:   make([]string, len(questions)),
		current:   0,
	}
}

func (d *UserQuestionDialog) Init() tea.Cmd {
	return nil
}

func (d *UserQuestionDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return d, func() tea.Msg { return EndUserQuestionsMsg{} }
		case tea.KeyEnter:
			// Get current selection
			if selected, ok := d.list.SelectedItem().(questionItem); ok {
				// Show options for the selected question
				options := d.showOptions(selected)
				// Pass current size to the options dialog
				if optDialog, ok := options.(*QuestionOptionsDialog); ok {
					optDialog.width = d.width
					optDialog.height = d.height
					return optDialog, nil
				}
				return options, nil
			}
		case tea.KeyUp, tea.KeyDown:
			var cmd tea.Cmd
			d.list, cmd = d.list.Update(msg)
			return d, cmd
		}
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		// Set list size to fit within dialog box
		// Use most of the screen width (90%) with max of 120 chars
		dialogWidth := min(max(80, d.width*90/100), 120)
		boxH, boxV := dialogBoxBaseStyle.GetFrameSize()
		d.list.SetSize(dialogWidth-boxH, msg.Height-boxV-6) // Leave room for title and instructions
	}

	var cmd tea.Cmd
	d.list, cmd = d.list.Update(msg)
	return d, cmd
}

func (d *UserQuestionDialog) showOptions(selected questionItem) tea.Model {
	// Create an options dialog for the selected question
	return NewQuestionOptionsDialog(selected, d)
}

func clampListSelection(l *list.Model) {
	items := l.VisibleItems()
	if len(items) == 0 {
		return
	}

	index := l.Index()
	if index < 0 {
		l.Select(0)
		return
	}
	if index >= len(items) {
		l.Select(len(items) - 1)
	}
}

func (d *UserQuestionDialog) View() string {
	var sb strings.Builder

	// Title
	sb.WriteString(dialogTitleStyle.Render("📋 Planning Questions"))
	sb.WriteString("\n\n")

	// Instructions
	helpText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("Use ↑/↓ to navigate, Enter to select, Esc to cancel")
	sb.WriteString(helpText)
	sb.WriteString("\n\n")

	// List
	clampListSelection(&d.list)
	sb.WriteString(d.list.View())

	// Progress indicator
	answered := 0
	for _, ans := range d.answers {
		if ans != "" {
			answered++
		}
	}
	progress := lipgloss.NewStyle().
		Foreground(lipgloss.Color("242")).
		Render(fmt.Sprintf("\nProgress: %d/%d questions answered", answered, len(d.questions)))
	sb.WriteString(progress)

	// Calculate responsive dialog width
	dialogWidth := min(max(80, d.width*90/100), 120)
	dialogBoxStyle := dialogBoxBaseStyle.Width(dialogWidth)

	return dialogBoxStyle.Render(sb.String())
}

func (d *UserQuestionDialog) GetAnswers() []string {
	return d.answers
}

// QuestionOptionsDialog handles selecting an option for a specific question
type QuestionOptionsDialog struct {
	question questionItem
	parent   *UserQuestionDialog
	list     list.Model
	width    int
	height   int
}

func NewQuestionOptionsDialog(question questionItem, parent *UserQuestionDialog) *QuestionOptionsDialog {
	items := make([]list.Item, len(question.question.Options))
	for i, opt := range question.question.Options {
		items[i] = optionItem{option: opt, index: i, letter: string(rune('a' + i))}
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "" // No title, we render our own
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowTitle(false)

	// Inherit size from parent if possible, or wait for resize?
	// The parent has the size.
	dialogWidth, dialogHeight := 0, 0
	if parent != nil {
		// Use responsive width matching parent dialog
		dialogWidth = min(max(80, parent.width*90/100), 120)
		dialogHeight = parent.height
		boxH, boxV := dialogBoxBaseStyle.GetFrameSize()
		listWidth := dialogWidth - boxH
		listHeight := parent.height - boxV - 6
		if listWidth > 0 && listHeight > 0 {
			l.SetSize(listWidth, listHeight)
		}
	}

	return &QuestionOptionsDialog{
		question: question,
		parent:   parent,
		list:     l,
		width:    dialogWidth,
		height:   dialogHeight,
	}
}

type optionItem struct {
	option string
	index  int
	letter string
}

func (i optionItem) Title() string       { return fmt.Sprintf("%s) %s", i.letter, i.option) }
func (i optionItem) Description() string { return "" }
func (i optionItem) FilterValue() string { return i.option }

func (d QuestionOptionsDialog) Init() tea.Cmd {
	return nil
}

func (d *QuestionOptionsDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return d.parent, nil // Go back to questions
		case tea.KeyEnter:
			if selected, ok := d.list.SelectedItem().(optionItem); ok {
				// Store the answer with mutex protection
				d.parent.mu.Lock()
				d.parent.answers[d.question.index] = fmt.Sprintf("%d) %s", selected.index+1, selected.option)
				// Check if all questions are answered
				allAnswered := true
				for _, ans := range d.parent.answers {
					if ans == "" {
						allAnswered = false
						break
					}
				}
				d.parent.mu.Unlock()
				// If all answered, close dialog, otherwise go back to question list
				if allAnswered {
					return d.parent, func() tea.Msg { return EndUserQuestionsMsg{} }
				}
				return d.parent, nil
			}
			// No item selected, stay in dialog
			return d, nil
		case tea.KeyUp, tea.KeyDown:
			var cmd tea.Cmd
			d.list, cmd = d.list.Update(msg)
			return d, cmd
		}
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		// Set list size to fit within dialog box
		// Use responsive width matching parent dialog
		dialogWidth := min(max(80, d.width*90/100), 120)
		boxH, boxV := dialogBoxBaseStyle.GetFrameSize()
		d.list.SetSize(dialogWidth-boxH, msg.Height-boxV-6)
	}

	var cmd tea.Cmd
	d.list, cmd = d.list.Update(msg)
	return d, cmd
}

func (d QuestionOptionsDialog) View() string {
	var sb strings.Builder

	// Title with question number
	questionNum := d.question.index + 1
	title := fmt.Sprintf("Question %d of %d", questionNum, len(d.parent.questions))
	sb.WriteString(dialogTitleStyle.Render(title))
	sb.WriteString("\n\n")

	// The question text
	questionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("214"))
	sb.WriteString(questionStyle.Render(d.question.question.Question))
	sb.WriteString("\n\n")

	// Instructions
	helpText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("Use ↑/↓ to navigate, Enter to select, Esc to go back")
	sb.WriteString(helpText)
	sb.WriteString("\n\n")

	// Options list
	clampListSelection(&d.list)
	sb.WriteString(d.list.View())

	// Calculate responsive dialog width
	dialogWidth := min(max(80, d.width*90/100), 120)
	dialogBoxStyle := dialogBoxBaseStyle.Width(dialogWidth)

	return dialogBoxStyle.Render(sb.String())
}
