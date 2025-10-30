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

		info := &ModelInfo{
			ID:                  m.ID,
			Name:                formatOpenAICompatibleModelName(m.ID),
			Provider:            "openai-compatible",
			Description:         getOpenAICompatibleModelDescription(m.ID),
			ContextWindow:       estimateOpenAICompatibleContextWindow(m.ID),
			MaxOutputTokens:     estimateOpenAICompatibleMaxOutputTokens(m.ID),
			SupportsToolCalling: true, // Most modern models support this
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

// Helper functions

func formatOpenAICompatibleModelName(id string) string {
	// Clean up common patterns in model IDs
	name := strings.ReplaceAll(id, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")

	// Capitalize first letter of each word
	parts := strings.Fields(name)
	formatted := make([]string, len(parts))
	for i, part := range parts {
		if len(part) > 0 {
			formatted[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}

	result := strings.Join(formatted, " ")
	if result == "" {
		return id
	}
	return result
}

func getOpenAICompatibleModelDescription(id string) string {
	idLower := strings.ToLower(id)

	// Detect common model families
	if strings.Contains(idLower, "llama") {
		if strings.Contains(idLower, "3.3") {
			return "Meta Llama 3.3 - Advanced reasoning and instruction following"
		}
		if strings.Contains(idLower, "3.2") {
			return "Meta Llama 3.2 - Efficient multilingual model"
		}
		if strings.Contains(idLower, "3.1") {
			return "Meta Llama 3.1 - Enhanced capabilities"
		}
		if strings.Contains(idLower, "3") {
			return "Meta Llama 3 - High-performance open model"
		}
		return "Meta Llama - Open-source language model"
	}

	if strings.Contains(idLower, "mistral") {
		if strings.Contains(idLower, "large") {
			return "Mistral Large - Top-tier reasoning"
		}
		if strings.Contains(idLower, "medium") {
			return "Mistral Medium - Balanced performance"
		}
		if strings.Contains(idLower, "small") {
			return "Mistral Small - Efficient model"
		}
		return "Mistral - Efficient language model"
	}

	if strings.Contains(idLower, "mixtral") {
		return "Mixtral - Mixture-of-Experts architecture"
	}

	if strings.Contains(idLower, "qwen") {
		return "Qwen - Alibaba's multilingual model"
	}

	if strings.Contains(idLower, "gemma") {
		return "Gemma - Google's open model"
	}

	if strings.Contains(idLower, "phi") {
		return "Phi - Microsoft's small language model"
	}

	if strings.Contains(idLower, "deepseek") {
		return "DeepSeek - Advanced reasoning model"
	}

	if strings.Contains(idLower, "code") {
		return "Code-specialized model"
	}

	if strings.Contains(idLower, "chat") {
		return "Chat-optimized model"
	}

	if strings.Contains(idLower, "instruct") {
		return "Instruction-following model"
	}

	return "OpenAI-compatible language model"
}

func estimateOpenAICompatibleContextWindow(id string) int {
	idLower := strings.ToLower(id)

	// Check for explicit context window indicators
	if strings.Contains(idLower, "128k") {
		return 131072
	}
	if strings.Contains(idLower, "64k") {
		return 65536
	}
	if strings.Contains(idLower, "32k") {
		return 32768
	}
	if strings.Contains(idLower, "16k") {
		return 16384
	}
	if strings.Contains(idLower, "8k") {
		return 8192
	}
	if strings.Contains(idLower, "4k") {
		return 4096
	}

	// Estimate based on model family
	if strings.Contains(idLower, "llama-3.3") || strings.Contains(idLower, "llama-3.2") {
		return 131072 // 128k
	}
	if strings.Contains(idLower, "llama-3.1") {
		return 131072 // 128k
	}
	if strings.Contains(idLower, "llama-3") {
		return 8192
	}
	if strings.Contains(idLower, "llama-2") {
		return 4096
	}

	if strings.Contains(idLower, "mistral") {
		if strings.Contains(idLower, "large") {
			return 131072 // 128k
		}
		return 32768
	}

	if strings.Contains(idLower, "mixtral") {
		if strings.Contains(idLower, "8x22b") {
			return 65536
		}
		return 32768
	}

	if strings.Contains(idLower, "qwen") {
		if strings.Contains(idLower, "2.5") {
			return 131072
		}
		return 32768
	}

	if strings.Contains(idLower, "deepseek") {
		return 65536
	}

	// Default conservative estimate
	return 8192
}

func estimateOpenAICompatibleMaxOutputTokens(id string) int {
	idLower := strings.ToLower(id)

	// Most models can output up to 4k-8k tokens
	// Adjust based on known model families
	if strings.Contains(idLower, "deepseek") {
		return 8192
	}

	if strings.Contains(idLower, "qwen-2.5") {
		return 8192
	}

	if strings.Contains(idLower, "mistral-large") {
		return 8192
	}

	if strings.Contains(idLower, "llama-3.3") || strings.Contains(idLower, "llama-3.2") || strings.Contains(idLower, "llama-3.1") {
		return 8192
	}

	// Default
	return 4096
}
