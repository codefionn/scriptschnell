package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/session"
)

func newModelWithTabs(t *testing.T, count int) *Model {
	t.Helper()
	m := New("test-model", "", true)
	m.sessions = make([]*TabSession, count)
	for i := 0; i < count; i++ {
		id := i + 1
		sess := session.NewSession(fmt.Sprintf("tab-%d", id), "")
		m.sessions[i] = &TabSession{ID: id, Session: sess, Messages: []message{}}
	}
	m.activeSessionIdx = 0
	return m
}

func TestQueuedPromptsProcessAfterCompletion(t *testing.T) {
	m := New("test-model", "", true)
	var submitted []string
	m.SetOnSubmit(func(input string) error {
		submitted = append(submitted, input)
		return nil
	})

	// Simulate an in-flight generation.
	m.generating = true
	m.generationTabIdx = m.activeSessionIdx

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

func TestQueuedPromptRunsOnQueuedTabNotActive(t *testing.T) {
	m := newModelWithTabs(t, 2)
	var submitted []string
	m.SetOnSubmit(func(input string) error {
		submitted = append(submitted, input)
		return nil
	})

	// Start generation in tab 0.
	cmd := m.startPrompt(0, "first prompt")
	if msg := cmd(); msg != nil {
		m.Update(msg)
	}
	if !m.generating || m.generationTabIdx != 0 {
		t.Fatalf("expected generation in tab 0, got generating=%v tabIdx=%d", m.generating, m.generationTabIdx)
	}

	// Switch to tab 1 and queue a prompt.
	m.activeSessionIdx = 1
	m.queuePrompt(1, "second prompt")
	if got := len(m.queuedPrompts[1]); got != 1 {
		t.Fatalf("expected 1 queued prompt on tab 1, got %d", got)
	}

	// Complete the first generation; the queued tab 1 prompt should start.
	model, cmd := m.Update(CompleteMsg{})
	m = model.(*Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}

	if len(m.queuedPrompts[1]) != 0 {
		t.Fatalf("expected tab 1 queue to be empty, got %d", len(m.queuedPrompts[1]))
	}
	if !m.generating || m.generationTabIdx != 1 {
		t.Fatalf("expected generation to continue on tab 1, got generating=%v tabIdx=%d", m.generating, m.generationTabIdx)
	}
	if len(submitted) < 2 || submitted[1] != "second prompt" {
		t.Fatalf("expected queued tab 1 prompt to submit, got %v", submitted)
	}

	tab1Msgs := m.sessions[1].Messages
	if len(tab1Msgs) == 0 || tab1Msgs[len(tab1Msgs)-1].role != "You" || tab1Msgs[len(tab1Msgs)-1].content != "second prompt" {
		t.Fatalf("expected tab 1 transcript to receive user message, got %+v", tab1Msgs)
	}

	tab0Msgs := m.sessions[0].Messages
	for _, msg := range tab0Msgs {
		if msg.role == "You" && msg.content == "second prompt" {
			t.Fatal("tab 0 should not contain tab 1's queued prompt")
		}
	}
}

func TestStreamingStaysOnGeneratingTabAfterSwitch(t *testing.T) {
	m := newModelWithTabs(t, 2)

	// Start generation on tab 0.
	m.startPrompt(0, "first prompt")
	if !m.generating || m.generationTabIdx != 0 {
		t.Fatalf("expected generation in tab 0, got generating=%v tabIdx=%d", m.generating, m.generationTabIdx)
	}

	// Stream a chunk while on tab 0.
	m.Update(GeneratingMsg{Content: "hello "})

	// Switch to tab 1 and stream another chunk; it should still go to tab 0.
	m.activeSessionIdx = 1
	m.Update(GeneratingMsg{Content: "world"})

	tab0Msgs := m.sessions[0].Messages
	if len(tab0Msgs) == 0 {
		t.Fatal("expected messages in tab 0")
	}
	last := tab0Msgs[len(tab0Msgs)-1]
	if last.role != "Assistant" || last.content != "hello world" {
		t.Fatalf("expected assistant content in tab 0 to be 'hello world', got role=%s content=%q", last.role, last.content)
	}

	tab1Msgs := m.sessions[1].Messages
	if len(tab1Msgs) != 0 {
		t.Fatalf("expected no assistant messages on tab 1, got %+v", tab1Msgs)
	}
}
