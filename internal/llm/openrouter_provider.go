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

		// Get context window from API or detect
		contextWindow := openRouterContextWindow(model)
		if contextWindow == 0 {
			family := DetectModelFamily(model.ID)
			contextWindow = DetectContextWindow(model.ID, family)
		}

		// Get max output tokens from API, otherwise detect
		family := DetectModelFamily(model.ID)
		maxOutputTokens := 0
		if model.TopProvider.MaxCompletionTokens != nil {
			maxOutputTokens = int(*model.TopProvider.MaxCompletionTokens)
		}
		if maxOutputTokens == 0 {
			maxOutputTokens = DetectMaxOutputTokens(model.ID, family, contextWindow)
		}

		// Use name from API if available
		displayName := model.Name
		if displayName == "" {
			displayName = FormatModelDisplayName(model.ID, family)
		}

		info := &ModelInfo{
			ID:                  model.ID,
			Name:                displayName,
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
	url := fmt.Sprintf("%s/key", strings.TrimRight(openRouterAPIBaseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", openRouterReferer)
	req.Header.Set("X-Title", openRouterAppTitle)

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

	var keyResp struct {
		Data struct {
			Label string `json:"label"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&keyResp); err != nil {
		return fmt.Errorf("failed to decode API key response: %w", err)
	}

	if strings.TrimSpace(keyResp.Data.Label) == "" {
		return fmt.Errorf("invalid API key")
	}

	return nil
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
