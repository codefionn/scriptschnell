package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/statcode-ai/scriptschnell/internal/actor"
)

// TodoActorInterface defines the interface for todo actors
type TodoActorInterface interface {
	actor.Actor
	SetChangeCallback(callback TodoChangeCallback)
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
		Status       string
		Priority     string
		ResponseChan chan *TodoItem
	}

	// TodoCheckMsg marks a todo as checked/unchecked
	TodoCheckMsg struct {
		ID           string
		Checked      bool
		ResponseChan chan error
	}

	// TodoSetStatusMsg sets the status of a todo
	TodoSetStatusMsg struct {
		ID           string
		Status       string
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
func (m TodoListMsg) Type() string      { return "TodoListMsg" }
func (m TodoAddMsg) Type() string       { return "TodoAddMsg" }
func (m TodoCheckMsg) Type() string     { return "TodoCheckMsg" }
func (m TodoSetStatusMsg) Type() string { return "TodoSetStatusMsg" }
func (m TodoDeleteMsg) Type() string    { return "TodoDeleteMsg" }
func (m TodoClearMsg) Type() string     { return "TodoClearMsg" }

// TodoChangeCallback is called when todos are modified
type TodoChangeCallback func(todos *TodoList)

// TodoActor manages todo items as an actor
type TodoActor struct {
	name     string
	todos    *TodoList
	mu       sync.RWMutex
	onChange TodoChangeCallback
}

// NewTodoActor creates a new TodoActor
func NewTodoActor(name string) *TodoActor {
	return &TodoActor{
		name:  name,
		todos: &TodoList{Items: make([]*TodoItem, 0)},
	}
}

// SetChangeCallback sets the callback function to be called when todos change
func (a *TodoActor) SetChangeCallback(callback TodoChangeCallback) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onChange = callback
}

// notifyChange calls the onChange callback if set (must be called with mutex unlocked)
func (a *TodoActor) notifyChange() {
	a.mu.RLock()
	callback := a.onChange
	items := make([]*TodoItem, len(a.todos.Items))
	copy(items, a.todos.Items)
	a.mu.RUnlock()

	if callback != nil {
		callback(&TodoList{Items: items})
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

		// Default values
		status := m.Status
		if status == "" {
			status = "pending"
		}
		priority := m.Priority
		if priority == "" {
			priority = "medium"
		}

		id := nextTodoID(a.todos)
		item := &TodoItem{
			ID:        id,
			Text:      m.Text,
			Completed: status == "completed",
			Status:    status,
			Priority:  priority,
			Created:   m.Timestamp,
			ParentID:  m.ParentID,
		}
		a.todos.Items = append(a.todos.Items, item)
		a.mu.Unlock()

		a.notifyChange()
		m.ResponseChan <- item
		return nil

	case TodoCheckMsg:
		a.mu.Lock()
		found := false
		for _, item := range a.todos.Items {
			if item.ID == m.ID {
				item.Completed = m.Checked
				// Update status based on checked state
				if m.Checked {
					item.Status = "completed"
				} else if item.Status == "completed" {
					item.Status = "pending"
				}
				found = true
				break
			}
		}
		a.mu.Unlock()

		if found {
			a.notifyChange()
			m.ResponseChan <- nil
		} else {
			m.ResponseChan <- fmt.Errorf("todo not found: %s", m.ID)
		}
		return nil

	case TodoSetStatusMsg:
		a.mu.Lock()
		found := false
		for _, item := range a.todos.Items {
			if item.ID == m.ID {
				item.Status = m.Status
				// Sync the Completed field with status for backward compatibility
				item.Completed = (m.Status == "completed")
				found = true
				break
			}
		}
		a.mu.Unlock()

		if found {
			a.notifyChange()
			m.ResponseChan <- nil
		} else {
			m.ResponseChan <- fmt.Errorf("todo not found: %s", m.ID)
		}
		return nil

	case TodoDeleteMsg:
		a.mu.Lock()
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

		if found {
			// Recursively delete all sub-todos
			a.deleteSubTodosRecursive(m.ID)
		}
		a.mu.Unlock()

		if found {
			a.notifyChange()
			m.ResponseChan <- nil
		} else {
			m.ResponseChan <- fmt.Errorf("todo not found: %s", m.ID)
		}
		return nil

	case TodoClearMsg:
		a.mu.Lock()
		a.todos = &TodoList{Items: make([]*TodoItem, 0)}
		a.mu.Unlock()

		a.notifyChange()
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

// Add adds a new todo with status and priority
func (c *TodoActorClient) Add(text string, timestamp string, parentID string, status string, priority string) (*TodoItem, error) {
	respChan := make(chan *TodoItem, 1)
	if err := c.actorRef.Send(TodoAddMsg{
		Text:         text,
		Timestamp:    timestamp,
		ParentID:     parentID,
		Status:       status,
		Priority:     priority,
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

// SetStatus sets the status of a todo
func (c *TodoActorClient) SetStatus(id string, status string) error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(TodoSetStatusMsg{
		ID:           id,
		Status:       status,
		ResponseChan: respChan,
	}); err != nil {
		return err
	}
	return <-respChan
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

// calculateItemLevel calculates the hierarchical level of a todo item
func calculateItemLevel(item *TodoItem, allItems []*TodoItem) int {
	if item.ParentID == "" {
		return 0 // Top-level item
	}

	// Find parent and calculate its level
	for _, parent := range allItems {
		if parent.ID == item.ParentID {
			return calculateItemLevel(parent, allItems) + 1
		}
	}

	return 0 // Parent not found, treat as top-level
}

// FormatTodoPlanAsText formats the todos as a readable plan text for display
func FormatTodoPlanAsText(todos *TodoList) string {
	if todos == nil || len(todos.Items) == 0 {
		return "📋 **Current Plan**: No tasks defined"
	}

	items := todos.Items

	// Create a map to track item levels for hierarchy
	itemLevels := make(map[string]int)
	for _, item := range items {
		itemLevels[item.ID] = calculateItemLevel(item, items)
	}

	var builder strings.Builder
	builder.WriteString("📋 **Current Plan**:\n\n")

	// Group items by status
	pendingItems := []*TodoItem{}
	inProgressItems := []*TodoItem{}
	completedItems := []*TodoItem{}

	for _, item := range items {
		status := item.Status
		if status == "" {
			status = "pending"
			if item.Completed {
				status = "completed"
			}
		}

		switch status {
		case "completed":
			completedItems = append(completedItems, item)
		case "in_progress":
			inProgressItems = append(inProgressItems, item)
		default: // "pending"
			pendingItems = append(pendingItems, item)
		}
	}

	// Show pending items
	if len(pendingItems) > 0 {
		builder.WriteString("**⏳ Pending:**\n")
		for _, item := range pendingItems {
			level := itemLevels[item.ID]
			indent := ""
			for i := 0; i < level; i++ {
				indent += "  "
			}
			builder.WriteString(fmt.Sprintf("%s• %s\n", indent, item.Text))
		}
		builder.WriteString("\n")
	}

	// Show in-progress items
	if len(inProgressItems) > 0 {
		builder.WriteString("**🔄 In Progress:**\n")
		for _, item := range inProgressItems {
			level := itemLevels[item.ID]
			indent := ""
			for i := 0; i < level; i++ {
				indent += "  "
			}
			builder.WriteString(fmt.Sprintf("%s▶ %s\n", indent, item.Text))
		}
		builder.WriteString("\n")
	}

	// Show completed items
	if len(completedItems) > 0 {
		builder.WriteString("**✅ Completed:**\n")
		for _, item := range completedItems {
			level := itemLevels[item.ID]
			indent := ""
			for i := 0; i < level; i++ {
				indent += "  "
			}
			builder.WriteString(fmt.Sprintf("%s✓ %s\n", indent, item.Text))
		}
	}

	return builder.String()
}
