package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/search"
)

// WebSearchTool performs web searches using configured search providers
type WebSearchTool struct {
	cfg *config.Config
}

// NewWebSearchTool creates a new web search tool
func NewWebSearchTool(cfg *config.Config) *WebSearchTool {
	return &WebSearchTool{cfg: cfg}
}

func (t *WebSearchTool) Name() string {
	return ToolNameWebSearch
}

func (t *WebSearchTool) Description() string {
	return "Search the web using the configured search provider (Exa, Google PSE, or Perplexity). Returns titles, URLs, and snippets from search results."
}

func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query to execute",
			},
			"num_results": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results to return (default: 10, max varies by provider)",
				"default":     10,
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	// Check if search is configured
	if t.cfg.Search.Provider == "" {
		return "", fmt.Errorf("no search provider configured - please configure a search provider in settings")
	}

	// Get query parameter
	query := GetStringParam(params, "query", "")
	if query == "" {
		return "", fmt.Errorf("missing required parameter 'query'")
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
			return "", fmt.Errorf("num_results must be an integer")
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
		return "", fmt.Errorf("unsupported search provider: %s", t.cfg.Search.Provider)
	}

	// Validate provider configuration
	if err := provider.Validate(); err != nil {
		return "", fmt.Errorf("search provider validation failed: %w", err)
	}

	// Perform the search
	response, err := provider.Search(ctx, query, numResults)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	// Format results for LLM consumption
	return formatSearchResults(response), nil
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
