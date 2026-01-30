package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/provider"
)

// Service manages the eval system
type Service struct {
	db              *Database
	loader          *Loader
	manager         *provider.Manager
	mu              sync.RWMutex
	openRouterToken string // Store token for lazy initialization
	logAssistant    bool   // Whether to log assistant messages to console
}

// NewService creates a new eval service
func NewService(dbPath, evalDir string) (*Service, error) {
	// Initialize database
	db, err := NewDatabase(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Initialize loader
	loader := NewLoader(evalDir)
	if err := loader.ValidateDirectory(); err != nil {
		return nil, fmt.Errorf("invalid eval directory: %w", err)
	}

	service := &Service{
		db:     db,
		loader: loader,
	}

	// Load existing configuration from database
	config, err := db.GetEvalConfig()
	if err == nil && config.OpenRouterToken != "" {
		// Store the token for lazy initialization
		service.openRouterToken = config.OpenRouterToken
	}

	return service, nil
}

// Close closes the service and cleans up resources
func (s *Service) Close() error {
	return s.db.Close()
}

// SetLogAssistant sets whether to log assistant messages to console
func (s *Service) SetLogAssistant(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logAssistant = enabled
}

// Config operations

// GetConfig gets the current configuration
func (s *Service) GetConfig() (*EvalConfig, error) {
	return s.db.GetEvalConfig()
}

// SetConfig updates the configuration
func (s *Service) SetConfig(config *EvalConfig) error {
	if config.OpenRouterToken != "" {
		// Test the token by creating a provider manager
		if err := s.testOpenRouterToken(config.OpenRouterToken); err != nil {
			return fmt.Errorf("invalid OpenRouter token: %w", err)
		}
		// Initialize provider manager with valid token
		if err := s.initProviderManager(config.OpenRouterToken); err != nil {
			return fmt.Errorf("failed to initialize provider manager: %w", err)
		}
	}

	// Always update the in-memory token
	s.openRouterToken = config.OpenRouterToken

	// Always update the in-memory token
	s.openRouterToken = config.OpenRouterToken

	return s.db.SetEvalConfig(config)
}

// testOpenRouterToken tests if the OpenRouter token is valid
func (s *Service) testOpenRouterToken(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	provider := llm.NewOpenRouterProvider(token)
	return provider.ValidateAPIKey(ctx)
}

// initProviderManager initializes the provider manager with OpenRouter
func (s *Service) initProviderManager(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// For now, just validate that the token is not empty
	// We'll initialize the provider manager lazily when needed
	// This avoids configuration issues during setup
	if token == "" {
		return fmt.Errorf("OpenRouter token cannot be empty")
	}

	// Store the token for later use
	// The provider manager will be initialized when we actually need it
	s.openRouterToken = token
	return nil
}

// Model operations

// RefreshModels refreshes the model list from OpenRouter
func (s *Service) RefreshModels() ([]*EvalModel, error) {
	s.mu.Lock()
	manager := s.manager

	// Initialize manager lazily if needed
	if manager == nil && s.openRouterToken != "" {
		// Create a config path for the provider manager
		configDir := s.db.getConfigDir()
		configPath := filepath.Join(configDir, "eval-providers.json")

		// Initialize provider manager
		var err error
		manager, err = provider.NewManager(configPath, "password")
		if err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("failed to create provider manager: %w", err)
		}

		// Add OpenRouter provider
		err = manager.AddProviderWithAPIListing(context.Background(), "openrouter", s.openRouterToken)
		if err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("failed to add OpenRouter provider: %w", err)
		}

		s.manager = manager
	}

	s.mu.Unlock()

	if manager == nil {
		return nil, fmt.Errorf("providers not initialized - please configure OpenRouter token first")
	}

	// Refresh OpenRouter models
	err := manager.RefreshModels(context.Background(), "openrouter")
	if err != nil {
		return nil, fmt.Errorf("failed to refresh models: %w", err)
	}

	// Get updated models from manager
	provider, ok := manager.GetProvider("openrouter")
	if !ok {
		return nil, fmt.Errorf("OpenRouter provider not found")
	}

	// Convert to EvalModel format
	var models []*EvalModel
	for _, model := range provider.Models {
		evalModel := &EvalModel{
			ID:            model.ID,
			Name:          model.Name,
			Provider:      model.Provider,
			Description:   model.Description,
			ContextWindow: model.ContextWindow,
		}
		models = append(models, evalModel)
	}

	// Save models to database
	for _, model := range models {
		if err := s.db.AddModel(model); err != nil {
			return nil, fmt.Errorf("failed to save model %s: %w", model.ID, err)
		}
	}

	return models, nil
}

// GetModels gets all models from the database
func (s *Service) GetModels() ([]*EvalModel, error) {
	return s.db.GetModels()
}

// GetSelectedModels gets models selected for evaluation
func (s *Service) GetSelectedModels() ([]*EvalModel, error) {
	return s.db.GetSelectedModels()
}

// SelectModel sets a model as selected for evaluation
func (s *Service) SelectModel(modelID string) error {
	return s.db.UpdateModelSelection(modelID, true)
}

// DeselectModel removes a model from selection
func (s *Service) DeselectModel(modelID string) error {
	return s.db.UpdateModelSelection(modelID, false)
}

// ClearModelSelection clears all model selections
func (s *Service) ClearModelSelection() error {
	return s.db.ClearModelSelection()
}

// Eval definition operations

// GetEvals gets all available evaluation definitions
func (s *Service) GetEvals() (map[string]*EvalDefinition, error) {
	return s.loader.LoadAll()
}

// GetEval gets a specific evaluation definition
func (s *Service) GetEval(evalID string) (*EvalDefinition, error) {
	return s.loader.LoadEval(evalID)
}

// Eval operations

// RunEval starts an evaluation run
func (s *Service) RunEval(evalID string) ([]*EvalRun, error) {
	// Get selected models
	selectedModels, err := s.GetSelectedModels()
	if err != nil {
		return nil, fmt.Errorf("failed to get selected models: %w", err)
	}

	if len(selectedModels) == 0 {
		return nil, fmt.Errorf("no models selected for evaluation")
	}

	// Get eval definition
	evalDef, err := s.GetEval(evalID)
	if err != nil {
		return nil, fmt.Errorf("failed to get eval definition: %w", err)
	}

	// Build image once for this eval definition (shared across all models)
	executor, err := NewContainerExecutor()
	if err != nil {
		return nil, fmt.Errorf("failed to create container executor: %w", err)
	}

	imageName := fmt.Sprintf("eval-scriptschnell-%s:latest", evalDef.ID)
	ctx := context.Background()
	buildCtx, buildCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer buildCancel()

	log.Printf("Building image %s for eval definition %s (will be reused for all %d models)...", imageName, evalDef.ID, len(selectedModels))
	if err := executor.BuildImage(buildCtx, imageName, evalDef.Image); err != nil {
		return nil, fmt.Errorf("failed to build image: %w", err)
	}
	log.Printf("Image %s built successfully", imageName)

	log.Printf("Skipping image validation step (configured from storage/database)")

	// Create runs for each selected model
	var runs []*EvalRun
	for _, model := range selectedModels {
		run, err := s.db.CreateEvalRun(evalID, model.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to create eval run for model %s: %w", model.ID, err)
		}
		runs = append(runs, run)

		// Start evaluation in background (image already built)
		go s.executeEval(run.ID, evalDef, model)
	}

	return runs, nil
}

// normalizeProvider converts capitalized provider names to lowercase
func normalizeProvider(provider string) string {
	return strings.ToLower(provider)
}

// executeEval executes an evaluation run with containerized execution
func (s *Service) executeEval(runID int64, evalDef *EvalDefinition, model *EvalModel) {
	// Update status to running
	if err := s.db.UpdateEvalRunStatus(runID, "running"); err != nil {
		logError(fmt.Sprintf("failed to update run status to running: %v", err))
	}

	// Create workspace manager
	workspaceMgr, err := NewWorkspaceManager("/tmp/eval-workspaces")
	if err != nil {
		if err := s.db.UpdateEvalRunStatus(runID, "failed"); err != nil {
			logError(fmt.Sprintf("failed to update run status to failed: %v", err))
		}
		logError(fmt.Sprintf("failed to create workspace manager: %v", err))
		return
	}

	// Create workspace for this specific run (eval + model combination)
	workspaceDir, err := workspaceMgr.CreateWorkspace(runID)
	if err != nil {
		if err := s.db.UpdateEvalRunStatus(runID, "failed"); err != nil {
			logError(fmt.Sprintf("failed to update run status to failed: %v", err))
		}
		logError(fmt.Sprintf("failed to create workspace: %v", err))
		return
	}
	defer func() {
		if err := workspaceMgr.CleanupWorkspace(workspaceDir); err != nil {
			logError(fmt.Sprintf("failed to cleanup workspace: %v", err))
		}
	}()

	// Create container executor
	executor, err := NewContainerExecutor()
	if err != nil {
		if err := s.db.UpdateEvalRunStatus(runID, "failed"); err != nil {
			logError(fmt.Sprintf("failed to update run status to failed: %v", err))
		}
		logError(fmt.Sprintf("failed to create container executor: %v", err))
		return
	}

	// Use pre-built image (already built in RunEval)
	imageName := fmt.Sprintf("eval-scriptschnell-%s:latest", evalDef.ID)
	ctx := context.Background()

	// Get API keys from environment AND from stored configuration
	env := s.collectAPIKeys()

	// Add stored OpenRouter token if available (from web UI configuration)
	if s.openRouterToken != "" {
		env["OPENROUTER_API_KEY"] = s.openRouterToken
		log.Printf("DEBUG: Main eval using OpenRouter token: '%s'", s.openRouterToken)
	}

	env["SCRIPTSCHNELL_LOG_LEVEL"] = "debug"
	env["SCRIPTSCHNELL_LOG_PATH"] = "/workspace/.logs/scriptschnell.log"

	// Enable full JSON output for eval tracking (includes all messages and tool outputs)
	env["SCRIPTSCHNELL_JSON_FULL"] = "1"

	// Prepare container config
	config := &ContainerConfig{
		Image:        imageName,
		WorkspaceDir: workspaceDir,
		Timeout:      consts.Timeout10Minutes,
		Env:          env,
	}

	// Execute eval in container with user prompt
	startTime := time.Now()
	result, err := executor.RunEval(ctx, config, evalDef.UserPrompt, model.ID, normalizeProvider(model.Provider), runID)
	responseTime := time.Since(startTime)

	// Save agent output and exit code regardless of error
	if result != nil {
		// Log assistant messages if enabled
		s.logAssistantMessages(result.Output)

		if updateErr := s.db.UpdateEvalRunAgentResult(runID, result.Output, result.ExitCode); updateErr != nil {
			logError(fmt.Sprintf("failed to save agent result for run %d: %v", runID, updateErr))
		}
	}

	if err != nil {
		if err := s.db.UpdateEvalRunStatus(runID, "failed"); err != nil {
			logError(fmt.Sprintf("failed to update run status to failed: %v", err))
		}
		logError(fmt.Sprintf("container execution failed for run %d: %v\nOutput: %s", runID, err, result.Output))
		return
	}

	// Parse usage from container output (JSON format from --json flag)
	usage, parseErr := s.parseUsageFromOutput(result.Output)
	if parseErr != nil {
		log.Printf("WARNING: failed to parse usage from JSON output for run %d: %v", runID, parseErr)
		// Continue with nil usage - test results will still be created with zero values
	}

	// Run CLI tests
	allPassed := true
	for _, testCase := range evalDef.CLITests {
		testResult := s.runCLITestInContainer(runID, testCase, evalDef, executor, config, responseTime, usage)
		if !testResult.Passed {
			allPassed = false
		}
		err := s.db.CreateEvalResult(testResult)
		if err != nil {
			logError(fmt.Sprintf("failed to save test result: %v", err))
		}
	}

	// Update status based on test results
	finalStatus := "completed"
	if !allPassed {
		finalStatus = "failed"
	}
	if err := s.db.UpdateEvalRunStatus(runID, finalStatus); err != nil {
		logError(fmt.Sprintf("failed to update run status to %s: %v", finalStatus, err))
	}
}

// runCLITestInContainer runs a single CLI test in container
func (s *Service) runCLITestInContainer(runID int64, testCase CLITestCase, evalDef *EvalDefinition, executor *ContainerExecutor, config *ContainerConfig, responseTime time.Duration, usage map[string]interface{}) *EvalResult {
	// Determine executable path
	execPath := filepath.Join("/workspace", evalDef.RunFile)

	// Get timeout from test case
	timeout := time.Duration(testCase.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Run test in container
	testResult, err := executor.RunCLITest(context.Background(), config, execPath, testCase.Args, timeout)

	// Generate container name for this specific test
	containerName := fmt.Sprintf("cli-test-%s-%d", testCase.ID, runID)

	// Extract usage info from the parsed usage map
	inputTokens := 0
	outputTokens := 0
	cost := 0.0

	if usage != nil {
		// Try various field names for input tokens
		for _, key := range []string{"input_tokens", "prompt_tokens"} {
			if val, ok := usage[key]; ok {
				switch v := val.(type) {
				case float64:
					inputTokens = int(v)
				case float32:
					inputTokens = int(v)
				case int:
					inputTokens = v
				case int64:
					inputTokens = int(v)
				}
				break
			}
		}

		// Try various field names for output tokens
		for _, key := range []string{"output_tokens", "completion_tokens"} {
			if val, ok := usage[key]; ok {
				switch v := val.(type) {
				case float64:
					outputTokens = int(v)
				case float32:
					outputTokens = int(v)
				case int:
					outputTokens = v
				case int64:
					outputTokens = int(v)
				}
				break
			}
		}

		// Extract cost
		for _, key := range []string{"cost", "total_cost"} {
			if val, ok := usage[key]; ok {
				switch v := val.(type) {
				case float64:
					cost = v
				case float32:
					cost = float64(v)
				}
				break
			}
		}
	}

	result := &EvalResult{
		RunID:                 runID,
		TestCaseID:            testCase.ID,
		ActualOutput:          testResult.Output,
		ExpectedOutput:        testCase.Expected,
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		EstimateCost:          cost,
		ResponseTime:          int(responseTime.Milliseconds()),
		ExecutionTime:         testResult.ExecutionTime,
		ContainerName:         containerName,
		RawOutput:             testResult.Output,
		Errors:                testResult.Error,
		DetailedExecutionInfo: fmt.Sprintf("Exit code: %d, Command executed with args: %v, Timeout: %v", testResult.ExitCode, testCase.Args, timeout),
	}

	// Compare output
	passed := strings.TrimSpace(result.ActualOutput) == strings.TrimSpace(result.ExpectedOutput)
	result.Passed = passed

	if !passed {
		result.Error = fmt.Sprintf("Output mismatch: got %q, expected %q", result.ActualOutput, result.ExpectedOutput)
	}

	if err != nil || testResult.Error != "" {
		result.Error = fmt.Sprintf("%v; %s", err, testResult.Error)
		result.Passed = false
	}

	return result
}

// collectAPIKeys collects all available API keys from environment
func (s *Service) collectAPIKeys() map[string]string {
	env := make(map[string]string)

	keys := []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"GEMINI_API_KEY",
		"MISTRAL_API_KEY",
		"GROQ_API_KEY",
		"OPENROUTER_API_KEY",
		"CEREBRAS_API_KEY",
		"OLLAMA_HOST",
		"PERPLEXITY_API_KEY",
	}

	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}

	return env
}

// parseUsageFromOutput extracts token usage from container output (JSON format)
// Handles multiple JSON output formats:
// - --json: Simple format with message and usage
// - --json-extended: One-liner per message with final usage line
// - --json-full: Single JSON object with messages array and usage
func (s *Service) parseUsageFromOutput(output string) (map[string]interface{}, error) {
	// The JSON output format from --json flag is:
	// {
	//   "message": "...",
	//   "usage": {
	//     "total_tokens": N,
	//     "input_tokens": N,
	//     "output_tokens": N,
	//     "cost": N
	//   }
	// }
	// The JSON-extended output format (--json-extended flag) outputs each message
	// as a JSON one-liner, ending with a final "role": "usage" object:
	// {"role": "user", "timestamp": "...", "content": "..."}
	// {"role": "assistant", "timestamp": "...", "tool_calls": [...], "content": "..."}
	// {"role": "usage", "timestamp": "...", "usage": {...}}
	//
	// The JSON-full output format (--json-full flag) outputs a single JSON object:
	// {
	//   "messages": [...],
	//   "final_message": "...",
	//   "usage": {...}
	// }

	// First, try to parse as --json-full format (single JSON object with messages array)
	var fullOutput struct {
		Messages     []map[string]interface{} `json:"messages"`
		FinalMessage string                   `json:"final_message"`
		Usage        map[string]interface{}   `json:"usage"`
	}
	if err := json.Unmarshal([]byte(output), &fullOutput); err == nil && fullOutput.Usage != nil {
		return fullOutput.Usage, nil
	}

	// Second, try to find the usage line in extended JSON format
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Skip lines that don't start with '{' (stderr output, logs, etc.)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		// Check if this line contains the usage object
		if strings.Contains(line, `"role": "usage"`) {
			var usageLine struct {
				Usage map[string]interface{} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(line), &usageLine); err == nil && usageLine.Usage != nil {
				return usageLine.Usage, nil
			}
		}
	}

	// Fallback to original format (single JSON object with usage)
	// Find the last complete JSON object that contains a usage field
	// We try multiple JSON objects in case stderr contains '{' characters
	var jsonEnds []int
	braceDepth := 0
	for i := 0; i < len(output); i++ {
		switch output[i] {
		case '{':
			braceDepth++
		case '}':
			braceDepth--
			if braceDepth == 0 {
				jsonEnds = append(jsonEnds, i+1)
			}
		}
	}

	// Try parsing from the end (most recent JSON)
	for idx := len(jsonEnds) - 1; idx >= 0; idx-- {
		jsonEnd := jsonEnds[idx]
		// Find matching start
		jsonStart := -1
		braceDepth = 0
		for i := jsonEnd - 1; i >= 0; i-- {
			switch output[i] {
			case '}':
				braceDepth++
			case '{':
				braceDepth--
				if braceDepth == 0 {
					jsonStart = i
					break
				}
			}
			if jsonStart != -1 {
				break
			}
		}

		if jsonStart == -1 {
			continue
		}

		jsonStr := output[jsonStart:jsonEnd]

		// Parse JSON
		var parsed struct {
			Usage map[string]interface{} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil && parsed.Usage != nil {
			// If input_tokens or output_tokens are not present but total_tokens is,
			// try to extract them from cached accumulated data or estimate
			// (This handles older output formats)
			if _, hasInput := parsed.Usage["input_tokens"]; !hasInput {
				if promptTokens, ok := parsed.Usage["prompt_tokens"]; ok {
					parsed.Usage["input_tokens"] = promptTokens
				}
			}
			if _, hasOutput := parsed.Usage["output_tokens"]; !hasOutput {
				if completionTokens, ok := parsed.Usage["completion_tokens"]; ok {
					parsed.Usage["output_tokens"] = completionTokens
				}
			}
			return parsed.Usage, nil
		}
	}

	return nil, fmt.Errorf("no JSON object with usage data found")
}

// Results operations

// GetEvalRuns gets evaluation runs
func (s *Service) GetEvalRuns(evalID string) ([]*EvalRun, error) {
	return s.db.GetEvalRuns(evalID)
}

// GetEvalResults gets results for a specific run
func (s *Service) GetEvalResults(runID int64) ([]EvalResult, error) {
	return s.db.GetEvalResults(runID)
}

// GetEvalStats gets aggregated statistics
func (s *Service) GetEvalStats(evalID string) ([]EvalStats, error) {
	return s.db.GetEvalStats(evalID)
}

// Helper function for logging errors
func logError(msg string) {
	log.Printf("ERROR: %s", msg)
}

// logAssistantMessages parses and logs assistant messages from JSON-extended output
func (s *Service) logAssistantMessages(output string) {
	s.mu.RLock()
	enabled := s.logAssistant
	s.mu.RUnlock()

	if !enabled {
		return
	}

	// Parse JSON-extended output (one JSON object per line)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}

		// Try to parse as a message object
		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		// Check if this is an assistant message
		role, hasRole := msg["role"].(string)
		if !hasRole || role != "assistant" {
			continue
		}

		// Log the full assistant message as JSON
		jsonBytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("ERROR: failed to marshal assistant message: %v", err)
			continue
		}

		// Output to stdout so it can be captured
		fmt.Printf("ASSISTANT_MESSAGE: %s\n", string(jsonBytes))
	}
}
