package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// TestBuildTaskPrompt tests the buildTaskPrompt method
func TestBuildTaskPrompt(t *testing.T) {
	orch := &Orchestrator{
		session:      session.NewSession("test-session", "."),
		loopDetector: loopdetector.NewLoopDetector(),
	}

	originalPrompt := "Create a REST API service with authentication"

	tests := []struct {
		name           string
		task           *session.PlanningTask
		taskIndex      int
		totalTasks     int
		expectedSubstr string
	}{
		{
			name: "simple task with subtasks",
			task: &session.PlanningTask{
				ID:          "task_1",
				Text:        "Set up project structure",
				Priority:    "high",
				Description: "Initialize Go module and create directories",
				Subtasks: []session.PlanningTask{
					{ID: "task_1_1", Text: "Initialize Go module", Status: "pending"},
					{ID: "task_1_2", Text: "Create directory structure", Status: "pending"},
				},
			},
			taskIndex:      0,
			totalTasks:     3,
			expectedSubstr: "task 1 of 3",
		},
		{
			name: "task with completed subtasks",
			task: &session.PlanningTask{
				ID:   "task_2",
				Text: "Implement authentication",
				Subtasks: []session.PlanningTask{
					{ID: "task_2_1", Text: "Create auth middleware", Status: "completed"},
					{ID: "task_2_2", Text: "Add JWT handling", Status: "pending"},
				},
			},
			taskIndex:      1,
			totalTasks:     3,
			expectedSubstr: "[x]",
		},
		{
			name: "task without subtasks",
			task: &session.PlanningTask{
				ID:       "task_3",
				Text:     "Write unit tests",
				Priority: "medium",
			},
			taskIndex:      2,
			totalTasks:     3,
			expectedSubstr: "task 3 of 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := orch.buildTaskPrompt(originalPrompt, tt.task, tt.taskIndex, tt.totalTasks)

			// Check overall objective is included
			if !strings.Contains(prompt, originalPrompt) {
				t.Errorf("prompt should contain overall objective")
			}

			// Check task text is included
			if !strings.Contains(prompt, tt.task.Text) {
				t.Errorf("prompt should contain task text: %s", tt.task.Text)
			}

			// Check task index is included (case-insensitive)
			lowerPrompt := strings.ToLower(prompt)
			if !strings.Contains(lowerPrompt, tt.expectedSubstr) {
				t.Errorf("prompt should contain %s, got: %s", tt.expectedSubstr, prompt)
			}

			// Check subtasks are included if present
			if len(tt.task.Subtasks) > 0 {
				if !strings.Contains(prompt, "Subtasks") {
					t.Errorf("prompt should mention subtasks when present")
				}
				for _, subtask := range tt.task.Subtasks {
					if !strings.Contains(prompt, subtask.Text) {
						t.Errorf("prompt should contain subtask: %s", subtask.Text)
					}
				}
			}
		})
	}
}

// TestExecutePlanningBoardSerialExecution tests that executePlanningBoard executes tasks serially
func TestExecutePlanningBoardSerialExecution(t *testing.T) {
	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Setting up project structure...",
		},
		&llm.CompletionResponse{
			Content: "Project structure completed.",
		},
		&llm.CompletionResponse{
			Content: "Implementing authentication...",
		},
		&llm.CompletionResponse{
			Content: "Authentication completed.",
		},
		&llm.CompletionResponse{
			Content: "Writing unit tests...",
		},
		&llm.CompletionResponse{
			Content: "All tests passed.",
		},
	)

	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	_ = providerMgr.AddProvider("openai", "test-key", []*provider.Model{{ID: "gpt-4", Name: "GPT-4"}})
	_ = providerMgr.SetOrchestrationModel("gpt-4")

	orch := &Orchestrator{
		session:             session.NewSession("test-session", "."),
		orchestrationClient: mockClient,
		fs:                  fs.NewMockFS(),
		config:              &config.Config{},
		loopDetector:        loopdetector.NewLoopDetector(),
		providerMgr:         providerMgr,
		toolRegistry:        tools.NewRegistry(nil),
	}

	planningBoard := &session.PlanningBoard{
		Description: "Create a REST API service",
		PrimaryTasks: []session.PlanningTask{
			{
				ID:       "task_1",
				Text:     "Set up project structure",
				Priority: "high",
				Subtasks: []session.PlanningTask{
					{ID: "task_1_1", Text: "Initialize Go module"},
					{ID: "task_1_2", Text: "Create directories"},
				},
			},
			{
				ID:       "task_2",
				Text:     "Implement authentication",
				Priority: "high",
				Subtasks: []session.PlanningTask{
					{ID: "task_2_1", Text: "Create auth middleware"},
					{ID: "task_2_2", Text: "Add JWT handling"},
				},
			},
			{
				ID:       "task_3",
				Text:     "Write unit tests",
				Priority: "medium",
			},
		},
	}

	ctx := context.Background()
	err = orch.executePlanningBoard(
		ctx,
		"Create a REST API service with authentication",
		dummyProgressCallback,
		dummyContextUsageCallback,
		dummyAuthCallback,
		dummyToolCallCallback,
		dummyToolResultCallback,
		dummyOpenRouterUsageCallback,
		planningBoard,
	)

	if err != nil {
		t.Fatalf("executePlanningBoard failed: %v", err)
	}

	// Verify all tasks were marked as completed
	for i, task := range planningBoard.PrimaryTasks {
		if task.Status != "completed" {
			t.Errorf("Task %d (%s) should be completed, got status: %s", i+1, task.Text, task.Status)
		}
	}

	// Verify execution order (should be 3 tasks with their subtasks)
	if mockClient.RequestCount() != 3 {
		t.Errorf("Expected 3 LLM requests (one per primary task), got %d", mockClient.RequestCount())
	}

	// Verify each request contained the appropriate task context
	for i, req := range mockClient.requests {
		if len(req.Messages) == 0 {
			t.Errorf("Request %d should have messages", i)
			continue
		}

		// Last message should be the task prompt
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role != "user" {
			t.Errorf("Request %d last message should be user role", i)
		}

		// Verify task-specific context
		expectedTask := planningBoard.PrimaryTasks[i].Text
		if !strings.Contains(lastMsg.Content, expectedTask) {
			t.Errorf("Request %d should contain task text: %s", i, expectedTask)
		}

		// Verify subtasks are mentioned
		if len(planningBoard.PrimaryTasks[i].Subtasks) > 0 {
			if !strings.Contains(lastMsg.Content, "Subtasks") {
				t.Errorf("Request %d should mention subtasks", i)
			}
		}
	}
}

// TestExecutePlanningBoardSkipsCompletedTasks tests that already completed tasks are skipped
func TestExecutePlanningBoardSkipsCompletedTasks(t *testing.T) {
	mockClient := newSequentialMockClient(
		&llm.CompletionResponse{
			Content: "Implementing authentication...",
		},
		&llm.CompletionResponse{
			Content: "Authentication completed.",
		},
	)

	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	_ = providerMgr.AddProvider("openai", "test-key", []*provider.Model{{ID: "gpt-4", Name: "GPT-4"}})
	_ = providerMgr.SetOrchestrationModel("gpt-4")

	orch := &Orchestrator{
		session:             session.NewSession("test-session", "."),
		orchestrationClient: mockClient,
		fs:                  fs.NewMockFS(),
		config:              &config.Config{},
		loopDetector:        loopdetector.NewLoopDetector(),
		providerMgr:         providerMgr,
		toolRegistry:        tools.NewRegistry(nil),
	}

	planningBoard := &session.PlanningBoard{
		Description: "Create a REST API service",
		PrimaryTasks: []session.PlanningTask{
			{
				ID:     "task_1",
				Text:   "Set up project structure",
				Status: "completed", // Already completed
			},
			{
				ID:     "task_2",
				Text:   "Implement authentication",
				Status: "pending",
			},
			{
				ID:     "task_3",
				Text:   "Write unit tests",
				Status: "pending",
			},
		},
	}

	ctx := context.Background()
	err = orch.executePlanningBoard(
		ctx,
		"Create a REST API service",
		dummyProgressCallback,
		dummyContextUsageCallback,
		dummyAuthCallback,
		dummyToolCallCallback,
		dummyToolResultCallback,
		dummyOpenRouterUsageCallback,
		planningBoard,
	)

	if err != nil {
		t.Fatalf("executePlanningBoard failed: %v", err)
	}

	// Only task 2 and 3 should be executed (task 1 was already completed)
	// Note: We'll get 2 responses from our mock client
	// The mock will use default responses for task 3
	if mockClient.RequestCount() != 2 {
		t.Errorf("Expected 2 LLM requests (tasks 2 and 3), got %d", mockClient.RequestCount())
	}

	// Verify task 1 remains completed
	if planningBoard.PrimaryTasks[0].Status != "completed" {
		t.Errorf("Task 1 should remain completed, got: %s", planningBoard.PrimaryTasks[0].Status)
	}

	// Verify task 2 is completed
	if planningBoard.PrimaryTasks[1].Status != "completed" {
		t.Errorf("Task 2 should be completed, got: %s", planningBoard.PrimaryTasks[1].Status)
	}

	// Verify task 3 is completed
	if planningBoard.PrimaryTasks[2].Status != "completed" {
		t.Errorf("Task 3 should be completed, got: %s", planningBoard.PrimaryTasks[2].Status)
	}
}

// TestExecutePlanningBoardWithEmptyBoard tests handling of empty planning board
func TestExecutePlanningBoardWithEmptyBoard(t *testing.T) {
	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	_ = providerMgr.AddProvider("openai", "test-key", []*provider.Model{{ID: "gpt-4", Name: "GPT-4"}})
	_ = providerMgr.SetOrchestrationModel("gpt-4")

	orch := &Orchestrator{
		session:             session.NewSession("test-session", "."),
		orchestrationClient: newSequentialMockClient(),
		fs:                  fs.NewMockFS(),
		config:              &config.Config{},
		loopDetector:        loopdetector.NewLoopDetector(),
		providerMgr:         providerMgr,
		toolRegistry:        tools.NewRegistry(nil),
	}

	planningBoard := &session.PlanningBoard{
		Description:  "Empty board",
		PrimaryTasks: []session.PlanningTask{},
	}

	ctx := context.Background()
	err = orch.executePlanningBoard(
		ctx,
		"Test prompt",
		dummyProgressCallback,
		dummyContextUsageCallback,
		dummyAuthCallback,
		dummyToolCallCallback,
		dummyToolResultCallback,
		dummyOpenRouterUsageCallback,
		planningBoard,
	)

	if err != nil {
		t.Fatalf("executePlanningBoard with empty board should succeed: %v", err)
	}
}

// TestExecutePlanningBoardWithNilBoard tests handling of nil planning board
func TestExecutePlanningBoardWithNilBoard(t *testing.T) {
	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	_ = providerMgr.AddProvider("openai", "test-key", []*provider.Model{{ID: "gpt-4", Name: "GPT-4"}})
	_ = providerMgr.SetOrchestrationModel("gpt-4")

	orch := &Orchestrator{
		session:             session.NewSession("test-session", "."),
		orchestrationClient: newSequentialMockClient(),
		fs:                  fs.NewMockFS(),
		config:              &config.Config{},
		loopDetector:        loopdetector.NewLoopDetector(),
		providerMgr:         providerMgr,
		toolRegistry:        tools.NewRegistry(nil),
	}

	ctx := context.Background()
	err = orch.executePlanningBoard(
		ctx,
		"Test prompt",
		dummyProgressCallback,
		dummyContextUsageCallback,
		dummyAuthCallback,
		dummyToolCallCallback,
		dummyToolResultCallback,
		dummyOpenRouterUsageCallback,
		nil,
	)

	if err != nil {
		t.Fatalf("executePlanningBoard with nil board should succeed: %v", err)
	}
}

// Helper functions

func dummyProgressCallback(update progress.Update) error {
	return nil
}

func dummyContextUsageCallback(freePercent int, contextWindow int) error {
	return nil
}

func dummyAuthCallback(toolName string, params map[string]interface{}, reason string) (bool, error) {
	return true, nil
}

func dummyToolCallCallback(toolName, toolID string, parameters map[string]interface{}) error {
	return nil
}

func dummyToolResultCallback(toolName, toolID, result, errorMsg string) error {
	return nil
}

func dummyOpenRouterUsageCallback(usage map[string]interface{}) error {
	return nil
}
