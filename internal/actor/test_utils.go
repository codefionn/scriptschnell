package actor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// TestMessage is a simple test message type
type TestMessage struct {
	ID      string
	Content string
}

func (m *TestMessage) Type() string {
	return "test"
}

// CounterMessage is used for counting operations
type CounterMessage struct{}

func (m *CounterMessage) Type() string {
	return "counter"
}

// ErrorMessage is used to trigger errors
type ErrorMessage struct{}

func (m *ErrorMessage) Type() string {
	return "error"
}

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

	if _, ok := msg.(*ErrorMessage); ok {
		return errors.New("error message received")
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
