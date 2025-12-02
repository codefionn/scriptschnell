package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/codefionn/scriptschnell/internal/config"
)

// ExaSearchProvider implements SearchProvider for Exa AI Search API
type ExaSearchProvider struct {
	apiKey string
	client *http.Client
}

// NewExaSearchProvider creates a new Exa search provider
func NewExaSearchProvider(cfg config.ExaConfig) *ExaSearchProvider {
	return &ExaSearchProvider{
		apiKey: cfg.APIKey,
		client: &http.Client{},
	}
}

// exaSearchRequest represents the request body for Exa search API
type exaSearchRequest struct {
	Query         string             `json:"query"`
	NumResults    int                `json:"numResults,omitempty"`
	UseAutoprompt bool               `json:"useAutoprompt,omitempty"`
	Contents      exaContentsOptions `json:"contents,omitempty"`
}

type exaContentsOptions struct {
	Text bool `json:"text,omitempty"`
}

// exaSearchResponse represents the response from Exa search API
type exaSearchResponse struct {
	Results []exaResult `json:"results"`
}

type exaResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Text    string `json:"text,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Search performs a web search using Exa API
func (e *ExaSearchProvider) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	if numResults <= 0 {
		numResults = 10
	}

	reqBody := exaSearchRequest{
		Query:         query,
		NumResults:    numResults,
		UseAutoprompt: true,
		Contents: exaContentsOptions{
			Text: true,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.exa.ai/search", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("exa API error (status %d): %s", resp.StatusCode, string(body))
	}

	var exaResp exaSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&exaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert Exa results to SearchResults
	results := make([]SearchResult, len(exaResp.Results))
	for i, r := range exaResp.Results {
		snippet := r.Snippet
		if snippet == "" && len(r.Text) > 200 {
			snippet = r.Text[:200] + "..."
		}

		results[i] = SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: snippet,
			Content: r.Text,
		}
	}

	return &SearchResponse{
		Results: results,
		Query:   query,
	}, nil
}

// Name returns the provider name
func (e *ExaSearchProvider) Name() string {
	return "exa"
}

// Validate checks if the provider is properly configured
func (e *ExaSearchProvider) Validate() error {
	if e.apiKey == "" {
		return fmt.Errorf("exa API key is not configured")
	}
	return nil
}
