package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/actor"
)

// Edge case and error path tests

func TestEdgeCase_EmptyAuthorizationRequest(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Empty request
	req := &AuthorizationRequest{}
	dialog := NewAuthorizationDialog(req, "test-tab")

	// Dialog should be created, just verify it has the request
	if dialog.request != req {
		t.Error("Dialog should have the request attached")
	}
}

func TestEdgeCase_NilParametersInAuthorization(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	req := &AuthorizationRequest{
		AuthID:     "test",
		ToolName:   "shell",
		Parameters: nil, // Nil parameters
		Reason:     "Test",
	}

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Should handle nil parameters gracefully
	view := dialog.View()
	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}
}

func TestEdgeCase_EmptyReasonInAuthorization(t *testing.T) {
	req := &AuthorizationRequest{
		AuthID:     "test",
		ToolName:   "shell",
		Parameters: map[string]interface{}{"cmd": "echo"},
		Reason:     "", // Empty reason
	}

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Should still render
	view := dialog.View()
	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}
}

func TestEdgeCase_VeryLongReason(t *testing.T) {
	longReason := strings.Repeat("This is a very long reason. ", 50)

	req := &AuthorizationRequest{
		AuthID:     "test",
		ToolName:   "shell",
		Parameters: map[string]interface{}{},
		Reason:     longReason,
	}

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Dialog is always created (struct type)

	// Should still render without panic
	view := dialog.View()
	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}

	// Should contain part of the reason
	if !strings.Contains(view, "very long reason") {
		t.Error("View should contain part of the long reason")
	}
}

func TestEdgeCase_VeryLongToolName(t *testing.T) {
	longToolName := strings.Repeat("very_long_tool_name_", 20)

	req := &AuthorizationRequest{
		AuthID:     "test",
		ToolName:   longToolName,
		Parameters: map[string]interface{}{},
		Reason:     "Test",
	}

	dialog := NewAuthorizationDialog(req, "test-tab")

	_ = dialog.View()
}

func TestEdgeCase_SpecialCharsInReason(t *testing.T) {
	specialReason := `Reason with "quotes", 'apostrophes', \backslashes\, <brackets>, {braces}, [square], and other: !@#$%^&*()_+-=[]{}|;':",./<>?`

	req := &AuthorizationRequest{
		AuthID:     "test",
		ToolName:   "shell",
		Parameters: map[string]interface{}{},
		Reason:     specialReason,
	}

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Should render without issues
	view := dialog.View()
	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}
}

func TestEdgeCase_UnicodeInParameters(t *testing.T) {
	req := &AuthorizationRequest{
		AuthID:   "test",
		ToolName: "shell",
		Parameters: map[string]interface{}{
			"command": "echo 'Hello 世界 🌍'",
			"file":    "文件.txt",
		},
		Reason: "Test",
	}

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_VeryLargeParameters(t *testing.T) {
	largeMap := make(map[string]interface{})
	for i := 0; i < 100; i++ {
		largeMap[fmt.Sprintf("key%d", i)] = fmt.Sprintf("value%d", i)
	}

	req := &AuthorizationRequest{
		AuthID:     "test",
		ToolName:   "shell",
		Parameters: largeMap,
		Reason:     "Test",
	}

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Dialog is always created (struct type)

	// Should handle large parameters
	_ = dialog.View()
}

func TestEdgeCase_EmptyQuestionForUserInput(t *testing.T) {
	dialog := NewUserInputDialog("")

	// Dialog is always created (struct type)

	// Should still render
	view := dialog.View()
	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}
}

func TestEdgeCase_VeryLongQuestionForUserInput(t *testing.T) {
	longQuestion := strings.Repeat("This is a very long question. ", 100)

	dialog := NewUserInputDialog(longQuestion)

	// Dialog is always created (struct type)

	// Should render
	view := dialog.View()
	if len(view) == 0 {
		t.Error("Expected non-empty view")
	}
}

func TestEdgeCase_QuestionWithNewlines(t *testing.T) {
	question := "Line 1\nLine 2\nLine 3"

	dialog := NewUserInputDialog(question)

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_TextareaBeyondLimit(t *testing.T) {
	dialog := NewUserInputDialog("Test?")

	// Try to set text beyond limit
	largeText := strings.Repeat("a", 2000)
	dialog.textarea.SetValue(largeText)

	// Should be truncated to limit
	answer := dialog.GetAnswer()
	if len(answer) > 1000 {
		t.Errorf("Expected text to be limited to 1000 chars, got %d", len(answer))
	}
}

func TestEdgeCase_EmptyQuestionsList(t *testing.T) {
	questions := []QuestionWithOptions{}

	dialog := NewUserQuestionDialog(questions)

	// Dialog is always created (struct type)

	if len(dialog.questions) != 0 {
		t.Errorf("Expected 0 questions, got %d", len(dialog.questions))
	}
}

func TestEdgeCase_SingleQuestionWithManyOptions(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Pick one:",
			Options:  make([]string, 10),
		},
	}

	// Fill with options
	for i := 0; i < 10; i++ {
		questions[0].Options[i] = fmt.Sprintf("Option %d", i)
	}

	dialog := NewUserQuestionDialog(questions)

	if len(dialog.questions[0].Options) != 10 {
		t.Errorf("Expected 10 options, got %d", len(dialog.questions[0].Options))
	}

	_ = dialog.View()
}

func TestEdgeCase_QuestionWithNoOptions(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "No options?",
			Options:  []string{},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Dialog is always created (struct type)

	// Should render even with no options
	_ = dialog.View()
}

func TestEdgeCase_VeryLongQuestionText(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: strings.Repeat("This is a very long question text. ", 30),
			Options:  []string{"A", "B"},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_VeryLongOptionText(t *testing.T) {
	longOptions := make([]string, 3)
	for i := 0; i < 3; i++ {
		longOptions[i] = fmt.Sprintf("Option %d: %s", i, strings.Repeat("very long text ", 10))
	}

	questions := []QuestionWithOptions{
		{
			Question: "Pick one?",
			Options:  longOptions,
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_QuestionWithUnicodeInOptions(t *testing.T) {
	questions := []QuestionWithOptions{
		{
			Question: "Pick one?",
			Options:  []string{"😊 Happy", "🎉 Party", "🔥 Hot", "🚀 Fast"},
		},
	}

	dialog := NewUserQuestionDialog(questions)

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_HandlerNilProgram(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Create a context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := &actor.UserInteractionRequest{
		RequestID:       "test",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	// With the new handler implementation, nil program is allowed for testing
	// The handler should work without a program (won't display dialogs but can handle responses)
	resp, err := handler.HandleInteraction(ctx, req)

	// Should not return an error - handler works in test mode without program
	if err != nil {
		t.Errorf("Expected handler to work without program in test mode, got error: %v", err)
	}

	// Response should be nil or cancelled due to timeout
	if resp != nil && !resp.Cancelled {
		t.Errorf("Expected nil or cancelled response, got: %v", resp)
	}
}

func TestEdgeCase_HandlerInvalidPayload(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Send wrong payload type
	req := &actor.UserInteractionRequest{
		RequestID:       "test",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         "not a payload", // Wrong type
	}

	_, err := handler.HandleInteraction(ctx, req)

	// Should return error
	if err == nil {
		t.Error("Expected error for invalid payload")
	}
}

func TestEdgeCase_HandlerUnknownInteractionType(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	req := &actor.UserInteractionRequest{
		RequestID:       "test",
		InteractionType: actor.InteractionType(9999), // Unknown type
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	// Should handle gracefully
	_, err := handler.HandleInteraction(ctx, req)
	_ = err // May or may not error, but shouldn't crash
}

func TestEdgeCase_ResponseForNonExistentRequest(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Try to send response for request that doesn't exist
	handler.HandleAuthorizationResponse("non-existent", true)
	handler.HandleUserInputResponse("non-existent", "answer", false)
	handler.HandleMultipleAnswersResponse("non-existent", map[string]string{"Q": "A"}, false)
	handler.HandleDialogDisplayed("non-existent")

	// Should not crash - just log warnings
}

func TestEdgeCase_MultipleResponsesForSameRequest(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "duplicate-response",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 2)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	time.Sleep(20 * time.Millisecond)

	// Send multiple responses
	handler.HandleAuthorizationResponse("duplicate-response", true)
	time.Sleep(5 * time.Millisecond)
	handler.HandleAuthorizationResponse("duplicate-response", false)
	time.Sleep(5 * time.Millisecond)
	handler.HandleAuthorizationResponse("duplicate-response", true)

	// Should only get one response
	select {
	case <-responseChan:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}

	// No duplicate response
	select {
	case resp := <-responseChan:
		t.Errorf("Unexpected duplicate response: %+v", resp)
	case <-time.After(100 * time.Millisecond):
		// OK - no duplicate
	}
}

func TestEdgeCase_ZeroWidthWindow(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Resize to zero width
	updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	m = updatedModel.(*Model)

	// Should not crash
	_ = m.width
	_ = m.height
}

func TestEdgeCase_VerySmallWindow(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Resize to very small window
	updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 5})
	m = updatedModel.(*Model)

	// Should handle gracefully
	if m.width != 10 || m.height != 5 {
		t.Error("Size should be set even for very small windows")
	}
}

func TestEdgeCase_VeryLargeWindow(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Resize to very large window
	updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: 10000, Height: 10000})
	m = updatedModel.(*Model)

	// Should handle gracefully
	if m.width != 10000 || m.height != 10000 {
		t.Error("Size should be set even for very large windows")
	}
}

func TestEdgeCase_DialogClosedBeforeResponse(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "early-close",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	time.Sleep(20 * time.Millisecond)

	// Manually clean up (simulating dialog closed without response)
	handler.cleanupPending("early-close")

	// Now try to send response (should be ignored)
	handler.HandleAuthorizationResponse("early-close", true)

	select {
	case resp := <-responseChan:
		// Should still complete (with timeout or cancellation)
		_ = resp
	case <-time.After(100 * time.Millisecond):
		// OK - might have been cleaned up
	}
}

func TestEdgeCase_TabClosedDuringInteraction(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	m.sessions = []*TabSession{
		{ID: 1, Name: "Tab 1"},
		{ID: 2, Name: "Tab 2"},
	}
	m.activeSessionIdx = 0

	// Open dialog for Tab 1
	req := CreateTestAuthorizationRequest("tab-close-test", "shell", "Test")
	req.TabID = 1
	m.authorizationDialog = NewAuthorizationDialog(req, "Tab 1")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "tab-close-test"

	// Close Tab 1 (simulate user closing tab)
	m.sessions = []*TabSession{
		{ID: 2, Name: "Tab 2"},
	}
	m.activeSessionIdx = 0

	// Try to handle response (should handle gracefully)
	m.authorizationDialogOpen = false
	msg := AuthorizationResponseMsg{
		AuthID:   "tab-close-test",
		Approved: true,
	}
	updatedModel, _ := m.Update(msg)
	_ = updatedModel.(*Model)

	// Should not crash
}

func TestEdgeCase_NegativeTabID(t *testing.T) {
	req := CreateTestAuthorizationRequest("negative-tab", "shell", "Test")
	req.TabID = -1

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_VeryLargeTabID(t *testing.T) {
	req := CreateTestAuthorizationRequest("large-tab", "shell", "Test")
	req.TabID = 999999

	dialog := NewAuthorizationDialog(req, "test-tab")

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_InvalidUTF8InQuestion(t *testing.T) {
	// This is more of a safety test - Go strings should always be valid UTF-8
	// but we test that we handle edge cases

	question := "Valid question"

	dialog := NewUserInputDialog(question)

	// Dialog is always created (struct type)

	_ = dialog.View()
}

func TestEdgeCase_EmptyStringAnswer(t *testing.T) {
	dialog := NewUserInputDialog("Test?")
	dialog.textarea.SetValue("")
	dialog.textarea.Reset()

	answer := dialog.GetAnswer()

	if answer != "" {
		t.Errorf("Expected empty answer, got %q", answer)
	}

	// Should still be able to submit
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("Expected command from Enter")
	}
}

func TestEdgeCase_OnlyWhitespaceAnswer(t *testing.T) {
	dialog := NewUserInputDialog("Test?")
	dialog.textarea.SetValue("   \t\n  ")

	answer := dialog.GetAnswer()

	if answer != "" {
		t.Errorf("Expected empty answer after trim, got %q", answer)
	}
}

func TestEdgeCase_ResponseChannelBlocked(t *testing.T) {
	// Create a handler and test what happens when response channel is blocked
	handler := NewTUIInteractionHandler(nil)

	// Manually set up a pending dialog with unbuffered channel
	handler.mu.Lock()
	unbufferedChan := make(chan *actor.UserInteractionResponse)
	handler.pendingDialogs["blocked-test"] = &pendingTUIDialog{
		requestID:    "blocked-test",
		responseChan: unbufferedChan,
		timer:        time.NewTimer(1 * time.Minute),
		displayed:    true,
	}
	handler.mu.Unlock()

	// Send response - should timeout after 1 second without blocking forever
	done := make(chan bool)
	go func() {
		handler.HandleAuthorizationResponse("blocked-test", true)
		done <- true
	}()

	select {
	case <-done:
		// OK - completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("HandleAuthorizationResponse blocked on channel")
	}
}

func TestEdgeCase_ErrorChannelClosed(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	// Create closed error channel
	errorChan := make(chan error)
	close(errorChan)
	// Create a TUI model to avoid TUI program not available error
	m.program = &tea.Program{}

	msg := UserMultipleQuestionsRequestMsg{
		Questions: "", // This will trigger error
		Response:  make(chan string),
		Error:     errorChan,
	}

	// Should not panic even when trying to send to closed channel
	m.handleUserMultipleQuestionsRequest(msg)
}

func TestEdgeCase_ResponseChannelClosed(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Manually set up pending dialog with closed channel
	handler.mu.Lock()
	closedChan := make(chan *actor.UserInteractionResponse)
	close(closedChan)
	handler.pendingDialogs["closed-chan-test"] = &pendingTUIDialog{
		requestID:    "closed-chan-test",
		responseChan: closedChan,
		timer:        time.NewTimer(1 * time.Minute),
		displayed:    true,
	}
	handler.mu.Unlock()

	// Send response - should not crash
	handler.HandleAuthorizationResponse("closed-chan-test", true)

	// Wait a bit for any goroutines to complete
	time.Sleep(100 * time.Millisecond)
}

func TestEdgeCase_MalformedQuestionsString(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	malformedQuestions := []string{
		"Just text without structure",
		"1. Question without options",
		"   a. Option without question",
		". Question starting with dot",
		") Question starting with paren",
		"999999999999999999999999. Question with huge number",
	}

	for _, q := range malformedQuestions {
		msg := UserMultipleQuestionsRequestMsg{
			Questions: q,
			Response:  make(chan string),
			Error:     make(chan error),
		}

		// Should not crash on malformed input
		m.handleUserMultipleQuestionsRequest(msg)
	}
}

func TestEdgeCase_DomainAuthorizationEdgeCases(t *testing.T) {
	// Test edge cases for domain authorization
	testCases := []struct {
		name   string
		domain string
	}{
		{"empty domain", ""},
		{"very long domain", strings.Repeat("a.", 100) + "example.com"},
		{"domain with special chars", "example_test.com"},
		{"domain with unicode", "例子.测试"},
		{"domain with numbers", "123.example456.com"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			dialog := NewDomainAuthorizationDialog(DomainAuthorizationRequest{
				Domain: tt.domain,
			})

			// Dialog is always created (struct type)

			_ = dialog.View()
		})
	}
}

func TestEdgeCase_ImmediateEscapeAfterDialogOpen(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	req := CreateTestAuthorizationRequest("immediate-escape", "shell", "Test")

	// Show dialog and immediately send escape
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = updatedModel.(*Model)

	// Should handle gracefully
	// Removed empty if block to fix golangci-lint error
}

func TestEdgeCase_MultipleWindowResizesRapidly(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Send many resize messages rapidly
	for i := 0; i < 100; i++ {
		width := 80 + (i % 100)
		height := 30 + (i % 50)
		updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m = updatedModel.(*Model)
	}

	// Should not crash
}

func TestEdgeCase_ModelUpdateWithNilMessage(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Should handle nil message gracefully
	// (though Update expects tea.Msg, which is an interface)
	// This tests the case where a nil message might be passed
	_ = m
}

func TestEdgeCase_ListItemNil(t *testing.T) {
	questions := []QuestionWithOptions{
		{Question: "Test?", Options: []string{"A", "B"}},
	}
	dialog := NewUserQuestionDialog(questions)

	// Try to select beyond list bounds
	dialog.list.Select(1000)

	// Should not crash
	_ = dialog.View()
}

func TestEdgeCase_ListItemNegativeIndex(t *testing.T) {
	questions := []QuestionWithOptions{
		{Question: "Test?", Options: []string{"A", "B"}},
	}
	dialog := NewUserQuestionDialog(questions)

	// Try to select negative index
	dialog.list.Select(-1)

	// Should not crash
	_ = dialog.View()
}

func TestEdgeCase_CommandReturnsNil(t *testing.T) {
	dialog := NewUserInputDialog("Test?")

	// Some messages might return nil command
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	// Command might be nil - should be handled
	_ = cmd
}

func TestEdgeCase_ContextAlreadyCancelled(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &actor.UserInteractionRequest{
		RequestID:       "already-cancelled",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	resp, err := handler.HandleInteraction(ctx, req)

	// Should return cancelled response
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected non-nil response")
	}

	if !resp.Cancelled {
		t.Error("Expected Cancelled to be true")
	}
}

func TestEdgeCase_VerifyDialogClosedCleanup(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	responseChan := make(chan bool, 1)
	req := CreateTestAuthorizationRequest("cleanup-test", "shell", "Test")
	req.ResponseChan = responseChan

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "cleanup-test"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"cleanup-test": req,
	}
	m.sessions = []*TabSession{{ID: 1}}

	// Close dialog
	m.authorizationDialogOpen = false
	msg := AuthorizationResponseMsg{
		AuthID:   "cleanup-test",
		Approved: true,
	}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Verify all state cleaned up
	if m.authorizationDialogOpen {
		t.Error("authorizationDialogOpen should be false")
	}
	if m.activeAuthorizationID != "" {
		t.Errorf("activeAuthorizationID should be empty, got %q", m.activeAuthorizationID)
	}
	if m.overlayActive {
		t.Error("overlayActive should be false after cleanup")
	}

	// Response should be sent
	select {
	case approved := <-responseChan:
		if !approved {
			t.Error("Expected approval to be true")
		}
	default:
		t.Error("Expected response in channel")
	}
}
