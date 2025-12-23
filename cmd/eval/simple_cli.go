package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// simpleCmd represents the simple evaluation command
var simpleCmd = &cobra.Command{
	Use:   "simple",
	Short: "Run simple model evaluation",
	Long: `Run simple model evaluation on test cases.

This is the original evaluation mode that tests models on individual prompts
with expected responses. Good for basic capability testing.

Examples:
  # Run simple evaluation with default config
  eval simple
  
  # Run specific models
  eval simple --models mistralai/devstral-2512,openai/gpt-4
  
  # Custom settings
  eval simple --temperature 0.3 --output my_results.json`,
	Run: runSimpleEval,
}

func init() {
	rootCmd.AddCommand(simpleCmd)
}

func runSimpleEval(cmd *cobra.Command, args []string) {
	fmt.Println("=== Simple Model Evaluation ===")
	fmt.Println()

	// Load configuration
	config, err := LoadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override config with command line arguments
	if modelsFlag != "" {
		enabledModels := parseModelList(modelsFlag)
		for i, model := range config.Models {
			model.Enabled = false
			for _, enabledID := range enabledModels {
				if model.ID == enabledID {
					model.Enabled = true
					break
				}
			}
			config.Models[i] = model
		}
	}

	config.Temperature = temperature
	if maxTokens > 0 {
		config.MaxTokens = maxTokens
	}
	if outputFile != "" {
		config.OutputFile = outputFile
	}
	config.Verbose = verbose

	// Validate configuration
	if err := config.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Display configuration
	enabledModels := config.GetEnabledModels()
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Models: %d enabled\n", len(enabledModels))
	fmt.Printf("  Test cases: %d\n", len(config.TestCases))
	fmt.Printf("  Temperature: %.1f\n", config.Temperature)
	fmt.Printf("  Max tokens: %d\n", config.MaxTokens)
	fmt.Printf("  Output file: %s\n", config.OutputFile)
	fmt.Println()

	// Display enabled models
	fmt.Println("Enabled models:")
	for _, model := range enabledModels {
		fmt.Printf("  - %s (%s)\n", model.DisplayName, model.ID)
	}
	fmt.Println()

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nInterrupt received, cancelling evaluation...")
		cancel()
	}()

	// Create evaluation runner
	runner, err := NewEvalRunner(config)
	if err != nil {
		log.Fatalf("Failed to create evaluation runner: %v", err)
	}

	// Add progress listener if verbose
	progressDisplay := NewProgressDisplay(verbose)
	runner.AddProgressListener(progressDisplay)

	// Run evaluation
	evalStart := time.Now()
	err = runner.Run(ctx)
	evalDuration := time.Since(evalStart)

	if err != nil {
		log.Fatalf("Evaluation failed: %v", err)
	}

	// Get results
	results := runner.GetResults()
	summary := runner.GenerateSummary()
	reporter := NewResultsReporter(verbose)

	// Save results
	if err := runner.SaveResults(config.OutputFile); err != nil {
		log.Printf("Warning: Failed to save results: %v", err)
	} else {
		fmt.Printf("Results saved to: %s\n", config.OutputFile)
	}

	// Save summary
	if err := runner.SaveSummary(summaryFile); err != nil {
		log.Printf("Warning: Failed to save summary: %v", err)
	} else {
		fmt.Printf("Summary saved to: %s\n", summaryFile)
	}

	// Export CSV if requested
	if exportCSV {
		csvFile := strings.TrimSuffix(config.OutputFile, ".json") + ".csv"
		if err := reporter.ExportToCSV(results, csvFile); err != nil {
			log.Printf("Warning: Failed to export CSV: %v", err)
		}
	}

	// Print summary
	reporter.PrintSummary(summary)

	if verbose {
		reporter.PrintDetailedResults(results)
	}

	reporter.PrintCostBreakdown(results)

	fmt.Printf("Total evaluation time: %v\n", evalDuration)
}

// parseModelList parses a comma-separated list of model IDs
func parseModelList(list string) []string {
	if list == "" {
		return nil
	}

	models := strings.Split(list, ",")
	for i, model := range models {
		models[i] = strings.TrimSpace(model)
	}
	return models
}

// ProgressDisplay implements ProgressListener for simple evaluation
type ProgressDisplay struct {
	verbose    bool
	lastUpdate time.Time
}

func NewProgressDisplay(verbose bool) *ProgressDisplay {
	return &ProgressDisplay{
		verbose:    verbose,
		lastUpdate: time.Now(),
	}
}

func (d *ProgressDisplay) OnProgressUpdate(progress EvalProgress) {
	// Only update display every 2 seconds to avoid spam
	if time.Since(d.lastUpdate) < 2*time.Second && progress.Status != "completed" && progress.Status != "failed" {
		return
	}
	d.lastUpdate = time.Now()

	switch progress.Status {
	case "running":
		fmt.Printf("\r⏳ Progress: Model %d/%d, Test %d/%d - Cost: $%.6f",
			progress.CompletedModels, progress.TotalModels,
			progress.CompletedTestCases, progress.TotalTestCases,
			progress.TotalCost)
	case "completed":
		fmt.Printf("\r✅ Evaluation completed!\n")
	case "failed":
		fmt.Printf("\r❌ Evaluation failed: %s\n", progress.Error)
	case "cancelled":
		fmt.Printf("\r⏹️ Evaluation cancelled\n")
	}
}

func (d *ProgressDisplay) OnResult(result EvalResult) {
	if d.verbose {
		status := "PASS"
		if !result.Success {
			status = "❌ FAIL"
		} else {
			status = "✅ PASS"
		}

		fmt.Printf("\n  Test %s on %s: %s (%v, $%.6f)\n",
			result.TestCaseID, result.ModelID, status, result.Duration, result.TotalCost)
	}
}
