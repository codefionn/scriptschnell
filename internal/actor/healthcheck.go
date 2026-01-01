package actor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// HealthStatus represents the health status of an actor
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthMetrics contains health-related metrics for an actor
type HealthMetrics struct {
	// Message queue metrics
	MailboxDepth    int     `json:"mailbox_depth"`
	MailboxCapacity int     `json:"mailbox_capacity"`
	MailboxUsage    float64 `json:"mailbox_usage"` // percentage

	// Activity metrics
	LastActivityTime time.Time     `json:"last_activity_time"`
	StartTime        time.Time     `json:"start_time"`
	Uptime           time.Duration `json:"uptime"`

	// Error metrics
	ErrorCount   int64     `json:"error_count"`
	LastError    time.Time `json:"last_error,omitempty"`
	LastErrorMsg string    `json:"last_error_msg,omitempty"`

	// Actor-specific metrics
	CustomMetrics interface{} `json:"custom_metrics,omitempty"`
}

// HealthReport contains the complete health assessment of an actor
type HealthReport struct {
	ActorID   string        `json:"actor_id"`
	Status    HealthStatus  `json:"status"`
	Metrics   HealthMetrics `json:"metrics"`
	Message   string        `json:"message"` // Human-readable description
	Timestamp time.Time     `json:"timestamp"`
}

// HealthCheckRequest is a message to request health check of an actor
type HealthCheckRequest struct {
	ResponseChan chan HealthCheckResponse
}

func (HealthCheckRequest) Type() string {
	return "HealthCheckRequest"
}

// HealthCheckResponse contains the health assessment of an actor
type HealthCheckResponse struct {
	Report HealthReport
	Error  error
}

// HealthCheckActor extends the Actor interface with health check capabilities
type HealthCheckActor interface {
	Actor

	// GetHealthMetrics returns current health metrics
	GetHealthMetrics() HealthMetrics

	// IsHealthy returns true if the actor is considered healthy
	IsHealthy() bool
}

// HealthCheckable provides a default health check implementation for actors
type HealthCheckable struct {
	id              string
	mu              sync.RWMutex
	mailbox         chan Message
	startTime       time.Time
	lastActivity    time.Time
	errorCount      int64
	lastError       time.Time
	lastErrorMsg    string
	metricsProvider func() interface{} // optional custom metrics provider
}

// NewHealthCheckable creates a new health checkable component
func NewHealthCheckable(id string, mailbox chan Message, metricsProvider func() interface{}) *HealthCheckable {
	return &HealthCheckable{
		id:              id,
		mailbox:         mailbox,
		startTime:       time.Now(),
		lastActivity:    time.Now(),
		metricsProvider: metricsProvider,
	}
}

// GetHealthMetrics returns current health metrics
func (h *HealthCheckable) GetHealthMetrics() HealthMetrics {
	h.mu.RLock()
	defer h.mu.RUnlock()

	mailboxDepth := len(h.mailbox)
	mailboxCapacity := cap(h.mailbox)
	var mailboxUsage float64
	if mailboxCapacity > 0 {
		mailboxUsage = float64(mailboxDepth) / float64(mailboxCapacity) * 100
	}

	customMetrics := interface{}(nil)
	if h.metricsProvider != nil {
		customMetrics = h.metricsProvider()
	}

	return HealthMetrics{
		MailboxDepth:     mailboxDepth,
		MailboxCapacity:  mailboxCapacity,
		MailboxUsage:     mailboxUsage,
		LastActivityTime: h.lastActivity,
		StartTime:        h.startTime,
		Uptime:           time.Since(h.startTime),
		ErrorCount:       h.errorCount,
		LastError:        h.lastError,
		LastErrorMsg:     h.lastErrorMsg,
		CustomMetrics:    customMetrics,
	}
}

// RecordActivity updates the last activity timestamp
func (h *HealthCheckable) RecordActivity() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastActivity = time.Now()
}

// RecordError records an error occurrence
func (h *HealthCheckable) RecordError(err error) {
	if err == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.errorCount++
	h.lastError = time.Now()
	h.lastErrorMsg = err.Error()
}

// IsHealthy returns true if the actor is considered healthy
func (h *HealthCheckable) IsHealthy() bool {
	metrics := h.GetHealthMetrics()

	// Default health criteria
	unhealthyConditions := 0

	// Check for high mailbox usage (>90%)
	if metrics.MailboxUsage > 90 {
		unhealthyConditions++
	}

	// Check for recent errors (last 5 minutes)
	if !metrics.LastError.IsZero() && time.Since(metrics.LastError) < 5*time.Minute && metrics.ErrorCount > 0 {
		unhealthyConditions++
	}

	// Check for lack of meaningful activity (over 1 hour old, with some exceptions)
	// We only consider no activity unhealthy if the actor has been running for at least 30 minutes
	if metrics.Uptime > 30*time.Minute && time.Since(metrics.LastActivityTime) > time.Hour {
		unhealthyConditions++
	}

	// Actor is healthy if no unhealthy conditions
	return unhealthyConditions == 0
}

// GenerateHealthReport creates a complete health report
func (h *HealthCheckable) GenerateHealthReport() HealthReport {
	metrics := h.GetHealthMetrics()

	var status HealthStatus
	var message string

	if h.IsHealthy() {
		status = HealthStatusHealthy
		message = "Actor is operating normally"
	} else {
		// Determine specific issues for degraded/unhealthy status
		issues := make([]string, 0)

		if metrics.MailboxUsage > 90 {
			issues = append(issues, fmt.Sprintf("high mailbox usage (%.1f%%)", metrics.MailboxUsage))
		}

		if time.Since(metrics.LastError) < 5*time.Minute && metrics.ErrorCount > 0 {
			issues = append(issues, fmt.Sprintf("recent errors (%d in last 5min)", metrics.ErrorCount))
		}

		if time.Since(metrics.LastActivityTime) > time.Hour {
			issues = append(issues, "no recent activity")
		}

		if len(issues) > 2 {
			status = HealthStatusUnhealthy
			message = fmt.Sprintf("Actor has multiple issues: %v", issues)
		} else {
			status = HealthStatusDegraded
			message = fmt.Sprintf("Actor has performance concerns: %v", issues)
		}
	}

	return HealthReport{
		ActorID:   h.id,
		Status:    status,
		Metrics:   metrics,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// HealthCheckHandler processes health check requests
func (h *HealthCheckable) HealthCheckHandler(ctx context.Context, msg Message) error {
	switch m := msg.(type) {
	case HealthCheckRequest:
		response := HealthCheckResponse{
			Report: h.GenerateHealthReport(),
		}

		select {
		case m.ResponseChan <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		return fmt.Errorf("unsupported message type: %T", msg)
	}

	return nil
}
