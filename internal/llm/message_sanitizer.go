package llm

import (
	"strings"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// SanitizeMessages validates and repairs a message array for OpenAI-compatible APIs.
// It fixes structural issues that can arise after context compaction races, such as:
//   - Orphan tool responses (no matching assistant tool call)
//   - Assistant messages with tool_calls but missing subsequent tool responses
//   - Messages starting with a non-user/non-system role
//   - Consecutive messages with the same role (merged where appropriate)
//
// The function returns a repaired copy and whether any repairs were made.
func SanitizeMessages(messages []*Message) ([]*Message, bool) {
	if len(messages) == 0 {
		return messages, false
	}

	repaired := false

	// Build a working copy of message pointers
	msgs := make([]*Message, len(messages))
	copy(msgs, messages)

	// Pass 1: Remove orphan tool responses (tool messages whose tool_call_id
	// doesn't match any preceding assistant tool call)
	msgs, changed := removeOrphanToolMessages(msgs)
	if changed {
		repaired = true
	}

	// Pass 2: Fix assistant messages with tool_calls that are missing their tool responses
	msgs, changed = fixMissingToolResponses(msgs)
	if changed {
		repaired = true
	}

	// Pass 3: Ensure first non-system message is a user message
	msgs, changed = ensureFirstUserMessage(msgs)
	if changed {
		repaired = true
	}

	// Pass 4: Merge consecutive user messages
	msgs, changed = mergeConsecutiveUserMessages(msgs)
	if changed {
		repaired = true
	}

	if repaired {
		logger.Info("message_sanitizer: repaired message array (%d -> %d messages)", len(messages), len(msgs))
	}

	return msgs, repaired
}

// removeOrphanToolMessages removes tool-role messages whose tool_call_id
// doesn't match any preceding assistant message's tool calls.
func removeOrphanToolMessages(msgs []*Message) ([]*Message, bool) {
	changed := false

	// Find the first non-system index
	firstNonSystem := 0
	for firstNonSystem < len(msgs) && msgs[firstNonSystem].Role == "system" {
		firstNonSystem++
	}

	// Remove tool messages that appear before we see an assistant message with tool_calls
	result := make([]*Message, 0, len(msgs))
	result = append(result, msgs[:firstNonSystem]...)

	for i := firstNonSystem; i < len(msgs); i++ {
		if msgs[i].Role == "tool" {
			// Check if there's a preceding assistant message with a matching tool call
			hasMatch := false
			for j := i - 1; j >= 0; j-- {
				if msgs[j].Role == "assistant" && len(msgs[j].ToolCalls) > 0 {
					if toolCallContainsID(msgs[j].ToolCalls, msgs[i].ToolID) {
						hasMatch = true
					}
					break
				}
			}
			if !hasMatch {
				logger.Debug("message_sanitizer: removing orphan tool message at position %d (tool_id=%s)", i, msgs[i].ToolID)
				changed = true
				continue
			}
		}
		result = append(result, msgs[i])
	}

	return result, changed
}

// fixMissingToolResponses handles assistant messages with tool_calls that don't have
// corresponding tool responses following them. It strips the tool_calls from such messages.
func fixMissingToolResponses(msgs []*Message) ([]*Message, bool) {
	changed := false

	for i, msg := range msgs {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		// Check if all tool calls have matching responses after this message
		expectedIDs := make(map[string]bool)
		for _, tc := range msg.ToolCalls {
			if id, ok := tc["id"].(string); ok && id != "" {
				expectedIDs[id] = true
			}
		}

		// Look forward for tool responses
		for j := i + 1; j < len(msgs); j++ {
			if msgs[j].Role == "tool" && msgs[j].ToolID != "" {
				delete(expectedIDs, msgs[j].ToolID)
			}
			// Stop scanning when we hit the next assistant or user message
			if msgs[j].Role == "assistant" || msgs[j].Role == "user" {
				break
			}
		}

		// If there are missing tool responses, strip tool_calls from the assistant message
		if len(expectedIDs) > 0 {
			logger.Debug("message_sanitizer: stripping %d unmatched tool_calls from assistant message at position %d", len(expectedIDs), i)
			// If ALL tool calls are missing responses, remove them all
			if len(expectedIDs) == len(msg.ToolCalls) {
				msgs[i] = &Message{
					Role:      msg.Role,
					Content:   msg.Content,
					Reasoning: msg.Reasoning,
				}
			} else {
				// Keep only the tool calls that have responses
				remaining := make([]map[string]any, 0)
				for _, tc := range msg.ToolCalls {
					if id, ok := tc["id"].(string); ok && !expectedIDs[id] {
						remaining = append(remaining, tc)
					}
				}
				msgs[i] = &Message{
					Role:      msg.Role,
					Content:   msg.Content,
					Reasoning: msg.Reasoning,
					ToolCalls: remaining,
				}
			}
			changed = true
		}
	}

	return msgs, changed
}

// ensureFirstUserMessage ensures the first non-system message is a user message.
// If the first message after system messages is assistant/tool, insert a placeholder user message.
func ensureFirstUserMessage(msgs []*Message) ([]*Message, bool) {
	firstNonSystem := 0
	for firstNonSystem < len(msgs) && msgs[firstNonSystem].Role == "system" {
		firstNonSystem++
	}

	if firstNonSystem >= len(msgs) {
		return msgs, false
	}

	if msgs[firstNonSystem].Role == "user" {
		return msgs, false
	}

	logger.Debug("message_sanitizer: inserting user message before %s message at position %d", msgs[firstNonSystem].Role, firstNonSystem)

	result := make([]*Message, 0, len(msgs)+1)
	result = append(result, msgs[:firstNonSystem]...)
	result = append(result, &Message{
		Role:    "user",
		Content: "Continue.",
	})
	result = append(result, msgs[firstNonSystem:]...)

	return result, true
}

// mergeConsecutiveUserMessages merges consecutive user messages into one.
func mergeConsecutiveUserMessages(msgs []*Message) ([]*Message, bool) {
	if len(msgs) < 2 {
		return msgs, false
	}

	changed := false
	result := make([]*Message, 0, len(msgs))
	result = append(result, msgs[0])

	for i := 1; i < len(msgs); i++ {
		prev := result[len(result)-1]
		cur := msgs[i]

		if prev.Role == "user" && cur.Role == "user" {
			// Merge into previous
			merged := &Message{
				Role:    "user",
				Content: strings.TrimSpace(prev.Content + "\n\n" + cur.Content),
			}
			result[len(result)-1] = merged
			changed = true
			logger.Debug("message_sanitizer: merged consecutive user messages at position %d", i)
		} else {
			result = append(result, cur)
		}
	}

	return result, changed
}

// toolCallContainsID checks if any tool call in the list has the given ID.
func toolCallContainsID(toolCalls []map[string]any, toolID string) bool {
	if toolID == "" {
		return false
	}
	for _, tc := range toolCalls {
		if id, ok := tc["id"].(string); ok && id == toolID {
			return true
		}
	}
	return false
}
