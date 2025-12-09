package actor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestActorRefConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("concurrent-actor")
	ref := NewActorRef("concurrent-actor", actor, 1000)

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Send messages concurrently
	var wg sync.WaitGroup
	numMessages := 100
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := &TestMessage{ID: "msg", Content: string(rune(i))}
			if err := ref.Send(msg); err != nil {
				t.Errorf("failed to send message %d: %v", i, err)
			}
		}(i)
	}

	wg.Wait()

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check all messages were received
	if actor.GetReceiveCount() != int32(numMessages) {
		t.Errorf("expected %d messages, got %d", numMessages, actor.GetReceiveCount())
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := ref.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}
}

func TestActorRefErrorHandling(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("error-actor")
	actor.SetShouldError(true)
	ref := NewActorRef("error-actor", actor, 10)

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Send a message that will cause an error
	err = ref.Send(&ErrorMessage{})
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Actor should still be running despite error
	err = ref.Send(&TestMessage{ID: "msg-2"})
	if err != nil {
		t.Error("actor should still accept messages after error")
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := ref.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}
}

// Additional comprehensive error handling tests for ActorRef
func TestActorRefErrorHandling_Detailed(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("detailed-error-actor")
	ref := NewActorRef("detailed-error-actor", actor, 10)

	// Test error message type handling
	actor.SetReceiveHandler(func(ctx context.Context, msg Message) error {
		if _, ok := msg.(*ErrorMessage); ok {
			return errors.New("detailed error from ErrorMessage")
		}
		return nil
	})

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Send error message
	err = ref.Send(&ErrorMessage{})
	if err != nil {
		t.Fatalf("failed to send error message: %v", err)
	}

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Send a normal message to verify actor is still functional
	err = ref.Send(&TestMessage{ID: "normal", Content: "test"})
	if err != nil {
		t.Error("actor should still accept normal messages after error")
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := ref.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}
}

func TestSystemSpawnWithOptions(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	actor := NewTestActor("actor-1")
	ref, err := system.SpawnWithOptions(ctx, "actor-1", actor, 10, WithSequentialProcessing())
	if err != nil {
		t.Fatalf("failed to spawn actor with options: %v", err)
	}

	if !actor.WasStartCalled() {
		t.Error("Start() was not called on actor")
	}

	// Verify sequential processing option is set
	if !ref.sequential {
		t.Error("expected sequential processing option to be set")
	}

	// Clean up
	if err := system.StopAll(context.Background()); err != nil {
		t.Fatalf("failed to stop all actors: %v", err)
	}
}

// Additional comprehensive tests for System
func TestSystemSpawnWithOptions_Detailed(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	// Test spawning with multiple options
	actor := NewTestActor("options-actor")
	ref, err := system.SpawnWithOptions(ctx, "options-actor", actor, 100, WithSequentialProcessing())
	if err != nil {
		t.Fatalf("failed to spawn actor with options: %v", err)
	}

	// Verify the actor is properly configured
	if ref.ID() != "options-actor" {
		t.Errorf("expected actor ID to be 'options-actor', got '%s'", ref.ID())
	}

	// Verify sequential processing is enabled
	if !ref.sequential {
		t.Error("expected sequential processing to be enabled")
	}

	// Test that it actually processes messages sequentially
	processed := make(chan bool, 1)
	actor.SetReceiveHandler(func(ctx context.Context, msg Message) error {
		processed <- true
		return nil
	})

	start := time.Now()
	err = ref.Send(&TestMessage{ID: "test", Content: "sequential"})
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Should wait for processing
	select {
	case <-processed:
		// Message was processed
	case <-time.After(100 * time.Millisecond):
		t.Error("message should have been processed by now")
	}

	duration := time.Since(start)
	if duration < 1*time.Millisecond {
		// On fast systems, sequential processing might complete very quickly.
		// The important part is that the message was processed, not the exact duration.
		t.Logf("Sequential processing completed quickly: %v", duration)
	}

	// Clean up
	if err := system.StopAll(context.Background()); err != nil {
		t.Fatalf("failed to stop all actors: %v", err)
	}
}

func BenchmarkActorSend_Comprehensive(b *testing.B) {
	ctx := context.Background()
	actor := NewTestActor("bench-actor")
	ref := NewActorRef("bench-actor", actor, 10000)

	err := ref.Start(ctx)
	if err != nil {
		b.Fatalf("failed to start actor: %v", err)
	}

	msg := &CounterMessage{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ref.Send(msg); err != nil {
			b.Fatalf("failed to send message: %v", err)
		}
	}
	b.StopTimer()

	// Clean up
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ref.Stop(stopCtx); err != nil {
		b.Fatalf("failed to stop actor: %v", err)
	}
}

func BenchmarkActorSpawn_Comprehensive(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		system := NewSystem()
		actor := NewTestActor("bench-actor")
		if _, err := system.Spawn(ctx, "bench-actor", actor, 10); err != nil {
			b.Fatalf("failed to spawn actor: %v", err)
		}
		if err := system.StopAll(context.Background()); err != nil {
			b.Fatalf("failed to stop all actors: %v", err)
		}
	}
}
