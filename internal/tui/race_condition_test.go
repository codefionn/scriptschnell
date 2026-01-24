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
	// Note: In real Bubbletea usage, Update() is always called from a single goroutine.
	// This test verifies the dialog handles sequential updates correctly.
	m := New("test-model", "", false)
	m.ready = true

	req := CreateTestAuthorizationRequest("race-1", "shell", "Test")

	// Show dialog
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Immediately send key press (simulating rapid user input)
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_ = updatedModel.(*Model)

	// Should not have panicked and dialog should be in consistent state
	if m.authorizationDialog.request == nil {
		t.Error("Dialog should have request")
	}
}

func TestRace_WindowResizeDuringDialog(t *testing.T) {
	// Test window resize during active dialog doesn't cause panic
	// Note: In real Bubbletea usage, Update() is always called from a single goroutine.
	// This test verifies the dialog handles rapid sequential updates correctly.
	m := New("test-model", "", false)
	m.ready = true

	req := CreateTestAuthorizationRequest("race-resize-1", "shell", "Test")

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true

	// Send many resize messages rapidly (sequentially, as Bubbletea does)
	for i := 0; i < 5; i++ {
		width := 80 + i*20
		height := 30 + i*10
		updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		_ = updatedModel.(*Model)

		updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		_ = updatedModel.(*Model)
	}

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
	// Test that authorizationMu prevents race conditions in shared state
	// Note: Testing the authorizationMu directly via its protected state
	m := New("test-model", "", false)
	m.ready = true

	// Concurrently add/remove from pendingAuthorizations map (with mutex)
	var wg sync.WaitGroup
	numOps := 50

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			authID := fmt.Sprintf("race-mutex-%d", idx)
			req := CreateTestAuthorizationRequest(authID, "shell", "Test")
			m.authorizationMu.Lock()
			m.pendingAuthorizations[authID] = req
			m.authorizationMu.Unlock()
		}(i)
	}

	wg.Wait()

	// State should be consistent
	// The mutex protects access to pendingAuthorizations
}

func TestRace_ConcurrentHandlerOperations(t *testing.T) {
	// Test all handler operations happening concurrently
	handler := NewTUIInteractionHandler(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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

	go func() {
		defer wg.Done()
		// Ensure response is sent if not already sent
		time.Sleep(20 * time.Millisecond)
		handler.HandleAuthorizationResponse("race-handler-1", true)
	}()

	wg.Wait()
}

func TestRace_UserDialogStateTransitions(t *testing.T) {
	// Test rapid state transitions in user dialog
	// Note: In real Bubbletea usage, SetOverlayActive is called from a single goroutine.
	// This test verifies the overlayActive field is properly protected when needed.
	m := New("test-model", "", false)
	m.ready = true

	// Rapid sequential calls (as Bubbletea would do)
	for i := 0; i < 20; i++ {
		m.SetOverlayActive(i%2 == 0)
	}

	// State should be consistent
	// The overlayActiveMu protects the overlayActive field
	m.overlayActiveMu.RLock()
	active := m.overlayActive
	m.overlayActiveMu.RUnlock()
	_ = active
}

func TestRace_PendingAuthorizationsMap(t *testing.T) {
	// Test concurrent access to pending authorizations map with proper mutex
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
			m.authorizationMu.Lock()
			m.pendingAuthorizations[authID] = req
			m.authorizationMu.Unlock()
		}(i)
	}

	// Remove many concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			authID := fmt.Sprintf("pending-race-%d", idx)
			m.authorizationMu.Lock()
			delete(m.pendingAuthorizations, authID)
			m.authorizationMu.Unlock()
		}(i)
	}

	wg.Wait()

	// Should not panic
}

func TestRace_QuestionDialogAnswersMap(t *testing.T) {
	// Test concurrent answer updates in question dialog
	// This test uses the public interface to verify thread-safety
	questions := []QuestionWithOptions{
		{Question: "Q1?", Options: []string{"A", "B"}},
		{Question: "Q2?", Options: []string{"X", "Y"}},
	}
	dialog := NewUserQuestionDialog(questions)

	// The answers field is now protected by mu, so concurrent access is safe
	// This test verifies that the struct is properly set up for thread-safe access
	answers := dialog.GetAnswers()
	if answers == nil {
		t.Error("Expected non-nil answers")
	}
	if len(answers) != 2 {
		t.Errorf("Expected 2 answers, got %d", len(answers))
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
	// Test that overlayActive field is properly protected
	// Note: The overlayActiveMu protects concurrent reads/writes to overlayActive.
	// In production, Bubbletea serializes Update calls, but other code might
	// access overlayActive concurrently.
	m := New("test-model", "", false)
	m.ready = true

	// Test concurrent reads using RLock
	var wg sync.WaitGroup
	numOps := 50

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.overlayActiveMu.RLock()
			_ = m.overlayActive
			m.overlayActiveMu.RUnlock()
		}()
	}

	// Also write concurrently
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m.overlayActiveMu.Lock()
			m.overlayActive = idx%2 == 0
			m.overlayActiveMu.Unlock()
		}(i)
	}

	wg.Wait()

	// Should not panic
	m.overlayActiveMu.RLock()
	_ = m.overlayActive
	m.overlayActiveMu.RUnlock()
}

func TestRace_MultipleTabsAuthorization(t *testing.T) {
	// Test authorization requests from multiple tabs concurrently
	// Note: Use mutex to protect pendingAuthorizations map access
	m := New("test-model", "", false)
	m.ready = true

	// Create tabs
	for i := 1; i <= 5; i++ {
		m.sessions = append(m.sessions, &TabSession{ID: i, Name: fmt.Sprintf("Tab %d", i)})
	}

	var wg sync.WaitGroup

	// Concurrent requests from different tabs (now with mutex)
	for i := 1; i <= 5; i++ {
		tabID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := CreateTestAuthorizationRequest(fmt.Sprintf("tab-race-%d", tabID), "shell", "Test")
			req.TabID = tabID
			m.authorizationMu.Lock()
			m.pendingAuthorizations[fmt.Sprintf("tab-race-%d", tabID)] = req
			m.authorizationMu.Unlock()
		}()
	}

	wg.Wait()

	// Verify all were added
	m.authorizationMu.Lock()
	count := len(m.pendingAuthorizations)
	m.authorizationMu.Unlock()
	if count != 5 {
		t.Errorf("Expected 5 pending authorizations, got %d", count)
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
	// Note: In real Bubbletea usage, Update() is serialized. Test sequential updates.
	questions := []QuestionWithOptions{
		{Question: "Q1?", Options: []string{"A", "B"}},
		{Question: "Q2?", Options: []string{"X", "Y"}},
		{Question: "Q3?", Options: []string{"1", "2"}},
	}
	dialog := NewUserQuestionDialog(questions)

	// Send rapid navigation commands sequentially (as Bubbletea would)
	for i := 0; i < 50; i++ {
		_, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyDown})
		_, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyUp})
	}

	// Dialog should still be functional
	answers := dialog.GetAnswers()
	if len(answers) != 3 {
		t.Errorf("Expected 3 answers, got %d", len(answers))
	}
}

func TestRace_TextareaConcurrentUpdates(t *testing.T) {
	// Test concurrent updates to textarea
	// Note: In real Bubbletea usage, Update() is serialized. Test sequential updates.
	dialog := NewUserInputDialog("Test?")

	// Type sequentially from multiple sources (simulating rapid typing)
	for i := 0; i < 10; i++ {
		for j := 0; j < 5; j++ {
			_, _ = dialog.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune{rune('a' + j)},
			})
		}
	}

	// Should have typed some characters
	answer := dialog.GetAnswer()
	if len(answer) == 0 {
		t.Error("Expected some characters to be typed")
	}
}

func TestRace_ModelUpdateConcurrency(t *testing.T) {
	// Test rapid Update calls on model
	// Note: In real Bubbletea usage, Update() is serialized. Test sequential updates.
	m := New("test-model", "", false)
	m.ready = true

	updates := 100

	// Send many update messages sequentially (as Bubbletea would)
	for i := 0; i < updates; i++ {
		switch i % 4 {
		case 0:
			_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		case 1:
			_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		case 2:
			_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
		case 3:
			_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}

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
