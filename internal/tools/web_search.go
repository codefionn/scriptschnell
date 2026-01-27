package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/safety"
	"github.com/codefionn/scriptschnell/internal/search"
)

// WebSearchToolSpec is the static specification for the web_search tool
type WebSearchToolSpec struct{}

func (s *WebSearchToolSpec) Name() string {
	return ToolNameWebSearch
}

func (s *WebSearchToolSpec) Description() string {
	return "Search the web using the configured search provider (Exa, Google PSE, or Perplexity). Accepts multiple queries and returns titles, URLs, and snippets from search results for each query."
}

func (s *WebSearchToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"queries": map[string]interface{}{
				"type":        "array",
				"description": "Array of search queries to execute",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"num_results": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results to return per query (default: 10, max varies by provider)",
				"default":     10,
			},
		},
		"required": []string{"queries"},
	}
}

// WebSearchTool is the executor with runtime dependencies
type WebSearchTool struct {
	cfg *config.Config
}

// NewWebSearchTool creates a new web search tool
func NewWebSearchTool(cfg *config.Config) *WebSearchTool {
	return &WebSearchTool{cfg: cfg}
}

// Legacy interface implementation for backward compatibility
func (t *WebSearchTool) Name() string        { return ToolNameWebSearch }
func (t *WebSearchTool) Description() string { return (&WebSearchToolSpec{}).Description() }
func (t *WebSearchTool) Parameters() map[string]interface{} {
	return (&WebSearchToolSpec{}).Parameters()
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	// Check if search is configured
	if t.cfg.Search.Provider == "" {
		return &ToolResult{Error: "no search provider configured - please configure a search provider in settings"}
	}

	// Get queries parameter
	queriesInterface, ok := params["queries"]
	if !ok {
		return &ToolResult{Error: "missing required parameter 'queries'"}
	}

	if queriesInterface == nil {
		return &ToolResult{Error: "missing required parameter 'queries'"}
	}

	// Convert to slice of strings
	var queries []string
	switch v := queriesInterface.(type) {
	case []interface{}:
		queries = make([]string, len(v))
		for i, item := range v {
			if str, ok := item.(string); ok {
				queries[i] = str
			} else {
				return &ToolResult{Error: "all queries must be strings"}
			}
		}
	case []string:
		queries = v
	default:
		return &ToolResult{Error: "queries must be an array of strings"}
	}

	if len(queries) == 0 {
		return &ToolResult{Error: "queries array cannot be empty"}
	}

	// Get num_results parameter (optional, default 10)
	numResults := 10
	if numResultsParam, ok := params["num_results"]; ok {
		switch v := numResultsParam.(type) {
		case float64:
			numResults = int(v)
		case int:
			numResults = v
		default:
			return &ToolResult{Error: "num_results must be an integer"}
		}
	}

	if numResults <= 0 {
		numResults = 10
	}

	// Create the appropriate search provider
	var provider search.SearchProvider
	switch t.cfg.Search.Provider {
	case "exa":
		provider = search.NewExaSearchProvider(t.cfg.Search.Exa)
	case "google_pse":
		provider = search.NewGooglePSEProvider(t.cfg.Search.GooglePSE)
	case "perplexity":
		provider = search.NewPerplexitySearchProvider(t.cfg.Search.Perplexity)
	default:
		return &ToolResult{Error: fmt.Sprintf("unsupported search provider: %s", t.cfg.Search.Provider)}
	}

	// Validate provider configuration
	if err := provider.Validate(); err != nil {
		return &ToolResult{Error: fmt.Sprintf("search provider validation failed: %v", err)}
	}

	// Perform multiple searches and collect results
	var allResults strings.Builder
	var allSearchResults []map[string]interface{}
	for i, query := range queries {
		response, err := provider.Search(ctx, query, numResults)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("search failed for query '%s': %v", query, err)}
		}

		// Collect results for safety evaluation
		for _, result := range response.Results {
			allSearchResults = append(allSearchResults, map[string]interface{}{
				"title":   result.Title,
				"snippet": result.Snippet,
				"url":     result.URL,
			})
		}

		// Format results for this query
		if i > 0 {
			allResults.WriteString("\n" + strings.Repeat("-", 80) + "\n\n")
		}
		allResults.WriteString(formatSearchResults(response))
	}

	// Safety evaluation of search results
	if len(allSearchResults) > 0 {
		// Create a simple safety evaluator using the search provider's configuration
		// Since we don't have direct access to an LLM client here, we'll create a basic evaluator
		// This is a placeholder - in a real implementation, you'd pass the LLM client through
		safetyEvaluator := safety.NewEvaluator(nil) // Will default to safe
		safetyResult, err := safetyEvaluator.EvaluateSearchResults(ctx, allSearchResults)
		if err != nil {
			logger.Warn("Failed to evaluate search results safety: %v", err)
		} else if !safetyResult.IsSafe {
			return &ToolResult{
				Error: fmt.Sprintf("Search results safety evaluation failed: %s (risk level: %s, category: %s)",
					safetyResult.Reason, safetyResult.RiskLevel, safetyResult.Category),
			}
		}
	}

	// Return all formatted results
	return &ToolResult{Result: allResults.String()}
}

func formatSearchResults(response *search.SearchResponse) string {
	if len(response.Results) == 0 {
		return fmt.Sprintf("No results found for query: %s", response.Query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s\n\n", response.Query))
	sb.WriteString(fmt.Sprintf("Found %d result(s):\n\n", len(response.Results)))

	for i, result := range response.Results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, result.Title))
		if result.URL != "" {
			sb.WriteString(fmt.Sprintf("   URL: %s\n", result.URL))
		}
		if result.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", result.Snippet))
		}
		if result.Content != "" && len(result.Content) > 500 {
			// Include a preview of full content if available
			sb.WriteString(fmt.Sprintf("   Preview: %s...\n", result.Content[:500]))
		} else if result.Content != "" {
			sb.WriteString(fmt.Sprintf("   Content: %s\n", result.Content))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// NewWebSearchToolFactory creates a factory for WebSearchTool
func NewWebSearchToolFactory(cfg *config.Config) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWebSearchTool(cfg)
	}
}
