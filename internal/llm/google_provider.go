package llm

import (
	"context"
	"fmt"
	"strings"

	genai "google.golang.org/genai"
)

// GoogleProvider implements the Provider interface using the official Google GenAI SDK.
type GoogleProvider struct {
	apiKey string
}

// NewGoogleProvider creates a new Google provider
func NewGoogleProvider(apiKey string) *GoogleProvider {
	return &GoogleProvider{apiKey: apiKey}
}

func (p *GoogleProvider) GetName() string {
	return "google"
}

func (p *GoogleProvider) ListModels(ctx context.Context) ([]*ModelInfo, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  p.apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create google genai client: %w", err)
	}

	models := make([]*ModelInfo, 0)
	for model, err := range client.Models.All(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate google models: %w", err)
		}
		if model == nil || model.Name == "" {
			continue
		}

		supported := append([]string(nil), model.SupportedActions...)

		info := &ModelInfo{
			ID:                  model.Name,
			Name:                googleModelDisplayName(model),
			Provider:            "google",
			Description:         model.Description,
			ContextWindow:       int(model.InputTokenLimit),
			MaxOutputTokens:     int(model.OutputTokenLimit),
			SupportsToolCalling: googleSupportsToolCalling(supported),
			SupportsStreaming:   googleSupportsStreaming(supported),
			OwnedBy:             "google",
			Capabilities:        supported,
		}

		models = append(models, info)
	}

	return models, nil
}

func (p *GoogleProvider) CreateClient(modelID string) (Client, error) {
	return NewGoogleAIClient(p.apiKey, modelID)
}

func (p *GoogleProvider) ValidateAPIKey(ctx context.Context) error {
	_, err := p.ListModels(ctx)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "api key") ||
			strings.Contains(strings.ToLower(err.Error()), "permission") ||
			strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
			return fmt.Errorf("invalid API key")
		}
		return err
	}
	return nil
}

func googleModelDisplayName(model *genai.Model) string {
	if model == nil {
		return ""
	}
	if model.DisplayName != "" {
		return model.DisplayName
	}
	return model.Name
}

func googleSupportsStreaming(methods []string) bool {
	for _, method := range methods {
		switch normalizeGoogleCapabilityName(method) {
		case "streamgeneratecontent":
			return true
		}
	}
	// Default to true when generateContent is available since streaming is typically supported alongside it
	for _, method := range methods {
		switch normalizeGoogleCapabilityName(method) {
		case "generatecontent":
			return true
		}
	}
	return false
}

func googleSupportsToolCalling(methods []string) bool {
	for _, method := range methods {
		switch normalizeGoogleCapabilityName(method) {
		case "functioncall", "tooluse":
			return true
		}
	}
	return false
}

func normalizeGoogleCapabilityName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	return normalized
}
