package loop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/progress"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxIterations != 256 {
		t.Errorf("Expected MaxIterations = 256, got %d", config.MaxIterations)
	}
	if config.MaxAutoContinueAttempts != 10 {
		t.Errorf("Expected MaxAutoContinueAttempts = 10, got %d", config.MaxAutoContinueAttempts)
	}
	if !config.EnableLoopDetection {
		t.Error("Expected EnableLoopDetection = true")
	}
	if !config.EnableAutoContinue {
		t.Error("Expected EnableAutoContinue = true")
	}
	if config.ContextCompactionThresholdPercent != 90 {
		t.Errorf("Expected ContextCompactionThresholdPercent = 90, got %d", config.ContextCompactionThresholdPercent)
	}
	if config.MaxConsecutiveCompactions != 2 {
		t.Errorf("Expected MaxConsecutiveCompactions = 2, got %d", config.MaxConsecutiveCompactions)
	}
}

func TestIterationResultString(t *testing.T) {
	tests := []struct {
		result   IterationResult
		expected string
	}{
		{Continue, "continue"},
		{Break, "break"},
		{BreakWithAutoContinue, "break_with_auto_continue"},
		{BreakMaxIterations, "break_max_iterations"},
		{BreakLoopDetected, "break_loop_detected"},
		{Error, "error"},
		{CompactionNeeded, "compaction_needed"},
		{IterationResult(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.result.String()
			if got != tt.expected {
				t.Errorf("IterationResult.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDefaultState(t *testing.T) {
	config := DefaultConfig()
	state := NewDefaultState(config)

	t.Run("Iteration", func(t *testing.T) {
		if state.Iteration() != 0 {
			t.Errorf("Initial iteration should be 0, got %d", state.Iteration())
		}

		if state.Increment() != 1 {
			t.Error("Increment should return 1")
		}

		if state.Iteration() != 1 {
			t.Errorf("Iteration should be 1, got %d", state.Iteration())
		}
	})

	t.Run("MaxIterations", func(t *testing.T) {
		if state.MaxIterations() != config.MaxIterations {
			t.Errorf("MaxIterations should be %d, got %d", config.MaxIterations, state.MaxIterations())
		}
	})

	t.Run("HasReachedLimit", func(t *testing.T) {
		// Reset state for this test
		state = NewDefaultState(config)
		state.iteration = config.MaxIterations - 1

		if state.HasReachedLimit() {
			t.Error("Should not have reached limit yet")
		}

		state.Increment()
		if !state.HasReachedLimit() {
			t.Error("Should have reached limit")
		}
	})

	t.Run("AutoContinue", func(t *testing.T) {
		state = NewDefaultState(config)

		if state.AutoContinueAttempts() != 0 {
			t.Error("Initial auto-continue attempts should be 0")
		}

		state.IncrementAutoContinue()
		if state.AutoContinueAttempts() != 1 {
			t.Errorf("Auto-continue attempts should be 1, got %d", state.AutoContinueAttempts())
		}

		state.ResetAutoContinue()
		if state.AutoContinueAttempts() != 0 {
			t.Error("Auto-continue attempts should be reset to 0")
		}
	})

	t.Run("HasReachedAutoContinueLimit", func(t *testing.T) {
		state = NewDefaultState(config)
		state.autoContinueAttempts = config.MaxAutoContinueAttempts - 1

		if state.HasReachedAutoContinueLimit() {
			t.Error("Should not have reached auto-continue limit yet")
		}

		state.IncrementAutoContinue()
		if !state.HasReachedAutoContinueLimit() {
			t.Error("Should have reached auto-continue limit")
		}
	})

	t.Run("Compaction", func(t *testing.T) {
		state = NewDefaultState(config)

		if state.CompactionAttempts() != 0 {
			t.Error("Initial compaction attempts should be 0")
		}

		state.IncrementCompactionAttempts()
		if state.CompactionAttempts() != 1 {
			t.Errorf("Compaction attempts should be 1, got %d", state.CompactionAttempts())
		}

		state.ResetCompactionAttempts()
		if state.CompactionAttempts() != 0 {
			t.Error("Compaction attempts should be reset to 0")
		}
	})

	t.Run("ShouldAllowCompaction", func(t *testing.T) {
		state = NewDefaultState(config)

		if !state.ShouldAllowCompaction() {
			t.Error("Should allow compaction initially")
		}

		state.RecordCompaction()
		if !state.ShouldAllowCompaction() {
			t.Error("Should still allow compaction after first")
		}

		state.RecordCompaction()
		if state.ShouldAllowCompaction() {
			t.Error("Should not allow compaction after two consecutive")
		}

		// Wait for more than 30 seconds (reset consecutive by setting time far in past)
		state.mu.Lock()
		state.lastCompactionTime = time.Now().Add(-31 * time.Second)
		state.mu.Unlock()
		// After time passes, consecutive compactions is reset but first compaction still counts
		// So with 2 consecutive but 31s ago, it should reset to 1 after next check
		_ = state.LastCompactionTime()
	})

	t.Run("LastCompactionTime", func(t *testing.T) {
		state = NewDefaultState(config)

		if !state.LastCompactionTime().IsZero() {
			t.Error("Initial last compaction time should be zero")
		}

		state.RecordCompaction()
		if state.LastCompactionTime().IsZero() {
			t.Error("Last compaction time should not be zero after recording")
		}
	})
}

func TestDefaultStateLoopDetection(t *testing.T) {
	config := DefaultConfig()
	state := NewDefaultState(config)

	// Add some text without loop
	isLoop, _, _ := state.RecordLoopDetection("This is some text")
	if isLoop {
		t.Error("Should not detect loop with single text")
	}

	// Reset and test with repetitive text
	state.ResetLoopDetection()

	// Add repetitive text
	for i := 0; i < 5; i++ {
		isLoop, pattern, count := state.RecordLoopDetection("repetitive pattern")
		if isLoop && i < 3 {
			t.Errorf("Should not detect loop yet at iteration %d", i)
		}
		_ = pattern
		_ = count
	}
}

func TestDefaultStateSetMaxAutoContinueAttempts(t *testing.T) {
	config := DefaultConfig()
	state := NewDefaultState(config)

	state.SetMaxAutoContinueAttempts(5)

	if state.MaxAutoContinueAttempts() != 5 {
		t.Errorf("Expected MaxAutoContinueAttempts = 5, got %d", state.MaxAutoContinueAttempts())
	}
}

func TestMockState(t *testing.T) {
	state := &MockState{
		MockIteration:               0,
		MockMaxIterations:           10,
		MockAutoContinueAttempts:    0,
		MockMaxAutoContinueAttempts: 5,
	}

	t.Run("Iteration", func(t *testing.T) {
		if state.Iteration() != 0 {
			t.Error("Initial iteration should be 0")
		}

		state.Increment()
		if state.Iteration() != 1 {
			t.Error("Iteration should be 1")
		}
	})

	t.Run("HasReachedLimit", func(t *testing.T) {
		state.MockIteration = 9
		state.MockMaxIterations = 10

		if state.HasReachedLimit() {
			t.Error("Should not have reached limit at 9/10")
		}

		state.MockIteration = 10
		if !state.HasReachedLimit() {
			t.Error("Should have reached limit at 10/10")
		}
	})

	t.Run("AutoContinue", func(t *testing.T) {
		state.MockAutoContinueAttempts = 0
		state.MockMaxAutoContinueAttempts = 3

		state.IncrementAutoContinue()
		if state.AutoContinueAttempts() != 1 {
			t.Error("Auto-continue should be 1")
		}

		state.ResetAutoContinue()
		if state.AutoContinueAttempts() != 0 {
			t.Error("Auto-continue should be reset to 0")
		}
	})
}

func TestMockStateCallbacks(t *testing.T) {
	var incrementCalled bool
	var autoContinueCalled bool
	var loopDetectionCalled bool
	var compactionCalled bool

	state := &MockState{
		OnIncrement: func() {
			incrementCalled = true
		},
		OnIncrementAutoContinue: func() {
			autoContinueCalled = true
		},
		OnRecordLoopDetection: func(text string) {
			loopDetectionCalled = true
		},
		OnRecordCompaction: func() {
			compactionCalled = true
		},
	}

	state.Increment()
	if !incrementCalled {
		t.Error("OnIncrement callback should be called")
	}

	state.IncrementAutoContinue()
	if !autoContinueCalled {
		t.Error("OnIncrementAutoContinue callback should be called")
	}

	state.RecordLoopDetection("test")
	if !loopDetectionCalled {
		t.Error("OnRecordLoopDetection callback should be called")
	}

	state.RecordCompaction()
	if !compactionCalled {
		t.Error("OnRecordCompaction callback should be called")
	}
}

func TestOrchestratorLoopBuilder(t *testing.T) {
	t.Run("BuildWithDefaults", func(t *testing.T) {
		iteration := &MockIteration{}
		deps := &Dependencies{}

		loop, err := NewBuilder().
			WithIteration(iteration).
			WithDependencies(deps).
			Build()

		if err != nil {
			t.Fatalf("Build should not error: %v", err)
		}

		if loop == nil {
			t.Fatal("Loop should not be nil")
		}

		if loop.config == nil {
			t.Error("Config should be set")
		}

		if loop.strategy == nil {
			t.Error("Strategy should be set")
		}
	})

	t.Run("BuildWithoutIteration", func(t *testing.T) {
		_, err := NewBuilder().Build()

		if err == nil {
			t.Error("Build should error without iteration")
		}

		if err.Error() != "iteration executor is required" {
			t.Errorf("Unexpected error message: %v", err)
		}
	})

	t.Run("BuildWithCustomConfig", func(t *testing.T) {
		config := &Config{
			MaxIterations: 100,
		}

		loop, _ := NewBuilder().
			WithConfig(config).
			WithIteration(&MockIteration{}).
			Build()

		if loop.config.MaxIterations != 100 {
			t.Errorf("Expected MaxIterations = 100, got %d", loop.config.MaxIterations)
		}
	})

	t.Run("MustBuildPanics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustBuild should panic on error")
			}
		}()

		NewBuilder().MustBuild()
	})
}

func TestNoOpLoop(t *testing.T) {
	t.Run("DefaultResult", func(t *testing.T) {
		loop := &NoOpLoop{}
		result, err := loop.Run(context.Background(), nil, nil)

		if err != nil {
			t.Fatalf("Should not error: %v", err)
		}

		if !result.Success {
			t.Error("Result should be successful")
		}

		if result.TerminationReason != "no-op" {
			t.Errorf("Expected termination reason 'no-op', got %q", result.TerminationReason)
		}
	})

	t.Run("MockResult", func(t *testing.T) {
		mockResult := &Result{
			Success:           false,
			TerminationReason: "test",
		}

		loop := &NoOpLoop{MockResult: mockResult}
		result, _ := loop.Run(context.Background(), nil, nil)

		if result != mockResult {
			t.Error("Should return mock result")
		}
	})

	t.Run("RunIteration", func(t *testing.T) {
		loop := &NoOpLoop{}
		outcome, _ := loop.RunIteration(context.Background(), nil)

		if outcome.Result != Continue {
			t.Errorf("Expected Continue, got %v", outcome.Result)
		}
	})

	t.Run("GetState", func(t *testing.T) {
		loop := &NoOpLoop{}
		state := loop.GetState()

		if state == nil {
			t.Error("GetState should not return nil")
		}
	})
}

func TestLoopFactory(t *testing.T) {
	deps := &Dependencies{}
	config := DefaultConfig()
	factory := NewLoopFactory(deps, config)

	t.Run("CreateDefault", func(t *testing.T) {
		loop, err := factory.CreateDefault()
		if err != nil {
			t.Fatalf("Should not error: %v", err)
		}
		if loop == nil {
			t.Error("Loop should not be nil")
		}
	})

	t.Run("CreateConservative", func(t *testing.T) {
		loop, err := factory.CreateConservative()
		if err != nil {
			t.Fatalf("Should not error: %v", err)
		}
		if loop == nil {
			t.Error("Loop should not be nil")
		}
	})

	t.Run("CreateAggressive", func(t *testing.T) {
		loop, err := factory.CreateAggressive()
		if err != nil {
			t.Fatalf("Should not error: %v", err)
		}
		if loop == nil {
			t.Error("Loop should not be nil")
		}
	})

	t.Run("CreateCustom", func(t *testing.T) {
		strategy := NewDefaultStrategy(config)
		iteration := &MockIteration{}

		loop := factory.CreateCustom(config, strategy, iteration)
		if loop == nil {
			t.Error("Loop should not be nil")
		}
	})
}

func TestMockIteration(t *testing.T) {
	t.Run("ReturnsMockOutcome", func(t *testing.T) {
		mockOutcome := &IterationOutcome{
			Result:  Break,
			Content: "Test",
		}

		iteration := &MockIteration{
			MockOutcome: mockOutcome,
		}

		outcome, err := iteration.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("Should not error: %v", err)
		}

		if outcome != mockOutcome {
			t.Error("Should return mock outcome")
		}

		if iteration.ExecuteCount != 1 {
			t.Errorf("ExecuteCount should be 1, got %d", iteration.ExecuteCount)
		}
	})

	t.Run("ReturnsMockError", func(t *testing.T) {
		mockErr := errors.New("test error")

		iteration := &MockIteration{
			MockError: mockErr,
		}

		_, err := iteration.Execute(context.Background(), nil)
		if err != mockErr {
			t.Error("Should return mock error")
		}
	})

	t.Run("UsesExecuteFunc", func(t *testing.T) {
		customOutcome := &IterationOutcome{Result: Continue}
		funcCalled := false

		iteration := &MockIteration{
			ExecuteFunc: func(ctx context.Context, state State) (*IterationOutcome, error) {
				funcCalled = true
				return customOutcome, nil
			},
		}

		outcome, _ := iteration.Execute(context.Background(), nil)
		if !funcCalled {
			t.Error("ExecuteFunc should be called")
		}
		if outcome != customOutcome {
			t.Error("Should return custom outcome")
		}
	})

	t.Run("TracksState", func(t *testing.T) {
		iteration := &MockIteration{}
		state := &MockState{}

		_, _ = iteration.Execute(context.Background(), state)

		if iteration.LastState != state {
			t.Error("Should track last state")
		}
	})
}

func TestOrchestratorLoopRun(t *testing.T) {
	t.Run("RunsSingleIteration", func(t *testing.T) {
		config := &Config{
			MaxIterations: 10,
		}

		iteration := &MockIteration{
			MockOutcome: &IterationOutcome{
				Result: Break,
			},
		}

		strategy := NewDefaultStrategy(config)
		loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})
		result, err := loop.Run(context.Background(), &MockSession{}, nil)

		if err != nil {
			t.Fatalf("Should not error: %v", err)
		}

		if !result.Success {
			t.Error("Result should be successful")
		}

		if iteration.ExecuteCount != 1 {
			t.Errorf("Expected 1 iteration, got %d", iteration.ExecuteCount)
		}
	})

	t.Run("RunsMultipleIterations", func(t *testing.T) {
		config := &Config{
			MaxIterations: 10,
		}

		callCount := 0
		iteration := &MockIteration{
			ExecuteFunc: func(ctx context.Context, state State) (*IterationOutcome, error) {
				callCount++
				if callCount < 3 {
					return &IterationOutcome{Result: Continue}, nil
				}
				return &IterationOutcome{Result: Break}, nil
			},
		}

		strategy := NewDefaultStrategy(config)
		loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})
		result, _ := loop.Run(context.Background(), &MockSession{}, nil)

		if !result.Success {
			t.Error("Result should be successful")
		}

		if callCount != 3 {
			t.Errorf("Expected 3 iterations, got %d", callCount)
		}
	})

	t.Run("RespectsMaxIterations", func(t *testing.T) {
		config := &Config{
			MaxIterations: 3,
		}

		iteration := &MockIteration{
			MockOutcome: &IterationOutcome{
				Result: Continue, // Always continue
			},
		}

		strategy := NewDefaultStrategy(config)
		loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})
		result, _ := loop.Run(context.Background(), &MockSession{}, nil)

		if !result.HitIterationLimit {
			t.Error("Result should indicate iteration limit hit")
		}

		if result.IterationsExecuted != 3 {
			t.Errorf("Expected 3 iterations, got %d", result.IterationsExecuted)
		}
	})

	t.Run("HandlesError", func(t *testing.T) {
		config := &Config{
			MaxIterations: 10,
		}

		mockErr := errors.New("test error")
		iteration := &MockIteration{
			MockError: mockErr,
		}

		strategy := NewDefaultStrategy(config)
		loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})
		result, _ := loop.Run(context.Background(), &MockSession{}, nil)

		if result.Success {
			t.Error("Result should not be successful")
		}

		if result.Error != mockErr {
			t.Error("Result should contain the error")
		}
	})

	t.Run("HandlesContextCancellation", func(t *testing.T) {
		config := &Config{
			MaxIterations: 10,
		}

		callCount := 0
		iteration := &MockIteration{
			ExecuteFunc: func(ctx context.Context, state State) (*IterationOutcome, error) {
				callCount++
				// Check context on second call to ensure it propagates
				if callCount >= 2 {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					default:
					}
				}
				return &IterationOutcome{Result: Continue}, nil
			},
		}

		ctx, cancel := context.WithCancel(context.Background())

		strategy := NewDefaultStrategy(config)
		loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})

		// Start loop in background and cancel after first iteration
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		_, err := loop.Run(ctx, &MockSession{}, nil)

		// The loop may complete without error or return context error depending on timing
		// Both are acceptable behaviors
		if err != nil && err != context.Canceled {
			t.Errorf("Expected context.Canceled or nil, got %v", err)
		}
	})

	t.Run("SendsProgressUpdates", func(t *testing.T) {
		config := &Config{
			MaxIterations: 2,
		}

		var progressCount int
		progressCb := func(update progress.Update) error {
			progressCount++
			return nil
		}

		iteration := &MockIteration{
			MockOutcome: &IterationOutcome{
				Result: Break,
			},
		}

		strategy := NewDefaultStrategy(config)
		loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})
		_, _ = loop.Run(context.Background(), &MockSession{}, progressCb)

		if progressCount == 0 {
			t.Error("Should have sent progress updates")
		}
	})
}

func TestOrchestratorLoopRunIteration(t *testing.T) {
	config := DefaultConfig()
	iteration := &MockIteration{
		MockOutcome: &IterationOutcome{Result: Break},
	}
	strategy := NewDefaultStrategy(config)
	loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})

	state := NewDefaultState(config)
	outcome, err := loop.RunIteration(context.Background(), state)

	if err != nil {
		t.Fatalf("Should not error: %v", err)
	}

	if outcome.Result != Break {
		t.Errorf("Expected Break, got %v", outcome.Result)
	}
}

func TestOrchestratorLoopGetState(t *testing.T) {
	config := DefaultConfig()
	iteration := &MockIteration{}
	strategy := NewDefaultStrategy(config)
	loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})

	state := loop.GetState()
	if state == nil {
		t.Error("GetState should not return nil")
	}
}

func TestOrchestratorLoopSetContextManager(t *testing.T) {
	config := DefaultConfig()
	iteration := &MockIteration{}
	strategy := NewDefaultStrategy(config)
	loop := NewOrchestratorLoop(config, strategy, iteration, &Dependencies{})

	// This just shouldn't panic
	loop.SetContextManager(nil)
}
