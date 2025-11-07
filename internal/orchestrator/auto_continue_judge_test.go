package orchestrator

import (
	"strings"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/session"
)

func TestCheckMessagesForLoops_NoLoop(t *testing.T) {
	messages := []*session.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there! How can I help you today?"},
		{Role: "user", Content: "Can you help me with a task?"},
		{Role: "assistant", Content: "Of course! What task would you like help with?"},
	}

	hasLoop, _ := checkMessagesForLoops(messages, 10, "assistant")
	if hasLoop {
		t.Errorf("Expected no loop for varied messages")
	}
}

func TestCheckMessagesForLoops_WithSimpleLoop(t *testing.T) {
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


	hasLoop, info := checkMessagesForLoops(messages, 10, "assistant")
	if !hasLoop {
		t.Errorf("Expected loop to be detected for repetitive messages")
	}
	if !strings.Contains(info, "pattern repeated") {
		t.Errorf("Expected loop info to contain pattern description, got: %s", info)
	}
}

func TestCheckMessagesForLoops_WithPatternLoop(t *testing.T) {
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

	hasLoop, _ := checkMessagesForLoops(messages, 10, "assistant")
	if !hasLoop {
		t.Errorf("Expected loop to be detected for repeating pattern")
	}
}

func TestCheckMessagesForLoops_EmptyMessages(t *testing.T) {
	messages := []*session.Message{}

	hasLoop, _ := checkMessagesForLoops(messages, 10, "assistant")
	if hasLoop {
		t.Errorf("Expected no loop for empty messages")
	}
}

func TestCheckMessagesForLoops_OnlyUserMessages(t *testing.T) {
	messages := []*session.Message{
		{Role: "user", Content: "Message 1"},
		{Role: "user", Content: "Message 2"},
		{Role: "user", Content: "Message 3"},
	}

	hasLoop, _ := checkMessagesForLoops(messages, 10, "assistant")
	if hasLoop {
		t.Errorf("Expected no loop when there are no assistant messages")
	}
}

func TestCheckMessagesForLoops_OnlyRecentMessages(t *testing.T) {
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

	hasLoop, _ := checkMessagesForLoops(messages, 10, "assistant")
	if !hasLoop {
		t.Errorf("Expected loop to be detected in recent messages")
	}
}

func TestCheckMessagesForLoops_EmptyContent(t *testing.T) {
	messages := []*session.Message{
		{Role: "assistant", Content: ""},
		{Role: "assistant", Content: "   "},
		{Role: "assistant", Content: "\n\n"},
	}

	hasLoop, _ := checkMessagesForLoops(messages, 10, "assistant")
	if hasLoop {
		t.Errorf("Expected no loop for empty content messages")
	}
}

func TestCheckMessagesForLoops_MixedRoles(t *testing.T) {
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

	hasLoop, _ := checkMessagesForLoops(messages, 10, "assistant")
	if !hasLoop {
		t.Errorf("Expected loop to be detected across mixed role messages")
	}
}

// Test new parameters: maxMessages = 0 (check all messages)
func TestCheckMessagesForLoops_CheckAllMessages(t *testing.T) {
	messages := []*session.Message{}

	// Add 15 repetitive messages (more than default maxMessages=10)
	for i := 0; i < 15; i++ {
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "Repeated content.",
		})
	}

	// With maxMessages=0, should check all 15 messages
	hasLoop, _ := checkMessagesForLoops(messages, 0, "assistant")
	if !hasLoop {
		t.Errorf("Expected loop to be detected when checking all messages")
	}
}

// Test new parameters: roleFilter = "" (all roles)
func TestCheckMessagesForLoops_AllRoles(t *testing.T) {
	messages := []*session.Message{}

	// Add repetitive messages from different roles
	for i := 0; i < 11; i++ {
		messages = append(messages, &session.Message{
			Role:    "user",
			Content: "Same user message.",
		})
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "Same assistant message.",
		})
	}

	// With roleFilter="", should detect loop across all roles
	hasLoop, _ := checkMessagesForLoops(messages, 25, "")
	if !hasLoop {
		t.Errorf("Expected loop to be detected across all roles")
	}
}

// Test that maxMessages limits the search
func TestCheckMessagesForLoops_RespectMaxMessages(t *testing.T) {
	messages := []*session.Message{}

	// Add 5 varied old messages
	for i := 0; i < 5; i++ {
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "Unique message " + string(rune('A'+i)),
		})
	}

	// Add 11 repetitive recent messages
	for i := 0; i < 11; i++ {
		messages = append(messages, &session.Message{
			Role:    "assistant",
			Content: "Loop content.",
		})
	}

	// With maxMessages=3, should only check last 3 messages (not enough to detect loop)
	hasLoop, _ := checkMessagesForLoops(messages, 3, "assistant")
	if hasLoop {
		t.Errorf("Expected no loop when maxMessages is too small to detect pattern")
	}

	// With maxMessages=11, should detect the loop
	hasLoop, _ = checkMessagesForLoops(messages, 11, "assistant")
	if !hasLoop {
		t.Errorf("Expected loop when maxMessages is sufficient")
	}
}
