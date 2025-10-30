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

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                groqDisplayName(m.ID),
			Provider:            "groq",
			Description:         groqModelDescription(m.ID),
			ContextWindow:       groqContextWindow(m),
			MaxOutputTokens:     groqMaxOutputTokens(m),
			SupportsToolCalling: groqSupportsToolCalling(m.ID),
			SupportsStreaming:   true,
			OwnedBy:             groqOwnedBy(m),
			Capabilities:        groqCapabilities(m),
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

// groqDisplayName converts a Groq model ID into a user-friendly display name.
func groqDisplayName(id string) string {
	displayNames := map[string]string{
		"llama-3.3-70b-versatile": "LLaMA 3.3 70B Versatile",
		"llama-3.1-8b-instant":    "LLaMA 3.1 8B Instant",
		"llama3-8b-8192":          "LLaMA 3 8B",
		"llama3-70b-8192":         "LLaMA 3 70B",
		"mixtral-8x7b-32768":      "Mixtral 8x7B",
		"mixtral-8x22b-32768":     "Mixtral 8x22B",
		"gemma2-9b-it":            "Gemma 2 9B Instruct",
		"gemma2-27b-it":           "Gemma 2 27B Instruct",
		"groq-llama3-8b":          "Groq LLaMA 3 8B",
		"groq-llama3-70b":         "Groq LLaMA 3 70B",
	}

	if name, ok := displayNames[id]; ok {
		return name
	}

	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})

	for i, part := range parts {
		if len(part) == 0 {
			continue
		}

		lower := strings.ToLower(part)
		if lower == "llama" {
			parts[i] = "LLaMA"
			continue
		}
		if lower == "groq" {
			parts[i] = "Groq"
			continue
		}

		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}

	if len(parts) == 0 {
		return id
	}

	return strings.Join(parts, " ")
}

// groqModelDescription provides human-friendly descriptions for Groq models.
func groqModelDescription(id string) string {
	descriptions := map[string]string{
		"llama-3.3-70b-versatile": "Balanced flagship model for reasoning, coding, and chat.",
		"llama-3.1-8b-instant":    "Fast 8B parameter model optimized for realtime tasks.",
		"llama3-8b-8192":          "General-purpose 8B model with 8K context window.",
		"llama3-70b-8192":         "70B parameter model offering stronger reasoning.",
		"mixtral-8x7b-32768":      "Mixture-of-Experts model with 32K context.",
		"mixtral-8x22b-32768":     "Large Mixture-of-Experts model for complex reasoning.",
		"gemma2-9b-it":            "Google Gemma 2 9B instruction-tuned model.",
		"gemma2-27b-it":           "Google Gemma 2 27B instruction-tuned model.",
		"groq-llama3-8b":          "Groq-optimized LLaMA 3 8B model for low latency.",
		"groq-llama3-70b":         "Groq-optimized LLaMA 3 70B model for high accuracy.",
	}

	if desc, ok := descriptions[id]; ok {
		return desc
	}

	if strings.Contains(strings.ToLower(id), "mixtral") {
		return "Mixtral model hosted by Groq."
	}
	if strings.Contains(strings.ToLower(id), "gemma") {
		return "Gemma model hosted by Groq."
	}
	if strings.Contains(strings.ToLower(id), "qwen") {
		return "Qwen model hosted by Groq."
	}

	return "Groq language model"
}

// groqContextWindow returns the context window for a Groq model, falling back to reasonable defaults.
func groqContextWindow(m groqModel) int {
	if m.ContextWindow > 0 {
		return m.ContextWindow
	}

	switch {
	case strings.Contains(m.ID, "32768"):
		return 32768
	case strings.Contains(m.ID, "8192"):
		return 8192
	case strings.Contains(strings.ToLower(m.ID), "70b"):
		return 32768
	}
	return 8192
}

// groqMaxOutputTokens estimates the maximum output tokens for a Groq model.
func groqMaxOutputTokens(m groqModel) int {
	if m.MaxCompletionTokens > 0 {
		return m.MaxCompletionTokens
	}

	// For most hosted models, Groq mirrors the upstream max output token limits (typically 4K-8K).
	ctx := groqContextWindow(m)
	if ctx >= 32768 {
		return 8192
	}
	if ctx >= 8192 {
		return 8192
	}
	return ctx
}

// groqSupportsToolCalling makes a best-effort guess about tool-calling support.
func groqSupportsToolCalling(id string) bool {
	lower := strings.ToLower(id)
	if strings.Contains(lower, "whisper") || strings.Contains(lower, "audio") || strings.Contains(lower, "tts") || strings.Contains(lower, "speech") {
		return false
	}
	return true
}

// groqCapabilities assembles a list of capability strings for the model.
func groqCapabilities(m groqModel) []string {
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
	return []*ModelInfo{
		{
			ID:                  "llama-3.3-70b-versatile",
			Name:                groqDisplayName("llama-3.3-70b-versatile"),
			Provider:            "groq",
			Description:         groqModelDescription("llama-3.3-70b-versatile"),
			ContextWindow:       131072,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "meta",
			Capabilities:        []string{"active", "context:131072"},
		},
		{
			ID:                  "llama-3.1-8b-instant",
			Name:                groqDisplayName("llama-3.1-8b-instant"),
			Provider:            "groq",
			Description:         groqModelDescription("llama-3.1-8b-instant"),
			ContextWindow:       8192,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "meta",
			Capabilities:        []string{"active", "context:8192"},
		},
		{
			ID:                  "mixtral-8x7b-32768",
			Name:                groqDisplayName("mixtral-8x7b-32768"),
			Provider:            "groq",
			Description:         groqModelDescription("mixtral-8x7b-32768"),
			ContextWindow:       32768,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "mistralai",
			Capabilities:        []string{"active", "context:32768"},
		},
		{
			ID:                  "gemma2-9b-it",
			Name:                groqDisplayName("gemma2-9b-it"),
			Provider:            "groq",
			Description:         groqModelDescription("gemma2-9b-it"),
			ContextWindow:       8192,
			MaxOutputTokens:     8192,
			SupportsToolCalling: true,
			SupportsStreaming:   true,
			OwnedBy:             "google",
			Capabilities:        []string{"active", "context:8192"},
		},
	}
}

// NewGroqClient creates a Groq client backed by the LangChain OpenAI-compatible implementation.
func NewGroqClient(apiKey, modelID string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("API key is required")
	}

	model := strings.TrimSpace(modelID)
	if model == "" {
		model = "llama-3.1-8b-instant"
	}

	client, err := NewOpenAICompatibleClient(apiKey, "https://api.groq.com/openai/v1", model)
	if err != nil {
		return nil, fmt.Errorf("failed to create Groq client: %w", err)
	}

	// Ensure the provider identifier is set to groq for downstream features.
	client.provider = "groq"

	return client, nil
}
