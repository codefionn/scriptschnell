package orchestrator

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/pkoukk/tiktoken-go"
)

const (
	systemMessageOverhead = 2
	perMessageOverhead    = 4
)

// estimateContextTokens returns the estimated token usage for the session messages
// and whether the calculation is approximate (no exact model encoding available).
func estimateContextTokens(modelID, systemPrompt string, messages []*session.Message) (int, []int, bool) {
	encoder, approx := encodingForModel(modelID)

	total := tokenCount(encoder, systemPrompt)
	if systemPrompt != "" {
		total += systemMessageOverhead
	}

	perMessage := make([]int, len(messages))

	for i, msg := range messages {
		tokens := tokenCount(encoder, msg.Content) + perMessageOverhead

		if msg.ToolID != "" {
			tokens += tokenCount(encoder, msg.ToolID)
		}
		if msg.ToolName != "" {
			tokens += tokenCount(encoder, msg.ToolName)
		}
		if len(msg.ToolCalls) > 0 {
			if data, err := json.Marshal(msg.ToolCalls); err == nil {
				tokens += tokenCount(encoder, string(data))
			}
		}

		perMessage[i] = tokens
		total += tokens
	}

	return total, perMessage, approx
}

func encodingForModel(modelID string) (*tiktoken.Tiktoken, bool) {
	encoder, err := tiktoken.EncodingForModel(modelID)
	if err == nil {
		return encoder, false
	}

	fallback, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, true
	}

	return fallback, true
}

func tokenCount(encoder *tiktoken.Tiktoken, text string) int {
	if text == "" {
		return 0
	}

	if encoder != nil {
		return len(encoder.Encode(text, nil, nil))
	}

	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}

	// Rough heuristic: 1 token ≈ 4 characters
	return (runes + 3) / 4
}
