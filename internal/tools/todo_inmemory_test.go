package tools

import (
	"context"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/actor"
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
	result1, err := todo.Execute(ctx, map[string]interface{}{"action": "add", "text": "Test todo"})
	if err != nil {
		t.Fatalf("Failed to add todo: %v", err)
	}
	addResult := result1.(map[string]interface{})
	if addResult["id"] != "todo_1" {
		t.Errorf("Expected id 'todo_1', got %v", addResult["id"])
	}

	// Test listing todos
	result2, err := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("Failed to list todos: %v", err)
	}
	listResult := result2.(map[string]interface{})
	if listResult["count"] != 1 {
		t.Errorf("Expected count 1, got %v", listResult["count"])
	}

	// Test checking a todo
	result3, err := todo.Execute(ctx, map[string]interface{}{"action": "check", "id": "todo_1"})
	if err != nil {
		t.Fatalf("Failed to check todo: %v", err)
	}
	checkResult := result3.(map[string]interface{})
	if checkResult["id"] != "todo_1" {
		t.Errorf("Expected id 'todo_1', got %v", checkResult["id"])
	}

	// Verify todo is checked
	result4, err := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("Failed to list todos after check: %v", err)
	}
	listResult2 := result4.(map[string]interface{})
	todos := listResult2["todos"].([]*TodoItem)
	if !todos[0].Completed {
		t.Error("Expected todo to be completed")
	}

	// Test deleting a todo
	_, err = todo.Execute(ctx, map[string]interface{}{"action": "delete", "id": "todo_1"})
	if err != nil {
		t.Fatalf("Failed to delete todo: %v", err)
	}

	// Verify todo is deleted
	result5, err := todo.Execute(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("Failed to list todos after delete: %v", err)
	}
	listResult3 := result5.(map[string]interface{})
	if listResult3["count"] != 0 {
		t.Errorf("Expected count 0 after delete, got %v", listResult3["count"])
	}
}

func TestTodoTool_InMemoryNotPersisted(t *testing.T) {
	// Create first instance and add a todo
	todo1, actorSystem1, cancel1 := setupTodoToolForTest()
	ctx := context.Background()
	_, _ = todo1.Execute(ctx, map[string]interface{}{"action": "add", "text": "Test todo"})

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

	result, _ := todo2.Execute(ctx, map[string]interface{}{"action": "list"})
	listResult := result.(map[string]interface{})
	if listResult["count"] != 0 {
		t.Errorf("Expected new instance to have 0 todos (different actor), got %v", listResult["count"])
	}
}
