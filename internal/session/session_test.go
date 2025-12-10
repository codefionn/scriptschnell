package session

import (
	"testing"
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
