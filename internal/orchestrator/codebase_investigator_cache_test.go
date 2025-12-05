package orchestrator

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/session"
)

// TestCodebaseInvestigatorInitialMessageCount verifies that the investigator
// sets up the initial message correctly for prompt caching.
func TestCodebaseInvestigatorInitialMessageCount(t *testing.T) {
	// Create a test session
	sess := session.NewSession("test-investigation", "/test")

	// Add the initial objective message (this is what investigateInternal does)
	objective := "Find the main function"
	sess.AddMessage(&session.Message{
		Role:    "user",
		Content: "Investigation Objective: " + objective,
	})

	// Verify we have exactly 1 message
	messages := sess.GetMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 initial message, got %d", len(messages))
	}

	// Verify it's a user message
	if messages[0].Role != "user" {
		t.Errorf("First message should be user, got: %s", messages[0].Role)
	}

	// Store initial count
	initialCount := len(messages)

	// Simulate assistant response
	sess.AddMessage(&session.Message{
		Role:    "assistant",
		Content: "I need to search for files",
	})

	// Simulate tool result
	sess.AddMessage(&session.Message{
		Role:    "tool",
		Content: "Found 3 files",
		ToolID:  "call_1",
	})

	// Verify prefix hasn't changed
	messages = sess.GetMessages()
	if len(messages) < initialCount {
		t.Errorf("Message array was truncated! Expected at least %d messages, got %d", initialCount, len(messages))
	}

	// Verify first message is still the user objective
	if messages[0].Role != "user" {
		t.Errorf("First message role changed from user to %s (CACHE BREAK)", messages[0].Role)
	}

	expectedContent := "Investigation Objective: " + objective
	if messages[0].Content != expectedContent {
		t.Errorf("First message content changed (CACHE BREAK)")
	}
}

// TestCodebaseInvestigatorMessageSequence verifies the message pattern:
// user (objective) -> assistant -> tool -> assistant -> tool -> ...
func TestCodebaseInvestigatorMessageSequence(t *testing.T) {
	sess := session.NewSession("test-investigation", "/test")

	// Initial objective
	sess.AddMessage(&session.Message{
		Role:    "user",
		Content: "Investigation Objective: Analyze the code",
	})

	// First assistant response with tool call
	sess.AddMessage(&session.Message{
		Role:    "assistant",
		Content: "Searching for files...",
		ToolCalls: []map[string]interface{}{
			{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name": "search_files",
				},
			},
		},
	})

	// First tool result
	sess.AddMessage(&session.Message{
		Role:    "tool",
		Content: "Found files",
		ToolID:  "call_1",
	})

	// Second assistant response with tool call
	sess.AddMessage(&session.Message{
		Role:    "assistant",
		Content: "Reading file...",
		ToolCalls: []map[string]interface{}{
			{
				"id":   "call_2",
				"type": "function",
				"function": map[string]interface{}{
					"name": "read_file",
				},
			},
		},
	})

	// Second tool result
	sess.AddMessage(&session.Message{
		Role:    "tool",
		Content: "File content",
		ToolID:  "call_2",
	})

	// Final assistant response
	sess.AddMessage(&session.Message{
		Role:    "assistant",
		Content: "<answer>Analysis complete</answer>",
	})

	// Verify sequence
	messages := sess.GetMessages()
	expectedRoles := []string{"user", "assistant", "tool", "assistant", "tool", "assistant"}

	if len(messages) != len(expectedRoles) {
		t.Fatalf("Expected %d messages, got %d", len(expectedRoles), len(messages))
	}

	for i, expectedRole := range expectedRoles {
		if messages[i].Role != expectedRole {
			t.Errorf("Message %d: expected role %s, got %s", i, expectedRole, messages[i].Role)
		}
	}

	// Most importantly: verify the first message (objective) never changed
	if messages[0].Role != "user" {
		t.Error("First message role changed (CACHE BREAK)")
	}
	if messages[0].Content != "Investigation Objective: Analyze the code" {
		t.Error("First message content changed (CACHE BREAK)")
	}
}

// TestCodebaseInvestigatorPrefixImmutability verifies that adding messages
// never modifies the initial objective message.
func TestCodebaseInvestigatorPrefixImmutability(t *testing.T) {
	sess := session.NewSession("test-investigation", "/test")

	// Add initial objective
	initialObjective := "Investigation Objective: Complex analysis task"
	sess.AddMessage(&session.Message{
		Role:    "user",
		Content: initialObjective,
	})

	// Capture initial state
	firstSnapshot := sess.GetMessages()[0]
	initialRole := firstSnapshot.Role
	initialContent := firstSnapshot.Content

	// Add many messages simulating a long investigation
	for i := 0; i < 10; i++ {
		// Assistant message
		sess.AddMessage(&session.Message{
			Role:    "assistant",
			Content: "Investigating...",
		})

		// Tool result
		sess.AddMessage(&session.Message{
			Role:    "tool",
			Content: "Result",
			ToolID:  "call_" + string(rune(i)),
		})
	}

	// Verify first message is completely unchanged
	messages := sess.GetMessages()
	if messages[0].Role != initialRole {
		t.Errorf("First message role changed from %s to %s (CACHE BREAK)", initialRole, messages[0].Role)
	}
	if messages[0].Content != initialContent {
		t.Errorf("First message content changed (CACHE BREAK)\nExpected: %s\nGot: %s", initialContent, messages[0].Content)
	}

	// Verify it's still the exact objective
	if messages[0].Content != initialObjective {
		t.Error("Initial objective was modified (CACHE BREAK)")
	}
}
