package llm

import "testing"

func TestNormalizeToolCallIDs(t *testing.T) {
	calls := []map[string]interface{}{
		{
			"type": "function",
			"id":   "",
			"function": map[string]interface{}{
				"name":      "read_file",
				"arguments": "{}",
			},
		},
		{
			"type": "function",
			// call_id should be preferred when present
			"call_id": "tc-123",
			"function": map[string]interface{}{
				"name": "search-files",
			},
		},
		nil, // should be skipped without panic
	}

	normalized := NormalizeToolCallIDs(calls)

	if normalized[0]["id"] == "" || normalized[0]["call_id"] == "" {
		t.Fatalf("expected generated id for first call, got id=%v call_id=%v", normalized[0]["id"], normalized[0]["call_id"])
	}
	if normalized[1]["id"] != "tc-123" || normalized[1]["call_id"] != "tc-123" {
		t.Fatalf("expected existing call_id to be preserved, got id=%v call_id=%v", normalized[1]["id"], normalized[1]["call_id"])
	}
}

func TestNormalizeToolCallIDs_MissingType(t *testing.T) {
	// Ollama and some providers omit the "type" field
	calls := []map[string]interface{}{
		{
			"function": map[string]interface{}{
				"name":      "read_file",
				"arguments": `{"path": "main.go"}`,
			},
		},
	}

	normalized := NormalizeToolCallIDs(calls)

	if typ, _ := normalized[0]["type"].(string); typ != "function" {
		t.Fatalf("expected type to be set to 'function', got %q", typ)
	}
}

func TestNormalizeToolCallIDs_ArgumentsAsMap(t *testing.T) {
	// Ollama returns arguments as a map, not a JSON string
	calls := []map[string]interface{}{
		{
			"type": "function",
			"id":   "call_1",
			"function": map[string]interface{}{
				"name": "read_file",
				"arguments": map[string]interface{}{
					"path": "main.go",
				},
			},
		},
	}

	normalized := NormalizeToolCallIDs(calls)

	fn := normalized[0]["function"].(map[string]interface{})
	args, ok := fn["arguments"].(string)
	if !ok {
		t.Fatalf("expected arguments to be a string, got %T", fn["arguments"])
	}
	if args != `{"path":"main.go"}` {
		t.Fatalf("expected arguments JSON string, got %q", args)
	}
}

func TestNormalizeToolCallIDs_NilArguments(t *testing.T) {
	calls := []map[string]interface{}{
		{
			"type": "function",
			"id":   "call_1",
			"function": map[string]interface{}{
				"name": "read_file",
			},
		},
	}

	normalized := NormalizeToolCallIDs(calls)

	fn := normalized[0]["function"].(map[string]interface{})
	args, ok := fn["arguments"].(string)
	if !ok {
		t.Fatalf("expected arguments to be a string, got %T", fn["arguments"])
	}
	if args != "{}" {
		t.Fatalf("expected empty JSON object, got %q", args)
	}
}
