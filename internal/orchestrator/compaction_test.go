package orchestrator

import (
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/session"
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

func TestAdjustCompactionBoundaryForTools_Backward(t *testing.T) {
	messages := []*session.Message{
		{Role: "assistant", ToolCalls: []map[string]interface{}{{"id": "call-1"}}},
		{Role: "tool", ToolID: "call-1", Content: "result"},
		{Role: "user", Content: "latest instructions"},
	}

	adjusted := adjustCompactionBoundaryForTools(messages, 1)

	if adjusted != 0 {
		t.Fatalf("expected boundary to move backward to keep tool exchange intact, got %d", adjusted)
	}
}

func TestAdjustCompactionBoundaryForTools_Forward(t *testing.T) {
	messages := []*session.Message{
		{Role: "assistant", Content: "older"},
		{Role: "user", Content: "more older"},
		{Role: "assistant", ToolCalls: []map[string]interface{}{{"id": "call-2"}}},
		{Role: "tool", ToolID: "call-2", Content: "tool output"},
		{Role: "user", Content: "recent prompt"},
		{Role: "assistant", Content: "latest reply"},
	}

	adjusted := adjustCompactionBoundaryForTools(messages, 3)

	if adjusted != 4 {
		t.Fatalf("expected boundary to move forward to fully compact tool exchange, got %d", adjusted)
	}
}

func TestAdjustCompactionBoundaryForTools_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		messages    []*session.Message
		prefixCount int
		expected    int
	}{
		{
			"prefix_zero",
			[]*session.Message{
				{Role: "user", Content: "a"},
				{Role: "assistant", Content: "b"},
			},
			0, 0,
		},
		{
			"prefix_equals_len",
			[]*session.Message{
				{Role: "user", Content: "a"},
				{Role: "assistant", Content: "b"},
			},
			2, 2,
		},
		{
			"prefix_exceeds_len",
			[]*session.Message{
				{Role: "user", Content: "a"},
			},
			5, 5,
		},
		{
			"no_tool_calls",
			[]*session.Message{
				{Role: "user", Content: "a"},
				{Role: "assistant", Content: "b"},
				{Role: "user", Content: "c"},
				{Role: "assistant", Content: "d"},
			},
			2, 2,
		},
		{
			"multiple_tool_exchanges",
			[]*session.Message{
				{Role: "assistant", ToolCalls: []map[string]interface{}{{"id": "c1"}}},
				{Role: "tool", ToolID: "c1", Content: "r1"},
				{Role: "assistant", ToolCalls: []map[string]interface{}{{"id": "c2"}}},
				{Role: "tool", ToolID: "c2", Content: "r2"},
				{Role: "user", Content: "prompt"},
				{Role: "assistant", Content: "reply"},
			},
			1, 2,
		},
		{
			"boundary_at_tool_call_start",
			[]*session.Message{
				{Role: "user", Content: "a"},
				{Role: "assistant", ToolCalls: []map[string]interface{}{{"id": "c1"}}},
				{Role: "tool", ToolID: "c1", Content: "r1"},
				{Role: "user", Content: "b"},
			},
			1, 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adjustCompactionBoundaryForTools(tt.messages, tt.prefixCount)
			if got != tt.expected {
				t.Fatalf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestCompactUserPrompts(t *testing.T) {
	tests := []struct {
		name    string
		prompts []string
		check   func(t *testing.T, result string)
	}{
		{
			"empty",
			[]string{},
			func(t *testing.T, result string) {
				if result != "" {
					t.Fatalf("expected empty, got %q", result)
				}
			},
		},
		{
			"single_short",
			[]string{"Hello world"},
			func(t *testing.T, result string) {
				if result != "Hello world" {
					t.Fatalf("expected 'Hello world', got %q", result)
				}
			},
		},
		{
			"single_long",
			[]string{strings.Repeat("x", 500)},
			func(t *testing.T, result string) {
				if !strings.HasSuffix(result, "...") {
					t.Fatalf("expected ellipsis for long content, got %q", result)
				}
				if len(result) > 400 {
					t.Fatalf("expected condensed to 400 chars, got %d", len(result))
				}
			},
		},
		{
			"multiple",
			[]string{"First prompt", "Second prompt"},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "- #1:") || !strings.Contains(result, "- #2:") {
					t.Fatalf("expected numbered list, got %q", result)
				}
			},
		},
		{
			"multiple_three",
			[]string{"A", "B", "C"},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "- #3:") {
					t.Fatalf("expected #3 entry, got %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compactUserPrompts(tt.prompts)
			tt.check(t, result)
		})
	}
}

func TestFindLatestUserPrompt(t *testing.T) {
	tests := []struct {
		name     string
		messages []*session.Message
		expected string
	}{
		{"empty_slice", []*session.Message{}, ""},
		{
			"no_user_messages",
			[]*session.Message{
				{Role: "assistant", Content: "reply"},
				{Role: "system", Content: "sys"},
			},
			"",
		},
		{
			"user_at_end",
			[]*session.Message{
				{Role: "assistant", Content: "reply"},
				{Role: "user", Content: "last"},
			},
			"last",
		},
		{
			"user_in_middle",
			[]*session.Message{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "reply"},
				{Role: "user", Content: "second"},
				{Role: "assistant", Content: "done"},
			},
			"second",
		},
		{
			"only_user",
			[]*session.Message{
				{Role: "user", Content: "only"},
			},
			"only",
		},
		{
			"user_with_whitespace",
			[]*session.Message{
				{Role: "user", Content: "  spaced  "},
			},
			"spaced",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findLatestUserPrompt(tt.messages)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestBuildConversationContent(t *testing.T) {
	tests := []struct {
		name     string
		messages []*session.Message
		check    func(t *testing.T, result string)
	}{
		{
			"empty",
			[]*session.Message{},
			func(t *testing.T, result string) {
				if result != "" {
					t.Fatalf("expected empty, got %q", result)
				}
			},
		},
		{
			"single_user",
			[]*session.Message{
				{Role: "user", Content: "hello"},
			},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "User: hello") {
					t.Fatalf("expected 'User: hello', got %q", result)
				}
				if !strings.Contains(result, "\n---\n") {
					t.Fatalf("expected separator, got %q", result)
				}
			},
		},
		{
			"assistant_with_tool",
			[]*session.Message{
				{Role: "assistant", ToolName: "shell", Content: "ran ls"},
			},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "Assistant (shell): ran ls") {
					t.Fatalf("expected tool label, got %q", result)
				}
			},
		},
		{
			"multiple_messages",
			[]*session.Message{
				{Role: "user", Content: "q"},
				{Role: "assistant", Content: "a"},
			},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "User: q") || !strings.Contains(result, "Assistant: a") {
					t.Fatalf("expected both messages, got %q", result)
				}
			},
		},
		{
			"whitespace_content",
			[]*session.Message{
				{Role: "user", Content: "  spaces  "},
			},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "User: spaces") {
					t.Fatalf("expected trimmed content, got %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildConversationContent(tt.messages)
			tt.check(t, result)
		})
	}
}

func TestFallbackConversationSummary(t *testing.T) {
	tests := []struct {
		name     string
		messages []*session.Message
		check    func(t *testing.T, result string)
	}{
		{
			"empty",
			[]*session.Message{},
			func(t *testing.T, result string) {
				if result != "" {
					t.Fatalf("expected empty, got %q", result)
				}
			},
		},
		{
			"single_message",
			[]*session.Message{
				{Role: "user", Content: "hello"},
			},
			func(t *testing.T, result string) {
				if !strings.HasPrefix(result, "Key points retained:") {
					t.Fatalf("expected 'Key points retained:' prefix, got %q", result)
				}
				if !strings.Contains(result, "- User: hello") {
					t.Fatalf("expected '- User: hello', got %q", result)
				}
			},
		},
		{
			"various_roles",
			[]*session.Message{
				{Role: "user", Content: "q"},
				{Role: "assistant", Content: "a"},
				{Role: "tool", ToolName: "shell", Content: "output"},
			},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "- User:") {
					t.Fatalf("expected User entry, got %q", result)
				}
				if !strings.Contains(result, "- Assistant:") {
					t.Fatalf("expected Assistant entry, got %q", result)
				}
				if !strings.Contains(result, "- Tool (shell):") {
					t.Fatalf("expected Tool (shell) entry, got %q", result)
				}
			},
		},
		{
			"long_content_truncated",
			[]*session.Message{
				{Role: "user", Content: strings.Repeat("x", 300)},
			},
			func(t *testing.T, result string) {
				if !strings.Contains(result, "...") {
					t.Fatalf("expected truncation ellipsis, got %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fallbackConversationSummary(tt.messages)
			tt.check(t, result)
		})
	}
}

func TestBuildUserCompactionSection_EdgeCases(t *testing.T) {
	t.Run("no_user_messages_no_latest", func(t *testing.T) {
		messages := []*session.Message{
			{Role: "assistant", Content: "hi"},
		}
		section := buildUserCompactionSection(messages, []int{10}, 1000, "")
		if section != "" {
			t.Fatalf("expected empty section, got %q", section)
		}
	})

	t.Run("no_user_messages_with_latest", func(t *testing.T) {
		messages := []*session.Message{
			{Role: "assistant", Content: "hi"},
		}
		section := buildUserCompactionSection(messages, []int{10}, 1000, "do stuff")
		if !strings.Contains(section, "do stuff") {
			t.Fatalf("expected latest prompt, got %q", section)
		}
		if !strings.Contains(section, "Continue to implement this.") {
			t.Fatalf("expected continuation directive, got %q", section)
		}
	})

	t.Run("zero_context_window", func(t *testing.T) {
		messages := []*session.Message{
			{Role: "user", Content: "a"},
		}
		section := buildUserCompactionSection(messages, []int{10}, 0, "go")
		if !strings.Contains(section, "condensed summary") {
			t.Fatalf("expected condensed summary for zero context window, got %q", section)
		}
	})

	t.Run("mismatched_token_length", func(t *testing.T) {
		messages := []*session.Message{
			{Role: "user", Content: "a"},
			{Role: "assistant", Content: "b"},
		}
		// perMessageTokens has only 1 element for 2 messages
		section := buildUserCompactionSection(messages, []int{10}, 1000, "x")
		if section == "" {
			t.Fatalf("expected non-empty section even with mismatched tokens")
		}
	})

	t.Run("empty_latest_prompt", func(t *testing.T) {
		messages := []*session.Message{
			{Role: "user", Content: "a"},
		}
		section := buildUserCompactionSection(messages, []int{5}, 10000, "")
		if strings.Contains(section, "Continue to implement this.") {
			t.Fatalf("should not have continuation directive with empty latest prompt, got %q", section)
		}
	})
}
