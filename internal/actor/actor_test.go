package actor

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestNewActorRef tests creating a new actor reference
func TestNewActorRef(t *testing.T) {
	actor := NewTestActor("test-1")
	ref := NewActorRef("test-1", actor, 10)

	if ref.ID() != "test-1" {
		t.Errorf("expected ID 'test-1', got '%s'", ref.ID())
	}

	if ref.actor != actor {
		t.Error("actor not set correctly")
	}

	if cap(ref.mailbox) != 10 {
		t.Errorf("expected mailbox size 10, got %d", cap(ref.mailbox))
	}
}

// TestActorRefStartStop tests starting and stopping an actor
func TestActorRefStartStop(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("test-1")
	ref := NewActorRef("test-1", actor, 10)

	// Start the actor
	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	if !actor.WasStartCalled() {
		t.Error("Start() was not called on the actor")
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	err = ref.Stop(stopCtx)
	if err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}

	if !actor.WasStopCalled() {
		t.Error("Stop() was not called on the actor")
	}
}

// TestActorRefSendReceive tests sending and receiving messages
func TestActorRefSendReceive(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("test-1")
	ref := NewActorRef("test-1", actor, 10)

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Send a message
	msg := &TestMessage{ID: "msg-1", Content: "hello"}
	err = ref.Send(msg)
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for message to be processed
	time.Sleep(50 * time.Millisecond)

	// Check message was received
	received := actor.GetReceivedMessages()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}

	testMsg, ok := received[0].(*TestMessage)
	if !ok {
		t.Fatal("received message is not a TestMessage")
	}

	if testMsg.ID != "msg-1" || testMsg.Content != "hello" {
		t.Errorf("message content incorrect: %+v", testMsg)
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := ref.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}
}

// TestActorRefSendMultiple tests sending multiple messages
func TestActorRefSendMultiple(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("test-1")
	ref := NewActorRef("test-1", actor, 100)

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Send multiple messages
	count := 50
	for i := 0; i < count; i++ {
		msg := &CounterMessage{}
		err = ref.Send(msg)
		if err != nil {
			t.Fatalf("failed to send message %d: %v", i, err)
		}
	}

	// Wait for messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Check all messages were received
	if actor.GetReceiveCount() != int32(count) {
		t.Errorf("expected %d messages, got %d", count, actor.GetReceiveCount())
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := ref.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}
}

// TestActorRefSendAfterStop tests sending to a stopped actor
func TestActorRefSendAfterStop(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("test-1")
	ref := NewActorRef("test-1", actor, 10)

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	err = ref.Stop(stopCtx)
	if err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}

	// Try to send a message
	msg := &TestMessage{ID: "msg-1", Content: "hello"}
	err = ref.Send(msg)
	if err == nil {
		t.Error("expected error when sending to stopped actor")
	}
}

// TestActorRefMailboxFull tests behavior when mailbox is full
func TestActorRefMailboxFull(t *testing.T) {
	actor := NewTestActor("test-1")

	// Create small mailbox
	ref := NewActorRef("test-1", actor, 2)

	// Don't start the actor so messages aren't processed
	// Fill the mailbox
	err := ref.Send(&TestMessage{ID: "msg-1"})
	if err != nil {
		t.Fatalf("failed to send first message: %v", err)
	}

	err = ref.Send(&TestMessage{ID: "msg-2"})
	if err != nil {
		t.Fatalf("failed to send second message: %v", err)
	}

	// Try to send when full
	err = ref.Send(&TestMessage{ID: "msg-3"})
	if err == nil {
		t.Error("expected error when mailbox is full")
	}
}

// TestActorRefReceiveError tests error handling in Receive
func TestActorRefReceiveError(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("test-1")
	ref := NewActorRef("test-1", actor, 10)

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Send a message that triggers an error
	msg := &ErrorMessage{}
	err = ref.Send(msg)
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for message to be processed
	time.Sleep(50 * time.Millisecond)

	// Actor should still be running despite error
	normalMsg := &TestMessage{ID: "msg-1", Content: "hello"}
	err = ref.Send(normalMsg)
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

func TestActorRefSequentialProcessing_Variants(t *testing.T) {
	ctx := context.Background()
	actor := NewTestActor("test-1")
	ref := NewActorRef("test-1", actor, 10, WithSequentialProcessing())

	if err := ref.Start(ctx); err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	actor.SetReceiveHandler(func(ctx context.Context, msg Message) error {
		time.Sleep(25 * time.Millisecond)
		return nil
	})

	start := time.Now()
	if err := ref.Send(&TestMessage{ID: "seq", Content: "sync"}); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}
	duration := time.Since(start)

	if duration < 20*time.Millisecond {
		t.Fatalf("expected send to wait for receive (duration=%s)", duration)
	}

	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := ref.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop actor: %v", err)
	}
}

// TestActorRefContextCancellation tests context cancellation during message processing
func TestActorRefContextCancellation_Basic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	actor := NewTestActor("test-1")

	// Set up handler that blocks
	blocker := make(chan struct{})
	actor.SetReceiveHandler(func(ctx context.Context, msg Message) error {
		<-blocker // Block until channel is closed
		return nil
	})

	ref := NewActorRef("test-1", actor, 10)

	err := ref.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start actor: %v", err)
	}

	// Send a message
	err = ref.Send(&TestMessage{ID: "msg-1"})
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Give message time to start processing
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait a bit for actor to stop
	time.Sleep(50 * time.Millisecond)

	// Unblock the handler
	close(blocker)

	// Stop should complete quickly since context is cancelled
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()

	err = ref.Stop(stopCtx)
	if err != nil && err != context.Canceled {
		t.Fatalf("unexpected error during stop: %v", err)
	}
}

// TestSystemSpawn tests spawning actors in the system
func TestSystemSpawn_Basic(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	actor1 := NewTestActor("actor-1")
	ref1, err := system.Spawn(ctx, "actor-1", actor1, 10)
	if err != nil {
		t.Fatalf("failed to spawn actor-1: %v", err)
	}

	if ref1.ID() != "actor-1" {
		t.Errorf("expected ID 'actor-1', got '%s'", ref1.ID())
	}

	if !actor1.WasStartCalled() {
		t.Error("Start() was not called on actor-1")
	}

	// Try to spawn duplicate ID
	actor2 := NewTestActor("actor-1")
	_, err = system.Spawn(ctx, "actor-1", actor2, 10)
	if err == nil {
		t.Error("expected error when spawning duplicate ID")
	}

	// Clean up
	if err := system.StopAll(context.Background()); err != nil {
		t.Fatalf("failed to stop all actors: %v", err)
	}
}

// TestSystemGet tests retrieving actors from the system
func TestSystemGet_Basic(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	actor1 := NewTestActor("actor-1")
	_, err := system.Spawn(ctx, "actor-1", actor1, 10)
	if err != nil {
		t.Fatalf("failed to spawn actor-1: %v", err)
	}

	// Get existing actor
	ref, ok := system.Get("actor-1")
	if !ok {
		t.Error("failed to get actor-1")
	}

	if ref.ID() != "actor-1" {
		t.Errorf("expected ID 'actor-1', got '%s'", ref.ID())
	}

	// Get non-existent actor
	_, ok = system.Get("actor-2")
	if ok {
		t.Error("expected false when getting non-existent actor")
	}

	// Clean up
	if err := system.StopAll(context.Background()); err != nil {
		t.Fatalf("failed to stop all actors: %v", err)
	}
}

// TestSystemStop tests stopping individual actors
func TestSystemStop_Basic(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	actor1 := NewTestActor("actor-1")
	_, err := system.Spawn(ctx, "actor-1", actor1, 10)
	if err != nil {
		t.Fatalf("failed to spawn actor-1: %v", err)
	}

	// Stop the actor
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	err = system.Stop(stopCtx, "actor-1")
	if err != nil {
		t.Fatalf("failed to stop actor-1: %v", err)
	}

	if !actor1.WasStopCalled() {
		t.Error("Stop() was not called on actor-1")
	}

	// Actor should be removed from system
	_, ok := system.Get("actor-1")
	if ok {
		t.Error("actor-1 should be removed from system")
	}

	// Try to stop non-existent actor
	err = system.Stop(stopCtx, "actor-2")
	if err == nil {
		t.Error("expected error when stopping non-existent actor")
	}
}

// TestSystemStopAll tests stopping all actors
func TestSystemStopAll_Basic(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	// Spawn multiple actors
	actors := []*TestActor{
		NewTestActor("actor-1"),
		NewTestActor("actor-2"),
		NewTestActor("actor-3"),
	}

	for _, actor := range actors {
		_, err := system.Spawn(ctx, actor.ID(), actor, 10)
		if err != nil {
			t.Fatalf("failed to spawn %s: %v", actor.ID(), err)
		}
	}

	// Stop all actors
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err := system.StopAll(stopCtx)
	if err != nil {
		t.Fatalf("failed to stop all actors: %v", err)
	}

	// All actors should have Stop() called
	for _, actor := range actors {
		if !actor.WasStopCalled() {
			t.Errorf("Stop() was not called on %s", actor.ID())
		}
	}

	// System should be empty
	for _, actor := range actors {
		_, ok := system.Get(actor.ID())
		if ok {
			t.Errorf("%s should be removed from system", actor.ID())
		}
	}
}

// TestSystemConcurrentAccess tests concurrent access to the system
func TestSystemConcurrentAccess_Basic(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Spawn actors concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			actor := NewTestActor(string(rune('a' + id)))
			_, err := system.Spawn(ctx, string(rune('a'+id)), actor, 10)
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("error during concurrent spawn: %v", err)
	}

	// Clean up
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := system.StopAll(stopCtx); err != nil {
		t.Fatalf("failed to stop all actors: %v", err)
	}
}

// TestActorCommunication tests communication between actors
func TestActorCommunication_Basic(t *testing.T) {
	ctx := context.Background()
	system := NewSystem()

	actor1 := NewTestActor("actor-1")
	actor2 := NewTestActor("actor-2")

	_, err := system.Spawn(ctx, "actor-1", actor1, 10)
	if err != nil {
		t.Fatalf("failed to spawn actor-1: %v", err)
	}

	ref2, err := system.Spawn(ctx, "actor-2", actor2, 10)
	if err != nil {
		t.Fatalf("failed to spawn actor-2: %v", err)
	}

	// Send message from actor-1 to actor-2
	msg := &TestMessage{ID: "msg-1", Content: "hello from actor-1"}
	err = ref2.Send(msg)
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Check actor-2 received the message
	received := actor2.GetReceivedMessages()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}

	testMsg, ok := received[0].(*TestMessage)
	if !ok {
		t.Fatal("received message is not a TestMessage")
	}

	if testMsg.Content != "hello from actor-1" {
		t.Errorf("unexpected message content: %s", testMsg.Content)
	}

	// Clean up
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := system.StopAll(stopCtx); err != nil {
		t.Fatalf("failed to stop all actors: %v", err)
	}
}

// BenchmarkActorSend benchmarks sending messages to an actor
func BenchmarkActorSend_Basic(b *testing.B) {
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

func BenchmarkActorSpawn_Basic(b *testing.B) {
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
