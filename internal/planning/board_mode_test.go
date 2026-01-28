package planning

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/session"
)

// TestPlanningAgent_BoardModeBasic tests basic board mode planning
func TestPlanningAgent_BoardModeBasic(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Mock LLM returns a board mode response
	mockLLM := NewMockLLMClient(`<answer>{
		"mode": "board",
		"board": {
			"description": "Implementation plan for feature X",
			"primary_tasks": [
				{
					"id": "task_1",
					"text": "Set up project structure",
					"priority": "high",
					"subtasks": [
						{"id": "task_1_1", "text": "Create directories", "status": "pending"},
						{"id": "task_1_2", "text": "Initialize modules", "status": "pending"}
					]
				},
				{
					"id": "task_2",
					"text": "Implement core functionality",
					"priority": "medium"
				}
			]
		},
		"complete": true
	}</answer>`)

	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Create a new feature",
		AllowQuestions: false,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)

	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if response.Mode != PlanningModeBoard {
		t.Errorf("Expected mode 'board', got '%s'", response.Mode)
	}

	if response.Board == nil {
		t.Fatal("Expected non-nil board")
	}

	if len(response.Board.PrimaryTasks) != 2 {
		t.Errorf("Expected 2 primary tasks, got %d", len(response.Board.PrimaryTasks))
	}

	if response.Board.Description != "Implementation plan for feature X" {
		t.Errorf("Expected description 'Implementation plan for feature X', got '%s'", response.Board.Description)
	}

	// Check first task has subtasks
	if len(response.Board.PrimaryTasks[0].Subtasks) != 2 {
		t.Errorf("Expected 2 subtasks for first task, got %d", len(response.Board.PrimaryTasks[0].Subtasks))
	}

	if !response.Complete {
		t.Error("Expected response to be complete")
	}
}

// TestPlanningAgent_BoardModeWithoutComplete tests board mode where LLM forgets to set complete: true
// This was the bug that caused the orchestration loop to terminate unexpectedly
func TestPlanningAgent_BoardModeWithoutComplete(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Mock LLM returns board mode with complete: false (the bug case)
	mockLLM := NewMockLLMClient(`<answer>{
		"mode": "board",
		"board": {
			"primary_tasks": [
				{"id": "task_1", "text": "First task"},
				{"id": "task_2", "text": "Second task"}
			]
		},
		"complete": false
	}</answer>`)

	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Create a feature",
		AllowQuestions: false,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)

	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	// The fix ensures we detect board content even when complete is false
	if response.Mode != PlanningModeBoard {
		t.Errorf("Expected mode 'board', got '%s'", response.Mode)
	}

	if response.Board == nil {
		t.Fatal("Expected non-nil board - this was the bug case")
	}

	if len(response.Board.PrimaryTasks) != 2 {
		t.Errorf("Expected 2 primary tasks, got %d", len(response.Board.PrimaryTasks))
	}
}

// TestPlanningAgent_BoardModeInference tests mode inference when mode field is empty
func TestPlanningAgent_BoardModeInference(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Mock LLM returns board structure without explicit mode
	mockLLM := NewMockLLMClient(`<answer>{
		"board": {
			"primary_tasks": [
				{"id": "task_1", "text": "Inferred task"}
			]
		},
		"complete": true
	}</answer>`)

	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Test inference",
		AllowQuestions: false,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)

	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	// Mode should be inferred as board
	if response.Mode != PlanningModeBoard {
		t.Errorf("Expected inferred mode 'board', got '%s'", response.Mode)
	}

	if response.Board == nil {
		t.Fatal("Expected non-nil board")
	}
}

// TestPlanningAgent_SimpleModeStillWorks tests that simple mode still works after board mode changes
func TestPlanningAgent_SimpleModeStillWorks(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Mock LLM returns simple mode response
	mockLLM := NewMockLLMClient(`<answer>{
		"mode": "simple",
		"plan": ["Step 1: Do something", "Step 2: Do something else"],
		"complete": true
	}</answer>`)

	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	req := &PlanningRequest{
		Objective:      "Simple task",
		AllowQuestions: false,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)

	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if response.Mode != PlanningModeSimple {
		t.Errorf("Expected mode 'simple', got '%s'", response.Mode)
	}

	if len(response.Plan) != 2 {
		t.Errorf("Expected 2 plan steps, got %d", len(response.Plan))
	}

	if response.Board != nil {
		t.Error("Expected nil board for simple mode")
	}
}

// TestTryParseJSONPlan_BoardMode tests the tryParseJSONPlan function with board mode
func TestTryParseJSONPlan_BoardMode(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("dummy")
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	tests := []struct {
		name           string
		json           string
		expectNil      bool
		expectMode     PlanningMode
		expectTasks    int
		expectPlanLen  int
		expectComplete bool
	}{
		{
			name: "valid board mode with tasks",
			json: `{
				"mode": "board",
				"board": {"primary_tasks": [{"id": "1", "text": "task"}]},
				"complete": true
			}`,
			expectNil:      false,
			expectMode:     PlanningModeBoard,
			expectTasks:    1,
			expectComplete: true,
		},
		{
			name: "board mode with empty tasks but complete",
			json: `{
				"mode": "board",
				"board": {"primary_tasks": []},
				"complete": true
			}`,
			expectNil:      false,
			expectMode:     PlanningModeBoard,
			expectTasks:    0,
			expectComplete: true,
		},
		{
			name: "board mode with nil board - should fail",
			json: `{
				"mode": "board",
				"complete": true
			}`,
			expectNil: true,
		},
		{
			name: "board mode with empty tasks not complete - should fail",
			json: `{
				"mode": "board",
				"board": {"primary_tasks": []},
				"complete": false,
				"needs_input": false
			}`,
			expectNil: true,
		},
		{
			name: "simple mode",
			json: `{
				"mode": "simple",
				"plan": ["step 1", "step 2"],
				"complete": true
			}`,
			expectNil:      false,
			expectMode:     PlanningModeSimple,
			expectPlanLen:  2,
			expectComplete: true,
		},
		{
			name: "inferred board mode",
			json: `{
				"board": {"primary_tasks": [{"id": "1", "text": "task"}]},
				"complete": true
			}`,
			expectNil:      false,
			expectMode:     PlanningModeBoard,
			expectTasks:    1,
			expectComplete: true,
		},
		{
			name: "inferred simple mode",
			json: `{
				"plan": ["step 1"],
				"complete": true
			}`,
			expectNil:      false,
			expectMode:     PlanningModeSimple,
			expectPlanLen:  1,
			expectComplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.tryParseJSONPlan(tt.json)

			if tt.expectNil {
				if result != nil {
					t.Errorf("Expected nil result, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if result.Mode != tt.expectMode {
				t.Errorf("Expected mode '%s', got '%s'", tt.expectMode, result.Mode)
			}

			if tt.expectMode == PlanningModeBoard && result.Board != nil {
				if len(result.Board.PrimaryTasks) != tt.expectTasks {
					t.Errorf("Expected %d tasks, got %d", tt.expectTasks, len(result.Board.PrimaryTasks))
				}
			}

			if tt.expectPlanLen > 0 && len(result.Plan) != tt.expectPlanLen {
				t.Errorf("Expected %d plan steps, got %d", tt.expectPlanLen, len(result.Plan))
			}

			if result.Complete != tt.expectComplete {
				t.Errorf("Expected complete=%v, got %v", tt.expectComplete, result.Complete)
			}
		})
	}
}

// TestExtractPartialPlan_BoardMode tests extractPartialPlan with board mode content
func TestExtractPartialPlan_BoardMode(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("dummy")
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	defer agent.Close(context.Background())

	tests := []struct {
		name        string
		content     string
		expectBoard bool
		expectTasks int
	}{
		{
			name: "board mode in answer tags",
			content: `<answer>{
				"mode": "board",
				"board": {"primary_tasks": [{"id": "1", "text": "task"}]},
				"complete": true
			}</answer>`,
			expectBoard: true,
			expectTasks: 1,
		},
		{
			name: "simple mode in answer tags",
			content: `<answer>{
				"mode": "simple",
				"plan": ["step 1"],
				"complete": true
			}</answer>`,
			expectBoard: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := []*llm.Message{
				{Role: "assistant", Content: tt.content},
			}

			result := agent.extractPartialPlan(messages)

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expectBoard {
				if result.Board == nil {
					t.Error("Expected non-nil board")
				} else if len(result.Board.PrimaryTasks) != tt.expectTasks {
					t.Errorf("Expected %d tasks, got %d", tt.expectTasks, len(result.Board.PrimaryTasks))
				}
			}

			// Partial plans should always have NeedsInput=true, Complete=false
			if !result.NeedsInput {
				t.Error("Expected NeedsInput=true for partial plan")
			}
			if result.Complete {
				t.Error("Expected Complete=false for partial plan")
			}
		})
	}
}

// TestHasContentCheck verifies the fix for the hasContent check
func TestHasContentCheck(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	tests := []struct {
		name          string
		response      string
		shouldReturn  bool
		expectedMode  PlanningMode
		expectedTasks int
		expectedPlan  int
	}{
		{
			name: "board mode with tasks, complete=false",
			response: `<answer>{
				"mode": "board",
				"board": {"primary_tasks": [{"id": "1", "text": "task"}]},
				"complete": false
			}</answer>`,
			shouldReturn:  true,
			expectedMode:  PlanningModeBoard,
			expectedTasks: 1,
		},
		{
			name: "board mode with tasks, complete=true",
			response: `<answer>{
				"mode": "board",
				"board": {"primary_tasks": [{"id": "1", "text": "task"}, {"id": "2", "text": "task2"}]},
				"complete": true
			}</answer>`,
			shouldReturn:  true,
			expectedMode:  PlanningModeBoard,
			expectedTasks: 2,
		},
		{
			name: "simple mode with plan, complete=false",
			response: `<answer>{
				"mode": "simple",
				"plan": ["step 1"],
				"complete": false
			}</answer>`,
			shouldReturn: true,
			expectedMode: PlanningModeSimple,
			expectedPlan: 1,
		},
		{
			name: "simple mode with plan, complete=true",
			response: `<answer>{
				"mode": "simple",
				"plan": ["step 1", "step 2"],
				"complete": true
			}</answer>`,
			shouldReturn: true,
			expectedMode: PlanningModeSimple,
			expectedPlan: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLLM := NewMockLLMClient(tt.response)
			agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
			defer agent.Close(context.Background())

			req := &PlanningRequest{
				Objective:      "Test hasContent",
				AllowQuestions: false,
			}

			ctx := context.Background()
			response, err := agent.Plan(ctx, req, nil)

			if err != nil {
				t.Fatalf("Planning failed: %v", err)
			}

			if !tt.shouldReturn {
				// This case shouldn't happen with valid responses
				t.Skip("Skipping non-return case")
			}

			if response == nil {
				t.Fatal("Expected non-nil response")
			}

			if response.Mode != tt.expectedMode {
				t.Errorf("Expected mode '%s', got '%s'", tt.expectedMode, response.Mode)
			}

			if tt.expectedMode == PlanningModeBoard {
				if response.Board == nil {
					t.Fatal("Expected non-nil board")
				}
				if len(response.Board.PrimaryTasks) != tt.expectedTasks {
					t.Errorf("Expected %d tasks, got %d", tt.expectedTasks, len(response.Board.PrimaryTasks))
				}
			}

			if tt.expectedPlan > 0 && len(response.Plan) != tt.expectedPlan {
				t.Errorf("Expected %d plan steps, got %d", tt.expectedPlan, len(response.Plan))
			}
		})
	}
}
