package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// NormalizeToolCallIDs ensures every tool call has a stable identifier and
// consistent format across providers. Some providers (e.g. Ollama) omit the
// "type" field or return "arguments" as a map instead of a JSON string.
// This function normalizes all tool calls to the canonical format:
//
//	{"id": "...", "type": "function", "function": {"name": "...", "arguments": "<json string>"}}
func NormalizeToolCallIDs(toolCalls []map[string]interface{}) []map[string]interface{} {
	for i, tc := range toolCalls {
		if tc == nil {
			continue
		}

		// Ensure type is set to "function" (some providers like Ollama omit it)
		if t, _ := tc["type"].(string); t == "" {
			tc["type"] = "function"
		}

		// Ensure arguments within function is a JSON string, not a map
		if fn, ok := tc["function"].(map[string]interface{}); ok {
			if _, isString := fn["arguments"].(string); !isString {
				if fn["arguments"] != nil {
					if b, err := json.Marshal(fn["arguments"]); err == nil {
						fn["arguments"] = string(b)
					} else {
						fn["arguments"] = "{}"
					}
				} else {
					fn["arguments"] = "{}"
				}
			}
		}

		id := firstNonEmptyString(tc["id"], tc["call_id"])
		if strings.TrimSpace(id) == "" {
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				if name := sanitizeToolName(fn["name"]); name != "" {
					id = fmt.Sprintf("call_%s_%d", name, i+1)
				}
			}
		}
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("call_%d", i+1)
		}

		tc["id"] = id
		tc["call_id"] = id
	}
	return toolCalls
}

func firstNonEmptyString(values ...interface{}) string {
	for _, v := range values {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func sanitizeToolName(raw interface{}) string {
	name, _ := raw.(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
