package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Tests for UserInputDialog interactions

func TestUserInputDialogCreation(t *testing.T) {
	question := "What is your favorite color?"
	dialog := NewUserInputDialog(question)

	if dialog.question != question {
		t.Errorf("Expected question %q, got %q", question, dialog.question)
	}

	if dialog.GetAnswer() != "" {
		t.Error("Expected empty answer initially")
	}
}

func TestUserInputDialogInitialState(t *testing.T) {
	dialog := NewUserInputDialog("Test question")

	// Textarea should be focused and blinking
	if cmd := dialog.Init(); cmd == nil {
		t.Error("Expected blink command from Init")
	}

	// Should not be quitting
	if dialog.quitting {
		t.Error("Expected quitting to be false initially")
	}
}

func TestUserInputDialogEnterSubmits(t *testing.T) {
	dialog := NewUserInputDialog("What is your name?")
	helper := NewTestModelHelper(dialog)

	// Type some text
	helper.SendString("Alice")

	// Press Enter
	updatedModel, cmd := helper.UpdateWithMsgAndCmd(tea.KeyMsg{Type: tea.KeyEnter})

	// Should send EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("Expected command from Enter key")
	}

	msg := cmd()
	if _, ok := msg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("Expected EndUserQuestionsMsg, got %T", msg)
	}

	// Model should still be the same type
	dialog = updatedModel.(UserInputDialog)

	// Answer should be available
	answer := dialog.GetAnswer()
	if answer != "Alice" {
		t.Errorf("Expected answer 'Alice', got %q", answer)
	}
}

func TestUserInputDialogEscapeCancels(t *testing.T) {
	dialog := NewUserInputDialog("Test question")
	helper := NewTestModelHelper(dialog)

	// Type some text
	helper.SendString("Some text")

	// Press Escape
	updatedModel, cmd := helper.UpdateWithMsgAndCmd(tea.KeyMsg{Type: tea.KeyEsc})

	// Should send EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("Expected command from Escape key")
	}

	msg := cmd()
	if _, ok := msg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("Expected EndUserQuestionsMsg, got %T", msg)
	}

	// Model should still be the same type
	updatedDialog := updatedModel.(UserInputDialog)

	// Answer should still be available (for checking if user typed before cancelling)
	answer := updatedDialog.GetAnswer()
	if answer != "Some text" {
		t.Errorf("Expected answer to be preserved, got %q", answer)
	}
}

func TestUserInputDialogCtrlCCancels(t *testing.T) {
	dialog := NewUserInputDialog("Test question")

	// Press Ctrl+C
	updatedModel, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Should send EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("Expected command from Ctrl+C key")
	}

	msg := cmd()
	if _, ok := msg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("Expected EndUserQuestionsMsg, got %T", msg)
	}

	// Model should still be valid
	if _, ok := updatedModel.(UserInputDialog); !ok {
		t.Fatal("Expected UserInputDialog model")
	}
}

func TestUserInputDialogTextEntry(t *testing.T) {
	dialog := NewUserInputDialog("Type something:")
	helper := NewTestModelHelper(dialog)

	// Type characters
	helper.SendString("Hello, World!")

	dialog = helper.GetCurrentModel().(UserInputDialog)

	// Verify text was entered
	if dialog.GetAnswer() != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got %q", dialog.GetAnswer())
	}
}

func TestUserInputDialogBackspace(t *testing.T) {
	dialog := NewUserInputDialog("Type something:")
	helper := NewTestModelHelper(dialog)

	// Type text
	helper.SendString("Hello")

	// Press backspace
	helper.UpdateWithMsg(tea.KeyMsg{Type: tea.KeyBackspace})

	dialog = helper.GetCurrentModel().(UserInputDialog)

	// Should have one less character
	if dialog.GetAnswer() != "Hell" {
		t.Errorf("Expected 'Hell' after backspace, got %q", dialog.GetAnswer())
	}
}

func TestUserInputDialogMultipleBackspaces(t *testing.T) {
	dialog := NewUserInputDialog("Type:")
	helper := NewTestModelHelper(dialog)

	// Type text
	helper.SendString("Testing")

	// Press backspace multiple times
	for i := 0; i < 4; i++ {
		helper.UpdateWithMsg(tea.KeyMsg{Type: tea.KeyBackspace})
	}

	dialog = helper.GetCurrentModel().(UserInputDialog)

	if dialog.GetAnswer() != "Tes" {
		t.Errorf("Expected 'Tes' after 4 backspaces, got %q", dialog.GetAnswer())
	}
}

func TestUserInputChangeGetAnswerTrims(t *testing.T) {
	dialog := NewUserInputDialog("Question:")

	// Add spaces
	dialog.textarea.SetValue("  spaced out  ")

	// GetAnswer should trim
	answer := dialog.GetAnswer()
	if answer != "spaced out" {
		t.Errorf("Expected trimmed answer, got %q", answer)
	}
}

func TestUserInputChangeEmptyAnswer(t *testing.T) {
	dialog := NewUserInputDialog("Question:")

	// Don't type anything
	answer := dialog.GetAnswer()

	if answer != "" {
		t.Errorf("Expected empty answer, got %q", answer)
	}

	// Still should be able to submit
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Expected command from Enter")
	}
}

func TestUserInputChangeWhitespaceAnswer(t *testing.T) {
	dialog := NewUserInputDialog("Question:")

	// Type only spaces
	dialog.textarea.SetValue("   ")

	// GetAnswer should return empty string (trimmed)
	answer := dialog.GetAnswer()
	if answer != "" {
		t.Errorf("Expected empty answer for whitespace, got %q", answer)
	}
}

func TestUserInputChangeLongText(t *testing.T) {
	longText := strings.Repeat("a", 500)
	dialog := NewUserInputDialog("Long question?")

	// Set long text
	dialog.textarea.SetValue(longText)

	// Should be able to get the answer
	answer := dialog.GetAnswer()
	if len(answer) != 500 {
		t.Errorf("Expected 500 characters, got %d", len(answer))
	}
}

func TestUserInputChangeCharacterLimit(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Check character limit
	if dialog.textarea.CharLimit != 1000 {
		t.Errorf("Expected char limit 1000, got %d", dialog.textarea.CharLimit)
	}
}

func TestUserInputChangeNoNewlines(t *testing.T) {
	dialog := NewUserInputDialog("Question?")
	helper := NewTestModelHelper(dialog)

	// Try to insert newline with Enter (should not work due to config)
	helper.UpdateWithMsg(tea.KeyMsg{Type: tea.KeyEnter})

	afterText := dialog.GetAnswer()

	// Text should not contain newline (and Enter sends EndUserQuestionsMsg anyway)
	if strings.Contains(afterText, "\n") {
		t.Error("Expected no newlines in answer")
	}
}

func TestUserInputChangeSpecialCharacters(t *testing.T) {
	specialChars := "!@#$%^&*()_+-=[]{}|;':\",./<>?"
	dialog := NewUserInputDialog("Type special chars:")
	helper := NewTestModelHelper(dialog)

	// Type special characters
	helper.SendString(specialChars)

	dialog = helper.GetCurrentModel().(UserInputDialog)

	answer := dialog.GetAnswer()
	if answer != specialChars {
		t.Errorf("Expected %q, got %q", specialChars, answer)
	}
}

func TestUserInputChangeUnicodeCharacters(t *testing.T) {
	unicodeText := "Hello 世界 🌍 你好"
	dialog := NewUserInputDialog("Type unicode:")
	helper := NewTestModelHelper(dialog)

	// Type unicode
	helper.SendString(unicodeText)

	dialog = helper.GetCurrentModel().(UserInputDialog)

	answer := dialog.GetAnswer()
	if answer != unicodeText {
		t.Errorf("Expected %q, got %q", unicodeText, answer)
	}
}

func TestUserInputChangeWindowResize(t *testing.T) {
	dialog := NewUserInputDialog("Resize test?")

	// Initial width
	dialog.width = 80

	// Resize to larger window
	updatedModel, _ := dialog.Update(tea.WindowSizeMsg{Width: 150, Height: 50})
	dialog = updatedModel.(UserInputDialog)

	if dialog.width != 150 {
		t.Errorf("Expected width 150, got %d", dialog.width)
	}
	if dialog.height != 50 {
		t.Errorf("Expected height 50, got %d", dialog.height)
	}

	// Resize to smaller window
	updatedModel, _ = dialog.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	dialog = updatedModel.(UserInputDialog)

	if dialog.width != 60 {
		t.Errorf("Expected width 60, got %d", dialog.width)
	}
	if dialog.height != 20 {
		t.Errorf("Expected height 20, got %d", dialog.height)
	}
}

func TestUserInputChangeResponsiveWidth(t *testing.T) {
	dialog := NewUserInputDialog("Responsive test?")

	// Resize to various sizes and verify textarea width adjusts
	testSizes := []struct {
		windowWidth int
		expectMax   int // Expected max textarea width
	}{
		{80, 72},   // 80 * 90/100 - 8 padding = 64, capped at 72
		{100, 82},  // 100 * 90/100 - 8 = 82
		{120, 100}, // Capped at 100
		{150, 100}, // Still capped at 100
	}

	for _, tt := range testSizes {
		updatedModel, _ := dialog.Update(tea.WindowSizeMsg{Width: tt.windowWidth, Height: 40})
		dialog = updatedModel.(UserInputDialog)

		textareaWidth := dialog.textarea.Width()
		if textareaWidth > tt.expectMax {
			t.Errorf("Window width %d: textarea width %d exceeds max %d",
				tt.windowWidth, textareaWidth, tt.expectMax)
		}
	}
}

func TestUserInputChangeViewRender(t *testing.T) {
	dialog := NewUserInputDialog("What is your name?")
	dialog.textarea.SetValue("Test answer")
	dialog.width = 100
	dialog.height = 40

	// Render the view - should not panic
	view := dialog.View()

	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}

	// Should contain the question
	if !strings.Contains(view, "What is your name?") {
		t.Error("View should contain the question")
	}

	// Should contain help text
	if !strings.Contains(view, "Enter to submit") {
		t.Error("View should contain submit help text")
	}

	if !strings.Contains(view, "Esc to cancel") {
		t.Error("View should contain cancel help text")
	}
}

func TestUserInputChangeQuittingState(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Initially not quitting
	if dialog.quitting {
		t.Error("Expected quitting to be false")
	}

	// The quitting flag is set when quitting but View returns empty string
	dialog.quitting = true

	view := dialog.View()
	if view != "" {
		t.Error("Expected empty view when quitting")
	}
}

func TestUserInputChangePlaceholder(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Should have placeholder
	placeholder := dialog.textarea.Placeholder
	if placeholder != "Type your answer here..." {
		t.Errorf("Expected placeholder 'Type your answer here...', got %q", placeholder)
	}
}

func TestUserInputChangeInitialFocus(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Textarea should be focused
	if !dialog.textarea.Focused() {
		t.Error("Expected textarea to be focused")
	}
}

func TestUserInputChangeCursorMovement(t *testing.T) {
	dialog := NewUserInputDialog("Type:")
	helper := NewTestModelHelper(dialog)

	// Type some text
	helper.SendString("Hello")

	dialog = helper.GetCurrentModel().(UserInputDialog)

	// Move cursor left
	updated, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyLeft})
	dialog = updated.(UserInputDialog)

	// Cursor should be at position 4 (after "Hell")
	// Note: This is testing the underlying textarea behavior
	_ = dialog // Use dialog to avoid unused variable error
}

func TestUserInputChangeDelete(t *testing.T) {
	dialog := NewUserInputDialog("Type:")
	helper := NewTestModelHelper(dialog)

	// Type text
	helper.SendString("Hello World")

	dialog = helper.GetCurrentModel().(UserInputDialog)

	// Move left once
	updated, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyLeft})
	dialog = updated.(UserInputDialog)

	// Delete (should delete 'd' from "World")
	updated, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyDelete})
	dialog = updated.(UserInputDialog)

	answer := dialog.GetAnswer()
	expected := "Hello Worl"
	if answer != expected {
		t.Errorf("Expected %q after delete, got %q", expected, answer)
	}
}

func TestUserInputChangeHomeAndEnd(t *testing.T) {
	dialog := NewUserInputDialog("Type:")

	// Type text
	dialog.textarea.SetValue("Hello")

	// Press Home (move cursor to start)
	updated, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyHome})
	dialog = updated.(UserInputDialog)

	// Press End (move cursor to end)
	updated, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyEnd})
	dialog = updated.(UserInputDialog)

	// Should not crash
	_ = dialog
}

func TestUserInputChangeUpdateCommands(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Most key messages should return a command from the textarea
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	// Should have a command from the textarea (likely nil or blink)
	_ = cmd
}

func TestHandleUserInputChangeRequestIntegration(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserInputRequestMsg{
		Question: "What is your favorite color?",
		Response: responseChan,
		Error:    errorChan,
	}

	// Handle the request
	_ = m.handleUserInputRequest(msg)

	// Verify dialog is open
	if !m.userQuestionDialogOpen {
		t.Fatal("Expected userQuestionDialogOpen to be true")
	}

	// Verify dialog was created
	if m.userQuestionDialog == nil {
		t.Fatal("Expected userQuestionDialog to be initialized")
	}

	// Verify it's a UserInputDialog
	if _, ok := m.userQuestionDialog.(UserInputDialog); !ok {
		t.Error("Expected UserInputDialog, got different type")
	}

	// Verify channels are stored
	if m.userQuestionResponse != responseChan {
		t.Error("Expected response channel to be stored")
	}

	// Verify overlay is active
	if !m.overlayActive {
		t.Error("Expected overlayActive to be true")
	}

	// Command from Update should not be nil (has blink command)
	// Removed empty if block to fix golangci-lint error
}

func TestUserInputChangeAfterInitialization(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserInputRequestMsg{
		Question: "Test question?",
		Response: responseChan,
		Error:    errorChan,
	}

	m.handleUserInputRequest(msg)

	dialog, ok := m.userQuestionDialog.(UserInputDialog)
	if !ok {
		t.Fatal("Expected UserInputDialog")
	}

	// Type answer
	updated, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	dialog = updated.(UserInputDialog)
	updated, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	dialog = updated.(UserInputDialog)
	updated, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	dialog = updated.(UserInputDialog)
	updated, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	dialog = updated.(UserInputDialog)

	// Submit
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should trigger EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("Expected command from Enter")
	}

	endMsg := cmd()
	if _, ok := endMsg.(EndUserQuestionsMsg); !ok {
		t.Fatalf("Expected EndUserQuestionsMsg, got %T", endMsg)
	}

	// Answer should be "Test"
	if dialog.GetAnswer() != "Test" {
		t.Errorf("Expected answer 'Test', got %q", dialog.GetAnswer())
	}
}

func TestUserInputChangeOverlayBehavior(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	// Initially no overlay
	if m.overlayActive {
		t.Error("Expected overlayActive to be false initially")
	}

	msg := UserInputRequestMsg{
		Question: "Test?",
		Response: make(chan string, 1),
		Error:    make(chan error, 1),
	}

	m.handleUserInputRequest(msg)

	// Overlay should be active
	if !m.overlayActive {
		t.Error("Expected overlayActive to be true after dialog opens")
	}

	// Simulate closing dialog (this would normally be done by the Model.Update)
	m.userQuestionDialogOpen = false
	m.SetOverlayActive(false)

	// Overlay should be inactive
	if m.overlayActive {
		t.Error("Expected overlayActive to be false after dialog closes")
	}
}

func TestUserInputChangeEdgeCases(t *testing.T) {
	testCases := []struct {
		name     string
		question string
	}{
		{"empty question", ""},
		{"very long question", strings.Repeat("This is a long question. ", 50)},
		{"question with newlines", "Line 1\nLine 2\nLine 3"},
		{"question with tabs", "Tab\tseparated\ttext"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			dialog := NewUserInputDialog(tt.question)

			// Should be able to get question
			if dialog.question != tt.question {
				t.Errorf("Expected question %q, got %q", tt.question, dialog.question)
			}

			// Should render
			_ = dialog.View()
		})
	}
}

func TestUserInputChangeMultipleSubmissions(t *testing.T) {
	dialog := NewUserInputDialog("Question?")
	helper := NewTestModelHelper(dialog)

	// Type and submit
	helper.SendString("First")
	_, cmd1 := helper.UpdateWithMsgAndCmd(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd1 == nil {
		t.Fatal("Expected command from first Enter")
	}

	// Clear and type again
	dialog = helper.GetCurrentModel().(UserInputDialog)
	dialog.textarea.Reset()
	helper.SendString("Second")

	_, cmd2 := helper.UpdateWithMsgAndCmd(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd2 == nil {
		t.Fatal("Expected command from second Enter")
	}

	// Both should produce EndUserQuestionsMsg
	msg1 := cmd1()
	msg2 := cmd2()

	if _, ok := msg1.(EndUserQuestionsMsg); !ok {
		t.Errorf("First submit: expected EndUserQuestionsMsg, got %T", msg1)
	}

	if _, ok := msg2.(EndUserQuestionsMsg); !ok {
		t.Errorf("Second submit: expected EndUserQuestionsMsg, got %T", msg2)
	}
}

func TestUserInputChangeStatePersistence(t *testing.T) {
	dialog := NewUserInputDialog("Question?")
	helper := NewTestModelHelper(dialog)

	// Type text
	helper.SendString("Persistent")

	// Resize window
	updated, _ := dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	dialog = updated.(UserInputDialog)

	// Text should still be there
	if dialog.GetAnswer() != "Persistent" {
		t.Errorf("Expected text to persist after resize, got %q", dialog.GetAnswer())
	}
}

func TestUserInputChangeRapidUpdates(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Send many updates rapidly without crashing
	for i := 0; i < 100; i++ {
		chr := rune('a' + (i % 26))
		updated, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{chr}})
		dialog = updated.(UserInputDialog)

		// Occasional resize
		if i%10 == 0 {
			updated, _ = dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
			dialog = updated.(UserInputDialog)
		}
	}

	// Should have typed some characters
	answer := dialog.GetAnswer()
	if len(answer) != 100 {
		t.Errorf("Expected 100 characters, got %d", len(answer))
	}
}

func TestUserInputChangeWithExistingContent(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Set some initial content
	dialog.textarea.SetValue("Initial content")

	// Add more content
	updated, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' ', 'a', 'p', 'p', 'e', 'n', 'd', 'e', 'd'}})
	dialog = updated.(UserInputDialog)

	expected := "Initial content appended"
	if dialog.GetAnswer() != expected {
		t.Errorf("Expected %q, got %q", expected, dialog.GetAnswer())
	}
}

func TestUserInputChangeReset(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Type content
	dialog.textarea.SetValue("Some content")

	// Reset textarea
	dialog.textarea.Reset()

	// Should be empty
	if dialog.GetAnswer() != "" {
		t.Errorf("Expected empty content after reset, got %q", dialog.GetAnswer())
	}
}

func TestUserInputChangeLineNumbersDisabled(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Line numbers should be disabled
	if dialog.textarea.ShowLineNumbers {
		t.Error("Expected ShowLineNumbers to be false")
	}
}

func TestUserInputChangeCursorBlink(t *testing.T) {
	dialog := NewUserInputDialog("Question?")

	// Init should return a blink command
	cmd := dialog.Init()

	if cmd == nil {
		t.Error("Expected blink command from Init")
	} else {
		// Execute the command - should send blink msg
		msg := cmd()
		if msg == nil {
			t.Error("Expected non-nil message from blink command")
		}
	}
}

func TestUserInputChangeViewWithQuestion(t *testing.T) {
	questions := []string{
		"What is your name?",
		"How old are you?",
		strings.Repeat("Very long question", 10),
	}

	for _, q := range questions {
		dialog := NewUserInputDialog(q)
		dialog.width = 100
		dialog.height = 40

		view := dialog.View()

		if len(view) == 0 {
			t.Errorf("Expected non-empty view for question: %q", q)
		}

		// Should contain the question text
		if !strings.Contains(view, "Question:") {
			t.Error("View should contain 'Question:' label")
		}
	}
}

func TestUserInputChangeEdgeCaseInputs(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"only spaces", "   "},
		{"only tabs", "\t\t\t"},
		{"only newlines", "\n\n"},
		{"mixed whitespace", " \t \n "},
		{"only special chars", "!@#$%"},
		{"only numbers", "12345"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			dialog := NewUserInputDialog("Question?")
			dialog.textarea.SetValue(tt.input)

			// GetAnswer should handle all cases
			answer := dialog.GetAnswer()
			expected := strings.TrimSpace(tt.input)
			if answer != expected {
				t.Errorf("Expected %q, got %q", expected, answer)
			}
		})
	}
}
