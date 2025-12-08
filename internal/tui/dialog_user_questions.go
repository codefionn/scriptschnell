package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// UserQuestionDialog presents questions to the user with multiple choice options
type UserQuestionDialog struct {
	questions []QuestionWithOptions
	list      list.Model
	answers   []string
	current   int
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
	return fmt.Sprintf("a) %s  b) %s  c) %s",
		i.question.Options[0],
		i.question.Options[1],
		i.question.Options[2])
}
func (i questionItem) FilterValue() string { return i.question.Question }

// NewUserQuestionDialog creates a dialog for user questions
func NewUserQuestionDialog(questions []QuestionWithOptions) UserQuestionDialog {
	items := make([]list.Item, len(questions))
	for i, q := range questions {
		items[i] = questionItem{question: q, index: i}
	}

	l := list.New(items, list.NewDefaultDelegate(), 80, 20)
	l.Title = "Planning Questions"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	return UserQuestionDialog{
		questions: questions,
		list:      l,
		answers:   make([]string, len(questions)),
		current:   0,
	}
}

func (d UserQuestionDialog) Init() tea.Cmd {
	return nil
}

func (d UserQuestionDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return d, tea.Quit
		case tea.KeyEnter:
			// Get current selection
			if selected, ok := d.list.SelectedItem().(questionItem); ok {
				// Show options for the selected question
				return d.showOptions(selected), nil
			}
		case tea.KeyUp, tea.KeyDown:
			var cmd tea.Cmd
			d.list, cmd = d.list.Update(msg)
			return d, cmd
		}
	}

	var cmd tea.Cmd
	d.list, cmd = d.list.Update(msg)
	return d, cmd
}

func (d UserQuestionDialog) showOptions(selected questionItem) tea.Model {
	// Create an options dialog for the selected question
	return NewQuestionOptionsDialog(selected, &d)
}

func (d UserQuestionDialog) View() string {
	return d.list.View()
}

func (d UserQuestionDialog) GetAnswers() []string {
	return d.answers
}

// QuestionOptionsDialog handles selecting an option for a specific question
type QuestionOptionsDialog struct {
	question questionItem
	parent   *UserQuestionDialog
	list     list.Model
}

func NewQuestionOptionsDialog(question questionItem, parent *UserQuestionDialog) QuestionOptionsDialog {
	items := make([]list.Item, len(question.question.Options))
	for i, opt := range question.question.Options {
		items[i] = optionItem{option: opt, index: i, letter: string(rune('a' + i))}
	}

	l := list.New(items, list.NewDefaultDelegate(), 80, 20)
	l.Title = "Select an option"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	return QuestionOptionsDialog{
		question: question,
		parent:   parent,
		list:     l,
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

func (d QuestionOptionsDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return d.parent, nil // Go back to questions
		case tea.KeyEnter:
			if selected, ok := d.list.SelectedItem().(optionItem); ok {
				// Store the answer
				d.parent.answers[d.question.index] = fmt.Sprintf("%d) %s", selected.index+1, selected.option)
				// Move to next question
				if d.question.index+1 < len(d.parent.questions) {
					d.list.Select(d.question.index + 1)
				}
				return d.parent, nil
			}
		case tea.KeyUp, tea.KeyDown:
			var cmd tea.Cmd
			d.list, cmd = d.list.Update(msg)
			return d, cmd
		}
	}

	var cmd tea.Cmd
	d.list, cmd = d.list.Update(msg)
	return d, cmd
}

func (d QuestionOptionsDialog) View() string {
	return d.list.View()
}
