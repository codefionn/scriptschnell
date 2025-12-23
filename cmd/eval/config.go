package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// EvalConfig holds the configuration for the evaluation system
type EvalConfig struct {
	// OpenRouter API configuration
	OpenRouterAPIKey string `json:"openrouter_api_key"`
	
	// Models to evaluate
	Models []ModelConfig `json:"models"`
	
	// Test cases to run
	TestCases []TestCase `json:"test_cases"`
	
	// Evaluation settings
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Timeout     time.Duration `json:"timeout"`
	
	// Output settings
	OutputFile string `json:"output_file"`
	Verbose    bool   `json:"verbose"`
}

// ModelConfig represents a model to evaluate
type ModelConfig struct {
	ID           string `json:"id"`            // e.g., "mistralai/devstral-2512"
	DisplayName  string `json:"display_name"`  // Human-readable name
	Provider     string `json:"provider"`      // Should be "openrouter" for all
	Enabled      bool   `json:"enabled"`       // Whether to include in evaluation
	MaxTokens    int    `json:"max_tokens"`    // Model-specific token limit (optional)
}

// TestCase represents a test case to run
type TestCase struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Prompt      string                 `json:"prompt"`
	Expected    interface{}            `json:"expected"` // Expected response type or content
	Category    string                 `json:"category"` // e.g., "calculator", "reasoning", "coding"
	Parameters  map[string]interface{} `json:"parameters"` // Additional parameters for this test case
}

// DefaultEvalConfig returns a configuration with the specified models for OpenRouter evaluation
func DefaultEvalConfig() *EvalConfig {
	return &EvalConfig{
		Models: []ModelConfig{
			{
				ID:          "mistralai/devstral-2512",
				DisplayName: "Mistral Devstral 2512",
				Provider:    "openrouter",
				Enabled:     true,
			},
			{
				ID:          "z-ai/glm-4.6v",
				DisplayName: "Z AI GLM-4.6V",
				Provider:    "openrouter",
				Enabled:     true,
			},
			{
				ID:          "openai/gpt-5.2",
				DisplayName: "OpenAI GPT-5.2",
				Provider:    "openrouter",
				Enabled:     true,
			},
			{
				ID:          "deepseek/deepseek-v3.2",
				DisplayName: "Deepseek V3.2",
				Provider:    "openrouter",
				Enabled:     true,
			},
			{
				ID:          "google/gemini-3-pro-preview",
				DisplayName: "Gemini 3 Pro Preview",
				Provider:    "openrouter",
				Enabled:     true,
			},
			{
				ID:          "moonshotai/kimi-k2-0905:exacto",
				DisplayName: "Moonshot Kimi K2 0905",
				Provider:    "openrouter",
				Enabled:     true,
			},
		},
		TestCases: []TestCase{
			{
				ID:          "calc_basic",
				Name:        "Basic Calculator",
				Description: "Test basic arithmetic calculation",
				Prompt:      "What is 15 + 27?",
				Expected:    "42",
				Category:    "calculator",
				Parameters: map[string]interface{}{
					"requires_reasoning": false,
				},
			},
			{
				ID:          "calc_precedence",
				Name:        "Operator Precedence",
				Description: "Test mathematical operator precedence",
				Prompt:      "What is 5 + 10 * 2?",
				Expected:    "25",
				Category:    "calculator",
				Parameters: map[string]interface{}{
					"requires_reasoning": true,
				},
			},
			{
				ID:          "reasoning_simple",
				Name:        "Simple Reasoning",
				Description: "Test logical reasoning",
				Prompt:      "If all cats are animals and Fluffy is a cat, is Fluffy an animal? Explain your reasoning.",
				Expected:    map[string]interface{}{"contains_animal": true},
				Category:    "reasoning",
			},
		},
		MaxTokens:   4096,
		Temperature: 0.7,
		Timeout:     60 * time.Second,
		OutputFile:  "eval_results.json",
		Verbose:     true,
	}
}

// LoadConfig loads configuration from file or returns default
func LoadConfig(configPath string) (*EvalConfig, error) {
	if configPath == "" {
		configPath = "eval_config.json"
	}
	
	// Try to load from file
	if data, err := os.ReadFile(configPath); err == nil {
		var config EvalConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
		
		// Set defaults for missing fields
		if config.MaxTokens == 0 {
			config.MaxTokens = 4096
		}
		if config.Temperature == 0 {
			config.Temperature = 0.7
		}
		if config.Timeout == 0 {
			config.Timeout = 60 * time.Second
		}
		if config.OutputFile == "" {
			config.OutputFile = "eval_results.json"
		}
		
		return &config, nil
	}
	
	// Return default config if file doesn't exist
	config := DefaultEvalConfig()
	
	// Override with environment variables if available
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		config.OpenRouterAPIKey = apiKey
	}
	
	return config, nil
}

// SaveConfig saves the configuration to a file
func (c *EvalConfig) SaveConfig(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	
	return os.WriteFile(path, data, 0644)
}

// Validate validates the configuration
func (c *EvalConfig) Validate() error {
	if c.OpenRouterAPIKey == "" {
		return fmt.Errorf("OpenRouter API key is required")
	}
	
	if len(c.Models) == 0 {
		return fmt.Errorf("at least one model must be configured")
	}
	
	if len(c.TestCases) == 0 {
		return fmt.Errorf("at least one test case must be configured")
	}
	
	enabledModels := 0
	for _, model := range c.Models {
		if model.Enabled {
			enabledModels++
		}
	}
	
	if enabledModels == 0 {
		return fmt.Errorf("at least one model must be enabled")
	}
	
	return nil
}

// GetEnabledModels returns all enabled models
func (c *EvalConfig) GetEnabledModels() []ModelConfig {
	var enabled []ModelConfig
	for _, model := range c.Models {
		if model.Enabled {
			enabled = append(enabled, model)
		}
	}
	return enabled
}

// GetTestCaseByID returns a test case by its ID
func (c *EvalConfig) GetTestCaseByID(id string) (TestCase, error) {
	for _, testCase := range c.TestCases {
		if testCase.ID == id {
			return testCase, nil
		}
	}
	return TestCase{}, fmt.Errorf("test case not found: %s", id)
}