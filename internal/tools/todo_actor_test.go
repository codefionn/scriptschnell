package tools

import (
	"context"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/actor"
)

func TestTodoActor(t *testing.T) {
	actorSystem := actor.NewSystem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	todoActor := NewTodoActor("test-todo")
	todoRef, err := actorSystem.Spawn(ctx, "test-todo", todoActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn todo actor: %v", err)
	}
	defer func() { _ = actorSystem.StopAll(context.Background()) }()

	client := NewTodoActorClient(todoRef)

	// Test adding a todo
	item, err := client.Add("Test task", "2024-01-01T00:00:00Z", "")
	if err != nil {
		t.Fatalf("Failed to add todo: %v", err)
	}
	if item.ID != "todo_1" {
		t.Errorf("Expected ID 'todo_1', got %s", item.ID)
	}
	if item.Text != "Test task" {
		t.Errorf("Expected text 'Test task', got %s", item.Text)
	}
	if item.Completed {
		t.Error("Expected todo to be uncompleted")
	}
	if item.ParentID != "" {
		t.Errorf("Expected no parent, got %s", item.ParentID)
	}

	// Test listing todos
	list, err := client.List()
	if err != nil {
		t.Fatalf("Failed to list todos: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("Expected 1 todo, got %d", len(list.Items))
	}

	// Test checking a todo
	err = client.Check("todo_1", true)
	if err != nil {
		t.Fatalf("Failed to check todo: %v", err)
	}

	// Verify todo is checked
	list, _ = client.List()
	if !list.Items[0].Completed {
		t.Error("Expected todo to be completed")
	}

	// Test unchecking a todo
	err = client.Check("todo_1", false)
	if err != nil {
		t.Fatalf("Failed to uncheck todo: %v", err)
	}

	// Verify todo is unchecked
	list, _ = client.List()
	if list.Items[0].Completed {
		t.Error("Expected todo to be uncompleted")
	}

	// Test deleting a todo
	err = client.Delete("todo_1")
	if err != nil {
		t.Fatalf("Failed to delete todo: %v", err)
	}

	// Verify todo is deleted
	list, _ = client.List()
	if len(list.Items) != 0 {
		t.Errorf("Expected 0 todos after delete, got %d", len(list.Items))
	}
}

func TestTodoActorConcurrency(t *testing.T) {
	actorSystem := actor.NewSystem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	todoActor := NewTodoActor("test-todo")
	todoRef, err := actorSystem.Spawn(ctx, "test-todo", todoActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn todo actor: %v", err)
	}
	defer func() { _ = actorSystem.StopAll(context.Background()) }()

	client := NewTodoActorClient(todoRef)

	// Add multiple todos concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			_, err := client.Add("Task", "2024-01-01T00:00:00Z", "")
			if err != nil {
				t.Errorf("Failed to add todo: %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all todos were added
	list, err := client.List()
	if err != nil {
		t.Fatalf("Failed to list todos: %v", err)
	}
	if len(list.Items) != 10 {
		t.Errorf("Expected 10 todos, got %d", len(list.Items))
	}
}

func TestTodoActorSubTodos(t *testing.T) {
	actorSystem := actor.NewSystem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	todoActor := NewTodoActor("test-todo")
	todoRef, err := actorSystem.Spawn(ctx, "test-todo", todoActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn todo actor: %v", err)
	}
	defer func() { _ = actorSystem.StopAll(context.Background()) }()

	client := NewTodoActorClient(todoRef)

	// Add a parent todo
	parent, err := client.Add("Parent task", "2024-01-01T00:00:00Z", "")
	if err != nil {
		t.Fatalf("Failed to add parent todo: %v", err)
	}

	// Add a sub-todo
	sub1, err := client.Add("Sub-task 1", "2024-01-01T00:00:00Z", parent.ID)
	if err != nil {
		t.Fatalf("Failed to add sub-todo: %v", err)
	}
	if sub1.ParentID != parent.ID {
		t.Errorf("Expected parent ID %s, got %s", parent.ID, sub1.ParentID)
	}

	// Add another sub-todo
	sub2, err := client.Add("Sub-task 2", "2024-01-01T00:00:00Z", parent.ID)
	if err != nil {
		t.Fatalf("Failed to add sub-todo: %v", err)
	}
	if sub2.ParentID != parent.ID {
		t.Errorf("Expected parent ID %s, got %s", parent.ID, sub2.ParentID)
	}

	// Add a nested sub-todo (grandchild)
	grandchild, err := client.Add("Nested sub-task", "2024-01-01T00:00:00Z", sub1.ID)
	if err != nil {
		t.Fatalf("Failed to add nested sub-todo: %v", err)
	}
	if grandchild.ParentID != sub1.ID {
		t.Errorf("Expected parent ID %s, got %s", sub1.ID, grandchild.ParentID)
	}

	// Verify all todos were added
	list, _ := client.List()
	if len(list.Items) != 4 {
		t.Errorf("Expected 4 todos, got %d", len(list.Items))
	}

	// Verify hierarchy
	parentCount := 0
	parentChildren := 0
	sub1Children := 0
	for _, item := range list.Items {
		if item.ParentID == "" {
			parentCount++
		}
		if item.ParentID == parent.ID {
			parentChildren++
		}
		if item.ParentID == sub1.ID {
			sub1Children++
		}
	}
	if parentCount != 1 {
		t.Errorf("Expected 1 top-level todo, got %d", parentCount)
	}
	if parentChildren != 2 {
		t.Errorf("Expected 2 direct children of parent, got %d", parentChildren)
	}
	if sub1Children != 1 {
		t.Errorf("Expected 1 child of sub1, got %d", sub1Children)
	}
}

func TestTodoActorSubTodoValidation(t *testing.T) {
	actorSystem := actor.NewSystem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	todoActor := NewTodoActor("test-todo")
	todoRef, err := actorSystem.Spawn(ctx, "test-todo", todoActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn todo actor: %v", err)
	}
	defer func() { _ = actorSystem.StopAll(context.Background()) }()

	client := NewTodoActorClient(todoRef)

	// Try to add a sub-todo with non-existent parent
	_, err = client.Add("Sub-task", "2024-01-01T00:00:00Z", "nonexistent_id")
	if err == nil {
		t.Error("Expected error when adding sub-todo with non-existent parent")
	}
}

func TestTodoActorRecursiveDelete(t *testing.T) {
	actorSystem := actor.NewSystem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	todoActor := NewTodoActor("test-todo")
	todoRef, err := actorSystem.Spawn(ctx, "test-todo", todoActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn todo actor: %v", err)
	}
	defer func() { _ = actorSystem.StopAll(context.Background()) }()

	client := NewTodoActorClient(todoRef)

	// Create a hierarchy: parent -> child1 -> grandchild
	//                            -> child2
	parent, _ := client.Add("Parent", "2024-01-01T00:00:00Z", "")
	child1, _ := client.Add("Child 1", "2024-01-01T00:00:00Z", parent.ID)
	_, _ = client.Add("Grandchild", "2024-01-01T00:00:00Z", child1.ID)
	_, _ = client.Add("Child 2", "2024-01-01T00:00:00Z", parent.ID)

	// Verify we have 4 todos
	list, _ := client.List()
	if len(list.Items) != 4 {
		t.Fatalf("Expected 4 todos, got %d", len(list.Items))
	}

	// Delete the parent - should delete all children recursively
	err = client.Delete(parent.ID)
	if err != nil {
		t.Fatalf("Failed to delete parent: %v", err)
	}

	// Verify all todos were deleted
	list, _ = client.List()
	if len(list.Items) != 0 {
		t.Errorf("Expected 0 todos after recursive delete, got %d", len(list.Items))
	}
}

func TestTodoActorDeleteSubTodoOnly(t *testing.T) {
	actorSystem := actor.NewSystem()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	todoActor := NewTodoActor("test-todo")
	todoRef, err := actorSystem.Spawn(ctx, "test-todo", todoActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn todo actor: %v", err)
	}
	defer func() { _ = actorSystem.StopAll(context.Background()) }()

	client := NewTodoActorClient(todoRef)

	// Create a hierarchy
	parent, _ := client.Add("Parent", "2024-01-01T00:00:00Z", "")
	child1, _ := client.Add("Child 1", "2024-01-01T00:00:00Z", parent.ID)
	grandchild, _ := client.Add("Grandchild", "2024-01-01T00:00:00Z", child1.ID)
	_, _ = client.Add("Child 2", "2024-01-01T00:00:00Z", parent.ID)

	// Delete only child1 - should also delete grandchild but keep parent and child2
	err = client.Delete(child1.ID)
	if err != nil {
		t.Fatalf("Failed to delete child1: %v", err)
	}

	// Verify correct todos remain
	list, _ := client.List()
	if len(list.Items) != 2 {
		t.Errorf("Expected 2 todos (parent and child2), got %d", len(list.Items))
	}

	// Verify parent and child2 still exist
	foundParent := false
	foundChild2 := false
	foundChild1 := false
	foundGrandchild := false
	for _, item := range list.Items {
		if item.ID == parent.ID {
			foundParent = true
		}
		if item.Text == "Child 2" {
			foundChild2 = true
		}
		if item.ID == child1.ID {
			foundChild1 = true
		}
		if item.ID == grandchild.ID {
			foundGrandchild = true
		}
	}

	if !foundParent {
		t.Error("Expected parent to still exist")
	}
	if !foundChild2 {
		t.Error("Expected child2 to still exist")
	}
	if foundChild1 {
		t.Error("Expected child1 to be deleted")
	}
	if foundGrandchild {
		t.Error("Expected grandchild to be deleted")
	}
}
