package orchestrator

import (
	"strings"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/session"
)

func TestBuildUserCompactionSection_UnifyUnderThreshold(t *testing.T) {
	messages := []*session.Message{
		{Role: "assistant", Content: "Assistant reply"},
		{Role: "user", Content: "First user question"},
		{Role: "user", Content: "Second user note"},
	}

	perMessageTokens := []int{50, 10, 10}

	section := buildUserCompactionSection(messages, perMessageTokens, 2000, "Latest detailed instruction")

	if !strings.Contains(section, "unified verbatim") {
		t.Fatalf("expected unified verbatim section, got: %s", section)
	}

	if !strings.Contains(section, "First user question") || !strings.Contains(section, "Second user note") {
		t.Fatalf("expected user prompts to be preserved verbatim, got: %s", section)
	}

	if !strings.Contains(section, "Continue to implement this.") {
		t.Fatalf("expected continuation directive, got: %s", section)
	}
}

func TestBuildUserCompactionSection_CondenseOverThreshold(t *testing.T) {
	messages := []*session.Message{
		{Role: "user", Content: "Extensive user specification that should be summarized because it is very long and detailed."},
		{Role: "assistant", Content: "Ack"},
		{Role: "user", Content: "Further clarifications that also need compaction."},
	}

	perMessageTokens := []int{800, 20, 600}

	section := buildUserCompactionSection(messages, perMessageTokens, 1000, "Ship it")

	if !strings.Contains(section, "condensed summary") {
		t.Fatalf("expected condensed summary section, got: %s", section)
	}

	if strings.Contains(section, "Ship it\nShip it") {
		t.Fatalf("latest user prompt should appear only once: %s", section)
	}

	if !strings.Contains(section, "Continue to implement this.") {
		t.Fatalf("expected continuation directive, got: %s", section)
	}
}
