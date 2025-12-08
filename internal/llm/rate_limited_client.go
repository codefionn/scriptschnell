package llm

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	defaultResponseTokenEstimate = 512
	minTokenEstimate             = 8
)

// rateLimitedClient wraps another Client and enforces request and token-based throttling.
type rateLimitedClient struct {
	delegate     Client
	interval     time.Duration
	mu           sync.Mutex
	nextAllowed  time.Time
	tokenMu      sync.Mutex
	nextToken    time.Time
	tokensPerMin int
}

// NewRateLimitedClient returns a Client that throttles calls using QPS and token budgets.
func NewRateLimitedClient(base Client, interval time.Duration, tokensPerMinute int) Client {
	if base == nil {
		return base
	}
	if interval <= 0 && tokensPerMinute <= 0 {
		return base
	}
	client := &rateLimitedClient{
		delegate: base,
		interval: interval,
	}
	if tokensPerMinute > 0 {
		client.tokensPerMin = tokensPerMinute
	}
	return client
}

func (c *rateLimitedClient) wait(ctx context.Context, tokens int) error {
	if err := c.waitInterval(ctx); err != nil {
		return err
	}
	if err := c.waitTokens(ctx, tokens); err != nil {
		return err
	}
	return nil
}

func (c *rateLimitedClient) waitInterval(ctx context.Context) error {
	if c.interval <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		c.mu.Lock()
		now := time.Now()
		if c.nextAllowed.IsZero() || !now.Before(c.nextAllowed) {
			c.nextAllowed = now.Add(c.interval)
			c.mu.Unlock()
			return nil
		}

		wait := time.Until(c.nextAllowed)
		c.mu.Unlock()

		if wait <= 0 {
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *rateLimitedClient) waitTokens(ctx context.Context, tokens int) error {
	if c.tokensPerMin <= 0 || tokens <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	delay := tokensToDuration(tokens, c.tokensPerMin)

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	start := time.Now()
	if c.nextToken.Before(start) {
		c.nextToken = start
	}
	waitUntil := c.nextToken
	c.nextToken = c.nextToken.Add(delay)

	if waitUntil.After(start) {
		timer := time.NewTimer(waitUntil.Sub(start))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (c *rateLimitedClient) Complete(ctx context.Context, prompt string) (string, error) {
	tokens := estimateTokensForPrompt(prompt)
	if err := c.wait(ctx, tokens); err != nil {
		return "", err
	}
	return c.delegate.Complete(ctx, prompt)
}

func (c *rateLimitedClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	tokens := estimateTokensForRequest(req)
	if err := c.wait(ctx, tokens); err != nil {
		return nil, err
	}
	return c.delegate.CompleteWithRequest(ctx, req)
}

func (c *rateLimitedClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	tokens := estimateTokensForRequest(req)
	if err := c.wait(ctx, tokens); err != nil {
		return err
	}
	return c.delegate.Stream(ctx, req, callback)
}

func (c *rateLimitedClient) GetModelName() string {
	return c.delegate.GetModelName()
}

func estimateTokensForPrompt(prompt string) int {
	chars := len(prompt)
	estimated := charsToTokens(chars)
	if estimated < minTokenEstimate {
		estimated = minTokenEstimate
	}
	return estimated + defaultResponseTokenEstimate
}

func estimateTokensForRequest(req *CompletionRequest) int {
	if req == nil {
		return defaultResponseTokenEstimate
	}

	toolTokens, totalTokens := estimateTokensFromMessages(req.Messages)
	tokens := toolTokens
	if tokens == 0 {
		tokens = totalTokens
	}
	if tokens <= 0 {
		tokens = minTokenEstimate
	}

	if req.MaxTokens > 0 {
		tokens += req.MaxTokens
	} else {
		tokens += defaultResponseTokenEstimate
	}
	return tokens
}

func estimateTokensFromMessages(messages []*Message) (toolTokens, totalTokens int) {
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		tokenEstimate := EstimateTokenCount(msg.Content)
		totalTokens += tokenEstimate
		if strings.EqualFold(msg.Role, "tool") {
			// Tool responses dominate the downstream prompt size.
			toolTokens += tokenEstimate
		}
	}
	return toolTokens, totalTokens
}

func tokensToDuration(tokens, tokensPerMinute int) time.Duration {
	if tokensPerMinute <= 0 || tokens <= 0 {
		return 0
	}
	return time.Duration(float64(time.Minute) * float64(tokens) / float64(tokensPerMinute))
}
