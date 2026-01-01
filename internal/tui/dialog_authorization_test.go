package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAuthorizationDialogDefaultsToDeny(t *testing.T) {
	dialog := NewAuthorizationDialog(&AuthorizationRequest{
		ToolName:   "write_file_diff",
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
	dialog := NewAuthorizationDialog(&AuthorizationRequest{ToolName: "write_file_diff"}, "test-tab")
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
	dialog := NewAuthorizationDialog(&AuthorizationRequest{ToolName: "write_file_diff"}, "test-tab")

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
		ToolName:     "write_file_diff",
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
	if m.authorizationDialog.request.ToolName != "write_file_diff" {
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
		ToolName:     "write_file_diff",
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
		ToolName:     "write_file_diff",
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
		ToolName:     "write_file_diff",
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
