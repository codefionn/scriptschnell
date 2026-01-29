package llm

import "context"

// cachingAwareClient wraps another Client and modifies caching behavior based on provider type.
// It disables EnableCaching for OpenAI-compatible providers to avoid errors with unsupported cache_control property.
type cachingAwareClient struct {
	delegate Client
	provider string
}

// NewCachingAwareClient returns a Client that adjusts caching behavior based on provider type.
// The provider parameter should be the provider name (e.g., "openai", "anthropic", "openai-compatible").
func NewCachingAwareClient(base Client, provider string) Client {
	if base == nil {
		return base
	}

	// Wrap the base client
	return &cachingAwareClient{
		delegate: base,
		provider: provider,
	}
}

func (c *cachingAwareClient) Complete(ctx context.Context, prompt string) (string, error) {
	return c.delegate.Complete(ctx, prompt)
}

func (c *cachingAwareClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// Disable caching for OpenAI-compatible providers
	if c.provider == "openai-compatible" && req.EnableCaching {
		// Clone the request to avoid modifying the original
		modifiedReq := *req

		// If there are messages, create a copy and remove CacheControl flags
		if req.Messages != nil {
			modifiedMessages := make([]*Message, len(req.Messages))
			copy(modifiedMessages, req.Messages)
			modifiedReq.Messages = modifiedMessages

			// Remove any CacheControl flags from messages
			for _, msg := range modifiedReq.Messages {
				if msg != nil {
					msg.CacheControl = false
				}
			}
		}

		// Disable caching in the request
		modifiedReq.EnableCaching = false

		return c.delegate.CompleteWithRequest(ctx, &modifiedReq)
	}

	return c.delegate.CompleteWithRequest(ctx, req)
}

func (c *cachingAwareClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	// Disable caching for OpenAI-compatible providers
	if c.provider == "openai-compatible" && req.EnableCaching {
		// Create a modified request
		modifiedReq := *req

		// If there are messages, create a copy and remove CacheControl flags
		if req.Messages != nil {
			modifiedMessages := make([]*Message, len(req.Messages))
			copy(modifiedMessages, req.Messages)
			modifiedReq.Messages = modifiedMessages

			// Remove any CacheControl flags from messages
			for _, msg := range modifiedReq.Messages {
				if msg != nil {
					msg.CacheControl = false
				}
			}
		}

		// Disable caching in the request
		modifiedReq.EnableCaching = false

		return c.delegate.Stream(ctx, &modifiedReq, callback)
	}

	return c.delegate.Stream(ctx, req, callback)
}

func (c *cachingAwareClient) GetModelName() string {
	return c.delegate.GetModelName()
}

func (c *cachingAwareClient) GetLastResponseID() string {
	return c.delegate.GetLastResponseID()
}

func (c *cachingAwareClient) SetPreviousResponseID(responseID string) {
	c.delegate.SetPreviousResponseID(responseID)
}
