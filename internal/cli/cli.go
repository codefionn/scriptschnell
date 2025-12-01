package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/htmlconv"
	"github.com/statcode-ai/statcode-ai/internal/progress"
	"github.com/statcode-ai/statcode-ai/internal/provider"
	"github.com/statcode-ai/statcode-ai/internal/tui"
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
}

// CLI handles command-line interface using the orchestrator
type CLI struct {
	config       *config.Config
	providerMgr  *provider.Manager
	orchestrator *tui.Orchestrator
	options      *Options
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

	// Use the orchestrator to process the prompt
	err := c.orchestrator.ProcessPrompt(ctx, prompt, progressCallback, contextCallback, authCallback, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to process prompt: %w", err)
	}

	fmt.Println() // Final newline

	// Note: Modified files tracking would require exposing the session from orchestrator
	// For now, tool output will show which files were written

	return nil
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
