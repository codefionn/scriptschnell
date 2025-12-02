package orchestrator

import (
	"testing"

	"github.com/statcode-ai/scriptschnell/internal/session"
)

func TestEstimateContextTokensUsesExactEncoding(t *testing.T) {
	messages := []*session.Message{
		{Role: "user", Content: "Hello there"},
		{Role: "assistant", Content: "General Kenobi"},
	}

	total, perMessage, approx := estimateContextTokens("gpt-4", "system", messages)

	if len(perMessage) != len(messages) {
		t.Fatalf("expected per-message counts, got %d", len(perMessage))
	}

	if total <= 0 {
		t.Fatalf("expected positive total tokens")
	}

	if approx {
		t.Fatalf("expected exact encoding for gpt-4")
	}
}

func TestEstimateContextTokensFallback(t *testing.T) {
	messages := []*session.Message{
		{Role: "user", Content: "short"},
	}

	_, _, approx := estimateContextTokens("unknown-model", "", messages)

	if !approx {
		t.Fatalf("expected approximate token counting for unknown model")
	}
}
