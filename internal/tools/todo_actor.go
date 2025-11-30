package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/statcode-ai/statcode-ai/internal/actor"
)

// TodoActorInterface defines the interface for todo actors
type TodoActorInterface interface {
	actor.Actor
	SendSetConnectionMsg(conn interface{}, sessionID string) error
}

// TodoMessage types for actor communication
type (
	// TodoListMsg requests the current list of todos
	TodoListMsg struct {
		ResponseChan chan *TodoList
	}

	// TodoAddMsg adds a new todo
	TodoAddMsg struct {
		Text         string
		Timestamp    string
		ParentID     string
		ResponseChan chan *TodoItem
	}

	// TodoCheckMsg marks a todo as checked/unchecked
	TodoCheckMsg struct {
		ID           string
		Checked      bool
		ResponseChan chan error
	}

	// TodoDeleteMsg deletes a todo
	TodoDeleteMsg struct {
		ID           string
		ResponseChan chan error
	}

	// TodoClearMsg clears all todos
	TodoClearMsg struct {
		ResponseChan chan error
	}
)

// Implement actor.Message interface for all message types
func (m TodoListMsg) Type() string   { return "TodoListMsg" }
func (m TodoAddMsg) Type() string    { return "TodoAddMsg" }
func (m TodoCheckMsg) Type() string  { return "TodoCheckMsg" }
func (m TodoDeleteMsg) Type() string { return "TodoDeleteMsg" }
func (m TodoClearMsg) Type() string  { return "TodoClearMsg" }

// TodoActor manages todo items as an actor
type TodoActor struct {
	name  string
	todos *TodoList
	mu    sync.RWMutex
}

// NewTodoActor creates a new TodoActor
func NewTodoActor(name string) *TodoActor {
	return &TodoActor{
		name:  name,
		todos: &TodoList{Items: make([]*TodoItem, 0)},
	}
}

// ID returns the actor's unique identifier
func (a *TodoActor) ID() string {
	return a.name
}

// Start initializes the actor
func (a *TodoActor) Start(ctx context.Context) error {
	return nil
}

// Stop stops the actor gracefully
func (a *TodoActor) Stop(ctx context.Context) error {
	return nil
}

// SendSetConnectionMsg is a no-op for the regular todo actor (ACP specific)
func (a *TodoActor) SendSetConnectionMsg(conn interface{}, sessionID string) error {
	// Regular todo actor doesn't support ACP connections
	return nil
}

// Receive handles incoming messages
func (a *TodoActor) Receive(ctx context.Context, msg actor.Message) error {
	switch m := msg.(type) {
	case TodoListMsg:
		a.mu.RLock()
		// Create a copy of the todo list to avoid concurrent access issues
		items := make([]*TodoItem, len(a.todos.Items))
		copy(items, a.todos.Items)
		a.mu.RUnlock()

		m.ResponseChan <- &TodoList{Items: items}
		return nil

	case TodoAddMsg:
		a.mu.Lock()

		// Validate parent exists if parentID is provided
		if m.ParentID != "" {
			parentExists := false
			for _, item := range a.todos.Items {
				if item.ID == m.ParentID {
					parentExists = true
					break
				}
			}
			if !parentExists {
				a.mu.Unlock()
				m.ResponseChan <- nil
				return fmt.Errorf("parent todo not found: %s", m.ParentID)
			}
		}

		id := nextTodoID(a.todos)
		item := &TodoItem{
			ID:        id,
			Text:      m.Text,
			Completed: false,
			Created:   m.Timestamp,
			ParentID:  m.ParentID,
		}
		a.todos.Items = append(a.todos.Items, item)
		a.mu.Unlock()

		m.ResponseChan <- item
		return nil

	case TodoCheckMsg:
		a.mu.Lock()
		defer a.mu.Unlock()

		for _, item := range a.todos.Items {
			if item.ID == m.ID {
				item.Completed = m.Checked
				m.ResponseChan <- nil
				return nil
			}
		}

		m.ResponseChan <- fmt.Errorf("todo not found: %s", m.ID)
		return nil

	case TodoDeleteMsg:
		a.mu.Lock()
		defer a.mu.Unlock()

		// Find the todo to delete
		found := false
		for i, item := range a.todos.Items {
			if item.ID == m.ID {
				found = true
				// Remove the todo
				a.todos.Items = append(a.todos.Items[:i], a.todos.Items[i+1:]...)
				break
			}
		}

		if !found {
			m.ResponseChan <- fmt.Errorf("todo not found: %s", m.ID)
			return nil
		}

		// Recursively delete all sub-todos
		a.deleteSubTodosRecursive(m.ID)

		m.ResponseChan <- nil
		return nil

	case TodoClearMsg:
		a.mu.Lock()
		a.todos = &TodoList{Items: make([]*TodoItem, 0)}
		a.mu.Unlock()

		m.ResponseChan <- nil
		return nil

	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

// deleteSubTodosRecursive deletes all sub-todos of the given parent ID recursively
// Must be called with mutex locked
func (a *TodoActor) deleteSubTodosRecursive(parentID string) {
	// Find all direct children
	var childrenToDelete []string
	for _, item := range a.todos.Items {
		if item.ParentID == parentID {
			childrenToDelete = append(childrenToDelete, item.ID)
		}
	}

	// Delete each child and their sub-todos
	for _, childID := range childrenToDelete {
		// First recursively delete this child's children
		a.deleteSubTodosRecursive(childID)

		// Then delete this child
		for i, item := range a.todos.Items {
			if item.ID == childID {
				a.todos.Items = append(a.todos.Items[:i], a.todos.Items[i+1:]...)
				break
			}
		}
	}
}

// TodoActorClient provides a convenient interface to interact with TodoActor
type TodoActorClient struct {
	actorRef interface {
		Send(msg actor.Message) error
	}
}

// NewTodoActorClient creates a new client for interacting with TodoActor
func NewTodoActorClient(actorRef interface{ Send(msg actor.Message) error }) *TodoActorClient {
	return &TodoActorClient{actorRef: actorRef}
}

// List returns the current list of todos
func (c *TodoActorClient) List() (*TodoList, error) {
	respChan := make(chan *TodoList, 1)
	if err := c.actorRef.Send(TodoListMsg{ResponseChan: respChan}); err != nil {
		return nil, err
	}
	return <-respChan, nil
}

// Add adds a new todo
func (c *TodoActorClient) Add(text string, timestamp string, parentID string) (*TodoItem, error) {
	respChan := make(chan *TodoItem, 1)
	if err := c.actorRef.Send(TodoAddMsg{
		Text:         text,
		Timestamp:    timestamp,
		ParentID:     parentID,
		ResponseChan: respChan,
	}); err != nil {
		return nil, err
	}
	item := <-respChan
	if item == nil {
		return nil, fmt.Errorf("failed to add todo")
	}
	return item, nil
}

// Check marks a todo as checked or unchecked
func (c *TodoActorClient) Check(id string, checked bool) error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(TodoCheckMsg{
		ID:           id,
		Checked:      checked,
		ResponseChan: respChan,
	}); err != nil {
		return err
	}
	return <-respChan
}

// Delete removes a todo
func (c *TodoActorClient) Delete(id string) error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(TodoDeleteMsg{
		ID:           id,
		ResponseChan: respChan,
	}); err != nil {
		return err
	}
	return <-respChan
}

// Clear removes all todos
func (c *TodoActorClient) Clear() error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(TodoClearMsg{ResponseChan: respChan}); err != nil {
		return err
	}
	return <-respChan
}
