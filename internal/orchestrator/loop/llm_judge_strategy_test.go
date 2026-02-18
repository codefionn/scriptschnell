package loop

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
)

func TestLLMJudgeStrategy(t *testing.T) {
	config := DefaultConfig()
	config.EnableLLMAutoContinueJudge = true

	t.Run("Fallback to pattern matching when LLM client is nil", func(t *testing.T) {
		strategy := NewLLMJudgeStrategy(config, nil, "", nil)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		// Test with incomplete content (should trigger pattern matching)
		shouldContinue := strategy.ShouldAutoContinue(state, "Here is a list of items:")
		if !shouldContinue {
			t.Error("Expected true for incomplete content ending with colon")
		}
	})

	t.Run("Fallback to pattern matching when modelID is empty", func(t *testing.T) {
		strategy := NewLLMJudgeStrategy(config, nil, "", nil)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, "")
		if shouldContinue {
			t.Error("Expected false for empty content")
		}
	})

	t.Run("Respects auto-continue limit", func(t *testing.T) {
		strategy := NewLLMJudgeStrategy(config, nil, "", nil)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    5,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, "")
		if shouldContinue {
			t.Error("Expected false when at auto-continue limit")
		}
	})

	t.Run("Respects EnableAutoContinue flag", func(t *testing.T) {
		config := DefaultConfig()
		config.EnableLLMAutoContinueJudge = true
		config.EnableAutoContinue = false

		strategy := NewLLMJudgeStrategy(config, nil, "", nil)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, "")
		if shouldContinue {
			t.Error("Expected false when auto-continue is disabled")
		}
	})
}

func TestLLMJudgeStrategyFactory(t *testing.T) {
	config := DefaultConfig()
	config.EnableLLMAutoContinueJudge = true
	factory := NewStrategyFactory(config)

	t.Run("Create llm-judge strategy with LLM client", func(t *testing.T) {
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
			},
		}
		strategy := factory.CreateWithLLMJudge("llm-judge", config, nil, "model-id", session)
		if strategy == nil {
			t.Fatal("Strategy should not be nil")
		}

		// Verify it's an LLMJudgeStrategy by checking embedded DefaultStrategy behavior
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		// Since LLM client is nil, it should fall back to pattern matching
		shouldContinue := strategy.ShouldAutoContinue(state, "")
		// Empty content should not continue
		if shouldContinue {
			t.Error("Expected false for empty content")
		}
	})

	t.Run("Create llm-judge strategy without LLM client falls back to default", func(t *testing.T) {
		strategy := factory.Create("llm-judge")
		if strategy == nil {
			t.Fatal("Strategy should not be nil")
		}

		// Should be a DefaultStrategy (fallback)
		_, isLLMJudge := strategy.(*LLMJudgeStrategy)
		if isLLMJudge {
			t.Error("Expected DefaultStrategy fallback when LLM client not provided")
		}
	})
}

// TestLLMJudgeStrategyWithMockClient tests the strategy with a mock LLM client
func TestLLMJudgeStrategyWithMockClient(t *testing.T) {
	config := DefaultConfig()
	config.EnableLLMAutoContinueJudge = true

	t.Run("CONTINUE response from LLM", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "CONTINUE",
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "Tell me about Go"},
				&SimpleMessage{Role: "assistant", Content: "Go is a programming language"},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "gpt-4", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, "Go is a programming language")
		// Should continue because LLM said CONTINUE
		if !shouldContinue {
			t.Error("Expected true when LLM returns CONTINUE")
		}
	})

	t.Run("STOP response from LLM still checks pattern matching", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "STOP",
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "List items"},
				&SimpleMessage{Role: "assistant", Content: "1. "},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "gpt-4", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		// Should still continue because pattern matching finds incomplete list
		shouldContinue := strategy.ShouldAutoContinue(state, "1. ")
		if !shouldContinue {
			t.Error("Expected true for incomplete list pattern even when LLM says STOP")
		}
	})

	t.Run("Qwen 3 conservative mode - only pristine CONTINUE", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "CONTINUE",
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
				&SimpleMessage{Role: "assistant", Content: "incomplete"},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "qwen-3-32b", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, "incomplete")
		if !shouldContinue {
			t.Error("Expected true for pristine CONTINUE from Qwen 3")
		}
	})

	t.Run("Qwen 3 conservative mode - rejects non-pristine CONTINUE", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "I think CONTINUE",
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
				&SimpleMessage{Role: "assistant", Content: "incomplete"},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "qwen-3-32b", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, "incomplete")
		if shouldContinue {
			t.Error("Expected false for non-pristine CONTINUE from Qwen 3 (should fall back to pattern matching)")
		}
	})

	t.Run("Mistral ultra-conservative mode - only pristine CONTINUE", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "CONTINUE",
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
				&SimpleMessage{Role: "assistant", Content: "incomplete"},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "mistral-large", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, "incomplete")
		if !shouldContinue {
			t.Error("Expected true for pristine CONTINUE from Mistral")
		}
	})
}

// TestLoopDetection tests the loop detection functionality
func TestLoopDetection(t *testing.T) {
	config := DefaultConfig()
	config.EnableLLMAutoContinueJudge = true

	t.Run("Detects repetitive patterns", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "CONTINUE",
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
				&SimpleMessage{Role: "assistant", Content: "Repeated text"},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "gpt-4", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		// Should NOT continue because loop was detected
		shouldContinue := strategy.ShouldAutoContinue(state, "Repeated text")
		if shouldContinue {
			t.Error("Expected false when loop is detected")
		}
	})
}

// TestColonNewlinePattern tests the colon-newline manual check
func TestColonNewlinePattern(t *testing.T) {
	config := DefaultConfig()
	config.EnableLLMAutoContinueJudge = true

	t.Run("Triggers on colon followed by newlines", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "STOP", // Won't matter, manual check should trigger first
			},
		}
		colonContent := "Here are the items:\n\n"
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
				&SimpleMessage{Role: "assistant", Content: colonContent},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "gpt-4", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		shouldContinue := strategy.ShouldAutoContinue(state, colonContent)
		if !shouldContinue {
			t.Error("Expected true for content ending with colon and newlines")
		}
	})
}

// TestEmptyLLMResponse tests handling of empty LLM responses
func TestEmptyLLMResponse(t *testing.T) {
	config := DefaultConfig()
	config.EnableLLMAutoContinueJudge = true

	t.Run("Empty response falls back to pattern matching", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "", // Empty response
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
				&SimpleMessage{Role: "assistant", Content: "Here's a list:"},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "gpt-4", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		// Should still continue because pattern matching finds colon
		shouldContinue := strategy.ShouldAutoContinue(state, "Here's a list:")
		if !shouldContinue {
			t.Error("Expected true when pattern matching finds incomplete content even with empty LLM response")
		}
	})
}

// TestInvalidLLMResponse tests handling of invalid LLM responses
func TestInvalidLLMResponse(t *testing.T) {
	config := DefaultConfig()
	config.EnableLLMAutoContinueJudge = true

	t.Run("Invalid response falls back to pattern matching", func(t *testing.T) {
		mockClient := &MockLLMClient{
			MockResponse: &llm.CompletionResponse{
				Content: "maybe later", // Not CONTINUE or STOP
			},
		}
		session := &MockSession{
			Messages: []Message{
				&SimpleMessage{Role: "user", Content: "test"},
				&SimpleMessage{Role: "assistant", Content: "Here's a list:"},
			},
		}
		strategy := NewLLMJudgeStrategy(config, mockClient, "gpt-4", session)
		state := &MockState{
			MockIteration:               0,
			MockMaxIterations:           10,
			MockAutoContinueAttempts:    0,
			MockMaxAutoContinueAttempts: 5,
		}

		// Should still continue because pattern matching finds colon
		shouldContinue := strategy.ShouldAutoContinue(state, "Here's a list:")
		if !shouldContinue {
			t.Error("Expected true when pattern matching finds incomplete content even with invalid LLM response")
		}
	})
}
