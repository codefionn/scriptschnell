package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GroqProvider implements the Provider interface for Groq's OpenAI-compatible API.
type GroqProvider struct {
	apiKey string
	client *http.Client
}

// NewGroqProvider creates a new Groq provider instance.
func NewGroqProvider(apiKey string) *GroqProvider {
	return &GroqProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (p *GroqProvider) GetName() string {
	return "groq"
}

// groqModelsResponse represents the response structure for the Groq models endpoint.
type groqModelsResponse struct {
	Object string      `json:"object"`
	Data   []groqModel `json:"data"`
}

// groqModel captures metadata for a single Groq model entry.
type groqModel struct {
	ID                  string          `json:"id"`
	Object              string          `json:"object"`
	Created             int64           `json:"created"`
	OwnedBy             string          `json:"owned_by"`
	Active              *bool           `json:"active"`
	ContextWindow       int             `json:"context_window"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Capabilities        json.RawMessage `json:"capabilities,omitempty"`
	PublicApps          json.RawMessage `json:"public_apps,omitempty"`
}

// ListModels retrieves the available Groq models via the REST API.
func (p *GroqProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.groq.com/openai/v1/models", nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var modelsResp groqModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*ModelInfo, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		if !shouldIncludeGroqModel(m) {
			continue
		}

		family := DetectModelFamily(m.ID)

		// Get context window from API or detect
		contextWindow := m.ContextWindow
		if contextWindow == 0 {
			contextWindow = DetectContextWindow(m.ID, family)
		}

		// Get max output tokens from API or detect
		maxOutputTokens := m.MaxCompletionTokens
		if maxOutputTokens == 0 {
			maxOutputTokens = DetectMaxOutputTokens(m.ID, family, contextWindow)
		}

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                FormatModelDisplayName(m.ID, family),
			Provider:            "groq",
			Description:         GetModelDescription(m.ID, family),
			ContextWindow:       contextWindow,
			MaxOutputTokens:     maxOutputTokens,
			SupportsToolCalling: SupportsToolCalling(m.ID, family),
			SupportsStreaming:   true,
			OwnedBy:             groqOwnedBy(m),
			Capabilities:        groqCapabilities(m, contextWindow),
		}

		models = append(models, info)
	}

	if len(models) == 0 {
		return groqFallbackModels(), nil
	}

	return models, nil
}

// CreateClient creates a new client for the specified Groq model.
func (p *GroqProvider) CreateClient(modelID string) (Client, error) {
	return NewGroqClient(p.apiKey, modelID)
}

// ValidateAPIKey validates the provided Groq API key by attempting to list models.
func (p *GroqProvider) ValidateAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.groq.com/openai/v1/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate API key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("invalid API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

// shouldIncludeGroqModel filters out non-chat models from the Groq listings.
func shouldIncludeGroqModel(m groqModel) bool {
	if m.Active != nil && !*m.Active {
		return false
	}

	idLower := strings.ToLower(m.ID)

	// Exclude audio, embedding, and moderation models that are not chat-capable.
	excludedKeywords := []string{
		"whisper", "audio", "tts", "speech", "embedding", "guard", "moderation",
	}
	for _, keyword := range excludedKeywords {
		if strings.Contains(idLower, keyword) {
			return false
		}
	}

	// For Groq, all remaining models are assumed to be chat-compatible.
	return true
}

// groqCapabilities assembles a list of capability strings for the model.
func groqCapabilities(m groqModel, contextWindow int) []string {
	caps := []string{}
	if m.Active != nil && *m.Active {
		caps = append(caps, "active")
	}

	if m.ContextWindow > 0 {
		caps = append(caps, fmt.Sprintf("context:%d", m.ContextWindow))
	}

	// We preserve the raw capabilities blob (if present) as indicators for advanced use-cases.
	if len(m.Capabilities) > 0 {
		caps = append(caps, "capabilities-json")
	}

	return caps
}

// groqOwnedBy normalises the owner information.
func groqOwnedBy(m groqModel) string {
	if m.OwnedBy != "" {
		return m.OwnedBy
	}
	if parts := strings.Split(m.ID, "/"); len(parts) > 1 {
		return parts[0]
	}
	return "groq"
}

// groqFallbackModels provides a static list of models in case the API is unavailable.
func groqFallbackModels() []*ModelInfo {
	models := []struct {
		id      string
		owner   string
		context int
	}{
		{"llama-3.3-70b-versatile", "meta", 131072},
		{"llama-3.1-8b-instant", "meta", 8192},
		{"mixtral-8x7b-32768", "mistralai", 32768},
		{"gemma2-9b-it", "google", 8192},
	}

	result := make([]*ModelInfo, 0, len(models))
	for _, m := range models {
		family := DetectModelFamily(m.id)
		contextWindow := m.context
		maxOutputTokens := DetectMaxOutputTokens(m.id, family, contextWindow)

		result = append(result, &ModelInfo{
			ID:                  m.id,
			Name:                FormatModelDisplayName(m.id, family),
			Provider:            "groq",
			Description:         GetModelDescription(m.id, family),
			ContextWindow:       contextWindow,
			MaxOutputTokens:     maxOutputTokens,
			SupportsToolCalling: SupportsToolCalling(m.id, family),
			SupportsStreaming:   true,
			OwnedBy:             m.owner,
			Capabilities:        []string{"active", fmt.Sprintf("context:%d", contextWindow)},
		})
	}

	return result
}

// NewGroqClient creates a Groq client backed by the native OpenAI-compatible implementation.
func NewGroqClient(apiKey, modelID string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("API key is required")
	}

	model := strings.TrimSpace(modelID)
	if model == "" {
		model = "llama-3.1-8b-instant"
	}

	return NewOpenAICompatibleClient(apiKey, "https://api.groq.com/openai/v1", model)
}
