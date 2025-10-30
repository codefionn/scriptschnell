package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenRouterProvider implements the Provider interface for OpenRouter
type OpenRouterProvider struct {
	apiKey string
	client *http.Client
}

// NewOpenRouterProvider creates a new OpenRouter provider instance
func NewOpenRouterProvider(apiKey string) *OpenRouterProvider {
	return &OpenRouterProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (p *OpenRouterProvider) GetName() string {
	return "openrouter"
}

type openRouterModelsResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID                  string                 `json:"id"`
	CanonicalSlug       string                 `json:"canonical_slug"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	ContextLength       *float64               `json:"context_length"`
	SupportedParameters []string               `json:"supported_parameters"`
	DefaultParameters   map[string]interface{} `json:"default_parameters"`
	Architecture        struct {
		Modality string `json:"modality"`
	} `json:"architecture"`
	TopProvider struct {
		ContextLength       *float64 `json:"context_length"`
		MaxCompletionTokens *float64 `json:"max_completion_tokens"`
		IsModerated         bool     `json:"is_moderated"`
	} `json:"top_provider"`
}

func (p *OpenRouterProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data openRouterModelsResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*ModelInfo, 0, len(data.Data))
	for _, model := range data.Data {
		capabilities := append([]string(nil), model.SupportedParameters...)
		if model.Architecture.Modality != "" {
			capabilities = append(capabilities, "modality:"+model.Architecture.Modality)
		}
		if model.CanonicalSlug != "" {
			capabilities = append(capabilities, "canonical:"+model.CanonicalSlug)
		}
		if len(model.DefaultParameters) > 0 {
			for key := range model.DefaultParameters {
				capabilities = append(capabilities, "default:"+key)
			}
		}

		contextWindow := openRouterContextWindow(model)
		maxOutputTokens := estimateMaxOutputTokens(model.ID, contextWindow)
		if maxOutputTokens == 0 && model.TopProvider.MaxCompletionTokens != nil {
			maxOutputTokens = int(*model.TopProvider.MaxCompletionTokens)
		}

		info := &ModelInfo{
			ID:                  model.ID,
			Name:                openRouterDisplayName(model.ID, model.Name),
			Provider:            "openrouter",
			Description:         model.Description,
			ContextWindow:       contextWindow,
			MaxOutputTokens:     maxOutputTokens,
			SupportsToolCalling: openRouterSupportsToolCalling(model.SupportedParameters),
			SupportsStreaming:   openRouterSupportsStreaming(model.SupportedParameters),
			OwnedBy:             openRouterOwner(model.ID),
			Capabilities:        capabilities,
		}

		models = append(models, info)
	}

	return models, nil
}

func (p *OpenRouterProvider) CreateClient(modelID string) (Client, error) {
	return NewOpenRouterClient(p.apiKey, modelID)
}

func (p *OpenRouterProvider) ValidateAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/auth/key", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func openRouterDisplayName(id, name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return id
}

func openRouterContextWindow(model openRouterModel) int {
	if model.ContextLength != nil {
		return int(*model.ContextLength)
	}
	if model.TopProvider.ContextLength != nil {
		return int(*model.TopProvider.ContextLength)
	}
	return 0
}

func openRouterOwner(id string) string {
	if parts := strings.Split(id, "/"); len(parts) > 1 {
		return parts[0]
	}
	return "openrouter"
}

func openRouterSupportsToolCalling(params []string) bool {
	for _, param := range params {
		switch strings.ToLower(param) {
		case "tools", "tool_choice", "functions", "function_call":
			return true
		}
	}
	return false
}

func openRouterSupportsStreaming(params []string) bool {
	for _, param := range params {
		normalized := strings.ToLower(strings.TrimSpace(param))
		if strings.Contains(normalized, "stream") {
			return true
		}
	}
	return false
}

// estimateMaxOutputTokens estimates max output tokens based on model ID and context window
// This handles models from various providers (OpenAI, Anthropic, Google, etc.) on OpenRouter
func estimateMaxOutputTokens(modelID string, contextWindow int) int {
	idLower := strings.ToLower(modelID)

	// OpenAI models
	if strings.Contains(idLower, "chatgpt-5") {
		return 128000 // ChatGPT-5 variant
	}
	if strings.Contains(idLower, "gpt-5") {
		if strings.Contains(idLower, "preview") && !strings.Contains(idLower, "mini") && !strings.Contains(idLower, "nano") {
			return 272000 // GPT-5 preview
		}
		return 128000 // GPT-5, GPT-5-mini, GPT-5-nano, GPT-5-codex
	}
	if strings.Contains(idLower, "o3") {
		return 100000
	}
	if strings.Contains(idLower, "o1-preview") {
		return 32768
	}
	if strings.Contains(idLower, "o1-mini") {
		return 65536
	}
	if strings.Contains(idLower, "o1") {
		return 100000
	}
	if strings.Contains(idLower, "gpt-4o-mini") {
		return 16384
	}
	if strings.Contains(idLower, "gpt-4o") {
		return 16384
	}
	if strings.Contains(idLower, "gpt-4") {
		if contextWindow >= 32000 {
			return 8192 // gpt-4-32k
		}
		if contextWindow >= 100000 {
			return 4096 // gpt-4-turbo
		}
		return 8192 // gpt-4
	}
	if strings.Contains(idLower, "gpt-3.5") {
		return 4096
	}

	// Anthropic Claude models
	if strings.Contains(idLower, "claude-4") {
		if strings.Contains(idLower, "sonnet") && contextWindow >= 1000000 {
			return 16384 // Claude 4.5 Sonnet with 1M context
		}
		return 8192 // Claude 4.x models
	}
	if strings.Contains(idLower, "claude-3-5") || strings.Contains(idLower, "claude-3.5") {
		return 8192
	}
	if strings.Contains(idLower, "claude-3") || strings.Contains(idLower, "claude-2") {
		return 4096
	}
	if strings.Contains(idLower, "claude") {
		return 4096
	}

	// Google Gemini models
	if strings.Contains(idLower, "gemini") {
		if strings.Contains(idLower, "flash") {
			return 8192
		}
		if strings.Contains(idLower, "pro") {
			return 8192
		}
		return 4096
	}

	// Meta Llama models
	if strings.Contains(idLower, "llama") {
		if contextWindow >= 100000 {
			return 4096
		}
		return 2048
	}

	// Mistral models
	if strings.Contains(idLower, "mistral") || strings.Contains(idLower, "mixtral") {
		return 4096
	}

	// Default: use a conservative estimate based on context window
	// Typically max output is much smaller than context window
	if contextWindow >= 200000 {
		return 8192
	}
	if contextWindow >= 100000 {
		return 4096
	}
	if contextWindow >= 32000 {
		return 4096
	}
	return 2048
}
