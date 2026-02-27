package session

import (
	"runtime"
	"testing"
	"time"
)

func TestCompactWithSummarySuccess(t *testing.T) {
	s := NewSession("test", ".")

	first := &Message{Role: "user", Content: "initial question"}
	second := &Message{Role: "assistant", Content: "response"}

	s.AddMessage(first)
	s.AddMessage(second)

	head := s.GetMessages()

	ok := s.CompactWithSummary(head[:1], "Summary content")
	if !ok {
		t.Fatalf("expected compaction to succeed")
	}

	messages := s.GetMessages()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages after compaction, got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Fatalf("expected first message role to be system, got %s", messages[0].Role)
	}

	if messages[1] != second {
		t.Fatalf("expected second message to remain unchanged")
	}
}

func TestCompactWithSummaryMismatch(t *testing.T) {
	s := NewSession("test", ".")

	s.AddMessage(&Message{Role: "user", Content: "one"})
	s.AddMessage(&Message{Role: "assistant", Content: "two"})

	original := []*Message{{Role: "user", Content: "not from session"}}

	if ok := s.CompactWithSummary(original, "noop"); ok {
		t.Fatal("expected compaction to fail when messages do not match session head")
	}
}

func TestSaveEmptySession(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	setSessionStorageEnv(t, tempDir)

	// Create session storage
	storage, err := NewSessionStorage()
	if err != nil {
		t.Fatalf("Failed to create session storage: %v", err)
	}

	// Test 1: Try to save an empty session
	emptySession := NewSession("test-empty", tempDir)
	err = storage.SaveSession(emptySession, "Empty Session")
	if err != nil {
		t.Errorf("Unexpected error saving empty session: %v", err)
	}

	// Verify no session was saved
	sessions, err := storage.ListSessions(tempDir)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions after saving empty session, got %d", len(sessions))
	}

	// Test 2: Save a session with messages
	sessionWithMessages := NewSession("test-with-messages", tempDir)
	sessionWithMessages.AddMessage(&Message{
		Role:    "user",
		Content: "Hello, world!",
	})
	err = storage.SaveSession(sessionWithMessages, "Session with Messages")
	if err != nil {
		t.Errorf("Unexpected error saving session with messages: %v", err)
	}

	// Verify session was saved
	sessions, err = storage.ListSessions(tempDir)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session after saving with messages, got %d", len(sessions))
	}
	if sessions[0].MessageCount != 1 {
		t.Errorf("Expected 1 message in saved session, got %d", sessions[0].MessageCount)
	}
}

func TestSaveSessionStorageDirectoryOverride(t *testing.T) {
	// This test validates the behavior when we need to test with a specific directory
	// In practice, the actual tests above work with the system's session storage
	tempDir := t.TempDir()

	// Create a session
	s := NewSession("test", tempDir)

	// Initially it should be dirty
	if !s.IsDirty() {
		t.Error("New session should be dirty")
	}

	// Add a message
	s.AddMessage(&Message{Role: "user", Content: "test"})

	// Should still be dirty
	if !s.IsDirty() {
		t.Error("Session with new message should be dirty")
	}

	// Mark as saved
	s.MarkSaved(s.UpdatedAt)

	// Should no longer be dirty
	if s.IsDirty() {
		t.Error("Session should not be dirty after being marked as saved")
	}
}

func TestSaveSessionStripsNativeFormat(t *testing.T) {
	tempDir := t.TempDir()
	setSessionStorageEnv(t, tempDir)

	storage, err := NewSessionStorage()
	if err != nil {
		t.Fatalf("Failed to create session storage: %v", err)
	}

	type nativePayload struct {
		Foo string
	}

	s := NewSession("test-native", tempDir)
	s.AddMessage(&Message{
		Role:              "user",
		Content:           "Hello",
		NativeFormat:      nativePayload{Foo: "bar"},
		NativeProvider:    "anthropic",
		NativeModelFamily: "claude-3",
		NativeTimestamp:   time.Now(),
	})

	if err := storage.SaveSession(s, "Native Format Session"); err != nil {
		t.Fatalf("Unexpected error saving session with native format: %v", err)
	}

	loaded, err := storage.LoadSession(tempDir, s.ID)
	if err != nil {
		t.Fatalf("Failed to load saved session: %v", err)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].NativeFormat != nil {
		t.Fatalf("Expected native format to be stripped on save")
	}
	if loaded.Messages[0].NativeProvider != "" || loaded.Messages[0].NativeModelFamily != "" {
		t.Fatalf("Expected native provider fields to be cleared on save")
	}
	if !loaded.Messages[0].NativeTimestamp.IsZero() {
		t.Fatalf("Expected native timestamp to be zero on save")
	}
}

func TestUserMessageCount(t *testing.T) {
	s := NewSession("count-test", ".")

	if count := s.UserMessageCount(); count != 0 {
		t.Fatalf("expected 0 user messages, got %d", count)
	}

	s.AddMessage(&Message{Role: "system", Content: "setup"})
	s.AddMessage(&Message{Role: "user", Content: "first"})
	s.AddMessage(&Message{Role: "assistant", Content: "reply"})
	s.AddMessage(&Message{Role: "user", Content: "second"})

	if count := s.UserMessageCount(); count != 2 {
		t.Fatalf("expected 2 user messages, got %d", count)
	}
}

func TestCompactWithSummary_EmptyOriginal(t *testing.T) {
	s := NewSession("test", ".")
	s.AddMessage(&Message{Role: "user", Content: "hello"})

	ok := s.CompactWithSummary([]*Message{}, "summary")
	if ok {
		t.Fatal("expected compaction to fail with empty original")
	}
}

func TestCompactWithSummary_OriginalLongerThanSession(t *testing.T) {
	s := NewSession("test", ".")
	s.AddMessage(&Message{Role: "user", Content: "one"})

	big := []*Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
	}

	ok := s.CompactWithSummary(big, "summary")
	if ok {
		t.Fatal("expected compaction to fail when original is longer than session")
	}
}

func TestCompactWithSummary_CompactsAllMessages(t *testing.T) {
	s := NewSession("test", ".")
	msg1 := &Message{Role: "user", Content: "one"}
	msg2 := &Message{Role: "assistant", Content: "two"}
	s.AddMessage(msg1)
	s.AddMessage(msg2)

	all := s.GetMessages()
	ok := s.CompactWithSummary(all, "Full summary")
	if !ok {
		t.Fatal("expected compaction to succeed")
	}

	messages := s.GetMessages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message after compacting all, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Fatalf("expected system role, got %s", messages[0].Role)
	}
	if messages[0].Content != "Full summary" {
		t.Fatalf("expected summary content, got %q", messages[0].Content)
	}
}

func TestCompactWithSummary_SetsDirtyFlag(t *testing.T) {
	s := NewSession("test", ".")
	msg1 := &Message{Role: "user", Content: "one"}
	msg2 := &Message{Role: "assistant", Content: "two"}
	s.AddMessage(msg1)
	s.AddMessage(msg2)

	s.MarkSaved(s.UpdatedAt)
	if s.IsDirty() {
		t.Fatal("expected not dirty after MarkSaved")
	}

	head := s.GetMessages()
	ok := s.CompactWithSummary(head[:1], "Summary")
	if !ok {
		t.Fatal("expected compaction to succeed")
	}
	if !s.IsDirty() {
		t.Fatal("expected dirty after compaction")
	}
}

func setSessionStorageEnv(t *testing.T, dir string) {
	t.Helper()
	switch runtime.GOOS {
	case "linux":
		t.Setenv("XDG_STATE_HOME", dir)
	case "windows":
		t.Setenv("LOCALAPPDATA", dir)
	default:
		t.Setenv("HOME", dir)
	}
}
