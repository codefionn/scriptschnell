package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAICompatibleProvider implements the Provider interface for OpenAI-compatible APIs
// This includes local LLMs (LM Studio, LocalAI, Ollama with OpenAI compat layer, etc.)
// and custom deployments that follow the OpenAI API specification
type OpenAICompatibleProvider struct {
	apiKey  string
	baseURL string // Custom base URL for the API
	client  *http.Client
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider
// baseURL should be the API endpoint (e.g., "http://localhost:1234/v1" for LM Studio)
// If apiKey is empty, requests will be made without authentication
func NewOpenAICompatibleProvider(apiKey string, baseURL string) *OpenAICompatibleProvider {
	// Ensure baseURL ends without trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &OpenAICompatibleProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (p *OpenAICompatibleProvider) GetName() string {
	return "openai-compatible"
}

// OpenAI-compatible API response structures (same as OpenAI)
type openAICompatibleModelsList struct {
	Data []openAICompatibleModelData `json:"data"`
}

type openAICompatibleModelData struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (p *OpenAICompatibleProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	modelsURL := p.baseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add auth header only if API key is provided
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
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

	var modelsList openAICompatibleModelsList
	if err := json.NewDecoder(resp.Body).Decode(&modelsList); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert all models to ModelInfo
	models := make([]*ModelInfo, 0)
	for _, m := range modelsList.Data {
		// Skip non-chat models if we can detect them
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
			Provider:            "openai-compatible",
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

func (p *OpenAICompatibleProvider) CreateClient(modelID string) (Client, error) {
	return NewOpenAICompatibleClient(p.apiKey, p.baseURL, modelID)
}

func (p *OpenAICompatibleProvider) ValidateAPIKey(ctx context.Context) error {
	modelsURL := p.baseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add auth header only if API key is provided
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid API key or unauthorized")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
