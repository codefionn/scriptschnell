package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// TaskTestCase represents an agentic task test case
type TaskTestCase struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`

	// Task setup
	TaskDir string `json:"task_dir"` // Directory containing task files
	Prompt  string `json:"prompt"`   // Initial prompt for the AI

	// Expected outcomes
	ExpectedResults map[string]interface{} `json:"expected_results"` // Expected files, outputs, etc.

	// Validation
	ValidationScript string                 `json:"validation_script"` // Script to validate the task
	SuccessCriteria  map[string]interface{} `json:"success_criteria"`  // Criteria for success

	// Task metadata
	Category   string                 `json:"category"`   // e.g., "coding", "reasoning", "agentic"
	Difficulty string                 `json:"difficulty"` // "easy", "medium", "hard"
	Timeout    time.Duration          `json:"timeout"`    // Task timeout
	Parameters map[string]interface{} `json:"parameters"` // Additional parameters
}

// TaskEvalResult represents the result of evaluating a task
type TaskEvalResult struct {
	// Basic info
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	TaskID       string `json:"task_id"`
	TaskName     string `json:"task_name"`
	TaskCategory string `json:"task_category"`

	// Execution
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`

	// Results
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	// Task-specific results
	GeneratedFiles    []GeneratedFile   `json:"generated_files,omitempty"`
	TaskOutput        string            `json:"task_output,omitempty"`
	ValidationResults ValidationResults `json:"validation_results,omitempty"`

	// LLM interaction info
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCost        float64 `json:"total_cost"`
	Currency         string  `json:"currency"`

	// Metadata
	Temperature float64 `json:"temperature"`
	Steps       int     `json:"steps"`      // Number of steps taken
	Iterations  int     `json:"iterations"` // Number of iterations/refinements
}

// GeneratedFile tracks files created during task execution
type GeneratedFile struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Type    string    `json:"type"` // "source", "binary", "config", etc.
}

// ValidationResults contains the results of task validation
type ValidationResults struct {
	Passed   bool            `json:"passed"`
	Tests    []TestResult    `json:"tests,omitempty"`
	Criteria map[string]bool `json:"criteria,omitempty"`
	Output   string          `json:"output,omitempty"`
	Duration time.Duration   `json:"duration"`
}

// TestResult represents a single test result from validation
type TestResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// TaskEvalClient handles agentic task evaluation
type TaskEvalClient struct {
	config    *EvalConfig
	llmClient *llm.OpenRouterClient
	tempDir   string
}

// NewTaskEvalClient creates a new task evaluation client
func NewTaskEvalClient(config *EvalConfig) (*TaskEvalClient, error) {
	if config.OpenRouterAPIKey == "" {
		return nil, fmt.Errorf("OpenRouter API key is required")
	}

	// Create temporary directory for task execution
	tempDir, err := os.MkdirTemp("", "scriptschnell-task-eval-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create LLM client (will be recreated per model)
	tempClient, err := llm.NewOpenRouterClient(config.OpenRouterAPIKey, "openai/gpt-4")
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	return &TaskEvalClient{
		config:    config,
		llmClient: tempClient.(*llm.OpenRouterClient),
		tempDir:   tempDir,
	}, nil
}

// Cleanup removes temporary files
func (c *TaskEvalClient) Cleanup() error {
	if c.tempDir != "" {
		return os.RemoveAll(c.tempDir)
	}
	return nil
}

// EvaluateTask runs a single task test case on a model
func (c *TaskEvalClient) EvaluateTask(ctx context.Context, model ModelConfig, task TaskTestCase) (*TaskEvalResult, error) {
	startTime := time.Now()

	log.Printf("Starting task evaluation: %s on model %s", task.ID, model.ID)

	// Create model-specific client
	client, err := llm.NewOpenRouterClient(c.config.OpenRouterAPIKey, model.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for model %s: %w", model.ID, err)
	}

	// Create task workspace
	taskDir, err := c.setupTaskWorkspace(task)
	if err != nil {
		return nil, fmt.Errorf("failed to setup task workspace: %w", err)
	}

	result := &TaskEvalResult{
		ModelID:      model.ID,
		ModelName:    model.DisplayName,
		TaskID:       task.ID,
		TaskName:     task.Name,
		TaskCategory: task.Category,
		StartTime:    startTime,
		Temperature:  c.config.Temperature,
	}

	// Execute the task
	err = c.executeTask(ctx, client, task, taskDir, result)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
	}

	// Validate the task
	if result.Success {
		validationResults, err := c.validateTask(task, taskDir)
		if err != nil {
			log.Printf("Task validation failed: %v", err)
			result.Success = false
			result.Error = fmt.Sprintf("Validation failed: %v", err)
		} else {
			result.ValidationResults = validationResults
			result.Success = validationResults.Passed
		}
	}

	// Collect generated files
	files, err := c.collectGeneratedFiles(taskDir)
	if err == nil {
		result.GeneratedFiles = files
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	// Cleanup task directory
	os.RemoveAll(taskDir)

	log.Printf("Task evaluation completed: %s on model %s, success=%v, duration=%v",
		task.ID, model.ID, result.Success, result.Duration)

	return result, nil
}

// setupTaskWorkspace creates a workspace for the task
func (c *TaskEvalClient) setupTaskWorkspace(task TaskTestCase) (string, error) {
	// Create task-specific directory in temp dir
	taskDir := filepath.Join(c.tempDir, task.ID)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return "", err
	}

	// Copy task files from the original task directory
	if task.TaskDir != "" {
		if _, err := os.Stat(task.TaskDir); os.IsNotExist(err) {
			return "", fmt.Errorf("task directory does not exist: %s", task.TaskDir)
		}

		// Copy directory contents
		cmd := exec.Command("cp", "-r", task.TaskDir+"/.", taskDir+"/")
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to copy task files: %w", err)
		}
	}

	return taskDir, nil
}

// executeTask runs the agentic task using scriptschnell
func (c *TaskEvalClient) executeTask(ctx context.Context, client llm.Client, task TaskTestCase, taskDir string, result *TaskEvalResult) error {
	// Create a scriptschnell prompt context with tools
	promptContext := fmt.Sprintf(`You are an AI programming assistant. Complete the following task:

TASK: %s
DESCRIPTION: %s

You have access to the following tools:
- read_file: Read files from the workspace
- create_file: Create new files 
- write_file_diff: Update existing files
- go_sandbox: Execute Go code in a sandbox
- todo: Manage todo items for planning

Please complete this task step by step:
1. Analyze the requirements
2. Plan your approach
3. Implement the solution
4. Test your implementation
5. Make any necessary adjustments

Workspace directory: %s

Start by examining the current workspace and understanding what needs to be done.

%s`, task.Name, task.Description, taskDir, task.Prompt)

	// For now, we'll simulate the agentic execution through LLM interaction
	// In a full implementation, this would integrate with scriptschnell's actor system

	// Create completion request
	req := &llm.CompletionRequest{
		Messages: []*llm.Message{
			{Role: "user", Content: promptContext},
		},
		Temperature: c.config.Temperature,
		MaxTokens:   c.config.MaxTokens,
	}

	// Execute with timeout
	taskTimeout := task.Timeout
	if taskTimeout == 0 {
		taskTimeout = c.config.Timeout
	}

	evalCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	resp, err := client.CompleteWithRequest(evalCtx, req)
	if err != nil {
		return fmt.Errorf("LLM completion failed: %w", err)
	}

	result.TaskOutput = resp.Content

	// Extract token usage and cost
	if resp.Usage != nil {
		if promptTokens, ok := resp.Usage["prompt_tokens"].(float64); ok {
			result.PromptTokens = int(promptTokens)
		}
		if completionTokens, ok := resp.Usage["completion_tokens"].(float64); ok {
			result.CompletionTokens = int(completionTokens)
		}
		if totalTokens, ok := resp.Usage["total_tokens"].(float64); ok {
			result.TotalTokens = int(totalTokens)
		}
		if totalCost, ok := resp.Usage["total_cost"].(float64); ok {
			result.TotalCost = totalCost
		}
		if currency, ok := resp.Usage["currency"].(string); ok {
			result.Currency = currency
		} else {
			result.Currency = "USD"
		}
	}

	return nil
}

// validateTask runs validation scripts and checks success criteria
func (c *TaskEvalClient) validateTask(task TaskTestCase, taskDir string) (ValidationResults, error) {
	startTime := time.Now()

	results := ValidationResults{
		Passed:   false,
		Tests:    []TestResult{},
		Criteria: make(map[string]bool),
	}

	// Run validation script if provided
	if task.ValidationScript != "" {
		cmd := exec.Command("bash", "-c", task.ValidationScript)
		cmd.Dir = taskDir

		output, err := cmd.CombinedOutput()
		results.Output = string(output)

		if err != nil {
			results.Duration = time.Since(startTime)
			return results, fmt.Errorf("validation script failed: %w", err)
		}
	}

	// Check success criteria
	for criterion, expected := range task.SuccessCriteria {
		passed := c.checkSuccessCriterion(criterion, expected, taskDir)
		results.Criteria[criterion] = passed
		if !passed {
			results.Duration = time.Since(startTime)
			return results, nil // Failed criteria
		}
	}

	results.Passed = true
	results.Duration = time.Since(startTime)

	return results, nil
}

// checkSuccessCriterion checks a single success criterion
func (c *TaskEvalClient) checkSuccessCriterion(criterion string, expected interface{}, taskDir string) bool {
	switch criterion {
	case "file_exists":
		if filename, ok := expected.(string); ok {
			_, err := os.Stat(filepath.Join(taskDir, filename))
			return err == nil
		}
	case "contains_code":
		if searchStr, ok := expected.(string); ok {
			// Search for string in all .go files
			matches, _ := c.searchInFiles(taskDir, "*.go", searchStr)
			return len(matches) > 0
		}
	case "compiles":
		// Try to compile Go code
		cmd := exec.Command("go", "build", ".")
		cmd.Dir = taskDir
		err := cmd.Run()
		return err == nil
	case "tests_pass":
		// Run Go tests
		cmd := exec.Command("go", "test", ".", "-v")
		cmd.Dir = taskDir
		err := cmd.Run()
		return err == nil
	}

	return false
}

// searchInFiles searches for a pattern in files matching a glob
func (c *TaskEvalClient) searchInFiles(dir, glob, pattern string) ([]string, error) {
	cmd := exec.Command("grep", "-r", "--include="+glob, pattern, dir)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var matches []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			matches = append(matches, line)
		}
	}

	return matches, nil
}

// collectGeneratedFiles collects information about files created during task execution
func (c *TaskEvalClient) collectGeneratedFiles(taskDir string) ([]GeneratedFile, error) {
	var files []GeneratedFile

	err := filepath.Walk(taskDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Skip hidden files and common build artifacts
		if strings.HasPrefix(info.Name(), ".") ||
			info.Name() == "go.sum" ||
			strings.HasSuffix(info.Name(), "~") {
			return nil
		}

		relPath, err := filepath.Rel(taskDir, path)
		if err != nil {
			return err
		}

		fileType := "other"
		switch {
		case strings.HasSuffix(relPath, ".go"):
			fileType = "source"
		case strings.HasSuffix(relPath, ".sh"):
			fileType = "script"
		case strings.HasSuffix(relPath, ".json"):
			fileType = "config"
		case info.IsDir() == false && strings.IndexByte(relPath, '.') == -1:
			// No extension, likely binary
			fileType = "binary"
		}

		files = append(files, GeneratedFile{
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Type:    fileType,
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// LoadTaskTestCase loads a task test case from a file
func LoadTaskTestCase(configPath string) (TaskTestCase, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return TaskTestCase{}, fmt.Errorf("failed to read task config: %w", err)
	}

	var task TaskTestCase
	if err := json.Unmarshal(data, &task); err != nil {
		return TaskTestCase{}, fmt.Errorf("failed to parse task config: %w", err)
	}

	// Validate and set defaults
	if err := task.Validate(); err != nil {
		return TaskTestCase{}, fmt.Errorf("invalid task config: %w", err)
	}

	return task, nil
}

// Validate validates a task configuration
func (tc *TaskTestCase) Validate() error {
	if tc.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	if tc.Name == "" {
		return fmt.Errorf("task name is required")
	}
	if tc.TaskDir == "" {
		// TaskDir can be empty if task starts from scratch
	}
	if tc.Prompt == "" {
		return fmt.Errorf("task prompt is required")
	}
	if tc.Category == "" {
		return fmt.Errorf("task category is required")
	}
	if tc.Timeout == 0 {
		tc.Timeout = 5 * time.Minute // Default timeout
	}
	return nil
}

// LoadTaskTestCasesFromDir loads all task test cases from a directory
func LoadTaskTestCasesFromDir(dir string) ([]TaskTestCase, error) {
	var tasks []TaskTestCase

	// Look for *.json files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read task directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		taskPath := filepath.Join(dir, entry.Name())
		task, err := LoadTaskTestCase(taskPath)
		if err != nil {
			log.Printf("Error loading task from %s: %v", taskPath, err)
			continue
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}
