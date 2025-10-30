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

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                formatModelName(m.ID),
			Provider:            "openai",
			Description:         getOpenAIModelDescription(m.ID),
			ContextWindow:       getOpenAIContextWindow(m.ID),
			MaxOutputTokens:     getOpenAIMaxOutputTokens(m.ID),
			SupportsToolCalling: supportsToolCalling(m.ID),
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

// Helper functions

func formatModelName(id string) string {
	// Convert gpt-4-turbo-preview -> GPT-4 Turbo Preview
	parts := strings.Split(id, "-")
	formatted := make([]string, 0, len(parts))

	for _, part := range parts {
		if part == "gpt" {
			formatted = append(formatted, strings.ToUpper(part))
		} else if len(part) > 0 {
			formatted = append(formatted, strings.ToUpper(part[:1])+part[1:])
		}
	}

	return strings.Join(formatted, " ")
}

func getOpenAIModelDescription(id string) string {
	descriptions := map[string]string{
		"gpt-4":               "Most capable GPT-4 model for complex tasks",
		"gpt-4-turbo":         "Latest GPT-4 Turbo with improved performance",
		"gpt-4-turbo-preview": "Preview of GPT-4 Turbo capabilities",
		"gpt-4-0125-preview":  "GPT-4 Turbo preview from January 2024",
		"gpt-4-1106-preview":  "GPT-4 Turbo preview from November 2023",
		"gpt-4-32k":           "GPT-4 with extended 32k context window",
		"gpt-3.5-turbo":       "Fast and efficient for most tasks",
		"gpt-3.5-turbo-16k":   "GPT-3.5 with 16k context window",
		"gpt-3.5-turbo-0125":  "Latest GPT-3.5 Turbo snapshot",
	}

	if desc, ok := descriptions[id]; ok {
		return desc
	}

	// Default descriptions based on model family
	if strings.HasPrefix(id, "gpt-4") {
		return "GPT-4 model variant"
	}
	if strings.HasPrefix(id, "gpt-3.5") {
		return "GPT-3.5 model variant"
	}

	return "OpenAI language model"
}

func getOpenAIContextWindow(id string) int {
	// Check exact matches first
	exactMatches := map[string]int{
		"gpt-5-chat-latest":   128000, // GPT-5 Chat Latest
		"chatgpt-5":           128000, // GPT-5 ChatGPT variant
		"gpt-5":               400000,
		"gpt-5-preview":       400000,
		"gpt-5-mini":          400000,
		"gpt-5-mini-preview":  400000,
		"gpt-5-nano":          400000,
		"gpt-5-nano-preview":  400000,
		"gpt-5-codex":         400000,
		"o3":                  200000,
		"o3-mini":             200000,
		"o1":                  200000,
		"o1-preview":          128000,
		"o1-mini":             128000,
		"gpt-4o":              128000,
		"gpt-4o-mini":         128000,
		"gpt-4-turbo":         128000,
		"gpt-4-turbo-preview": 128000,
		"gpt-4-0125-preview":  128000,
		"gpt-4-1106-preview":  128000,
		"gpt-4":               8192,
		"gpt-4-32k":           32768,
		"gpt-3.5-turbo":       16385,
		"gpt-3.5-turbo-16k":   16385,
		"gpt-3.5-turbo-0125":  16385,
	}

	if window, ok := exactMatches[id]; ok {
		return window
	}

	// Pattern matching - check longer prefixes first to avoid false matches
	// (e.g., "gpt-4o-2024" should match "gpt-4o" not "gpt-4")
	prefixPatterns := []struct {
		prefix string
		window int
	}{
		{"gpt-5-chat-latest", 128000},
		{"chatgpt-5", 128000},
		{"gpt-5-preview", 400000},
		{"gpt-5-mini-preview", 400000},
		{"gpt-5-mini", 400000},
		{"gpt-5-nano-preview", 400000},
		{"gpt-5-nano", 400000},
		{"gpt-5-codex", 400000},
		{"gpt-5", 400000},
		{"o3-mini", 200000},
		{"o3", 200000},
		{"o1-preview", 128000},
		{"o1-mini", 128000},
		{"o1", 200000},
		{"gpt-4o-mini", 128000},
		{"gpt-4o", 128000},
		{"gpt-4-turbo-preview", 128000},
		{"gpt-4-turbo", 128000},
		{"gpt-4-32k", 32768},
		{"gpt-3.5-turbo-16k", 16385},
		{"gpt-3.5-turbo", 16385},
	}

	for _, pattern := range prefixPatterns {
		if strings.HasPrefix(id, pattern.prefix) {
			return pattern.window
		}
	}

	// Fallback: check for special naming patterns
	if strings.Contains(id, "32k") {
		return 32768
	}
	if strings.Contains(id, "16k") {
		return 16385
	}

	// Final fallbacks by model family
	if strings.HasPrefix(id, "gpt-4") {
		return 8192
	}
	if strings.HasPrefix(id, "gpt-3.5") {
		return 4096
	}

	return 4096
}

func getOpenAIMaxOutputTokens(id string) int {
	// Check exact matches first
	exactMatches := map[string]int{
		"gpt-5-chat-latest":   128000, // GPT-5 Chat Latest
		"chatgpt-5":           128000, // GPT-5 ChatGPT variant has different limits
		"gpt-5":               128000,
		"gpt-5-preview":       272000,
		"gpt-5-mini":          128000,
		"gpt-5-mini-preview":  128000,
		"gpt-5-nano":          128000,
		"gpt-5-nano-preview":  128000,
		"gpt-5-codex":         128000,
		"o3":                  100000,
		"o3-mini":             100000,
		"o1":                  100000,
		"o1-preview":          32768,
		"o1-mini":             65536,
		"gpt-4o":              16384,
		"gpt-4o-mini":         16384,
		"gpt-4-turbo":         4096,
		"gpt-4-turbo-preview": 4096,
		"gpt-4-0125-preview":  4096,
		"gpt-4-1106-preview":  4096,
		"gpt-4":               8192,
		"gpt-4-32k":           8192,
		"gpt-3.5-turbo":       4096,
		"gpt-3.5-turbo-16k":   4096,
		"gpt-3.5-turbo-0125":  4096,
	}

	if maxOutput, ok := exactMatches[id]; ok {
		return maxOutput
	}

	// Pattern matching - check longer prefixes first to avoid false matches
	prefixPatterns := []struct {
		prefix    string
		maxOutput int
	}{
		{"gpt-5-chat-latest", 128000},
		{"chatgpt-5", 128000},
		{"gpt-5-preview", 272000},
		{"gpt-5-mini-preview", 128000},
		{"gpt-5-mini", 128000},
		{"gpt-5-nano-preview", 128000},
		{"gpt-5-nano", 128000},
		{"gpt-5-codex", 128000},
		{"gpt-5", 128000},
		{"o3-mini", 100000},
		{"o3", 100000},
		{"o1-preview", 32768},
		{"o1-mini", 65536},
		{"o1", 100000},
		{"gpt-4o-mini", 16384},
		{"gpt-4o", 16384},
		{"gpt-4-turbo-preview", 4096},
		{"gpt-4-turbo", 4096},
		{"gpt-4-32k", 8192},
		{"gpt-3.5-turbo-16k", 4096},
		{"gpt-3.5-turbo", 4096},
	}

	for _, pattern := range prefixPatterns {
		if strings.HasPrefix(id, pattern.prefix) {
			return pattern.maxOutput
		}
	}

	// Final fallbacks by model family
	if strings.HasPrefix(id, "gpt-4") {
		return 4096
	}
	if strings.HasPrefix(id, "gpt-3.5") {
		return 4096
	}

	return 4096
}

func supportsToolCalling(id string) bool {
	// Most modern GPT models support tool calling
	// Only exclude very old models
	if strings.Contains(id, "0301") || strings.Contains(id, "0314") {
		return false
	}
	return true
}
