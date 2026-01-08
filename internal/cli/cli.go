package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

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
	JSONExtended        bool
}

// CLI handles command-line interface using the orchestrator
type CLI struct {
	config       *config.Config
	providerMgr  *provider.Manager
	orchestrator *tui.Orchestrator
	options      *Options
}

func New(cfg *config.Config, providerMgr *provider.Manager, opts *Options) (*CLI, error) {
	// Priority order for model/provider selection:
	// 1. CLI flags (-model, -provider)
	// 2. Environment variables (SCRIPTSCHNELL_MODEL, SCRIPTSCHNELL_PROVIDER)
	// 3. Auto-detect from provider API keys

	// Check environment variables first if no CLI options
	if opts == nil || (opts.Model == "" && opts.Provider == "") {
		envModel := strings.TrimSpace(os.Getenv("SCRIPTSCHNELL_MODEL"))
		envProvider := strings.TrimSpace(os.Getenv("SCRIPTSCHNELL_PROVIDER"))

		if envModel != "" || envProvider != "" {
			if opts == nil {
				opts = &Options{}
			}
			if envModel != "" {
				opts.Model = envModel
			}
			if envProvider != "" {
				opts.Provider = envProvider
			}
		}
	}

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
		if c.options != nil && (c.options.JSONOutput || c.options.JSONExtended) {
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

	// Usage callback: session now accumulates usage internally, no-op here
	usageCallback := func(usage map[string]interface{}) error {
		// Usage is now accumulated directly in the session
		return nil
	}

	// Use the orchestrator to process the prompt
	err := c.orchestrator.ProcessPrompt(ctx, prompt, progressCallback, contextCallback, authCallback, nil, nil, usageCallback)
	if err != nil {
		return fmt.Errorf("failed to process prompt: %w", err)
	}

	if c.options == nil || (!c.options.JSONOutput && !c.options.JSONExtended) {
		fmt.Println() // Final newline
	}

	// Print accumulated usage statistics from session
	session := c.orchestrator.GetSession()
	if session != nil {
		usageStats := session.GetUsageStats()
		if len(usageStats) > 0 && (c.options == nil || (!c.options.JSONOutput && !c.options.JSONExtended)) {
			fmt.Fprintf(os.Stderr, "\n--- Usage Statistics ---\n")

			// Get totals from session
			totalTokens := session.GetTotalTokens()
			if totalTokens > 0 {
				fmt.Fprintf(os.Stderr, "Total tokens: %d\n", totalTokens)
			}

			// Show cached tokens if available
			cachedTokens := session.TotalCachedTokens + session.TotalCacheReadTokens
			if cachedTokens > 0 {
				percentage := float64(cachedTokens) / float64(totalTokens) * 100
				fmt.Fprintf(os.Stderr, "Cached tokens: %d (%.1f%%)\n", cachedTokens, percentage)
			}

			// Show cost if available
			totalCost := session.GetTotalCost()
			if totalCost > 0 {
				fmt.Fprintf(os.Stderr, "Total cost: $%.6f\n", totalCost)
			}

			fmt.Fprintf(os.Stderr, "------------------------\n")
		}
	}

	if c.options != nil && c.options.JSONOutput {
		return c.outputJSON()
	}

	if c.options != nil && c.options.JSONExtended {
		return c.outputJSONExtended()
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

// outputJSONExtended outputs all messages as JSON one-liners plus usage statistics.
func (c *CLI) outputJSONExtended() error {
	if c.orchestrator == nil {
		return fmt.Errorf("orchestrator not initialized")
	}

	session := c.orchestrator.GetSession()
	if session == nil {
		return fmt.Errorf("no session available")
	}

	messages := session.GetMessages()

	// Output each message as a JSON one-liner
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		// Build message object for output
		msgObj := map[string]interface{}{
			"role":      msg.Role,
			"timestamp": msg.Timestamp.Format(time.RFC3339),
		}

		if msg.Content != "" {
			msgObj["content"] = msg.Content
		}

		if msg.ToolID != "" {
			msgObj["tool_id"] = msg.ToolID
		}

		if msg.ToolName != "" {
			msgObj["tool_name"] = msg.ToolName
		}

		if len(msg.ToolCalls) > 0 {
			msgObj["tool_calls"] = msg.ToolCalls
		}

		// Marshal to single-line JSON
		data, err := json.Marshal(msgObj)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}
		fmt.Println(string(data))
	}

	// Output final usage statistics
	usageObj := map[string]interface{}{
		"role":      "usage",
		"timestamp": time.Now().Format(time.RFC3339),
	}

	if usage := c.buildUsageSummary(); len(usage) > 0 {
		usageObj["usage"] = usage
	}

	data, err := json.Marshal(usageObj)
	if err != nil {
		return fmt.Errorf("failed to marshal usage: %w", err)
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

	session := c.orchestrator.GetSession()
	if session == nil {
		return summary
	}

	totalTokens := session.GetTotalTokens()
	if totalTokens > 0 {
		summary["total_tokens"] = totalTokens
	}

	// Add separate input and output tokens for eval tracking
	if session.TotalPromptTokens > 0 {
		summary["input_tokens"] = session.TotalPromptTokens
	}
	if session.TotalCompletionTokens > 0 {
		summary["output_tokens"] = session.TotalCompletionTokens
	}

	cachedTokens := session.TotalCachedTokens + session.TotalCacheReadTokens
	if cachedTokens > 0 {
		summary["cached_tokens"] = cachedTokens
	}

	if session.TotalCacheCreationTokens > 0 {
		summary["cache_creation_input_tokens"] = session.TotalCacheCreationTokens
	}

	if session.TotalCacheReadTokens > 0 {
		summary["cache_read_input_tokens"] = session.TotalCacheReadTokens
	}

	if cachedTokens > 0 && totalTokens > 0 {
		cachePercent := float64(cachedTokens) / float64(totalTokens) * 100
		summary["cache_hit_percent"] = math.Round(cachePercent*10) / 10
	}

	totalCost := session.GetTotalCost()
	if totalCost > 0 {
		summary["cost"] = totalCost
	}

	return summary
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
