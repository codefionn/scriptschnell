package orchestrator

import (
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/session"
)

func TestHasLoopInRecentMessages_NoLoop(t *testing.T) {
	messages := []*session.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there! How can I help you today?"},
		{Role: "user", Content: "Can you help me with a task?"},
		{Role: "assistant", Content: "Of course! What task would you like help with?"},
	}

	if hasLoopInRecentMessages(messages) {
		t.Errorf("Expected no loop for varied messages")
	}
}

func TestHasLoopInRecentMessages_WithSimpleLoop(t *testing.T) {
	// Create messages with repetitive assistant responses
	messages := []*session.Message{
		{Role: "user", Content: "Start task"},
	}

	// Add the same assistant message 11 times (threshold is 10)
	for i := 0; i < 11; i++ {
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "I'm processing your request. Please wait.",
		})
	}

	if !hasLoopInRecentMessages(messages) {
		t.Errorf("Expected loop to be detected for repetitive messages")
	}
}

func TestHasLoopInRecentMessages_WithPatternLoop(t *testing.T) {
	// Create messages with a repeating 2-sentence pattern
	messages := []*session.Message{
		{Role: "user", Content: "Do something"},
	}

	pattern := "First step is done. Moving to next step."
	for i := 0; i < 11; i++ {
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: pattern,
		})
	}

	if !hasLoopInRecentMessages(messages) {
		t.Errorf("Expected loop to be detected for repeating pattern")
	}
}

func TestHasLoopInRecentMessages_EmptyMessages(t *testing.T) {
	messages := []*session.Message{}

	if hasLoopInRecentMessages(messages) {
		t.Errorf("Expected no loop for empty messages")
	}
}

func TestHasLoopInRecentMessages_OnlyUserMessages(t *testing.T) {
	messages := []*session.Message{
		{Role: "user", Content: "Message 1"},
		{Role: "user", Content: "Message 2"},
		{Role: "user", Content: "Message 3"},
	}

	if hasLoopInRecentMessages(messages) {
		t.Errorf("Expected no loop when there are no assistant messages")
	}
}

func TestHasLoopInRecentMessages_OnlyRecentMessages(t *testing.T) {
	// Create many old messages followed by looping recent ones
	messages := []*session.Message{}

	// Add 20 varied messages
	for i := 0; i < 20; i++ {
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "Unique message number " + string(rune('A'+i)),
		})
	}

	// Add 11 repetitive messages at the end
	for i := 0; i < 11; i++ {
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "This is the loop.",
		})
	}

	if !hasLoopInRecentMessages(messages) {
		t.Errorf("Expected loop to be detected in recent messages")
	}
}

func TestHasLoopInRecentMessages_EmptyContent(t *testing.T) {
	messages := []*session.Message{
		{Role: "assistant", Content: ""},
		{Role: "assistant", Content: "   "},
		{Role: "assistant", Content: "\n\n"},
	}

	if hasLoopInRecentMessages(messages) {
		t.Errorf("Expected no loop for empty content messages")
	}
}

func TestHasLoopInRecentMessages_MixedRoles(t *testing.T) {
	messages := []*session.Message{}

	// Interleave user and assistant messages with loops
	for i := 0; i < 6; i++ {
		messages = append(messages, &session.Message{
			Role:    "user",
			Content: "User message " + string(rune('0'+i)),
		})
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "Looping response.",
		})
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "Looping response.",
		})
	}

	if !hasLoopInRecentMessages(messages) {
		t.Errorf("Expected loop to be detected across mixed role messages")
	}
}
