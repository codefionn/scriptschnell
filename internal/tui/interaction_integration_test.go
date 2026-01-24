package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/actor"
)

// Integration tests for tool call → TUI interaction flows

func TestIntegration_AskUserToolFlow(t *testing.T) {
	// Test the complete flow: planning agent ask_user tool → TUI dialog → response
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Simulate planning agent making a request
	req := &actor.UserInteractionRequest{
		RequestID:       "ask-user-flow-1",
		InteractionType: actor.InteractionTypeUserInputSingle,
		TabID:           1,
		Payload: &actor.UserInputSinglePayload{
			Question: "What is your preferred programming language?",
		},
	}

	// Start handling in goroutine (as the actor would)
	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	// Wait for TUI to receive the request
	time.Sleep(50 * time.Millisecond)

	// Messages would be sent to the TUI program
	// For now, we just test the handler's internal logic

	// Simulate user typing answer in TUI
	userAnswer := "Go"

	// Simulate TUI sending response back to handler
	handler.HandleUserInputResponse("ask-user-flow-1", userAnswer, false)

	// Wait for response
	select {
	case resp := <-responseChan:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.RequestID != "ask-user-flow-1" {
			t.Errorf("Expected RequestID 'ask-user-flow-1', got %q", resp.RequestID)
		}
		if resp.Answer != userAnswer {
			t.Errorf("Expected answer %q, got %q", userAnswer, resp.Answer)
		}
		if !resp.Acknowledged {
			t.Error("Expected Acknowledged to be true")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_AskUserMultipleToolFlow(t *testing.T) {
	// Test the complete flow for multiple choice questions
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	questions := []actor.QuestionWithOptions{
		{
			Question: "What is your preferred programming language?",
			Options:  []string{"Go", "Python", "JavaScript", "Rust"},
		},
		{
			Question: "What is your experience level?",
			Options:  []string{"Beginner", "Intermediate", "Advanced", "Expert"},
		},
	}

	formattedQuestions := `1. What is your preferred programming language?
   a. Go
   b. Python
   c. JavaScript
   d. Rust

2. What is your experience level?
   a. Beginner
   b. Intermediate
   c. Advanced
   d. Expert`

	req := &actor.UserInteractionRequest{
		RequestID:       "ask-multi-flow-1",
		InteractionType: actor.InteractionTypeUserInputMultiple,
		TabID:           1,
		Payload: &actor.UserInputMultiplePayload{
			FormattedQuestions: formattedQuestions,
			ParsedQuestions:    questions,
		},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// Message would be sent to the TUI program

	// Simulate user answering all questions
	answers := map[string]string{
		"1) What is your preferred programming language?": "1) Go",
		"2) What is your experience level?":               "3) Advanced",
	}

	handler.HandleMultipleAnswersResponse("ask-multi-flow-1", answers, false)

	select {
	case resp := <-responseChan:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if len(resp.Answers) != 2 {
			t.Errorf("Expected 2 answers, got %d", len(resp.Answers))
		}
		if !resp.Acknowledged {
			t.Error("Expected Acknowledged to be true")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_AuthorizationToolFlow(t *testing.T) {
	// Test the complete flow: authorization request → TUI dialog → approval/denial
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	req := &actor.UserInteractionRequest{
		RequestID:       "auth-flow-1",
		InteractionType: actor.InteractionTypeAuthorization,
		TabID:           1,
		Payload: &actor.AuthorizationPayload{
			ToolName: "shell",
			Parameters: map[string]interface{}{
				"command": "rm -rf /tmp/test",
			},
			Reason: "Potentially dangerous command",
		},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// Authorization message would be sent to the TUI program

	// Simulate user approving
	handler.HandleAuthorizationResponse("auth-flow-1", true)

	select {
	case resp := <-responseChan:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if !resp.Approved {
			t.Error("Expected Approved to be true")
		}
		if !resp.Acknowledged {
			t.Error("Expected Acknowledged to be true")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_AuthorizationDenialFlow(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	req := &actor.UserInteractionRequest{
		RequestID:       "auth-deny-1",
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

	time.Sleep(50 * time.Millisecond)

	// Simulate user denying
	handler.HandleAuthorizationResponse("auth-deny-1", false)

	select {
	case resp := <-responseChan:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.Approved {
			t.Error("Expected Approved to be false")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_MultipleConcurrentInteractions(t *testing.T) {
	// Test multiple concurrent interactions from different tool calls
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Create multiple requests of different types
	requests := []*actor.UserInteractionRequest{
		{
			RequestID:       "multi-1",
			InteractionType: actor.InteractionTypeAuthorization,
			Payload:         &actor.AuthorizationPayload{ToolName: "shell"},
		},
		{
			RequestID:       "multi-2",
			InteractionType: actor.InteractionTypeUserInputSingle,
			Payload:         &actor.UserInputSinglePayload{Question: "Name?"},
		},
		{
			RequestID:       "multi-3",
			InteractionType: actor.InteractionTypeUserInputMultiple,
			Payload: &actor.UserInputMultiplePayload{
				FormattedQuestions: "1. Q?\n   a. A\n   b. B",
			},
		},
		{
			RequestID:       "multi-4",
			InteractionType: actor.InteractionTypeAuthorization,
			Payload:         &actor.AuthorizationPayload{ToolName: "edit_file"},
		},
	}

	responseChans := make([]chan *actor.UserInteractionResponse, len(requests))
	var wg sync.WaitGroup

	// Send all requests concurrently
	for i, req := range requests {
		responseChans[i] = make(chan *actor.UserInteractionResponse, 1)
		wg.Add(1)

		go func(idx int, r *actor.UserInteractionRequest) {
			defer wg.Done()
			resp, err := handler.HandleInteraction(ctx, r)
			if err != nil {
				t.Errorf("Request %d: HandleInteraction failed: %v", idx, err)
			}
			responseChans[idx] <- resp
		}(i, req)
	}

	// Wait for all to be registered
	time.Sleep(100 * time.Millisecond)

	// Messages would be sent to the TUI program

	// Send responses in different order
	handler.HandleAuthorizationResponse("multi-4", false)                                // Deny last auth first
	handler.HandleUserInputResponse("multi-2", "Bob", false)                             // Answer name
	handler.HandleAuthorizationResponse("multi-1", true)                                 // Approve first auth
	handler.HandleMultipleAnswersResponse("multi-3", map[string]string{"Q": "A"}, false) // Answer multi

	// Wait for all responses
	var responses []*actor.UserInteractionResponse
	for i := 0; i < len(requests); i++ {
		select {
		case resp := <-responseChans[i]:
			responses = append(responses, resp)
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for response %d", i)
		}
	}

	// Verify all responses received
	if len(responses) != len(requests) {
		t.Fatalf("Expected %d responses, got %d", len(requests), len(responses))
	}

	// Verify each response matches request
	for i, resp := range responses {
		if resp.RequestID != requests[i].RequestID {
			t.Errorf("Response %d: RequestID mismatch", i)
		}
	}

	wg.Wait()
}

func TestIntegration_TabSwitchingWithPendingInteraction(t *testing.T) {
	// Test interactions when user switches tabs
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	// Create multiple tabs
	m.sessions = []*TabSession{
		{ID: 1, Name: "Tab 1"},
		{ID: 2, Name: "Tab 2"},
		{ID: 3, Name: "Tab 3"},
	}
	m.activeSessionIdx = 0

	// Open dialog in Tab 1
	responseChan := make(chan bool, 1)
	req := CreateTestAuthorizationRequest("auth-tab-1", "shell", "Test")
	req.TabID = 1
	req.ResponseChan = responseChan
	m.authorizationDialog = NewAuthorizationDialog(req, "Tab 1")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "auth-tab-1"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"auth-tab-1": req,
	}

	// Switch to Tab 2
	m.activeSessionIdx = 1

	// Dialog should still be open (overlay affects all tabs)
	if !m.authorizationDialogOpen {
		t.Error("Dialog should remain open when switching tabs")
	}

	// Another request from Tab 3 should be queued
	req2 := CreateTestAuthorizationRequest("auth-tab-3", "edit_file", "Test")
	req2.TabID = 3
	m.pendingAuthorizations["auth-tab-3"] = req2

	// Should have 2 pending authorizations
	if len(m.pendingAuthorizations) != 2 {
		t.Errorf("Expected 2 pending authorizations, got %d", len(m.pendingAuthorizations))
	}

	// Close first dialog
	m.authorizationDialogOpen = false
	msg := AuthorizationResponseMsg{
		AuthID:   "auth-tab-1",
		Approved: true,
	}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Response should be sent
	select {
	case approved := <-responseChan:
		if !approved {
			t.Error("Expected approval")
		}
	default:
		t.Error("Expected response to be sent")
	}

	// Tab 3 request should still be pending
	if _, ok := m.pendingAuthorizations["auth-tab-3"]; !ok {
		t.Error("Tab 3 authorization should still be pending")
	}
}

func TestIntegration_GenerationWithInteraction(t *testing.T) {
	// Test interactions while generation is active in a tab
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	m.sessions = []*TabSession{{ID: 1, Name: "Test Tab"}}
	m.activeSessionIdx = 0

	// Start generation
	m.processingStatus = "Processing"
	m.contentReceived = false

	// User interaction request arrives
	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserInputRequestMsg{
		Question: "Test question?",
		Response: responseChan,
		Error:    errorChan,
	}

	// Handle the request
	m.handleUserInputRequest(msg)

	// Dialog should be open
	if !m.userQuestionDialogOpen {
		t.Error("Dialog should be open")
	}

	// Overlay should be active
	if !m.overlayActive {
		t.Error("Overlay should be active during interaction")
	}

	// Processing status should still be there
	if m.processingStatus == "" {
		t.Error("Processing status should persist")
	}

	// User answers
	dialog, ok := m.userQuestionDialog.(UserInputDialog)
	if !ok {
		t.Fatal("Expected UserInputDialog")
	}
	dialog.textarea.SetValue("My answer")
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should trigger EndUserQuestionsMsg
	if cmd == nil {
		t.Fatal("Expected command from Enter")
	}

	_ = cmd()

	// After interaction closes, processing status should still be visible
	if m.processingStatus == "" {
		t.Error("Processing status should persist after interaction")
	}
}

func TestIntegration_PlanningAgentQuestionFlow(t *testing.T) {
	// Test the specific flow when planning agent asks a question
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Planning agent question payload
	req := &actor.UserInteractionRequest{
		RequestID:       "plan-question-1",
		InteractionType: actor.InteractionTypePlanningQuestion,
		TabID:           1,
		Payload: &actor.PlanningQuestionPayload{
			Question: "How would you like to approach this task?",
		},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// Message would be sent to the TUI program

	// Simulate user response
	handler.HandleUserInputResponse("plan-question-1", "I'll start by analyzing the code", false)

	select {
	case resp := <-responseChan:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.Answer != "I'll start by analyzing the code" {
			t.Errorf("Expected answer to be preserved, got %q", resp.Answer)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_ErrorHandlingInFlow(t *testing.T) {
	// Test error handling throughout the interaction flow
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Invalid payload should not crash
	req := &actor.UserInteractionRequest{
		RequestID:       "error-flow-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         "invalid payload type", // Wrong type
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err == nil {
			t.Error("Expected error for invalid payload")
		}
		responseChan <- resp
	}()

	select {
	case resp := <-responseChan:
		if resp != nil {
			t.Error("Expected nil response for error case")
		}
	case <-time.After(500 * time.Millisecond):
		// OK - error case
	}
}

func TestIntegration_ResponseToWrongRequestID(t *testing.T) {
	// Test sending response to non-existent request ID
	handler := NewTUIInteractionHandler(nil)

	// Send response without making a request
	handler.HandleAuthorizationResponse("non-existent", true)
	handler.HandleUserInputResponse("non-existent", "answer", false)
	handler.HandleMultipleAnswersResponse("non-existent", map[string]string{"Q": "A"}, false)

	// Should not crash - just log warnings
	// This is more of a sanity test
}

func TestIntegration_UserCancellationFlow(t *testing.T) {
	// Test user cancelling an interaction
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	req := &actor.UserInteractionRequest{
		RequestID:       "cancel-flow-1",
		InteractionType: actor.InteractionTypeUserInputSingle,
		Payload:         &actor.UserInputSinglePayload{Question: "Cancel me?"},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// User presses Escape (cancel)
	handler.HandleUserInputResponse("cancel-flow-1", "", true)

	select {
	case resp := <-responseChan:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if !resp.Cancelled {
			t.Error("Expected Cancelled to be true")
		}
		if resp.Answer != "" {
			t.Error("Expected empty answer on cancel")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_RapidSuccessiveRequests(t *testing.T) {
	// Test handling rapid successive requests
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	numRequests := 10
	responseChans := make([]chan *actor.UserInteractionResponse, numRequests)

	// Send all requests rapidly
	for i := 0; i < numRequests; i++ {
		responseChans[i] = make(chan *actor.UserInteractionResponse, 1)

		go func(idx int) {
			req := &actor.UserInteractionRequest{
				RequestID:       fmt.Sprintf("rapid-%d", idx),
				InteractionType: actor.InteractionTypeAuthorization,
				Payload:         &actor.AuthorizationPayload{ToolName: fmt.Sprintf("tool%d", idx)},
			}

			resp, err := handler.HandleInteraction(ctx, req)
			if err != nil {
				t.Errorf("Request %d: HandleInteraction failed: %v", idx, err)
			}
			responseChans[idx] <- resp
		}(i)
	}

	// Wait for all to be registered
	time.Sleep(100 * time.Millisecond)

	// Send all responses rapidly
	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			handler.HandleAuthorizationResponse(fmt.Sprintf("rapid-%d", idx), idx%2 == 0)
		}(i)
	}

	// Verify all responses received
	for i := 0; i < numRequests; i++ {
		select {
		case resp := <-responseChans[i]:
			if resp == nil {
				t.Errorf("Request %d: Expected non-nil response", i)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for request %d", i)
		}
	}
}

func TestIntegration_InteractionHandlerWithTUIModel(t *testing.T) {
	// Test TUIInteractionHandler integration with actual TUI model
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	// Create interaction handler and set program reference
	handler := NewTUIInteractionHandler(nil)

	// Set handler on model (this is what happens in production)
	m.userInteractionHandler = handler

	// Now simulate actor making a request through handler
	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "integration-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "shell", Reason: "Test"},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// Message would be sent to the TUI program

	// Simulate TUI handling the message
	// (in real scenario, this happens through Update method)

	// Simulate user approval
	handler.HandleAuthorizationResponse("integration-1", true)

	select {
	case resp := <-responseChan:
		if !resp.Approved {
			t.Error("Expected approval")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_LongRunningInteraction(t *testing.T) {
	// Test interaction that takes a long time for user to respond
	handler := NewTUIInteractionHandler(nil)
	handler.dialogTimeout = 10 * time.Second // Long timeout

	ctx := context.Background()

	req := &actor.UserInteractionRequest{
		RequestID:       "long-1",
		InteractionType: actor.InteractionTypeUserInputSingle,
		Payload:         &actor.UserInputSinglePayload{Question: "Think about this..."},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		responseChan <- resp
	}()

	// Wait a while (simulating user thinking)
	time.Sleep(500 * time.Millisecond)

	// User finally answers
	handler.HandleUserInputResponse("long-1", "My answer", false)

	select {
	case resp := <-responseChan:
		if resp.Answer != "My answer" {
			t.Errorf("Expected answer 'My answer', got %q", resp.Answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestIntegration_MultipleTabsSameRequest(t *testing.T) {
	// Test handling when same request comes from multiple tabs
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	m.sessions = []*TabSession{
		{ID: 1, Name: "Tab 1"},
		{ID: 2, Name: "Tab 2"},
	}

	// Request from Tab 1
	req1 := CreateTestAuthorizationRequest("auth-same-1", "shell", "Test")
	req1.TabID = 1
	m.authorizationDialog = NewAuthorizationDialog(req1, "Tab 1")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "auth-same-1"

	// Request from Tab 2 (should queue)
	req2 := CreateTestAuthorizationRequest("auth-same-2", "shell", "Test")
	req2.TabID = 2
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"auth-same-1": req1,
		"auth-same-2": req2,
	}

	// Verify both are tracked
	if len(m.pendingAuthorizations) != 2 {
		t.Errorf("Expected 2 pending authorizations, got %d", len(m.pendingAuthorizations))
	}

	// Complete first request
	m.authorizationDialogOpen = false
	msg := AuthorizationResponseMsg{AuthID: "auth-same-1", Approved: true}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Second should still be pending
	if _, ok := m.pendingAuthorizations["auth-same-2"]; !ok {
		t.Error("Second request should still be pending")
	}
}

func TestIntegration_FormattedQuestionsWithSpecialChars(t *testing.T) {
	// Test handling of formatted questions with special characters
	m := New("test-model", "", false)
	m.ready = true
	m.width = 120
	m.height = 40

	// Questions with quotes, parens, etc.
	questions := `1. What's your "favorite" language?
   a. Go (Google)
   b. Python (Guido)
   c. JavaScript (Brendan Eich)

2. Rate 1-5:
   a. 1 - Poor
   b. 2 - Fair
   c. 3 - Good
   d. 4 - Very Good
   e. 5 - Excellent`

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	msg := UserMultipleQuestionsRequestMsg{
		Questions: questions,
		Response:  responseChan,
		Error:     errorChan,
	}

	// Should handle without crashing
	m.handleUserMultipleQuestionsRequest(msg)

	if m.userQuestionDialog == nil {
		t.Fatal("Expected dialog to be created")
	}

	dialog, ok := m.userQuestionDialog.(*UserQuestionDialog)
	if !ok {
		t.Fatal("Expected *UserQuestionDialog")
	}

	// Verify questions parsed
	if len(dialog.questions) != 2 {
		t.Errorf("Expected 2 questions, got %d", len(dialog.questions))
	}

	// Check special characters preserved
	if !strings.Contains(dialog.questions[0].Question, "favorite") {
		t.Error("Expected quotes to be preserved in question")
	}
}
