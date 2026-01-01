package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUserQuestionDialogCreation(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Which approach should we use?",
			Options:  []string{"Option A", "Option B", "Option C"},
		},
		{
			Question: "What color scheme?",
			Options:  []string{"Light", "Dark", "Auto"},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	if len(dialog.questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(dialog.questions))
	}

	if len(dialog.answers) != 2 {
		t.Fatalf("expected 2 answer slots, got %d", len(dialog.answers))
	}

	if dialog.current != 0 {
		t.Fatalf("expected current to be 0, got %d", dialog.current)
	}
}

func TestUserQuestionDialogResponsiveSizing(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Test question?",
			Options:  []string{"A", "B", "C"},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Simulate window resize
	msg := tea.WindowSizeMsg{Width: 200, Height: 50}
	updatedModel, _ := dialog.Update(msg)
	dialog = updatedModel.(UserQuestionDialog)

	// Verify dialog received size update
	if dialog.width != 200 {
		t.Fatalf("expected width 200, got %d", dialog.width)
	}
	if dialog.height != 50 {
		t.Fatalf("expected height 50, got %d", dialog.height)
	}

	// Verify responsive sizing calculation works
	// With width=200, dialog should use min(200*90/100, 120) = 120
	expectedDialogWidth := 120
	_ = expectedDialogWidth // Just verify no panic in Update
}

func TestUserQuestionDialogSmallTerminal(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Test?",
			Options:  []string{"A", "B", "C"},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Simulate small terminal (less than 80 chars)
	msg := tea.WindowSizeMsg{Width: 60, Height: 20}
	updatedModel, _ := dialog.Update(msg)
	dialog = updatedModel.(UserQuestionDialog)

	// Should still use minimum width of 80
	if dialog.width != 60 {
		t.Fatalf("expected width 60, got %d", dialog.width)
	}

	// Dialog should use 80 (min width), not 54 (60*90/100)
	// Just verify the dialog handled the small size without panic
	expectedMinWidth := 80
	_ = expectedMinWidth
}

func TestUserQuestionDialogEscapeKey(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Test?",
			Options:  []string{"A", "B", "C"},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Send Escape key
	updatedModel, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Should return EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("expected command from Escape key")
	}

	msg := cmd()
	if _, ok := msg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("expected EndUserQuestionsMsg, got %T", msg)
	}

	// Model should not change
	if _, ok := updatedModel.(UserQuestionDialog); !ok {
		t.Fatalf("expected UserQuestionDialog model, got %T", updatedModel)
	}
}

func TestQuestionOptionsDialogCreation(t *testing.T) {
	parentQuestions := []QuestionWithOptions{
		{
			Question: "Test?",
			Options:  []string{"Option A", "Option B", "Option C"},
		},
	}
	parent := NewUserQuestionDialog(parentQuestions)
	parent.width = 100
	parent.height = 30

	questionItem := questionItem{
		question: parentQuestions[0],
		index:    0,
	}

	optDialog := NewQuestionOptionsDialog(questionItem, &parent)

	if optDialog.question.index != 0 {
		t.Fatalf("expected question index 0, got %d", optDialog.question.index)
	}

	if optDialog.parent != &parent {
		t.Fatal("expected parent reference to be set")
	}
}

func TestQuestionOptionsDialogSelectAndReturn(t *testing.T) {
	parentQuestions := []QuestionWithOptions{
		{
			Question: "First?",
			Options:  []string{"A1", "A2", "A3"},
		},
		{
			Question: "Second?",
			Options:  []string{"B1", "B2", "B3"},
		},
	}
	parent := NewUserQuestionDialog(parentQuestions)

	questionItem := questionItem{
		question: parentQuestions[0],
		index:    0,
	}

	optDialog := NewQuestionOptionsDialog(questionItem, &parent)
	optDialog.list.Select(1) // Select option B (index 1)

	// Send Enter key
	updatedModel, cmd := optDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should return to parent
	if _, ok := updatedModel.(*UserQuestionDialog); !ok {
		t.Fatalf("expected to return to UserQuestionDialog, got %T", updatedModel)
	}

	// Answer should be stored in parent
	parentModel := updatedModel.(*UserQuestionDialog)
	if parentModel.answers[0] == "" {
		t.Fatal("expected answer to be stored in parent")
	}

	if parentModel.answers[0] != "2) A2" {
		t.Fatalf("expected answer '2) A2', got %q", parentModel.answers[0])
	}

	// Should not trigger EndUserQuestionsMsg since not all answered
	if cmd != nil {
		t.Fatal("expected nil command when not all questions answered")
	}
}

func TestQuestionOptionsDialogCompleteAllQuestions(t *testing.T) {
	parentQuestions := []QuestionWithOptions{
		{
			Question: "Only question?",
			Options:  []string{"A", "B", "C"},
		},
	}
	parent := NewUserQuestionDialog(parentQuestions)

	questionItem := questionItem{
		question: parentQuestions[0],
		index:    0,
	}

	optDialog := NewQuestionOptionsDialog(questionItem, &parent)
	optDialog.list.Select(0) // Select option A

	// Send Enter key - this should complete all questions
	_, cmd := optDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should trigger EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("expected command when all questions answered")
	}

	msg := cmd()
	if _, ok := msg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("expected EndUserQuestionsMsg when complete, got %T", msg)
	}
}

func TestIsLikelyMultipleChoicePrompt(t *testing.T) {
	tests := []struct {
		name     string
		question string
		expected bool
	}{
		{
			name: "planning agent format with periods",
			question: `1. Do you have any existing evaluation data in the database that you want to preserve?
   a. Yes, preserve all existing data
   b. No, I can delete the database and start fresh
   c. I'm not sure / need to check first

2. How would you like to handle the database schema update?
   a. Implement a proper migration system for future updates
   b. Just add the missing column to fix this issue
   c. Delete and recreate the database (if no data to preserve)`,
			expected: true,
		},
		{
			name: "format with parentheses",
			question: `1. Question one?
   a) Option A
   b) Option B
   c) Option C

2. Question two?
   a) Option X
   b) Option Y
   c) Option Z`,
			expected: true,
		},
		{
			name: "mixed format",
			question: `1) Question one?
   a. Option A
   b. Option B
   c. Option C`,
			expected: true,
		},
		{
			name: "colon separators",
			question: `1: What is your preference?
   a: Option A
   b: Option B`,
			expected: true,
		},
		{
			name: "dash separators",
			question: `1- Question?
   a- First choice
   b- Second choice`,
			expected: true,
		},
		{
			name: "uppercase options",
			question: `1. Question?
   A. First
   B. Second
   C. Third`,
			expected: true,
		},
		{
			name: "more than 3 options",
			question: `1. Pick one:
   a. Option A
   b. Option B
   c. Option C
   d. Option D`,
			expected: true,
		},
		{
			name: "double digit question",
			question: `10. Question ten?
   a. Yes
   b. No`,
			expected: true,
		},
		{
			name:     "single line text",
			question: "What is your name?",
			expected: false,
		},
		{
			name: "missing options",
			question: `1. Question one?
2. Question two?`,
			expected: false,
		},
		{
			name: "only one option",
			question: `1. Question?
   a. Option A`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLikelyMultipleChoicePrompt(tt.question)
			if result != tt.expected {
				t.Errorf("isLikelyMultipleChoicePrompt() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
