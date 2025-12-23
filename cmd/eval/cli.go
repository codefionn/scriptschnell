package main

import (
	"log"

	"github.com/spf13/cobra"
)

var (
	configFile  string
	modelsFlag  string
	temperature float64
	maxTokens   int
	outputFile  string
	summaryFile string
	verbose     bool
	exportCSV   bool
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "eval",
	Short: "Model evaluation tool for scriptschnell",
	Long: `Eval is a comprehensive model evaluation tool that supports:

- Simple model evaluation: Test models on individual prompts
- Task-based evaluation: Test models on complex agentic tasks
- Multiple providers: OpenRouter and other LLM providers
- Detailed analytics: Cost analysis, performance metrics, rankings

Use 'eval help <command>' for more information on a specific command.

If no subcommand is specified, runs simple evaluation by default.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Default to simple evaluation if no subcommand
		runSimpleEval(cmd, args)
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func init() {
	// Root command flags (shared with the simple evaluation command)
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Configuration file (JSON)")
	rootCmd.PersistentFlags().StringVar(&modelsFlag, "models", "", "Comma-separated list of model IDs to enable")
	rootCmd.PersistentFlags().Float64Var(&temperature, "temperature", 0.7, "Temperature parameter")
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 4096, "Maximum tokens")
	rootCmd.PersistentFlags().StringVar(&outputFile, "output", "eval_results.json", "Output file for results")
	rootCmd.PersistentFlags().StringVar(&summaryFile, "summary", "eval_summary.json", "Output file for summary")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	rootCmd.PersistentFlags().BoolVar(&exportCSV, "csv", false, "Also export to CSV format")
}
