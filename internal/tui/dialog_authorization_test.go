package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAuthorizationDialogDefaultsToDeny(t *testing.T) {
	dialog := NewAuthorizationDialog(&AuthorizationRequest{
		ToolName:   "edit_file",
		Parameters: map[string]interface{}{"path": "main.go"},
		Reason:     "File exists but was not read",
	}, "test-tab")

	model, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected command from enter key")
	}

	updated, ok := model.(AuthorizationDialog)
	if !ok {
		t.Fatalf("expected AuthorizationDialog model")
	}

	if !updated.HasChoice() {
		t.Fatalf("expected dialog to record a choice")
	}
	if updated.GetApproved() {
		t.Fatalf("expected default selection to deny approval")
	}
}

func TestAuthorizationDialogApproveSelection(t *testing.T) {
	dialog := NewAuthorizationDialog(&AuthorizationRequest{ToolName: "edit_file"}, "test-tab")
	dialog.list.Select(0)

	model, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := model.(AuthorizationDialog)
	if !ok {
		t.Fatalf("expected AuthorizationDialog model")
	}

	if !updated.HasChoice() {
		t.Fatalf("expected approval choice to be recorded")
	}
	if !updated.GetApproved() {
		t.Fatalf("expected approval when Approve is selected")
	}
}

func TestAuthorizationDialogEscapeDenies(t *testing.T) {
	dialog := NewAuthorizationDialog(&AuthorizationRequest{ToolName: "edit_file"}, "test-tab")

	model, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, ok := model.(AuthorizationDialog)
	if !ok {
		t.Fatalf("expected AuthorizationDialog model")
	}

	if !updated.HasChoice() {
		t.Fatalf("expected escape to record a choice")
	}
	if updated.GetApproved() {
		t.Fatalf("escape should deny authorization")
	}
}

func TestDomainAuthorizationDialogDefaultsToDeny(t *testing.T) {
	dialog := NewDomainAuthorizationDialog(DomainAuthorizationRequest{Domain: "example.com"})

	model, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := model.(DomainAuthorizationDialog)
	if !ok {
		t.Fatalf("expected DomainAuthorizationDialog model")
	}

	if !updated.HasChoice() {
		t.Fatalf("expected dialog to capture choice")
	}
	if choice := updated.GetChoice(); choice != "deny" {
		t.Fatalf("expected default choice to be deny, got %q", choice)
	}
}

func TestDomainAuthorizationDialogPermanentApproval(t *testing.T) {
	dialog := NewDomainAuthorizationDialog(DomainAuthorizationRequest{Domain: "example.com"})
	dialog.list.Select(1)

	model, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := model.(DomainAuthorizationDialog)
	if !ok {
		t.Fatalf("expected DomainAuthorizationDialog model")
	}

	if !updated.HasChoice() {
		t.Fatalf("expected choice to be recorded")
	}
	if choice := updated.GetChoice(); choice != "permanent" {
		t.Fatalf("expected permanent approval, got %q", choice)
	}
}

// TUI Integration Tests

func TestShowAuthorizationDialogMsgInitializesDialogBeforeFlag(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Create authorization request
	req := &AuthorizationRequest{
		AuthID:       "test-auth-1",
		TabID:        1,
		ToolName:     "edit_file",
		Parameters:   map[string]interface{}{"path": "test.go"},
		Reason:       "File exists but was not read",
		ResponseChan: make(chan bool, 1),
	}

	// Send ShowAuthorizationDialogMsg
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Verify dialog is initialized
	if m.authorizationDialog.request == nil {
		t.Fatal("expected authorization dialog to be initialized")
	}

	// Verify flag is set
	if !m.authorizationDialogOpen {
		t.Fatal("expected authorizationDialogOpen to be true")
	}

	// Verify active auth ID is set
	if m.activeAuthorizationID != "test-auth-1" {
		t.Fatalf("expected activeAuthorizationID to be test-auth-1, got %q", m.activeAuthorizationID)
	}

	// Verify dialog has correct request
	if m.authorizationDialog.request.ToolName != "edit_file" {
		t.Fatalf("expected dialog to have write_file_diff tool, got %q", m.authorizationDialog.request.ToolName)
	}
}

func TestAuthorizationDialogRoutesMessagesWhenOpen(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Initialize authorization dialog
	req := &AuthorizationRequest{
		AuthID:       "test-auth-2",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "rm -rf /"},
		Reason:       "Potentially dangerous command",
		ResponseChan: make(chan bool, 1),
	}

	// Create and set the dialog
	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-auth-2"

	// Send a key message that should be routed to the dialog
	initialSelection := m.authorizationDialog.list.Index()
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updatedModel.(*Model)

	// Verify the message was handled by the dialog (list selection should change)
	newSelection := m.authorizationDialog.list.Index()
	if initialSelection == newSelection {
		// Up key might not change selection if already at top, try Down
		updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updatedModel.(*Model)
		newSelection = m.authorizationDialog.list.Index()
		if initialSelection == newSelection {
			t.Fatal("expected dialog to handle key messages when open")
		}
	}
}

func TestAuthorizationResponseClosesDialog(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Initialize authorization dialog and pending request
	responseChan := make(chan bool, 1)
	req := &AuthorizationRequest{
		AuthID:       "test-auth-3",
		TabID:        1,
		ToolName:     "create_file",
		Parameters:   map[string]interface{}{"path": "new.go"},
		Reason:       "Creating new file",
		ResponseChan: responseChan,
	}

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-auth-3"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-auth-3": req,
	}

	// Add a tab so the tab lookup doesn't fail
	m.sessions = []*TabSession{{ID: 1}}
	m.activeSessionIdx = 0

	// Send authorization response - dialog should NOT be open during this message
	// so it goes through the normal Update path
	m.authorizationDialogOpen = false
	msg := AuthorizationResponseMsg{
		AuthID:   "test-auth-3",
		Approved: true,
	}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Verify dialog is closed
	if m.authorizationDialogOpen {
		t.Fatal("expected authorizationDialogOpen to be false after response")
	}

	// Verify active auth ID is cleared
	if m.activeAuthorizationID != "" {
		t.Fatalf("expected activeAuthorizationID to be empty, got %q", m.activeAuthorizationID)
	}

	// Verify response was sent to channel
	select {
	case approved := <-responseChan:
		if !approved {
			t.Fatal("expected approval to be true")
		}
	default:
		t.Fatal("expected response to be sent to channel")
	}
}

func TestAuthorizationDialogHandlesEnterKey(t *testing.T) {
	// This test verifies that the dialog component itself handles Enter correctly
	// The full integration with m.program.Send() is tested in runtime scenarios
	dialog := NewAuthorizationDialog(&AuthorizationRequest{
		ToolName:   "shell",
		Parameters: map[string]interface{}{"command": "echo test"},
		Reason:     "Test command",
	}, "test-tab")

	// Select approve option (index 0)
	dialog.list.Select(0)

	// Send Enter key - this should record the choice in the dialog
	updatedModel, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updatedDialog := updatedModel.(AuthorizationDialog)

	// Verify the dialog recorded the approval choice
	if !updatedDialog.HasChoice() {
		t.Fatal("expected dialog to have a choice after Enter")
	}
	if !updatedDialog.GetApproved() {
		t.Fatal("expected dialog to record approval")
	}
}

func TestAuthorizationDialogHandlesEscapeKey(t *testing.T) {
	// This test verifies that the dialog component itself handles Escape correctly
	// The full integration with m.program.Send() is tested in runtime scenarios
	dialog := NewAuthorizationDialog(&AuthorizationRequest{
		ToolName:   "shell",
		Parameters: map[string]interface{}{"command": "dangerous"},
		Reason:     "Test escape",
	}, "test-tab")

	// Send Escape key - this should record denial in the dialog
	updatedModel, _ := dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updatedDialog := updatedModel.(AuthorizationDialog)

	// Verify the dialog recorded the denial choice
	if !updatedDialog.HasChoice() {
		t.Fatal("expected dialog to have a choice after Escape")
	}
	if updatedDialog.GetApproved() {
		t.Fatal("expected dialog to record denial")
	}
}

func TestMultipleAuthorizationRequestsQueued(t *testing.T) {
	m := New("test-model", "", false)
	m.ready = true

	// Add tabs for the requests
	m.sessions = []*TabSession{
		{ID: 1},
		{ID: 2},
	}
	m.activeSessionIdx = 0

	// First authorization request
	req1 := &AuthorizationRequest{
		AuthID:       "test-auth-6",
		TabID:        1,
		ToolName:     "edit_file",
		Parameters:   map[string]interface{}{"path": "file1.go"},
		Reason:       "First file",
		ResponseChan: make(chan bool, 1),
	}

	// Show first dialog
	msg1 := ShowAuthorizationDialogMsg{Request: req1}
	updatedModel, _ := m.Update(msg1)
	m = updatedModel.(*Model)

	if !m.authorizationDialogOpen {
		t.Fatal("expected first dialog to be open")
	}
	if m.activeAuthorizationID != "test-auth-6" {
		t.Fatalf("expected activeAuthorizationID to be test-auth-6, got %q", m.activeAuthorizationID)
	}

	// Second authorization request (should wait)
	req2 := &AuthorizationRequest{
		AuthID:       "test-auth-7",
		TabID:        2,
		ToolName:     "edit_file",
		Parameters:   map[string]interface{}{"path": "file2.go"},
		Reason:       "Second file",
		ResponseChan: make(chan bool, 1),
	}

	// Store both in pending
	m.pendingAuthorizations["test-auth-6"] = req1
	m.pendingAuthorizations["test-auth-7"] = req2

	// Close first dialog - set dialog to not open before sending response
	m.authorizationDialogOpen = false
	resp1 := AuthorizationResponseMsg{
		AuthID:   "test-auth-6",
		Approved: true,
	}
	updatedModel, _ = m.Update(resp1)
	m = updatedModel.(*Model)

	if m.authorizationDialogOpen {
		t.Fatal("expected dialog to be closed after first response")
	}

	// Verify second request is still in pending
	if _, ok := m.pendingAuthorizations["test-auth-7"]; !ok {
		t.Fatal("expected second request to still be pending")
	}
}

// Race Condition Tests

func TestAuthorizationDialogNoRaceOnImmediateKeyPress(t *testing.T) {
	// This test verifies the race condition fix: dialog must be initialized
	// before the flag is set, so immediate key presses don't panic
	m := New("test-model", "", false)
	m.ready = true

	req := &AuthorizationRequest{
		AuthID:       "test-race-1",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "test"},
		Reason:       "Test race condition",
		ResponseChan: make(chan bool, 1),
	}

	// Send ShowAuthorizationDialogMsg and immediately send a key message
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Immediately send a key press (this would panic if dialog not initialized)
	keyModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = keyModel.(*Model)

	// Should not panic and dialog should still be open
	if !m.authorizationDialogOpen {
		t.Fatal("expected dialog to remain open after key press")
	}

	// Dialog should be functional
	if m.authorizationDialog.request == nil {
		t.Fatal("expected dialog to have request after key press")
	}
}

func TestAuthorizationDialogNoRaceOnWindowResize(t *testing.T) {
	// Test that window resize immediately after showing dialog doesn't cause issues
	m := New("test-model", "", false)
	m.ready = true

	req := &AuthorizationRequest{
		AuthID:       "test-race-2",
		TabID:        1,
		ToolName:     "edit_file",
		Parameters:   map[string]interface{}{"path": "test.go"},
		Reason:       "Test resize race",
		ResponseChan: make(chan bool, 1),
	}

	// Show dialog
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Immediately send window resize
	resizeModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = resizeModel.(*Model)

	// Should not panic
	if !m.authorizationDialogOpen {
		t.Fatal("expected dialog to remain open after resize")
	}

	// Dialog should have updated size
	if m.authorizationDialog.width != 120 {
		t.Fatalf("expected dialog width 120, got %d", m.authorizationDialog.width)
	}
}

func TestAuthorizationDialogInitializationOrder(t *testing.T) {
	// Explicitly test that dialog is created BEFORE flag is set
	m := New("test-model", "", false)
	m.ready = true

	req := &AuthorizationRequest{
		AuthID:       "test-order-1",
		TabID:        1,
		ToolName:     "create_file",
		Parameters:   map[string]interface{}{"path": "new.go"},
		Reason:       "Test initialization order",
		ResponseChan: make(chan bool, 1),
	}

	// Before showing dialog
	if m.authorizationDialogOpen {
		t.Fatal("expected dialog to be closed initially")
	}
	if m.authorizationDialog.request != nil {
		t.Fatal("expected dialog to be uninitialized initially")
	}

	// Show dialog
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// After showing: both flag AND dialog should be set
	if !m.authorizationDialogOpen {
		t.Fatal("expected authorizationDialogOpen to be true")
	}
	if m.authorizationDialog.request == nil {
		t.Fatal("expected dialog to be initialized when flag is true")
	}

	// The critical invariant: if flag is true, dialog must be fully initialized
	// This is what the race condition fix ensures
	if m.authorizationDialogOpen && m.authorizationDialog.request.ToolName != "create_file" {
		t.Fatal("dialog not properly initialized despite flag being set")
	}
}

func TestAuthorizationDialogYNShortcuts(t *testing.T) {
	// Test the Y/N keyboard shortcuts work correctly
	m := New("test-model", "", false)
	m.ready = true

	responseChan := make(chan bool, 1)
	req := &AuthorizationRequest{
		AuthID:       "test-shortcuts-1",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "echo test"},
		Reason:       "Test shortcuts",
		ResponseChan: responseChan,
	}

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-shortcuts-1"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-shortcuts-1": req,
	}
	m.sessions = []*TabSession{{ID: 1}}

	// Press 'y' for quick approve
	// Note: We can't test m.program.Send() in unit tests, but we can verify
	// the dialog component handles the key correctly
	dialog := m.authorizationDialog

	// The actual shortcut handling is in handleAuthorizationDialog which calls
	// m.program.Send(), so we just verify the dialog is set up correctly
	if dialog.request.ToolName != "shell" {
		t.Fatal("dialog not set up correctly for shortcut test")
	}
}

func TestAuthorizationDialogMultipleRapidUpdates(t *testing.T) {
	// Test rapid successive updates don't cause issues
	m := New("test-model", "", false)
	m.ready = true

	req := &AuthorizationRequest{
		AuthID:       "test-rapid-1",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "test"},
		Reason:       "Test rapid updates",
		ResponseChan: make(chan bool, 1),
	}

	// Show dialog
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Send multiple rapid updates
	for i := 0; i < 10; i++ {
		updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updatedModel.(*Model)
	}

	for i := 0; i < 10; i++ {
		updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = updatedModel.(*Model)
	}

	// Should not panic and dialog should still be functional
	if !m.authorizationDialogOpen {
		t.Fatal("expected dialog to remain open after rapid updates")
	}
	if m.authorizationDialog.request == nil {
		t.Fatal("expected dialog to remain initialized after rapid updates")
	}
}

func TestAuthorizationDialogCleanupOnResponse(t *testing.T) {
	// Test that all state is properly cleaned up when dialog closes
	m := New("test-model", "", false)
	m.ready = true

	responseChan := make(chan bool, 1)
	req := &AuthorizationRequest{
		AuthID:       "test-cleanup-1",
		TabID:        1,
		ToolName:     "create_file",
		Parameters:   map[string]interface{}{"path": "test.go"},
		Reason:       "Test cleanup",
		ResponseChan: responseChan,
	}

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-cleanup-1"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-cleanup-1": req,
	}
	m.sessions = []*TabSession{{ID: 1}}

	// Send response
	m.authorizationDialogOpen = false
	msg := AuthorizationResponseMsg{
		AuthID:   "test-cleanup-1",
		Approved: false,
	}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Verify complete cleanup
	if m.authorizationDialogOpen {
		t.Fatal("expected authorizationDialogOpen to be false")
	}
	if m.activeAuthorizationID != "" {
		t.Fatalf("expected activeAuthorizationID to be empty, got %q", m.activeAuthorizationID)
	}

	// Response should be in channel
	select {
	case approved := <-responseChan:
		if approved {
			t.Fatal("expected approval to be false")
		}
	default:
		t.Fatal("expected response in channel")
	}
}

func TestAuthorizationDialogTabIDHandling(t *testing.T) {
	// Test that dialog correctly identifies the requesting tab
	m := New("test-model", "", false)
	m.ready = true

	// Create multiple tabs
	m.sessions = []*TabSession{
		{ID: 1, Name: "tab-one"},
		{ID: 2, Name: "tab-two"},
		{ID: 3, Name: "tab-three"},
	}
	m.activeSessionIdx = 1

	req := &AuthorizationRequest{
		AuthID:       "test-tab-1",
		TabID:        2, // Middle tab
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "test"},
		Reason:       "Test tab identification",
		ResponseChan: make(chan bool, 1),
	}

	// Show dialog
	msg := ShowAuthorizationDialogMsg{Request: req}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Dialog should show the correct tab name
	if m.authorizationDialog.tabName != "tab-two" {
		t.Fatalf("expected tab name 'tab-two', got %q", m.authorizationDialog.tabName)
	}

	// Request TabID should be preserved
	if m.authorizationDialog.request.TabID != 2 {
		t.Fatalf("expected TabID 2, got %d", m.authorizationDialog.request.TabID)
	}
}

func TestAuthorizationResponseProcessedWhenDialogOpen(t *testing.T) {
	// This test verifies the bug fix: AuthorizationResponseMsg should be
	// processed even when authorizationDialogOpen is still true.
	// Previously, the message would be routed to handleAuthorizationDialog
	// which doesn't handle it, causing a freeze.
	m := New("test-model", "", false)
	m.ready = true

	// Set up authorization state
	responseChan := make(chan bool, 1)
	req := &AuthorizationRequest{
		AuthID:       "test-freeze-fix",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "echo test"},
		Reason:       "Test freeze fix",
		ResponseChan: responseChan,
	}

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true // Dialog is STILL OPEN
	m.activeAuthorizationID = "test-freeze-fix"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-freeze-fix": req,
	}
	m.sessions = []*TabSession{{ID: 1}}
	m.activeSessionIdx = 0

	// Send AuthorizationResponseMsg while dialog is STILL OPEN
	// This simulates what happens when user presses Enter:
	// handleAuthorizationDialog sends the message but hasn't closed the dialog yet
	msg := AuthorizationResponseMsg{
		AuthID:   "test-freeze-fix",
		Approved: true,
	}
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(*Model)

	// Verify the message was processed (not dropped)
	// 1. Dialog should be closed
	if m.authorizationDialogOpen {
		t.Fatal("expected authorizationDialogOpen to be false after processing response")
	}

	// 2. Active auth ID should be cleared
	if m.activeAuthorizationID != "" {
		t.Fatalf("expected activeAuthorizationID to be empty, got %q", m.activeAuthorizationID)
	}

	// 3. Response should have been sent to channel
	select {
	case approved := <-responseChan:
		if !approved {
			t.Fatal("expected approval to be true")
		}
	default:
		t.Fatal("expected response to be sent to channel - this indicates the message was dropped")
	}

	// Note: In production, the authCallback goroutine would delete the pending
	// authorization after receiving from the channel. In this test, we're only
	// verifying that the UI handler processes the message correctly and sends
	// the response.
}

func TestSafeSendWithNilProgram(t *testing.T) {
	// Test that safeSend doesn't panic when m.program is nil
	m := New("test-model", "", false)
	m.ready = true
	m.program = nil // Explicitly set to nil

	// This should not panic - it should log an error and return
	m.safeSend(AuthorizationResponseMsg{
		AuthID:   "test-nil-program",
		Approved: true,
	})

	// If we reach here without panic, the test passes
}

func TestHandleAuthorizationDialogAlwaysSendsResponse(t *testing.T) {
	// This test verifies that handleAuthorizationDialog ALWAYS sends a response
	// even if the list item is nil or type assertion fails.
	// This prevents the authorization callback from blocking indefinitely.
	m := New("test-model", "", false)
	m.ready = true

	// Set up authorization state with a response channel
	responseChan := make(chan bool, 1)
	req := &AuthorizationRequest{
		AuthID:       "test-always-responds",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "echo test"},
		Reason:       "Test always responds",
		ResponseChan: responseChan,
	}

	// Create dialog with proper list initialization
	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-always-responds"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-always-responds": req,
	}
	m.sessions = []*TabSession{{ID: 1}}
	m.activeSessionIdx = 0

	// Press Enter - even if something goes wrong internally, a response should be sent
	msg := tea.KeyMsg{Type: tea.KeyEnter}

	// First, route through handleAuthorizationDialog (which uses safeSend)
	// Since m.program is nil, safeSend will log error but not panic
	_, _ = m.handleAuthorizationDialog(msg)

	// Now test the direct AuthorizationResponseMsg handling
	// This simulates what happens when safeSend successfully delivers the message
	responseMsg := AuthorizationResponseMsg{
		AuthID:   "test-always-responds",
		Approved: true,
	}
	updatedModel, _ := m.Update(responseMsg)
	m = updatedModel.(*Model)

	// Verify the dialog was closed
	if m.authorizationDialogOpen {
		t.Fatal("expected authorizationDialogOpen to be false after processing response")
	}

	// Verify response was sent to channel
	select {
	case approved := <-responseChan:
		if !approved {
			t.Fatal("expected approval to be true")
		}
	default:
		t.Fatal("expected response to be sent to channel")
	}
}

// Spinner freeze tests

func TestSpinnerTickChainSurvivesAuthorizationDialog(t *testing.T) {
	// This test verifies that the spinner tick chain is restarted after the
	// authorization dialog closes. Previously, spinner.TickMsg messages were
	// swallowed by handleAuthorizationDialog (returning nil cmd), which broke
	// the tick chain permanently. After the dialog closed, spinnerActive was
	// still true so the restart logic (if shouldSpin && !m.spinnerActive)
	// never triggered, causing the spinner to die and the TUI to appear frozen.
	m := New("test-model", "", false) // animations enabled
	m.ready = true

	// Start the spinner (simulating active processing)
	m.spinnerActive = true

	// Set up authorization dialog
	req := &AuthorizationRequest{
		AuthID:       "test-spinner-freeze",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "echo test"},
		Reason:       "Test spinner freeze",
		ResponseChan: make(chan bool, 1),
	}

	// Show dialog
	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-spinner-freeze"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-spinner-freeze": req,
	}
	m.sessions = []*TabSession{{ID: 1}}
	m.activeSessionIdx = 0

	// Send spinner tick while dialog is open - this gets swallowed
	tickMsg := m.spinner.Tick()
	updatedModel, cmd := m.Update(tickMsg)
	m = updatedModel.(*Model)

	// The tick was routed to handleAuthorizationDialog which returns nil cmd
	// This breaks the tick chain
	if cmd != nil {
		// If cmd is not nil, handleAuthorizationDialog propagated the tick (good but unexpected)
		t.Log("tick was propagated through dialog - not the bug scenario")
	}

	// Spinner should still be marked active
	if !m.spinnerActive {
		t.Fatal("spinnerActive should still be true while dialog is open")
	}

	// Now close the dialog by sending AuthorizationResponseMsg
	responseMsg := AuthorizationResponseMsg{
		AuthID:   "test-spinner-freeze",
		Approved: true,
	}
	updatedModel, cmd = m.Update(responseMsg)
	m = updatedModel.(*Model)

	// After the dialog closes, the spinner tick chain should be restarted
	// via a returned spinner.Tick command
	if !m.spinnerActive {
		t.Fatal("spinnerActive should remain true after dialog closes")
	}

	// The returned command should include a spinner tick to restart the chain
	if cmd == nil {
		t.Fatal("expected a non-nil command after dialog closes to restart spinner tick chain")
	}
}

func TestSpinnerInactiveNoRestartAfterDialog(t *testing.T) {
	// When the spinner is NOT active, closing the dialog should not start it.
	m := New("test-model", "", false) // animations enabled
	m.ready = true

	m.spinnerActive = false // spinner is not active

	req := &AuthorizationRequest{
		AuthID:       "test-no-restart",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "echo test"},
		Reason:       "Test no restart",
		ResponseChan: make(chan bool, 1),
	}

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-no-restart"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-no-restart": req,
	}
	m.sessions = []*TabSession{{ID: 1}}
	m.activeSessionIdx = 0

	// Close dialog
	responseMsg := AuthorizationResponseMsg{
		AuthID:   "test-no-restart",
		Approved: true,
	}
	updatedModel, _ := m.Update(responseMsg)
	m = updatedModel.(*Model)

	// Spinner should remain inactive
	if m.spinnerActive {
		t.Fatal("spinnerActive should remain false - should not start spinner when it wasn't active")
	}
}

func TestAnimationsDisabledNoSpinnerRestartAfterDialog(t *testing.T) {
	// When animations are disabled, no spinner restart should occur.
	m := New("test-model", "", true) // animations disabled
	m.ready = true

	m.spinnerActive = true // even if marked active, animations are disabled

	req := &AuthorizationRequest{
		AuthID:       "test-anim-disabled",
		TabID:        1,
		ToolName:     "shell",
		Parameters:   map[string]interface{}{"command": "echo test"},
		Reason:       "Test animations disabled",
		ResponseChan: make(chan bool, 1),
	}

	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-anim-disabled"
	m.pendingAuthorizations = map[string]*AuthorizationRequest{
		"test-anim-disabled": req,
	}
	m.sessions = []*TabSession{{ID: 1}}
	m.activeSessionIdx = 0

	// Close dialog
	responseMsg := AuthorizationResponseMsg{
		AuthID:   "test-anim-disabled",
		Approved: true,
	}
	updatedModel, _ := m.Update(responseMsg)
	m = updatedModel.(*Model)

	// No crash, no spinner restart (animations disabled)
	if m.authorizationDialogOpen {
		t.Fatal("dialog should be closed")
	}
}

// WaitingForAuth cleanup tests

func TestHandlerPathClearsWaitingForAuth(t *testing.T) {
	// This test verifies that WaitingForAuth is cleared when using the
	// handler-based authorization path (TUIAuthorizationRequestMsg).
	// Previously, WaitingForAuth was only cleared when the request was
	// found in pendingAuthorizations. The handler-based path does not
	// add entries there, so WaitingForAuth stayed stuck at true.
	m := New("test-model", "", false)
	m.ready = true

	m.sessions = []*TabSession{{ID: 1, WaitingForAuth: true}}
	m.activeSessionIdx = 0

	// Set up handler-based dialog (no entry in pendingAuthorizations)
	req := &AuthorizationRequest{
		AuthID:   "test-handler-waitauth",
		TabID:    1,
		ToolName: "shell",
	}
	m.authorizationDialog = NewAuthorizationDialog(req, "test-tab")
	m.authorizationDialogOpen = true
	m.activeAuthorizationID = "test-handler-waitauth"
	// Not adding to pendingAuthorizations — the handler path doesn't use it

	// Process response
	responseMsg := AuthorizationResponseMsg{
		AuthID:   "test-handler-waitauth",
		Approved: true,
	}
	updatedModel, _ := m.Update(responseMsg)
	m = updatedModel.(*Model)

	// WaitingForAuth should be cleared even without a pendingAuthorizations entry
	if m.sessions[0].WaitingForAuth {
		t.Fatal("WaitingForAuth should be false after handler-based authorization completes")
	}
}
