package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnthropicProvider implements the Provider interface for Anthropic
type AnthropicProvider struct {
	apiKey string
	client *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (p *AnthropicProvider) GetName() string {
	return "anthropic"
}

// Anthropic API response structures
type anthropicModelsResponse struct {
	Data    []anthropicModelData `json:"data"`
	HasMore bool                 `json:"has_more"`
	FirstID *string              `json:"first_id"`
	LastID  *string              `json:"last_id"`
}

type anthropicModelData struct {
	Type         string   `json:"type"`
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	CreatedAt    string   `json:"created_at"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func (p *AnthropicProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	// Use Anthropic's /v1/models API endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var modelsResp anthropicModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*ModelInfo, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		// Only include actual models (type: "model")
		if m.Type != "model" {
			continue
		}

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                getClaudeDisplayName(m.ID, m.DisplayName),
			Provider:            "anthropic",
			Description:         getClaudeDescription(m.ID),
			ContextWindow:       getClaudeContextWindow(m.ID),
			MaxOutputTokens:     getClaudeMaxOutputTokens(m.ID),
			SupportsToolCalling: true, // All Claude models support tool calling
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			CreatedAt:           m.CreatedAt,
			Capabilities:        m.Capabilities,
		}

		models = append(models, info)
	}

	// If API returns no models, fall back to hardcoded list
	if len(models) == 0 {
		return p.getFallbackModels(), nil
	}

	return models, nil
}

func (p *AnthropicProvider) getFallbackModels() []*ModelInfo {
	// Fallback hardcoded list in case API fails or returns nothing
	return []*ModelInfo{
		// Claude 4 Series
		{
			ID:                  "claude-4-5-sonnet-20250514",
			Name:                "Claude 4.5 Sonnet",
			Provider:            "anthropic",
			Description:         "Latest Claude model with extended context",
			ContextWindow:       1000000,
			MaxOutputTokens:     16384,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use", "extended-context"},
		},
		{
			ID:                  "claude-4-5-haiku-20250514",
			Name:                "Claude 4.5 Haiku",
			Provider:            "anthropic",
			Description:         "Fast Claude 4 model",
			ContextWindow:       200000,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
		{
			ID:                  "claude-4-1-opus-20250514",
			Name:                "Claude 4.1 Opus",
			Provider:            "anthropic",
			Description:         "Most powerful Claude 4 model",
			ContextWindow:       200000,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
		// Claude 3 Series
		{
			ID:                  "claude-3-5-sonnet-20241022",
			Name:                "Claude 3.5 Sonnet (New)",
			Provider:            "anthropic",
			Description:         "Most intelligent Claude model with improved coding and analysis",
			ContextWindow:       200000,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use", "extended-thinking"},
		},
		{
			ID:                  "claude-3-5-sonnet-20240620",
			Name:                "Claude 3.5 Sonnet",
			Provider:            "anthropic",
			Description:         "Intelligent model for complex tasks",
			ContextWindow:       200000,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
		{
			ID:                  "claude-3-opus-20240229",
			Name:                "Claude 3 Opus",
			Provider:            "anthropic",
			Description:         "Most powerful model for highly complex tasks",
			ContextWindow:       200000,
			MaxOutputTokens:     4096,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
		{
			ID:                  "claude-3-sonnet-20240229",
			Name:                "Claude 3 Sonnet",
			Provider:            "anthropic",
			Description:         "Balanced model for scaled deployments",
			ContextWindow:       200000,
			MaxOutputTokens:     4096,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
		{
			ID:                  "claude-3-haiku-20240307",
			Name:                "Claude 3 Haiku",
			Provider:            "anthropic",
			Description:         "Fastest model for quick and accurate responses",
			ContextWindow:       200000,
			MaxOutputTokens:     4096,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
	}
}

// Helper functions for Claude model metadata

func getClaudeDisplayName(id, displayName string) string {
	if displayName != "" {
		return displayName
	}
	// Generate display name from ID
	if strings.Contains(id, "claude-4-5-sonnet") {
		return "Claude 4.5 Sonnet"
	}
	if strings.Contains(id, "claude-4-5-haiku") {
		return "Claude 4.5 Haiku"
	}
	if strings.Contains(id, "claude-4-1-opus") {
		return "Claude 4.1 Opus"
	}
	if strings.Contains(id, "claude-3-5-sonnet-20241022") {
		return "Claude 3.5 Sonnet (New)"
	}
	if strings.Contains(id, "claude-3-5-sonnet") {
		return "Claude 3.5 Sonnet"
	}
	if strings.Contains(id, "claude-3-opus") {
		return "Claude 3 Opus"
	}
	if strings.Contains(id, "claude-3-sonnet") {
		return "Claude 3 Sonnet"
	}
	if strings.Contains(id, "claude-3-haiku") {
		return "Claude 3 Haiku"
	}
	return id
}

func getClaudeDescription(id string) string {
	if strings.Contains(id, "claude-4-5-sonnet") {
		return "Latest Claude model with extended context"
	}
	if strings.Contains(id, "claude-4-5-haiku") {
		return "Fast Claude 4 model"
	}
	if strings.Contains(id, "claude-4-1-opus") {
		return "Most powerful Claude 4 model"
	}
	if strings.Contains(id, "claude-3-5-sonnet") {
		return "Intelligent model for complex tasks"
	}
	if strings.Contains(id, "claude-3-opus") {
		return "Most powerful model for highly complex tasks"
	}
	if strings.Contains(id, "claude-3-sonnet") {
		return "Balanced model for scaled deployments"
	}
	if strings.Contains(id, "claude-3-haiku") {
		return "Fastest model for quick and accurate responses"
	}
	return "Claude language model"
}

func getClaudeContextWindow(id string) int {
	// Claude 4 series
	if strings.Contains(id, "claude-4-5-sonnet") {
		return 1000000 // 1M context
	}
	if strings.Contains(id, "claude-4") {
		return 200000 // Claude 4.x default
	}
	// Claude 3 series all have 200K context
	if strings.Contains(id, "claude-3") {
		return 200000
	}
	// Claude 2 series
	if strings.Contains(id, "claude-2") {
		return 200000
	}
	return 200000 // Default
}

func getClaudeMaxOutputTokens(id string) int {
	// Claude 4 series
	if strings.Contains(id, "claude-4-5-sonnet") {
		return 16384 // 16K output for 4.5 Sonnet
	}
	if strings.Contains(id, "claude-4-5-haiku") || strings.Contains(id, "claude-4-1-opus") {
		return 8192
	}
	// Claude 3.5 Sonnet
	if strings.Contains(id, "claude-3-5-sonnet") || strings.Contains(id, "claude-3.5-sonnet") {
		return 8192
	}
	// Claude 3 series
	if strings.Contains(id, "claude-3") {
		return 4096
	}
	// Claude 2 series
	if strings.Contains(id, "claude-2") {
		return 4096
	}
	return 4096 // Default
}

func (p *AnthropicProvider) CreateClient(modelID string) (Client, error) {
	return NewAnthropicClient(p.apiKey, modelID)
}

func (p *AnthropicProvider) ValidateAPIKey(ctx context.Context) error {
	// Create a test client and make a minimal request
	client, err := p.CreateClient("claude-3-haiku-20240307")
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Try a minimal completion request
	_, err = client.Complete(ctx, "Hi")
	if err != nil {
		if strings.Contains(err.Error(), "authentication") ||
			strings.Contains(err.Error(), "unauthorized") ||
			strings.Contains(err.Error(), "invalid") {
			return fmt.Errorf("invalid API key")
		}
		// Other errors might be network-related, still return them
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}
