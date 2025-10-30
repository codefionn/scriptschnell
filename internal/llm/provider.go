package llm

import (
	"context"
)

// ModelInfo represents detailed information about an LLM model
type ModelInfo struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Provider            string   `json:"provider"`
	Description         string   `json:"description,omitempty"`
	ContextWindow       int      `json:"context_window,omitempty"`    // Input context window size
	MaxOutputTokens     int      `json:"max_output_tokens,omitempty"` // Maximum output tokens
	SupportsToolCalling bool     `json:"supports_tool_calling"`
	SupportsStreaming   bool     `json:"supports_streaming"`
	CreatedAt           string   `json:"created_at,omitempty"`
	OwnedBy             string   `json:"owned_by,omitempty"`
	Capabilities        []string `json:"capabilities,omitempty"`
}

// Provider is the interface for LLM providers
type Provider interface {
	// GetName returns the provider name (e.g., "openai", "anthropic")
	GetName() string

	// ListModels lists all available models from the provider
	// Returns models that support the current API version
	ListModels(ctx context.Context) ([]*ModelInfo, error)

	// CreateClient creates a new LLM client for the specified model
	CreateClient(modelID string) (Client, error)

	// ValidateAPIKey tests if the API key is valid
	ValidateAPIKey(ctx context.Context) error
}
