package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/statcode-ai/scriptschnell/internal/config"
)

// PerplexitySearchProvider implements SearchProvider for Perplexity Search API
type PerplexitySearchProvider struct {
	apiKey string
	client *http.Client
}

// NewPerplexitySearchProvider creates a new Perplexity search provider
func NewPerplexitySearchProvider(cfg config.PerplexityConfig) *PerplexitySearchProvider {
	return &PerplexitySearchProvider{
		apiKey: cfg.APIKey,
		client: &http.Client{},
	}
}

// perplexitySearchRequest represents the request for Perplexity Search API
type perplexitySearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// perplexitySearchResponse represents the response from Perplexity Search API
type perplexitySearchResponse struct {
	Results []perplexityResult `json:"results"`
}

type perplexityResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	Date        string `json:"date,omitempty"`
	LastUpdated string `json:"last_updated,omitempty"`
}

// Search performs a web search using Perplexity Search API
func (p *PerplexitySearchProvider) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	if numResults <= 0 {
		numResults = 10
	}
	if numResults > 20 {
		numResults = 20 // Perplexity API max
	}

	reqBody := perplexitySearchRequest{
		Query:      query,
		MaxResults: numResults,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.perplexity.ai/search", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("perplexity API error (status %d): %s", resp.StatusCode, string(body))
	}

	var pplxResp perplexitySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&pplxResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert Perplexity results to SearchResults
	results := make([]SearchResult, len(pplxResp.Results))
	for i, r := range pplxResp.Results {
		results[i] = SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Snippet,
			Content: "", // Perplexity doesn't return full content by default
		}
	}

	return &SearchResponse{
		Results: results,
		Query:   query,
	}, nil
}

// Name returns the provider name
func (p *PerplexitySearchProvider) Name() string {
	return "perplexity"
}

// Validate checks if the provider is properly configured
func (p *PerplexitySearchProvider) Validate() error {
	if p.apiKey == "" {
		return fmt.Errorf("perplexity API key is not configured")
	}
	return nil
}
