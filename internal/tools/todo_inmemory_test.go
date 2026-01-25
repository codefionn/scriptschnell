package tools

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/actor"
)

func setupTodoToolForTest() (*TodoTool, *actor.System, context.CancelFunc) {
	actorSystem := actor.NewSystem()
	ctx, cancel := context.WithCancel(context.Background())

	todoActor := NewTodoActor("test-todo")
	todoRef, err := actorSystem.Spawn(ctx, "test-todo", todoActor, 16)
	if err != nil {
		panic(err)
	}

	client := NewTodoActorClient(todoRef)
	tool := NewTodoTool(client)

	return tool, actorSystem, cancel
}

func TestTodoTool_InMemory(t *testing.T) {
	todo, actorSystem, cancel := setupTodoToolForTest()
	defer cancel()
	defer func() {
		if err := actorSystem.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop todo actor system: %v", err)
		}
	}()

	ctx := context.Background()

	// Test adding a todo
	result1 := todo.Execute(ctx, map[string]interface{}{"action": "add", "text": "Test todo"})
	if result1.Error != "" {
		t.Fatalf("Failed to add todo: %s", result1.Error)
	}
	addResult := result1.Result.(map[string]interface{})
	if addResult["id"] != "todo_1" {
		t.Errorf("Expected id 'todo_1', got %v", addResult["id"])
	}

	// Test listing todos
	result2 := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	if result2.Error != "" {
		t.Fatalf("Failed to list todos: %s", result2.Error)
	}
	listResult := result2.Result.(map[string]interface{})
	if listResult["count"] != 1 {
		t.Errorf("Expected count 1, got %v", listResult["count"])
	}

	// Test checking a todo
	result3 := todo.Execute(ctx, map[string]interface{}{"action": "check", "id": "todo_1"})
	if result3.Error != "" {
		t.Fatalf("Failed to check todo: %s", result3.Error)
	}
	checkResult := result3.Result.(map[string]interface{})
	if checkResult["id"] != "todo_1" {
		t.Errorf("Expected id 'todo_1', got %v", checkResult["id"])
	}

	// Verify todo is checked
	result4 := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	if result4.Error != "" {
		t.Fatalf("Failed to list todos after check: %s", result4.Error)
	}
	listResult2 := result4.Result.(map[string]interface{})
	todos := listResult2["todos"].([]*TodoItem)
	if !todos[0].Completed {
		t.Error("Expected todo to be completed")
	}

	// Test deleting a todo
	result5 := todo.Execute(ctx, map[string]interface{}{"action": "delete", "id": "todo_1"})
	if result5.Error != "" {
		t.Fatalf("Failed to delete todo: %s", result5.Error)
	}

	// Verify todo is deleted
	result6 := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	if result6.Error != "" {
		t.Fatalf("Failed to list todos after delete: %s", result6.Error)
	}
	listResult3 := result6.Result.(map[string]interface{})
	if listResult3["count"] != 0 {
		t.Errorf("Expected count 0 after delete, got %v", listResult3["count"])
	}
}

func TestTodoTool_InMemoryNotPersisted(t *testing.T) {
	// Create first instance and add a todo
	todo1, actorSystem1, cancel1 := setupTodoToolForTest()
	ctx := context.Background()
	_ = todo1.Execute(ctx, map[string]interface{}{"action": "add", "text": "Test todo"})

	// Create second instance - should be empty (different actor)
	todo2, actorSystem2, cancel2 := setupTodoToolForTest()
	defer cancel1()
	defer cancel2()
	defer func() {
		if err := actorSystem1.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop first todo actor system: %v", err)
		}
	}()
	defer func() {
		if err := actorSystem2.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop second todo actor system: %v", err)
		}
	}()

	result := todo2.Execute(ctx, map[string]interface{}{"action": "list"})
	listResult := result.Result.(map[string]interface{})
	if listResult["count"] != 0 {
		t.Errorf("Expected new instance to have 0 todos (different actor), got %v", listResult["count"])
	}
}

func TestTodoTool_AddMany(t *testing.T) {
	todo, actorSystem, cancel := setupTodoToolForTest()
	defer cancel()
	defer func() {
		if err := actorSystem.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop todo actor system: %v", err)
		}
	}()

	ctx := context.Background()

	// Test adding multiple todos at once
	result := todo.Execute(ctx, map[string]interface{}{
		"action": "add_many",
		"todos": []interface{}{
			map[string]interface{}{"text": "Task 1", "status": "pending", "priority": "high"},
			map[string]interface{}{"text": "Task 2", "status": "in_progress", "priority": "medium"},
			map[string]interface{}{"text": "Task 3", "status": "completed", "priority": "low"},
		},
	})

	if result.Error != "" {
		t.Fatalf("Failed to add many todos: %s", result.Error)
	}

	addResult := result.Result.(map[string]interface{})
	if addResult["count"] != 3 {
		t.Errorf("Expected count 3, got %v", addResult["count"])
	}

	todos := addResult["todos"].([]map[string]interface{})
	if len(todos) != 3 {
		t.Errorf("Expected 3 todos in result, got %d", len(todos))
	}

	// Verify IDs were assigned
	todo1 := todos[0]
	todo2 := todos[1]
	todo3 := todos[2]

	if todo1["id"] != "todo_1" {
		t.Errorf("Expected first ID to be todo_1, got %v", todo1["id"])
	}
	if todo2["id"] != "todo_2" {
		t.Errorf("Expected second ID to be todo_2, got %v", todo2["id"])
	}
	if todo3["id"] != "todo_3" {
		t.Errorf("Expected third ID to be todo_3, got %v", todo3["id"])
	}

	// Verify properties
	if todo1["text"] != "Task 1" || todo1["status"] != "pending" || todo1["priority"] != "high" {
		t.Error("First todo properties don't match")
	}

	// Verify list contains all todos
	listResult := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	if listResult.Error != "" {
		t.Fatalf("Failed to list todos: %s", listResult.Error)
	}
	listData := listResult.Result.(map[string]interface{})
	if listData["count"] != 3 {
		t.Errorf("Expected 3 todos in list, got %v", listData["count"])
	}
}

func TestTodoTool_AddManyWithHierarchy(t *testing.T) {
	todo, actorSystem, cancel := setupTodoToolForTest()
	defer cancel()
	defer func() {
		if err := actorSystem.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop todo actor system: %v", err)
		}
	}()

	ctx := context.Background()

	// Test adding hierarchical todos using array indices
	result := todo.Execute(ctx, map[string]interface{}{
		"action": "add_many",
		"todos": []interface{}{
			map[string]interface{}{"text": "Parent task", "priority": "high"},
			map[string]interface{}{"text": "Sub-task 1", "parent_id": "0"},
			map[string]interface{}{"text": "Sub-task 2", "parent_id": "0"},
			map[string]interface{}{"text": "Nested sub-task", "parent_id": "1"},
		},
	})

	if result.Error != "" {
		t.Fatalf("Failed to add many todos: %s", result.Error)
	}

	addResult := result.Result.(map[string]interface{})
	todos := addResult["todos"].([]map[string]interface{})

	// Verify hierarchy
	todo0 := todos[0]
	todo1 := todos[1]
	todo2 := todos[2]
	todo3 := todos[3]

	if todo0["parent_id"] != "" {
		t.Errorf("Expected first todo to have no parent, got %v", todo0["parent_id"])
	}
	if todo1["parent_id"] != "todo_1" {
		t.Errorf("Expected second todo to have parent todo_1, got %v", todo1["parent_id"])
	}
	if todo2["parent_id"] != "todo_1" {
		t.Errorf("Expected third todo to have parent todo_1, got %v", todo2["parent_id"])
	}
	if todo3["parent_id"] != "todo_2" {
		t.Errorf("Expected fourth todo to have parent todo_2, got %v", todo3["parent_id"])
	}
}

func TestTodoTool_AddManyEmpty(t *testing.T) {
	todo, actorSystem, cancel := setupTodoToolForTest()
	defer cancel()
	defer func() {
		if err := actorSystem.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop todo actor system: %v", err)
		}
	}()

	ctx := context.Background()

	// Test with empty array
	result := todo.Execute(ctx, map[string]interface{}{
		"action": "add_many",
		"todos":  []interface{}{},
	})

	if result.Error == "" {
		t.Error("Expected error for empty todos array")
	}
}

func TestTodoTool_AddManyMissingTodos(t *testing.T) {
	todo, actorSystem, cancel := setupTodoToolForTest()
	defer cancel()
	defer func() {
		if err := actorSystem.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop todo actor system: %v", err)
		}
	}()

	ctx := context.Background()

	// Test without todos parameter
	result := todo.Execute(ctx, map[string]interface{}{
		"action": "add_many",
	})

	if result.Error == "" {
		t.Error("Expected error when todos parameter is missing")
	}
}

func TestTodoTool_AddManyInvalidParentIndex(t *testing.T) {
	todo, actorSystem, cancel := setupTodoToolForTest()
	defer cancel()
	defer func() {
		if err := actorSystem.StopAll(context.Background()); err != nil {
			t.Errorf("failed to stop todo actor system: %v", err)
		}
	}()

	ctx := context.Background()

	// Test with invalid parent index (forward reference)
	result := todo.Execute(ctx, map[string]interface{}{
		"action": "add_many",
		"todos": []interface{}{
			map[string]interface{}{"text": "Task 1"},
			map[string]interface{}{"text": "Task 2", "parent_id": "2"}, // Invalid - index 2 doesn't exist
		},
	})

	if result.Error == "" {
		t.Error("Expected error for invalid parent index")
	}

	// Verify no todos were added (atomic operation)
	listResult := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	listData := listResult.Result.(map[string]interface{})
	if listData["count"] != 0 {
		t.Errorf("Expected 0 todos after failed atomic operation, got %v", listData["count"])
	}
}
