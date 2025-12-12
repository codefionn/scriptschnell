package tui

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestRestoreLoadedSessionReplacesActiveTabState(t *testing.T) {
	m := New("test-model", "", false)
	m.config = &config.Config{}

	origSession := session.NewSession("orig", "/orig")
	tab := &TabSession{
		ID:        1,
		Session:   origSession,
		Messages:  []message{{role: "System", content: "cached"}},
		CreatedAt: origSession.CreatedAt,
	}
	m.sessions = []*TabSession{tab}
	m.activeSessionIdx = 0
	m.messages = tab.Messages
	m.processingStatus = "Generating..."
	m.contentReceived = true
	m.thinkingTokens = 42
	m.spinnerActive = true

	loaded := session.NewSession("loaded", "/loaded")
	loaded.AddMessage(&session.Message{Role: "user", Content: "hello"})

	m.RestoreLoadedSession(&LoadedSessionInfo{
		Session: loaded,
		Name:    "restored",
	})

	if m.processingStatus != "" || m.contentReceived || m.thinkingTokens != 0 {
		t.Fatalf("expected generation state reset, got status=%q contentReceived=%v thinkingTokens=%d", m.processingStatus, m.contentReceived, m.thinkingTokens)
	}
	if m.spinnerActive {
		t.Fatal("expected spinner to stop after restore")
	}
	if got := len(m.messages); got != 1 || m.messages[0].content != "hello" || m.messages[0].role != "You" {
		t.Fatalf("unexpected messages after restore: %+v", m.messages)
	}
	if m.sessions[0].Session != loaded {
		t.Fatalf("expected active tab session to be replaced")
	}
	if m.sessions[0].Name != "restored" {
		t.Fatalf("expected tab name to update, got %q", m.sessions[0].Name)
	}
}

func TestClearMessagesResetsActiveTab(t *testing.T) {
	m := newModelWithTabs(t, 1)
	m.messages = []message{{role: "Assistant", content: "keep"}}
	m.sessions[0].Messages = m.messages

	m.ClearMessages()

	if len(m.messages) != 0 {
		t.Fatalf("expected cleared messages, got %d", len(m.messages))
	}
	if len(m.sessions[0].Messages) != 0 {
		t.Fatalf("expected tab messages cleared, got %d", len(m.sessions[0].Messages))
	}
}

func TestSessionMessagesToTuiMessagesConversion(t *testing.T) {
	s := session.NewSession("s1", "/tmp")
	s.AddMessage(&session.Message{Role: "Assistant", Content: "hi"})
	s.AddMessage(&session.Message{Role: "Tool", Content: "done", ToolName: "write_file", ToolID: "1"})

	messages := sessionMessagesToTuiMessages(s.GetMessages())
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].role != "Assistant" || messages[0].content != "hi" {
		t.Fatalf("unexpected first message: %+v", messages[0])
	}
	if messages[1].role != "Tool" || messages[1].toolName != "write_file" || messages[1].toolID != "1" || messages[1].content != "done" {
		t.Fatalf("unexpected tool message conversion: %+v", messages[1])
	}
}
