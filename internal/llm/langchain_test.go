package llm

import (
	"reflect"
	"testing"
)

func TestTransformMessagesForProvider_MistralMergesAssistantMessages(t *testing.T) {
	client := &LangChainClient{provider: "mistral"}

	toolCall := map[string]interface{}{
		"id": "call-1",
		"function": map[string]interface{}{
			"name":      "read_file",
			"arguments": `{"path":"main.go"}`,
		},
	}

	original := []*Message{
		{Role: "user", Content: "What does the file contain?"},
		{Role: "assistant", Content: "Let me take a look."},
		{Role: "assistant", Content: "I'll read the file now.", ToolCalls: []map[string]interface{}{toolCall}},
		{Role: "tool", ToolID: "call-1", ToolName: "read_file", Content: "file contents"},
	}

	transformed := client.transformMessagesForProvider(original)

	if len(transformed) != 3 {
		t.Fatalf("expected 3 messages after merge, got %d", len(transformed))
	}

	merged := transformed[1]
	if merged.Role != "assistant" {
		t.Fatalf("expected merged message role assistant, got %s", merged.Role)
	}

	expectedContent := "Let me take a look.\nI'll read the file now."
	if merged.Content != expectedContent {
		t.Fatalf("expected merged content %q, got %q", expectedContent, merged.Content)
	}

	if len(merged.ToolCalls) != 1 {
		t.Fatalf("expected merged tool calls length 1, got %d", len(merged.ToolCalls))
	}

	// Ensure original slice was not mutated.
	if original[1].Content != "Let me take a look." {
		t.Fatalf("expected original message content to remain unchanged, got %q", original[1].Content)
	}
	if len(original[2].ToolCalls) != 1 {
		t.Fatalf("expected original tool calls to remain intact")
	}
}

func TestTransformMessagesForProvider_OtherProviderNoChange(t *testing.T) {
	client := &LangChainClient{provider: "openai"}

	messages := []*Message{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello"},
	}

	transformed := client.transformMessagesForProvider(messages)

	if !reflect.DeepEqual(transformed, messages) {
		t.Fatalf("expected messages to remain unchanged for non-mistral provider")
	}
}

func TestTrimTrailingAssistantMessages(t *testing.T) {
	client := &LangChainClient{provider: "mistral"}

	messages := []*Message{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Working on it"},
	}

	transformed := client.transformMessagesForProvider(messages)

	if len(transformed) != 1 {
		t.Fatalf("expected trailing assistant message to be trimmed, got %d messages", len(transformed))
	}
	if transformed[0].Role != "user" {
		t.Fatalf("expected remaining message to be user, got %s", transformed[0].Role)
	}
}

func TestTrimTrailingSystemMessages(t *testing.T) {
	client := &LangChainClient{provider: "mistral"}

	messages := []*Message{
		{Role: "user", Content: "Hi"},
		{Role: "system", Content: "summary"},
	}

	transformed := client.transformMessagesForProvider(messages)
	if len(transformed) != 1 {
		t.Fatalf("expected trailing system message to be trimmed, got %d messages", len(transformed))
	}
	if transformed[0].Role != "user" {
		t.Fatalf("expected remaining message to be user, got %s", transformed[0].Role)
	}
}
