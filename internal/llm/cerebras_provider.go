package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CerebrasProvider implements the Provider interface for Cerebras Cloud AI.
type CerebrasProvider struct {
	apiKey string
	client *http.Client
}

// NewCerebrasProvider creates a new Cerebras provider instance.
func NewCerebrasProvider(apiKey string) *CerebrasProvider {
	return &CerebrasProvider{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetName returns the provider name.
func (p *CerebrasProvider) GetName() string {
	return "cerebras"
}

// cerebrasModelResponse represents the response from Cerebras /v1/models endpoint.
type cerebrasModelResponse struct {
	Object string               `json:"object"`
	Data   []cerebrasModelEntry `json:"data"`
}

type cerebrasModelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ListModels fetches available models from the Cerebras API.
func (p *CerebrasProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.cerebras.ai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return p.getFallbackModels(), nil // Return fallback on network error
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return p.getFallbackModels(), nil // Return fallback on API error
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return p.getFallbackModels(), nil
	}

	var modelsResp cerebrasModelResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return p.getFallbackModels(), nil
	}

	if len(modelsResp.Data) == 0 {
		return p.getFallbackModels(), nil
	}

	models := make([]*ModelInfo, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		// Skip non-model entries
		if m.Object != "model" {
			continue
		}

		models = append(models, &ModelInfo{
			ID:                  m.ID,
			Provider:            "cerebras",
			Name:                getCerebrasDisplayName(m.ID),
			Description:         getCerebrasDescription(m.ID),
			ContextWindow:       getCerebrasContextWindow(m.ID),
			MaxOutputTokens:     getCerebrasMaxOutputTokens(m.ID),
			SupportsStreaming:   true,
			SupportsToolCalling: cerebrasSupportsToolCalling(m.ID),
		})
	}

	return models, nil
}

// CreateClient creates a new client for the specified Cerebras model.
func (p *CerebrasProvider) CreateClient(modelID string) (Client, error) {
	return NewCerebrasClient(p.apiKey, modelID)
}

// getFallbackModels returns a curated list of Cerebras models as a fallback.
func (p *CerebrasProvider) getFallbackModels() []*ModelInfo {
	models := []string{
		"llama-4-scout-17b-16e-instruct",
		"llama3.1-8b",
		"llama-3.3-70b",
		"qwen-3-32b",
		"qwen-3-235b-a22b-instruct-2507",
		"qwen-3-235b-a22b-thinking-2507",
		"qwen-3-coder-480b",
		"gpt-oss-120b",
	}

	result := make([]*ModelInfo, 0, len(models))
	for _, id := range models {
		result = append(result, &ModelInfo{
			ID:                  id,
			Provider:            "cerebras",
			Name:                getCerebrasDisplayName(id),
			Description:         getCerebrasDescription(id),
			ContextWindow:       getCerebrasContextWindow(id),
			MaxOutputTokens:     getCerebrasMaxOutputTokens(id),
			SupportsStreaming:   true,
			SupportsToolCalling: cerebrasSupportsToolCalling(id),
		})
	}

	return result
}

// getCerebrasDisplayName returns a user-friendly display name for a Cerebras model.
func getCerebrasDisplayName(id string) string {
	displayNames := map[string]string{
		"llama-4-scout-17b-16e-instruct": "Llama 4 Scout (17B, 16-Expert)",
		"llama3.1-8b":                    "Llama 3.1 (8B)",
		"llama-3.3-70b":                  "Llama 3.3 (70B)",
		"qwen-3-32b":                     "Qwen 3 (32B)",
		"qwen-3-235b-a22b-instruct-2507": "Qwen 3 (235B, Instruct)",
		"qwen-3-235b-a22b-thinking-2507": "Qwen 3 (235B, Thinking)",
		"qwen-3-coder-480b":              "Qwen 3 Coder (480B)",
		"gpt-oss-120b":                   "GPT-OSS (120B)",
	}

	if displayName, ok := displayNames[id]; ok {
		return displayName
	}
	return id
}

// getCerebrasDescription returns a description for a Cerebras model.
func getCerebrasDescription(id string) string {
	descriptions := map[string]string{
		"llama-4-scout-17b-16e-instruct": "Llama 4 Scout with 16 experts, optimized for instruction following",
		"llama3.1-8b":                    "Llama 3.1 8B parameter model, fast and efficient",
		"llama-3.3-70b":                  "Llama 3.3 70B parameter model, powerful general-purpose model",
		"qwen-3-32b":                     "Qwen 3 32B parameter model, multilingual capabilities",
		"qwen-3-235b-a22b-instruct-2507": "Qwen 3 235B parameter model, instruction-tuned (preview)",
		"qwen-3-235b-a22b-thinking-2507": "Qwen 3 235B parameter model with thinking capabilities (preview)",
		"qwen-3-coder-480b":              "Qwen 3 Coder 480B parameter model, specialized for code generation (preview)",
		"gpt-oss-120b":                   "GPT-OSS 120B parameter model with reasoning capabilities",
	}

	if description, ok := descriptions[id]; ok {
		return description
	}
	return "Cerebras Cloud AI model"
}

// getCerebrasContextWindow returns the context window size for a Cerebras model.
func getCerebrasContextWindow(id string) int {
	// Based on Cerebras documentation and typical model sizes
	contextWindows := map[string]int{
		"llama-4-scout-17b-16e-instruct": 128000, // 128k context
		"llama3.1-8b":                    128000, // 128k context
		"llama-3.3-70b":                  128000, // 128k context
		"qwen-3-32b":                     128000, // 128k context
		"qwen-3-235b-a22b-instruct-2507": 128000, // 128k context
		"qwen-3-235b-a22b-thinking-2507": 128000, // 128k context
		"qwen-3-coder-480b":              128000, // 128k context
		"gpt-oss-120b":                   128000, // 128k context
	}

	if window, ok := contextWindows[id]; ok {
		return window
	}
	return 128000 // Default: 128k
}

// getCerebrasMaxOutputTokens returns the maximum output tokens for a Cerebras model.
func getCerebrasMaxOutputTokens(id string) int {
	// Based on Cerebras documentation
	// Default settings: qwen-3-32b = 40k | llama-3.3-70b = 64k
	maxOutputTokens := map[string]int{
		"llama-4-scout-17b-16e-instruct": 8192,  // Conservative estimate
		"llama3.1-8b":                    8192,  // Conservative estimate
		"llama-3.3-70b":                  65536, // 64k documented
		"qwen-3-32b":                     40960, // 40k documented
		"qwen-3-235b-a22b-instruct-2507": 40960, // 40k estimate
		"qwen-3-235b-a22b-thinking-2507": 40960, // 40k estimate
		"qwen-3-coder-480b":              40960, // 40k estimate
		"gpt-oss-120b":                   8192,  // Conservative estimate
	}

	if maxTokens, ok := maxOutputTokens[id]; ok {
		return maxTokens
	}
	return 8192 // Default: 8k
}

// cerebrasSupportsToolCalling checks if a Cerebras model supports tool/function calling.
func cerebrasSupportsToolCalling(id string) bool {
	// Based on Cerebras documentation, tool calling is supported
	// The documentation shows tool_choice and tools parameters in the API
	toolCallingModels := map[string]bool{
		"llama-4-scout-17b-16e-instruct": true,
		"llama3.1-8b":                    true,
		"llama-3.3-70b":                  true,
		"qwen-3-32b":                     true,
		"qwen-3-235b-a22b-instruct-2507": true,
		"qwen-3-235b-a22b-thinking-2507": true,
		"qwen-3-coder-480b":              true,
		"gpt-oss-120b":                   true,
	}

	if supports, ok := toolCallingModels[id]; ok {
		return supports
	}
	return true // Default: assume support
}

// ValidateAPIKey validates the Cerebras API key by attempting to list models.
func (p *CerebrasProvider) ValidateAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.cerebras.ai/v1/models", nil)
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
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetCerebrasModelInfo returns detailed information about a specific Cerebras model.
// This is a helper function for testing and debugging.
func GetCerebrasModelInfo(modelID string) *ModelInfo {
	return &ModelInfo{
		ID:                  modelID,
		Provider:            "cerebras",
		Name:                getCerebrasDisplayName(modelID),
		Description:         getCerebrasDescription(modelID),
		ContextWindow:       getCerebrasContextWindow(modelID),
		MaxOutputTokens:     getCerebrasMaxOutputTokens(modelID),
		SupportsToolCalling: cerebrasSupportsToolCalling(modelID),
	}
}
