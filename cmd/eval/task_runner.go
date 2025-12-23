package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// TaskEvalRunner coordinates task-based evaluation across multiple models
type TaskEvalRunner struct {
	config       *EvalConfig
	client       *TaskEvalClient
	results      []TaskEvalResult
	progress     *TaskEvalProgress
	progressMux  sync.RWMutex
	listeners    []TaskProgressListener
	listenersMux sync.RWMutex
}

// TaskEvalProgress tracks the overall progress of task evaluation
type TaskEvalProgress struct {
	StartTime       time.Time  `json:"start_time"`
	EndTime         *time.Time `json:"end_time,omitempty"`
	Status          string     `json:"status"` // "running", "completed", "failed", "cancelled"
	TotalModels     int        `json:"total_models"`
	CompletedModels int        `json:"completed_models"`
	CurrentModel    string     `json:"current_model,omitempty"`
	CurrentTask     string     `json:"current_task,omitempty"`
	TotalTasks      int        `json:"total_tasks"`
	CompletedTasks  int        `json:"completed_tasks"`
	ResultsCount    int        `json:"results_count"`
	TotalCost       float64    `json:"total_cost"`
	TotalTokens     int        `json:"total_tokens"`
	Error           string     `json:"error,omitempty"`
}

// TaskProgressListener receives progress updates during task evaluation
type TaskProgressListener interface {
	OnTaskProgressUpdate(progress TaskEvalProgress)
	OnTaskResult(result TaskEvalResult)
}

// NewTaskEvalRunner creates a new task evaluation runner
func NewTaskEvalRunner(config *EvalConfig) (*TaskEvalRunner, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	client, err := NewTaskEvalClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create task eval client: %w", err)
	}

	return &TaskEvalRunner{
		config:  config,
		client:  client,
		results: make([]TaskEvalResult, 0),
		progress: &TaskEvalProgress{
			Status: "initialized",
		},
	}, nil
}

// Run starts the task evaluation process
func (r *TaskEvalRunner) Run(ctx context.Context, tasks []TaskTestCase) error {
	r.updateProgress(func(p *TaskEvalProgress) {
		p.StartTime = time.Now()
		p.Status = "running"
		p.TotalModels = len(r.config.GetEnabledModels())
		p.TotalTasks = len(tasks)
		p.CompletedModels = 0
		p.CompletedTasks = 0
		p.ResultsCount = 0
		p.TotalCost = 0.0
		p.TotalTokens = 0
	})

	log.Printf("Starting task evaluation with %d models and %d tasks",
		r.progress.TotalModels, r.progress.TotalTasks)

	// Evaluate each model on all tasks
	enabledModels := r.config.GetEnabledModels()
	for i, model := range enabledModels {
		r.updateProgress(func(p *TaskEvalProgress) {
			p.CurrentModel = model.ID
			p.CurrentTask = ""
			p.CompletedModels = i
		})

		modelResults, err := r.evaluateModelOnTasks(ctx, model, tasks)
		if err != nil {
			log.Printf("Error evaluating model %s: %v", model.ID, err)
			r.updateProgress(func(p *TaskEvalProgress) {
				p.Error = fmt.Sprintf("Model %s failed: %v", model.ID, err)
			})
			// Continue with other models even if one fails
		} else {
			r.addResults(modelResults)
		}

		// Check if context was cancelled
		select {
		case <-ctx.Done():
			r.updateProgress(func(p *TaskEvalProgress) {
				p.Status = "cancelled"
				if p.EndTime == nil {
					endTime := time.Now()
					p.EndTime = &endTime
				}
			})
			return ctx.Err()
		default:
		}
	}

	// Final progress update
	r.updateProgress(func(p *TaskEvalProgress) {
		p.Status = "completed"
		p.CompletedModels = len(enabledModels)
		p.CurrentModel = ""
		p.CurrentTask = ""
		p.ResultsCount = len(r.results)
		endTime := time.Now()
		p.EndTime = &endTime

		// Calculate totals
		for _, result := range r.results {
			if result.Success {
				p.TotalCost += result.TotalCost
				p.TotalTokens += result.TotalTokens
			}
		}
	})

	log.Printf("Task evaluation completed: %d results, total cost $%.6f, total tokens %d",
		len(r.results), r.progress.TotalCost, r.progress.TotalTokens)

	return nil
}

// evaluateModelOnTasks runs all tasks on a single model
func (r *TaskEvalRunner) evaluateModelOnTasks(ctx context.Context, model ModelConfig, tasks []TaskTestCase) ([]TaskEvalResult, error) {
	log.Printf("Evaluating model %s on %d tasks", model.ID, len(tasks))

	var results []TaskEvalResult

	for _, task := range tasks {
		r.updateProgress(func(p *TaskEvalProgress) {
			p.CurrentTask = task.ID
			p.CompletedTasks = len(results)
		})

		result, err := r.client.EvaluateTask(ctx, model, task)
		if err != nil {
			log.Printf("Error evaluating task %s on model %s: %v", task.ID, model.ID, err)

			// Create an error result
			results = append(results, TaskEvalResult{
				ModelID:      model.ID,
				ModelName:    model.DisplayName,
				TaskID:       task.ID,
				TaskName:     task.Name,
				TaskCategory: task.Category,
				Success:      false,
				Error:        err.Error(),
				StartTime:    time.Now(),
				EndTime:      time.Now(),
			})
		} else {
			results = append(results, *result)
		}

		// Update cost and token counts
		r.updateProgress(func(p *TaskEvalProgress) {
			if result.Success {
				p.TotalCost += result.TotalCost
				p.TotalTokens += result.TotalTokens
			}
		})

		// Notify listeners of the result
		r.notifyTaskResult(results[len(results)-1])

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}
	}

	return results, nil
}

// AddProgressListener adds a listener for progress updates
func (r *TaskEvalRunner) AddProgressListener(listener TaskProgressListener) {
	r.listenersMux.Lock()
	defer r.listenersMux.Unlock()
	r.listeners = append(r.listeners, listener)
}

// GetProgress returns the current progress
func (r *TaskEvalRunner) GetProgress() TaskEvalProgress {
	r.progressMux.RLock()
	defer r.progressMux.RUnlock()
	return *r.progress
}

// GetResults returns all collected results
func (r *TaskEvalRunner) GetResults() []TaskEvalResult {
	r.progressMux.RLock()
	defer r.progressMux.RUnlock()

	// Return a copy to avoid modification
	results := make([]TaskEvalResult, len(r.results))
	copy(results, r.results)
	return results
}

// SaveResults saves the results to a JSON file
func (r *TaskEvalRunner) SaveResults(filepath string) error {
	results := r.GetResults()

	// Convert to EvalResult format for compatibility
	// For now, save as task-specific format
	data, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	return os.WriteFile(filepath, data, 0644)
}

// SaveSummary saves a summary report
func (r *TaskEvalRunner) SaveSummary(filepath string) error {
	summary := r.GenerateSummary()

	return SaveTaskSummary(summary, filepath)
}

// GenerateSummary creates a summary of the task evaluation results
func (r *TaskEvalRunner) GenerateSummary() EvalSummary {
	results := r.GetResults()
	return GenerateTaskSummary(results, *r.config)
}

// cleanup performs resource cleanup
func (r *TaskEvalRunner) cleanup() {
	if r.client != nil {
		r.client.Cleanup()
	}
}

// updateProgress safely updates the progress and notifies listeners
func (r *TaskEvalRunner) updateProgress(updateFunc func(*TaskEvalProgress)) {
	r.progressMux.Lock()
	defer r.progressMux.Unlock()

	updateFunc(r.progress)

	// Notify listeners
	r.listenersMux.RLock()
	for _, listener := range r.listeners {
		listener.OnTaskProgressUpdate(*r.progress)
	}
	r.listenersMux.RUnlock()
}

// addResults safely adds results and updates progress
func (r *TaskEvalRunner) addResults(results []TaskEvalResult) {
	r.progressMux.Lock()
	defer r.progressMux.Unlock()

	r.results = append(r.results, results...)
	r.progress.ResultsCount = len(r.results)
}

// notifyResult notifies all listeners of a new result
func (r *TaskEvalRunner) notifyTaskResult(result TaskEvalResult) {
	r.listenersMux.RLock()
	defer r.listenersMux.RUnlock()

	for _, listener := range r.listeners {
		listener.OnTaskResult(result)
	}
}

// Cancel stops the currently running evaluation
func (r *TaskEvalRunner) Cancel() {
	r.updateProgress(func(p *TaskEvalProgress) {
		p.Status = "cancelled"
		if p.EndTime == nil {
			endTime := time.Now()
			p.EndTime = &endTime
		}
	})
}
