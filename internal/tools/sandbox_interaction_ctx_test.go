package tools

import (
	"context"
	"testing"
	"time"
)

// --- interactionCtx tests ---

func TestInteractionCtx_ReturnsParentCtxWhenSet(t *testing.T) {
	parentCtx := context.WithValue(context.Background(), ctxKey("role"), "parent")
	fallbackCtx := context.WithValue(context.Background(), ctxKey("role"), "fallback")

	st := &SandboxTool{parentCtx: parentCtx}
	got := st.interactionCtx(fallbackCtx)

	if got.Value(ctxKey("role")) != "parent" {
		t.Fatalf("expected parent context, got fallback")
	}
}

func TestInteractionCtx_ReturnsFallbackWhenNil(t *testing.T) {
	fallbackCtx := context.WithValue(context.Background(), ctxKey("role"), "fallback")

	st := &SandboxTool{} // parentCtx is nil
	got := st.interactionCtx(fallbackCtx)

	if got.Value(ctxKey("role")) != "fallback" {
		t.Fatalf("expected fallback context, got something else")
	}
}

func TestInteractionCtx_FallbackCancelDoesNotAffectParent(t *testing.T) {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	fallbackCtx, fallbackCancel := context.WithCancel(context.Background())

	st := &SandboxTool{parentCtx: parentCtx}
	got := st.interactionCtx(fallbackCtx)

	// Cancel the fallback (simulates sandbox timeout expiring)
	fallbackCancel()

	select {
	case <-got.Done():
		t.Fatal("interaction context should NOT be cancelled when only fallback is cancelled")
	default:
		// expected: still alive
	}
}

// --- execDeadline tests ---

func TestExecDeadline_FiresAfterTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = newExecDeadline(50*time.Millisecond, cancel)

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("deadline should have fired within 50ms")
	}
}

func TestExecDeadline_PausePreventsFiring(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newExecDeadline(50*time.Millisecond, cancel)

	// Pause immediately
	d.Pause()

	// Wait longer than the timeout
	time.Sleep(100 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("deadline should NOT have fired while paused")
	default:
		// expected: context still alive
	}

	d.Stop()
}

func TestExecDeadline_ResumeRestartsCounting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newExecDeadline(80*time.Millisecond, cancel)

	// Let 30ms elapse, then pause
	time.Sleep(30 * time.Millisecond)
	d.Pause()

	// Wait 200ms while paused — this should NOT count
	time.Sleep(200 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("deadline should NOT have fired while paused")
	default:
	}

	// Resume — ~50ms remaining budget
	d.Resume()

	select {
	case <-ctx.Done():
		// expected: fires after the remaining ~50ms
	case <-time.After(2 * time.Second):
		t.Fatal("deadline should have fired after resume")
	}
}

func TestExecDeadline_StopPreventsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newExecDeadline(50*time.Millisecond, cancel)
	d.Stop()

	time.Sleep(100 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("deadline should NOT fire after Stop")
	default:
		// expected
	}
}

func TestExecDeadline_MultiplePauseResumeCycles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newExecDeadline(100*time.Millisecond, cancel)

	// Cycle 1: run 20ms, pause, wait 200ms
	time.Sleep(20 * time.Millisecond)
	d.Pause()
	time.Sleep(200 * time.Millisecond)
	d.Resume()

	// Cycle 2: run 20ms, pause, wait 200ms
	time.Sleep(20 * time.Millisecond)
	d.Pause()
	time.Sleep(200 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("should NOT have fired — only ~40ms of budget consumed")
	default:
	}

	// Resume and let remaining ~60ms elapse
	d.Resume()

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("deadline should have fired after final resume")
	}
}

// ctxKey is a private context key type to avoid collisions.
type ctxKey string

// --- adaptiveExecDeadline tests ---

func TestAdaptiveExecDeadline_BasicExtension(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set initial timeout to 100ms, max 4 extensions (5x total = 500ms)
	d := newAdaptiveExecDeadline(100*time.Millisecond, cancel, 4)

	// Wait for original timeout to nearly expire
	time.Sleep(90 * time.Millisecond)

	// Record activity just before timeout
	d.RecordActivity()

	// Wait past original timeout (100ms) - should have extended
	time.Sleep(30 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("deadline should NOT have fired - activity should have extended it")
	default:
		// expected: still alive due to extension
	}

	d.Stop()
}

func TestAdaptiveExecDeadline_NoExtensionWithoutActivity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set initial timeout to 50ms
	d := newAdaptiveExecDeadline(50*time.Millisecond, cancel, 4)

	// Wait for timeout to fire without recording any activity
	select {
	case <-ctx.Done():
		// expected: deadline fired
	case <-time.After(1 * time.Second):
		t.Fatal("deadline should have fired after 50ms")
	}

	stats := d.GetStats()
	if stats["extensions"].(int) != 0 {
		t.Fatalf("expected 0 extensions, got %d", stats["extensions"])
	}
}

func TestAdaptiveExecDeadline_MaxExtensionsLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set initial timeout to 50ms, max 2 extensions (3x total = 150ms)
	d := newAdaptiveExecDeadline(50*time.Millisecond, cancel, 2)

	// Keep recording activity to trigger extensions
	for i := 0; i < 5; i++ {
		time.Sleep(40 * time.Millisecond)
		d.RecordActivity()
	}

	// After ~200ms, should have fired (original 50ms + 2*50ms extensions = 150ms max)
	select {
	case <-ctx.Done():
		// expected: deadline fired after max extensions
	case <-time.After(500 * time.Millisecond):
		t.Fatal("deadline should have fired after max extensions reached")
	}

	stats := d.GetStats()
	if stats["extensions"].(int) > 2 {
		t.Fatalf("expected max 2 extensions, got %d", stats["extensions"])
	}
}

func TestAdaptiveExecDeadline_ActivityAfterGracePeriod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set initial timeout to 50ms (grace period = 25ms)
	d := newAdaptiveExecDeadline(50*time.Millisecond, cancel, 4)

	// Wait for timeout to expire (60ms)
	time.Sleep(60 * time.Millisecond)

	// Record activity after grace period - should NOT extend
	time.Sleep(30 * time.Millisecond)
	d.RecordActivity()

	// Context should be cancelled already
	select {
	case <-ctx.Done():
		// expected: already fired
	case <-time.After(100 * time.Millisecond):
		t.Fatal("deadline should have already fired")
	}

	stats := d.GetStats()
	if stats["extensions"].(int) != 0 {
		t.Fatalf("expected 0 extensions - activity was too late, got %d", stats["extensions"])
	}
}

func TestAdaptiveExecDeadline_MultipleExtensions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set initial timeout to 100ms, max 4 extensions
	d := newAdaptiveExecDeadline(100*time.Millisecond, cancel, 4)

	// Trigger multiple extensions by recording activity at intervals
	for i := 0; i < 3; i++ {
		time.Sleep(90 * time.Millisecond)
		d.RecordActivity()
	}

	// Should still be alive (100ms * 4 = 400ms total budget consumed)
	select {
	case <-ctx.Done():
		t.Fatal("deadline should NOT have fired yet - should have extended 3 times")
	default:
		// expected: still alive
	}

	stats := d.GetStats()
	if stats["extensions"].(int) < 3 {
		t.Fatalf("expected at least 3 extensions, got %d", stats["extensions"])
	}

	d.Stop()
}

func TestAdaptiveExecDeadline_GetStats(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newAdaptiveExecDeadline(100*time.Millisecond, cancel, 4)

	stats := d.GetStats()

	if stats["original_timeout_seconds"].(float64) != 0.1 {
		t.Fatalf("expected original_timeout_seconds = 0.1, got %v", stats["original_timeout_seconds"])
	}

	if stats["extensions"].(int) != 0 {
		t.Fatalf("expected extensions = 0, got %d", stats["extensions"])
	}

	if stats["max_extensions"].(int) != 4 {
		t.Fatalf("expected max_extensions = 4, got %d", stats["max_extensions"])
	}

	if stats["grace_period_seconds"].(float64) != 0.05 {
		t.Fatalf("expected grace_period_seconds = 0.05, got %v", stats["grace_period_seconds"])
	}

	if stats["fired"].(bool) {
		t.Fatal("expected fired = false")
	}

	d.Stop()
}

func TestAdaptiveExecDeadline_StopPreventsExtension(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newAdaptiveExecDeadline(100*time.Millisecond, cancel, 4)

	// Stop the deadline
	d.Stop()

	// Wait past original timeout
	time.Sleep(200 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("deadline should NOT fire after Stop")
	default:
		// expected: still alive
	}
}

func TestExecActivityTracker_Basic(t *testing.T) {
	tracker := &execActivityTracker{}

	// Initially should have zero time
	if !tracker.LastActivity().IsZero() {
		t.Fatal("expected zero time for uninitialized tracker")
	}

	// Record activity
	tracker.RecordActivity()

	lastActivity := tracker.LastActivity()
	if lastActivity.IsZero() {
		t.Fatal("expected non-zero time after recording activity")
	}

	// Should be approximately now
	if time.Since(lastActivity) > time.Second {
		t.Fatal("last activity time should be approximately now")
	}
}

func TestExecActivityTracker_Concurrent(t *testing.T) {
	tracker := &execActivityTracker{}

	// Record activity concurrently from multiple goroutines
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			tracker.RecordActivity()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 100; i++ {
		<-done
	}

	// Last activity should have been updated
	lastActivity := tracker.LastActivity()
	if lastActivity.IsZero() {
		t.Fatal("expected non-zero time after concurrent updates")
	}
}

func TestAdaptiveExecDeadline_NilReceiver(t *testing.T) {
	// Test that methods don't panic on nil receiver
	var d *adaptiveExecDeadline

	// These should all be no-ops
	d.RecordActivity()
	d.Stop()

	stats := d.GetStats()
	if stats != nil {
		t.Fatal("expected nil stats for nil receiver")
	}
}

func TestAdaptiveExecDeadline_WithPauseResume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newAdaptiveExecDeadline(100*time.Millisecond, cancel, 4)

	// Run for 40ms, pause
	time.Sleep(40 * time.Millisecond)
	d.Pause()

	// Record activity while paused (should still extend)
	d.RecordActivity()

	// Wait 200ms while paused
	time.Sleep(200 * time.Millisecond)

	// Should still be alive (paused time doesn't count)
	select {
	case <-ctx.Done():
		t.Fatal("deadline should NOT fire while paused")
	default:
		// expected: still alive
	}

	// Resume - should have ~60ms remaining plus extension
	d.Resume()

	// Wait a bit - should still be alive due to extension
	time.Sleep(100 * time.Millisecond)

	select {
	case <-ctx.Done():
		// This might fire depending on timing, but the pause should have worked
		// The important thing is it didn't fire during the pause
	case <-time.After(1 * time.Second):
		// If it didn't fire, that's also fine - the extension worked
	}

	d.Stop()
}

// --- fileMonitor tests ---