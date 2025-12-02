package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/statcode-ai/scriptschnell/internal/config"
)

// GooglePSEProvider implements SearchProvider for Google Programmable Search Engine
type GooglePSEProvider struct {
	apiKey string
	cx     string // Search Engine ID
	client *http.Client
}

// NewGooglePSEProvider creates a new Google PSE search provider
func NewGooglePSEProvider(cfg config.GooglePSEConfig) *GooglePSEProvider {
	return &GooglePSEProvider{
		apiKey: cfg.APIKey,
		cx:     cfg.CX,
		client: &http.Client{},
	}
}

// googlePSEResponse represents the response from Google Custom Search API
type googlePSEResponse struct {
	Items []googlePSEItem `json:"items"`
}

type googlePSEItem struct {
	Title   string            `json:"title"`
	Link    string            `json:"link"`
	Snippet string            `json:"snippet"`
	Pagemap *googlePSEPagemap `json:"pagemap,omitempty"`
}

type googlePSEPagemap struct {
	Metatags []map[string]string `json:"metatags,omitempty"`
}

// Search performs a web search using Google PSE API
func (g *GooglePSEProvider) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	if numResults <= 0 {
		numResults = 10
	}
	if numResults > 10 {
		numResults = 10 // Google PSE API max per request
	}

	// Build API URL
	apiURL := "https://www.googleapis.com/customsearch/v1"
	params := url.Values{}
	params.Set("key", g.apiKey)
	params.Set("cx", g.cx)
	params.Set("q", query)
	params.Set("num", fmt.Sprintf("%d", numResults))

	fullURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google PSE API error (status %d): %s", resp.StatusCode, string(body))
	}

	var googleResp googlePSEResponse
	if err := json.NewDecoder(resp.Body).Decode(&googleResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert Google PSE results to SearchResults
	results := make([]SearchResult, len(googleResp.Items))
	for i, item := range googleResp.Items {
		results[i] = SearchResult{
			Title:   item.Title,
			URL:     item.Link,
			Snippet: item.Snippet,
			Content: "", // Google PSE doesn't return full content by default
		}
	}

	return &SearchResponse{
		Results: results,
		Query:   query,
	}, nil
}

// Name returns the provider name
func (g *GooglePSEProvider) Name() string {
	return "google_pse"
}

// Validate checks if the provider is properly configured
func (g *GooglePSEProvider) Validate() error {
	if g.apiKey == "" {
		return fmt.Errorf("google PSE API key is not configured")
	}
	if g.cx == "" {
		return fmt.Errorf("google PSE CX (Search Engine ID) is not configured")
	}
	return nil
}
