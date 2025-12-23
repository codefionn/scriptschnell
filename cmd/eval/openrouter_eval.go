package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// OpenRouterEvalClient wraps the OpenRouter client for evaluation with cost tracking
type OpenRouterEvalClient struct {
	client *llm.OpenRouterClient
	config *EvalConfig
}

// EvalResult represents the result of evaluating a single model on a single test case
type EvalResult struct {
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	TestCaseID   string `json:"test_case_id"`
	TestCaseName string `json:"test_case_name"`
	TestCategory string `json:"test_category"`

	// Response data
	Response string `json:"response"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`

	// Timing
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`

	// Token usage
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Cost information
	PromptCost     float64 `json:"prompt_cost"`
	CompletionCost float64 `json:"completion_cost"`
	TotalCost      float64 `json:"total_cost"`
	Currency       string  `json:"currency"`

	// Additional metadata
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
}

// NewOpenRouterEvalClient creates a new evaluation client
func NewOpenRouterEvalClient(config *EvalConfig) (*OpenRouterEvalClient, error) {
	if config.OpenRouterAPIKey == "" {
		return nil, fmt.Errorf("OpenRouter API key is required")
	}

	// Create a temporary client for validation (will be recreated for each model)
	tempClient, err := llm.NewOpenRouterClient(config.OpenRouterAPIKey, "openai/o3-mini")
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenRouter client: %w", err)
	}

	return &OpenRouterEvalClient{
		client: tempClient.(*llm.OpenRouterClient),
		config: config,
	}, nil
}

// EvaluateModel runs all test cases on a specific model
func (c *OpenRouterEvalClient) EvaluateModel(ctx context.Context, model ModelConfig) ([]EvalResult, error) {
	log.Printf("Starting evaluation for model: %s (%s)", model.DisplayName, model.ID)

	// Create client for this specific model
	client, err := llm.NewOpenRouterClient(c.config.OpenRouterAPIKey, model.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for model %s: %w", model.ID, err)
	}

	// Verify the model name matches
	if client.GetModelName() != model.ID {
		log.Printf("Warning: Model ID mismatch - requested: %s, got: %s", model.ID, client.GetModelName())
	}

	var results []EvalResult

	for _, testCase := range c.config.TestCases {
		result, err := c.evaluateTestCase(ctx, client, model, testCase)
		if err != nil {
			log.Printf("Error evaluating test case %s on model %s: %v", testCase.ID, model.ID, err)

			// Create an error result
			results = append(results, EvalResult{
				ModelID:      model.ID,
				ModelName:    model.DisplayName,
				TestCaseID:   testCase.ID,
				TestCaseName: testCase.Name,
				TestCategory: testCase.Category,
				Success:      false,
				Error:        err.Error(),
				StartTime:    time.Now(),
				EndTime:      time.Now(),
				Currency:     "USD",
				Temperature:  c.config.Temperature,
				MaxTokens:    c.config.MaxTokens,
			})
		} else {
			results = append(results, *result)
		}
	}

	log.Printf("Completed evaluation for model %s: %d test cases", model.ID, len(results))
	return results, nil
}

// evaluateTestCase runs a single test case on a model
func (c *OpenRouterEvalClient) evaluateTestCase(ctx context.Context, client llm.Client, model ModelConfig, testCase TestCase) (*EvalResult, error) {
	startTime := time.Now()

	if c.config.Verbose {
		log.Printf("Evaluating test case %s on model %s", testCase.ID, model.ID)
	}

	// Create completion request
	req := &llm.CompletionRequest{
		Messages: []*llm.Message{
			{Role: "user", Content: testCase.Prompt},
		},
		Temperature: c.config.Temperature,
		MaxTokens:   c.config.MaxTokens,
	}

	// Add test case specific parameters if any
	if testCase.Parameters != nil {
		// Could extend this to handle custom parameters per test case
		req.EnableCaching = false // Disable caching for consistent evaluation
	}

	// Create a child context with timeout
	evalCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Execute the completion
	resp, err := client.CompleteWithRequest(evalCtx, req)
	endTime := time.Now()

	if err != nil {
		return nil, fmt.Errorf("completion failed: %w", err)
	}

	// Build result
	result := &EvalResult{
		ModelID:      model.ID,
		ModelName:    model.DisplayName,
		TestCaseID:   testCase.ID,
		TestCaseName: testCase.Name,
		TestCategory: testCase.Category,
		Response:     resp.Content,
		Success:      true,
		StartTime:    startTime,
		EndTime:      endTime,
		Duration:     endTime.Sub(startTime),
		Temperature:  c.config.Temperature,
		MaxTokens:    c.config.MaxTokens,
	}

	// Extract token usage and cost information
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

		// Extract cost information from OpenRouter usage
		if promptCost, ok := resp.Usage["prompt_cost"].(float64); ok {
			result.PromptCost = promptCost
		}
		if completionCost, ok := resp.Usage["completion_cost"].(float64); ok {
			result.CompletionCost = completionCost
		}
		if totalCost, ok := resp.Usage["total_cost"].(float64); ok {
			result.TotalCost = totalCost
		} else {
			// Fallback: calculate total cost from prompt and completion costs
			result.TotalCost = result.PromptCost + result.CompletionCost
		}

		if currency, ok := resp.Usage["currency"].(string); ok {
			result.Currency = currency
		} else {
			result.Currency = "USD" // OpenRouter default
		}
	}

	// Log token usage if enabled
	if c.config.Verbose {
		log.Printf("Test case %s on model %s: %d tokens, cost $%.6f",
			testCase.ID, model.ID, result.TotalTokens, result.TotalCost)
	}

	return result, nil
}

// ValidateConnection tests the OpenRouter connection
func (c *OpenRouterEvalClient) ValidateConnection(ctx context.Context) error {
	// Try a simple completion to test the connection
	testReq := &llm.CompletionRequest{
		Messages:    []*llm.Message{{Role: "user", Content: "test"}},
		MaxTokens:   10,
		Temperature: 0.1,
	}

	_, err := c.client.CompleteWithRequest(ctx, testReq)
	if err != nil {
		return fmt.Errorf("connection validation failed: %w", err)
	}

	log.Println("OpenRouter connection validated successfully")
	return nil
}

// GetModelInfo retrieves information about available models
func (c *OpenRouterEvalClient) GetModelInfo(ctx context.Context) error {
	// Create an OpenRouter provider to list models
	provider := llm.NewOpenRouterProvider(c.config.OpenRouterAPIKey)

	models, err := provider.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	log.Printf("Available OpenRouter models: %d", len(models))

	// Check if our target models are available
	targetModels := make(map[string]bool)
	for _, model := range c.config.Models {
		targetModels[model.ID] = true
	}

	foundModels := 0
	for _, model := range models {
		if targetModels[model.ID] {
			log.Printf("Found target model: %s (%s)", model.ID, model.Name)
			foundModels++
		}
	}

	if foundModels < len(targetModels) {
		log.Printf("Warning: Only found %d of %d target models", foundModels, len(targetModels))
	}

	return nil
}

// PrintCostSummary prints a summary of costs for the evaluation
func PrintCostSummary(results []EvalResult) {
	totalCost := 0.0
	totalTokens := 0
	modelCosts := make(map[string]float64)
	modelTokens := make(map[string]int)

	for _, result := range results {
		if result.Success {
			totalCost += result.TotalCost
			totalTokens += result.TotalTokens

			modelCosts[result.ModelID] += result.TotalCost
			modelTokens[result.ModelID] += result.TotalTokens
		}
	}

	fmt.Printf("\n=== Cost Summary ===\n")
	fmt.Printf("Total Cost: $%.6f %s\n", totalCost, "USD")
	fmt.Printf("Total Tokens: %d\n", totalTokens)
	fmt.Printf("\n=== Cost by Model ===\n")

	for modelID, cost := range modelCosts {
		tokens := modelTokens[modelID]
		fmt.Printf("%s: $%.6f (%d tokens)\n", modelID, cost, tokens)
	}
	fmt.Printf("===================\n\n")
}
