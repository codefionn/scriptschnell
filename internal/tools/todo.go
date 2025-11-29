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
	Completed bool   `json:"completed"`
	Created   string `json:"created"`
	ParentID  string `json:"parent_id,omitempty"` // Empty string means top-level todo
}

// TodoList represents a list of todos
type TodoList struct {
	Items []*TodoItem `json:"items"`
}

// TodoTool manages todos via a TodoActor
type TodoTool struct {
	client *TodoActorClient
}

func NewTodoTool(client *TodoActorClient) *TodoTool {
	return &TodoTool{
		client: client,
	}
}

func (t *TodoTool) Name() string {
	return ToolNameTodo
}

func (t *TodoTool) Description() string {
	return "Manage todo items with hierarchical sub-todos. Supports reading, writing, checking/unchecking todos, and adding sub-todos to parent tasks."
}

func (t *TodoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: 'list', 'add', 'check', 'uncheck', 'delete'",
				"enum":        []string{"list", "add", "check", "uncheck", "delete"},
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Todo text (for 'add' action)",
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Todo ID (for 'check', 'uncheck', 'delete' actions)",
			},
			"parent_id": map[string]interface{}{
				"type":        "string",
				"description": "Parent todo ID (for 'add' action, optional - creates a sub-todo under the specified parent)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *TodoTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	action := GetStringParam(params, "action", "")
	if action == "" {
		return &ToolResult{Error: fmt.Sprintf("action is required")}
	}

	switch action {
	case "list":
		return &ToolResult{Result: t.listTodos()}

	case "add":
		text := GetStringParam(params, "text", "")
		if text == "" {
			return &ToolResult{Error: fmt.Sprintf("text is required for add action")}
		}
		parentID := GetStringParam(params, "parent_id", "")
		result, err := t.addTodo(ctx, text, parentID)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "check":
		id := GetStringParam(params, "id", "")
		if id == "" {
			return &ToolResult{Error: fmt.Sprintf("id is required for check action")}
		}
		result, err := t.checkTodo(id, true)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "uncheck":
		id := GetStringParam(params, "id", "")
		if id == "" {
			return &ToolResult{Error: fmt.Sprintf("id is required for uncheck action")}
		}
		result, err := t.checkTodo(id, false)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}

	case "delete":
		id := GetStringParam(params, "id", "")
		if id == "" {
			return &ToolResult{Error: fmt.Sprintf("id is required for delete action")}
		}
		result, err := t.deleteTodo(id)
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

func (t *TodoTool) addTodo(ctx context.Context, text string, parentID string) (interface{}, error) {
	timestamp := todoTimestamp(ctx)
	item, err := t.client.Add(text, timestamp, parentID)
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
		"message":   message,
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
