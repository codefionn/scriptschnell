package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/statcode-ai/statcode-ai/internal/actor"
	"github.com/statcode-ai/statcode-ai/internal/logger"
)

// ACPTodoMessage types for actor communication with ACP plan support
type (
	// ACPTodoListMsg requests the current list of todos
	ACPTodoListMsg struct {
		ResponseChan chan *TodoList
	}

	// ACPTodoAddMsg adds a new todo and sends plan update via ACP
	ACPTodoAddMsg struct {
		Text         string
		Timestamp    string
		ParentID     string
		Priority     string // high, medium, low
		ResponseChan chan *TodoItem
	}

	// ACPTodoCheckMsg marks a todo as checked/unchecked and sends plan update via ACP
	ACPTodoCheckMsg struct {
		ID           string
		Checked      bool
		ResponseChan chan error
	}

	// ACPTodoDeleteMsg deletes a todo and sends plan update via ACP
	ACPTodoDeleteMsg struct {
		ID           string
		ResponseChan chan error
	}

	// ACPTodoClearMsg clears all todos and sends plan update via ACP
	ACPTodoClearMsg struct {
		ResponseChan chan error
	}

	// ACPTodoSetConnectionMsg sets the ACP connection for plan updates
	ACPTodoSetConnectionMsg struct {
		Conn         *acp.AgentSideConnection
		SessionID    string
		ResponseChan chan error
	}
)

// Implement actor.Message interface for all ACP todo message types
func (m ACPTodoListMsg) Type() string          { return "ACPTodoListMsg" }
func (m ACPTodoAddMsg) Type() string           { return "ACPTodoAddMsg" }
func (m ACPTodoCheckMsg) Type() string         { return "ACPTodoCheckMsg" }
func (m ACPTodoDeleteMsg) Type() string        { return "ACPTodoDeleteMsg" }
func (m ACPTodoClearMsg) Type() string         { return "ACPTodoClearMsg" }
func (m ACPTodoSetConnectionMsg) Type() string { return "ACPTodoSetConnectionMsg" }

// ACPTodoActor manages todo items as an actor with ACP agent plan support
type ACPTodoActor struct {
	name      string
	todos     *TodoList
	mu        sync.RWMutex
	conn      *acp.AgentSideConnection
	sessionID string
	ctx       context.Context
}

// NewACPTodoActor creates a new ACPTodoActor
func NewACPTodoActor(name string) *ACPTodoActor {
	logger.Debug("ACPTodoActor: creating actor %s", name)
	return &ACPTodoActor{
		name:  name,
		todos: &TodoList{Items: make([]*TodoItem, 0)},
	}
}

// ID returns the actor's unique identifier
func (a *ACPTodoActor) ID() string {
	return a.name
}

// Start initializes the actor
func (a *ACPTodoActor) Start(ctx context.Context) error {
	a.ctx = ctx
	return nil
}

// Stop stops the actor gracefully
func (a *ACPTodoActor) Stop(ctx context.Context) error {
	return nil
}

// SendSetConnectionMsg sends the ACP connection to the actor
func (a *ACPTodoActor) SendSetConnectionMsg(conn interface{}, sessionID string) error {
	logger.Debug("ACPTodoActor[%s]: SendSetConnectionMsg session=%s payloadConn=%p", a.name, sessionID, conn)
	if acpConn, ok := conn.(*acp.AgentSideConnection); ok {
		respChan := make(chan error, 1)
		msg := ACPTodoSetConnectionMsg{
			Conn:         acpConn,
			SessionID:    sessionID,
			ResponseChan: respChan,
		}

		// Send the message to ourselves
		err := a.Receive(a.ctx, msg)
		if err != nil {
			logger.Warn("ACPTodoActor[%s]: SendSetConnectionMsg failed: %v", a.name, err)
			return err
		}

		return <-respChan
	}
	return fmt.Errorf("invalid connection type, expected *acp.AgentSideConnection")
}

// Receive handles incoming messages
func (a *ACPTodoActor) Receive(ctx context.Context, msg actor.Message) error {
	switch m := msg.(type) {
	case ACPTodoListMsg:
		logger.Debug("ACPTodoActor[%s]: received list request", a.name)
		return a.handleList(m.ResponseChan)

	case TodoListMsg:
		logger.Debug("ACPTodoActor[%s]: received legacy list request", a.name)
		return a.handleList(m.ResponseChan)

	case ACPTodoAddMsg:
		logger.Debug("ACPTodoActor[%s]: add todo text=%q parent=%s", a.name, m.Text, m.ParentID)
		return a.handleAdd(m.Text, m.Timestamp, m.ParentID, m.Priority, m.ResponseChan)

	case TodoAddMsg:
		logger.Debug("ACPTodoActor[%s]: add todo text=%q parent=%s (legacy)", a.name, m.Text, m.ParentID)
		return a.handleAdd(m.Text, m.Timestamp, m.ParentID, "", m.ResponseChan)

	case ACPTodoCheckMsg:
		logger.Debug("ACPTodoActor[%s]: check todo id=%s checked=%t", a.name, m.ID, m.Checked)
		return a.handleCheck(m.ID, m.Checked, m.ResponseChan)

	case TodoCheckMsg:
		logger.Debug("ACPTodoActor[%s]: check todo id=%s checked=%t (legacy)", a.name, m.ID, m.Checked)
		return a.handleCheck(m.ID, m.Checked, m.ResponseChan)

	case ACPTodoDeleteMsg:
		logger.Debug("ACPTodoActor[%s]: delete todo id=%s", a.name, m.ID)
		return a.handleDelete(m.ID, m.ResponseChan)

	case TodoDeleteMsg:
		logger.Debug("ACPTodoActor[%s]: delete todo id=%s (legacy)", a.name, m.ID)
		return a.handleDelete(m.ID, m.ResponseChan)

	case ACPTodoClearMsg:
		logger.Debug("ACPTodoActor[%s]: clear all todos", a.name)
		return a.handleClear(m.ResponseChan)

	case TodoClearMsg:
		logger.Debug("ACPTodoActor[%s]: clear all todos (legacy)", a.name)
		return a.handleClear(m.ResponseChan)

	case ACPTodoSetConnectionMsg:
		logger.Debug("ACPTodoActor[%s]: setting ACP connection session=%s", a.name, m.SessionID)
		a.mu.Lock()
		a.conn = m.Conn
		a.sessionID = m.SessionID
		a.mu.Unlock()

		// Send initial plan update when connection is set
		a.sendPlanUpdate()

		m.ResponseChan <- nil
		return nil

	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

func (a *ACPTodoActor) handleList(resp chan<- *TodoList) error {
	if resp == nil {
		return fmt.Errorf("response channel is nil")
	}

	a.mu.RLock()
	items := make([]*TodoItem, len(a.todos.Items))
	copy(items, a.todos.Items)
	a.mu.RUnlock()

	resp <- &TodoList{Items: items}
	return nil
}

func (a *ACPTodoActor) handleAdd(text, timestamp, parentID, priority string, resp chan<- *TodoItem) error {
	if resp == nil {
		return fmt.Errorf("response channel is nil")
	}

	a.mu.Lock()

	if parentID != "" {
		parentExists := false
		for _, item := range a.todos.Items {
			if item.ID == parentID {
				parentExists = true
				break
			}
		}
		if !parentExists {
			a.mu.Unlock()
			resp <- nil
			return fmt.Errorf("parent todo not found: %s", parentID)
		}
	}

	id := nextTodoID(a.todos)
	item := &TodoItem{
		ID:        id,
		Text:      text,
		Completed: false,
		Created:   timestamp,
		ParentID:  parentID,
	}
	a.todos.Items = append(a.todos.Items, item)
	a.mu.Unlock()

	logger.Debug("ACPTodoActor[%s]: added todo id=%s", a.name, id)
	a.sendPlanUpdate()

	resp <- item
	return nil
}

func (a *ACPTodoActor) handleCheck(id string, checked bool, resp chan<- error) error {
	if resp == nil {
		return fmt.Errorf("response channel is nil")
	}

	a.mu.Lock()
	found := false
	for _, item := range a.todos.Items {
		if item.ID == id {
			item.Completed = checked
			found = true
			break
		}
	}
	a.mu.Unlock()

	if !found {
		logger.Warn("ACPTodoActor[%s]: check failed, todo not found: %s", a.name, id)
		resp <- fmt.Errorf("todo not found: %s", id)
		return nil
	}

	a.sendPlanUpdate()
	resp <- nil
	return nil
}

func (a *ACPTodoActor) handleDelete(id string, resp chan<- error) error {
	if resp == nil {
		return fmt.Errorf("response channel is nil")
	}

	a.mu.Lock()
	found := false
	for i, item := range a.todos.Items {
		if item.ID == id {
			found = true
			a.todos.Items = append(a.todos.Items[:i], a.todos.Items[i+1:]...)
			break
		}
	}

	if found {
		a.deleteSubTodosRecursive(id)
	}
	a.mu.Unlock()

	if !found {
		logger.Warn("ACPTodoActor[%s]: delete failed, todo not found: %s", a.name, id)
		resp <- fmt.Errorf("todo not found: %s", id)
		return nil
	}

	a.sendPlanUpdate()
	resp <- nil
	return nil
}

func (a *ACPTodoActor) handleClear(resp chan<- error) error {
	if resp == nil {
		return fmt.Errorf("response channel is nil")
	}

	a.mu.Lock()
	a.todos = &TodoList{Items: make([]*TodoItem, 0)}
	a.mu.Unlock()

	a.sendPlanUpdate()
	resp <- nil
	return nil
}

// deleteSubTodosRecursive deletes all sub-todos of the given parent ID recursively
// Must be called with mutex locked
func (a *ACPTodoActor) deleteSubTodosRecursive(parentID string) {
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

// sendPlanUpdate sends the current todo list as an agent plan update via ACP
func (a *ACPTodoActor) sendPlanUpdate() {
	a.mu.RLock()
	conn := a.conn
	sessionID := a.sessionID
	items := make([]*TodoItem, len(a.todos.Items))
	copy(items, a.todos.Items)
	a.mu.RUnlock()

	if conn == nil {
		logger.Debug("ACPTodoActor[%s]: skipping plan update, no ACP connection", a.name)
		// No ACP connection available, skip plan update
		return
	}

	// Convert todo items to plan entries according to the protocol specification
	planEntries := make([]map[string]interface{}, 0, len(items))

	// Create a map to track item levels for hierarchy
	itemLevels := make(map[string]int)

	// First pass: calculate levels for each item
	for _, item := range items {
		level := a.calculateItemLevel(item, items)
		itemLevels[item.ID] = level
	}

	// Second pass: create plan entries with proper hierarchy
	for _, item := range items {
		status := "pending"
		if item.Completed {
			status = "completed"
		}

		// Determine priority based on level and content
		priority := "medium"
		if itemLevels[item.ID] == 0 {
			// Top-level items get higher priority by default
			priority = "high"
		}

		// Add indentation for sub-todos
		displayText := item.Text
		if itemLevels[item.ID] > 0 {
			indent := ""
			for i := 0; i < itemLevels[item.ID]; i++ {
				indent += "  "
			}
			displayText = indent + item.Text
		}

		planEntries = append(planEntries, map[string]interface{}{
			"content":  displayText,
			"priority": priority,
			"status":   status,
		})
	}

	// Try to send the plan update using a text message with plan information
	// Since UpdatePlan might not be available in this SDK version, we'll send
	// the plan as a formatted text message that shows the current status
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Format the plan as a readable text message
	planText := a.formatPlanAsText(items, itemLevels)

	logger.Debug("ACPTodoActor[%s]: sending plan update with %d items", a.name, len(items))
	err := conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sessionID),
		Update:    acp.UpdateAgentMessageText(planText),
	})

	if err != nil {
		logger.Error("Failed to send plan update via ACP: %v", err)
	}
}

// formatPlanAsText formats the current todos as a readable plan text
func (a *ACPTodoActor) formatPlanAsText(items []*TodoItem, itemLevels map[string]int) string {
	if len(items) == 0 {
		return "📋 **Current Plan**: No tasks defined"
	}

	var builder strings.Builder
	builder.WriteString("📋 **Current Plan**:\n\n")

	// Group items by status
	pendingItems := []*TodoItem{}
	completedItems := []*TodoItem{}

	for _, item := range items {
		if item.Completed {
			completedItems = append(completedItems, item)
		} else {
			pendingItems = append(pendingItems, item)
		}
	}

	// Show pending items first
	if len(pendingItems) > 0 {
		builder.WriteString("**🔄 In Progress:**\n")
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

// calculateItemLevel calculates the hierarchical level of a todo item
func (a *ACPTodoActor) calculateItemLevel(item *TodoItem, allItems []*TodoItem) int {
	if item.ParentID == "" {
		return 0 // Top-level item
	}

	// Find parent and calculate its level
	for _, parent := range allItems {
		if parent.ID == item.ParentID {
			return a.calculateItemLevel(parent, allItems) + 1
		}
	}

	return 0 // Parent not found, treat as top-level
}

// ACPTodoActorClient provides a convenient interface to interact with ACPTodoActor
type ACPTodoActorClient struct {
	actorRef interface {
		Send(msg actor.Message) error
	}
}

// NewACPTodoActorClient creates a new client for interacting with ACPTodoActor
func NewACPTodoActorClient(actorRef interface{ Send(msg actor.Message) error }) *ACPTodoActorClient {
	return &ACPTodoActorClient{actorRef: actorRef}
}

// SetACPConnection sets the ACP connection for plan updates
func (c *ACPTodoActorClient) SetACPConnection(conn *acp.AgentSideConnection, sessionID string) error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(ACPTodoSetConnectionMsg{
		Conn:         conn,
		SessionID:    sessionID,
		ResponseChan: respChan,
	}); err != nil {
		return err
	}
	return <-respChan
}

// List returns the current list of todos
func (c *ACPTodoActorClient) List() (*TodoList, error) {
	respChan := make(chan *TodoList, 1)
	if err := c.actorRef.Send(ACPTodoListMsg{ResponseChan: respChan}); err != nil {
		return nil, err
	}
	return <-respChan, nil
}

// Add adds a new todo with priority and sends plan update
func (c *ACPTodoActorClient) Add(text string, timestamp string, parentID, priority string) (*TodoItem, error) {
	respChan := make(chan *TodoItem, 1)
	if err := c.actorRef.Send(ACPTodoAddMsg{
		Text:         text,
		Timestamp:    timestamp,
		ParentID:     parentID,
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

// Check marks a todo as checked or unchecked and sends plan update
func (c *ACPTodoActorClient) Check(id string, checked bool) error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(ACPTodoCheckMsg{
		ID:           id,
		Checked:      checked,
		ResponseChan: respChan,
	}); err != nil {
		return err
	}
	return <-respChan
}

// Delete removes a todo and sends plan update
func (c *ACPTodoActorClient) Delete(id string) error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(ACPTodoDeleteMsg{
		ID:           id,
		ResponseChan: respChan,
	}); err != nil {
		return err
	}
	return <-respChan
}

// Clear removes all todos and sends plan update
func (c *ACPTodoActorClient) Clear() error {
	respChan := make(chan error, 1)
	if err := c.actorRef.Send(ACPTodoClearMsg{ResponseChan: respChan}); err != nil {
		return err
	}
	return <-respChan
}
