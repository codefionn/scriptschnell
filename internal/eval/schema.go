package eval

import (
	"encoding/json"
	"fmt"
	"time"
)

// EvalDefinition defines an evaluation test case
type EvalDefinition struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Image         string            `json:"image"` // Docker/Podman base image
	UserPrompt    string            `json:"user_prompt"`
	ExpectedFiles map[string]string `json:"expected_files"` // path -> content
	RunFile       string            `json:"run_file"`       // which file to execute
	CLITests      []CLITestCase     `json:"cli_tests"`
}

// CLITestCase defines a CLI test case
type CLITestCase struct {
	ID          string   `json:"id"`
	Args        []string `json:"args"`
	Expected    string   `json:"expected"`
	Description string   `json:"description,omitempty"`
	Timeout     int      `json:"timeout,omitempty"` // timeout in seconds, default 30
}

// EvalRun represents a single evaluation run
type EvalRun struct {
	ID            int64      `json:"id" db:"id"`
	EvalID        string     `json:"eval_id" db:"eval_id"`
	ModelID       string     `json:"model_id" db:"model_id"`
	Status        string     `json:"status" db:"status"` // pending, running, completed, failed
	AgentOutput   string     `json:"agent_output,omitempty" db:"agent_output"`
	AgentExitCode int        `json:"agent_exit_code,omitempty" db:"agent_exit_code"`
	StartedAt     time.Time  `json:"started_at" db:"started_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

// EvalResult represents the result of a single test case within an eval run
type EvalResult struct {
	ID                    int64   `json:"id" db:"id"`
	RunID                 int64   `json:"run_id" db:"run_id"`
	TestCaseID            string  `json:"test_case_id" db:"test_case_id"`
	Passed                bool    `json:"passed" db:"passed"`
	ActualOutput          string  `json:"actual_output" db:"actual_output"`
	ExpectedOutput        string  `json:"expected_output" db:"expected_output"`
	Error                 string  `json:"error,omitempty" db:"error"`
	InputTokens           int     `json:"input_tokens" db:"input_tokens"`
	OutputTokens          int     `json:"output_tokens" db:"output_tokens"`
	EstimateCost          float64 `json:"estimate_cost" db:"estimate_cost"`      // in USD
	ResponseTime          int     `json:"response_time_ms" db:"response_time"`   // in milliseconds
	ExecutionTime         int     `json:"execution_time_ms" db:"execution_time"` // in milliseconds
	ContainerName         string  `json:"container_name,omitempty" db:"container_name"`
	RawOutput             string  `json:"raw_output,omitempty" db:"raw_output"`
	Errors                string  `json:"errors,omitempty" db:"errors"`
	DetailedExecutionInfo string  `json:"detailed_execution_info,omitempty" db:"detailed_execution_info"`
}

// EvalConfig stores configuration
type EvalConfig struct {
	OpenRouterToken string `json:"openrouter_token" db:"openrouter_token"`
}

// EvalModel represents a model available for evaluation
type EvalModel struct {
	ID            string `json:"id" db:"id"`
	Name          string `json:"name" db:"name"`
	Provider      string `json:"provider" db:"provider"`
	Selected      bool   `json:"selected" db:"selected"`
	Description   string `json:"description,omitempty" db:"description"`
	ContextWindow int    `json:"context_window,omitempty" db:"context_window"`
}

// Validate validates the eval definition
func (e *EvalDefinition) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("id is required")
	}
	if e.Name == "" {
		return fmt.Errorf("name is required")
	}
	if e.Image == "" {
		return fmt.Errorf("image is required")
	}
	if e.UserPrompt == "" {
		return fmt.Errorf("user_prompt is required")
	}
	if e.RunFile == "" {
		return fmt.Errorf("run_file is required")
	}
	if len(e.CLITests) == 0 {
		return fmt.Errorf("at least one CLI test is required")
	}

	// Validate CLI tests
	for i, test := range e.CLITests {
		if test.ID == "" {
			return fmt.Errorf("test case %d: id is required", i)
		}
		if test.Expected == "" {
			return fmt.Errorf("test case %d: expected output is required", i)
		}
		if test.Timeout <= 0 {
			test.Timeout = 30 // default timeout
		}
	}

	return nil
}

// LoadEvalDefinition loads an eval definition from JSON data
func LoadEvalDefinition(data []byte) (*EvalDefinition, error) {
	var def EvalDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("invalid eval definition: %w", err)
	}
	return &def, nil
}

// GetStats aggregates statistics for an eval run
func (r *EvalRun) GetStats(results []EvalResult) EvalStats {
	stats := EvalStats{
		RunID:       r.ID,
		EvalID:      r.EvalID,
		ModelID:     r.ModelID,
		Status:      r.Status,
		StartedAt:   r.StartedAt,
		CompletedAt: r.CompletedAt,
	}

	for _, result := range results {
		if result.Passed {
			stats.PassedCount++
		} else {
			stats.FailedCount++
		}
		stats.TotalTokens += result.InputTokens + result.OutputTokens
		stats.TotalCost += result.EstimateCost
		stats.ResponseTime += result.ResponseTime
		stats.ExecutionTime += result.ExecutionTime
	}

	stats.TotalCount = stats.PassedCount + stats.FailedCount
	stats.PassRate = float64(stats.PassedCount) / float64(stats.TotalCount) * 100

	// Calculate averages
	if stats.TotalCount > 0 {
		stats.AvgResponseTime = stats.ResponseTime / stats.TotalCount
		stats.AvgExecutionTime = stats.ExecutionTime / stats.TotalCount
	}

	return stats
}

// EvalStats provides aggregated statistics
type EvalStats struct {
	RunID            int64      `json:"run_id"`
	EvalID           string     `json:"eval_id"`
	ModelID          string     `json:"model_id"`
	Status           string     `json:"status"`
	TotalCount       int        `json:"total_count"`
	PassedCount      int        `json:"passed_count"`
	FailedCount      int        `json:"failed_count"`
	PassRate         float64    `json:"pass_rate"`
	TotalTokens      int        `json:"total_tokens"`
	TotalCost        float64    `json:"total_cost"`
	AvgResponseTime  int        `json:"avg_response_time_ms"`
	AvgExecutionTime int        `json:"avg_execution_time_ms"`
	ResponseTime     int        `json:"response_time_ms"`
	ExecutionTime    int        `json:"execution_time_ms"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}
