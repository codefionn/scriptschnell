package actor

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockHandler is a test implementation of UserInteractionHandler
type mockHandler struct {
	mode            string
	supportedTypes  map[InteractionType]bool
	handleFunc      func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error)
	handleCallCount int
	mu              sync.Mutex
}

func newMockHandler(mode string) *mockHandler {
	return &mockHandler{
		mode: mode,
		supportedTypes: map[InteractionType]bool{
			InteractionTypeAuthorization:     true,
			InteractionTypePlanningQuestion:  true,
			InteractionTypeUserInputSingle:   true,
			InteractionTypeUserInputMultiple: true,
		},
	}
}

func (h *mockHandler) Mode() string {
	return h.mode
}

func (h *mockHandler) SupportsInteraction(t InteractionType) bool {
	return h.supportedTypes[t]
}

func (h *mockHandler) HandleInteraction(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
	h.mu.Lock()
	h.handleCallCount++
	h.mu.Unlock()

	if h.handleFunc != nil {
		return h.handleFunc(ctx, req)
	}

	// Default: approve authorization, answer questions
	return &UserInteractionResponse{
		RequestID:    req.RequestID,
		Approved:     true,
		Answer:       "test answer",
		Acknowledged: true,
	}, nil
}

func (h *mockHandler) getCallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handleCallCount
}

func TestUserInteractionActorBasic(t *testing.T) {
	handler := newMockHandler("test")
	actor := NewUserInteractionActor("test-actor", handler)

	if actor.ID() != "test-actor" {
		t.Errorf("Expected ID 'test-actor', got '%s'", actor.ID())
	}

	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := actor.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestUserInteractionActorAuthorizationApproved(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     true,
			Acknowledged: true,
		}, nil
	}

	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	responseChan := make(chan *UserInteractionResponse, 1)
	req := &UserInteractionRequest{
		RequestID:       "auth-1",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName: "shell",
			Reason:   "test reason",
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	select {
	case resp := <-responseChan:
		if !resp.Approved {
			t.Error("Expected Approved to be true")
		}
		if resp.RequestID != "auth-1" {
			t.Errorf("Expected RequestID 'auth-1', got '%s'", resp.RequestID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestUserInteractionActorAuthorizationDenied(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     false,
			Acknowledged: true,
		}, nil
	}

	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	responseChan := make(chan *UserInteractionResponse, 1)
	req := &UserInteractionRequest{
		RequestID:       "auth-2",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName: "shell",
			Reason:   "test reason",
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Approved {
			t.Error("Expected Approved to be false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestUserInteractionActorTimeout(t *testing.T) {
	handler := newMockHandler("test")
	// Handler that never responds
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		// Block until context is cancelled
		<-ctx.Done()
		return &UserInteractionResponse{
			RequestID: req.RequestID,
			Cancelled: true,
		}, ctx.Err()
	}

	actor := NewUserInteractionActor("test-actor", handler)
	actor.defaultTimeout = 100 * time.Millisecond // Short timeout for test

	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	responseChan := make(chan *UserInteractionResponse, 1)
	req := &UserInteractionRequest{
		RequestID:       "timeout-test",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName: "shell",
			Reason:   "test",
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
		Timeout:      50 * time.Millisecond, // Even shorter
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	select {
	case resp := <-responseChan:
		if !resp.TimedOut {
			t.Error("Expected TimedOut to be true")
		}
		if resp.Error == nil {
			t.Error("Expected error for timeout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for timeout response")
	}
}

func TestUserInteractionActorCancellation(t *testing.T) {
	handler := newMockHandler("test")
	started := make(chan struct{})
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		close(started)
		// Wait for cancellation
		<-ctx.Done()
		return &UserInteractionResponse{
			RequestID: req.RequestID,
			Cancelled: true,
		}, ctx.Err()
	}

	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	reqCtx, cancel := context.WithCancel(ctx)
	responseChan := make(chan *UserInteractionResponse, 1)
	req := &UserInteractionRequest{
		RequestID:       "cancel-test",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName: "shell",
			Reason:   "test",
		},
		RequestCtx:   reqCtx,
		ResponseChan: responseChan,
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	// Wait for handler to start
	<-started

	// Cancel the request
	cancel()

	select {
	case resp := <-responseChan:
		if !resp.Cancelled {
			t.Error("Expected Cancelled to be true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for cancellation response")
	}
}

func TestUserInteractionActorUnsupportedType(t *testing.T) {
	handler := newMockHandler("test")
	handler.supportedTypes = map[InteractionType]bool{
		InteractionTypeAuthorization: false, // Not supported
	}

	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	responseChan := make(chan *UserInteractionResponse, 1)
	req := &UserInteractionRequest{
		RequestID:       "unsupported-test",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName: "shell",
			Reason:   "test",
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Error == nil {
			t.Error("Expected error for unsupported type")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

func TestUserInteractionActorConcurrentRequests(t *testing.T) {
	handler := newMockHandler("test")
	var callOrder []string
	var orderMu sync.Mutex

	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		orderMu.Lock()
		callOrder = append(callOrder, req.RequestID)
		orderMu.Unlock()

		// Small delay to allow concurrent processing
		time.Sleep(10 * time.Millisecond)

		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     true,
			Acknowledged: true,
		}, nil
	}

	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	// Send multiple requests concurrently
	numRequests := 5
	responseChans := make([]chan *UserInteractionResponse, numRequests)
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		responseChans[i] = make(chan *UserInteractionResponse, 1)
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()
			req := &UserInteractionRequest{
				RequestID:       "concurrent-" + string(rune('0'+idx)),
				InteractionType: InteractionTypeAuthorization,
				Payload: &AuthorizationPayload{
					ToolName: "shell",
					Reason:   "test",
				},
				RequestCtx:   ctx,
				ResponseChan: responseChans[idx],
			}
			if err := actor.Receive(ctx, req); err != nil {
				t.Errorf("Failed to receive request: %v", err)
			}
		}(i)
	}

	// Wait for all requests to be sent
	wg.Wait()

	// Collect all responses
	for i := 0; i < numRequests; i++ {
		select {
		case resp := <-responseChans[i]:
			if !resp.Approved {
				t.Errorf("Request %d: expected Approved", i)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Request %d: timeout waiting for response", i)
		}
	}

	// Verify all requests were handled
	if handler.getCallCount() != numRequests {
		t.Errorf("Expected %d handler calls, got %d", numRequests, handler.getCallCount())
	}
}

func TestUserInteractionActorHealthCheck(t *testing.T) {
	handler := newMockHandler("test-mode")
	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	responseChan := make(chan HealthCheckResponse, 1)
	req := &HealthCheckRequest{
		ResponseChan: responseChan,
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Report.ActorID != "test-actor" {
			t.Errorf("Expected ActorID 'test-actor', got '%s'", resp.Report.ActorID)
		}
		if resp.Report.Status != HealthStatusHealthy {
			t.Errorf("Expected status Healthy, got '%s'", resp.Report.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for health check response")
	}
}

func TestUserInteractionActorStopCancelsPending(t *testing.T) {
	handler := newMockHandler("test")
	blockChan := make(chan struct{})
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		<-blockChan // Block forever
		return nil, nil
	}

	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}

	responseChan := make(chan *UserInteractionResponse, 1)
	req := &UserInteractionRequest{
		RequestID:       "stop-test",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName: "shell",
			Reason:   "test",
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Errorf("Failed to receive request: %v", err)
	}

	// Small delay to ensure request is pending
	time.Sleep(10 * time.Millisecond)

	// Stop the actor
	if err := actor.Stop(ctx); err != nil {
		t.Errorf("Failed to stop actor: %v", err)
	}

	select {
	case resp := <-responseChan:
		if !resp.Cancelled {
			t.Error("Expected Cancelled to be true after Stop")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for cancellation response after Stop")
	}
}

func TestUserInteractionActorMetrics(t *testing.T) {
	handler := newMockHandler("test")
	actor := NewUserInteractionActor("test-actor", handler)
	ctx := context.Background()
	if err := actor.Start(ctx); err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}
	defer func() {
		if err := actor.Stop(ctx); err != nil {
			t.Errorf("Failed to stop actor: %v", err)
		}
	}()

	// Send a request
	responseChan := make(chan *UserInteractionResponse, 1)
	req := &UserInteractionRequest{
		RequestID:       "metrics-test",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName: "shell",
			Reason:   "test",
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
	}

	if err := actor.Receive(ctx, req); err != nil {
		t.Errorf("Failed to receive request: %v", err)
	}
	<-responseChan

	metrics := actor.GetMetrics()
	if metrics["total_requests"] != 1 {
		t.Errorf("Expected total_requests=1, got %d", metrics["total_requests"])
	}
}
