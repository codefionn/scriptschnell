package session

import "testing"

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
