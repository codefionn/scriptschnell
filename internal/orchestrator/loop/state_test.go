package loop

import (
	"testing"
	"time"
)

func TestNewDefaultState(t *testing.T) {
	t.Run("With nil config uses defaults", func(t *testing.T) {
		state := NewDefaultState(nil)

		if state.maxIterations != 256 {
			t.Errorf("Expected default MaxIterations 256, got %d", state.maxIterations)
		}

		if state.maxAutoContinueAttempts != 10 {
			t.Errorf("Expected default MaxAutoContinueAttempts 10, got %d", state.maxAutoContinueAttempts)
		}

		if !state.enableLoopDetection {
			t.Error("Expected LoopDetection enabled by default")
		}
	})

	t.Run("With custom config", func(t *testing.T) {
		config := &Config{
			MaxIterations:           100,
			MaxAutoContinueAttempts: 5,
			EnableLoopDetection:     false,
		}

		state := NewDefaultState(config)

		if state.maxIterations != 100 {
			t.Errorf("Expected MaxIterations 100, got %d", state.maxIterations)
		}

		if state.maxAutoContinueAttempts != 5 {
			t.Errorf("Expected MaxAutoContinueAttempts 5, got %d", state.maxAutoContinueAttempts)
		}

		if state.enableLoopDetection {
			t.Error("Expected LoopDetection disabled")
		}
	})
}

func TestDefaultStateIteration(t *testing.T) {
	state := NewDefaultState(DefaultConfig())

	t.Run("Initial iteration is 0", func(t *testing.T) {
		if state.Iteration() != 0 {
			t.Errorf("Expected initial iteration 0, got %d", state.Iteration())
		}
	})

	t.Run("Increment increases iteration", func(t *testing.T) {
		newVal := state.Increment()

		if newVal != 1 {
			t.Errorf("Expected increment to return 1, got %d", newVal)
		}

		if state.Iteration() != 1 {
			t.Errorf("Expected iteration 1, got %d", state.Iteration())
		}
	})

	t.Run("Multiple increments", func(t *testing.T) {
		state := NewDefaultState(DefaultConfig())

		for i := 0; i < 5; i++ {
			state.Increment()
		}

		if state.Iteration() != 5 {
			t.Errorf("Expected iteration 5, got %d", state.Iteration())
		}
	})

	t.Run("HasReachedLimit at max", func(t *testing.T) {
		state := NewDefaultState(&Config{MaxIterations: 3})

		if state.HasReachedLimit() {
			t.Error("Should not have reached limit at 0")
		}

		state.Increment()
		state.Increment()
		state.Increment()

		if !state.HasReachedLimit() {
			t.Error("Should have reached limit at 3")
		}
	})
}

func TestDefaultStateAutoContinue(t *testing.T) {
	state := NewDefaultState(&Config{MaxAutoContinueAttempts: 3})

	t.Run("Initial auto-continue is 0", func(t *testing.T) {
		if state.AutoContinueAttempts() != 0 {
			t.Errorf("Expected initial auto-continue 0, got %d", state.AutoContinueAttempts())
		}
	})

	t.Run("Increment auto-continue", func(t *testing.T) {
		newVal := state.IncrementAutoContinue()

		if newVal != 1 {
			t.Errorf("Expected increment to return 1, got %d", newVal)
		}

		if state.AutoContinueAttempts() != 1 {
			t.Errorf("Expected auto-continue 1, got %d", state.AutoContinueAttempts())
		}
	})

	t.Run("Reset auto-continue", func(t *testing.T) {
		state.IncrementAutoContinue()
		state.IncrementAutoContinue()
		state.ResetAutoContinue()

		if state.AutoContinueAttempts() != 0 {
			t.Errorf("Expected auto-continue 0 after reset, got %d", state.AutoContinueAttempts())
		}
	})

	t.Run("HasReachedAutoContinueLimit", func(t *testing.T) {
		state := NewDefaultState(&Config{MaxAutoContinueAttempts: 2})

		if state.HasReachedAutoContinueLimit() {
			t.Error("Should not have reached limit at 0")
		}

		state.IncrementAutoContinue()

		if state.HasReachedAutoContinueLimit() {
			t.Error("Should not have reached limit at 1")
		}

		state.IncrementAutoContinue()

		if !state.HasReachedAutoContinueLimit() {
			t.Error("Should have reached limit at 2")
		}
	})
}

func TestDefaultStateCompaction(t *testing.T) {
	t.Run("Compaction attempts tracking", func(t *testing.T) {
		state := NewDefaultState(DefaultConfig())

		if state.CompactionAttempts() != 0 {
			t.Errorf("Expected initial compaction attempts 0, got %d", state.CompactionAttempts())
		}

		state.IncrementCompactionAttempts()

		if state.CompactionAttempts() != 1 {
			t.Errorf("Expected compaction attempts 1, got %d", state.CompactionAttempts())
		}

		state.ResetCompactionAttempts()

		if state.CompactionAttempts() != 0 {
			t.Errorf("Expected compaction attempts 0 after reset, got %d", state.CompactionAttempts())
		}
	})

	t.Run("Consecutive compactions", func(t *testing.T) {
		state := NewDefaultState(DefaultConfig())

		// First compaction
		state.RecordCompaction()

		if state.ConsecutiveCompactions() != 1 {
			t.Errorf("Expected consecutive compactions 1, got %d", state.ConsecutiveCompactions())
		}

		// Second compaction within 30 seconds
		state.RecordCompaction()

		if state.ConsecutiveCompactions() != 2 {
			t.Errorf("Expected consecutive compactions 2, got %d", state.ConsecutiveCompactions())
		}

		// Third compaction
		state.RecordCompaction()

		if state.ConsecutiveCompactions() != 3 {
			t.Errorf("Expected consecutive compactions 3, got %d", state.ConsecutiveCompactions())
		}

		// Should not allow compaction after 2 consecutive
		if state.ShouldAllowCompaction() {
			t.Error("Should not allow compaction after 3 consecutive")
		}
	})

	t.Run("Compaction with time gap resets consecutive count", func(t *testing.T) {
		state := NewDefaultState(DefaultConfig())

		// First compaction
		state.RecordCompaction()
		state.RecordCompaction()

		if state.ConsecutiveCompactions() != 2 {
			t.Errorf("Expected consecutive compactions 2, got %d", state.ConsecutiveCompactions())
		}

		// Manually set last compaction time to more than 30 seconds ago
		state.mu.Lock()
		state.lastCompactionTime = time.Now().Add(-31 * time.Second)
		state.mu.Unlock()

		// Next compaction should reset consecutive count
		state.RecordCompaction()

		if state.ConsecutiveCompactions() != 1 {
			t.Errorf("Expected consecutive compactions to reset to 1, got %d", state.ConsecutiveCompactions())
		}
	})

	t.Run("LastCompactionTime", func(t *testing.T) {
		state := NewDefaultState(DefaultConfig())

		if !state.LastCompactionTime().IsZero() {
			t.Error("Expected initial last compaction time to be zero")
		}

		state.RecordCompaction()

		if state.LastCompactionTime().IsZero() {
			t.Error("Expected last compaction time to be set")
		}
	})
}

func TestDefaultStateLoopDetectionExtended(t *testing.T) {
	t.Run("Loop detection enabled", func(t *testing.T) {
		state := NewDefaultState(&Config{EnableLoopDetection: true})

		// First few additions shouldn't detect a loop
		for i := 0; i < 3; i++ {
			isLoop, _, _ := state.RecordLoopDetection("some text")
			if isLoop {
				t.Errorf("Should not detect loop at iteration %d", i)
			}
		}
	})

	t.Run("Loop detection disabled", func(t *testing.T) {
		state := NewDefaultState(&Config{EnableLoopDetection: false})

		isLoop, _, _ := state.RecordLoopDetection("some text")

		if isLoop {
			t.Error("Should not detect loop when disabled")
		}
	})

	t.Run("Reset loop detection", func(t *testing.T) {
		state := NewDefaultState(DefaultConfig())

		// Add some text
		state.RecordLoopDetection("text")

		// Reset
		state.ResetLoopDetection()

		// Should be able to start fresh
		isLoop, _, _ := state.RecordLoopDetection("text")

		if isLoop {
			t.Error("Should not detect loop immediately after reset")
		}
	})
}

func TestDefaultStateSetMaxAutoContinueAttemptsExtended(t *testing.T) {
	state := NewDefaultState(DefaultConfig())

	state.SetMaxAutoContinueAttempts(20)

	if state.MaxAutoContinueAttempts() != 20 {
		t.Errorf("Expected MaxAutoContinueAttempts 20, got %d", state.MaxAutoContinueAttempts())
	}
}

func TestDefaultStateResetIterationCounter(t *testing.T) {
	state := NewDefaultState(DefaultConfig())

	state.Increment()
	state.Increment()
	state.Increment()

	if state.Iteration() != 3 {
		t.Errorf("Expected iteration 3, got %d", state.Iteration())
	}

	state.ResetIterationCounter()

	if state.Iteration() != 0 {
		t.Errorf("Expected iteration 0 after reset, got %d", state.Iteration())
	}
}

func TestDefaultStateThreadSafety(t *testing.T) {
	state := NewDefaultState(DefaultConfig())

	// Run concurrent operations
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				state.Increment()
			}
			done <- true
		}()
	}

	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = state.Iteration()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	if state.Iteration() != 500 {
		t.Errorf("Expected iteration 500, got %d", state.Iteration())
	}
}

// MockState tests

func TestMockStateExtended(t *testing.T) {
	state := &MockState{
		MockIteration:               5,
		MockMaxIterations:           10,
		MockAutoContinueAttempts:    2,
		MockMaxAutoContinueAttempts: 5,
		MockCompactionAttempts:      1,
		MockConsecutiveCompactions:  2,
		MockShouldAllowCompaction:   true,
		MockLoopDetectionResult:     true,
		MockLoopPattern:             "pattern",
		MockLoopCount:               3,
	}

	t.Run("Iteration methods", func(t *testing.T) {
		if state.Iteration() != 5 {
			t.Error("Iteration mismatch")
		}

		newVal := state.Increment()
		if newVal != 6 {
			t.Errorf("Expected 6 after increment, got %d", newVal)
		}

		if state.MockIteration != 6 {
			t.Error("MockIteration not updated")
		}
	})

	t.Run("Limit checks", func(t *testing.T) {
		if state.HasReachedLimit() {
			t.Error("Should not have reached limit at 6/10")
		}

		state.MockIteration = 10

		if !state.HasReachedLimit() {
			t.Error("Should have reached limit at 10/10")
		}
	})

	t.Run("Auto-continue methods", func(t *testing.T) {
		if state.AutoContinueAttempts() != 2 {
			t.Error("AutoContinueAttempts mismatch")
		}

		state.IncrementAutoContinue()

		if state.AutoContinueAttempts() != 3 {
			t.Error("AutoContinueAttempts not incremented")
		}

		state.ResetAutoContinue()

		if state.AutoContinueAttempts() != 0 {
			t.Error("AutoContinueAttempts not reset")
		}
	})

	t.Run("Compaction methods", func(t *testing.T) {
		if state.CompactionAttempts() != 1 {
			t.Error("CompactionAttempts mismatch")
		}

		if state.ConsecutiveCompactions() != 2 {
			t.Error("ConsecutiveCompactions mismatch")
		}

		if !state.ShouldAllowCompaction() {
			t.Error("ShouldAllowCompaction mismatch")
		}

		state.RecordCompaction()

		if state.LastCompactionTime().IsZero() {
			t.Error("LastCompactionTime not set")
		}
	})

	t.Run("Loop detection", func(t *testing.T) {
		isLoop, pattern, count := state.RecordLoopDetection("text")

		if !isLoop {
			t.Error("Expected loop detection")
		}

		if pattern != "pattern" {
			t.Error("Pattern mismatch")
		}

		if count != 3 {
			t.Error("Count mismatch")
		}
	})
}

func TestMockStateCallbacksExtended(t *testing.T) {
	var incrementCalled, autoContinueCalled, loopDetectionCalled, compactionCalled bool

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
	state.IncrementAutoContinue()
	state.RecordLoopDetection("test")
	state.RecordCompaction()

	if !incrementCalled {
		t.Error("OnIncrement not called")
	}

	if !autoContinueCalled {
		t.Error("OnIncrementAutoContinue not called")
	}

	if !loopDetectionCalled {
		t.Error("OnRecordLoopDetection not called")
	}

	if !compactionCalled {
		t.Error("OnRecordCompaction not called")
	}
}
