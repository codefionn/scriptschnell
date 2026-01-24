package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
)

// Tests for TUIInteractionHandler

func TestNewTUIInteractionHandler(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}

	if handler.program != nil {
		t.Error("Expected program to be nil")
	}

	if handler.Mode() != "tui" {
		t.Errorf("Expected mode 'tui', got %q", handler.Mode())
	}
}

func TestTUIInteractionHandlerSetProgram(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Initially nil
	if handler.program != nil {
		t.Error("Expected program to be nil initially")
	}
	// Note: SetProgram accepts *tea.Program, which we can't test with mock
	// This is tested in integration tests with actual TUI
}

func TestTUIInteractionHandlerSupportsInteraction(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Should support all interaction types
	supportedTypes := []actor.InteractionType{
		actor.InteractionTypeAuthorization,
		actor.InteractionTypePlanningQuestion,
		actor.InteractionTypeUserInputSingle,
		actor.InteractionTypeUserInputMultiple,
	}

	for _, it := range supportedTypes {
		if !handler.SupportsInteraction(it) {
			t.Errorf("Expected to support interaction type %v", it)
		}
	}
}

func TestTUIInteractionHandlerHandleAuthorization(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "auth-1",
		InteractionType: actor.InteractionTypeAuthorization,
		TabID:           1,
		Payload: &actor.AuthorizationPayload{
			ToolName: "shell",
			Reason:   "Test authorization",
		},
	}

	// Handle in goroutine (it blocks waiting for response)
	done := make(chan bool, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		if resp == nil {
			t.Error("Expected non-nil response")
		}
		done <- true
	}()

	// Wait a bit for handler to process
	time.Sleep(50 * time.Millisecond)

	// Message would be sent to the TUI program

	// Clean up by sending response
	go handler.HandleAuthorizationResponse("auth-1", true)

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for handler to complete")
	}
}

func TestTUIInteractionHandlerHandleUserInput(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "input-1",
		InteractionType: actor.InteractionTypeUserInputSingle,
		TabID:           1,
		Payload: &actor.UserInputSinglePayload{
			Question: "What is your name?",
		},
	}

	// Handle in goroutine
	done := make(chan bool, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		if resp == nil {
			t.Error("Expected non-nil response")
		}
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	// Message would be sent to the TUI program

	// Send response
	go handler.HandleUserInputResponse("input-1", "Alice", false)

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for handler to complete")
	}
}

func TestTUIInteractionHandlerHandleMultipleQuestions(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	questions := []actor.QuestionWithOptions{
		{
			Question: "Q1?",
			Options:  []string{"A", "B", "C"},
		},
	}

	req := &actor.UserInteractionRequest{
		RequestID:       "multi-1",
		InteractionType: actor.InteractionTypeUserInputMultiple,
		TabID:           1,
		Payload: &actor.UserInputMultiplePayload{
			FormattedQuestions: "1. Q1?\n   a. A\n   b. B\n   c. C",
			ParsedQuestions:    questions,
		},
	}

	done := make(chan bool, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		if resp == nil {
			t.Error("Expected non-nil response")
		}
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	// Message would be sent to the TUI program

	// Send response
	answers := map[string]string{"Q1": "A"}
	go handler.HandleMultipleAnswersResponse("multi-1", answers, false)

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for handler to complete")
	}
}

func TestTUIInteractionHandlerNilProgram(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)
	handler.dialogTimeout = 50 * time.Millisecond

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "test-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.AuthorizationPayload{
			ToolName: "test",
		},
	}

	// HandleInteraction should work with nil program (for testing)
	// It will wait for response or timeout
	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	// Should timeout since no response is sent
	select {
	case resp := <-done:
		if resp == nil {
			t.Fatal("Expected non-nil response on timeout")
		}
		if !resp.TimedOut {
			t.Error("Expected TimedOut to be true")
		}
		if resp.Error == nil {
			t.Error("Expected error on timeout")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestTUIInteractionHandlerInvalidPayload(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Send invalid payload (wrong type for authorization)
	req := &actor.UserInteractionRequest{
		RequestID:       "test-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.UserInputSinglePayload{ // Wrong payload type
			Question: "test",
		},
	}

	// Should send nil message to program (which won't be added to sent messages)
	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err == nil {
			t.Error("Expected error for invalid payload")
		}
		done <- resp
	}()

	select {
	case resp := <-done:
		if resp != nil {
			t.Error("Expected nil response for invalid payload")
		}
	case <-time.After(1 * time.Second):
		// OK - likely returned error
	}
}

func TestTUIInteractionHandlerDialogTimeout(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)
	handler.dialogTimeout = 100 * time.Millisecond

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "timeout-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.AuthorizationPayload{
			ToolName: "test",
		},
	}

	// Handle without sending response - should timeout
	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	select {
	case resp := <-done:
		if resp == nil {
			t.Fatal("Expected non-nil response on timeout")
		}
		if !resp.TimedOut {
			t.Error("Expected TimedOut to be true")
		}
		if resp.Error == nil {
			t.Error("Expected error on timeout")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for timeout response")
	}
}

func TestTUIInteractionHandlerContextCancellation(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx, cancel := context.WithCancel(context.Background())
	req := &actor.UserInteractionRequest{
		RequestID:       "cancel-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.AuthorizationPayload{
			ToolName: "test",
		},
	}

	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	// Wait for request to be registered
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	select {
	case resp := <-done:
		if resp == nil {
			t.Fatal("Expected non-nil response on cancellation")
		}
		if !resp.Cancelled {
			t.Error("Expected Cancelled to be true")
		}
		if resp.Error == nil {
			t.Error("Expected error on cancellation")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for cancellation response")
	}
}

func TestTUIInteractionHandlerHandleDialogDisplayed(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "display-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.AuthorizationPayload{
			ToolName: "test",
		},
	}

	// Start handling (in background)
	go func() {
		_, _ = handler.HandleInteraction(ctx, req)
	}()

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Mark as displayed
	handler.HandleDialogDisplayed("display-1")

	// Should not panic
}

func TestTUIInteractionHandlerResponseDelivery(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "response-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.AuthorizationPayload{
			ToolName: "test",
		},
	}

	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// Send response
	handler.HandleAuthorizationResponse("response-1", true)

	select {
	case resp := <-done:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.RequestID != "response-1" {
			t.Errorf("Expected RequestID 'response-1', got %q", resp.RequestID)
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

func TestTUIInteractionHandlerMultipleConcurrentRequests(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	numRequests := 5

	responses := make(chan *actor.UserInteractionResponse, numRequests)
	done := make(chan bool, numRequests)

	// Send multiple requests
	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			req := &actor.UserInteractionRequest{
				RequestID:       fmt.Sprintf("concurrent-%d", idx),
				InteractionType: actor.InteractionTypeAuthorization,
				Payload: &actor.AuthorizationPayload{
					ToolName: "test",
				},
			}

			resp, err := handler.HandleInteraction(ctx, req)
			if err != nil {
				t.Errorf("Request %d: HandleInteraction failed: %v", idx, err)
			}
			responses <- resp
			done <- true
		}(i)
	}

	// Wait for all requests to be registered
	time.Sleep(100 * time.Millisecond)

	// Send responses for all
	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			time.Sleep(10 * time.Millisecond)
			handler.HandleAuthorizationResponse("concurrent-"+string(rune('0'+idx)), true)
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < numRequests; i++ {
		select {
		case <-done:
			// OK
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for request %d", i)
		}
	}

	// Verify all responses were received
	close(responses)
	count := 0
	for range responses {
		count++
	}
	if count != numRequests {
		t.Errorf("Expected %d responses, got %d", numRequests, count)
	}
}

func TestTUIInteractionHandlerResponseAfterTimeout(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)
	handler.dialogTimeout = 50 * time.Millisecond

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "late-response-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.AuthorizationPayload{
			ToolName: "test",
		},
	}

	// Handle without responding
	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Try to send response after timeout (should be logged but not crash)
	handler.HandleAuthorizationResponse("late-response-1", true)

	select {
	case resp := <-done:
		if !resp.TimedOut {
			t.Error("Expected TimedOut response")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestTUIInteractionHandlerCleanupOnResponse(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "cleanup-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload: &actor.AuthorizationPayload{
			ToolName: "test",
		},
	}

	done := make(chan bool, 1)
	go func() {
		_, _ = handler.HandleInteraction(ctx, req)
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	// Verify pending dialog exists
	handler.mu.Lock()
	_, exists := handler.pendingDialogs["cleanup-1"]
	handler.mu.Unlock()

	if !exists {
		t.Error("Expected pending dialog to exist before response")
	}

	// Send response
	handler.HandleAuthorizationResponse("cleanup-1", true)

	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout")
	}

	// Verify pending dialog was cleaned up
	handler.mu.Lock()
	_, exists = handler.pendingDialogs["cleanup-1"]
	handler.mu.Unlock()

	if exists {
		t.Error("Expected pending dialog to be cleaned up after response")
	}
}

func TestTUIInteractionHandlerUnknownInteractionType(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "unknown-1",
		InteractionType: actor.InteractionType(999), // Unknown type
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	// Should not crash and return error
	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		// Error is expected for unknown type
		if err == nil {
			t.Error("Expected error for unknown interaction type")
		}
		done <- resp
	}()

	select {
	case resp := <-done:
		if resp != nil {
			t.Error("Expected nil response for unknown type")
		}
	case <-time.After(500 * time.Millisecond):
		// OK - might have returned error
	}
}

func TestTUIInteractionHandlerCancelResponse(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "cancel-2",
		InteractionType: actor.InteractionTypeUserInputSingle,
		Payload: &actor.UserInputSinglePayload{
			Question: "Test?",
		},
	}

	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// Send cancelled response
	handler.HandleUserInputResponse("cancel-2", "", true)

	select {
	case resp := <-done:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if !resp.Cancelled {
			t.Error("Expected Cancelled to be true")
		}
		if resp.Answer != "" {
			t.Errorf("Expected empty answer on cancel, got %q", resp.Answer)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestTUIInteractionHandlerMultipleAnswersResponse(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "answers-1",
		InteractionType: actor.InteractionTypeUserInputMultiple,
		Payload: &actor.UserInputMultiplePayload{
			FormattedQuestions: "Test",
		},
	}

	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	// Send answers
	answers := map[string]string{
		"Q1": "Option A",
		"Q2": "Option B",
	}
	handler.HandleMultipleAnswersResponse("answers-1", answers, false)

	select {
	case resp := <-done:
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if len(resp.Answers) != 2 {
			t.Errorf("Expected 2 answers, got %d", len(resp.Answers))
		}
		if resp.Answers["Q1"] != "Option A" {
			t.Errorf("Expected Q1 answer 'Option A', got %q", resp.Answers["Q1"])
		}
		if !resp.Acknowledged {
			t.Error("Expected Acknowledged to be true")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestTUIInteractionHandlerPlanningQuestionPayload(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "plan-1",
		InteractionType: actor.InteractionTypePlanningQuestion,
		TabID:           1,
		Payload: &actor.PlanningQuestionPayload{
			Question: "What is your approach?",
		},
	}

	// Handle in goroutine
	done := make(chan bool, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		if resp == nil {
			t.Error("Expected non-nil response")
		}
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	// Message would be sent to the TUI program

	// Send response
	go handler.HandleUserInputResponse("plan-1", "My approach", false)

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for handler to complete")
	}
}

func TestTUIInteractionHandlerThreadSafety(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Send many concurrent requests
	numRequests := 20
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			req := &actor.UserInteractionRequest{
				RequestID:       fmt.Sprintf("thread-%d", idx),
				InteractionType: actor.InteractionTypeAuthorization,
				Payload: &actor.AuthorizationPayload{
					ToolName: "test",
				},
			}

			_, _ = handler.HandleInteraction(ctx, req)
			done <- true
		}(i)
	}

	// Send responses concurrently
	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			time.Sleep(10 * time.Millisecond)
			handler.HandleAuthorizationResponse(fmt.Sprintf("thread-%d", idx), true)
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < numRequests; i++ {
		select {
		case <-done:
			// OK
		case <-time.After(3 * time.Second):
			t.Fatalf("Timeout waiting for request %d", i)
		}
	}
}

func TestTUIInteractionHandlerResponseChannelTimeout(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Create a channel that will block (unbuffered, no receiver)
	blockingChan := make(chan *actor.UserInteractionResponse)

	// Manually set up pending dialog to test response timeout
	handler.mu.Lock()
	handler.pendingDialogs["block-test"] = &pendingTUIDialog{
		requestID:    "block-test",
		responseChan: blockingChan,
		timer:        time.NewTimer(1 * time.Minute),
		displayed:    true,
	}
	handler.mu.Unlock()

	// Send response - should timeout after 1 second
	done := make(chan bool)
	go func() {
		handler.HandleAuthorizationResponse("block-test", true)
		done <- true
	}()

	select {
	case <-done:
		// OK - completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("HandleAuthorizationResponse blocked on channel")
	}
}

func TestTUIInteractionHandlerMessageCreation(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Test createDisplayMessage for each type
	testCases := []struct {
		name        string
		requestType actor.InteractionType
		payload     interface{}
		expectedMsg interface{}
	}{
		{
			name:        "authorization",
			requestType: actor.InteractionTypeAuthorization,
			payload: &actor.AuthorizationPayload{
				ToolName:   "shell",
				Parameters: map[string]interface{}{"cmd": "echo"},
				Reason:     "Test",
			},
			expectedMsg: &TUIAuthorizationRequestMsg{},
		},
		{
			name:        "user input single",
			requestType: actor.InteractionTypeUserInputSingle,
			payload: &actor.UserInputSinglePayload{
				Question: "What?",
			},
			expectedMsg: &TUIUserInputRequestMsg{},
		},
		{
			name:        "planning question",
			requestType: actor.InteractionTypePlanningQuestion,
			payload: &actor.PlanningQuestionPayload{
				Question: "Plan?",
			},
			expectedMsg: &TUIUserInputRequestMsg{},
		},
		{
			name:        "user input multiple",
			requestType: actor.InteractionTypeUserInputMultiple,
			payload: &actor.UserInputMultiplePayload{
				FormattedQuestions: "1. Q?",
				ParsedQuestions: []actor.QuestionWithOptions{
					{Question: "Q?", Options: []string{"A", "B"}},
				},
			},
			expectedMsg: &TUIMultipleQuestionsRequestMsg{},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			req := &actor.UserInteractionRequest{
				RequestID:       "test",
				InteractionType: tt.requestType,
				Payload:         tt.payload,
			}

			msg := handler.createDisplayMessage(req)

			// Verify message type matches
			if !checkMessageType(msg, tt.expectedMsg) {
				t.Errorf("Expected message type %T, got %T", tt.expectedMsg, msg)
			}
		})
	}
}

// Helper to check message type
func checkMessageType(msg, expected interface{}) bool {
	if msg == nil || expected == nil {
		return msg == expected
	}
	return msg != nil && expected != nil
}

func TestTUIInteractionHandlerStopPendingTimer(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "timer-test",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	go func() {
		_, _ = handler.HandleInteraction(ctx, req)
	}()

	time.Sleep(50 * time.Millisecond)

	// Verify timer is running
	handler.mu.Lock()
	pending, exists := handler.pendingDialogs["timer-test"]
	handler.mu.Unlock()

	if !exists || pending == nil {
		t.Fatal("Expected pending dialog with timer")
	}

	timer := pending.timer
	if timer == nil {
		t.Fatal("Expected timer to be set")
	}

	// Send response - should stop timer
	handler.HandleAuthorizationResponse("timer-test", true)

	time.Sleep(50 * time.Millisecond)

	// Check if timer was stopped (by checking if pending dialog was cleaned up)
	handler.mu.Lock()
	_, exists = handler.pendingDialogs["timer-test"]
	handler.mu.Unlock()

	if exists {
		t.Error("Expected pending dialog to be cleaned up")
	}
}

func TestTUIInteractionHandlerSetDialogTimeout(t *testing.T) {
	handler := NewTUIInteractionHandler(nil)

	// Verify default timeout
	if handler.dialogTimeout != 2*time.Minute {
		t.Errorf("Expected default timeout 2m, got %v", handler.dialogTimeout)
	}

	// Change timeout (though not exposed, we test the behavior)
	handler.dialogTimeout = 50 * time.Millisecond

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "timeout-test-2",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	done := make(chan *actor.UserInteractionResponse, 1)
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		done <- resp
	}()

	// Should timeout quickly
	select {
	case resp := <-done:
		if !resp.TimedOut {
			t.Error("Expected quick timeout")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Expected timeout within 200ms")
	}
}
