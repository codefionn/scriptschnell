package llm

import (
	"fmt"
	"strings"
	"unicode"
)

// NormalizeToolCallIDs ensures every tool call has a stable identifier.
// Some providers occasionally omit call IDs, which breaks downstream requests
// that require tool_call_id on tool messages.
func NormalizeToolCallIDs(toolCalls []map[string]interface{}) []map[string]interface{} {
	for i, tc := range toolCalls {
		if tc == nil {
			continue
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
