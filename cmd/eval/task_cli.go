package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	taskConfigFile  string
	taskTasksDir    string
	taskModelIDs    []string
	taskOutputFile  string
	taskVerbose     bool
	taskTimeout     time.Duration
	taskTemperature float64
	taskMaxTokens   int
	taskClean       bool
)

// taskCmd represents the task evaluation command
var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Run agentic task evaluation",
	Long: `Run agentic task evaluation on multiple models.

Task evaluation tests models on complex multi-step problems that require
autonomous problem-solving, planning, and implementation.

Examples:
  # Run task evaluation with default config
  eval task
  
  # Run specific task
  eval task --task-dir tasks/calculator-agentic.json
  
  # Run all tasks in directory
  eval task --tasks-dir tasks/
  
  # Evaluate specific models
  eval task --models mistralai/devstral-2512 openai/gpt-4
  
  # Custom settings
  eval task --temperature 0.3 --timeout 10m --output results.json`,
	Run: runTaskEval,
}

func init() {
	rootCmd.AddCommand(taskCmd)

	taskCmd.Flags().StringVar(&taskConfigFile, "config", "", "Configuration file (JSON)")
	taskCmd.Flags().StringVar(&taskTasksDir, "tasks-dir", "tasks", "Directory containing task definitions")
	taskCmd.Flags().StringSliceVar(&taskModelIDs, "models", []string{}, "Specific model IDs to evaluate (default: all enabled)")
	taskCmd.Flags().StringVar(&taskOutputFile, "output", "task_eval_results.json", "Output file for results")
	taskCmd.Flags().BoolVar(&taskVerbose, "verbose", false, "Enable verbose output")
	taskCmd.Flags().DurationVar(&taskTimeout, "timeout", 30*time.Minute, "Overall timeout")
	taskCmd.Flags().Float64Var(&taskTemperature, "temperature", 0.7, "LLM temperature (0.0-1.0)")
	taskCmd.Flags().IntVar(&taskMaxTokens, "max-tokens", 4096, "Maximum tokens per request")
	taskCmd.Flags().BoolVar(&taskClean, "clean", false, "Clean temporary files after evaluation")
}

// TaskProgressDisplay handles displaying progress for task evaluation
type TaskProgressDisplay struct {
	verbose    bool
	lastUpdate time.Time
}

func NewTaskProgressDisplay(verbose bool) *TaskProgressDisplay {
	return &TaskProgressDisplay{
		verbose:    verbose,
		lastUpdate: time.Now(),
	}
}

func (d *TaskProgressDisplay) OnTaskProgressUpdate(progress TaskEvalProgress) {
	// Only update display every 2 seconds to avoid spam
	if time.Since(d.lastUpdate) < 2*time.Second && progress.Status != "completed" && progress.Status != "failed" {
		return
	}
	d.lastUpdate = time.Now()

	switch progress.Status {
	case "running":
		fmt.Printf("\r%s Evaluating: Model %d/%d, Task %d/%d - Cost: $%.6f",
			color.YellowString("⏳"),
			progress.CompletedModels, progress.TotalModels,
			progress.CompletedTasks, progress.TotalTasks,
			progress.TotalCost)
	case "completed":
		fmt.Printf("\r%s Task evaluation completed!\n",
			color.GreenString("✅"))
	case "failed":
		fmt.Printf("\r%s Task evaluation failed: %s\n",
			color.RedString("❌"), progress.Error)
	case "cancelled":
		fmt.Printf("\r%s Task evaluation cancelled\n",
			color.YellowString("⏹️"))
	}
}

func (d *TaskProgressDisplay) OnTaskResult(result TaskEvalResult) {
	if d.verbose {
		status := "PASS"
		if !result.Success {
			status = color.RedString("FAIL")
		} else {
			status = color.GreenString("PASS")
		}

		fmt.Printf("\n  Task %s on %s: %s (%v, $%.6f)\n",
			result.TaskID, result.ModelID, status, result.Duration, result.TotalCost)
	}
}

func runTaskEval(cmd *cobra.Command, args []string) {
	fmt.Println(color.CyanString("=== Agentic Task Evaluation ===\n"))

	// Load configuration
	config, err := LoadConfig(taskConfigFile)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Override config with command line flags
	if taskTemperature != 0.7 {
		config.Temperature = taskTemperature
	}
	if taskMaxTokens != 4096 {
		config.MaxTokens = taskMaxTokens
	}
	if taskTimeout != 30*time.Minute {
		config.Timeout = taskTimeout
	}
	config.Verbose = taskVerbose
	config.OutputFile = taskOutputFile

	// Filter models if specified
	if len(taskModelIDs) > 0 {
		enabledModels := make([]ModelConfig, 0)
		for _, model := range config.Models {
			for _, modelID := range taskModelIDs {
				if model.ID == modelID {
					model.Enabled = true
					enabledModels = append(enabledModels, model)
					break
				}
			}
		}
		config.Models = enabledModels
	}

	// Load task definitions
	var tasks []TaskTestCase
	if _, err := os.Stat(taskTasksDir); os.IsNotExist(err) {
		// Assume tasksDir is a single file
		task, err := LoadTaskTestCase(taskTasksDir)
		if err != nil {
			fmt.Printf("Error loading task from %s: %v\n", taskTasksDir, err)
			os.Exit(1)
		}
		tasks = []TaskTestCase{task}
	} else {
		// Load all tasks from directory
		tasks, err = LoadTaskTestCasesFromDir(taskTasksDir)
		if err != nil {
			fmt.Printf("Error loading tasks from directory %s: %v\n", taskTasksDir, err)
			os.Exit(1)
		}
	}

	if len(tasks) == 0 {
		fmt.Printf("No tasks found in %s\n", taskTasksDir)
		os.Exit(1)
	}

	fmt.Printf("Configuration:\n")
	fmt.Printf("  Tasks: %d (from %s)\n", len(tasks), taskTasksDir)
	fmt.Printf("  Models: %d enabled\n", len(config.GetEnabledModels()))
	fmt.Printf("  Temperature: %.1f\n", config.Temperature)
	fmt.Printf("  Max Tokens: %d\n", config.MaxTokens)
	fmt.Printf("  Timeout: %v\n", config.Timeout)
	fmt.Printf("  Output: %s\n", config.OutputFile)
	fmt.Println()

	// Display tasks
	fmt.Println(color.GreenString("Tasks to evaluate:"))
	for i, task := range tasks {
		fmt.Printf("  %d. %s (%s) - %s\n", i+1, task.Name, task.Difficulty, task.Category)
	}
	fmt.Println()

	// Create context with cancellation
	ctx, cancel := context.WithTimeout(context.Background(), taskTimeout)
	defer cancel()

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\n" + color.YellowString("Interrupt received, cancelling evaluation..."))
		cancel()
	}()

	// Create task evaluation runner
	runner, err := NewTaskEvalRunner(config)
	if err != nil {
		fmt.Printf("Error creating task runner: %v\n", err)
		os.Exit(1)
	}
	defer runner.cleanup()

	// Add progress display
	progressDisplay := NewTaskProgressDisplay(taskVerbose)
	runner.AddProgressListener(progressDisplay)

	// Run the evaluation
	evalStart := time.Now()
	err = runner.Run(ctx, tasks)
	evalDuration := time.Since(evalStart)

	if err != nil {
		fmt.Printf("\nError during evaluation: %v\n", err)
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Println(color.RedString("Evaluation timed out"))
		}
		os.Exit(1)
	}

	summary := runner.GenerateSummary()

	// Display summary
	PrintTaskSummary(summary)

	// Additional timing info
	fmt.Printf("Total evaluation time: %v\n", evalDuration)

	// Save results
	resultsFile := fmt.Sprintf("task_results_%s.json", time.Now().Format("20060102_150405"))
	if err := runner.SaveResults(resultsFile); err != nil {
		fmt.Printf("Warning: Failed to save detailed results: %v\n", err)
	} else {
		fmt.Printf("Detailed results saved to: %s\n", resultsFile)
	}

	// Save summary
	summaryFile := fmt.Sprintf("task_summary_%s.json", time.Now().Format("20060102_150405"))
	if err := runner.SaveSummary(summaryFile); err != nil {
		fmt.Printf("Warning: Failed to save summary: %v\n", err)
	} else {
		fmt.Printf("Summary saved to: %s\n", summaryFile)
	}

	// Exit with appropriate code
	if summary.SuccessRate < 0.5 {
		fmt.Println(color.RedString("Overall success rate below 50%"))
		os.Exit(1)
	}

	fmt.Println(color.GreenString("Task evaluation completed successfully!"))
}
