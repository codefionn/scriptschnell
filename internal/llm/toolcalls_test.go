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
