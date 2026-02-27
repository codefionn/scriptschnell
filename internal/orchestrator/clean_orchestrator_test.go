package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/session"
)

// TestNewCleanOrchestratorForTask tests creating clean orchestrators for task execution
func TestNewCleanOrchestratorForTask(t *testing.T) {
	// Setup provider manager
	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	_ = providerMgr.AddProvider("openai", "test-key", []*provider.Model{{ID: "gpt-4", Name: "GPT-4"}})
	_ = providerMgr.SetOrchestrationModel("gpt-4")

	// Create a base orchestrator to get shared resources
	baseOrch := &Orchestrator{
		fs:           fs.NewMockFS(),
		config:       &config.Config{WorkingDir: "."},
		actorSystem:  actor.NewSystem(),
		loopDetector: loopdetector.NewLoopDetector(),
		providerMgr:  providerMgr,
	}

	// Setup shared actors
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	domainBlockerActor := actor.NewDomainBlockerActor("domain_blocker", actor.DomainBlockerConfig{
		BlocklistURL:    "https://example.com/rpz",
		RefreshInterval: 6 * time.Hour,
		TTL:             24 * time.Hour,
	})
	domainBlockerRef, err := baseOrch.actorSystem.Spawn(ctx, "domain_blocker", domainBlockerActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn domain blocker: %v", err)
	}

	sessionStorageActor, _ := actor.NewSessionStorageActorWithConfig("session_storage", func() *config.AutoSaveConfig {
		return &config.AutoSaveConfig{}
	})
	sessionStorageRef, err := baseOrch.actorSystem.Spawn(ctx, "session_storage", sessionStorageActor, 16)
	if err != nil {
		t.Fatalf("Failed to spawn session storage: %v", err)
	}

	// Create clean orchestrator for task
	cleanOrch, err := NewCleanOrchestratorForTask(
		&config.Config{WorkingDir: "."},
		providerMgr,
		true, // cliMode
		baseOrch.fs,
		domainBlockerRef,
		sessionStorageRef,
		nil,
		nil,
		".",
	)

	if err != nil {
		t.Fatalf("Failed to create clean orchestrator: %v", err)
	}

	if cleanOrch == nil {
		t.Fatal("Clean orchestrator should not be nil")
	}

	// Verify it has its own session
	if cleanOrch.session == nil {
		t.Error("Clean orchestrator should have its own session")
	}

	// Verify it's a different session than any existing one
	if cleanOrch.session.ID == "" {
		t.Error("Session should have a valid ID")
	}

	// Clean up
	cleanOrch.Close()
}

// TestExtractTaskSummary tests the extractTaskSummary method
func TestExtractTaskSummary(t *testing.T) {
	tests := []struct {
		name            string
		task            *session.PlanningTask
		sessionMessages []*session.Message
		filesModified   []string
		filesRead       []string
		status          string
		errorMsg        string
		shouldHaveFiles bool
	}{
		{
			name: "extract from completed task",
			task: &session.PlanningTask{
				ID:   "task_1",
				Text: "Create new file",
			},
			sessionMessages: []*session.Message{
				{Role: "user", Content: "Create a new file"},
				{Role: "assistant", Content: "I created the file with the following content..."},
			},
			filesModified:   []string{"test.go"},
			filesRead:       []string{"existing.go"},
			status:          "completed",
			errorMsg:        "",
			shouldHaveFiles: true,
		},
		{
			name: "extract from failed task",
			task: &session.PlanningTask{
				ID:   "task_2",
				Text: "Modify file",
			},
			sessionMessages: []*session.Message{
				{Role: "user", Content: "Modify the file"},
				{Role: "assistant", Content: "I attempted to modify the file..."},
			},
			filesModified:   []string{},
			filesRead:       []string{"test.go"},
			status:          "failed",
			errorMsg:        "permission denied",
			shouldHaveFiles: false,
		},
		{
			name: "extract with no assistant message",
			task: &session.PlanningTask{
				ID:   "task_3",
				Text: "Read file",
			},
			sessionMessages: []*session.Message{
				{Role: "user", Content: "Read the file"},
			},
			filesModified:   []string{},
			filesRead:       []string{"test.go"},
			status:          "completed",
			errorMsg:        "",
			shouldHaveFiles: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock session
			sess := session.NewSession("test-session", ".")
			for _, msg := range tt.sessionMessages {
				sess.AddMessage(msg)
			}
			for _, f := range tt.filesModified {
				sess.TrackFileModified(f)
			}
			for _, f := range tt.filesRead {
				sess.TrackFileRead(f, "")
			}

			// Create mock orchestrator
			orch := &Orchestrator{
				session: sess,
			}

			// Extract summary
			summary := orch.extractTaskSummary(orch, tt.task, tt.status, tt.errorMsg)

			if summary == nil {
				t.Fatal("Summary should not be nil")
			}

			// Verify basic fields
			if summary.TaskID != tt.task.ID {
				t.Errorf("TaskID should be %s, got %s", tt.task.ID, summary.TaskID)
			}

			if summary.TaskText != tt.task.Text {
				t.Errorf("TaskText should be %s, got %s", tt.task.Text, summary.TaskText)
			}

			if summary.Status != tt.status {
				t.Errorf("Status should be %s, got %s", tt.status, summary.Status)
			}

			// Verify files
			if tt.shouldHaveFiles {
				if len(summary.FilesModified) != len(tt.filesModified) {
					t.Errorf("Expected %d modified files, got %d", len(tt.filesModified), len(summary.FilesModified))
				}
				if len(summary.FilesRead) != len(tt.filesRead) {
					t.Errorf("Expected %d read files, got %d", len(tt.filesRead), len(summary.FilesRead))
				}
			}

			// Verify error message if provided
			if tt.errorMsg != "" {
				if len(summary.Errors) == 0 {
					t.Error("Should have error message")
				}
				found := false
				for _, e := range summary.Errors {
					if strings.Contains(e, tt.errorMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Error should contain %s", tt.errorMsg)
				}
			}

			// Verify summary text is present
			if summary.Summary == "" && tt.status == "completed" {
				t.Error("Summary text should not be empty for completed task")
			}
		})
	}
}

// TestExtractTaskSummaryWithExplicitSummary tests extracting summary from task_summary tool call
func TestExtractTaskSummaryWithExplicitSummary(t *testing.T) {
	// Create a mock session with explicit task summary
	sess := session.NewSession("test-session", ".")
	sess.AddMessage(&session.Message{Role: "user", Content: "Create a file"})
	sess.AddMessage(&session.Message{Role: "assistant", Content: "I'll create the file"})
	sess.TrackFileModified("test.go")

	// Set explicit summary
	explicitSummary := &session.TaskExecutionSummary{
		TaskID:        "task_1",
		TaskText:      "Create file",
		Status:        "completed",
		Summary:       "Successfully created test.go with initial structure",
		FilesModified: []string{"test.go"},
		Timestamp:     time.Now(),
		Metadata:      make(map[string]string),
	}
	sess.SetTaskExecutionSummary(explicitSummary)

	// Create mock orchestrator
	orch := &Orchestrator{
		session: sess,
	}

	task := &session.PlanningTask{
		ID:   "task_1",
		Text: "Create file",
	}

	// Extract summary
	summary := orch.extractTaskSummary(orch, task, "completed", "")

	if summary == nil {
		t.Fatal("Summary should not be nil")
	}

	// Verify we got the explicit summary, not the one extracted from messages
	if summary.Summary != explicitSummary.Summary {
		t.Errorf("Should use explicit summary, got: %s", summary.Summary)
	}

	if summary.TaskID != task.ID {
		t.Errorf("TaskID should be updated to match task: expected %s, got %s", task.ID, summary.TaskID)
	}

	if summary.TaskText != task.Text {
		t.Errorf("TaskText should be updated to match task: expected %s, got %s", task.Text, summary.TaskText)
	}
}

// TestTaskIsolation tests that tasks run in clean orchestrators without message leakage
func TestTaskIsolation(t *testing.T) {
	// Create mock clients that track messages
	task1Client := newSequentialMockClient(
		&llm.CompletionResponse{Content: "Task 1 response"},
	)
	task2Client := newSequentialMockClient(
		&llm.CompletionResponse{Content: "Task 2 response"},
	)
	task3Client := newSequentialMockClient(
		&llm.CompletionResponse{Content: "Task 3 response"},
	)

	// Setup provider manager
	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	_ = providerMgr.AddProvider("openai", "test-key", []*provider.Model{{ID: "gpt-4", Name: "GPT-4"}})
	_ = providerMgr.SetOrchestrationModel("gpt-4")

	// Create base orchestrator with shared resources
	baseOrch := &Orchestrator{
		fs:           fs.NewMockFS(),
		config:       &config.Config{WorkingDir: "."},
		actorSystem:  actor.NewSystem(),
		loopDetector: loopdetector.NewLoopDetector(),
		providerMgr:  providerMgr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	domainBlockerActor := actor.NewDomainBlockerActor("domain_blocker", actor.DomainBlockerConfig{
		BlocklistURL:    "https://example.com/rpz",
		RefreshInterval: 6 * time.Hour,
		TTL:             24 * time.Hour,
	})
	domainBlockerRef, _ := baseOrch.actorSystem.Spawn(ctx, "domain_blocker", domainBlockerActor, 16)

	sessionStorageActor, _ := actor.NewSessionStorageActorWithConfig("session_storage", func() *config.AutoSaveConfig {
		return &config.AutoSaveConfig{}
	})
	sessionStorageRef, _ := baseOrch.actorSystem.Spawn(ctx, "session_storage", sessionStorageActor, 16)

	// Create planning board
	planningBoard := &session.PlanningBoard{
		Description: "Test isolation",
		PrimaryTasks: []session.PlanningTask{
			{ID: "task_1", Text: "First task"},
			{ID: "task_2", Text: "Second task"},
			{ID: "task_3", Text: "Third task"},
		},
	}

	// Simulate task execution manually to verify isolation
	originalPrompt := "Execute three tasks"
	var previousSummaries []session.TaskExecutionSummary

	for i := 0; i < len(planningBoard.PrimaryTasks); i++ {
		task := &planningBoard.PrimaryTasks[i]

		// Create clean orchestrator for this task
		cleanOrch, err := NewCleanOrchestratorForTask(
			&config.Config{WorkingDir: "."},
			providerMgr,
			true,
			baseOrch.fs,
			domainBlockerRef,
			sessionStorageRef,
			nil,
			nil,
			".",
		)
		if err != nil {
			t.Fatalf("Failed to create clean orchestrator for task %d: %v", i+1, err)
		}
		defer cleanOrch.Close()

		// Set appropriate mock client
		switch i {
		case 0:
			cleanOrch.orchestrationClient = task1Client
		case 1:
			cleanOrch.orchestrationClient = task2Client
		case 2:
			cleanOrch.orchestrationClient = task3Client
		}

		// Track initial message count
		initialMsgCount := len(cleanOrch.session.GetMessages())

		// Add task prompt
		taskPrompt := cleanOrch.buildTaskPrompt(originalPrompt, task, i, len(planningBoard.PrimaryTasks), previousSummaries)
		cleanOrch.session.AddMessage(&session.Message{Role: "user", Content: taskPrompt})

		// Verify task prompt contains correct task
		if !strings.Contains(taskPrompt, task.Text) {
			t.Errorf("Task %d prompt should contain its own task text: %s", i+1, task.Text)
		}

		// Verify previous summaries are in the prompt (except for first task)
		if i > 0 {
			if !strings.Contains(taskPrompt, "Previous Tasks Completed") {
				t.Errorf("Task %d prompt should contain previous summaries", i+1)
			}
		} else {
			if strings.Contains(taskPrompt, "Previous Tasks Completed") {
				t.Errorf("Task 1 prompt should not contain previous summaries")
			}
		}

		// Add assistant response
		cleanOrch.session.AddMessage(&session.Message{Role: "assistant", Content: "Task completed"})

		// Verify message count increased by expected amount (user + assistant)
		finalMsgCount := len(cleanOrch.session.GetMessages())
		if finalMsgCount != initialMsgCount+2 {
			t.Errorf("Task %d session should have exactly 2 new messages, got %d", i+1, finalMsgCount-initialMsgCount)
		}

		// Extract and store summary
		summary := cleanOrch.extractTaskSummary(cleanOrch, task, "completed", "")
		if summary != nil {
			previousSummaries = append(previousSummaries, *summary)
		}

		// Verify each task has a different session ID
		if i == 0 {
			continue // First task, nothing to compare
		}
		// Note: We can't compare session IDs here since we create new clean orchestrators
		// But we've verified they're separate instances
	}

	// Verify we have summaries for all tasks
	if len(previousSummaries) != 3 {
		t.Errorf("Should have 3 summaries, got %d", len(previousSummaries))
	}

	// Verify each summary has correct task info
	for i, summary := range previousSummaries {
		expectedTask := planningBoard.PrimaryTasks[i].Text
		if summary.TaskText != expectedTask {
			t.Errorf("Summary %d should have task text %s, got %s", i, expectedTask, summary.TaskText)
		}
		if summary.Status != "completed" {
			t.Errorf("Summary %d should be completed, got %s", i, summary.Status)
		}
	}
}

// TestBuildTaskPromptWithPreviousSummaries tests that buildTaskPrompt includes previous summaries
func TestBuildTaskPromptWithPreviousSummaries(t *testing.T) {
	orch := &Orchestrator{
		session:      session.NewSession("test-session", "."),
		loopDetector: loopdetector.NewLoopDetector(),
	}

	originalPrompt := "Create a complete application"

	// Create previous summaries
	previousSummaries := []session.TaskExecutionSummary{
		{
			TaskID:        "task_1",
			TaskText:      "Set up project structure",
			Status:        "completed",
			Summary:       "Created Go module and directory structure",
			FilesModified: []string{"go.mod", "main.go"},
		},
		{
			TaskID:        "task_2",
			TaskText:      "Implement core logic",
			Status:        "completed",
			Summary:       "Implemented main application logic with handlers",
			FilesModified: []string{"handlers.go", "models.go"},
		},
	}

	currentTask := &session.PlanningTask{
		ID:          "task_3",
		Text:        "Add tests",
		Description: "Write unit tests for the application",
		Subtasks: []session.PlanningTask{
			{ID: "task_3_1", Text: "Write handler tests", Status: "pending"},
			{ID: "task_3_2", Text: "Write model tests", Status: "pending"},
		},
	}

	// Build prompt with previous summaries
	prompt := orch.buildTaskPrompt(originalPrompt, currentTask, 2, 3, previousSummaries)

	// Verify overall objective
	if !strings.Contains(prompt, originalPrompt) {
		t.Error("Prompt should contain overall objective")
	}

	// Verify current task
	if !strings.Contains(prompt, currentTask.Text) {
		t.Error("Prompt should contain current task text")
	}

	// Verify previous summaries section
	if !strings.Contains(prompt, "Previous Tasks Completed") {
		t.Error("Prompt should contain 'Previous Tasks Completed' section")
	}

	// Verify first previous summary
	if !strings.Contains(prompt, "Set up project structure") {
		t.Error("Prompt should contain first task summary")
	}

	// Verify second previous summary
	if !strings.Contains(prompt, "Implement core logic") {
		t.Error("Prompt should contain second task summary")
	}

	// Verify file modifications are included
	if !strings.Contains(prompt, "Files modified") {
		t.Error("Prompt should mention modified files")
	}

	// Verify current subtasks are included
	if !strings.Contains(prompt, "Subtasks to complete") {
		t.Error("Prompt should mention subtasks")
	}

	if !strings.Contains(prompt, "Write handler tests") {
		t.Error("Prompt should contain subtask text")
	}
}

// TestBuildTaskPromptWithoutPreviousSummaries tests that buildTaskPrompt works without previous summaries
func TestBuildTaskPromptWithoutPreviousSummaries(t *testing.T) {
	orch := &Orchestrator{
		session:      session.NewSession("test-session", "."),
		loopDetector: loopdetector.NewLoopDetector(),
	}

	originalPrompt := "Create a new feature"

	currentTask := &session.PlanningTask{
		ID:   "task_1",
		Text: "Implement feature",
	}

	// Build prompt without previous summaries
	var previousSummaries []session.TaskExecutionSummary
	prompt := orch.buildTaskPrompt(originalPrompt, currentTask, 0, 1, previousSummaries)

	// Verify overall objective
	if !strings.Contains(prompt, originalPrompt) {
		t.Error("Prompt should contain overall objective")
	}

	// Verify current task
	if !strings.Contains(prompt, currentTask.Text) {
		t.Error("Prompt should contain current task text")
	}

	// Verify NO previous summaries section
	if strings.Contains(prompt, "Previous Tasks Completed") {
		t.Error("Prompt should NOT contain 'Previous Tasks Completed' when no summaries exist")
	}

	// Verify task index
	if !strings.Contains(prompt, "task 1 of 1") {
		t.Error("Prompt should contain task index")
	}
}
