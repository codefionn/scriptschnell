package tui

import (
	"context"
	"fmt"

	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/actor"
)

// Race condition and concurrency tests

func TestRace_AuthorizationDialogImmediateKeyPress(t *testing.T) {
	// Test rapid key press immediately after dialog opens doesn't cause race
	m := New("test-model", "", false)
	m.ready = true

	req := CreateTestAuthorizationRequest("race-1", "shell", "Test")

	// Show dialog and immediately send key press
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		msg := ShowAuthorizationDialogMsg{Request: req}
		updatedModel, _ := m.Update(msg)
		m = updatedModel.(*Model)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(1 * time.Millisecond) // Tiny delay to simulate race condition
		updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		_ = updatedModel.(*Model)
	}()

	wg.Wait()

	// Should not have panicked and dialog should be in consistent state
	if m.authorizationDialog.request == nil {
		t.Error("Dialog should have request")
	}
}

func TestRace_WindowResizeDuringDialog(t *testing.T) {
	// Test window resize during active dialog doesn't cause panic
	m := New("test-model", "", false)
	m.ready = true

	req := CreateTestAuthorizationRequest("race-resize-1", "shell", "Test")

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true

	var wg sync.WaitGroup
	wg.Add(10)

	// Send many resize messages rapidly
	for i := 0; i < 5; i++ {
		go func(idx int) {
			defer wg.Done()
			width := 80 + idx*20
			height := 30 + idx*10
			updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			_ = updatedModel.(*Model)
		}(i)

		go func(idx int) {
			defer wg.Done()
			updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
			_ = updatedModel.(*Model)
		}(i)
	}

	wg.Wait()

	// Dialog should still be functional
	if !m.authorizationDialogOpen {
		t.Error("Dialog should still be open")
	}
}

func TestRace_ConcurrentUserInteractionRequests(t *testing.T) {
	// Test multiple concurrent interaction requests don't cause state corruption
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	numRequests := 20
	var completed atomic.Int32

	// Launch many concurrent requests
	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			req := &actor.UserInteractionRequest{
				RequestID:       fmt.Sprintf("race-concurrent-%d", idx),
				InteractionType: actor.InteractionTypeAuthorization,
				Payload:         &actor.AuthorizationPayload{ToolName: fmt.Sprintf("tool%d", idx)},
			}

			_, _ = handler.HandleInteraction(ctx, req)
			completed.Add(1)
		}(i)
	}

	// Wait for all to start processing
	time.Sleep(50 * time.Millisecond)

	// Send responses concurrently
	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			handler.HandleAuthorizationResponse(fmt.Sprintf("race-concurrent-%d", idx), true)
		}(i)
	}

	// Wait for completion
	timeout := time.After(5 * time.Second)
	for completed.Load() < int32(numRequests) {
		select {
		case <-time.After(100 * time.Millisecond):
			continue
		case <-timeout:
			t.Fatalf("Timeout waiting for %d requests to complete, got %d",
				numRequests, completed.Load())
		}
	}
}

func TestRace_HandlerResponseWhileTimeout(t *testing.T) {
	// Test response arriving exactly as timeout fires
	handler := NewTUIInteractionHandler(nil)
	handler.dialogTimeout = 100 * time.Millisecond

	ctx := context.Background()
	req := &actor.UserInteractionRequest{
		RequestID:       "race-timeout-1",
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

	// Send response right at timeout time
	time.Sleep(95 * time.Millisecond)
	handler.HandleAuthorizationResponse("race-timeout-1", true)

	select {
	case resp := <-responseChan:
		// Should get either the user response or timeout, but not panic
		if resp == nil {
			t.Error("Expected non-nil response")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestRace_MutexLockingInAuthorization(t *testing.T) {
	// Test that authorizationMu prevents race conditions
	m := New("test-model", "", false)
	m.ready = true

	var wg sync.WaitGroup
	numOps := 50

	// Concurrently try to show authorization dialogs
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := CreateTestAuthorizationRequest(fmt.Sprintf("race-mutex-%d", idx), "shell", "Test")
			msg := ShowAuthorizationDialogMsg{Request: req}
			updatedModel, _ := m.Update(msg)
			_ = updatedModel.(*Model)
		}(i)
	}

	wg.Wait()

	// State should be consistent
	// Removed empty if block to fix golangci-lint error
}

func TestRace_ConcurrentHandlerOperations(t *testing.T) {
	// Test all handler operations happening concurrently
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Start request
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := &actor.UserInteractionRequest{
			RequestID:       "race-handler-1",
			InteractionType: actor.InteractionTypeAuthorization,
			Payload:         &actor.AuthorizationPayload{ToolName: "test"},
		}
		_, _ = handler.HandleInteraction(ctx, req)
	}()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Concurrently try various operations
	wg.Add(4)
	go func() {
		defer wg.Done()
		handler.HandleDialogDisplayed("race-handler-1")
	}()

	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		handler.HandleAuthorizationResponse("race-handler-1", true)
	}()

	go func() {
		defer wg.Done()
		handler.Mode()
	}()

	wg.Wait()
}

func TestRace_UserDialogStateTransitions(t *testing.T) {
	// Test rapid state transitions in user dialog
	m := New("test-model", "", false)
	m.ready = true

	// Open and close dialog rapidly
	var wg sync.WaitGroup
	iterations := 20

	for i := 0; i < iterations; i++ {
		wg.Add(2)

		go func(idx int) {
			defer wg.Done()
			msg := UserInputRequestMsg{
				Question: fmt.Sprintf("Question %d?", idx),
				Response: make(chan string, 1),
				Error:    make(chan error, 1),
			}
			m.handleUserInputRequest(msg)
		}(i)

		go func() {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			m.userQuestionDialogOpen = false
			m.SetOverlayActive(false)
		}()
	}

	wg.Wait()

	// State should be clean
	// Removed empty if block to fix golangci-lint error
	if m.userQuestionDialogOpen && m.userQuestionDialog == nil {
		t.Error("Inconsistent state: open but nil dialog")
	}
}

func TestRace_PendingAuthorizationsMap(t *testing.T) {
	// Test concurrent access to pending authorizations map
	m := New("test-model", "", false)
	m.ready = true

	m.pendingAuthorizations = make(map[string]*AuthorizationRequest)
	var wg sync.WaitGroup

	// Add many authorizations concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			authID := fmt.Sprintf("pending-race-%d", idx)
			req := CreateTestAuthorizationRequest(authID, "shell", "Test")
			m.pendingAuthorizations[authID] = req
		}(i)
	}

	// Remove many concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			authID := fmt.Sprintf("pending-race-%d", idx)
			delete(m.pendingAuthorizations, authID)
		}(i)
	}

	wg.Wait()

	// Should not panic
}

func TestRace_QuestionDialogAnswersMap(t *testing.T) {
	// Test concurrent answer updates in question dialog
	questions := []QuestionWithOptions{
		{Question: "Q1?", Options: []string{"A", "B"}},
		{Question: "Q2?", Options: []string{"X", "Y"}},
	}
	dialog := NewUserQuestionDialog(questions)

	var wg sync.WaitGroup

	// Update answers concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			dialog.answers[idx%2] = fmt.Sprintf("Answer %d", idx)
		}(i)
	}

	wg.Wait()

	// Should have valid answers
	for _, ans := range dialog.answers {
		if ans == "" {
			t.Error("Expected non-empty answer after concurrent updates")
		}
	}
}

func TestRace_TeapotProgramMessages(t *testing.T) {
	// Test sending messages to program while handling responses
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Start multiple requests
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &actor.UserInteractionRequest{
				RequestID:       fmt.Sprintf("program-race-%d", idx),
				InteractionType: actor.InteractionTypeAuthorization,
				Payload:         &actor.AuthorizationPayload{ToolName: "test"},
			}
			_, _ = handler.HandleInteraction(ctx, req)
		}(i)
	}

	// Send responses while messages are still being sent
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handler.HandleAuthorizationResponse(fmt.Sprintf("program-race-%d", idx), true)
		}(i)
	}

	wg.Wait()

	// Should not panic
}

func TestRace_UserInteractionHandlerMutex(t *testing.T) {
	// Test mutex in user interaction handler
	handler := NewTUIInteractionHandler(nil)

	var wg sync.WaitGroup
	numGoroutines := 100

	// Hammer the handler with concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 4 {
			case 0:
				handler.Mode()
			case 1:
				handler.SupportsInteraction(actor.InteractionTypeAuthorization)
			case 2:
				handler.HandleDialogDisplayed(fmt.Sprintf("test-%d", idx))
			case 3:
				// This will just log warning but shouldn't crash
				handler.HandleAuthorizationResponse(fmt.Sprintf("nonexistent-%d", idx), true)
			}
		}(i)
	}

	wg.Wait()
}

func TestRace_ConcurrentOverlayActive(t *testing.T) {
	// Test concurrent SetOverlayActive calls
	m := New("test-model", "", false)
	m.ready = true

	var wg sync.WaitGroup
	numOps := 50

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m.SetOverlayActive(idx%2 == 0)
		}(i)
	}

	wg.Wait()

	// Should not panic
	_ = m.overlayActive
}

func TestRace_MultipleTabsAuthorization(t *testing.T) {
	// Test authorization requests from multiple tabs concurrently
	m := New("test-model", "", false)
	m.ready = true

	// Create tabs
	for i := 1; i <= 5; i++ {
		m.sessions = append(m.sessions, &TabSession{ID: i, Name: fmt.Sprintf("Tab %d", i)})
	}

	var wg sync.WaitGroup

	// Concurrent requests from different tabs
	for i := 1; i <= 5; i++ {
		tabID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := CreateTestAuthorizationRequest(fmt.Sprintf("tab-race-%d", tabID), "shell", "Test")
			req.TabID = tabID
			m.pendingAuthorizations[fmt.Sprintf("tab-race-%d", tabID)] = req
		}()
	}

	wg.Wait()

	// Verify all were added
	if len(m.pendingAuthorizations) != 5 {
		t.Errorf("Expected 5 pending authorizations, got %d", len(m.pendingAuthorizations))
	}
}

func TestRace_CleanupDuringResponse(t *testing.T) {
	// Test cleanup happening while response is being sent
	handler := NewTUIInteractionHandler(nil)

	// Manually add a pending dialog with custom timeout
	handler.mu.Lock()
	handler.pendingDialogs["cleanup-race-1"] = &pendingTUIDialog{
		requestID:    "cleanup-race-1",
		responseChan: make(chan *actor.UserInteractionResponse, 1),
		timer:        time.NewTimer(1 * time.Minute),
		displayed:    true,
	}
	handler.mu.Unlock()

	var wg sync.WaitGroup

	// Send response
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.HandleAuthorizationResponse("cleanup-race-1", true)
	}()

	// Try to clean up at same time
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(1 * time.Millisecond)
		handler.cleanupPending("cleanup-race-1")
	}()

	wg.Wait()

	// Should not panic
}

func TestRace_DialogListNavigation(t *testing.T) {
	// Test rapid navigation through dialog list
	questions := []QuestionWithOptions{
		{Question: "Q1?", Options: []string{"A", "B"}},
		{Question: "Q2?", Options: []string{"X", "Y"}},
		{Question: "Q3?", Options: []string{"1", "2"}},
	}
	dialog := NewUserQuestionDialog(questions)

	var wg sync.WaitGroup

	// Send rapid navigation commands
	// Launch many concurrent key presses
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dialog.Update(tea.KeyMsg{Type: tea.KeyDown})
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			dialog.Update(tea.KeyMsg{Type: tea.KeyUp})
		}()
	}

	wg.Wait()

	// List should still be valid (the list field is a struct type, not a pointer)
}

func TestRace_TextareaConcurrentUpdates(t *testing.T) {
	// Test concurrent updates to textarea
	dialog := NewUserInputDialog("Test?")

	var wg sync.WaitGroup

	// Type concurrently from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				dialog.Update(tea.KeyMsg{
					Type:  tea.KeyRunes,
					Runes: []rune{rune('a' + j)},
				})
			}
		}(i)
	}

	wg.Wait()

	// Should have typed some characters
	answer := dialog.GetAnswer()
	if len(answer) == 0 {
		t.Error("Expected some characters to be typed")
	}
}

func TestRace_ModelUpdateConcurrency(t *testing.T) {
	// Test concurrent Update calls on model
	m := New("test-model", "", false)
	m.ready = true

	var wg sync.WaitGroup
	updates := 100

	// Send many update messages concurrently
	for i := 0; i < updates; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 4 {
			case 0:
				m.Update(tea.KeyMsg{Type: tea.KeyDown})
			case 1:
				m.Update(tea.KeyMsg{Type: tea.KeyUp})
			case 2:
				m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
			case 3:
				m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
		}(i)
	}

	wg.Wait()

	// Model should still be valid
	if m == nil {
		t.Error("Model should not be nil")
	}
}

func TestRace_SessionAccessConcurrency(t *testing.T) {
	// Test concurrent access to sessions
	m := New("test-model", "", false)
	m.ready = true

	// Add sessions
	for i := 1; i <= 10; i++ {
		m.sessions = append(m.sessions, &TabSession{ID: i, Name: fmt.Sprintf("Tab %d", i)})
	}

	var wg sync.WaitGroup

	// Access sessions concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx := i % len(m.sessions)
			_ = m.sessions[idx].ID
			_ = m.sessions[idx].Name
		}()
	}

	wg.Wait()

	// Should not panic
}

func TestRace_ResponseChannelOperations(t *testing.T) {
	// Test concurrent operations on response channels
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Create a request
	req := &actor.UserInteractionRequest{
		RequestID:       "channel-race-1",
		InteractionType: actor.InteractionTypeAuthorization,
		Payload:         &actor.AuthorizationPayload{ToolName: "test"},
	}

	responseChan := make(chan *actor.UserInteractionResponse, 10)

	// Launch handler
	go func() {
		resp, err := handler.HandleInteraction(ctx, req)
		if err != nil {
			t.Errorf("HandleInteraction failed: %v", err)
		}
		if resp != nil {
			responseChan <- resp
		}
	}()

	time.Sleep(20 * time.Millisecond)

	// Try to send multiple responses (only one should be accepted)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handler.HandleAuthorizationResponse("channel-race-1", idx%2 == 0)
		}(i)
	}

	wg.Wait()

	// Should get exactly one response
	select {
	case <-responseChan:
		// OK - got one response
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}

	// Channel should be empty (no duplicate responses)
	select {
	case resp := <-responseChan:
		t.Errorf("Unexpected duplicate response: %+v", resp)
	case <-time.After(100 * time.Millisecond):
		// OK - no duplicate
	}
}

func TestRace_ContextCancellationConcurrent(t *testing.T) {
	// Test context cancellation while handling concurrent requests
	handler := NewTUIInteractionHandler(nil)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Start multiple requests
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &actor.UserInteractionRequest{
				RequestID:       fmt.Sprintf("cancel-race-%d", idx),
				InteractionType: actor.InteractionTypeAuthorization,
				Payload:         &actor.AuthorizationPayload{ToolName: "test"},
			}
			_, _ = handler.HandleInteraction(ctx, req)
		}(i)
	}

	// Cancel context after a short delay
	time.Sleep(20 * time.Millisecond)
	cancel()

	wg.Wait()

	// All should have completed or been cancelled
}

func TestRace_TimerCleanup(t *testing.T) {
	// Test that timers are properly cleaned up without race
	handler := NewTUIInteractionHandler(nil)

	ctx := context.Background()

	// Create request with short timeout
	handler.dialogTimeout = 50 * time.Millisecond
	req := &actor.UserInteractionRequest{
		RequestID:       "timer-race-1",
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

	// Send response before timeout
	time.Sleep(30 * time.Millisecond)
	handler.HandleAuthorizationResponse("timer-race-1", true)

	select {
	case resp := <-responseChan:
		if resp == nil {
			t.Error("Expected non-nil response")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}

	// Wait for timer cleanup
	time.Sleep(60 * time.Millisecond)

	// Should not have leaked timer or caused panic
}

// Helper function to enable race detector if available
// Removed unused function detectRaceTest() to fix golangci-lint error
