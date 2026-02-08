package tools

import (
	"context"
	"sync"
	"time"
)

// execDeadline implements a pausable execution deadline for sandbox timeout.
// It counts only actual execution time, excluding time spent waiting for user
// interaction (e.g., authorization prompts). When paused, the remaining budget
// is preserved and the timer restarts from where it left off on resume.
type execDeadline struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	timer     *time.Timer
	remaining time.Duration
	startedAt time.Time
	paused    bool
	fired     bool
}

// newExecDeadline creates a deadline that will call cancel after timeout
// elapses (excluding paused intervals).
func newExecDeadline(timeout time.Duration, cancel context.CancelFunc) *execDeadline {
	d := &execDeadline{
		cancel:    cancel,
		remaining: timeout,
		startedAt: time.Now(),
	}
	d.timer = time.AfterFunc(timeout, func() {
		d.mu.Lock()
		d.fired = true
		d.mu.Unlock()
		cancel()
	})
	return d
}

// Pause stops the countdown and preserves the remaining budget.
// Safe to call on a nil receiver or multiple times; subsequent calls are no-ops.
func (d *execDeadline) Pause() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.paused || d.fired {
		return
	}
	if d.timer.Stop() {
		elapsed := time.Since(d.startedAt)
		d.remaining -= elapsed
		if d.remaining < 0 {
			d.remaining = 0
		}
	}
	d.paused = true
}

// Resume restarts the countdown with the remaining budget.
// Safe to call on a nil receiver or multiple times; subsequent calls are no-ops.
func (d *execDeadline) Resume() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.paused || d.fired {
		return
	}
	d.paused = false
	d.startedAt = time.Now()
	if d.remaining <= 0 {
		d.fired = true
		d.cancel()
		return
	}
	d.timer.Reset(d.remaining)
}

// Stop permanently disables the deadline. The cancel function is NOT called.
// Safe to call on a nil receiver.
func (d *execDeadline) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.timer.Stop()
}
