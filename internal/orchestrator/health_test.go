package orchestrator

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/provider"
)

// MockHealthCheckableActor for testing
type MockHealthCheckableActor struct {
	id      string
	health  *actor.HealthCheckable
	started bool
}

func NewMockHealthCheckableActor(id string) *MockHealthCheckableActor {
	return &MockHealthCheckableActor{
		id:     id,
		health: actor.NewHealthCheckable(id, make(chan actor.Message, 10), nil),
	}
}

func (a *MockHealthCheckableActor) ID() string { return a.id }

func (a *MockHealthCheckableActor) Start(ctx context.Context) error {
	a.health.RecordActivity()
	a.started = true
	return nil
}

func (a *MockHealthCheckableActor) Stop(ctx context.Context) error {
	a.health.RecordActivity()
	a.started = false
	return nil
}

func (a *MockHealthCheckableActor) Receive(ctx context.Context, msg actor.Message) error {
	a.health.RecordActivity()
	// Handle health check requests
	if healthMsg, ok := msg.(actor.HealthCheckRequest); ok {
		response := actor.HealthCheckResponse{
			Report: a.health.GenerateHealthReport(),
		}
		select {
		case healthMsg.ResponseChan <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	return nil
}

func (a *MockHealthCheckableActor) GetHealthMetrics() actor.HealthMetrics {
	return a.health.GetHealthMetrics()
}

func (a *MockHealthCheckableActor) IsHealthy() bool {
	return a.health.IsHealthy()
}

func TestOrchestrator_HealthCheck(t *testing.T) {
	tests := []struct {
		name                string
		setupFunc           func(*Orchestrator)
		expectedStatus      actor.HealthStatus
		expectedIssues      int
		expectedTotalActors int
	}{
		{
			name: "health check with nil health manager",
			setupFunc: func(o *Orchestrator) {
				o.healthManager = nil
			},
			expectedStatus:      actor.HealthStatusUnknown,
			expectedIssues:      1,
			expectedTotalActors: 0,
		},
		{
			name: "health check with health manager (no actors)",
			setupFunc: func(o *Orchestrator) {
				// Health manager will be set up by default but with no actors
			},
			expectedStatus:      actor.HealthStatusUnknown,
			expectedIssues:      1,
			expectedTotalActors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			cfg := &config.Config{
				WorkingDir: ".",
			}

			providerMgr, err := provider.NewManager("", "")
			if err != nil {
				t.Fatalf("Failed to create provider manager: %v", err)
			}
			orchestrator, err := NewOrchestrator(cfg, providerMgr, false)
			if err != nil {
				t.Fatalf("Failed to create orchestrator: %v", err)
			}
			defer orchestrator.Stop()

			tt.setupFunc(orchestrator)

			report := orchestrator.HealthCheck(ctx)

			if report.OverallStatus != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s", tt.expectedStatus, report.OverallStatus)
			}

			if len(report.Issues) != tt.expectedIssues {
				t.Errorf("Expected %d issues, got %d", tt.expectedIssues, len(report.Issues))
			}

			if report.SessionMetrics.TotalActors != tt.expectedTotalActors {
				t.Errorf("Expected %d total actors, got %d", tt.expectedTotalActors, report.SessionMetrics.TotalActors)
			}
		})
	}
}

func TestOrchestrator_GetActorHealth(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(*Orchestrator)
		actorID        string
		expectError    bool
		expectedStatus actor.HealthStatus
	}{
		{
			name: "get actor health with nil health manager",
			setupFunc: func(o *Orchestrator) {
				o.healthManager = nil
			},
			actorID:        "test-actor",
			expectError:    false,
			expectedStatus: actor.HealthStatusUnknown,
		},
		{
			name: "get actor health with health manager (non-existent actor)",
			setupFunc: func(o *Orchestrator) {
				// Health manager will be set up by default
			},
			actorID:        "non-existent",
			expectError:    true,
			expectedStatus: actor.HealthStatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			cfg := &config.Config{
				WorkingDir: ".",
			}

			providerMgr, err := provider.NewManager("", "")
			if err != nil {
				t.Fatalf("Failed to create provider manager: %v", err)
			}
			orchestrator, err := NewOrchestrator(cfg, providerMgr, false)
			if err != nil {
				t.Fatalf("Failed to create orchestrator: %v", err)
			}
			defer orchestrator.Stop()

			tt.setupFunc(orchestrator)

			report, err := orchestrator.GetActorHealth(ctx, tt.actorID)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if report.Status != tt.expectedStatus {
					t.Errorf("Expected status %s, got %s", tt.expectedStatus, report.Status)
				}
			}
		})
	}
}

func TestOrchestrator_HealthMonitoring(t *testing.T) {
	tests := []struct {
		name             string
		setupFunc        func(*Orchestrator)
		expectedEnabled  bool
		expectedInterval int64
	}{
		{
			name: "health monitoring with nil health manager",
			setupFunc: func(o *Orchestrator) {
				o.healthManager = nil
			},
			expectedEnabled:  false,
			expectedInterval: 0,
		},
		{
			name: "health monitoring with health manager (default interval)",
			setupFunc: func(o *Orchestrator) {
				// Health manager will be set up by default with 30s interval
			},
			expectedEnabled:  false,
			expectedInterval: 30000, // 30 seconds default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				WorkingDir: ".",
			}

			providerMgr, err := provider.NewManager("", "")
			if err != nil {
				t.Fatalf("Failed to create provider manager: %v", err)
			}
			orchestrator, err := NewOrchestrator(cfg, providerMgr, false)
			if err != nil {
				t.Fatalf("Failed to create orchestrator: %v", err)
			}
			defer orchestrator.Stop()

			tt.setupFunc(orchestrator)

			// Test HealthMonitoringEnabled
			enabled := orchestrator.HealthMonitoringEnabled()
			if enabled != tt.expectedEnabled {
				t.Errorf("Expected enabled=%v, got %v", tt.expectedEnabled, enabled)
			}

			// Test HealthCheckInterval
			interval := orchestrator.HealthCheckInterval()
			if interval != tt.expectedInterval {
				t.Errorf("Expected interval %d, got %d", tt.expectedInterval, interval)
			}

			// Test EnableHealthMonitoring
			orchestrator.EnableHealthMonitoring(5000) // 5 seconds

			// Test SetHealthCheckInterval
			orchestrator.SetHealthCheckInterval(10000) // 10 seconds

			// Test DisableHealthMonitoring
			orchestrator.DisableHealthMonitoring()
		})
	}
}

func TestOrchestrator_HealthCheckIntegration(t *testing.T) {
	cfg := &config.Config{
		WorkingDir: ".",
	}

	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	orchestrator, err := NewOrchestrator(cfg, providerMgr, false)
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}
	defer orchestrator.Stop()

	// Enable health monitoring
	orchestrator.EnableHealthMonitoring(1000) // 1 second

	// Test that health monitoring is enabled
	if !orchestrator.HealthMonitoringEnabled() {
		t.Error("Health monitoring should be enabled")
	}

	// Test health check interval
	expectedInterval := int64(1000)
	if orchestrator.HealthCheckInterval() != expectedInterval {
		t.Errorf("Expected interval %d, got %d", expectedInterval, orchestrator.HealthCheckInterval())
	}

	// Test health check
	ctx := context.Background()
	report := orchestrator.HealthCheck(ctx)

	// Session ID should be empty because it was created with empty string
	if report.SessionID != "" {
		t.Errorf("Expected empty session ID, got '%s'", report.SessionID)
	}

	// Should have no actors
	if report.SessionMetrics.TotalActors != 0 {
		t.Errorf("Expected 0 total actors, got %d", report.SessionMetrics.TotalActors)
	}

	// Disable health monitoring
	orchestrator.DisableHealthMonitoring()

	// Test that health monitoring is disabled
	if orchestrator.HealthMonitoringEnabled() {
		t.Error("Health monitoring should be disabled")
	}
}

func TestOrchestrator_HealthCheckWithActors(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		WorkingDir: ".",
	}

	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}
	orchestrator, err := NewOrchestrator(cfg, providerMgr, false)
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}
	defer orchestrator.Stop()

	// Create a mock actor and add it to the orchestrator's actor system
	mockActor := NewMockHealthCheckableActor("test-actor")
	_, err = orchestrator.actorSystem.Spawn(ctx, "test-actor", mockActor, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor: %v", err)
	}

	// The health manager needs to be recreated with the correct actor system
	orchestrator.healthManager = actor.NewSessionHealthManager(orchestrator.actorSystem, "test-session")

	// Test health check with actors
	report := orchestrator.HealthCheck(ctx)

	// We should have at least 1 actor (our test actor)
	if report.SessionMetrics.TotalActors < 1 {
		t.Errorf("Expected at least 1 total actor, got %d", report.SessionMetrics.TotalActors)
	}

	// We should have at least 1 healthy actor
	if report.SessionMetrics.HealthyActors < 1 {
		t.Errorf("Expected at least 1 healthy actor, got %d", report.SessionMetrics.HealthyActors)
	}

	// Overall status should be healthy since we have healthy actors
	if report.OverallStatus != actor.HealthStatusHealthy {
		t.Errorf("Expected overall status %s, got %s", actor.HealthStatusHealthy, report.OverallStatus)
	}

	// Test individual actor health
	actorReport, err := orchestrator.GetActorHealth(ctx, "test-actor")
	if err != nil {
		t.Fatalf("Failed to get actor health: %v", err)
	}

	if actorReport.ActorID != "test-actor" {
		t.Errorf("Expected actor ID 'test-actor', got '%s'", actorReport.ActorID)
	}

	if actorReport.Status != actor.HealthStatusHealthy {
		t.Errorf("Expected actor status %s, got %s", actor.HealthStatusHealthy, actorReport.Status)
	}
}

func TestOrchestrator_HealthCheckErrorScenarios(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*Orchestrator)
		testFunc    func(*Orchestrator) error
		expectError bool
	}{
		{
			name: "get actor health with nil health manager",
			setupFunc: func(o *Orchestrator) {
				o.healthManager = nil
			},
			testFunc: func(o *Orchestrator) error {
				_, err := o.GetActorHealth(context.Background(), "test-actor")
				return err
			},
			expectError: false, // Should not return error, just unknown status
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// ctx := context.Background()  // unused variable

			cfg := &config.Config{
				WorkingDir: ".",
			}

			providerMgr, err := provider.NewManager("", "")
			if err != nil {
				t.Fatalf("Failed to create provider manager: %v", err)
			}
			orchestrator, err := NewOrchestrator(cfg, providerMgr, false)
			if err != nil {
				t.Fatalf("Failed to create orchestrator: %v", err)
			}
			defer orchestrator.Stop()

			tt.setupFunc(orchestrator)

			err = tt.testFunc(orchestrator)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
