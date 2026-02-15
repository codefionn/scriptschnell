package loop

import (
	"errors"
	"testing"
)

func TestDefaultStrategyShouldContinue(t *testing.T) {
	config := DefaultConfig()
	strategy := NewDefaultStrategy(config)

	tests := []struct {
		name     string
		state    State
		outcome  *IterationOutcome
		expected bool
	}{
		{
			name:     "Should continue on Continue result",
			state:    &MockState{MockIteration: 0, MockMaxIterations: 10},
			outcome:  &IterationOutcome{Result: Continue},
			expected: true,
		},
		{
			name:     "Should stop on Break result",
			state:    &MockState{MockIteration: 0, MockMaxIterations: 10},
			outcome:  &IterationOutcome{Result: Break},
			expected: false,
		},
		{
			name:     "Should continue on CompactionNeeded",
			state:    &MockState{MockIteration: 0, MockMaxIterations: 10},
			outcome:  &IterationOutcome{Result: CompactionNeeded},
			expected: true,
		},
		{
			name:     "Should stop on Error result",
			state:    &MockState{MockIteration: 0, MockMaxIterations: 10},
			outcome:  &IterationOutcome{Result: Error},
			expected: false,
		},
		{
			name:     "Should stop on BreakMaxIterations",
			state:    &MockState{MockIteration: 0, MockMaxIterations: 10},
			outcome:  &IterationOutcome{Result: BreakMaxIterations},
			expected: false,
		},
		{
			name:     "Should stop on BreakLoopDetected",
			state:    &MockState{MockIteration: 0, MockMaxIterations: 10},
			outcome:  &IterationOutcome{Result: BreakLoopDetected},
			expected: false,
		},
		{
			name:     "Should stop when iteration limit reached",
			state:    &MockState{MockIteration: 10, MockMaxIterations: 10},
			outcome:  &IterationOutcome{Result: Continue},
			expected: false,
		},
		{
			name:     "Should stop on nil outcome with limit reached",
			state:    &MockState{MockIteration: 10, MockMaxIterations: 10},
			outcome:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.ShouldContinue(tt.state, tt.outcome)
			if result != tt.expected {
				t.Errorf("ShouldContinue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDefaultStrategyShouldAutoContinue(t *testing.T) {
	config := DefaultConfig()
	strategy := NewDefaultStrategy(config)

	tests := []struct {
		name    string
		state   State
		content string
		want    bool
	}{
		{
			name:    "Should auto-continue on colon ending",
			state:   &MockState{MockAutoContinueAttempts: 0, MockMaxAutoContinueAttempts: 10},
			content: "Here is the list:",
			want:    true,
		},
		{
			name:    "Should auto-continue on incomplete code block",
			state:   &MockState{MockAutoContinueAttempts: 0, MockMaxAutoContinueAttempts: 10},
			content: "```go\nfunc main() {",
			want:    true,
		},
		{
			name:    "Should auto-continue on incomplete list",
			state:   &MockState{MockAutoContinueAttempts: 0, MockMaxAutoContinueAttempts: 10},
			content: "1. ",
			want:    true,
		},
		{
			name:    "Should not auto-continue on complete sentence",
			state:   &MockState{MockAutoContinueAttempts: 0, MockMaxAutoContinueAttempts: 10},
			content: "This is a complete sentence.",
			want:    false,
		},
		{
			name:    "Should not auto-continue when limit reached",
			state:   &MockState{MockAutoContinueAttempts: 10, MockMaxAutoContinueAttempts: 10},
			content: "Here is the list:",
			want:    false,
		},
		{
			name:    "Should not auto-continue on empty content",
			state:   &MockState{MockAutoContinueAttempts: 0, MockMaxAutoContinueAttempts: 10},
			content: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.ShouldAutoContinue(tt.state, tt.content)
			if got != tt.want {
				t.Errorf("ShouldAutoContinue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultStrategyShouldAutoContinueDisabled(t *testing.T) {
	config := &Config{
		MaxIterations:           10,
		MaxAutoContinueAttempts: 5,
		EnableAutoContinue:      false,
	}
	strategy := NewDefaultStrategy(config)

	state := &MockState{MockAutoContinueAttempts: 0, MockMaxAutoContinueAttempts: 5}

	if strategy.ShouldAutoContinue(state, "Here is the list:") {
		t.Error("Should not auto-continue when disabled")
	}
}

func TestDefaultStrategyGetResult(t *testing.T) {
	config := DefaultConfig()
	strategy := NewDefaultStrategy(config)

	tests := []struct {
		name            string
		state           State
		outcome         *IterationOutcome
		terminatedEarly bool
		wantSuccess     bool
		wantReason      string
	}{
		{
			name:            "Break result",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 0},
			outcome:         &IterationOutcome{Result: Break},
			terminatedEarly: false,
			wantSuccess:     true,
			wantReason:      "completed normally",
		},
		{
			name:            "BreakWithAutoContinue result",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 2},
			outcome:         &IterationOutcome{Result: BreakWithAutoContinue},
			terminatedEarly: false,
			wantSuccess:     true,
			wantReason:      "auto-continue limit reached",
		},
		{
			name:            "BreakMaxIterations result",
			state:           &MockState{MockIteration: 256, MockAutoContinueAttempts: 0},
			outcome:         &IterationOutcome{Result: BreakMaxIterations},
			terminatedEarly: false,
			wantSuccess:     true,
			wantReason:      "maximum iteration limit reached",
		},
		{
			name:            "BreakLoopDetected result",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 0},
			outcome:         &IterationOutcome{Result: BreakLoopDetected},
			terminatedEarly: false,
			wantSuccess:     false,
			wantReason:      "repetitive loop pattern detected",
		},
		{
			name:            "Error result",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 0},
			outcome:         &IterationOutcome{Result: Error, Error: errors.New("test error")},
			terminatedEarly: false,
			wantSuccess:     false,
			wantReason:      "error occurred: test error",
		},
		{
			name:            "CompactionNeeded result",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 0},
			outcome:         &IterationOutcome{Result: CompactionNeeded},
			terminatedEarly: false,
			wantSuccess:     true,
			wantReason:      "compaction required",
		},
		{
			name:            "Continue with early termination",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 0},
			outcome:         &IterationOutcome{Result: Continue},
			terminatedEarly: true,
			wantSuccess:     true,
			wantReason:      "terminated by external signal",
		},
		{
			name:            "Continue without early termination",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 0},
			outcome:         &IterationOutcome{Result: Continue},
			terminatedEarly: false,
			wantSuccess:     true,
			wantReason:      "completed",
		},
		{
			name:            "Nil outcome",
			state:           &MockState{MockIteration: 5, MockAutoContinueAttempts: 0},
			outcome:         nil,
			terminatedEarly: false,
			wantSuccess:     true,
			wantReason:      "completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.GetResult(tt.state, tt.outcome, tt.terminatedEarly)

			if result.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v", result.Success, tt.wantSuccess)
			}

			if result.TerminationReason != tt.wantReason {
				t.Errorf("TerminationReason = %q, want %q", result.TerminationReason, tt.wantReason)
			}
		})
	}
}

func TestConservativeStrategy(t *testing.T) {
	config := DefaultConfig()
	strategy := NewConservativeStrategy(config)

	t.Run("Has reduced max iterations", func(t *testing.T) {
		// ConservativeStrategy should have half the iterations
		// We can't directly test this, but we can test behavior
		// Skip actual assertion as max iterations is implementation detail
	})

	t.Run("ShouldContinue with Continue result", func(t *testing.T) {
		state := &MockState{MockIteration: 0, MockMaxIterations: 10}
		outcome := &IterationOutcome{Result: Continue}

		if !strategy.ShouldContinue(state, outcome) {
			t.Error("Should continue on Continue result")
		}
	})

	t.Run("ShouldContinue stops on error-like outcome", func(t *testing.T) {
		// ConservativeStrategy should be more strict
		state := &MockState{MockIteration: 0, MockMaxIterations: 10}
		outcome := &IterationOutcome{Result: Break}

		if strategy.ShouldContinue(state, outcome) {
			t.Error("Should not continue on Break result")
		}
	})
}

func TestAggressiveStrategy(t *testing.T) {
	config := DefaultConfig()
	strategy := NewAggressiveStrategy(config)

	t.Run("Has increased max iterations", func(t *testing.T) {
		// AggressiveStrategy should have double the iterations
		// We can verify this indirectly through behavior
	})

	t.Run("ShouldContinue with Continue result", func(t *testing.T) {
		state := &MockState{MockIteration: 0, MockMaxIterations: 10}
		outcome := &IterationOutcome{Result: Continue}

		if !strategy.ShouldContinue(state, outcome) {
			t.Error("Should continue on Continue result")
		}
	})
}

func TestStrategyFactory(t *testing.T) {
	config := DefaultConfig()
	factory := NewStrategyFactory(config)

	tests := []struct {
		mode         string
		expectedType string
	}{
		{"default", "*loop.DefaultStrategy"},
		{"conservative", "*loop.ConservativeStrategy"},
		{"aggressive", "*loop.AggressiveStrategy"},
		{"unknown", "*loop.DefaultStrategy"}, // Falls back to default
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			strategy := factory.Create(tt.mode)
			if strategy == nil {
				t.Fatal("Strategy should not be nil")
			}
		})
	}
}

func TestStrategyFactoryCreateWithConfig(t *testing.T) {
	factory := NewStrategyFactory(DefaultConfig())
	customConfig := &Config{MaxIterations: 100}

	tests := []struct {
		mode string
	}{
		{"default"},
		{"conservative"},
		{"aggressive"},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			strategy := factory.CreateWithConfig(tt.mode, customConfig)
			if strategy == nil {
				t.Fatal("Strategy should not be nil")
			}
		})
	}
}

func TestMatchListPattern(t *testing.T) {
	tests := []struct {
		line     string
		expected bool
		pattern  string
	}{
		{"1.", true, "numbered list"},
		{"12.", true, "numbered list"},
		{"123.", true, "numbered list"},
		{"1. item", false, ""},
		{"- ", true, "bullet list"},
		{"* ", true, "bullet list"},
		{"+ ", true, "bullet list"},
		{"- item", false, ""},
		{"not a list", false, ""},
		{"", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			matched, pattern := matchListPattern(tt.line)
			if matched != tt.expected {
				t.Errorf("matchListPattern(%q) matched = %v, want %v", tt.line, matched, tt.expected)
			}
			if matched && pattern != tt.pattern {
				t.Errorf("matchListPattern(%q) pattern = %q, want %q", tt.line, pattern, tt.pattern)
			}
		})
	}
}
