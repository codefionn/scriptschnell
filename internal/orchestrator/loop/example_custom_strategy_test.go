// Package loop provides examples for creating custom loop strategies.
//
// This file demonstrates how to implement a custom strategy for specialized
// use cases. The example shows a "BudgetAwareStrategy" that stops the loop
// based on token usage budget rather than just iteration count.
package loop

import (
	"fmt"
)

// BudgetAwareStrategy is a custom strategy that stops based on token budget.
// This is useful when you want to limit the total token usage rather than
// just the number of iterations.
type BudgetAwareStrategy struct {
	DefaultStrategy
	maxTokens     int
	tokensUsed    int
	tokenCallback func(tokens int) // Called to report token usage
}

// NewBudgetAwareStrategy creates a new BudgetAwareStrategy.
//
// Example:
//
//	strategy := NewBudgetAwareStrategy(
//	    loop.DefaultConfig(),
//	    100000, // 100k token budget
//	    func(tokens int) {
//	        fmt.Printf("Tokens used: %d\n", tokens)
//	    },
//	)
func NewBudgetAwareStrategy(config *Config, maxTokens int, tokenCallback func(tokens int)) *BudgetAwareStrategy {
	if config == nil {
		config = DefaultConfig()
	}
	return &BudgetAwareStrategy{
		DefaultStrategy: *NewDefaultStrategy(config),
		maxTokens:       maxTokens,
		tokenCallback:   tokenCallback,
	}
}

// ShouldContinue overrides the default to check token budget.
func (s *BudgetAwareStrategy) ShouldContinue(state State, outcome *IterationOutcome) bool {
	// First check the default logic
	if !s.DefaultStrategy.ShouldContinue(state, outcome) {
		return false
	}

	// Check token budget
	if s.tokensUsed >= s.maxTokens {
		return false
	}

	return true
}

// GetResult returns the final result with budget information.
func (s *BudgetAwareStrategy) GetResult(state State, lastOutcome *IterationOutcome, terminatedEarly bool) *Result {
	result := s.DefaultStrategy.GetResult(state, lastOutcome, terminatedEarly)

	// Add budget information to metadata
	if result.Metadata == nil {
		result.Metadata = make(map[string]interface{})
	}
	result.Metadata["tokens_used"] = s.tokensUsed
	result.Metadata["tokens_budget"] = s.maxTokens
	result.Metadata["tokens_remaining"] = s.maxTokens - s.tokensUsed

	// Override termination reason if budget was exceeded
	if s.tokensUsed >= s.maxTokens {
		result.TerminationReason = "token budget exceeded"
	}

	return result
}

// RecordTokenUsage records token usage for an iteration.
func (s *BudgetAwareStrategy) RecordTokenUsage(tokens int) {
	s.tokensUsed += tokens
	if s.tokenCallback != nil {
		s.tokenCallback(s.tokensUsed)
	}
}

// Example demonstrates how to use the BudgetAwareStrategy.
func ExampleBudgetAwareStrategy() {
	// Create a budget-aware strategy with a 10,000 token limit
	strategy := NewBudgetAwareStrategy(
		DefaultConfig(),
		10000,
		func(tokens int) {
			fmt.Printf("Progress: %d tokens used\n", tokens)
		},
	)

	// Create dependencies (simplified for example)
	config := DefaultConfig()
	state := NewDefaultState(config)

	// Simulate some iterations
	for i := 0; i < 3; i++ {
		state.Increment()

		// Simulate token usage for this iteration
		strategy.RecordTokenUsage(500)

		// Create a mock outcome
		outcome := &IterationOutcome{
			Result: Continue,
		}

		// Check if we should continue
		if !strategy.ShouldContinue(state, outcome) {
			fmt.Println("Stopping: budget or limit reached")
			break
		}
	}

	// Get final result
	result := strategy.GetResult(state, &IterationOutcome{Result: Break}, false)
	fmt.Printf("Iterations: %d\n", result.IterationsExecuted)
	fmt.Printf("Tokens used: %v\n", result.Metadata["tokens_used"])
	fmt.Printf("Reason: %s\n", result.TerminationReason)
}

// LoggingStrategy is another example that logs all decisions.
// This can be useful for debugging or auditing loop behavior.
type LoggingStrategy struct {
	DefaultStrategy
	logger func(message string)
}

// NewLoggingStrategy creates a strategy that logs all decisions.
func NewLoggingStrategy(config *Config, logger func(message string)) *LoggingStrategy {
	if config == nil {
		config = DefaultConfig()
	}
	if logger == nil {
		logger = func(msg string) { fmt.Println(msg) }
	}
	return &LoggingStrategy{
		DefaultStrategy: *NewDefaultStrategy(config),
		logger:          logger,
	}
}

// ShouldContinue logs the decision and delegates to default.
func (s *LoggingStrategy) ShouldContinue(state State, outcome *IterationOutcome) bool {
	shouldContinue := s.DefaultStrategy.ShouldContinue(state, outcome)
	s.logger(fmt.Sprintf(
		"[Loop Decision] iteration=%d, result=%s, should_continue=%v",
		state.Iteration(),
		outcome.Result,
		shouldContinue,
	))
	return shouldContinue
}

// ShouldAutoContinue logs the decision and delegates to default.
func (s *LoggingStrategy) ShouldAutoContinue(state State, content string) bool {
	shouldAutoContinue := s.DefaultStrategy.ShouldAutoContinue(state, content)
	s.logger(fmt.Sprintf(
		"[Auto-Continue Decision] auto_continue_attempts=%d, should_auto_continue=%v",
		state.AutoContinueAttempts(),
		shouldAutoContinue,
	))
	return shouldAutoContinue
}

// ExampleLoggingStrategy demonstrates the logging strategy.
func ExampleLoggingStrategy() {
	// Create a logging strategy
	strategy := NewLoggingStrategy(
		DefaultConfig(),
		func(msg string) { fmt.Println(msg) },
	)

	// Simulate loop execution
	config := DefaultConfig()
	state := NewDefaultState(config)

	for i := 0; i < 2; i++ {
		state.Increment()
		outcome := &IterationOutcome{Result: Continue}
		strategy.ShouldContinue(state, outcome)
	}
}

// CompositeStrategy combines multiple strategies.
// This demonstrates the decorator pattern for strategies.
type CompositeStrategy struct {
	strategies []Strategy
}

// NewCompositeStrategy creates a strategy that combines multiple strategies.
// All strategies must agree to continue for the loop to continue.
func NewCompositeStrategy(strategies ...Strategy) *CompositeStrategy {
	return &CompositeStrategy{strategies: strategies}
}

// ShouldContinue returns true only if all strategies agree.
func (c *CompositeStrategy) ShouldContinue(state State, outcome *IterationOutcome) bool {
	for _, s := range c.strategies {
		if !s.ShouldContinue(state, outcome) {
			return false
		}
	}
	return true
}

// ShouldAutoContinue returns true if any strategy suggests auto-continue.
func (c *CompositeStrategy) ShouldAutoContinue(state State, content string) bool {
	for _, s := range c.strategies {
		if s.ShouldAutoContinue(state, content) {
			return true
		}
	}
	return false
}

// GetResult returns the result from the first strategy.
func (c *CompositeStrategy) GetResult(state State, lastOutcome *IterationOutcome, terminatedEarly bool) *Result {
	if len(c.strategies) > 0 {
		return c.strategies[0].GetResult(state, lastOutcome, terminatedEarly)
	}
	return &Result{
		Success:           true,
		TerminationReason: "no strategies configured",
	}
}

// ExampleCompositeStrategy demonstrates combining strategies.
func ExampleCompositeStrategy() {
	// Create a conservative strategy with a budget limit
	conservative := NewConservativeStrategy(DefaultConfig())
	budget := NewBudgetAwareStrategy(DefaultConfig(), 5000, nil)

	// Combine them - both must agree to continue
	composite := NewCompositeStrategy(conservative, budget)

	config := DefaultConfig()
	state := NewDefaultState(config)

	// This will stop when either strategy says to stop
	outcome := &IterationOutcome{Result: Continue}
	shouldContinue := composite.ShouldContinue(state, outcome)
	fmt.Printf("Should continue: %v\n", shouldContinue)
}
