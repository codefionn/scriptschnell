package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/search"
)

// MockSearchProvider is a mock search provider for testing
type MockSearchProvider struct {
	response *search.SearchResponse
	err      error
	name     string
	isValid  bool
}

func (m *MockSearchProvider) Search(ctx context.Context, query string, numResults int) (*search.SearchResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.response == nil {
		return &search.SearchResponse{
			Query:   query,
			Results: []search.SearchResult{},
		}, nil
	}
	return m.response, nil
}

func (m *MockSearchProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func (m *MockSearchProvider) Validate() error {
	if !m.isValid {
		return fmt.Errorf("provider not configured")
	}
	return nil
}

func TestWebSearchToolSpec_Name(t *testing.T) {
	spec := &WebSearchToolSpec{}
	if spec.Name() != ToolNameWebSearch {
		t.Errorf("expected name %s, got %s", ToolNameWebSearch, spec.Name())
	}
}

func TestWebSearchToolSpec_Description(t *testing.T) {
	spec := &WebSearchToolSpec{}
	desc := spec.Description()
	if !strings.Contains(desc, "Search the web") {
		t.Error("description should mention web search")
	}
	if !strings.Contains(desc, "multiple queries") {
		t.Error("description should mention multiple queries")
	}
}

func TestWebSearchToolSpec_Parameters(t *testing.T) {
	spec := &WebSearchToolSpec{}
	params := spec.Parameters()

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in parameters")
	}

	if _, ok := props["queries"]; !ok {
		t.Error("expected 'queries' in properties")
	}

	// Check that queries property is an array with string items
	queriesProp, ok := props["queries"].(map[string]interface{})
	if !ok {
		t.Fatal("expected queries to be an object with type and items")
	}

	if queriesProp["type"] != "array" {
		t.Errorf("expected queries type to be 'array', got %v", queriesProp["type"])
	}

	items, ok := queriesProp["items"].(map[string]interface{})
	if !ok {
		t.Fatal("expected items in queries property")
	}

	if items["type"] != "string" {
		t.Errorf("expected items type to be 'string', got %v", items["type"])
	}

	if _, ok := props["num_results"]; !ok {
		t.Error("expected 'num_results' in properties")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}

	if len(required) != 1 || required[0] != "queries" {
		t.Errorf("expected only 'queries' in required, got %v", required)
	}
}

func TestWebSearchTool_Name(t *testing.T) {
	cfg := &config.Config{}
	tool := NewWebSearchTool(cfg)

	if tool.Name() != ToolNameWebSearch {
		t.Errorf("expected name %s, got %s", ToolNameWebSearch, tool.Name())
	}
}

func TestWebSearchTool_NoProviderConfigured(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "", // No provider
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"test query"},
	})

	if result.Error == "" {
		t.Fatal("expected error when no provider configured")
	}

	if !strings.Contains(result.Error, "no search provider configured") {
		t.Errorf("expected no provider error, got: %s", result.Error)
	}
}

func TestWebSearchTool_MissingQueries(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{})

	if result.Error == "" {
		t.Fatal("expected error when queries is missing")
	}

	if !strings.Contains(result.Error, "missing required parameter 'queries'") {
		t.Errorf("expected queries missing error, got: %s", result.Error)
	}
}

func TestWebSearchTool_EmptyQueries(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{},
	})

	if result.Error == "" {
		t.Fatal("expected error when queries array is empty")
	}

	if !strings.Contains(result.Error, "queries array cannot be empty") {
		t.Errorf("expected empty queries error, got: %s", result.Error)
	}
}

func TestWebSearchTool_DefaultNumResults(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	// The tool will try to validate the provider, which will fail with a real provider
	// but we're just testing parameter parsing here
	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"test query"},
	})

	// Should get a validation error, not a parameter error
	if result.Error == "" {
		t.Fatal("expected error")
	}

	// Should not be a num_results error
	if strings.Contains(result.Error, "num_results") {
		t.Error("should use default num_results, not error")
	}
}

func TestWebSearchTool_CustomNumResults(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	tests := []struct {
		name       string
		numResults interface{}
		shouldFail bool
	}{
		{
			name:       "float64 format",
			numResults: float64(5),
			shouldFail: false,
		},
		{
			name:       "int format",
			numResults: 5,
			shouldFail: false,
		},
		{
			name:       "invalid string",
			numResults: "not a number",
			shouldFail: true,
		},
		{
			name:       "zero uses default",
			numResults: 0,
			shouldFail: false,
		},
		{
			name:       "negative uses default",
			numResults: -5,
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), map[string]interface{}{
				"queries":     []string{"test"},
				"num_results": tt.numResults,
			})

			if tt.shouldFail {
				if result.Error == "" {
					t.Fatal("expected error")
				}
				if !strings.Contains(result.Error, "num_results must be an integer") {
					t.Errorf("expected num_results error, got: %s", result.Error)
				}
			}
		})
	}
}

func TestWebSearchTool_UnsupportedProvider(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "unknown_provider",
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"test query"},
	})

	if result.Error == "" {
		t.Fatal("expected error for unsupported provider")
	}

	if !strings.Contains(result.Error, "unsupported search provider") {
		t.Errorf("expected unsupported provider error, got: %s", result.Error)
	}
}

func TestFormatSearchResults_NoResults(t *testing.T) {
	response := &search.SearchResponse{
		Query:   "test query",
		Results: []search.SearchResult{},
	}

	formatted := formatSearchResults(response)

	if !strings.Contains(formatted, "No results found") {
		t.Error("expected 'No results found' message")
	}

	if !strings.Contains(formatted, "test query") {
		t.Error("expected query in output")
	}
}

func TestFormatSearchResults_SingleResult(t *testing.T) {
	response := &search.SearchResponse{
		Query: "test query",
		Results: []search.SearchResult{
			{
				Title:   "Test Result",
				URL:     "https://example.com",
				Snippet: "This is a test snippet",
			},
		},
	}

	formatted := formatSearchResults(response)

	if !strings.Contains(formatted, "Test Result") {
		t.Error("expected title in output")
	}

	if !strings.Contains(formatted, "https://example.com") {
		t.Error("expected URL in output")
	}

	if !strings.Contains(formatted, "This is a test snippet") {
		t.Error("expected snippet in output")
	}

	if !strings.Contains(formatted, "1 result(s)") {
		t.Error("expected result count")
	}
}

func TestFormatSearchResults_MultipleResults(t *testing.T) {
	response := &search.SearchResponse{
		Query: "test query",
		Results: []search.SearchResult{
			{
				Title:   "Result 1",
				URL:     "https://example1.com",
				Snippet: "Snippet 1",
			},
			{
				Title:   "Result 2",
				URL:     "https://example2.com",
				Snippet: "Snippet 2",
			},
			{
				Title:   "Result 3",
				URL:     "https://example3.com",
				Snippet: "Snippet 3",
			},
		},
	}

	formatted := formatSearchResults(response)

	if !strings.Contains(formatted, "3 result(s)") {
		t.Error("expected count of 3 results")
	}

	// Check all results are present
	for i := 1; i <= 3; i++ {
		expectedTitle := fmt.Sprintf("Result %d", i)
		if !strings.Contains(formatted, expectedTitle) {
			t.Errorf("expected %s in output", expectedTitle)
		}
	}

	// Check numbering
	if !strings.Contains(formatted, "1. Result 1") {
		t.Error("expected numbered list starting with 1")
	}
}

func TestFormatSearchResults_WithContent(t *testing.T) {
	shortContent := "Short content here"
	longContent := strings.Repeat("x", 600)

	tests := []struct {
		name            string
		content         string
		expectPreview   bool
		expectFullLabel bool
	}{
		{
			name:            "short content",
			content:         shortContent,
			expectPreview:   false,
			expectFullLabel: true,
		},
		{
			name:          "long content",
			content:       longContent,
			expectPreview: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := &search.SearchResponse{
				Query: "test",
				Results: []search.SearchResult{
					{
						Title:   "Test",
						URL:     "https://example.com",
						Content: tt.content,
					},
				},
			}

			formatted := formatSearchResults(response)

			if tt.expectPreview {
				if !strings.Contains(formatted, "Preview:") {
					t.Error("expected 'Preview:' label for long content")
				}
				if strings.Contains(formatted, tt.content) {
					t.Error("should not contain full long content")
				}
			}

			if tt.expectFullLabel {
				if !strings.Contains(formatted, "Content:") {
					t.Error("expected 'Content:' label for short content")
				}
				if !strings.Contains(formatted, tt.content) {
					t.Error("expected full short content")
				}
			}
		})
	}
}

func TestFormatSearchResults_MissingFields(t *testing.T) {
	tests := []struct {
		name   string
		result search.SearchResult
	}{
		{
			name: "no URL",
			result: search.SearchResult{
				Title:   "Test Result",
				Snippet: "Test snippet",
			},
		},
		{
			name: "no snippet",
			result: search.SearchResult{
				Title: "Test Result",
				URL:   "https://example.com",
			},
		},
		{
			name: "only title",
			result: search.SearchResult{
				Title: "Test Result",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := &search.SearchResponse{
				Query:   "test",
				Results: []search.SearchResult{tt.result},
			}

			formatted := formatSearchResults(response)

			// Should always contain title
			if !strings.Contains(formatted, tt.result.Title) {
				t.Error("expected title in output")
			}

			// Should not panic or error
			if formatted == "" {
				t.Error("expected non-empty formatted output")
			}
		})
	}
}

func TestFormatSearchResults_Formatting(t *testing.T) {
	response := &search.SearchResponse{
		Query: "golang testing",
		Results: []search.SearchResult{
			{
				Title:   "Go Testing Guide",
				URL:     "https://go.dev/doc/tutorial/testing",
				Snippet: "Learn how to write tests in Go",
			},
		},
	}

	formatted := formatSearchResults(response)

	// Check for proper structure
	if !strings.Contains(formatted, "Search results for: golang testing") {
		t.Error("expected search query header")
	}

	if !strings.Contains(formatted, "1. Go Testing Guide") {
		t.Error("expected numbered result")
	}

	if !strings.Contains(formatted, "URL: https://go.dev/doc/tutorial/testing") {
		t.Error("expected URL label")
	}

	// Check indentation (3 spaces for URL and snippet)
	if !strings.Contains(formatted, "   URL:") {
		t.Error("expected indented URL")
	}

	if !strings.Contains(formatted, "   Learn how to write tests in Go") {
		t.Error("expected indented snippet")
	}
}

func TestNewWebSearchToolFactory(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}

	factory := NewWebSearchToolFactory(cfg)
	registry := NewRegistry(nil)

	executor := factory(registry)
	if executor == nil {
		t.Fatal("expected executor from factory")
	}

	// Verify it's the right type
	tool, ok := executor.(*WebSearchTool)
	if !ok {
		t.Fatalf("expected *WebSearchTool, got %T", executor)
	}

	if tool.Name() != ToolNameWebSearch {
		t.Errorf("expected name %s, got %s", ToolNameWebSearch, tool.Name())
	}
}

func TestWebSearchTool_ProviderValidationExaConfig(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "", // Empty API key should fail validation
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"test query"},
	})

	if result.Error == "" {
		t.Fatal("expected error for invalid provider configuration")
	}

	if !strings.Contains(result.Error, "validation failed") {
		t.Errorf("expected validation error, got: %s", result.Error)
	}
}

func TestWebSearchTool_ProviderValidationGooglePSEConfig(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "google_pse",
			GooglePSE: config.GooglePSEConfig{
				APIKey: "", // Empty should fail
				CX:     "",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"test query"},
	})

	if result.Error == "" {
		t.Fatal("expected error for invalid provider configuration")
	}

	if !strings.Contains(result.Error, "validation failed") {
		t.Errorf("expected validation error, got: %s", result.Error)
	}
}

func TestWebSearchTool_ProviderValidationPerplexityConfig(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "perplexity",
			Perplexity: config.PerplexityConfig{
				APIKey: "", // Empty should fail
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"test query"},
	})

	if result.Error == "" {
		t.Fatal("expected error for invalid provider configuration")
	}

	if !strings.Contains(result.Error, "validation failed") {
		t.Errorf("expected validation error, got: %s", result.Error)
	}
}

func TestFormatSearchResults_SpecialCharacters(t *testing.T) {
	response := &search.SearchResponse{
		Query: "test & <special> characters",
		Results: []search.SearchResult{
			{
				Title:   "Result with & <tags>",
				URL:     "https://example.com?param=value&other=123",
				Snippet: "Snippet with \"quotes\" and 'apostrophes'",
			},
		},
	}

	formatted := formatSearchResults(response)

	// Should preserve special characters
	if !strings.Contains(formatted, "&") {
		t.Error("expected ampersands to be preserved")
	}

	if !strings.Contains(formatted, "\"quotes\"") {
		t.Error("expected quotes to be preserved")
	}

	if !strings.Contains(formatted, "'apostrophes'") {
		t.Error("expected apostrophes to be preserved")
	}
}

func TestWebSearchTool_MultipleQueries(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"first query", "second query", "third query"},
	})

	// Should get a validation error (since we're using a mock config),
	// but not a parameter parsing error
	if result.Error == "" {
		t.Fatal("expected error")
	}

	// Should not be a queries parameter error
	if strings.Contains(result.Error, "queries") {
		t.Error("should parse queries parameter correctly, not error")
	}
}

func TestWebSearchTool_InvalidQueriesTypes(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	tests := []struct {
		name      string
		queries   interface{}
		expectErr string
	}{
		{
			name:      "string instead of array",
			queries:   "not an array",
			expectErr: "queries must be an array of strings",
		},
		{
			name:      "array with non-string items",
			queries:   []interface{}{"valid", 123, "another valid"},
			expectErr: "all queries must be strings",
		},
		{
			name:      "nil",
			queries:   nil,
			expectErr: "missing required parameter 'queries'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), map[string]interface{}{
				"queries": tt.queries,
			})

			if result.Error == "" {
				t.Fatal("expected error")
			}

			if !strings.Contains(result.Error, tt.expectErr) {
				t.Errorf("expected error '%s', got: %s", tt.expectErr, result.Error)
			}
		})
	}
}

func TestWebSearchTool_SingleQuery(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []string{"single query"},
	})

	// Should get a validation error (since we're using a mock config),
	// but not a parameter parsing error
	if result.Error == "" {
		t.Fatal("expected error")
	}

	// Should not be a queries parameter error
	if strings.Contains(result.Error, "queries") {
		t.Error("should parse single query in array correctly")
	}
}

func TestWebSearchTool_QueriesAsInterfaceArray(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Provider: "exa",
			Exa: config.ExaConfig{
				APIKey: "test-key",
			},
		},
	}
	tool := NewWebSearchTool(cfg)

	// Test with []interface{} instead of []string
	result := tool.Execute(context.Background(), map[string]interface{}{
		"queries": []interface{}{"query 1", "query 2"},
	})

	// Should get a validation error (since we're using a mock config),
	// but not a parameter parsing error
	if result.Error == "" {
		t.Fatal("expected error")
	}

	// Should not be a queries parameter error
	if strings.Contains(result.Error, "queries") {
		t.Error("should parse []interface{} queries correctly")
	}
}
