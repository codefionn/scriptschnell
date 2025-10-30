package actor

import (
	"context"
	"fmt"
	"sync"
)

// Message represents a message sent between actors
type Message interface {
	Type() string
}

// Actor represents an actor in the actor model
type Actor interface {
	// Receive processes incoming messages
	Receive(ctx context.Context, msg Message) error
	// Start starts the actor
	Start(ctx context.Context) error
	// Stop stops the actor gracefully
	Stop(ctx context.Context) error
	// ID returns the actor's unique identifier
	ID() string
}

// ActorRef is a reference to an actor for sending messages
type ActorRef struct {
	id      string
	mailbox chan Message
	actor   Actor
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	mu      sync.RWMutex
	stopped bool
}

// NewActorRef creates a new actor reference
func NewActorRef(id string, actor Actor, mailboxSize int) *ActorRef {
	return &ActorRef{
		id:      id,
		actor:   actor,
		mailbox: make(chan Message, mailboxSize),
	}
}

// ID returns the actor's ID
func (ref *ActorRef) ID() string {
	return ref.id
}

// Send sends a message to the actor (non-blocking)
func (ref *ActorRef) Send(msg Message) error {
	ref.mu.RLock()
	defer ref.mu.RUnlock()

	if ref.stopped {
		return fmt.Errorf("actor %s is stopped", ref.id)
	}

	select {
	case ref.mailbox <- msg:
		return nil
	default:
		return fmt.Errorf("actor %s mailbox is full", ref.id)
	}
}

// Start starts the actor's message processing loop
func (ref *ActorRef) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	ref.cancel = cancel

	if err := ref.actor.Start(ctx); err != nil {
		cancel()
		return err
	}

	ref.wg.Add(1)
	go ref.run(ctx)
	return nil
}

// Stop stops the actor gracefully
func (ref *ActorRef) Stop(ctx context.Context) error {
	ref.mu.Lock()
	if ref.stopped {
		ref.mu.Unlock()
		return nil
	}
	ref.stopped = true
	ref.mu.Unlock()

	if ref.cancel != nil {
		ref.cancel()
	}

	// Wait for actor to finish processing
	done := make(chan struct{})
	go func() {
		ref.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return ref.actor.Stop(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run is the actor's main message processing loop
func (ref *ActorRef) run(ctx context.Context) {
	defer ref.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ref.mailbox:
			if err := ref.actor.Receive(ctx, msg); err != nil {
				// Log error but continue processing
				fmt.Printf("Actor %s error processing message: %v\n", ref.id, err)
			}
		}
	}
}

// System manages a collection of actors
type System struct {
	actors map[string]*ActorRef
	mu     sync.RWMutex
}

// NewSystem creates a new actor system
func NewSystem() *System {
	return &System{
		actors: make(map[string]*ActorRef),
	}
}

// Spawn creates and starts a new actor
func (s *System) Spawn(ctx context.Context, id string, actor Actor, mailboxSize int) (*ActorRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.actors[id]; exists {
		return nil, fmt.Errorf("actor with id %s already exists", id)
	}

	ref := NewActorRef(id, actor, mailboxSize)
	if err := ref.Start(ctx); err != nil {
		return nil, err
	}

	s.actors[id] = ref
	return ref, nil
}

// Get retrieves an actor reference by ID
func (s *System) Get(id string) (*ActorRef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref, ok := s.actors[id]
	return ref, ok
}

// Stop stops an actor by ID
func (s *System) Stop(ctx context.Context, id string) error {
	s.mu.Lock()
	ref, exists := s.actors[id]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("actor %s not found", id)
	}
	delete(s.actors, id)
	s.mu.Unlock()

	return ref.Stop(ctx)
}

// StopAll stops all actors in the system
func (s *System) StopAll(ctx context.Context) error {
	s.mu.Lock()
	actors := make([]*ActorRef, 0, len(s.actors))
	for _, ref := range s.actors {
		actors = append(actors, ref)
	}
	s.actors = make(map[string]*ActorRef)
	s.mu.Unlock()

	var firstErr error
	for _, ref := range actors {
		if err := ref.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
