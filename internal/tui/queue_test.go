package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQueuedPromptsProcessAfterCompletion(t *testing.T) {
	m := New("test-model", "", true)
	var submitted []string
	m.SetOnSubmit(func(input string) error {
		submitted = append(submitted, input)
		return nil
	})

	// Simulate an in-flight generation.
	m.generating = true

	m.textarea.SetValue("second prompt")
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(*Model)

	if got := len(m.queuedPrompts); got != 1 {
		t.Fatalf("expected 1 queued prompt bucket, got %d", got)
	}
	if got := len(m.queuedPrompts[m.activeSessionIdx]); got != 1 {
		t.Fatalf("expected 1 queued prompt in active tab, got %d", got)
	}

	if len(m.messages) == 0 {
		t.Fatal("expected queued prompt to add system notification")
	}

	if m.messages[len(m.messages)-1].role != "System" {
		t.Fatalf("expected last message to be system notification, got role %q", m.messages[len(m.messages)-1].role)
	}

	model, cmd := m.Update(CompleteMsg{})
	m = model.(*Model)

	if len(m.queuedPrompts[m.activeSessionIdx]) != 0 {
		t.Fatalf("expected queued prompt to start processing, queue has %d remaining", len(m.queuedPrompts[m.activeSessionIdx]))
	}

	if !m.generating {
		t.Fatal("expected model to be generating queued prompt")
	}

	if cmd == nil {
		t.Fatal("expected non-nil command to process queued prompt")
	}

	if msg := cmd(); msg != nil {
		// Deliver any follow-up message (e.g. errors) back into the model.
		m.Update(msg)
	}

	if len(submitted) != 1 || submitted[0] != "second prompt" {
		t.Fatalf("expected queued prompt to be submitted, got %v", submitted)
	}

	if len(m.messages) < 2 || m.messages[len(m.messages)-1].role != "You" {
		t.Fatal("expected queued prompt to add user message to transcript")
	}
}
