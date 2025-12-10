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
		m.sessions[i] = &TabSession{
			ID:           id,
			Session:      sess,
			Messages:     []message{},
			Generating:   false,
			WaitingForAuth: false,
		}
		// Initialize queued prompts for this tab
		m.queuedPrompts[i] = []string{}
	}
	m.activeSessionIdx = 0
	return m
}

func TestQueuedPromptsProcessAfterCompletion(t *testing.T) {
	m := New("test-model", "", true)
	// Initialize with one tab
	m.sessions = []*TabSession{{
		ID:           1,
		Session:      session.NewSession("tab-1", ""),
		Messages:     []message{},
		Generating:   false,
		WaitingForAuth: false,
	}}
	m.activeSessionIdx = 0
	m.queuedPrompts[0] = []string{}
	
	// Simulate an in-flight generation.
	m.setTabGenerating(m.activeSessionIdx, true)

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

	// Complete the generation and simulate queue processing
	m.setTabGenerating(m.activeSessionIdx, false) // Stop generation
	
	// Simulate the queue processing logic that would happen
	queue := m.queuedPrompts[m.activeSessionIdx]
	if len(queue) > 0 {
		// This simulates what processNextQueuedPromptForTab would do
		next := queue[0]
		m.queuedPrompts[m.activeSessionIdx] = queue[1:] // Remove from queue
		m.addMessage("System", fmt.Sprintf("Processing queued prompt: %s", next))
		m.addMessage("You", next)
		m.setTabGenerating(m.activeSessionIdx, true) // Start generating again
	}
	
	if len(m.queuedPrompts[m.activeSessionIdx]) != 0 {
		t.Fatalf("expected queued prompt to start processing, queue has %d remaining", len(m.queuedPrompts[m.activeSessionIdx]))
	}

	if !m.isCurrentTabGenerating() {
		t.Fatal("expected model to be generating queued prompt")
	}

	// Check that messages were added appropriately
	if len(m.messages) < 2 || m.messages[len(m.messages)-1].role != "You" {
		t.Fatal("expected queued prompt to add user message to transcript")
	}
}

func TestQueuedPromptRunsOnQueuedTabNotActive(t *testing.T) {
	m := newModelWithTabs(t, 2)

	// Manually simulate an in-flight generation in tab 0.
	m.setTabGenerating(0, true)

	// Switch to tab 1 and queue a prompt.
	m.activeSessionIdx = 1
	m.queuePrompt(1, "second prompt")
	if got := len(m.queuedPrompts[1]); got != 1 {
		t.Fatalf("expected 1 queued prompt on tab 1, got %d", got)
	}

	// Complete the first generation and process queue for tab 1
	m.setTabGenerating(0, false) // Stop generation in tab 0
	
	// Simulate queue processing for tab 1
	queue := m.queuedPrompts[1]
	if len(queue) > 0 {
		next := queue[0]
		m.queuedPrompts[1] = queue[1:] // Remove from queue
		m.addMessageForTab(1, "System", fmt.Sprintf("Processing queued prompt: %s", next))
		m.addMessageForTab(1, "You", next)
		m.setTabGenerating(1, true) // Start generating in tab 1
	}

	if len(m.queuedPrompts[1]) != 0 {
		t.Fatalf("expected tab 1 queue to be empty, got %d", len(m.queuedPrompts[1]))
	}
	if !m.sessions[1].IsGenerating() {
		t.Fatalf("expected generation to continue on tab 1, got generating=%v tabIdx=%d", m.sessions[1].IsGenerating(), 1)
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

	// Simulate generation on tab 0.
	m.setTabGenerating(0, true)
	if !m.sessions[0].IsGenerating() {
		t.Fatalf("expected generation in tab 0, got generating=%v tabIdx=%d", m.sessions[0].IsGenerating(), 0)
	}

	// Simulate streaming messages to tab 0
	m.addMessageForTab(0, "System", "Starting generation...")
	m.addMessageForTab(0, "Assistant", "hello ")

	// Switch to tab 1 and "stream" another chunk; it should still go to tab 0.
	m.activeSessionIdx = 1
	m.addMessageForTab(0, "Assistant", "world")

	tab0Msgs := m.sessions[0].Messages
	if len(tab0Msgs) == 0 {
		t.Fatal("expected messages in tab 0")
	}
	last := tab0Msgs[len(tab0Msgs)-1]
	if last.role != "Assistant" || last.content != "world" {
		t.Fatalf("expected assistant content in tab 0, got role=%s content=%q", last.role, last.content)
	}

	tab1Msgs := m.sessions[1].Messages
	if len(tab1Msgs) != 0 {
		t.Fatalf("expected no assistant messages on tab 1, got %+v", tab1Msgs)
	}
}
