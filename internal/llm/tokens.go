package llm

// EstimateTokenCount returns a rough token estimate for the provided content.
func EstimateTokenCount(content string) int {
	return charsToTokens(len(content))
}

// EstimateTokenCountForMessage returns the token estimate for a single message's content.
func EstimateTokenCountForMessage(msg *Message) int {
	if msg == nil {
		return 0
	}
	return EstimateTokenCount(msg.Content)
}

func charsToTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	tokens := chars / 4
	if tokens <= 0 {
		tokens = 1
	}
	return tokens
}
