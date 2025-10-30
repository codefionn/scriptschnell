package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaProvider implements the Provider interface for local or remote Ollama instances.
type OllamaProvider struct {
	baseURL string
	client  *http.Client
}

// NewOllamaProvider creates a new Ollama provider.
// The apiKey parameter is reused as a base URL for compatibility with the provider manager.
func NewOllamaProvider(apiKey string) *OllamaProvider {
	return &OllamaProvider{
		baseURL: normalizeOllamaBaseURL(apiKey),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *OllamaProvider) GetName() string {
	return "ollama"
}

func (p *OllamaProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	endpoint := strings.TrimRight(p.baseURL, "/") + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create ollama request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return p.getFallbackModels(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return p.getFallbackModels(), nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return p.getFallbackModels(), nil
	}

	var tags ollamaTagsResponse
	if err := json.Unmarshal(data, &tags); err != nil {
		return p.getFallbackModels(), nil
	}

	if len(tags.Models) == 0 {
		return p.getFallbackModels(), nil
	}

	models := make([]*ModelInfo, 0, len(tags.Models))
	for _, model := range tags.Models {
		info := &ModelInfo{
			ID:                  strings.TrimSpace(model.Name),
			Name:                formatOllamaModelName(model.Name),
			Provider:            "ollama",
			Description:         buildOllamaDescription(model),
			SupportsToolCalling: true,
			SupportsStreaming:   true,
		}

		if model.Details != nil {
			if model.Details.ContextLength > 0 {
				info.ContextWindow = model.Details.ContextLength
			}
		}

		models = append(models, info)
	}

	return models, nil
}

func (p *OllamaProvider) CreateClient(modelID string) (Client, error) {
	return NewOllamaClient(p.baseURL, modelID)
}

func (p *OllamaProvider) ValidateAPIKey(ctx context.Context) error {
	endpoint := strings.TrimRight(p.baseURL, "/") + "/api/version"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create validation request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to contact ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama validation failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func (p *OllamaProvider) getFallbackModels() []*ModelInfo {
	// Provide a minimal, commonly available set of community models.
	fallback := []string{
		"llama3.1",
		"llama3.1:8b",
		"llama3.1:70b",
		"llama3.1:405b",
		"gemma2",
		"gemma2:9b",
		"gemma2:27b",
		"phi3",
		"phi3:3.8b",
		"mistral",
		"qwen2.5",
	}

	models := make([]*ModelInfo, 0, len(fallback))
	for _, id := range fallback {
		models = append(models, &ModelInfo{
			ID:                  id,
			Name:                formatOllamaModelName(id),
			Provider:            "ollama",
			Description:         "Ollama local model",
			SupportsToolCalling: true,
			SupportsStreaming:   true,
		})
	}

	return models
}

type ollamaTagsResponse struct {
	Models []ollamaTag `json:"models"`
}

type ollamaTag struct {
	Name    string            `json:"name"`
	Details *ollamaTagDetails `json:"details,omitempty"`
}

type ollamaTagDetails struct {
	Family            string   `json:"family"`
	Format            string   `json:"format"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
	ContextLength     int      `json:"context_length"`
}

func formatOllamaModelName(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "Ollama Model"
	}

	normalized := strings.ReplaceAll(trimmed, ":", " ")
	normalized = strings.ReplaceAll(normalized, "-", " ")
	parts := strings.Fields(normalized)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func buildOllamaDescription(model ollamaTag) string {
	if model.Details == nil {
		return "Local Ollama model"
	}

	segments := make([]string, 0, 4)
	if model.Details.Family != "" {
		segments = append(segments, fmt.Sprintf("Family %s", strings.ToUpper(model.Details.Family)))
	}
	if model.Details.ParameterSize != "" {
		segments = append(segments, fmt.Sprintf("%s parameters", model.Details.ParameterSize))
	}
	if model.Details.Format != "" {
		segments = append(segments, fmt.Sprintf("format %s", strings.ToUpper(model.Details.Format)))
	}
	if model.Details.QuantizationLevel != "" {
		segments = append(segments, fmt.Sprintf("quantization %s", strings.ToUpper(model.Details.QuantizationLevel)))
	}

	if len(segments) == 0 {
		return "Local Ollama model"
	}

	return strings.Join(segments, ", ")
}
