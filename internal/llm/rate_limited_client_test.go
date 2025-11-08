package llm

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClient struct {
	mu    sync.Mutex
	calls []time.Time
}

func (f *fakeClient) recordCall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, time.Now())
}

func (f *fakeClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	f.recordCall()
	return &CompletionResponse{Content: "ok"}, nil
}

func (f *fakeClient) Complete(ctx context.Context, prompt string) (string, error) {
	f.recordCall()
	return "ok", nil
}

func (f *fakeClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	f.recordCall()
	return nil
}

func (f *fakeClient) GetModelName() string {
	return "fake"
}

func (f *fakeClient) callTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]time.Time, len(f.calls))
	copy(cp, f.calls)
	return cp
}

func TestRateLimitedClientEnforcesInterval(t *testing.T) {
	base := &fakeClient{}
	const interval = 50 * time.Millisecond
	client := NewRateLimitedClient(base, interval, 0)

	ctx := context.Background()
	if _, err := client.Complete(ctx, "first"); err != nil {
		t.Fatalf("first completion failed: %v", err)
	}
	if _, err := client.Complete(ctx, "second"); err != nil {
		t.Fatalf("second completion failed: %v", err)
	}

	times := base.callTimes()
	if len(times) != 2 {
		t.Fatalf("expected 2 calls recorded, got %d", len(times))
	}

	diff := times[1].Sub(times[0])
	if diff+5*time.Millisecond < interval {
		t.Fatalf("expected delay of at least %v (got %v)", interval, diff)
	}
}

func TestRateLimitedClientPassthroughWhenDisabled(t *testing.T) {
	base := &fakeClient{}
	client := NewRateLimitedClient(base, 0, 0)

	if client != base {
		t.Fatalf("expected rate limiter to return base client when disabled")
	}
}

func TestRateLimitedClientEnforcesTokenBudget(t *testing.T) {
	base := &fakeClient{}
	const tokensPerMinute = 120 // 2 tokens/sec
	client := NewRateLimitedClient(base, 0, tokensPerMinute)

	req := &CompletionRequest{
		Messages: []*Message{
			{Role: "tool", Content: strings.Repeat("a", 400)}, // ~100 tokens
		},
		MaxTokens: 32,
	}

	if _, err := client.CompleteWithRequest(context.Background(), req); err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := client.CompleteWithRequest(timeoutCtx, req); err == nil {
		t.Fatalf("expected second request to be throttled and respect context timeout")
	}

	if got := len(base.callTimes()); got != 1 {
		t.Fatalf("expected only first request to reach delegate, got %d", got)
	}
}
