package tools

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/actor"
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
	item, err := client.Add("Test task", "2024-01-01T00:00:00Z", "", "pending", "medium")
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
			_, err := client.Add("Task", "2024-01-01T00:00:00Z", "", "pending", "medium")
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
	parent, err := client.Add("Parent task", "2024-01-01T00:00:00Z", "", "pending", "medium")
	if err != nil {
		t.Fatalf("Failed to add parent todo: %v", err)
	}

	// Add a sub-todo
	sub1, err := client.Add("Sub-task 1", "2024-01-01T00:00:00Z", parent.ID, "pending", "medium")
	if err != nil {
		t.Fatalf("Failed to add sub-todo: %v", err)
	}
	if sub1.ParentID != parent.ID {
		t.Errorf("Expected parent ID %s, got %s", parent.ID, sub1.ParentID)
	}

	// Add another sub-todo
	sub2, err := client.Add("Sub-task 2", "2024-01-01T00:00:00Z", parent.ID, "pending", "medium")
	if err != nil {
		t.Fatalf("Failed to add sub-todo: %v", err)
	}
	if sub2.ParentID != parent.ID {
		t.Errorf("Expected parent ID %s, got %s", parent.ID, sub2.ParentID)
	}

	// Add a nested sub-todo (grandchild)
	grandchild, err := client.Add("Nested sub-task", "2024-01-01T00:00:00Z", sub1.ID, "pending", "medium")
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
	_, err = client.Add("Sub-task", "2024-01-01T00:00:00Z", "nonexistent_id", "pending", "medium")
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
	parent, _ := client.Add("Parent", "2024-01-01T00:00:00Z", "", "pending", "medium")
	child1, _ := client.Add("Child 1", "2024-01-01T00:00:00Z", parent.ID, "pending", "medium")
	_, _ = client.Add("Grandchild", "2024-01-01T00:00:00Z", child1.ID, "pending", "medium")
	_, _ = client.Add("Child 2", "2024-01-01T00:00:00Z", parent.ID, "pending", "medium")

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
	parent, _ := client.Add("Parent", "2024-01-01T00:00:00Z", "", "pending", "medium")
	child1, _ := client.Add("Child 1", "2024-01-01T00:00:00Z", parent.ID, "pending", "medium")
	grandchild, _ := client.Add("Grandchild", "2024-01-01T00:00:00Z", child1.ID, "pending", "medium")
	_, _ = client.Add("Child 2", "2024-01-01T00:00:00Z", parent.ID, "pending", "medium")

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

func TestTodoActorClear(t *testing.T) {
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

	// Seed multiple todos (including nested)
	parent, err := client.Add("Parent", "2024-01-01T00:00:00Z", "", "pending", "medium")
	if err != nil {
		t.Fatalf("Failed to add parent todo: %v", err)
	}
	if _, err = client.Add("Child", "2024-01-01T00:00:00Z", parent.ID, "pending", "medium"); err != nil {
		t.Fatalf("Failed to add child todo: %v", err)
	}
	if _, err = client.Add("Root", "2024-01-01T00:00:00Z", "", "pending", "medium"); err != nil {
		t.Fatalf("Failed to add root todo: %v", err)
	}

	// Clear all todos
	if err := client.Clear(); err != nil {
		t.Fatalf("Failed to clear todos: %v", err)
	}

	list, err := client.List()
	if err != nil {
		t.Fatalf("Failed to list todos: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("Expected 0 todos after clear, got %d", len(list.Items))
	}
}

func TestTodoActorAddManySimple(t *testing.T) {
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

	inputs := []TodoInput{
		{Text: "Task 1", Status: "pending", Priority: "high"},
		{Text: "Task 2", Status: "in_progress", Priority: "medium"},
		{Text: "Task 3", Status: "completed", Priority: "low"},
	}

	items, err := client.AddMany(inputs, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Failed to add many todos: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(items))
	}

	// Verify IDs are sequential
	if items[0].ID != "todo_1" {
		t.Errorf("Expected first ID to be todo_1, got %s", items[0].ID)
	}
	if items[1].ID != "todo_2" {
		t.Errorf("Expected second ID to be todo_2, got %s", items[1].ID)
	}
	if items[2].ID != "todo_3" {
		t.Errorf("Expected third ID to be todo_3, got %s", items[2].ID)
	}

	// Verify properties
	if items[0].Text != "Task 1" || items[0].Status != "pending" || items[0].Priority != "high" {
		t.Error("First item properties don't match")
	}
	if items[1].Text != "Task 2" || items[1].Status != "in_progress" || items[1].Priority != "medium" {
		t.Error("Second item properties don't match")
	}
	if items[2].Text != "Task 3" || items[2].Status != "completed" || items[2].Priority != "low" {
		t.Error("Third item properties don't match")
	}

	// Verify all todos were added to the list
	list, _ := client.List()
	if len(list.Items) != 3 {
		t.Errorf("Expected 3 todos in list, got %d", len(list.Items))
	}
}

func TestTodoActorAddManyWithHierarchy(t *testing.T) {
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

	inputs := []TodoInput{
		{Text: "Parent task", Status: "pending", Priority: "high"},
		{Text: "Sub-task 1", ParentID: "0", Status: "pending", Priority: "medium"},
		{Text: "Sub-task 2", ParentID: "0", Status: "pending", Priority: "medium"},
		{Text: "Nested sub-task", ParentID: "1", Status: "pending", Priority: "low"},
	}

	items, err := client.AddMany(inputs, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Failed to add many todos: %v", err)
	}

	if len(items) != 4 {
		t.Errorf("Expected 4 items, got %d", len(items))
	}

	// Verify hierarchy
	if items[0].ParentID != "" {
		t.Errorf("Expected first item to have no parent, got %s", items[0].ParentID)
	}
	if items[1].ParentID != "todo_1" {
		t.Errorf("Expected second item to have parent todo_1, got %s", items[1].ParentID)
	}
	if items[2].ParentID != "todo_1" {
		t.Errorf("Expected third item to have parent todo_1, got %s", items[2].ParentID)
	}
	if items[3].ParentID != "todo_2" {
		t.Errorf("Expected fourth item to have parent todo_2, got %s", items[3].ParentID)
	}

	// Verify in list
	list, _ := client.List()
	if len(list.Items) != 4 {
		t.Errorf("Expected 4 todos in list, got %d", len(list.Items))
	}

	// Count hierarchy
	parentChildren := 0
	sub1Children := 0
	for _, item := range list.Items {
		if item.ParentID == "todo_1" {
			parentChildren++
		}
		if item.ParentID == "todo_2" {
			sub1Children++
		}
	}
	if parentChildren != 2 {
		t.Errorf("Expected 2 children of parent, got %d", parentChildren)
	}
	if sub1Children != 1 {
		t.Errorf("Expected 1 child of sub-task 1, got %d", sub1Children)
	}
}

func TestTodoActorAddManyWithExistingParent(t *testing.T) {
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

	// Add an existing parent todo first
	parent, err := client.Add("Existing parent", "2024-01-01T00:00:00Z", "", "pending", "high")
	if err != nil {
		t.Fatalf("Failed to add parent: %v", err)
	}

	// Add batch with reference to existing parent
	inputs := []TodoInput{
		{Text: "Child 1", ParentID: parent.ID, Status: "pending", Priority: "medium"},
		{Text: "Child 2", ParentID: parent.ID, Status: "pending", Priority: "low"},
	}

	items, err := client.AddMany(inputs, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Failed to add many todos: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}

	// Verify parent references
	if items[0].ParentID != parent.ID {
		t.Errorf("Expected first item to have parent %s, got %s", parent.ID, items[0].ParentID)
	}
	if items[1].ParentID != parent.ID {
		t.Errorf("Expected second item to have parent %s, got %s", parent.ID, items[1].ParentID)
	}

	// Verify total todos
	list, _ := client.List()
	if len(list.Items) != 3 {
		t.Errorf("Expected 3 todos in list, got %d", len(list.Items))
	}
}

func TestTodoActorAddManyInvalidParentIndex(t *testing.T) {
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

	// Try to reference an index that doesn't exist yet (forward reference)
	inputs := []TodoInput{
		{Text: "Task 1", Status: "pending"},
		{Text: "Task 2", ParentID: "2", Status: "pending"}, // Invalid: index 2 doesn't exist yet
	}

	_, err = client.AddMany(inputs, "2024-01-01T00:00:00Z")
	if err == nil {
		t.Error("Expected error when referencing non-existent parent index")
	}

	// Verify no todos were added (atomic operation)
	list, _ := client.List()
	if len(list.Items) != 0 {
		t.Errorf("Expected 0 todos after failed atomic operation, got %d", len(list.Items))
	}
}

func TestTodoActorAddManyInvalidExistingParent(t *testing.T) {
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

	// Try to reference a non-existent existing parent
	inputs := []TodoInput{
		{Text: "Task 1", ParentID: "nonexistent_id", Status: "pending"},
		{Text: "Task 2", Status: "pending"},
	}

	_, err = client.AddMany(inputs, "2024-01-01T00:00:00Z")
	if err == nil {
		t.Error("Expected error when referencing non-existent parent ID")
	}

	// Verify no todos were added (atomic operation)
	list, _ := client.List()
	if len(list.Items) != 0 {
		t.Errorf("Expected 0 todos after failed atomic operation, got %d", len(list.Items))
	}
}

func TestTodoActorAddManyEmpty(t *testing.T) {
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

	inputs := []TodoInput{}

	_, err = client.AddMany(inputs, "2024-01-01T00:00:00Z")
	if err == nil {
		t.Error("Expected error for empty todos array")
	}
}

func TestTodoActorAddManyWithDefaults(t *testing.T) {
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

	inputs := []TodoInput{
		{Text: "Task 1"}, // No status or priority specified
		{Text: "Task 2", Status: ""},
		{Text: "Task 3", Priority: ""},
		{Text: "Task 4", Status: "", Priority: ""},
	}

	items, err := client.AddMany(inputs, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Failed to add many todos: %v", err)
	}

	// Verify all have default status and priority
	for i, item := range items {
		if item.Status != "pending" {
			t.Errorf("Expected item %d to have status 'pending', got '%s'", i, item.Status)
		}
		if item.Priority != "medium" {
			t.Errorf("Expected item %d to have priority 'medium', got '%s'", i, item.Priority)
		}
	}
}
