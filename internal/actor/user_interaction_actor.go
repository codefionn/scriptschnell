package actor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// UserInteractionActor coordinates user interactions across modes.
// It receives interaction requests and dispatches them to the mode-specific handler.
type UserInteractionActor struct {
	id              string
	handler         UserInteractionHandler
	pendingRequests map[string]*pendingInteractionRequest
	mu              sync.RWMutex

	// Configuration
	defaultTimeout time.Duration

	// Metrics
	totalRequests     int64
	timedOutRequests  int64
	cancelledRequests int64
}

type pendingInteractionRequest struct {
	request   *UserInteractionRequest
	createdAt time.Time
	timer     *time.Timer
}

// NewUserInteractionActor creates a new user interaction actor with the given handler.
func NewUserInteractionActor(id string, handler UserInteractionHandler) *UserInteractionActor {
	return &UserInteractionActor{
		id:              id,
		handler:         handler,
		pendingRequests: make(map[string]*pendingInteractionRequest),
		defaultTimeout:  2 * time.Minute,
	}
}

// ID returns the actor's unique identifier
func (a *UserInteractionActor) ID() string {
	return a.id
}

// Start initializes the actor
func (a *UserInteractionActor) Start(ctx context.Context) error {
	logger.Info("UserInteractionActor[%s] started with handler mode: %s", a.id, a.handler.Mode())
	return nil
}

// Stop gracefully shuts down the actor, cancelling any pending requests
func (a *UserInteractionActor) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	logger.Info("UserInteractionActor[%s] stopping, cancelling %d pending requests", a.id, len(a.pendingRequests))

	// Cancel all pending requests
	for requestID, pending := range a.pendingRequests {
		pending.timer.Stop()
		select {
		case pending.request.ResponseChan <- &UserInteractionResponse{
			RequestID: requestID,
			Cancelled: true,
			Error:     fmt.Errorf("actor stopped"),
		}:
		default:
			logger.Warn("UserInteractionActor[%s] could not send cancellation for %s", a.id, requestID)
		}
	}
	a.pendingRequests = make(map[string]*pendingInteractionRequest)

	return nil
}

// Receive processes incoming messages
func (a *UserInteractionActor) Receive(ctx context.Context, msg Message) error {
	switch m := msg.(type) {
	case *UserInteractionRequest:
		return a.handleRequest(ctx, m)
	case *UserInteractionAck:
		return a.handleAcknowledgment(m)
	case *UserInteractionCancel:
		return a.handleCancellation(m)
	case *HealthCheckRequest:
		return a.handleHealthCheck(m)
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

// handleRequest processes a user interaction request
func (a *UserInteractionActor) handleRequest(ctx context.Context, req *UserInteractionRequest) error {
	logger.Debug("UserInteractionActor[%s] received request %s type=%s", a.id, req.RequestID, req.InteractionType)

	// Check if handler supports this interaction type
	if !a.handler.SupportsInteraction(req.InteractionType) {
		logger.Debug("UserInteractionActor[%s] handler does not support %s", a.id, req.InteractionType)
		select {
		case req.ResponseChan <- &UserInteractionResponse{
			RequestID: req.RequestID,
			Error:     fmt.Errorf("handler mode '%s' does not support %s interactions", a.handler.Mode(), req.InteractionType),
		}:
		default:
		}
		return nil
	}

	// Set timeout
	timeout := req.Timeout
	if timeout == 0 {
		timeout = a.defaultTimeout
	}

	// Create timer for timeout
	timer := time.AfterFunc(timeout, func() {
		a.handleTimeout(req.RequestID)
	})

	// Store pending request
	a.mu.Lock()
	a.pendingRequests[req.RequestID] = &pendingInteractionRequest{
		request:   req,
		createdAt: time.Now(),
		timer:     timer,
	}
	atomic.AddInt64(&a.totalRequests, 1)
	a.mu.Unlock()

	// Dispatch to handler in goroutine (non-blocking)
	go func() {
		// Use the request's context for cancellation
		handlerCtx := req.RequestCtx
		if handlerCtx == nil {
			handlerCtx = ctx
		}

		resp, err := a.handler.HandleInteraction(handlerCtx, req)
		if err != nil {
			logger.Warn("UserInteractionActor[%s] handler error for %s: %v", a.id, req.RequestID, err)
			// Use handler's response if available, otherwise create new one
			if resp == nil {
				resp = &UserInteractionResponse{
					RequestID: req.RequestID,
				}
			}
			resp.RequestID = req.RequestID
			resp.Error = err
			a.completeRequest(req.RequestID, resp)
			return
		}

		resp.RequestID = req.RequestID
		a.completeRequest(req.RequestID, resp)
	}()

	return nil
}

// handleAcknowledgment processes an acknowledgment that a dialog was displayed
func (a *UserInteractionActor) handleAcknowledgment(ack *UserInteractionAck) error {
	a.mu.RLock()
	pending, exists := a.pendingRequests[ack.RequestID]
	a.mu.RUnlock()

	if !exists {
		logger.Debug("UserInteractionActor[%s] ack for unknown request %s", a.id, ack.RequestID)
		return nil
	}

	logger.Debug("UserInteractionActor[%s] request %s acknowledged (displayed)", a.id, ack.RequestID)
	_ = pending // Could update state to track acknowledgment
	return nil
}

// handleCancellation processes a request to cancel a pending interaction
func (a *UserInteractionActor) handleCancellation(cancel *UserInteractionCancel) error {
	a.mu.Lock()
	pending, exists := a.pendingRequests[cancel.RequestID]
	if !exists {
		a.mu.Unlock()
		logger.Debug("UserInteractionActor[%s] cancel for unknown request %s", a.id, cancel.RequestID)
		return nil
	}

	pending.timer.Stop()
	delete(a.pendingRequests, cancel.RequestID)
	atomic.AddInt64(&a.cancelledRequests, 1)
	a.mu.Unlock()

	logger.Debug("UserInteractionActor[%s] request %s cancelled: %s", a.id, cancel.RequestID, cancel.Reason)

	select {
	case pending.request.ResponseChan <- &UserInteractionResponse{
		RequestID: cancel.RequestID,
		Cancelled: true,
		Error:     fmt.Errorf("cancelled: %s", cancel.Reason),
	}:
	default:
		logger.Warn("UserInteractionActor[%s] could not send cancellation response for %s", a.id, cancel.RequestID)
	}

	return nil
}

// completeRequest sends a response for a pending request and cleans up
func (a *UserInteractionActor) completeRequest(requestID string, resp *UserInteractionResponse) {
	a.mu.Lock()
	pending, exists := a.pendingRequests[requestID]
	if !exists {
		a.mu.Unlock()
		logger.Debug("UserInteractionActor[%s] complete for unknown/expired request %s", a.id, requestID)
		return
	}

	// Stop timeout timer
	pending.timer.Stop()
	delete(a.pendingRequests, requestID)

	if resp.Cancelled {
		atomic.AddInt64(&a.cancelledRequests, 1)
	}
	a.mu.Unlock()

	logger.Debug("UserInteractionActor[%s] completing request %s (approved=%v, cancelled=%v, timedOut=%v)",
		a.id, requestID, resp.Approved, resp.Cancelled, resp.TimedOut)

	// Send response - try non-blocking first, then allow blocking if buffer is temporarily full
	// Note: We don't check context.Done() here because even if cancelled, we still want to send the response
	select {
	case pending.request.ResponseChan <- resp:
		logger.Debug("UserInteractionActor[%s] response sent for %s", a.id, requestID)
	default:
		logger.Warn("UserInteractionActor[%s] could not send response for %s: channel blocked", a.id, requestID)
	}
}

// handleTimeout is called when a request times out
func (a *UserInteractionActor) handleTimeout(requestID string) {
	a.mu.Lock()
	pending, exists := a.pendingRequests[requestID]
	if !exists {
		a.mu.Unlock()
		return
	}

	delete(a.pendingRequests, requestID)
	atomic.AddInt64(&a.timedOutRequests, 1)
	a.mu.Unlock()

	logger.Warn("UserInteractionActor[%s] request %s timed out after %v", a.id, requestID, a.defaultTimeout)

	select {
	case pending.request.ResponseChan <- &UserInteractionResponse{
		RequestID: requestID,
		TimedOut:  true,
		Error:     fmt.Errorf("user interaction timed out after %v", a.defaultTimeout),
	}:
	default:
		logger.Warn("UserInteractionActor[%s] could not send timeout response for %s", a.id, requestID)
	}
}

// handleHealthCheck responds to health check requests
func (a *UserInteractionActor) handleHealthCheck(req *HealthCheckRequest) error {
	a.mu.RLock()
	pendingCount := len(a.pendingRequests)
	a.mu.RUnlock()

	resp := HealthCheckResponse{
		Report: HealthReport{
			ActorID:   a.id,
			Status:    HealthStatusHealthy,
			Timestamp: time.Now(),
			Message: fmt.Sprintf("handler=%s pending=%d total=%d timedOut=%d cancelled=%d",
				a.handler.Mode(),
				pendingCount,
				atomic.LoadInt64(&a.totalRequests),
				atomic.LoadInt64(&a.timedOutRequests),
				atomic.LoadInt64(&a.cancelledRequests),
			),
		},
	}

	select {
	case req.ResponseChan <- resp:
	default:
	}

	return nil
}

// SetHandler updates the handler (useful for mode changes)
func (a *UserInteractionActor) SetHandler(handler UserInteractionHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handler = handler
	logger.Info("UserInteractionActor[%s] handler changed to mode: %s", a.id, handler.Mode())
}

// GetMetrics returns current metrics
func (a *UserInteractionActor) GetMetrics() map[string]int64 {
	return map[string]int64{
		"total_requests":     atomic.LoadInt64(&a.totalRequests),
		"timed_out_requests": atomic.LoadInt64(&a.timedOutRequests),
		"cancelled_requests": atomic.LoadInt64(&a.cancelledRequests),
	}
}
