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

// EvalRunner coordinates the evaluation process across multiple models
type EvalRunner struct {
	config         *EvalConfig
	client         *OpenRouterEvalClient
	results        []EvalResult
	progress       *EvalProgress
	progressMux    sync.RWMutex
	listeners      []ProgressListener
	listenersMux   sync.RWMutex
}

// EvalProgress tracks the overall progress of evaluation
type EvalProgress struct {
	StartTime         time.Time    `json:"start_time"`
	EndTime           *time.Time   `json:"end_time,omitempty"`
	Status            string       `json:"status"` // "running", "completed", "failed", "cancelled"
	TotalModels       int          `json:"total_models"`
	CompletedModels   int          `json:"completed_models"`
	CurrentModel      string       `json:"current_model,omitempty"`
	CurrentTestCase   string       `json:"current_test_case,omitempty"`
	TotalTestCases    int          `json:"total_test_cases"`
	CompletedTestCases int         `json:"completed_test_cases"`
	ResultsCount      int          `json:"results_count"`
	TotalCost         float64      `json:"total_cost"`
	TotalTokens       int          `json:"total_tokens"`
	Error             string       `json:"error,omitempty"`
}

// ProgressListener receives progress updates during evaluation
type ProgressListener interface {
	OnProgressUpdate(progress EvalProgress)
	OnResult(result EvalResult)
}

// NewEvalRunner creates a new evaluation runner
func NewEvalRunner(config *EvalConfig) (*EvalRunner, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	client, err := NewOpenRouterEvalClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create eval client: %w", err)
	}
	
	return &EvalRunner{
		config:  config,
		client:  client,
		results: make([]EvalResult, 0),
		progress: &EvalProgress{
			Status: "initialized",
		},
	}, nil
}

// Run starts the evaluation process
func (r *EvalRunner) Run(ctx context.Context) error {
	r.updateProgress(func(p *EvalProgress) {
		p.StartTime = time.Now()
		p.Status = "running"
		p.TotalModels = len(r.config.GetEnabledModels())
		p.TotalTestCases = len(r.config.TestCases)
		p.CompletedModels = 0
		p.CompletedTestCases = 0
		p.ResultsCount = 0
		p.TotalCost = 0.0
		p.TotalTokens = 0
	})
	
	log.Printf("Starting evaluation with %d models and %d test cases", 
		r.progress.TotalModels, r.progress.TotalTestCases)
	
	// Validate connection before starting
	if err := r.client.ValidateConnection(ctx); err != nil {
		r.updateProgress(func(p *EvalProgress) {
			p.Status = "failed"
			p.Error = err.Error()
			if p.EndTime == nil {
				endTime := time.Now()
				p.EndTime = &endTime
			}
		})
		return fmt.Errorf("connection validation failed: %w", err)
	}
	
	// Get model information
	if r.config.Verbose {
		if err := r.client.GetModelInfo(ctx); err != nil {
			log.Printf("Warning: Could not retrieve model info: %v", err)
		}
	}
	
	// Evaluate each model
	enabledModels := r.config.GetEnabledModels()
	for i, model := range enabledModels {
		r.updateProgress(func(p *EvalProgress) {
			p.CurrentModel = model.ID
			p.CurrentTestCase = ""
			p.CompletedModels = i
		})
		
		modelResults, err := r.evaluateModel(ctx, model)
		if err != nil {
			log.Printf("Error evaluating model %s: %v", model.ID, err)
			r.updateProgress(func(p *EvalProgress) {
				p.Error = fmt.Sprintf("Model %s failed: %v", model.ID, err)
			})
			// Continue with other models even if one fails
		} else {
			r.addResults(modelResults)
		}
		
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			r.updateProgress(func(p *EvalProgress) {
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
	r.updateProgress(func(p *EvalProgress) {
		p.Status = "completed"
		p.CompletedModels = len(enabledModels)
		p.CurrentModel = ""
		p.CurrentTestCase = ""
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
	
	log.Printf("Evaluation completed: %d results, total cost $%.6f, total tokens %d", 
		len(r.results), r.progress.TotalCost, r.progress.TotalTokens)
	
	return nil
}

// evaluateModel runs all test cases on a single model
func (r *EvalRunner) evaluateModel(ctx context.Context, model ModelConfig) ([]EvalResult, error) {
	log.Printf("Evaluating model: %s", model.ID)
	
	results, err := r.client.EvaluateModel(ctx, model)
	if err != nil {
		return nil, err
	}
	
	// Update counts and notify listeners of results
	for _, result := range results {
		r.updateProgress(func(p *EvalProgress) {
			if result.Success {
				p.TotalCost += result.TotalCost
				p.TotalTokens += result.TotalTokens
			}
		})
		
		r.notifyResult(result)
	}
	
	return results, nil
}

// AddProgressListener adds a listener for progress updates
func (r *EvalRunner) AddProgressListener(listener ProgressListener) {
	r.listenersMux.Lock()
	defer r.listenersMux.Unlock()
	r.listeners = append(r.listeners, listener)
}

// GetProgress returns the current progress
func (r *EvalRunner) GetProgress() EvalProgress {
	r.progressMux.RLock()
	defer r.progressMux.RUnlock()
	return *r.progress
}

// GetResults returns all collected results
func (r *EvalRunner) GetResults() []EvalResult {
	r.progressMux.RLock()
	defer r.progressMux.RUnlock()
	
	// Return a copy to avoid modification
	results := make([]EvalResult, len(r.results))
	copy(results, r.results)
	return results
}

// SaveResults saves the results to a JSON file
func (r *EvalRunner) SaveResults(filepath string) error {
	results := r.GetResults()
	
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}
	
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write results file: %w", err)
	}
	
	log.Printf("Results saved to: %s", filepath)
	return nil
}

// SaveSummary saves a summary report
func (r *EvalRunner) SaveSummary(filepath string) error {
	summary := r.GenerateSummary()
	
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal summary: %w", err)
	}
	
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write summary file: %w", err)
	}
	
	log.Printf("Summary saved to: %s", filepath)
	return nil
}

// GenerateSummary creates a summary of the evaluation results
func (r *EvalRunner) GenerateSummary() EvalSummary {
	results := r.GetResults()
	summary := EvalSummary{
		EvalTime:     time.Now(),
		Config:       *r.config,
		Progress:     r.GetProgress(),
		ModelSummaries: make(map[string]ModelSummary),
		CategorySummaries: make(map[string]CategorySummary),
	}
	
	// Calculate model summaries
	for _, result := range results {
		modelID := result.ModelID
		
		if _, exists := summary.ModelSummaries[modelID]; !exists {
			summary.ModelSummaries[modelID] = ModelSummary{
				ModelID:     modelID,
				ModelName:   result.ModelName,
				TotalCost:   0.0,
				TotalTokens: 0,
				SuccessCount: 0,
				TotalCount:  0,
			}
		}
		
		modelSummary := summary.ModelSummaries[modelID]
		modelSummary.TotalCost += result.TotalCost
		modelSummary.TotalTokens += result.TotalTokens
		modelSummary.TotalCount++
		if result.Success {
			modelSummary.SuccessCount++
		}
		summary.ModelSummaries[modelID] = modelSummary
		
		// Calculate category summaries
		category := result.TestCategory
		if _, exists := summary.CategorySummaries[category]; !exists {
			summary.CategorySummaries[category] = CategorySummary{
				Category:     category,
				SuccessCount: 0,
				TotalCount:   0,
			}
		}
		
		catSummary := summary.CategorySummaries[category]
		catSummary.TotalCount++
		if result.Success {
			catSummary.SuccessCount++
		}
		summary.CategorySummaries[category] = catSummary
	}
	
	return summary
}

// updateProgress safely updates the progress and notifies listeners
func (r *EvalRunner) updateProgress(updateFunc func(*EvalProgress)) {
	r.progressMux.Lock()
	defer r.progressMux.Unlock()
	
	updateFunc(r.progress)
	
	// Notify listeners
	r.listenersMux.RLock()
	for _, listener := range r.listeners {
		listener.OnProgressUpdate(*r.progress)
	}
	r.listenersMux.RUnlock()
}

// addResults safely adds results and updates progress
func (r *EvalRunner) addResults(results []EvalResult) {
	r.progressMux.Lock()
	defer r.progressMux.Unlock()
	
	r.results = append(r.results, results...)
	r.progress.ResultsCount = len(r.results)
}

// notifyResult notifies all listeners of a new result
func (r *EvalRunner) notifyResult(result EvalResult) {
	r.listenersMux.RLock()
	defer r.listenersMux.RUnlock()
	
	for _, listener := range r.listeners {
		listener.OnResult(result)
	}
}

// Helper function to get a copy of progress for listeners
func g(progress *EvalProgress) EvalProgress {
	// Return a copy to avoid race conditions
	p := *progress
	if progress.EndTime != nil {
		endTime := *progress.EndTime
		p.EndTime = &endTime
	}
	return p
}

// Cancel stops the currently running evaluation
func (r *EvalRunner) Cancel() {
	r.updateProgress(func(p *EvalProgress) {
		p.Status = "cancelled"
		if p.EndTime == nil {
			endTime := time.Now()
			p.EndTime = &endTime
		}
	})
}