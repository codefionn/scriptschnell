package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	apiKey string
	client *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (p *OpenAIProvider) GetName() string {
	return "openai"
}

// OpenAI API response structures
type openAIModelsList struct {
	Data []openAIModelData `json:"data"`
}

type openAIModelData struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (p *OpenAIProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
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

	var modelsList openAIModelsList
	if err := json.NewDecoder(resp.Body).Decode(&modelsList); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Filter for chat models only and add metadata
	models := make([]*ModelInfo, 0)
	for _, m := range modelsList.Data {
		// Only include GPT models (chat completion models)
		if !strings.HasPrefix(m.ID, "gpt-") {
			continue
		}

		// Skip fine-tuned models (they contain colons)
		if strings.Contains(m.ID, ":") {
			continue
		}

		// Skip embeddings, TTS, whisper, dall-e, etc.
		if strings.Contains(m.ID, "embedding") ||
			strings.Contains(m.ID, "tts") ||
			strings.Contains(m.ID, "whisper") ||
			strings.Contains(m.ID, "dall-e") {
			continue
		}

		family := DetectModelFamily(m.ID)
		contextWindow := DetectContextWindow(m.ID, family)

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                FormatModelDisplayName(m.ID, family),
			Provider:            "openai",
			Description:         GetModelDescription(m.ID, family),
			ContextWindow:       contextWindow,
			MaxOutputTokens:     DetectMaxOutputTokens(m.ID, family, contextWindow),
			SupportsToolCalling: SupportsToolCalling(m.ID, family),
			SupportsStreaming:   true,
			OwnedBy:             m.OwnedBy,
		}

		models = append(models, info)
	}

	return models, nil
}

func (p *OpenAIProvider) CreateClient(modelID string) (Client, error) {
	return NewOpenAIClient(p.apiKey, modelID)
}

func (p *OpenAIProvider) ValidateAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
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
