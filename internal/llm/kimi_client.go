package llm

import (
	"context"
	"fmt"
	"strings"
)

// KimiClient implements the Client interface for Kimi (Moonshot AI)
// Kimi's API is OpenAI-compatible, so we wrap the OpenAICompatibleClient
type KimiClient struct {
	client *OpenAICompatibleClient
	model  string
}

// NewKimiClient creates a new Kimi client
func NewKimiClient(apiKey, model string) (*KimiClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for Kimi provider")
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("model name is required for Kimi provider")
	}

	// Kimi uses the OpenAI-compatible API at https://api.moonshot.ai/v1
	client, err := NewOpenAICompatibleClient(apiKey, "https://api.moonshot.ai/v1", model)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kimi client: %w", err)
	}

	return &KimiClient{
		client: client,
		model:  model,
	}, nil
}

func (c *KimiClient) GetModelName() string {
	return c.model
}

func (c *KimiClient) GetLastResponseID() string {
	return "" // Not applicable for Kimi
}

func (c *KimiClient) SetPreviousResponseID(responseID string) {
	// Not applicable for Kimi
}

func (c *KimiClient) Complete(ctx context.Context, prompt string) (string, error) {
	return c.client.Complete(ctx, prompt)
}

func (c *KimiClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// Kimi supports standard parameters, but some models have restrictions
	// We can pass through the request directly to the OpenAI-compatible client
	return c.client.CompleteWithRequest(ctx, req)
}

func (c *KimiClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	return c.client.Stream(ctx, req, callback)
}
