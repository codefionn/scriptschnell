package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// MistralProvider implements the Provider interface for Mistral AI
type MistralProvider struct {
	apiKey string
	client *http.Client
}

// NewMistralProvider creates a new Mistral provider
func NewMistralProvider(apiKey string) *MistralProvider {
	return &MistralProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (p *MistralProvider) GetName() string {
	return "mistral"
}

// Mistral API response structures
type mistralModelsResponse struct {
	Object string             `json:"object"`
	Data   []mistralModelData `json:"data"`
}

type mistralModelData struct {
	ID           string              `json:"id"`
	Object       string              `json:"object"`
	Created      int64               `json:"created"`
	OwnedBy      string              `json:"owned_by"`
	Capabilities mistralCapabilities `json:"capabilities,omitempty"`
}

type mistralCapabilities struct {
	CompletionChat  bool `json:"completion_chat"`
	CompletionFim   bool `json:"completion_fim"`
	FineTuning      bool `json:"fine_tuning"`
	FunctionCalling bool `json:"function_calling"`
	VisionCapable   bool `json:"vision"`
}

func (p *MistralProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.mistral.ai/v1/models", nil)
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

	var modelsResp mistralModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*ModelInfo, 0)
	for _, m := range modelsResp.Data {
		// Only include chat completion models
		if !m.Capabilities.CompletionChat {
			continue
		}

		family := DetectModelFamily(m.ID)
		contextWindow := DetectContextWindow(m.ID, family)

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                FormatModelDisplayName(m.ID, family),
			Provider:            "mistral",
			Description:         GetModelDescription(m.ID, family),
			ContextWindow:       contextWindow,
			MaxOutputTokens:     DetectMaxOutputTokens(m.ID, family, contextWindow),
			SupportsToolCalling: m.Capabilities.FunctionCalling,
			SupportsStreaming:   true,
			OwnedBy:             m.OwnedBy,
		}

		// Add capabilities
		capabilities := []string{}
		if m.Capabilities.FunctionCalling {
			capabilities = append(capabilities, "function-calling")
		}
		if m.Capabilities.VisionCapable {
			capabilities = append(capabilities, "vision")
		}
		if m.Capabilities.CompletionFim {
			capabilities = append(capabilities, "fim")
		}
		info.Capabilities = capabilities

		models = append(models, info)
	}

	// If API returns no models, fall back to hardcoded list
	if len(models) == 0 {
		return p.getFallbackModels(), nil
	}

	return models, nil
}

func (p *MistralProvider) getFallbackModels() []*ModelInfo {
	// Fallback hardcoded list in case API fails or returns nothing
	return []*ModelInfo{
		{
			ID:                  "mistral-large-latest",
			Name:                "Mistral Large",
			Provider:            "mistral",
			Description:         "Top-tier reasoning for complex tasks",
			ContextWindow:       131072,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling", "vision"},
		},
		{
			ID:                  "mistral-small-latest",
			Name:                "Mistral Small",
			Provider:            "mistral",
			Description:         "Cost-efficient reasoning for simpler tasks",
			ContextWindow:       32768,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling"},
		},
		{
			ID:                  "codestral-latest",
			Name:                "Codestral",
			Provider:            "mistral",
			Description:         "Code generation and completion specialist",
			ContextWindow:       32768,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling", "fim"},
		},
		{
			ID:                  "mistral-medium-latest",
			Name:                "Mistral Medium",
			Provider:            "mistral",
			Description:         "Balanced performance for most tasks",
			ContextWindow:       32768,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling"},
		},
		{
			ID:                  "pixtral-12b-latest",
			Name:                "Pixtral 12B",
			Provider:            "mistral",
			Description:         "Multimodal model with vision capabilities",
			ContextWindow:       131072,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling", "vision"},
		},
		{
			ID:                  "open-mistral-nemo",
			Name:                "Mistral Nemo",
			Provider:            "mistral",
			Description:         "Apache 2.0 licensed, efficient and powerful",
			ContextWindow:       131072,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling"},
		},
		{
			ID:                  "open-mistral-7b",
			Name:                "Mistral 7B",
			Provider:            "mistral",
			Description:         "Apache 2.0 licensed, fast and efficient",
			ContextWindow:       32768,
			MaxOutputTokens:     8192,
			SupportsToolCalling: false,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{},
		},
		{
			ID:                  "open-mixtral-8x7b",
			Name:                "Mixtral 8x7B",
			Provider:            "mistral",
			Description:         "Apache 2.0 licensed, Mixture-of-Experts architecture",
			ContextWindow:       32768,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling"},
		},
		{
			ID:                  "open-mixtral-8x22b",
			Name:                "Mixtral 8x22B",
			Provider:            "mistral",
			Description:         "Apache 2.0 licensed, large Mixture-of-Experts model",
			ContextWindow:       65536,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"function-calling"},
		},
	}
}

func (p *MistralProvider) CreateClient(modelID string) (Client, error) {
	return NewMistralClient(p.apiKey, modelID)
}

func (p *MistralProvider) ValidateAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.mistral.ai/v1/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("invalid API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
