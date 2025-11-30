package llm

import "strings"

// normalizeMistralConversation enforces Mistral's requirement that a conversation
// not end with assistant/system turns and that consecutive assistant messages are merged.
func normalizeMistralConversation(messages []*Message) []*Message {
	if len(messages) == 0 {
		return messages
	}

	cloned := make([]*Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		cloned = append(cloned, cloneMessage(msg))
	}

	if len(cloned) == 0 {
		return cloned
	}

	trimmed := trimTrailingAssistantMessages(cloned)
	if len(trimmed) == 0 {
		return trimmed
	}

	merged := make([]*Message, 0, len(trimmed))
	for _, msg := range trimmed {
		if len(merged) == 0 {
			merged = append(merged, msg)
			continue
		}

		prev := merged[len(merged)-1]
		if strings.EqualFold(prev.Role, "assistant") && strings.EqualFold(msg.Role, "assistant") {
			switch {
			case prev.Content != "" && msg.Content != "":
				prev.Content = prev.Content + "\n" + msg.Content
			case prev.Content == "":
				prev.Content = msg.Content
			case msg.Content != "":
				prev.Content += msg.Content
			}

			if len(msg.ToolCalls) > 0 {
				prev.ToolCalls = append(prev.ToolCalls, cloneToolCalls(msg.ToolCalls)...)
			}
			continue
		}

		merged = append(merged, msg)
	}

	return merged
}

func trimTrailingAssistantMessages(messages []*Message) []*Message {
	if len(messages) == 0 {
		return messages
	}

	end := len(messages)
	for end > 0 {
		role := strings.ToLower(strings.TrimSpace(messages[end-1].Role))
		if role == "user" || role == "tool" {
			break
		}
		end--
	}

	if end == 0 || end == len(messages) {
		return messages
	}

	return messages[:end]
}

func cloneMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}

	cloned := &Message{
		Role:     msg.Role,
		Content:  msg.Content,
		ToolID:   msg.ToolID,
		ToolName: msg.ToolName,
	}

	if len(msg.ToolCalls) > 0 {
		cloned.ToolCalls = cloneToolCalls(msg.ToolCalls)
	}

	return cloned
}

func cloneToolCalls(toolCalls []map[string]interface{}) []map[string]interface{} {
	if len(toolCalls) == 0 {
		return nil
	}

	cloned := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if tc == nil {
			continue
		}
		copyMap := make(map[string]interface{}, len(tc))
		for k, v := range tc {
			copyMap[k] = v
		}
		cloned = append(cloned, copyMap)
	}

	return cloned
}
