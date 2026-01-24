package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// Tests for handleUserMultipleQuestionsRequest integration

func TestHandleUserMultipleQuestionsRequest_ValidFormat(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: CreateSampleMultipleChoicePrompt(),
		Response:  responseChan,
		Error:     errorChan,
	}

	// Handle the request
	m.handleUserMultipleQuestionsRequest(msg)

	// Verify dialog is open
	if !m.userQuestionDialogOpen {
		t.Fatal("Expected userQuestionDialogOpen to be true")
	}

	// Verify dialog was created
	if m.userQuestionDialog == nil {
		t.Fatal("Expected userQuestionDialog to be initialized")
	}

	// Verify overlay is active
	if !m.overlayActive {
		t.Error("Expected overlayActive to be true")
	}
}

func TestHandleUserMultipleQuestionsRequest_ParsingVariations(t *testing.T) {
	testCases := []struct {
		name       string
		questions  string
		wantParse  bool
		wantQCount int
	}{
		{
			name: "periods",
			questions: `1. First question?
   a. Option A
   b. Option B
   c. Option C`,
			wantParse:  true,
			wantQCount: 1,
		},
		{
			name: "parentheses",
			questions: `1) First question?
   a) Option A
   b) Option B
   c) Option C`,
			wantParse:  true,
			wantQCount: 1,
		},
		{
			name: "colons",
			questions: `1: First question?
   a: Option A
   b: Option B
   c: Option C`,
			wantParse:  true,
			wantQCount: 1,
		},
		{
			name: "dashes",
			questions: `1- First question?
   a- Option A
   b- Option B
   c- Option C`,
			wantParse:  true,
			wantQCount: 1,
		},
		{
			name: "mixed separators",
			questions: `1. Question one?
   a) Option A
   b. Option B
   c) Option C`,
			wantParse:  true,
			wantQCount: 1,
		},
		{
			name: "uppercase options",
			questions: `1. Question?
   A. Option A
   B. Option B
   C. Option C`,
			wantParse:  true,
			wantQCount: 1,
		},
		{
			name: "double digit questions",
			questions: `10. Question ten?
   a. Option A
   b. Option B`,
			wantParse:  true,
			wantQCount: 1,
		},
		{
			name: "multiple questions",
			questions: `1. First?
   a. A
   b. B
   c. C

2. Second?
   a. X
   b. Y
   c. Z`,
			wantParse:  true,
			wantQCount: 2,
		},
		{
			name:       "no questions",
			questions:  `Just plain text without proper formatting`,
			wantParse:  false,
			wantQCount: 0,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			m := New("test-model", "", false)
			m.ready = true
			m.width = 120
			m.height = 40

			responseChan := make(chan string, 1)
			errorChan := make(chan error, 1)

			msg := UserMultipleQuestionsRequestMsg{
				Questions: tt.questions,
				Response:  responseChan,
				Error:     errorChan,
			}

			// Handle the request
			m.handleUserMultipleQuestionsRequest(msg)

			if tt.wantParse {
				// Should have opened the multiple choice dialog
				if m.userQuestionDialog == nil {
					t.Fatal("Expected userQuestionDialog to be initialized")
				}

				// Verify it's a UserQuestionDialog (not UserInputDialog)
				if _, ok := m.userQuestionDialog.(*UserQuestionDialog); !ok {
					t.Error("Expected UserQuestionDialog, got different type")
				}
			} else {
				// Should have fallen back to single input dialog
				// or sent an error
				select {
				case err := <-errorChan:
					if err == nil {
						t.Error("Expected error for unparseable questions")
					}
				case <-time.After(100 * time.Millisecond):
					// No error sent, check if dialog is UserInputDialog
					if m.userQuestionDialog != nil {
						if _, ok := m.userQuestionDialog.(UserInputDialog); ok {
							// OK - fell back to single input
						} else {
							t.Error("Expected UserInputDialog for unparseable questions")
						}
					}
				}
			}
		})
	}
}

func TestHandleUserMultipleQuestionsRequest_TooManyOptions(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: CreateSamplePromptWithTooManyOptions(),
		Response:  responseChan,
		Error:     errorChan,
	}

	// Handle the request - should fall back to single input
	m.handleUserMultipleQuestionsRequest(msg)

	// Verify dialog was created
	if m.userQuestionDialog == nil {
		t.Fatal("Expected userQuestionDialog to be initialized (fallback)")
	}

	// Should be UserInputDialog (fallback)
	if _, ok := m.userQuestionDialog.(UserInputDialog); !ok {
		t.Error("Expected UserInputDialog when parsing fails due to too many options")
	}
}

func TestHandleUserMultipleQuestionsRequest_TooFewOptions(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: CreateSamplePromptWithTooFewOptions(),
		Response:  responseChan,
		Error:     errorChan,
	}

	// Handle the request - should fall back to single input
	m.handleUserMultipleQuestionsRequest(msg)

	// Verify dialog was created
	if m.userQuestionDialog == nil {
		t.Fatal("Expected userQuestionDialog to be initialized (fallback)")
	}

	// Should be UserInputDialog (fallback)
	if _, ok := m.userQuestionDialog.(UserInputDialog); !ok {
		t.Error("Expected UserInputDialog when parsing fails due to too few options")
	}
}

func TestHandleUserMultipleQuestionsRequest_UnicodeCharacters(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: CreateSamplePromptWithUnicode(),
		Response:  responseChan,
		Error:     errorChan,
	}

	// Handle the request
	m.handleUserMultipleQuestionsRequest(msg)

	// Verify dialog is open
	if !m.userQuestionDialogOpen {
		t.Fatal("Expected userQuestionDialogOpen to be true")
	}

	// Verify dialog was created
	if m.userQuestionDialog == nil {
		t.Fatal("Expected userQuestionDialog to be initialized")
	}

	dialog, ok := m.userQuestionDialog.(*UserQuestionDialog)
	if !ok {
		t.Fatal("Expected UserQuestionDialog")
	}

	// Verify questions were parsed
	if len(dialog.questions) == 0 {
		t.Fatal("Expected questions to be parsed")
	}

	// Verify unicode in options
	if len(dialog.questions[0].Options) == 0 {
		t.Fatal("Expected options to be parsed")
	}

	// First option should contain emoji
	if dialog.questions[0].Options[0] != "😊" {
		t.Errorf("Expected '😊' as first option, got %q", dialog.questions[0].Options[0])
	}
}

func TestHandleUserMultipleQuestionsRequest_EmptyQuestions(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: "",
		Response:  responseChan,
		Error:     errorChan,
	}

	// Handle the request
	m.handleUserMultipleQuestionsRequest(msg)

	// Should send an error
	select {
	case err := <-errorChan:
		if err == nil {
			t.Error("Expected error for empty questions")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for error")
	}
}

func TestUserQuestionDialogStateTransitions(t *testing.T) {
	// Test the full state flow: question list → options → answer → complete
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: CreateSampleMultipleChoicePrompt(),
		Response:  responseChan,
		Error:     errorChan,
	}

	// Initialize dialog
	m.handleUserMultipleQuestionsRequest(msg)

	dialog, ok := m.userQuestionDialog.(*UserQuestionDialog)
	if !ok {
		t.Fatal("Expected UserQuestionDialog")
	}

	// State 1: Question list is shown
	// Verify current question index
	if dialog.current != 0 {
		t.Fatalf("Expected current to be 0, got %d", dialog.current)
	}

	// Verify list has items
	if dialog.list.FilterState() == list.Filtering {
		t.Fatal("List should not be in filtering state initially")
	}

	// State 2: Press Enter on first question
	updatedModel, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Expected nil command when showing options")
	}

	optionsDialog, ok := updatedModel.(*QuestionOptionsDialog)
	if !ok {
		t.Fatal("Expected QuestionOptionsDialog after Enter")
	}

	// State 3: Select first option and press Enter
	optionsDialog.list.Select(0)
	updatedModel, _ = optionsDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should return to question list
	returnedDialog, ok := updatedModel.(*UserQuestionDialog)
	if !ok {
		t.Fatalf("Expected to return to *UserQuestionDialog, got %T", updatedModel)
	}

	// First answer should be stored
	if returnedDialog.answers[0] == "" {
		t.Error("Expected first answer to be stored")
	}

	// State 4: Navigate to second question and select
	returnedDialog.list.Select(1)
	updatedModel, _ = returnedDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	optionsDialog = updatedModel.(*QuestionOptionsDialog)
	optionsDialog.list.Select(1)
	updatedModel, _ = optionsDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// State 5: Both questions answered, should complete
	returnedDialog, ok = updatedModel.(*UserQuestionDialog)
	if !ok {
		t.Fatal("Expected UserQuestionDialog")
	}
	if returnedDialog.answers[0] == "" {
		t.Error("Expected first answer to still be present")
	}
	if returnedDialog.answers[1] == "" {
		t.Error("Expected second answer to be stored")
	}

	// All questions answered, so next Enter should trigger completion
	// Navigate back to first question
	returnedDialog.list.Select(0)
	updatedModel, _ = returnedDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	optionsDialog = updatedModel.(*QuestionOptionsDialog)
	optionsDialog.list.Select(0)
	_, cmd = optionsDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should trigger EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("Expected EndUserQuestionsMsg when all questions answered")
	}

	endMsg := cmd()
	if _, ok := endMsg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("Expected EndUserQuestionsMsg, got %T", endMsg)
	}
}

func TestUserQuestionDialogKeyboardNavigation(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Question 1?",
			Options:  []string{"A", "B", "C"},
		},
		{
			Question: "Question 2?",
			Options:  []string{"X", "Y", "Z"},
		},
		{
			Question: "Question 3?",
			Options:  []string{"1", "2", "3"},
		},
	}

	d := NewUserQuestionDialog(questions)
	helper := NewTestModelHelper(d)

	// Test down arrow
	helper.SendDown()
	dialog := helper.GetCurrentModel().(*UserQuestionDialog)
	if dialog.list.Index() != 1 {
		t.Errorf("Expected index 1 after down arrow, got %d", dialog.list.Index())
	}

	// Test down arrow again
	helper.SendDown()
	dialog = helper.GetCurrentModel().(*UserQuestionDialog)
	if dialog.list.Index() != 2 {
		t.Errorf("Expected index 2 after second down arrow, got %d", dialog.list.Index())
	}

	// Test up arrow
	helper.SendUp()
	dialog = helper.GetCurrentModel().(*UserQuestionDialog)
	if dialog.list.Index() != 1 {
		t.Errorf("Expected index 1 after up arrow, got %d", dialog.list.Index())
	}

	// Test up arrow at top (should stay at top)
	helper.SendUp()
	dialog = helper.GetCurrentModel().(*UserQuestionDialog)
	if dialog.list.Index() != 0 {
		t.Errorf("Expected index 0 at top, got %d", dialog.list.Index())
	}
	helper.SendUp()
	dialog = helper.GetCurrentModel().(*UserQuestionDialog)
	if dialog.list.Index() != 0 {
		t.Errorf("Expected index 0 to stay at top, got %d", dialog.list.Index())
	}
}

func TestQuestionOptionsDialogNavigation(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Test?",
			Options:  []string{"Option A", "Option B", "Option C", "Option D"},
		},
	}
	parent := NewUserQuestionDialog(questions)

	questionItem := questionItem{
		question: questions[0],
		index:    0,
	}

	dialog := NewQuestionOptionsDialog(questionItem, parent)
	helper := NewTestModelHelper(dialog)

	// Initial selection should be 0
	if dialog.list.Index() != 0 {
		t.Errorf("Expected initial index 0, got %d", dialog.list.Index())
	}

	// Test down arrow
	helper.SendDown()
	dialog = helper.GetCurrentModel().(*QuestionOptionsDialog)
	if dialog.list.Index() != 1 {
		t.Errorf("Expected index 1 after down arrow, got %d", dialog.list.Index())
	}

	// Test down arrow multiple times
	helper.SendDown()
	helper.SendDown()
	dialog = helper.GetCurrentModel().(*QuestionOptionsDialog)
	if dialog.list.Index() != 3 {
		t.Errorf("Expected index 3 after multiple down arrows, got %d", dialog.list.Index())
	}

	// Test up arrow
	helper.SendUp()
	dialog = helper.GetCurrentModel().(*QuestionOptionsDialog)
	if dialog.list.Index() != 2 {
		t.Errorf("Expected index 2 after up arrow, got %d", dialog.list.Index())
	}
}

func TestQuestionOptionsDialogEscapeGoesBack(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Test?",
			Options:  []string{"A", "B", "C"},
		},
	}
	parent := NewUserQuestionDialog(questions)
	parent.width = 100
	parent.height = 30

	questionItem := questionItem{
		question: questions[0],
		index:    0,
	}

	dialog := NewQuestionOptionsDialog(questionItem, parent)

	// Press Escape
	updatedModel, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Should return to parent
	returnedParent, ok := updatedModel.(*UserQuestionDialog)
	if !ok {
		t.Fatal("Expected to return to UserQuestionDialog on Escape")
	}

	// Verify it's the same parent
	if returnedParent != parent {
		t.Error("Expected to return to same parent instance")
	}
}

func TestUserQuestionDialogResponsiveSizing(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 80
	m.height = 24

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: CreateSampleMultipleChoicePrompt(),
		Response:  responseChan,
		Error:     errorChan,
	}

	// Handle request with small window
	m.handleUserMultipleQuestionsRequest(msg)

	dialog, ok := m.userQuestionDialog.(*UserQuestionDialog)
	if !ok {
		t.Fatal("Expected UserQuestionDialog")
	}

	// Should have received the size
	if dialog.width != 80 {
		t.Errorf("Expected width 80, got %d", dialog.width)
	}
	if dialog.height != 24 {
		t.Errorf("Expected height 24, got %d", dialog.height)
	}

	// Resize window
	updatedModel, _ := dialog.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	dialog, ok = updatedModel.(*UserQuestionDialog)
	if !ok {
		t.Fatal("Expected UserQuestionDialog")
	}

	// Should update size
	if dialog.width != 200 {
		t.Errorf("Expected width 200 after resize, got %d", dialog.width)
	}
	if dialog.height != 60 {
		t.Errorf("Expected height 60 after resize, got %d", dialog.height)
	}
}

func TestUserQuestionDialogWindowResizeWithActiveOptionsDialog(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Test question with very long text that should wrap properly?",
			Options:  []string{"Option A with some text", "Option B", "Option C"},
		},
	}
	parent := NewUserQuestionDialog(questions)
	parent.width = 100
	parent.height = 30

	questionItem := questionItem{
		question: questions[0],
		index:    0,
	}

	dialog := NewQuestionOptionsDialog(questionItem, parent)

	// Initial size (parent width 100, dialog gets 90% = 90, then min(90, 120) = 90)
	if dialog.width != 90 {
		t.Errorf("Expected initial width 90, got %d", dialog.width)
	}

	// Resize to larger window
	updatedModel, _ := dialog.Update(tea.WindowSizeMsg{Width: 150, Height: 50})
	dialog = updatedModel.(*QuestionOptionsDialog)

	if dialog.width != 150 {
		t.Errorf("Expected width 150 after resize, got %d", dialog.width)
	}
	if dialog.height != 50 {
		t.Errorf("Expected height 50 after resize, got %d", dialog.height)
	}

	// Resize to smaller window
	updatedModel, _ = dialog.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	dialog = updatedModel.(*QuestionOptionsDialog)

	if dialog.width != 90 {
		t.Errorf("Expected width 90 after resize, got %d", dialog.width)
	}
	if dialog.height != 25 {
		t.Errorf("Expected height 25 after resize, got %d", dialog.height)
	}
}

func TestUserQuestionDialogAnswerFormat(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "What is your choice?",
			Options:  []string{"First Option", "Second Option", "Third Option"},
		},
	}
	parent := NewUserQuestionDialog(questions)

	questionItem := questionItem{
		question: questions[0],
		index:    0,
	}

	dialog := NewQuestionOptionsDialog(questionItem, parent)

	// Select second option (index 1)
	dialog.list.Select(1)
	updatedModel, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	returnedParent := updatedModel.(*UserQuestionDialog)

	// Answer should be formatted as "2) Second Option"
	expectedAnswer := "2) Second Option"
	if returnedParent.answers[0] != expectedAnswer {
		t.Errorf("Expected answer %q, got %q", expectedAnswer, returnedParent.answers[0])
	}

	// Should trigger completion since this is the only question
	if cmd == nil {
		t.Error("Expected command when all questions answered")
	}
}

func TestUserQuestionDialogCompleteTriggersEndMsg(t *testing.T) {
	// Single question scenario - answering it should complete
	questions := []QuestionWithOptions{
		{
			Question: "Single question?",
			Options:  []string{"A", "B", "C"},
		},
	}
	parent := NewUserQuestionDialog(questions)

	questionItem := questionItem{
		question: questions[0],
		index:    0,
	}

	dialog := NewQuestionOptionsDialog(questionItem, parent)

	// Select option
	dialog.list.Select(0)
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should trigger EndUserQuestionsMsg since all questions answered
	if cmd == nil {
		t.Fatal("Expected EndUserQuestionsMsg when single question answered")
	}

	msg := cmd()
	if _, ok := msg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("Expected EndUserQuestionsMsg, got %T", msg)
	}
}

func TestHandleUserMultipleQuestionsRequest_OverlayActivation(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	// Initially overlay should not be active
	if m.overlayActive {
		t.Error("Expected overlayActive to be false initially")
	}

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: CreateSampleMultipleChoicePrompt(),
		Response:  responseChan,
		Error:     errorChan,
	}

	m.handleUserMultipleQuestionsRequest(msg)

	// Overlay should be active after dialog opens
	if !m.overlayActive {
		t.Error("Expected overlayActive to be true after dialog opens")
	}
}

func TestUserQuestionDialogAnswerStorage(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "First?",
			Options:  []string{"A", "B", "C"},
		},
		{
			Question: "Second?",
			Options:  []string{"X", "Y", "Z"},
		},
		{
			Question: "Third?",
			Options:  []string{"1", "2", "3"},
		},
	}
	dialog := NewUserQuestionDialog(questions)

	// Answer first question
	qItem1 := questionItem{question: questions[0], index: 0}
	optDialog1 := NewQuestionOptionsDialog(qItem1, dialog)
	optDialog1.list.Select(1) // Select B
	updated, _ := optDialog1.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dialog = updated.(*UserQuestionDialog)

	if dialog.answers[0] != "2) B" {
		t.Errorf("Expected answer[0] = '2) B', got %q", dialog.answers[0])
	}
	if dialog.answers[1] != "" {
		t.Errorf("Expected answer[1] to be empty, got %q", dialog.answers[1])
	}

	// Answer second question
	dialog.list.Select(1)
	qItem2 := questionItem{question: questions[1], index: 1}
	optDialog2 := NewQuestionOptionsDialog(qItem2, dialog)
	optDialog2.list.Select(2) // Select Z
	updated, _ = optDialog2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dialog = updated.(*UserQuestionDialog)

	if dialog.answers[0] != "2) B" {
		t.Errorf("Expected answer[0] to persist, got %q", dialog.answers[0])
	}
	if dialog.answers[1] != "3) Z" {
		t.Errorf("Expected answer[1] = '3) Z', got %q", dialog.answers[1])
	}

	// Answer third question
	dialog.list.Select(2)
	qItem3 := questionItem{question: questions[2], index: 2}
	optDialog3 := NewQuestionOptionsDialog(qItem3, dialog)
	optDialog3.list.Select(0) // Select 1
	_, cmd := optDialog3.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should trigger completion
	if cmd == nil {
		t.Fatal("Expected EndUserQuestionsMsg")
	}

	if dialog.answers[2] != "1) 1" {
		t.Errorf("Expected answer[2] = '1) 1', got %q", dialog.answers[2])
	}
}

func TestUserQuestionDialogPartialAnswers(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "First?",
			Options:  []string{"A", "B"},
		},
		{
			Question: "Second?",
			Options:  []string{"X", "Y"},
		},
	}
	dialog := NewUserQuestionDialog(questions)

	// Answer only first question
	qItem1 := questionItem{question: questions[0], index: 0}
	optDialog1 := NewQuestionOptionsDialog(qItem1, dialog)
	optDialog1.list.Select(0)
	updated, _ := optDialog1.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dialog = updated.(*UserQuestionDialog)

	// First answer should be set
	if dialog.answers[0] != "1) A" {
		t.Errorf("Expected answer[0] = '1) A', got %q", dialog.answers[0])
	}

	// Second answer should be empty
	if dialog.answers[1] != "" {
		t.Errorf("Expected answer[1] to be empty, got %q", dialog.answers[1])
	}

	// Try to complete by pressing Escape on second question
	dialog.list.Select(1)
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Escape should send EndUserQuestionsMsg with partial answers
	if cmd == nil {
		t.Fatal("Expected EndUserQuestionsMsg on Escape")
	}

	msg := cmd()
	if _, ok := msg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("Expected EndUserQuestionsMsg, got %T", msg)
	}
}

func TestHandleUserMultipleQuestionsRequest_ErrorChannelBlocked(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	// Create unbuffered error channel
	errorChan := make(chan error)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: "", // Empty questions should trigger error
		Response:  make(chan string),
		Error:     errorChan,
	}

	// Handle should not block even if error channel is unbuffered
	// It should timeout after 5 seconds
	done := make(chan bool)
	go func() {
		m.handleUserMultipleQuestionsRequest(msg)
		done <- true
	}()

	select {
	case <-done:
		// OK - completed
	case <-time.After(6 * time.Second):
		t.Fatal("handleUserMultipleQuestionsRequest blocked on error channel")
	}
}

func TestUserQuestionDialogListItemDescription(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Which option?",
			Options:  []string{"Alpha", "Beta", "Gamma"},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Get the first item
	items := dialog.list.Items()
	if len(items) == 0 {
		t.Fatal("Expected items in list")
	}

	item, ok := items[0].(questionItem)
	if !ok {
		t.Fatal("Expected questionItem")
	}

	// Verify description format
	expectedDesc := "a) Alpha  b) Beta  c) Gamma"
	if item.Description() != expectedDesc {
		t.Errorf("Expected description %q, got %q", expectedDesc, item.Description())
	}
}

func TestUserQuestionDialogWithLongQuestion(t *testing.T) {
	longQuestion := "This is a very long question that has many words and should be displayed properly in the dialog without causing any issues with the rendering or the layout of the interface in any way shape or form whatsoever"

	questions := []QuestionWithOptions{
		{
			Question: longQuestion,
			Options:  []string{"Option A", "Option B"},
		},
	}

	dialog := NewUserQuestionDialog(questions)
	dialog.width = 120
	dialog.height = 40

	helper := NewTestModelHelper(dialog)

	// Should not panic on rendering
	view := dialog.View()
	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}

	// Should handle keyboard navigation
	helper.SendDown()
	dialog = helper.GetCurrentModel().(*UserQuestionDialog)
	if dialog.list.Index() != 0 {
		t.Errorf("Expected index to stay at 0 with single question, got %d", dialog.list.Index())
	}
}

func TestQuestionOptionsDialogOptionItemFormat(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Test?",
			Options:  []string{"First", "Second", "Third"},
		},
	}
	parent := NewUserQuestionDialog(questions)

	questionItem := questionItem{
		question: questions[0],
		index:    0,
	}

	dialog := NewQuestionOptionsDialog(questionItem, parent)

	// Get items
	items := dialog.list.Items()
	if len(items) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(items))
	}

	// Verify each item format
	for i, item := range items {
		optItem, ok := item.(optionItem)
		if !ok {
			t.Fatalf("Expected optionItem at index %d", i)
		}

		expectedLetter := string([]rune{'a' + rune(i)})
		if optItem.letter != expectedLetter {
			t.Errorf("Expected letter %q at index %d, got %q", expectedLetter, i, optItem.letter)
		}

		expectedTitle := fmt.Sprintf("%s) %s", expectedLetter, questions[0].Options[i])
		if optItem.Title() != expectedTitle {
			t.Errorf("Expected title %q, got %q", expectedTitle, optItem.Title())
		}

		if optItem.index != i {
			t.Errorf("Expected index %d, got %d", i, optItem.index)
		}
	}
}

func TestUserQuestionDialogEdgeCases(t *testing.T) {
	testCases := []struct {
		name      string
		questions string
	}{
		{
			name:      "only spaces",
			questions: "   \n   \n   ",
		},
		{
			name:      "tabs instead of spaces",
			questions: "1. Question?\n\ta. Option A\n\tb. Option B",
		},
		{
			name:      "mixed whitespace",
			questions: "  1. Question?\n   \t  a. Option A\n  \t b. Option B",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			m := New("test-model", "", false)
			m.ready = true
			m.width = 120
			m.height = 40

			msg := UserMultipleQuestionsRequestMsg{
				Questions: tt.questions,
				Response:  make(chan string, 1),
				Error:     make(chan error, 1),
			}

			// Should not panic
			m.handleUserMultipleQuestionsRequest(msg)
		})
	}
}
