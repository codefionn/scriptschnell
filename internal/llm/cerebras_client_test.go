package llm

import (
	"testing"
)

func TestConvertMessagesToCerebrasIncludesSystemPrompt(t *testing.T) {
	req := &CompletionRequest{
		SystemPrompt: "Always be helpful.",
		Messages: []*Message{
			{Role: "user", Content: "Hi"},
		},
	}

	msgs, err := convertMessagesToCerebras(req)
	if err != nil {
		t.Fatalf("convertMessagesToCerebras returned error: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "system" || msgs[0].Content != "Always be helpful." {
		t.Fatalf("system prompt not injected correctly: %#v", msgs[0])
	}

	if msgs[1].Role != "user" || msgs[1].Content != "Hi" {
		t.Fatalf("user message not preserved: %#v", msgs[1])
	}
}

func TestConvertMessagesToCerebrasRequiresMessage(t *testing.T) {
	_, err := convertMessagesToCerebras(&CompletionRequest{})
	if err == nil {
		t.Fatal("expected error when no messages present")
	}
}

func TestCerebrasBuildChatRequest(t *testing.T) {
	client := &CerebrasClient{model: "gpt-oss-120b"}
	req := &CompletionRequest{
		SystemPrompt: "system",
		Messages: []*Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 0.3,
		MaxTokens:   256,
		Tools: []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name": "noop",
				},
			},
		},
	}

	payload, err := client.buildChatRequest(req, false)
	if err != nil {
		t.Fatalf("buildChatRequest returned error: %v", err)
	}

	if payload.Model != "gpt-oss-120b" {
		t.Fatalf("expected model gpt-oss-120b, got %s", payload.Model)
	}

	if payload.Stream {
		t.Fatalf("expected non-streaming payload")
	}

	if len(payload.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(payload.Messages))
	}

	if payload.Temperature == nil || *payload.Temperature != 0.3 {
		t.Fatalf("temperature not propagated: %v", payload.Temperature)
	}

	if payload.MaxCompletionTokens != 256 {
		t.Fatalf("expected MaxCompletionTokens=256, got %d", payload.MaxCompletionTokens)
	}

	if len(payload.Tools) != 1 || payload.ToolChoice != "auto" {
		t.Fatalf("tools not attached correctly: %+v", payload.Tools)
	}
}

func TestConvertCerebrasToolCalls(t *testing.T) {
	toolCalls := []cerebrasToolCall{
		{
			ID:   "call_123",
			Type: "function",
			Function: cerebrasToolCallFunction{
				Name:      "lookup",
				Arguments: `{"id":1}`,
			},
		},
	}

	result := convertCerebrasToolCalls(toolCalls)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result))
	}

	call := result[0]
	if call["id"] != "call_123" {
		t.Fatalf("unexpected id: %v", call["id"])
	}

	fn, _ := call["function"].(map[string]interface{})
	if fn["name"] != "lookup" || fn["arguments"] != `{"id":1}` {
		t.Fatalf("unexpected function payload: %#v", fn)
	}
}

func TestConvertMessagesToCerebras_NativeFormat(t *testing.T) {
	// Create messages with native Cerebras format
	req := &CompletionRequest{
		SystemPrompt: "Be helpful",
		Messages: []*Message{
			{
				Role:           "user",
				Content:        "Hello",
				NativeFormat:   map[string]interface{}{"role": "system", "content": "Be helpful"},
				NativeProvider: "cerebras",
			},
			{
				Role:           "user",
				Content:        "Hello",
				NativeFormat:   map[string]interface{}{"role": "user", "content": "Hello"},
				NativeProvider: "cerebras",
			},
		},
	}

	msgs, err := convertMessagesToCerebras(req)
	if err != nil {
		t.Fatalf("convertMessagesToCerebras returned error: %v", err)
	}

	// Should have 2 messages: system (from native) + user (from native)
	// System prompt should NOT be duplicated
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (no duplicate system prompt), got %d", len(msgs))
	}

	// First message should be system from native format
	if msgs[0].Role != "system" || msgs[0].Content != "Be helpful" {
		t.Errorf("first message should be system from native format, got: role=%s content=%s", msgs[0].Role, msgs[0].Content)
	}

	// Second message should be user from native format
	if msgs[1].Role != "user" || msgs[1].Content != "Hello" {
		t.Errorf("second message should be user from native format, got: role=%s content=%s", msgs[1].Role, msgs[1].Content)
	}
}

func TestConvertMessagesToCerebras_FallbackOnMissingNative(t *testing.T) {
	// Create messages with native format but missing NativeFormat field
	req := &CompletionRequest{
		SystemPrompt: "Be helpful",
		Messages: []*Message{
			{
				Role:           "user",
				Content:        "Hello",
				NativeProvider: "cerebras", // Has provider but no NativeFormat
			},
		},
	}

	msgs, err := convertMessagesToCerebras(req)
	if err != nil {
		t.Fatalf("convertMessagesToCerebras returned error: %v", err)
	}

	// Should fall back to unified conversion
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user from fallback), got %d", len(msgs))
	}

	if msgs[0].Role != "system" || msgs[0].Content != "Be helpful" {
		t.Errorf("fallback should include system prompt, got: role=%s content=%s", msgs[0].Role, msgs[0].Content)
	}
}
