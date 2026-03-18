package llm

import (
	"testing"
)

func TestSanitizeMessages_NoRepairNeeded(t *testing.T) {
	msgs := []*Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	result, repaired := SanitizeMessages(msgs)
	if repaired {
		t.Error("expected no repair needed")
	}
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}
}

func TestSanitizeMessages_OrphanToolResponseAtStart(t *testing.T) {
	msgs := []*Message{
		{Role: "system", Content: "Summary of context."},
		{Role: "tool", Content: "result", ToolID: "call_123", ToolName: "read_file"},
		{Role: "user", Content: "Continue"},
	}
	result, repaired := SanitizeMessages(msgs)
	if !repaired {
		t.Error("expected repair")
	}
	// The orphan tool message should be removed
	for _, m := range result {
		if m.Role == "tool" {
			t.Error("orphan tool message should have been removed")
		}
	}
}

func TestSanitizeMessages_OrphanToolResponseMiddle(t *testing.T) {
	msgs := []*Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Let me help."},
		// This tool response has no matching tool call
		{Role: "tool", Content: "some result", ToolID: "call_orphan", ToolName: "read_file"},
		{Role: "user", Content: "Thanks"},
	}
	result, repaired := SanitizeMessages(msgs)
	if !repaired {
		t.Error("expected repair")
	}
	// The orphan tool response should be removed
	for _, m := range result {
		if m.Role == "tool" {
			t.Error("orphan tool message should have been removed")
		}
	}
}

func TestSanitizeMessages_MissingToolResponses(t *testing.T) {
	msgs := []*Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "I'll use a tool.", ToolCalls: []map[string]any{
			{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file"}},
		}},
		// No tool response follows - it was compacted away
		{Role: "user", Content: "Continue"},
	}
	result, repaired := SanitizeMessages(msgs)
	if !repaired {
		t.Error("expected repair")
	}
	// The assistant message should have its tool_calls stripped
	for _, m := range result {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			t.Error("assistant message should have had tool_calls stripped")
		}
	}
}

func TestSanitizeMessages_PartialToolResponses(t *testing.T) {
	msgs := []*Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "I'll use tools.", ToolCalls: []map[string]any{
			{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file"}},
			{"id": "call_2", "type": "function", "function": map[string]any{"name": "write_file"}},
		}},
		// Only one tool response
		{Role: "tool", Content: "file content", ToolID: "call_1", ToolName: "read_file"},
		{Role: "user", Content: "Continue"},
	}
	result, repaired := SanitizeMessages(msgs)
	if !repaired {
		t.Error("expected repair")
	}
	// The assistant message should only keep call_1
	for _, m := range result {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			if len(m.ToolCalls) != 1 {
				t.Errorf("expected 1 tool call, got %d", len(m.ToolCalls))
			}
			if id, ok := m.ToolCalls[0]["id"].(string); ok && id != "call_1" {
				t.Errorf("expected call_1, got %s", id)
			}
		}
	}
}

func TestSanitizeMessages_FirstMessageNotUser(t *testing.T) {
	msgs := []*Message{
		{Role: "assistant", Content: "I was mid-thought."},
		{Role: "user", Content: "Continue"},
	}
	result, repaired := SanitizeMessages(msgs)
	if !repaired {
		t.Error("expected repair")
	}
	if result[0].Role != "user" {
		t.Errorf("expected first message to be user, got %s", result[0].Role)
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected second message to be assistant, got %s", result[1].Role)
	}
}

func TestSanitizeMessages_ConsecutiveUserMessages(t *testing.T) {
	msgs := []*Message{
		{Role: "user", Content: "Hello"},
		{Role: "user", Content: "Are you there?"},
		{Role: "assistant", Content: "Yes!"},
	}
	result, repaired := SanitizeMessages(msgs)
	if !repaired {
		t.Error("expected repair")
	}
	if len(result) != 2 {
		t.Errorf("expected 2 messages after merge, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Error("first message should be user")
	}
	if result[0].Content != "Hello\n\nAre you there?" {
		t.Errorf("unexpected merged content: %s", result[0].Content)
	}
}

func TestSanitizeMessages_ValidToolExchange(t *testing.T) {
	msgs := []*Message{
		{Role: "user", Content: "Read my file."},
		{Role: "assistant", Content: "", ToolCalls: []map[string]any{
			{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file"}},
		}},
		{Role: "tool", Content: "file content", ToolID: "call_1", ToolName: "read_file"},
		{Role: "assistant", Content: "Here's the file content."},
	}
	result, repaired := SanitizeMessages(msgs)
	if repaired {
		t.Error("expected no repair needed for valid tool exchange")
	}
	if len(result) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result))
	}
}

func TestSanitizeMessages_PostCompactionScenario(t *testing.T) {
	// Simulate a typical post-compaction race: system summary + orphan tool response + rest
	msgs := []*Message{
		{Role: "system", Content: "Summary of earlier context (auto-compacted): ..."},
		{Role: "tool", Content: "result of some tool", ToolID: "call_old", ToolName: "sandbox"},
		{Role: "assistant", Content: "Based on the results..."},
		{Role: "user", Content: "Now do X."},
	}
	result, repaired := SanitizeMessages(msgs)
	if !repaired {
		t.Error("expected repair for post-compaction scenario")
	}
	// Should not have orphan tool messages
	for _, m := range result {
		if m.Role == "tool" {
			t.Error("orphan tool message should have been removed")
		}
	}
	// First non-system should be user
	for _, m := range result {
		if m.Role == "system" {
			continue
		}
		if m.Role != "user" {
			t.Errorf("first non-system message should be user, got %s", m.Role)
		}
		break
	}
}

func TestSanitizeMessages_Empty(t *testing.T) {
	result, repaired := SanitizeMessages(nil)
	if repaired {
		t.Error("expected no repair for nil")
	}
	if result != nil {
		t.Error("expected nil result")
	}

	result, repaired = SanitizeMessages([]*Message{})
	if repaired {
		t.Error("expected no repair for empty")
	}
	if len(result) != 0 {
		t.Error("expected empty result")
	}
}
