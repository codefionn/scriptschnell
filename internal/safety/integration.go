package safety

import (
	"context"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// EvaluateUserPromptSafety evaluates a user prompt for safety and returns an error if unsafe
func EvaluateUserPromptSafety(ctx context.Context, client llm.Client, prompt string) error {
	if client == nil {
		logger.Debug("No safety model configured, allowing prompt by default")
		return nil
	}

	evaluator := NewEvaluator(client)
	result, err := evaluator.EvaluateUserPrompt(ctx, prompt)
	if err != nil {
		return fmt.Errorf("safety evaluation failed: %w", err)
	}

	if !result.IsSafe {
		return fmt.Errorf("prompt safety evaluation failed: %s (risk level: %s, category: %s)",
			result.Reason, result.RiskLevel, result.Category)
	}

	return nil
}

// EvaluateWebContentSafety evaluates web content for safety and returns an error if unsafe
func EvaluateWebContentSafety(ctx context.Context, client llm.Client, content, url string) error {
	if client == nil {
		logger.Debug("No safety model configured, allowing web content by default")
		return nil
	}

	evaluator := NewEvaluator(client)
	result, err := evaluator.EvaluateWebContent(ctx, content, url)
	if err != nil {
		return fmt.Errorf("web content safety evaluation failed: %w", err)
	}

	if !result.IsSafe {
		return fmt.Errorf("web content safety evaluation failed: %s (risk level: %s, category: %s)",
			result.Reason, result.RiskLevel, result.Category)
	}

	return nil
}

// EvaluateSearchResultsSafety evaluates search results for safety and returns an error if unsafe
func EvaluateSearchResultsSafety(ctx context.Context, client llm.Client, results []map[string]interface{}) error {
	if client == nil {
		logger.Debug("No safety model configured, allowing search results by default")
		return nil
	}

	evaluator := NewEvaluator(client)
	result, err := evaluator.EvaluateSearchResults(ctx, results)
	if err != nil {
		return fmt.Errorf("search results safety evaluation failed: %w", err)
	}

	if !result.IsSafe {
		return fmt.Errorf("search results safety evaluation failed: %s (risk level: %s, category: %s)",
			result.Reason, result.RiskLevel, result.Category)
	}

	return nil
}
