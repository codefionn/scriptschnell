package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/htmlconv"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/tui"
)

// Options represent CLI-specific authorization adjustments and model configuration.
type Options struct {
	DangerouslyAllowAll bool
	AllowAllNetwork     bool
	AllowedDirs         []string
	AllowedFiles        []string
	AllowedDomains      []string
	Model               string
	Provider            string
	JSONOutput          bool
}

// CLI handles command-line interface using the orchestrator
type CLI struct {
	config           *config.Config
	providerMgr      *provider.Manager
	orchestrator     *tui.Orchestrator
	options          *Options
	accumulatedUsage map[string]interface{}
}

func New(cfg *config.Config, providerMgr *provider.Manager, opts *Options) (*CLI, error) {
	// Handle CLI model/provider options or auto-detect from environment
	if opts != nil && (opts.Model != "" || opts.Provider != "") {
		if err := configureProviderFromOptions(providerMgr, opts); err != nil {
			return nil, fmt.Errorf("failed to configure provider from CLI options: %w", err)
		}
	} else if providerMgr.GetOrchestrationModel() == "" {
		// Auto-detect from environment variables if no model is configured
		fmt.Fprintln(os.Stderr, "No model configured, attempting auto-configuration from environment...")
		if err := autoConfigureFromEnvironment(providerMgr); err != nil {
			return nil, fmt.Errorf("failed to auto-configure from environment: %w", err)
		}
	}

	// Create orchestrator which handles all the tool execution logic
	// CLI mode is always unattended, so pass cliMode=true
	orch, err := tui.NewOrchestrator(cfg, providerMgr, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}

	cli := &CLI{
		config:       cfg,
		providerMgr:  providerMgr,
		orchestrator: orch,
		options:      opts,
	}

	return cli, nil
}

// Close releases resources associated with the CLI runner.
func (c *CLI) Close() error {
	if c.orchestrator != nil {
		c.orchestrator.Close()
	}
	return nil
}

// Run executes a single prompt using the orchestrator
func (c *CLI) Run(ctx context.Context, prompt string) error {
	// Convert HTML to markdown if detected
	if converted, wasConverted := htmlconv.ConvertIfHTML(prompt); wasConverted {
		prompt = converted
		fmt.Fprintln(os.Stderr, "[Detected and converted HTML to markdown]")
	}

	// Progress callback: print streaming to stdout and status to stderr
	progressCallback := func(update progress.Update) error {
		if c.options != nil && c.options.JSONOutput {
			return nil
		}
		normalized := progress.Normalize(update)
		if normalized.ShouldStatus() {
			if normalized.Message == "" {
				return nil
			}
			msg := normalized.Message
			if !strings.HasSuffix(msg, "\n") {
				msg += "\n"
			}
			fmt.Fprint(os.Stderr, msg)
			return nil
		}
		if normalized.Message == "" || !normalized.ShouldStream() {
			return nil
		}
		fmt.Print(normalized.Message)
		return nil
	}

	// Context callback: we can ignore this in CLI mode
	contextCallback := func(percent int, contextWindow int) error {
		return nil
	}

	// Authorization callback: auto-approve if dangerous-allow-all is set
	authCallback := func(toolName string, params map[string]interface{}, reason string) (bool, error) {
		if c.options != nil && c.options.DangerouslyAllowAll {
			// Auto-approve everything
			fmt.Fprintf(os.Stderr, "[Auto-approved: %s]\n", toolName)
			return true, nil
		}

		// Check if specific authorizations are set
		if c.options != nil {
			// Check allowed directories for write operations
			if toolName == "create_file" || toolName == "write_file_diff" {
				var filePath string
				if v, ok := params["path"].(string); ok {
					filePath = v
				} else if v, ok := params["file_path"].(string); ok {
					filePath = v
				}

				if filePath != "" {
					for _, allowedDir := range c.options.AllowedDirs {
						if strings.HasPrefix(filePath, allowedDir) {
							return true, nil
						}
					}
					for _, allowedFile := range c.options.AllowedFiles {
						if filePath == allowedFile {
							return true, nil
						}
					}
				}
			}

			// Check allowed domains for web operations
			if toolName == "web_search" {
				if c.options.AllowAllNetwork {
					return true, nil
				}
				// Could check specific domains here
			}
		}

		// In CLI mode without auto-approval, deny by default
		// (Interactive approval would require more complex TTY handling)
		return false, fmt.Errorf("authorization required but not granted via CLI flags")
	}

	// Usage callback: accumulate usage statistics
	usageCallback := func(usage map[string]interface{}) error {
		if c.accumulatedUsage == nil {
			c.accumulatedUsage = make(map[string]interface{})
		}

		// Helper to add numeric values
		addNumeric := func(key string, value interface{}) {
			num, ok := toFloat64(value)
			if !ok {
				return
			}
			if existing, ok := c.accumulatedUsage[key].(float64); ok {
				c.accumulatedUsage[key] = existing + num
			} else {
				c.accumulatedUsage[key] = num
			}
		}

		// Accumulate various token types
		addNumeric("prompt_tokens", usage["prompt_tokens"])
		addNumeric("completion_tokens", usage["completion_tokens"])
		addNumeric("total_tokens", usage["total_tokens"])
		addNumeric("cached_tokens", usage["cached_tokens"])
		addNumeric("input_tokens", usage["input_tokens"])
		addNumeric("output_tokens", usage["output_tokens"])
		addNumeric("cache_creation_input_tokens", usage["cache_creation_input_tokens"])
		addNumeric("cache_read_input_tokens", usage["cache_read_input_tokens"])
		// OpenRouter returns "cost" not "total_cost"
		addNumeric("cost", usage["cost"])
		addNumeric("total_cost", usage["total_cost"])

		return nil
	}

	// Use the orchestrator to process the prompt
	err := c.orchestrator.ProcessPrompt(ctx, prompt, progressCallback, contextCallback, authCallback, nil, nil, usageCallback)
	if err != nil {
		return fmt.Errorf("failed to process prompt: %w", err)
	}

	if c.options == nil || !c.options.JSONOutput {
		fmt.Println() // Final newline
	}

	// Print accumulated usage statistics
	if len(c.accumulatedUsage) > 0 && (c.options == nil || !c.options.JSONOutput) {
		fmt.Fprintf(os.Stderr, "\n--- Usage Statistics ---\n")

		// Calculate total tokens (try multiple field combinations)
		totalTokens := c.calculateTotalTokens()
		if totalTokens > 0 {
			fmt.Fprintf(os.Stderr, "Total tokens: %.0f\n", totalTokens)
		}

		// Show cached tokens if available
		if cached := c.getUsageValue("cached_tokens"); cached > 0 {
			percentage := (cached / totalTokens) * 100
			fmt.Fprintf(os.Stderr, "Cached tokens: %.0f (%.1f%%)\n", cached, percentage)
		}

		// Show cost if available (OpenRouter returns "cost" in dollars)
		cost := c.getUsageValue("cost")
		if cost == 0 {
			cost = c.getUsageValue("total_cost")
		}
		if cost > 0 {
			fmt.Fprintf(os.Stderr, "Total cost: $%.6f\n", cost)
		}

		fmt.Fprintf(os.Stderr, "------------------------\n")
	}

	if c.options != nil && c.options.JSONOutput {
		return c.outputJSON()
	}

	// Note: Modified files tracking would require exposing the session from orchestrator
	// For now, tool output will show which files were written

	return nil
}

// outputJSON marshals the last assistant response and available usage into JSON.
func (c *CLI) outputJSON() error {
	result := map[string]interface{}{
		"message": c.lastAssistantMessage(),
	}

	if usage := c.buildUsageSummary(); len(usage) > 0 {
		result["usage"] = usage
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON output: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// lastAssistantMessage returns the content of the latest assistant message.
func (c *CLI) lastAssistantMessage() string {
	if c.orchestrator == nil {
		return ""
	}

	session := c.orchestrator.GetSession()
	if session == nil {
		return ""
	}

	messages := session.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil || msg.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		return msg.Content
	}

	return ""
}

// buildUsageSummary prepares usage information suitable for JSON output.
func (c *CLI) buildUsageSummary() map[string]interface{} {
	summary := make(map[string]interface{})

	totalTokens := c.calculateTotalTokens()
	if totalTokens > 0 {
		summary["total_tokens"] = int(totalTokens)
	}

	cachedTokens := c.getUsageValue("cached_tokens")
	if cachedTokens > 0 {
		summary["cached_tokens"] = int(cachedTokens)
	}

	cacheCreation := c.getUsageValue("cache_creation_input_tokens")
	if cacheCreation > 0 {
		summary["cache_creation_input_tokens"] = int(cacheCreation)
	}

	cacheRead := c.getUsageValue("cache_read_input_tokens")
	if cacheRead > 0 {
		summary["cache_read_input_tokens"] = int(cacheRead)
	}

	cachePercent := c.cacheHitPercent(totalTokens, cachedTokens, cacheRead)
	if cachePercent > 0 {
		summary["cache_hit_percent"] = cachePercent
	}

	cost := c.getUsageValue("cost")
	if cost == 0 {
		cost = c.getUsageValue("total_cost")
	}
	if cost > 0 {
		summary["cost"] = cost
	}

	return summary
}

// cacheHitPercent calculates prompt cache hit percentage when available.
func (c *CLI) cacheHitPercent(totalTokens, cachedTokens, cacheReadTokens float64) float64 {
	if totalTokens <= 0 {
		return 0
	}

	var cachedBasis float64
	switch {
	case cachedTokens > 0:
		cachedBasis = cachedTokens
	case cacheReadTokens > 0:
		cachedBasis = cacheReadTokens
	default:
		return 0
	}

	return math.Round(((cachedBasis/totalTokens)*100)*10) / 10
}

// getUsageValue retrieves an accumulated usage field as float64.
func (c *CLI) getUsageValue(key string) float64 {
	if c.accumulatedUsage == nil {
		return 0
	}
	if value, ok := c.accumulatedUsage[key]; ok {
		if num, ok := value.(float64); ok {
			return num
		}
	}
	return 0
}

// calculateTotalTokens computes total tokens from available usage fields.
func (c *CLI) calculateTotalTokens() float64 {
	if c.accumulatedUsage == nil {
		return 0
	}

	if total := c.getUsageValue("total_tokens"); total > 0 {
		return total
	}

	var totalTokens float64

	// For providers that use input_tokens + output_tokens
	if input := c.getUsageValue("input_tokens"); input > 0 {
		totalTokens += input
	}
	if output := c.getUsageValue("output_tokens"); output > 0 {
		totalTokens += output
	}

	// For providers that use prompt_tokens + completion_tokens
	if prompt := c.getUsageValue("prompt_tokens"); prompt > 0 {
		totalTokens += prompt
	}
	if completion := c.getUsageValue("completion_tokens"); completion > 0 {
		totalTokens += completion
	}

	return totalTokens
}

// toFloat64 converts various numeric types to float64 for accumulation.
func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	case json.Number:
		num, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return num, true
	default:
		return 0, false
	}
}

// configureProviderFromOptions configures a provider based on CLI options
func configureProviderFromOptions(providerMgr *provider.Manager, opts *Options) error {
	if opts.Provider == "" || opts.Model == "" {
		return fmt.Errorf("both --provider and --model must be specified together")
	}

	// Get API key from environment
	apiKey := getAPIKeyForProvider(opts.Provider)
	if apiKey == "" {
		return fmt.Errorf("no API key found for provider %s", opts.Provider)
	}

	// Add provider with API listing if not exists
	ctx := context.Background()
	if err := providerMgr.AddProviderWithAPIListing(ctx, opts.Provider, apiKey); err != nil {
		return fmt.Errorf("failed to add provider %s: %w", opts.Provider, err)
	}

	// Set the model for both orchestration and summarization
	if err := providerMgr.SetOrchestrationModel(opts.Model); err != nil {
		return fmt.Errorf("failed to set orchestration model: %w", err)
	}
	if err := providerMgr.SetSummarizeModel(opts.Model); err != nil {
		return fmt.Errorf("failed to set summarization model: %w", err)
	}

	return nil
}

// autoConfigureFromEnvironment auto-detects provider from environment variables
func autoConfigureFromEnvironment(providerMgr *provider.Manager) error {
	// Providers that can be auto-configured via environment variables.
	candidates := []string{
		"anthropic",
		"openai",
		"google",
		"mistral",
		"openrouter",
		"cerebras",
		"groq",
	}

	ctx := context.Background()

	for _, providerName := range candidates {
		apiKey := provider.ResolveAPIKey(providerName)
		if apiKey == "" {
			continue
		}

		if err := providerMgr.AddProviderWithAPIListing(ctx, providerName, apiKey); err != nil {
			return fmt.Errorf("failed to add provider %s: %w", providerName, err)
		}

		// Use shared function to choose and configure default model
		modelName, err := providerMgr.ChooseDefaultModel(providerName, provider.PreferredModels[providerName])
		if err != nil {
			return err
		}

		if err := providerMgr.SetOrchestrationModel(modelName); err != nil {
			return fmt.Errorf("failed to set orchestration model: %w", err)
		}
		if err := providerMgr.SetSummarizeModel(modelName); err != nil {
			return fmt.Errorf("failed to set summarization model: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Auto-configured provider: %s (model: %s)\n", providerName, modelName)
		return nil
	}

	// No providers detected; build a helpful error message.
	missingVars := make(map[string]struct{})
	for _, providerName := range candidates {
		for _, envVar := range provider.EnvVarHints(providerName) {
			missingVars[envVar] = struct{}{}
		}
	}

	if len(missingVars) == 0 {
		return fmt.Errorf("no supported provider environment variables found")
	}

	var hints []string
	for envVar := range missingVars {
		hints = append(hints, envVar)
	}
	sort.Strings(hints)

	return fmt.Errorf("no API key found in environment (%s)", strings.Join(hints, ", "))
}

// getAPIKeyForProvider retrieves the API key for a given provider from environment
func getAPIKeyForProvider(providerName string) string {
	return provider.ResolveAPIKey(providerName)
}
