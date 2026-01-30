package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// KimiProvider implements the Provider interface for Kimi (Moonshot AI)
type KimiProvider struct {
	apiKey string
	client *http.Client
}

// NewKimiProvider creates a new Kimi provider
func NewKimiProvider(apiKey string) *KimiProvider {
	return &KimiProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (p *KimiProvider) GetName() string {
	return "kimi"
}

// Kimi API response structures
type kimiModelsList struct {
	Data []kimiModelData `json:"data"`
}

type kimiModelData struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (p *KimiProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.moonshot.ai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
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

	var modelsList kimiModelsList
	if err := json.NewDecoder(resp.Body).Decode(&modelsList); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert models to ModelInfo with Kimi-specific metadata
	models := make([]*ModelInfo, 0)
	for _, m := range modelsList.Data {
		info := kimiModelInfo(m.ID)
		if info != nil {
			models = append(models, info)
		}
	}

	// If API returned no models, use fallback list
	if len(models) == 0 {
		return kimiFallbackModels(), nil
	}

	return models, nil
}

func (p *KimiProvider) CreateClient(modelID string) (Client, error) {
	return NewKimiClient(p.apiKey, modelID)
}

func (p *KimiProvider) ValidateAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.moonshot.ai/v1/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// kimiModelInfo returns ModelInfo for a Kimi model ID
func kimiModelInfo(modelID string) *ModelInfo {
	// Kimi model specifications based on official documentation
	modelSpecs := map[string]struct {
		name              string
		contextWindow     int
		maxOutputTokens   int
		supportsToolCall  bool
		supportsStreaming bool
		description       string
	}{
		"kimi-k2.5": {
			name:              "Kimi K2.5",
			contextWindow:     256000,
			maxOutputTokens:   32000,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Latest Kimi model with enhanced capabilities and thinking mode",
		},
		"kimi-k2-0905-preview": {
			name:              "Kimi K2 Preview",
			contextWindow:     256000,
			maxOutputTokens:   32000,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Preview version of Kimi K2 model",
		},
		"kimi-k2-0711-preview": {
			name:              "Kimi K2 Preview",
			contextWindow:     256000,
			maxOutputTokens:   32000,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Preview version of Kimi K2 model",
		},
		"kimi-k2-turbo-preview": {
			name:              "Kimi K2 Turbo",
			contextWindow:     256000,
			maxOutputTokens:   32000,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Fast response Kimi K2 model for quick interactions",
		},
		"kimi-k2-thinking-turbo": {
			name:              "Kimi K2 Thinking Turbo",
			contextWindow:     256000,
			maxOutputTokens:   32000,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Kimi K2 model with enhanced reasoning capabilities",
		},
		"kimi-k2-thinking": {
			name:              "Kimi K2 Thinking",
			contextWindow:     256000,
			maxOutputTokens:   32000,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Kimi K2 model with enhanced reasoning capabilities",
		},
		"moonshot-v1-8k": {
			name:              "Moonshot V1 8K",
			contextWindow:     8192,
			maxOutputTokens:   4096,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Moonshot V1 model with 8K context window",
		},
		"moonshot-v1-32k": {
			name:              "Moonshot V1 32K",
			contextWindow:     32768,
			maxOutputTokens:   8192,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Moonshot V1 model with 32K context window",
		},
		"moonshot-v1-128k": {
			name:              "Moonshot V1 128K",
			contextWindow:     131072,
			maxOutputTokens:   8192,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Moonshot V1 model with 128K context window",
		},
		"moonshot-v1-auto": {
			name:              "Moonshot V1 Auto",
			contextWindow:     131072,
			maxOutputTokens:   8192,
			supportsToolCall:  true,
			supportsStreaming: true,
			description:       "Moonshot V1 model with automatic context selection",
		},
		"moonshot-v1-8k-vision-preview": {
			name:              "Moonshot V1 8K Vision",
			contextWindow:     8192,
			maxOutputTokens:   4096,
			supportsToolCall:  false, // Vision models may have limited tool support
			supportsStreaming: true,
			description:       "Moonshot V1 model with 8K context and vision capabilities",
		},
		"moonshot-v1-32k-vision-preview": {
			name:              "Moonshot V1 32K Vision",
			contextWindow:     32768,
			maxOutputTokens:   8192,
			supportsToolCall:  false, // Vision models may have limited tool support
			supportsStreaming: true,
			description:       "Moonshot V1 model with 32K context and vision capabilities",
		},
		"moonshot-v1-128k-vision-preview": {
			name:              "Moonshot V1 128K Vision",
			contextWindow:     131072,
			maxOutputTokens:   8192,
			supportsToolCall:  false, // Vision models may have limited tool support
			supportsStreaming: true,
			description:       "Moonshot V1 model with 128K context and vision capabilities",
		},
	}

	spec, ok := modelSpecs[modelID]
	if !ok {
		return nil
	}

	return &ModelInfo{
		ID:                  modelID,
		Name:                spec.name,
		Provider:            "kimi",
		Description:         spec.description,
		ContextWindow:       spec.contextWindow,
		MaxOutputTokens:     spec.maxOutputTokens,
		SupportsToolCalling: spec.supportsToolCall,
		SupportsStreaming:   spec.supportsStreaming,
		OwnedBy:             "moonshot-ai",
		Capabilities: []string{
			"chat",
			"chinese-support",
			"english-support",
			fmt.Sprintf("context:%d", spec.contextWindow),
		},
	}
}

// kimiFallbackModels provides a static list of Kimi models in case the API is unavailable
func kimiFallbackModels() []*ModelInfo {
	modelIDs := []string{
		"kimi-k2.5",
		"kimi-k2-turbo-preview",
		"kimi-k2-thinking",
		"moonshot-v1-8k",
		"moonshot-v1-32k",
		"moonshot-v1-128k",
		"moonshot-v1-8k-vision-preview",
		"moonshot-v1-32k-vision-preview",
		"moonshot-v1-128k-vision-preview",
	}

	models := make([]*ModelInfo, 0)
	for _, id := range modelIDs {
		info := kimiModelInfo(id)
		if info != nil {
			models = append(models, info)
		}
	}
	return models
}
