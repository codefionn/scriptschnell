package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAuthorizationDialogDefaultsToDeny(t *testing.T) {
	dialog := NewAuthorizationDialog(AuthorizationRequest{
		ToolName:   "write_file_diff",
		Parameters: map[string]interface{}{"path": "main.go"},
		Reason:     "File exists but was not read",
	})

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
	dialog := NewAuthorizationDialog(AuthorizationRequest{ToolName: "write_file_diff"})
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
	dialog := NewAuthorizationDialog(AuthorizationRequest{ToolName: "write_file_diff"})

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
