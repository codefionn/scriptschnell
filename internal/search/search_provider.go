package search

import "context"

// SearchResult represents a single search result
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Content string `json:"content,omitempty"` // Full content if available
}

// SearchResponse represents the response from a search provider
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Query   string         `json:"query"`
}

// SearchProvider defines the interface for web search providers
type SearchProvider interface {
	// Search performs a web search with the given query
	Search(ctx context.Context, query string, numResults int) (*SearchResponse, error)

	// Name returns the name of the search provider
	Name() string

	// Validate checks if the provider is properly configured
	Validate() error
}
