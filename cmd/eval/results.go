package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
)

// EvalSummary provides a summary of evaluation results
type EvalSummary struct {
	EvalTime          time.Time                  `json:"eval_time"`
	Config            EvalConfig                 `json:"config"`
	Progress          EvalProgress               `json:"progress"`
	ModelSummaries    map[string]ModelSummary    `json:"model_summaries"`
	CategorySummaries map[string]CategorySummary `json:"category_summaries"`
	TaskSummaries     map[string]TaskSummary     `json:"task_summaries,omitempty"`

	// Overall metrics
	TotalCost     float64       `json:"total_cost"`
	TotalTokens   int           `json:"total_tokens"`
	TotalDuration time.Duration `json:"total_duration"`
	SuccessRate   float64       `json:"success_rate"`

	// Rankings
	ModelRankings    []ModelRanking    `json:"model_rankings"`
	CategoryRankings []CategoryRanking `json:"category_rankings"`
}

// ModelSummary provides summary stats for a single model
type ModelSummary struct {
	ModelID      string  `json:"model_id"`
	ModelName    string  `json:"model_name"`
	TotalCost    float64 `json:"total_cost"`
	TotalTokens  int     `json:"total_tokens"`
	SuccessCount int     `json:"success_count"`
	TotalCount   int     `json:"total_count"`
	AvgLatency   float64 `json:"avg_latency_ms"`

	// Calculated fields
	SuccessRate    float64 `json:"success_rate"`
	CostPerToken   float64 `json:"cost_per_token"`
	CostPerSuccess float64 `json:"cost_per_success"`
}

// CategorySummary provides summary stats for test categories
type CategorySummary struct {
	Category     string  `json:"category"`
	SuccessCount int     `json:"success_count"`
	TotalCount   int     `json:"total_count"`
	SuccessRate  float64 `json:"success_rate"`
}

// TaskSummary provides per-task statistics (for task-based evals)
type TaskSummary struct {
	TaskID       string `json:"task_id"`
	TaskName     string `json:"task_name"`
	TaskCategory string `json:"task_category"`
	Difficulty   string `json:"difficulty"`

	// Performance metrics
	TotalCount   int     `json:"total_count"`
	SuccessCount int     `json:"success_count"`
	SuccessRate  float64 `json:"success_rate"`

	// Cost and timing
	TotalCost   float64       `json:"total_cost"`
	TotalTokens int           `json:"total_tokens"`
	AvgDuration time.Duration `json:"avg_duration"`
}

// CategoryStats provides category performance for a model
type CategoryStats struct {
	TotalCount   int           `json:"total_count"`
	SuccessCount int           `json:"success_count"`
	SuccessRate  float64       `json:"success_rate"`
	AvgCost      float64       `json:"avg_cost"`
	AvgDuration  time.Duration `json:"avg_duration"`
}

// ModelRanking ranks models by overall performance
type ModelRanking struct {
	Rank           int     `json:"rank"`
	ModelID        string  `json:"model_id"`
	ModelName      string  `json:"model_name"`
	Score          float64 `json:"score"`
	SuccessRate    float64 `json:"success_rate"`
	TotalCost      float64 `json:"total_cost"`
	CostPerSuccess float64 `json:"cost_per_success"`
}

// CategoryRanking ranks performance by category
type CategoryRanking struct {
	Category        string  `json:"category"`
	BestModel       string  `json:"best_model"`
	BestSuccessRate float64 `json:"best_success_rate"`
	AvgSuccessRate  float64 `json:"avg_success_rate"`
}

// ResultsReporter handles output and reporting of evaluation results
type ResultsReporter struct {
	colorEnabled bool
	verbose      bool
}

// NewResultsReporter creates a new results reporter
func NewResultsReporter(verbose bool) *ResultsReporter {
	return &ResultsReporter{
		colorEnabled: true, // could detect based on terminal
		verbose:      verbose,
	}
}

// PrintSummary prints a formatted summary to stdout
func (r *ResultsReporter) PrintSummary(summary EvalSummary) {
	fmt.Print("\n" + color.CyanString("=== Evaluation Summary ===\n"))
	fmt.Printf("Eval Time: %s\n", summary.EvalTime.Format(time.RFC3339))
	fmt.Printf("Status: %s\n", summary.Progress.Status)

	if summary.Progress.EndTime != nil {
		duration := summary.Progress.EndTime.Sub(summary.Progress.StartTime)
		fmt.Printf("Duration: %v\n", duration)
	}

	fmt.Printf("Total Models: %d\n", summary.Progress.TotalModels)
	fmt.Printf("Total Test Cases: %d\n", summary.Progress.TotalTestCases)
	fmt.Printf("Total Results: %d\n", summary.Progress.ResultsCount)
	fmt.Printf("Total Cost: $%.6f USD\n", summary.Progress.TotalCost)
	fmt.Printf("Total Tokens: %d\n", summary.Progress.TotalTokens)
	fmt.Printf("\n")

	// Print model summaries
	r.printModelSummaries(summary.ModelSummaries)

	// Print category summaries
	r.printCategorySummaries(summary.CategorySummaries)

	fmt.Print("\n" + color.CyanString("=== End Summary ===\n\n"))
}

// printModelSummaries prints model-specific results
func (r *ResultsReporter) printModelSummaries(models map[string]ModelSummary) {
	fmt.Print(color.GreenString("Model Performance:\n"))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tSUCCESS RATE\tCOST\tTOKENS\tCOST/TOKEN")

	for _, summary := range models {
		successRate := float64(summary.SuccessCount) / float64(summary.TotalCount) * 100
		costPerToken := summary.TotalCost / float64(summary.TotalTokens)

		modelName := summary.ModelName
		if len(modelName) > 20 {
			modelName = modelName[:20] + "..."
		}

		fmt.Fprintf(w, "%s\t%.1f%%\t$%.6f\t%d\t$%.8f\n",
			modelName,
			successRate,
			summary.TotalCost,
			summary.TotalTokens,
			costPerToken,
		)
	}

	w.Flush()
	fmt.Printf("\n")
}

// printCategorySummaries prints category-specific results
func (r *ResultsReporter) printCategorySummaries(categories map[string]CategorySummary) {
	fmt.Print(color.GreenString("Category Performance:\n"))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tSUCCESS RATE\tCOUNT")

	for category, summary := range categories {
		successRate := float64(summary.SuccessCount) / float64(summary.TotalCount) * 100

		fmt.Fprintf(w, "%s\t%.1f%%\t%d/%d\n",
			category,
			successRate,
			summary.SuccessCount,
			summary.TotalCount,
		)
	}

	w.Flush()
	fmt.Printf("\n")
}

// PrintDetailedResults prints all individual test results
func (r *ResultsReporter) PrintDetailedResults(results []EvalResult) {
	if !r.verbose {
		return
	}

	fmt.Print(color.YellowString("=== Detailed Results ===\n"))

	// Group by model
	modelResults := make(map[string][]EvalResult)
	for _, result := range results {
		modelResults[result.ModelID] = append(modelResults[result.ModelID], result)
	}

	for modelID, modelResults := range modelResults {
		fmt.Print("\n" + color.CyanString("Model: %s\n", modelID))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TEST\tSTATUS\tCOST\tTOKENS\tLATENCY")

		for _, result := range modelResults {
			status := "PASS"
			if !result.Success {
				status = color.RedString("FAIL")
			} else {
				status = color.GreenString("PASS")
			}

			latency := result.Duration.Milliseconds()

			fmt.Fprintf(w, "%s\t%s\t$%.6f\t%d\t%dms\n",
				result.TestCaseID,
				status,
				result.TotalCost,
				result.TotalTokens,
				latency,
			)
		}

		w.Flush()
	}

	fmt.Print(color.YellowString("=== End Detailed Results ===\n\n"))
}

// ExportToCSV exports results to CSV format
func (r *ResultsReporter) ExportToCSV(results []EvalResult, filename string) error {
	var file *os.File
	var err error

	if filename == "-" {
		file = os.Stdout
	} else {
		file, err = os.Create(filename)
		if err != nil {
			return fmt.Errorf("failed to create CSV file: %w", err)
		}
		defer file.Close()
	}

	// Write header
	fmt.Fprintf(file, "model_id,model_name,test_case_id,test_case_name,category,success,error,")
	fmt.Fprintf(file, "start_time,end_time,duration_ms,")
	fmt.Fprintf(file, "prompt_tokens,completion_tokens,total_tokens,")
	fmt.Fprintf(file, "prompt_cost,completion_cost,total_cost,currency,")
	fmt.Fprintf(file, "temperature,max_tokens,response\n")

	// Write data rows
	for _, result := range results {
		fmt.Fprintf(file, "%s,%s,%s,%s,%s,%t,",
			result.ModelID, result.ModelName, result.TestCaseID,
			result.TestCaseName, result.TestCategory, result.Success)

		// Escape error message for CSV
		errorStr := result.Error
		if errorStr != "" {
			errorStr = `"` + strings.ReplaceAll(errorStr, `"`, `""`) + `"`
		}
		fmt.Fprintf(file, "%s,", errorStr)

		fmt.Fprintf(file, "%s,%s,%d,",
			result.StartTime.Format(time.RFC3339),
			result.EndTime.Format(time.RFC3339),
			result.Duration.Milliseconds())

		fmt.Fprintf(file, "%d,%d,%d,",
			result.PromptTokens, result.CompletionTokens, result.TotalTokens)

		fmt.Fprintf(file, "%.6f,%.6f,%.6f,%s,",
			result.PromptCost, result.CompletionCost, result.TotalCost, result.Currency)

		fmt.Fprintf(file, "%.1f,%d,",
			result.Temperature, result.MaxTokens)

		// Escape response for CSV
		response := result.Response
		if response != "" {
			response = `"` + strings.ReplaceAll(response, `"`, `""`) + `"`
		}
		fmt.Fprintf(file, "%s\n", response)
	}

	log.Printf("Results exported to CSV: %s", filename)
	return nil
}

// ExportToJSON exports results to JSON format
func (r *ResultsReporter) ExportToJSON(results []EvalResult, filename string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write JSON file: %w", err)
	}

	log.Printf("Results exported to JSON: %s", filename)
	return nil
}

// PrintCostBreakdown prints a detailed cost analysis
func (r *ResultsReporter) PrintCostBreakdown(results []EvalResult) {
	fmt.Print(color.MagentaString("=== Cost Breakdown ===\n"))

	// Total costs
	totalCost := 0.0
	totalTokens := 0
	modelCosts := make(map[string]float64)
	categoryCosts := make(map[string]float64)

	for _, result := range results {
		if result.Success {
			totalCost += result.TotalCost
			totalTokens += result.TotalTokens
			modelCosts[result.ModelID] += result.TotalCost
			categoryCosts[result.TestCategory] += result.TotalCost
		}
	}

	fmt.Printf("Total Cost: $%.6f USD\n", totalCost)
	fmt.Printf("Total Tokens: %d\n", totalTokens)
	fmt.Printf("Average Cost per Token: $%.8f\n", totalCost/float64(totalTokens))

	fmt.Print("\n" + color.MagentaString("Cost by Model:\n"))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tCOST\tTOKENS\tCOST/TOKEN\tPERCENT")

	for modelID, cost := range modelCosts {
		percent := cost / totalCost * 100
		// Find token count for this model
		modelTokens := 0
		for _, result := range results {
			if result.ModelID == modelID && result.Success {
				modelTokens += result.TotalTokens
			}
		}
		costPerToken := cost / float64(modelTokens)

		fmt.Fprintf(w, "%s\t$%.6f\t%d\t$%.8f\t%.1f%%\n",
			modelID, cost, modelTokens, costPerToken, percent)
	}
	w.Flush()

	fmt.Print("\n" + color.MagentaString("Cost by Category:\n"))
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tCOST\tPERCENT")

	for category, cost := range categoryCosts {
		percent := cost / totalCost * 100
		fmt.Fprintf(w, "%s\t$%.6f\t%.1f%%\n", category, cost, percent)
	}
	w.Flush()

	fmt.Print(color.MagentaString("=== End Cost Breakdown ===\n\n"))
}

// CalculateStatistics calculates additional statistics for results
func (r *ResultsReporter) CalculateStatistics(results []EvalResult) map[string]interface{} {
	stats := make(map[string]interface{})

	successCount := 0
	totalCost := 0.0
	totalTokens := 0
	latencies := make([]int64, 0)

	for _, result := range results {
		if result.Success {
			successCount++
			totalCost += result.TotalCost
			totalTokens += result.TotalTokens
		}
		latencies = append(latencies, result.Duration.Milliseconds())
	}

	// Calculate average latency
	var avgLatency int64
	if len(latencies) > 0 {
		for _, latency := range latencies {
			avgLatency += latency
		}
		avgLatency /= int64(len(latencies))
	}

	stats["total_results"] = len(results)
	stats["success_count"] = successCount
	stats["success_rate"] = float64(successCount) / float64(len(results))
	stats["total_cost"] = totalCost
	stats["total_tokens"] = totalTokens
	stats["average_latency_ms"] = avgLatency
	stats["cost_per_token"] = totalCost / float64(totalTokens)

	return stats
}

// GenerateTaskSummary creates summary for task evaluation results
func GenerateTaskSummary(results []TaskEvalResult, config EvalConfig) EvalSummary {
	summary := EvalSummary{
		EvalTime:          time.Now(),
		Config:            config,
		ModelSummaries:    make(map[string]ModelSummary),
		TaskSummaries:     make(map[string]TaskSummary),
		CategorySummaries: make(map[string]CategorySummary),
	}

	// Calculate model and task summaries
	for _, result := range results {
		modelID := result.ModelID
		taskID := result.TaskID
		category := result.TaskCategory

		// Model summary
		if _, exists := summary.ModelSummaries[modelID]; !exists {
			summary.ModelSummaries[modelID] = ModelSummary{
				ModelID:   modelID,
				ModelName: result.ModelName,
			}
		}

		modelSummary := summary.ModelSummaries[modelID]
		modelSummary.TotalCount++
		modelSummary.TotalCost += result.TotalCost
		modelSummary.TotalTokens += result.TotalTokens
		if result.Success {
			modelSummary.SuccessCount++
		}
		summary.ModelSummaries[modelID] = modelSummary

		// Task summary
		if _, exists := summary.TaskSummaries[taskID]; !exists {
			summary.TaskSummaries[taskID] = TaskSummary{
				TaskID:       taskID,
				TaskName:     result.TaskName,
				TaskCategory: result.TaskCategory,
			}
		}

		taskSummary := summary.TaskSummaries[taskID]
		taskSummary.TotalCount++
		taskSummary.TotalCost += result.TotalCost
		taskSummary.TotalTokens += result.TotalTokens
		if result.Success {
			taskSummary.SuccessCount++
		}
		summary.TaskSummaries[taskID] = taskSummary

		// Category summary
		if _, exists := summary.CategorySummaries[category]; !exists {
			summary.CategorySummaries[category] = CategorySummary{
				Category: category,
			}
		}

		catSummary := summary.CategorySummaries[category]
		catSummary.TotalCount++
		if result.Success {
			catSummary.SuccessCount++
		}
		summary.CategorySummaries[category] = catSummary
	}

	// Calculate derived metrics
	for modelID, modelSummary := range summary.ModelSummaries {
		if modelSummary.TotalCount > 0 {
			modelSummary.SuccessRate = float64(modelSummary.SuccessCount) / float64(modelSummary.TotalCount)
		}
		summary.ModelSummaries[modelID] = modelSummary
	}

	for taskID, taskSummary := range summary.TaskSummaries {
		if taskSummary.TotalCount > 0 {
			taskSummary.SuccessRate = float64(taskSummary.SuccessCount) / float64(taskSummary.TotalCount)
			taskSummary.AvgDuration = time.Duration(int64(summary.TotalDuration) / int64(taskSummary.TotalCount))
		}
		summary.TaskSummaries[taskID] = taskSummary
	}

	for category, catSummary := range summary.CategorySummaries {
		if catSummary.TotalCount > 0 {
			catSummary.SuccessRate = float64(catSummary.SuccessCount) / float64(catSummary.TotalCount)
		}
		summary.CategorySummaries[category] = catSummary
	}

	// Calculate overall metrics
	totalCount := len(results)
	totalSuccess := 0
	summary.TotalDuration = 0

	for _, result := range results {
		summary.TotalCost += result.TotalCost
		summary.TotalTokens += result.TotalTokens
		summary.TotalDuration += result.Duration
		if result.Success {
			totalSuccess++
		}
	}

	if totalCount > 0 {
		summary.SuccessRate = float64(totalSuccess) / float64(totalCount)
		summary.TotalDuration = time.Duration(int64(summary.TotalDuration) / int64(totalCount))
	}

	// Generate rankings
	summary.ModelRankings = generateModelRankings(summary.ModelSummaries)
	summary.CategoryRankings = generateCategoryRankings(summary.CategorySummaries, summary.ModelSummaries)

	return summary
}

// generateModelRankings ranks models by overall performance
func generateModelRankings(modelSummaries map[string]ModelSummary) []ModelRanking {
	var rankings []ModelRanking

	for modelID, summary := range modelSummaries {
		// Score combines success rate and cost efficiency
		var score float64
		if summary.TotalCost > 0 {
			// Higher success rate is better, lower cost is better
			score = summary.SuccessRate * 100.0 / (1 + math.Log(summary.TotalCost))
		} else {
			score = summary.SuccessRate * 100.0
		}

		costPerSuccess := 0.0
		if summary.SuccessCount > 0 {
			costPerSuccess = summary.TotalCost / float64(summary.SuccessCount)
		}

		rankings = append(rankings, ModelRanking{
			ModelID:        modelID,
			ModelName:      summary.ModelName,
			Score:          score,
			SuccessRate:    summary.SuccessRate,
			TotalCost:      summary.TotalCost,
			CostPerSuccess: costPerSuccess,
		})
	}

	// Sort by score (descending)
	sort.Slice(rankings, func(i, j int) bool {
		if rankings[i].Score == rankings[j].Score {
			// Tie-breaker: lower cost is better
			return rankings[i].TotalCost < rankings[j].TotalCost
		}
		return rankings[i].Score > rankings[j].Score
	})

	// Assign ranks
	for i := range rankings {
		rankings[i].Rank = i + 1
	}

	return rankings
}

// generateCategoryRankings creates category-level rankings
func generateCategoryRankings(categorySummaries map[string]CategorySummary, modelSummaries map[string]ModelSummary) []CategoryRanking {
	var rankings []CategoryRanking

	for category, catSummary := range categorySummaries {
		// Find best model for this category
		var bestModel string
		var bestSuccessRate float64

		for modelID, modelSummary := range modelSummaries {
			// Check if this model has results in this category
			// This is simplified - in a full implementation, we'd track per-category model performance
			if modelSummary.SuccessRate > bestSuccessRate {
				bestSuccessRate = modelSummary.SuccessRate
				bestModel = modelID
			}
		}

		rankings = append(rankings, CategoryRanking{
			Category:        category,
			BestModel:       bestModel,
			BestSuccessRate: bestSuccessRate,
			AvgSuccessRate:  catSummary.SuccessRate,
		})
	}

	// Sort by average success rate (descending)
	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].AvgSuccessRate > rankings[j].AvgSuccessRate
	})

	return rankings
}

// PrintTaskSummary prints a human-readable summary of task evaluation results
func PrintTaskSummary(summary EvalSummary) {
	fmt.Print("\n" + color.CyanString("=== Task Evaluation Summary ===\n"))
	fmt.Printf("Evaluated at: %s\n", summary.EvalTime.Format(time.RFC3339))
	fmt.Printf("Total Cost: $%.6f %s\n", summary.TotalCost, "USD")
	fmt.Printf("Total Tokens: %d\n", summary.TotalTokens)
	fmt.Printf("Overall Success Rate: %.1f%%\n", summary.SuccessRate*100)
	fmt.Printf("Average Duration: %v\n", summary.TotalDuration)

	fmt.Print("\n" + color.GreenString("=== Model Performance ===\n"))
	fmt.Printf("%-30s %8s %12s %12s %8s\n", "Model", "Tests", "Success%", "Cost ($)", "Tokens")
	fmt.Printf("%s\n", strings.Repeat("-", 80))

	// Print in ranking order
	for _, ranking := range summary.ModelRankings {
		modelSummary := summary.ModelSummaries[ranking.ModelID]
		fmt.Printf("%-30s %8d %11.1f%% $%11.6f %8d\n",
			truncateString(modelSummary.ModelName, 30),
			modelSummary.TotalCount,
			modelSummary.SuccessRate*100,
			modelSummary.TotalCost,
			modelSummary.TotalTokens)
	}

	if len(summary.TaskSummaries) > 0 {
		fmt.Print("\n" + color.GreenString("=== Task Performance ===\n"))
		fmt.Printf("%-20s %12s %12s %8s\n", "Task", "Success%", "Cost ($)", "Duration")
		fmt.Printf("%s\n", strings.Repeat("-", 60))

		taskIDs := make([]string, 0, len(summary.TaskSummaries))
		for taskID := range summary.TaskSummaries {
			taskIDs = append(taskIDs, taskID)
		}
		sort.Strings(taskIDs)

		for _, taskID := range taskIDs {
			task := summary.TaskSummaries[taskID]
			fmt.Printf("%-20s %11.1f%% $%11.6f %8v\n",
				truncateString(task.TaskName, 20),
				task.SuccessRate*100,
				task.TotalCost,
				task.AvgDuration)
		}
	}

	fmt.Print("\n" + color.GreenString("=== Category Performance ===\n"))
	for category, catSummary := range summary.CategorySummaries {
		fmt.Printf("%-15s: %d/%d (%.1f%%)\n",
			category,
			catSummary.SuccessCount,
			catSummary.TotalCount,
			catSummary.SuccessRate*100)
	}

	fmt.Print("\n" + color.GreenString("=== Top Ranked Models ===\n"))
	for i, ranking := range summary.ModelRankings {
		if i >= 5 { // Top 5
			break
		}
		fmt.Printf("%d. %s (Score: %.1f)\n",
			ranking.Rank,
			truncateString(ranking.ModelName, 40),
			ranking.Score)
	}

	fmt.Print("\n" + color.CyanString("=== End Summary ===\n\n"))
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// SaveTaskSummary saves the task evaluation summary to a JSON file
func SaveTaskSummary(summary EvalSummary, filepath string) error {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal summary: %w", err)
	}

	return os.WriteFile(filepath, data, 0644)
}
