package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/search"
)

// WebSearchToolSpec is the static specification for the web_search tool
type WebSearchToolSpec struct{}

func (s *WebSearchToolSpec) Name() string {
	return ToolNameWebSearch
}

func (s *WebSearchToolSpec) Description() string {
	return "Search the web using the configured search provider (Exa, Google PSE, or Perplexity). Returns titles, URLs, and snippets from search results."
}

func (s *WebSearchToolSpec) Parameters() map[string]interface{} {
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

	// Get query parameter
	query := GetStringParam(params, "query", "")
	if query == "" {
		return &ToolResult{Error: "missing required parameter 'query'"}
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

	// Perform the search
	response, err := provider.Search(ctx, query, numResults)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("search failed: %v", err)}
	}

	// Format results for LLM consumption
	return &ToolResult{Result: formatSearchResults(response)}
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
