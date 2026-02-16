package tools

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// ExecDeadline is the interface for pausable execution deadlines.
type ExecDeadline interface {
	Pause()
	Resume()
	Stop()
	RecordActivity()
}

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

// RecordActivity is a no-op for the base execDeadline.
// Adaptive deadlines override this to track activity and extend timeouts.
// Safe to call on a nil receiver.
func (d *execDeadline) RecordActivity() {
	// No-op for base deadline
}

// execActivityTracker tracks the timestamp of the last recorded activity.
type execActivityTracker struct {
	mu           sync.Mutex
	lastActivity time.Time
}

// RecordActivity updates the last activity timestamp to the current time.
// Thread-safe and can be called concurrently.
func (t *execActivityTracker) RecordActivity() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastActivity = time.Now()
}

// LastActivity returns the timestamp of the last recorded activity.
// Thread-safe. Returns zero time if no activity has been recorded.
func (t *execActivityTracker) LastActivity() time.Time {
	if t == nil {
		return time.Time{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastActivity
}

// adaptiveExecDeadline extends execDeadline with activity-aware timeout extension.
// If activity occurs within timeout/2 after the original timeout expires,
// the deadline can extend up to 5x the original timeout.
type adaptiveExecDeadline struct {
	*execDeadline
	activityTracker *execActivityTracker
	originalTimeout time.Duration
	maxExtension    time.Duration
	gracePeriod     time.Duration
	extensions      int
	maxExtensions   int
}

// newAdaptiveExecDeadline creates an adaptive deadline that extends the timeout
// when activity is detected after the original timeout expires.
//
// Parameters:
//   - timeout: The initial timeout duration
//   - cancel: Function to call when the (possibly extended) timeout is reached
//   - maxExtensions: Maximum number of extensions allowed (default 4 for 5x total)
func newAdaptiveExecDeadline(timeout time.Duration, cancel context.CancelFunc, maxExtensions int) *adaptiveExecDeadline {
	if maxExtensions <= 0 {
		maxExtensions = 4 // Allow up to 5x original timeout
	}

	activityTracker := &execActivityTracker{}
	activityTracker.RecordActivity() // Record initial activity at start

	return &adaptiveExecDeadline{
		execDeadline:    newExecDeadline(timeout, cancel),
		activityTracker: activityTracker,
		originalTimeout: timeout,
		maxExtension:    time.Duration(maxExtensions) * timeout,
		gracePeriod:     timeout / 2,
		extensions:      0,
		maxExtensions:   maxExtensions,
	}
}

// RecordActivity records activity and checks if the deadline should be extended.
// Safe to call on a nil receiver.
func (d *adaptiveExecDeadline) RecordActivity() {
	if d == nil {
		return
	}
	d.activityTracker.RecordActivity()

	// Check if we should extend the deadline
	d.maybeExtend()
}

// Pause stops the countdown and preserves the remaining budget.
// Delegates to the embedded execDeadline.
func (d *adaptiveExecDeadline) Pause() {
	if d == nil {
		return
	}
	d.execDeadline.Pause()
}

// Resume restarts the countdown with the remaining budget.
// Delegates to the embedded execDeadline.
func (d *adaptiveExecDeadline) Resume() {
	if d == nil {
		return
	}
	d.execDeadline.Resume()
}

// maybeExtend checks if activity occurred recently and extends the deadline if appropriate.
// Must be called with the mutex locked.
func (d *adaptiveExecDeadline) maybeExtend() {
	d.execDeadline.mu.Lock()
	defer d.execDeadline.mu.Unlock()

	// Don't extend if already fired or stopped
	if d.fired {
		return
	}

	// Check if we've reached the maximum number of extensions
	if d.extensions >= d.maxExtensions {
		return
	}

	lastActivity := d.activityTracker.LastActivity()
	if lastActivity.IsZero() {
		return
	}

	// Check if activity occurred within the grace period after the original timeout would have fired
	elapsedSinceActivity := time.Since(lastActivity)
	if elapsedSinceActivity > d.gracePeriod {
		// No recent activity, don't extend
		return
	}

	// Activity is recent - extend the deadline
	d.extend()
	logger.Debug("sandbox: adaptive deadline extended (extension %d/%d, original timeout: %v, total: %v)",
		d.extensions, d.maxExtensions,
		d.originalTimeout,
		time.Duration(d.extensions+1)*d.originalTimeout)
}

// extend extends the deadline by another timeout period.
// Must be called with the mutex locked.
func (d *adaptiveExecDeadline) extend() {
	// Stop the current timer
	if !d.execDeadline.timer.Stop() {
		// Timer already fired
		return
	}

	// Calculate elapsed time since last start
	elapsed := time.Since(d.startedAt)
	d.remaining -= elapsed
	if d.remaining < 0 {
		d.remaining = 0
	}

	// Add another timeout period to remaining
	d.remaining += d.originalTimeout
	d.extensions++

	// Restart the timer with new remaining time
	d.paused = false
	d.startedAt = time.Now()
	d.timer.Reset(d.remaining)
}

// GetStats returns statistics about deadline extensions for debugging.
func (d *adaptiveExecDeadline) GetStats() map[string]interface{} {
	if d == nil {
		return nil
	}
	d.execDeadline.mu.Lock()
	defer d.execDeadline.mu.Unlock()

	var totalTimeout time.Duration
	if d.extensions == 0 {
		totalTimeout = d.originalTimeout
	} else {
		totalTimeout = time.Duration(d.extensions+1) * d.originalTimeout
	}

	return map[string]interface{}{
		"original_timeout_seconds": d.originalTimeout.Seconds(),
		"extensions":               d.extensions,
		"max_extensions":           d.maxExtensions,
		"total_timeout_seconds":    totalTimeout.Seconds(),
		"grace_period_seconds":     d.gracePeriod.Seconds(),
		"fired":                    d.fired,
	}
}

// Stop permanently disables the adaptive deadline. The cancel function is NOT called.
// Safe to call on a nil receiver.
func (d *adaptiveExecDeadline) Stop() {
	if d == nil {
		return
	}
	d.execDeadline.Stop()
}

// fileMonitor polls stdout and stderr files for size changes to detect activity.
type fileMonitor struct {
	ctx      context.Context
	cancel   context.CancelFunc
	stdout   string
	stderr   string
	deadline *adaptiveExecDeadline
	interval time.Duration

	// Last known file sizes
	lastStdoutSize int64
	lastStderrSize int64
}

// newFileMonitor creates a file monitor for stdout.txt and stderr.txt files.
// The monitor polls the files at the specified interval and records activity
// to the adaptive deadline when size changes are detected.
func newFileMonitor(stdoutPath, stderrPath string, deadline *adaptiveExecDeadline) *fileMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	// Get initial file sizes
	stdoutSize := getFileSize(stdoutPath)
	stderrSize := getFileSize(stderrPath)

	return &fileMonitor{
		ctx:            ctx,
		cancel:         cancel,
		stdout:         stdoutPath,
		stderr:         stderrPath,
		deadline:       deadline,
		interval:       100 * time.Millisecond,
		lastStdoutSize: stdoutSize,
		lastStderrSize: stderrSize,
	}
}

// Start begins monitoring the files in a background goroutine.
func (fm *fileMonitor) Start() {
	if fm == nil {
		return
	}
	go fm.monitor()
}

// monitor polls the files for size changes and records activity.
func (fm *fileMonitor) monitor() {
	ticker := time.NewTicker(fm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-fm.ctx.Done():
			return
		case <-ticker.C:
			fm.checkForActivity()
		}
	}
}

// checkForActivity checks if either file size has changed and records activity.
func (fm *fileMonitor) checkForActivity() {
	stdoutSize := getFileSize(fm.stdout)
	stderrSize := getFileSize(fm.stderr)

	activityDetected := false

	if stdoutSize != fm.lastStdoutSize {
		fm.lastStdoutSize = stdoutSize
		activityDetected = true
	}

	if stderrSize != fm.lastStderrSize {
		fm.lastStderrSize = stderrSize
		activityDetected = true
	}

	if activityDetected && fm.deadline != nil {
		fm.deadline.RecordActivity()
	}
}

// Stop stops the file monitor goroutine.
func (fm *fileMonitor) Stop() {
	if fm == nil {
		return
	}
	fm.cancel()
}

// getFileSize returns the size of a file, or -1 if the file doesn't exist.
func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}
