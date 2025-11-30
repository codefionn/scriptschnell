package llm

import (
	"context"
)

// Message represents a chat message
type Message struct {
	Role      string                   `json:"role"`
	Content   string                   `json:"content"`
	ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolID    string                   `json:"tool_id,omitempty"`
	ToolName  string                   `json:"tool_name,omitempty"` // Name of the tool for tool responses
}

// CompletionRequest represents a completion request
type CompletionRequest struct {
	Messages       []*Message               `json:"messages"`
	Tools          []map[string]interface{} `json:"tools,omitempty"`
	Temperature    float64                  `json:"temperature"`
	MaxTokens      int                      `json:"max_tokens,omitempty"`
	SystemPrompt   string                   `json:"system_prompt,omitempty"`
	EnableCaching  bool                     `json:"enable_caching,omitempty"`   // Enable prompt caching (Anthropic, OpenAI, OpenRouter)
	CacheTTL       string                   `json:"cache_ttl,omitempty"`        // Cache TTL: "5m" or "1h" (Anthropic only, others use provider defaults)
}

// CompletionResponse represents a completion response
type CompletionResponse struct {
	Content    string                   `json:"content"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	StopReason string                   `json:"stop_reason"`
}

// Client is the interface for LLM clients
type Client interface {
	// Complete sends a completion request and returns the response
	CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	// Complete is a simplified version for single prompt
	Complete(ctx context.Context, prompt string) (string, error)
	// Stream sends a streaming completion request
	Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error
	// GetModelName returns the model name
	GetModelName() string
}

// Model represents an LLM model
type Model struct {
	Provider string `json:"provider"` // openai, anthropic, etc.
	Name     string `json:"name"`
	ID       string `json:"id"`
}
