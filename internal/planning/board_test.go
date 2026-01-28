package planning

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestPlanningTask_MarshalUnmarshal tests JSON serialization/deserialization of PlanningTask
func TestPlanningTask_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		task     *PlanningTask
		wantJSON string
		wantErr  bool
	}{
		{
			name: "minimal task",
			task: &PlanningTask{
				ID:   "task_1",
				Text: "Do something",
			},
			wantJSON: `{"id":"task_1","text":"Do something"}`,
			wantErr:  false,
		},
		{
			name: "task with priority and status",
			task: &PlanningTask{
				ID:       "task_2",
				Text:     "Important task",
				Priority: "high",
				Status:   "pending",
			},
			wantJSON: `{"id":"task_2","text":"Important task","priority":"high","status":"pending"}`,
			wantErr:  false,
		},
		{
			name: "task with description",
			task: &PlanningTask{
				ID:          "task_3",
				Text:        "Complex task",
				Description: "This is a detailed description of what needs to be done",
			},
			wantJSON: `{"id":"task_3","text":"Complex task","description":"This is a detailed description of what needs to be done"}`,
			wantErr:  false,
		},
		{
			name: "task with subtasks",
			task: &PlanningTask{
				ID:   "task_4",
				Text: "Parent task",
				Subtasks: []PlanningTask{
					{ID: "sub_1", Text: "Subtask 1"},
					{ID: "sub_2", Text: "Subtask 2"},
				},
			},
			wantJSON: `{"id":"task_4","text":"Parent task","subtasks":[{"id":"sub_1","text":"Subtask 1"},{"id":"sub_2","text":"Subtask 2"}]}`,
			wantErr:  false,
		},
		{
			name: "complete task with all fields",
			task: &PlanningTask{
				ID:          "task_5",
				Text:        "Complete task",
				Priority:    "medium",
				Status:      "in_progress",
				Description: "Full task description",
				Subtasks: []PlanningTask{
					{
						ID:       "sub_1",
						Text:     "Subtask 1",
						Priority: "high",
						Status:   "completed",
					},
				},
			},
			wantJSON: `{"id":"task_5","text":"Complete task","priority":"medium","status":"in_progress","description":"Full task description","subtasks":[{"id":"sub_1","text":"Subtask 1","priority":"high","status":"completed"}]}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.task)
			if (err != nil) != tt.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				var gotJSON string
				if err := json.Unmarshal(data, &gotJSON); err == nil {
					// Verify JSON can be parsed back
					if !json.Valid(data) {
						t.Errorf("Marshal() produced invalid JSON: %s", string(data))
					}
				}
			}

			// Test unmarshaling
			var unmarshaled PlanningTask
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Errorf("Unmarshal() error = %v", err)
				return
			}

			// Verify all fields match
			if unmarshaled.ID != tt.task.ID {
				t.Errorf("ID = %v, want %v", unmarshaled.ID, tt.task.ID)
			}
			if unmarshaled.Text != tt.task.Text {
				t.Errorf("Text = %v, want %v", unmarshaled.Text, tt.task.Text)
			}
			if unmarshaled.Priority != tt.task.Priority {
				t.Errorf("Priority = %v, want %v", unmarshaled.Priority, tt.task.Priority)
			}
			if unmarshaled.Status != tt.task.Status {
				t.Errorf("Status = %v, want %v", unmarshaled.Status, tt.task.Status)
			}
			if unmarshaled.Description != tt.task.Description {
				t.Errorf("Description = %v, want %v", unmarshaled.Description, tt.task.Description)
			}
			if len(unmarshaled.Subtasks) != len(tt.task.Subtasks) {
				t.Errorf("Subtasks count = %v, want %v", len(unmarshaled.Subtasks), len(tt.task.Subtasks))
			}
		})
	}
}

// TestPlanningBoard_MarshalUnmarshal tests JSON serialization/deserialization of PlanningBoard
func TestPlanningBoard_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		board   *PlanningBoard
		wantErr bool
		verify  func(*testing.T, *PlanningBoard)
	}{
		{
			name: "empty board",
			board: &PlanningBoard{
				PrimaryTasks: []PlanningTask{},
			},
			wantErr: false,
			verify: func(t *testing.T, b *PlanningBoard) {
				if len(b.PrimaryTasks) != 0 {
					t.Errorf("Expected 0 primary tasks, got %d", len(b.PrimaryTasks))
				}
			},
		},
		{
			name: "board with single task",
			board: &PlanningBoard{
				Description: "Simple board",
				PrimaryTasks: []PlanningTask{
					{ID: "task_1", Text: "First task"},
				},
			},
			wantErr: false,
			verify: func(t *testing.T, b *PlanningBoard) {
				if b.Description != "Simple board" {
					t.Errorf("Description = %v, want 'Simple board'", b.Description)
				}
				if len(b.PrimaryTasks) != 1 {
					t.Errorf("Expected 1 primary task, got %d", len(b.PrimaryTasks))
				}
			},
		},
		{
			name: "board with multiple tasks",
			board: &PlanningBoard{
				Description: "Complex board",
				PrimaryTasks: []PlanningTask{
					{ID: "task_1", Text: "Task 1", Priority: "high"},
					{ID: "task_2", Text: "Task 2", Priority: "medium"},
					{ID: "task_3", Text: "Task 3", Priority: "low"},
				},
			},
			wantErr: false,
			verify: func(t *testing.T, b *PlanningBoard) {
				if len(b.PrimaryTasks) != 3 {
					t.Errorf("Expected 3 primary tasks, got %d", len(b.PrimaryTasks))
				}
			},
		},
		{
			name: "board with nested subtasks",
			board: &PlanningBoard{
				Description: "Nested board",
				PrimaryTasks: []PlanningTask{
					{
						ID:   "task_1",
						Text: "Parent task",
						Subtasks: []PlanningTask{
							{
								ID:   "sub_1",
								Text: "Child task",
								Subtasks: []PlanningTask{
									{ID: "subsub_1", Text: "Grandchild task"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			verify: func(t *testing.T, b *PlanningBoard) {
				if len(b.PrimaryTasks) != 1 {
					t.Fatalf("Expected 1 primary task, got %d", len(b.PrimaryTasks))
				}
				if len(b.PrimaryTasks[0].Subtasks) != 1 {
					t.Fatalf("Expected 1 subtask, got %d", len(b.PrimaryTasks[0].Subtasks))
				}
				if len(b.PrimaryTasks[0].Subtasks[0].Subtasks) != 1 {
					t.Errorf("Expected 1 nested subtask, got %d", len(b.PrimaryTasks[0].Subtasks[0].Subtasks))
				}
			},
		},
		{
			name: "complete board with all fields",
			board: &PlanningBoard{
				Description: "Complete board description",
				PrimaryTasks: []PlanningTask{
					{
						ID:          "task_1",
						Text:        "Implement feature",
						Priority:    "high",
						Status:      "pending",
						Description: "Main feature implementation",
						Subtasks: []PlanningTask{
							{ID: "task_1_1", Text: "Design API", Priority: "high", Status: "completed"},
							{ID: "task_1_2", Text: "Implement endpoints", Priority: "high", Status: "in_progress"},
							{ID: "task_1_3", Text: "Add tests", Priority: "medium", Status: "pending"},
						},
					},
					{
						ID:          "task_2",
						Text:        "Write documentation",
						Priority:    "medium",
						Status:      "pending",
						Description: "User and API documentation",
					},
				},
			},
			wantErr: false,
			verify: func(t *testing.T, b *PlanningBoard) {
				if len(b.PrimaryTasks) != 2 {
					t.Errorf("Expected 2 primary tasks, got %d", len(b.PrimaryTasks))
				}
				if len(b.PrimaryTasks[0].Subtasks) != 3 {
					t.Errorf("Expected 3 subtasks for first task, got %d", len(b.PrimaryTasks[0].Subtasks))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.board)
			if (err != nil) != tt.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify JSON is valid
			if !tt.wantErr && !json.Valid(data) {
				t.Errorf("Marshal() produced invalid JSON: %s", string(data))
			}

			// Test unmarshaling
			var unmarshaled PlanningBoard
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Errorf("Unmarshal() error = %v", err)
				return
			}

			// Run verification function
			if tt.verify != nil {
				tt.verify(t, &unmarshaled)
			}
		})
	}
}

// TestPlanningTask_StatusTransitions tests task status field validation
func TestPlanningTask_StatusTransitions(t *testing.T) {
	validStatuses := []string{"pending", "in_progress", "completed"}

	for _, status := range validStatuses {
		t.Run("valid status_"+status, func(t *testing.T) {
			task := &PlanningTask{
				ID:     "task_1",
				Text:   "Test task",
				Status: status,
			}

			// Verify it marshals/unmarshals correctly
			data, err := json.Marshal(task)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var unmarshaled PlanningTask
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if unmarshaled.Status != status {
				t.Errorf("Status = %v, want %v", unmarshaled.Status, status)
			}
		})
	}
}

// TestPlanningTask_PriorityLevels tests task priority field validation
func TestPlanningTask_PriorityLevels(t *testing.T) {
	validPriorities := []string{"high", "medium", "low"}

	for _, priority := range validPriorities {
		t.Run("valid priority_"+priority, func(t *testing.T) {
			task := &PlanningTask{
				ID:       "task_1",
				Text:     "Test task",
				Priority: priority,
			}

			// Verify it marshals/unmarshals correctly
			data, err := json.Marshal(task)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var unmarshaled PlanningTask
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if unmarshaled.Priority != priority {
				t.Errorf("Priority = %v, want %v", unmarshaled.Priority, priority)
			}
		})
	}
}

// TestPlanningBoard_TaskCount verifies task counting
func TestPlanningBoard_TaskCount(t *testing.T) {
	board := &PlanningBoard{
		Description: "Test board",
		PrimaryTasks: []PlanningTask{
			{
				ID:   "task_1",
				Text: "Task 1",
				Subtasks: []PlanningTask{
					{ID: "sub_1", Text: "Subtask 1"},
					{ID: "sub_2", Text: "Subtask 2"},
				},
			},
			{
				ID:   "task_2",
				Text: "Task 2",
				Subtasks: []PlanningTask{
					{ID: "sub_3", Text: "Subtask 3"},
				},
			},
		},
	}

	if len(board.PrimaryTasks) != 2 {
		t.Errorf("Expected 2 primary tasks, got %d", len(board.PrimaryTasks))
	}

	totalSubtasks := 0
	for _, task := range board.PrimaryTasks {
		totalSubtasks += len(task.Subtasks)
	}

	if totalSubtasks != 3 {
		t.Errorf("Expected 3 total subtasks, got %d", totalSubtasks)
	}
}

// TestPlanningBoard_DeepNesting tests deeply nested task structures
func TestPlanningBoard_DeepNesting(t *testing.T) {
	board := &PlanningBoard{
		PrimaryTasks: []PlanningTask{
			{
				ID:   "task_1",
				Text: "Level 0",
				Subtasks: []PlanningTask{
					{
						ID:   "task_1_1",
						Text: "Level 1",
						Subtasks: []PlanningTask{
							{
								ID:   "task_1_1_1",
								Text: "Level 2",
								Subtasks: []PlanningTask{
									{ID: "task_1_1_1_1", Text: "Level 3"},
								},
							},
						},
					},
				},
			},
		},
	}

	// Test marshaling deep structure
	data, err := json.Marshal(board)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Test unmarshaling deep structure
	var unmarshaled PlanningBoard
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify deep nesting is preserved
	if len(unmarshaled.PrimaryTasks) != 1 {
		t.Fatalf("Expected 1 primary task, got %d", len(unmarshaled.PrimaryTasks))
	}

	if len(unmarshaled.PrimaryTasks[0].Subtasks) != 1 {
		t.Fatalf("Expected 1 level-1 subtask, got %d", len(unmarshaled.PrimaryTasks[0].Subtasks))
	}

	if len(unmarshaled.PrimaryTasks[0].Subtasks[0].Subtasks) != 1 {
		t.Fatalf("Expected 1 level-2 subtask, got %d", len(unmarshaled.PrimaryTasks[0].Subtasks[0].Subtasks))
	}

	if len(unmarshaled.PrimaryTasks[0].Subtasks[0].Subtasks[0].Subtasks) != 1 {
		t.Fatalf("Expected 1 level-3 subtask, got %d", len(unmarshaled.PrimaryTasks[0].Subtasks[0].Subtasks[0].Subtasks))
	}
}

// TestPlanningResponse_WithBoard tests PlanningResponse with board mode
func TestPlanningResponse_WithBoard(t *testing.T) {
	response := &PlanningResponse{
		Mode: PlanningModeBoard,
		Board: &PlanningBoard{
			Description: "Test response",
			PrimaryTasks: []PlanningTask{
				{ID: "task_1", Text: "Task 1"},
			},
		},
		Complete: true,
	}

	// Test marshaling
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Test unmarshaling
	var unmarshaled PlanningResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if unmarshaled.Mode != PlanningModeBoard {
		t.Errorf("Mode = %v, want %v", unmarshaled.Mode, PlanningModeBoard)
	}

	if unmarshaled.Board == nil {
		t.Fatal("Board should not be nil")
	}

	if len(unmarshaled.Board.PrimaryTasks) != 1 {
		t.Errorf("Expected 1 primary task, got %d", len(unmarshaled.Board.PrimaryTasks))
	}

	if !unmarshaled.Complete {
		t.Error("Expected Complete to be true")
	}
}

// TestPlanningResponse_WithPlan tests PlanningResponse with simple mode
func TestPlanningResponse_WithPlan(t *testing.T) {
	response := &PlanningResponse{
		Mode:     PlanningModeSimple,
		Plan:     []string{"Step 1", "Step 2", "Step 3"},
		Complete: true,
	}

	// Test marshaling
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Test unmarshaling
	var unmarshaled PlanningResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if unmarshaled.Mode != PlanningModeSimple {
		t.Errorf("Mode = %v, want %v", unmarshaled.Mode, PlanningModeSimple)
	}

	if len(unmarshaled.Plan) != 3 {
		t.Errorf("Expected 3 plan steps, got %d", len(unmarshaled.Plan))
	}

	if !unmarshaled.Complete {
		t.Error("Expected Complete to be true")
	}
}

// TestPlanningResponse_WithQuestions tests PlanningResponse with questions
func TestPlanningResponse_WithQuestions(t *testing.T) {
	response := &PlanningResponse{
		Mode:       PlanningModeSimple,
		Questions:  []string{"What framework?", "What database?"},
		NeedsInput: true,
		Complete:   false,
	}

	// Test marshaling
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Test unmarshaling
	var unmarshaled PlanningResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(unmarshaled.Questions) != 2 {
		t.Errorf("Expected 2 questions, got %d", len(unmarshaled.Questions))
	}

	if !unmarshaled.NeedsInput {
		t.Error("Expected NeedsInput to be true")
	}

	if unmarshaled.Complete {
		t.Error("Expected Complete to be false")
	}
}

// TestPlanningTask_EmptyFields tests handling of empty/nil fields
func TestPlanningTask_EmptyFields(t *testing.T) {
	task := &PlanningTask{
		ID:   "task_1",
		Text: "Task",
		// Other fields are empty/zero values
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var unmarshaled PlanningTask
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if unmarshaled.ID != "task_1" {
		t.Errorf("ID = %v, want 'task_1'", unmarshaled.ID)
	}
	if unmarshaled.Text != "Task" {
		t.Errorf("Text = %v, want 'Task'", unmarshaled.Text)
	}
	if unmarshaled.Priority != "" {
		t.Errorf("Priority should be empty string, got %v", unmarshaled.Priority)
	}
	if unmarshaled.Status != "" {
		t.Errorf("Status should be empty string, got %v", unmarshaled.Status)
	}
	if unmarshaled.Description != "" {
		t.Errorf("Description should be empty string, got %v", unmarshaled.Description)
	}
	if len(unmarshaled.Subtasks) != 0 {
		t.Errorf("Subtasks should be empty, got %d items", len(unmarshaled.Subtasks))
	}
}

// TestPlanningBoard_EmojisAndUnicode tests handling of emoji and unicode characters
func TestPlanningBoard_EmojisAndUnicode(t *testing.T) {
	board := &PlanningBoard{
		Description: "🚀 Project with unicode áéíóú 中文",
		PrimaryTasks: []PlanningTask{
			{ID: "task_1", Text: "🎯 Set up environment"},
			{ID: "task_2", Text: "📝 Write documentation"},
			{ID: "task_3", Text: "🧪 Add tests 中文"},
		},
	}

	data, err := json.Marshal(board)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var unmarshaled PlanningBoard
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if unmarshaled.Description != board.Description {
		t.Errorf("Description with unicode was not preserved: got %v, want %v", unmarshaled.Description, board.Description)
	}

	for i, task := range board.PrimaryTasks {
		if unmarshaled.PrimaryTasks[i].Text != task.Text {
			t.Errorf("Task text with unicode was not preserved: got %v, want %v", unmarshaled.PrimaryTasks[i].Text, task.Text)
		}
	}
}

// TestPlanningBoard_LargeScale tests handling of many tasks
func TestPlanningBoard_LargeScale(t *testing.T) {
	const taskCount = 100

	tasks := make([]PlanningTask, taskCount)
	for i := 0; i < taskCount; i++ {
		tasks[i] = PlanningTask{
			ID:   fmt.Sprintf("task_%d", i),
			Text: fmt.Sprintf("Task number %d", i),
		}
	}

	board := &PlanningBoard{
		Description:  "Large board",
		PrimaryTasks: tasks,
	}

	// Test marshaling
	data, err := json.Marshal(board)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Test unmarshaling
	var unmarshaled PlanningBoard
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(unmarshaled.PrimaryTasks) != taskCount {
		t.Fatalf("Expected %d tasks, got %d", taskCount, len(unmarshaled.PrimaryTasks))
	}

	// Verify all tasks are present
	for i := 0; i < taskCount; i++ {
		expectedID := fmt.Sprintf("task_%d", i)
		if unmarshaled.PrimaryTasks[i].ID != expectedID {
			t.Errorf("Task %d: ID = %v, want %v", i, unmarshaled.PrimaryTasks[i].ID, expectedID)
		}
	}
}
