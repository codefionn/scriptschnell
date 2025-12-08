package actor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMessage is a simple test message type
type TestMessage struct {
	ID      string
	Content string
}

func (m *TestMessage) Type() string {
	return "test"
}

// TestActor is a simple actor implementation for testing

// TestActor is a simple actor implementation for testing
type TestActor struct {
	id             string
	receivedMsgs   []Message
	receiveCount   atomic.Int32
	startCalled    atomic.Bool
	stopCalled     atomic.Bool
	shouldError    atomic.Bool
	mu             sync.Mutex
	receiveHandler func(ctx context.Context, msg Message) error
}

func NewTestActor(id string) *TestActor {
	return &TestActor{
		id:           id,
		receivedMsgs: make([]Message, 0),
	}
}

func (a *TestActor) ID() string {
	return a.id
}

func (a *TestActor) Start(ctx context.Context) error {
	a.startCalled.Store(true)
	return nil
}

func (a *TestActor) Stop(ctx context.Context) error {
	a.stopCalled.Store(true)
	return nil
}

func (a *TestActor) Receive(ctx context.Context, msg Message) error {
	a.receiveCount.Add(1)

	if a.receiveHandler != nil {
		return a.receiveHandler(ctx, msg)
	}

	a.mu.Lock()
	a.receivedMsgs = append(a.receivedMsgs, msg)
	a.mu.Unlock()

	if a.shouldError.Load() {
		return errors.New("test error")
	}

	return nil
}

func (a *TestActor) GetReceivedMessages() []Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	msgs := make([]Message, len(a.receivedMsgs))
	copy(msgs, a.receivedMsgs)
	return msgs
}

func (a *TestActor) GetReceiveCount() int32 {
	return a.receiveCount.Load()
}

func (a *TestActor) WasStartCalled() bool {
	return a.startCalled.Load()
}

func (a *TestActor) WasStopCalled() bool {
	return a.stopCalled.Load()
}

func (a *TestActor) SetShouldError(val bool) {
	a.shouldError.Store(val)
}

func (a *TestActor) SetReceiveHandler(handler func(ctx context.Context, msg Message) error) {
	a.receiveHandler = handler
}

// Comprehensive tests for ActorRef
func TestActorRefSequentialProcessing(t *testing.T) {
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

func TestActorRefContextCancellation(t *testing.T) {
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

// Comprehensive tests for System
func TestSystemSpawn(t *testing.T) {
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

func TestSystemGet(t *testing.T) {
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

func TestSystemStop(t *testing.T) {
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

func TestSystemStopAll(t *testing.T) {
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

func TestSystemConcurrentAccess(t *testing.T) {
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

func TestActorCommunication(t *testing.T) {
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

// Benchmark tests
func BenchmarkActorSend(b *testing.B) {
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

func BenchmarkActorSpawn(b *testing.B) {
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
