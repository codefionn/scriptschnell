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
	// Use Anthropic's /v1/models API endpoint (beta to access latest models)
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/v1/models?beta=true", nil)
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

		family := DetectModelFamily(m.ID)
		contextWindow := DetectContextWindow(m.ID, family)

		// Use display name from API if available
		displayName := m.DisplayName
		if displayName == "" {
			displayName = FormatModelDisplayName(m.ID, family)
		}

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                displayName,
			Provider:            "anthropic",
			Description:         GetModelDescription(m.ID, family),
			ContextWindow:       contextWindow,
			MaxOutputTokens:     DetectMaxOutputTokens(m.ID, family, contextWindow),
			SupportsToolCalling: SupportsToolCalling(m.ID, family),
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
		// Claude 4/5 Series
		{
			ID:                  "claude-opus-4-5",
			Name:                "Claude Opus 4.5",
			Provider:            "anthropic",
			Description:         "Premium Claude model with top-tier reasoning",
			ContextWindow:       1000000,
			MaxOutputTokens:     64000,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use", "extended-context"},
		},
		{
			ID:                  "claude-sonnet-4-5",
			Name:                "Claude Sonnet 4.5",
			Provider:            "anthropic",
			Description:         "Balanced Claude 4.5 model",
			ContextWindow:       200000,
			MaxOutputTokens:     64000,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
		{
			ID:                  "claude-haiku-4-5",
			Name:                "Claude Haiku 4.5",
			Provider:            "anthropic",
			Description:         "Fast Claude 4.5 model",
			ContextWindow:       200000,
			MaxOutputTokens:     64000,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use"},
		},
		// Claude 3 Series
		{
			ID:                  "claude-3-7-sonnet-latest",
			Name:                "Claude 3.7 Sonnet",
			Provider:            "anthropic",
			Description:         "High-performance model with extended thinking",
			ContextWindow:       200000,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "anthropic",
			Capabilities:        []string{"vision", "tool-use", "extended-thinking"},
		},
		{
			ID:                  "claude-3-5-haiku-latest",
			Name:                "Claude 3.5 Haiku",
			Provider:            "anthropic",
			Description:         "Fast Claude 3.5 model",
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

func (p *AnthropicProvider) CreateClient(modelID string) (Client, error) {
	return NewAnthropicClient(p.apiKey, modelID)
}

func (p *AnthropicProvider) ValidateAPIKey(ctx context.Context) error {
	// Create a test client and make a minimal request
	client, err := p.CreateClient("claude-haiku-4-5")
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
