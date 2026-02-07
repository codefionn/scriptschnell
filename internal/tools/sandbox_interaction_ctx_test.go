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
