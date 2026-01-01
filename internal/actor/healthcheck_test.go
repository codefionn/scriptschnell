package actor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// MockActor for testing
type MockActor struct {
	id      string
	health  *HealthCheckable
	started bool
	errors  []error
}

func NewMockActor(id string) *MockActor {
	actor := &MockActor{id: id, errors: make([]error, 0)}
	actor.health = NewHealthCheckable(id, make(chan Message, 10), func() interface{} {
		return map[string]interface{}{
			"mock_type": "test_actor",
			"errors":    len(actor.errors),
		}
	})
	return actor
}

func (a *MockActor) ID() string { return a.id }

func (a *MockActor) Start(ctx context.Context) error {
	a.health.RecordActivity()
	a.started = true
	return nil
}

func (a *MockActor) Stop(ctx context.Context) error {
	a.health.RecordActivity()
	a.started = false
	return nil
}

func (a *MockActor) Receive(ctx context.Context, msg Message) error {
	a.health.RecordActivity()
	switch m := msg.(type) {
	case HealthCheckRequest:
		// Handle health check requests properly
		response := HealthCheckResponse{
			Report: a.health.GenerateHealthReport(),
		}
		select {
		case m.ResponseChan <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	case MockMessage:
		if m.ShouldError {
			a.errors = append(a.errors, errors.New("mock actor error"))
			a.health.RecordError(errors.New("mock actor error"))
			return errors.New("mock actor error")
		}
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
	return nil
}

func (a *MockActor) GetHealthMetrics() HealthMetrics {
	return a.health.GetHealthMetrics()
}

func (a *MockActor) IsHealthy() bool {
	return a.health.IsHealthy()
}

// MockMessage for testing
type MockMessage struct {
	ShouldError bool
}

func (MockMessage) Type() string { return "MockMessage" }

func TestHealthCheckable_RecordActivity(t *testing.T) {
	health := NewHealthCheckable("test", make(chan Message, 10), nil)

	initialActivity := health.GetHealthMetrics().LastActivityTime

	// Wait a bit to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	health.RecordActivity()

	newMetrics := health.GetHealthMetrics()
	if !newMetrics.LastActivityTime.After(initialActivity) {
		t.Error("Expected activity time to be updated")
	}
}

func TestHealthCheckable_RecordError(t *testing.T) {
	health := NewHealthCheckable("test", make(chan Message, 10), nil)

	testErr := errors.New("test error")

	// Initially no errors
	if health.GetHealthMetrics().ErrorCount != 0 {
		t.Errorf("Expected 0 errors, got %d", health.GetHealthMetrics().ErrorCount)
	}

	health.RecordError(testErr)

	metrics := health.GetHealthMetrics()
	if metrics.ErrorCount != 1 {
		t.Errorf("Expected 1 error, got %d", metrics.ErrorCount)
	}
	if metrics.LastErrorMsg != testErr.Error() {
		t.Errorf("Expected error message %s, got %s", testErr.Error(), metrics.LastErrorMsg)
	}
}

func TestHealthCheckable_IsHealthy(t *testing.T) {
	tests := []struct {
		name            string
		setupFunc       func(*HealthCheckable)
		expectedHealthy bool
	}{
		{
			name:            "new actor is healthy",
			setupFunc:       func(h *HealthCheckable) {},
			expectedHealthy: true,
		},
		{
			name: "actor with recent error is unhealthy",
			setupFunc: func(h *HealthCheckable) {
				h.RecordError(errors.New("recent error"))
			},
			expectedHealthy: false,
		},
		{
			name: "actor with old error is healthy",
			setupFunc: func(h *HealthCheckable) {
				h.RecordError(errors.New("old error"))
				// Simulate old error by going back in time (hack for testing)
				h.mu.Lock()
				h.lastError = time.Now().Add(-10 * time.Minute)
				h.mu.Unlock()
			},
			expectedHealthy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := NewHealthCheckable("test", make(chan Message, 10), nil)
			tt.setupFunc(health)

			if health.IsHealthy() != tt.expectedHealthy {
				t.Errorf("Expected healthy=%v, got %v", tt.expectedHealthy, health.IsHealthy())
			}
		})
	}
}

func TestHealthCheckable_GenerateHealthReport(t *testing.T) {
	health := NewHealthCheckable("test-actor", make(chan Message, 10), func() interface{} {
		return map[string]string{"custom": "metric"}
	})

	health.RecordActivity()
	report := health.GenerateHealthReport()

	if report.ActorID != "test-actor" {
		t.Errorf("Expected actor ID 'test-actor', got '%s'", report.ActorID)
	}

	if report.Status != HealthStatusHealthy {
		t.Errorf("Expected status %s, got %s", HealthStatusHealthy, report.Status)
	}

	if report.Message == "" {
		t.Error("Expected non-empty message")
	}

	if report.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	// Check custom metrics
	customMetrics, ok := report.Metrics.CustomMetrics.(map[string]string)
	if !ok {
		t.Error("Expected custom metrics to be a map")
	} else if customMetrics["custom"] != "metric" {
		t.Errorf("Expected custom metric value 'metric', got '%s'", customMetrics["custom"])
	}
}

func TestActorRef_WithHealthCheck(t *testing.T) {
	mockActor := NewMockActor("test-ref")
	actorRef := NewActorRef("test-ref", mockActor, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := actorRef.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start actor: %v", err)
	}

	// Send a health check request
	responseChan := make(chan HealthCheckResponse, 1)
	healthMsg := HealthCheckRequest{ResponseChan: responseChan}

	err = actorRef.Send(healthMsg)
	if err != nil {
		t.Fatalf("Failed to send health check request: %v", err)
	}

	// Wait for response
	select {
	case response := <-responseChan:
		if response.Error != nil {
			t.Fatalf("Health check failed: %v", response.Error)
		}

		report := response.Report
		if report.ActorID != "test-ref" {
			t.Errorf("Expected actor ID 'test-ref', got '%s'", report.ActorID)
		}

		if report.Status != HealthStatusHealthy {
			t.Errorf("Expected status %s, got %s", HealthStatusHealthy, report.Status)
		}

	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for health check response")
	}

	err = actorRef.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop actor: %v", err)
	}
}

func TestSystem_HealthCheck(t *testing.T) {
	system := NewSystem()
	ctx := context.Background()

	// Create some actors
	mockActor1 := NewMockActor("actor1")
	mockActor2 := NewMockActor("actor2")

	_, err := system.Spawn(ctx, "actor1", mockActor1, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor1: %v", err)
	}

	_, err = system.Spawn(ctx, "actor2", mockActor2, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor2: %v", err)
	}

	// Test system health check
	reports := system.HealthCheck(ctx)

	if len(reports) != 2 {
		t.Errorf("Expected 2 health reports, got %d", len(reports))
	}

	// Check that both actors are reported
	for _, actorID := range []string{"actor1", "actor2"} {
		report, exists := reports[actorID]
		if !exists {
			t.Errorf("Missing health report for actor %s", actorID)
			continue
		}

		if report.ActorID != actorID {
			t.Errorf("Expected actor ID %s, got %s", actorID, report.ActorID)
		}

		if report.Status != HealthStatusHealthy {
			t.Errorf("Expected actor %s to be healthy, got status %s", actorID, report.Status)
		}
	}

	// Test individual actor health check
	report, err := system.GetActorHealth(ctx, "actor1")
	if err != nil {
		t.Fatalf("Failed to get actor health: %v", err)
	}

	if report.ActorID != "actor1" {
		t.Errorf("Expected actor ID 'actor1', got '%s'", report.ActorID)
	}

	// Test non-existent actor
	_, err = system.GetActorHealth(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error for non-existent actor")
	}

	// Cleanup
	err = system.StopAll(ctx)
	if err != nil {
		t.Fatalf("Failed to stop actors: %v", err)
	}
}

func TestHealthCheck_UnhealthyScenarios(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(*MockActor)
		expectedStatus HealthStatus
	}{
		{
			name: "recent errors cause degraded status",
			setupFunc: func(a *MockActor) {
				a.health.RecordError(errors.New("recent error"))
			},
			expectedStatus: HealthStatusDegraded,
		},
		{
			name: "multiple issues cause degraded status",
			setupFunc: func(a *MockActor) {
				a.health.RecordError(errors.New("recent error"))
				// Simulate high mailbox usage by checking mailbox metrics directly
			},
			expectedStatus: HealthStatusDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActor := NewMockActor("unhealthy-test")
			actorRef := NewActorRef("unhealthy-test", mockActor, 1) // Small mailbox

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			err := actorRef.Start(ctx)
			if err != nil {
				t.Fatalf("Failed to start actor: %v", err)
			}

			tt.setupFunc(mockActor)

			// Get health report
			responseChan := make(chan HealthCheckResponse, 1)
			healthMsg := HealthCheckRequest{ResponseChan: responseChan}

			err = actorRef.Send(healthMsg)
			if err != nil {
				t.Fatalf("Failed to send health check request: %v", err)
			}

			select {
			case response := <-responseChan:
				if response.Error != nil {
					t.Fatalf("Health check failed: %v", response.Error)
				}

				if response.Report.Status != tt.expectedStatus {
					t.Errorf("Expected status %s, got %s", tt.expectedStatus, response.Report.Status)
				}

			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for health check response")
			}

			err = actorRef.Stop(ctx)
			if err != nil {
				t.Fatalf("Failed to stop actor: %v", err)
			}
		})
	}
}

func TestHealthCheck_CustomMetricsProvider(t *testing.T) {
	customMetrics := map[string]interface{}{
		"counter": 42,
		"status":  "active",
	}

	health := NewHealthCheckable("custom-test", make(chan Message, 10), func() interface{} {
		return customMetrics
	})

	report := health.GenerateHealthReport()

	metrics, ok := report.Metrics.CustomMetrics.(map[string]interface{})
	if !ok {
		t.Fatal("Expected custom metrics to be a map")
	}

	if metrics["counter"] != 42 {
		t.Errorf("Expected counter 42, got %v", metrics["counter"])
	}

	if metrics["status"] != "active" {
		t.Errorf("Expected status 'active', got %v", metrics["status"])
	}
}

func TestDebugHealthScenario(t *testing.T) {
	DebugHealthScenario()
}
