package loop

import (
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/loopdetector"
)

// DefaultState implements the State interface with thread-safe state management.
// It tracks iteration counters, auto-continue attempts, and loop detection.
type DefaultState struct {
	mu sync.RWMutex

	// Core iteration tracking
	iteration     int
	maxIterations int

	// Auto-continue tracking
	autoContinueAttempts    int
	maxAutoContinueAttempts int

	// Compaction tracking
	compactionAttempts     int
	consecutiveCompactions int
	lastCompactionTime     time.Time

	// Loop detection
	loopDetector        *loopdetector.LoopDetector
	enableLoopDetection bool
}

// NewDefaultState creates a new DefaultState with the specified configuration
func NewDefaultState(config *Config) *DefaultState {
	if config == nil {
		config = DefaultConfig()
	}

	return &DefaultState{
		maxIterations:           config.MaxIterations,
		maxAutoContinueAttempts: config.MaxAutoContinueAttempts,
		enableLoopDetection:     config.EnableLoopDetection,
		loopDetector:            loopdetector.NewLoopDetector(),
	}
}

// Iteration returns the current iteration count (0-based)
func (s *DefaultState) Iteration() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.iteration
}

// Increment advances the iteration counter and returns the new count
func (s *DefaultState) Increment() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iteration++
	return s.iteration
}

// MaxIterations returns the maximum number of iterations allowed
func (s *DefaultState) MaxIterations() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxIterations
}

// HasReachedLimit returns true if the maximum iteration limit has been reached
func (s *DefaultState) HasReachedLimit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.iteration >= s.maxIterations
}

// AutoContinueAttempts returns the current number of auto-continue attempts
func (s *DefaultState) AutoContinueAttempts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.autoContinueAttempts
}

// IncrementAutoContinue increments the auto-continue counter and returns the new count
func (s *DefaultState) IncrementAutoContinue() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoContinueAttempts++
	return s.autoContinueAttempts
}

// MaxAutoContinueAttempts returns the maximum number of auto-continue attempts allowed
func (s *DefaultState) MaxAutoContinueAttempts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxAutoContinueAttempts
}

// HasReachedAutoContinueLimit returns true if auto-continue limit has been reached
func (s *DefaultState) HasReachedAutoContinueLimit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.autoContinueAttempts >= s.maxAutoContinueAttempts
}

// ResetAutoContinue resets the auto-continue counter to zero
func (s *DefaultState) ResetAutoContinue() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoContinueAttempts = 0
}

// RecordLoopDetection records a potential loop pattern for detection
// Returns true if a loop is detected, along with the pattern and repetition count
func (s *DefaultState) RecordLoopDetection(text string) (isLoop bool, pattern string, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enableLoopDetection || s.loopDetector == nil {
		return false, "", 0
	}

	return s.loopDetector.AddText(text)
}

// ResetLoopDetection resets the loop detector state
func (s *DefaultState) ResetLoopDetection() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loopDetector != nil {
		s.loopDetector.Reset()
	}
}

// CompactionAttempts returns the number of compaction attempts for current request
func (s *DefaultState) CompactionAttempts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.compactionAttempts
}

// IncrementCompactionAttempts increments the compaction counter
func (s *DefaultState) IncrementCompactionAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactionAttempts++
	return s.compactionAttempts
}

// ResetCompactionAttempts resets compaction attempts to zero
func (s *DefaultState) ResetCompactionAttempts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactionAttempts = 0
}

// ConsecutiveCompactions returns the number of consecutive compactions
func (s *DefaultState) ConsecutiveCompactions() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.consecutiveCompactions
}

// RecordCompaction records a compaction event with timestamp
func (s *DefaultState) RecordCompaction() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// Check if this is consecutive (within 30 seconds of last compaction)
	if !s.lastCompactionTime.IsZero() && now.Sub(s.lastCompactionTime) < 30*time.Second {
		s.consecutiveCompactions++
	} else {
		s.consecutiveCompactions = 1
	}
	s.lastCompactionTime = now
}

// ShouldAllowCompaction returns true if compaction should be allowed based on recent history
func (s *DefaultState) ShouldAllowCompaction() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Limit consecutive compactions to prevent thrashing
	return s.consecutiveCompactions < 2
}

// LastCompactionTime returns the timestamp of the last compaction
func (s *DefaultState) LastCompactionTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastCompactionTime
}

// SetMaxAutoContinueAttempts updates the maximum auto-continue attempts
// This is useful for model-specific limits
func (s *DefaultState) SetMaxAutoContinueAttempts(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxAutoContinueAttempts = max
}

// ResetIterationCounter resets the iteration counter (for testing/reset)
func (s *DefaultState) ResetIterationCounter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iteration = 0
}

// MockState is a test-friendly implementation of State with configurable behavior
type MockState struct {
	MockIteration               int
	MockMaxIterations           int
	MockAutoContinueAttempts    int
	MockMaxAutoContinueAttempts int
	MockCompactionAttempts      int
	MockConsecutiveCompactions  int
	MockShouldAllowCompaction   bool
	MockLoopDetectionResult     bool
	MockLoopPattern             string
	MockLoopCount               int
	MockLastCompactionTime      time.Time

	// Callbacks for tracking calls
	OnIncrement             func()
	OnIncrementAutoContinue func()
	OnRecordLoopDetection   func(text string)
	OnRecordCompaction      func()
}

// Iteration returns the mock iteration count
func (m *MockState) Iteration() int { return m.MockIteration }

// Increment advances the mock iteration counter
func (m *MockState) Increment() int {
	if m.OnIncrement != nil {
		m.OnIncrement()
	}
	m.MockIteration++
	return m.MockIteration
}

// MaxIterations returns the mock maximum
func (m *MockState) MaxIterations() int { return m.MockMaxIterations }

// HasReachedLimit returns whether mock limit is reached
func (m *MockState) HasReachedLimit() bool {
	return m.MockIteration >= m.MockMaxIterations
}

// AutoContinueAttempts returns the mock count
func (m *MockState) AutoContinueAttempts() int { return m.MockAutoContinueAttempts }

// IncrementAutoContinue increments the mock auto-continue counter
func (m *MockState) IncrementAutoContinue() int {
	if m.OnIncrementAutoContinue != nil {
		m.OnIncrementAutoContinue()
	}
	m.MockAutoContinueAttempts++
	return m.MockAutoContinueAttempts
}

// MaxAutoContinueAttempts returns the mock maximum
func (m *MockState) MaxAutoContinueAttempts() int { return m.MockMaxAutoContinueAttempts }

// HasReachedAutoContinueLimit returns whether mock limit is reached
func (m *MockState) HasReachedAutoContinueLimit() bool {
	return m.MockAutoContinueAttempts >= m.MockMaxAutoContinueAttempts
}

// ResetAutoContinue resets the mock auto-continue counter
func (m *MockState) ResetAutoContinue() { m.MockAutoContinueAttempts = 0 }

// RecordLoopDetection records mock loop detection
func (m *MockState) RecordLoopDetection(text string) (bool, string, int) {
	if m.OnRecordLoopDetection != nil {
		m.OnRecordLoopDetection(text)
	}
	return m.MockLoopDetectionResult, m.MockLoopPattern, m.MockLoopCount
}

// ResetLoopDetection is a no-op for the mock
func (m *MockState) ResetLoopDetection() {}

// CompactionAttempts returns the mock count
func (m *MockState) CompactionAttempts() int { return m.MockCompactionAttempts }

// IncrementCompactionAttempts increments the mock compaction counter
func (m *MockState) IncrementCompactionAttempts() int {
	m.MockCompactionAttempts++
	return m.MockCompactionAttempts
}

// ResetCompactionAttempts resets the mock compaction counter
func (m *MockState) ResetCompactionAttempts() { m.MockCompactionAttempts = 0 }

// ConsecutiveCompactions returns the mock count
func (m *MockState) ConsecutiveCompactions() int { return m.MockConsecutiveCompactions }

// RecordCompaction records a mock compaction event
func (m *MockState) RecordCompaction() {
	if m.OnRecordCompaction != nil {
		m.OnRecordCompaction()
	}
	m.MockLastCompactionTime = time.Now()
}

// ShouldAllowCompaction returns the mock value
func (m *MockState) ShouldAllowCompaction() bool { return m.MockShouldAllowCompaction }

// LastCompactionTime returns the mock timestamp
func (m *MockState) LastCompactionTime() time.Time { return m.MockLastCompactionTime }
