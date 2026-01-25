package tools

import (
	"context"
	"fmt"
	"time"
)

// TodoItem represents a todo item
type TodoItem struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Completed bool   `json:"completed"` // Deprecated: use Status instead
	Status    string `json:"status"`    // "pending", "in_progress", "completed"
	Priority  string `json:"priority"`  // "high", "medium", "low"
	Created   string `json:"created"`
	ParentID  string `json:"parent_id,omitempty"` // Empty string means top-level todo
}

// TodoList represents a list of todos
type TodoList struct {
	Items []*TodoItem `json:"items"`
}

// TodoToolSpec is the static specification for the todo tool
type TodoToolSpec struct{}

func (s *TodoToolSpec) Name() string {
	return ToolNameTodo
}

func (s *TodoToolSpec) Description() string {
	return "Manage todo items with hierarchical sub-todos. Supports reading, writing, checking/unchecking todos, adding sub-todos to parent tasks, and batch adding multiple todos at once. Each todo has a status (pending/in_progress/completed) and priority (high/medium/low)."
}

func (s *TodoToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: 'list', 'add', 'add_many', 'check', 'uncheck', 'delete', 'clear', 'set_status'",
				"enum":        []string{"list", "add", "add_many", "check", "uncheck", "delete", "clear", "set_status"},
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Todo text (for 'add' action)",
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Todo ID (for 'check', 'uncheck', 'delete', 'set_status' actions)",
			},
			"parent_id": map[string]interface{}{
				"type":        "string",
				"description": "Parent todo ID (for 'add' action, optional - creates a sub-todo under the specified parent)",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "Status of the todo (for 'add' and 'set_status' actions): 'pending', 'in_progress', 'completed'",
				"enum":        []string{"pending", "in_progress", "completed"},
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "Priority of the todo (for 'add' and 'add_many' actions): 'high', 'medium', 'low'. Defaults to 'medium' if not specified.",
				"enum":        []string{"high", "medium", "low"},
			},
			"todos": map[string]interface{}{
				"type":        "array",
				"description": "Array of todo items (for 'add_many' action). Each item can have: text (required), parent_id (optional, use array index like '0', '1' to reference other items in same batch), status (optional, defaults to 'pending'), priority (optional, defaults to 'medium').",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{
							"type":        "string",
							"description": "Todo text (required)",
						},
						"parent_id": map[string]interface{}{
							"type":        "string",
							"description": "Parent todo reference as array index (e.g., '0', '1') to create sub-todos within the same batch, or an existing todo ID",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Status of the todo: 'pending', 'in_progress', 'completed'. Defaults to 'pending'.",
							"enum":        []string{"pending", "in_progress", "completed"},
						},
						"priority": map[string]interface{}{
							"type":        "string",
							"description": "Priority of the todo: 'high', 'medium', 'low'. Defaults to 'medium'.",
							"enum":        []string{"high", "medium", "low"},
						},
					},
					"required": []string{"text"},
				},
			},
		},
		"required": []string{"action"},
	}
}

// TodoTool is the executor with runtime dependencies
type TodoTool struct {
	client *TodoActorClient
}

func NewTodoTool(client *TodoActorClient) *TodoTool {
	return &TodoTool{
		client: client,
	}
}

// Legacy interface implementation for backward compatibility
func (t *TodoTool) Name() string        { return ToolNameTodo }
func (t *TodoTool) Description() string { return (&TodoToolSpec{}).Description() }
func (t *TodoTool) Parameters() map[string]interface{} {
	return (&TodoToolSpec{}).Parameters()
}

func (t *TodoTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	action := GetStringParam(params, "action", "")
	if action == "" {
		return &ToolResult{Error: "action is required"}
	}

	switch action {
	case "list":
		return &ToolResult{Result: t.listTodos()}

	case "add":
		text := GetStringParam(params, "text", "")
		if text == "" {
			return &ToolResult{Error: "text is required for add action"}
		}
		parentID := GetStringParam(params, "parent_id", "")
		status := GetStringParam(params, "status", "pending")
		priority := GetStringParam(params, "priority", "medium")
		result, err := t.addTodo(ctx, text, parentID, status, priority)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "add_many":
		todosParam, ok := params["todos"]
		if !ok {
			return &ToolResult{Error: "todos array is required for add_many action"}
		}
		result, err := t.addManyTodos(ctx, todosParam)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "check":
		id := GetStringParam(params, "id", "")
		if id == "" {
			return &ToolResult{Error: "id is required for check action"}
		}
		result, err := t.checkTodo(id, true)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "uncheck":
		id := GetStringParam(params, "id", "")
		if id == "" {
			return &ToolResult{Error: "id is required for uncheck action"}
		}
		result, err := t.checkTodo(id, false)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "set_status":
		id := GetStringParam(params, "id", "")
		if id == "" {
			return &ToolResult{Error: "id is required for set_status action"}
		}
		status := GetStringParam(params, "status", "")
		if status == "" {
			return &ToolResult{Error: "status is required for set_status action"}
		}
		if status != "pending" && status != "in_progress" && status != "completed" {
			return &ToolResult{Error: fmt.Sprintf("invalid status: %s (must be 'pending', 'in_progress', or 'completed')", status)}
		}
		result, err := t.setStatus(id, status)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "delete":
		id := GetStringParam(params, "id", "")
		if id == "" {
			return &ToolResult{Error: "id is required for delete action"}
		}
		result, err := t.deleteTodo(id)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "clear":
		result, err := t.clearTodos()
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	default:
		return &ToolResult{Error: fmt.Sprintf("unknown action: %s", action)}
	}
}

func (t *TodoTool) listTodos() interface{} {
	todos, err := t.client.List()
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
			"count": 0,
		}
	}
	return map[string]interface{}{
		"todos": todos.Items,
		"count": len(todos.Items),
	}
}

func (t *TodoTool) addTodo(ctx context.Context, text string, parentID string, status string, priority string) (interface{}, error) {
	timestamp := todoTimestamp(ctx)

	// Validate status
	if status != "pending" && status != "in_progress" && status != "completed" {
		status = "pending" // Default to pending if invalid
	}

	// Validate priority
	if priority != "high" && priority != "medium" && priority != "low" {
		priority = "medium" // Default to medium if invalid
	}

	item, err := t.client.Add(text, timestamp, parentID, status, priority)
	if err != nil {
		return nil, err
	}

	message := "Todo added successfully"
	if parentID != "" {
		message = fmt.Sprintf("Sub-todo added successfully under parent %s", parentID)
	}

	return map[string]interface{}{
		"id":        item.ID,
		"parent_id": item.ParentID,
		"status":    item.Status,
		"priority":  item.Priority,
		"message":   message,
	}, nil
}

func (t *TodoTool) addManyTodos(ctx context.Context, todosParam interface{}) (interface{}, error) {
	todosSlice, ok := todosParam.([]interface{})
	if !ok {
		return nil, fmt.Errorf("todos must be an array")
	}

	if len(todosSlice) == 0 {
		return nil, fmt.Errorf("todos array cannot be empty")
	}

	// Parse all todos first to validate structure
	inputs := make([]TodoInput, len(todosSlice))
	for i, item := range todosSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("todo at index %d must be an object", i)
		}

		text := GetStringParam(itemMap, "text", "")
		if text == "" {
			return nil, fmt.Errorf("todo at index %d must have a text field", i)
		}

		parentID := GetStringParam(itemMap, "parent_id", "")
		status := GetStringParam(itemMap, "status", "pending")
		priority := GetStringParam(itemMap, "priority", "medium")

		// Validate status
		if status != "pending" && status != "in_progress" && status != "completed" {
			status = "pending"
		}

		// Validate priority
		if priority != "high" && priority != "medium" && priority != "low" {
			priority = "medium"
		}

		inputs[i] = TodoInput{
			Text:     text,
			ParentID: parentID,
			Status:   status,
			Priority: priority,
		}
	}

	timestamp := todoTimestamp(ctx)
	items, err := t.client.AddMany(inputs, timestamp)
	if err != nil {
		return nil, err
	}

	// Build response with summary and details
	addedTodos := make([]map[string]interface{}, len(items))
	for i, item := range items {
		addedTodos[i] = map[string]interface{}{
			"id":        item.ID,
			"text":      item.Text,
			"parent_id": item.ParentID,
			"status":    item.Status,
			"priority":  item.Priority,
		}
	}

	return map[string]interface{}{
		"message": fmt.Sprintf("Successfully added %d todos", len(items)),
		"count":   len(items),
		"todos":   addedTodos,
	}, nil
}

func (t *TodoTool) checkTodo(id string, checked bool) (interface{}, error) {
	err := t.client.Check(id, checked)
	if err != nil {
		return nil, err
	}

	status := "checked"
	if !checked {
		status = "unchecked"
	}
	return map[string]interface{}{
		"id":      id,
		"message": fmt.Sprintf("Todo %s successfully", status),
	}, nil
}

func (t *TodoTool) setStatus(id string, status string) (interface{}, error) {
	err := t.client.SetStatus(id, status)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":      id,
		"status":  status,
		"message": fmt.Sprintf("Todo status set to '%s' successfully", status),
	}, nil
}

func (t *TodoTool) deleteTodo(id string) (interface{}, error) {
	err := t.client.Delete(id)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":      id,
		"message": "Todo deleted successfully",
	}, nil
}

func (t *TodoTool) clearTodos() (interface{}, error) {
	if err := t.client.Clear(); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"message": "All todos cleared successfully",
	}, nil
}

func nextTodoID(todos *TodoList) string {
	max := 0
	for _, item := range todos.Items {
		if item == nil {
			continue
		}
		var idx int
		if _, err := fmt.Sscanf(item.ID, "todo_%d", &idx); err == nil {
			if idx > max {
				max = idx
			}
		}
	}
	return fmt.Sprintf("todo_%d", max+1)
}

func todoTimestamp(ctx context.Context) string {
	if ctx == nil {
		return time.Now().Format(time.RFC3339)
	}
	if ts := ctx.Value("timestamp"); ts != nil {
		switch v := ts.(type) {
		case string:
			if v != "" {
				return v
			}
		case time.Time:
			return v.Format(time.RFC3339)
		case fmt.Stringer:
			return v.String()
		}
	}
	return time.Now().Format(time.RFC3339)
}

// NewTodoToolFactory creates a factory for TodoTool
func NewTodoToolFactory(client *TodoActorClient) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewTodoTool(client)
	}
}
