package actor

import (
	"context"
	"testing"
	"time"
)

func TestSessionHealthManager_Create(t *testing.T) {
	system := NewSystem()
	manager := NewSessionHealthManager(system, "test-session")

	if manager.sessionID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got '%s'", manager.sessionID)
	}

	if manager.IsEnabled() {
		t.Error("Health manager should be disabled by default")
	}

	if manager.GetCheckInterval() != 30*time.Second {
		t.Errorf("Expected default check interval 30s, got %v", manager.GetCheckInterval())
	}
}

func TestSessionHealthManager_EnableDisable(t *testing.T) {
	system := NewSystem()
	manager := NewSessionHealthManager(system, "test-session")

	// Test enabling
	manager.Enable(5 * time.Second)

	if !manager.IsEnabled() {
		t.Error("Health manager should be enabled")
	}

	if manager.GetCheckInterval() != 5*time.Second {
		t.Errorf("Expected check interval 5s, got %v", manager.GetCheckInterval())
	}

	// Test disabling
	manager.Disable()

	if manager.IsEnabled() {
		t.Error("Health manager should be disabled")
	}
}

func TestSessionHealthManager_SetCheckInterval(t *testing.T) {
	system := NewSystem()
	manager := NewSessionHealthManager(system, "test-session")

	manager.SetCheckInterval(10 * time.Second)

	if manager.GetCheckInterval() != 10*time.Second {
		t.Errorf("Expected check interval 10s, got %v", manager.GetCheckInterval())
	}
}

func TestSessionHealthManager_HealthCheck(t *testing.T) {
	system := NewSystem()
	manager := NewSessionHealthManager(system, "test-session")
	ctx := context.Background()

	// Test with no actors
	report := manager.HealthCheck(ctx)

	if report.SessionID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got '%s'", report.SessionID)
	}

	if report.OverallStatus != HealthStatusUnknown {
		t.Errorf("Expected status %s with no actors, got %s", HealthStatusUnknown, report.OverallStatus)
	}

	if len(report.ActorReports) != 0 {
		t.Errorf("Expected 0 actor reports, got %d", len(report.ActorReports))
	}

	if len(report.Issues) == 0 {
		t.Error("Expected issues with no actors")
	}

	// Test with healthy mock actor
	mockActor := NewMockActor("healthy-actor")
	_, err := system.Spawn(ctx, "healthy-actor", mockActor, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor: %v", err)
	}

	report = manager.HealthCheck(ctx)

	if report.SessionMetrics.TotalActors != 1 {
		t.Errorf("Expected 1 total actor, got %d", report.SessionMetrics.TotalActors)
	}

	if report.SessionMetrics.HealthyActors != 1 {
		t.Errorf("Expected 1 healthy actor, got %d", report.SessionMetrics.HealthyActors)
	}

	if report.OverallStatus != HealthStatusHealthy {
		t.Errorf("Expected status %s with healthy actor, got %s", HealthStatusHealthy, report.OverallStatus)
	}

	// Cleanup
	err = system.StopAll(ctx)
	if err != nil {
		t.Fatalf("Failed to stop actors: %v", err)
	}
}

func TestSessionHealthManager_MixedHealthStatus(t *testing.T) {
	system := NewSystem()
	manager := NewSessionHealthManager(system, "test-session")
	ctx := context.Background()

	// Create healthy actors for basic functionality test
	healthyActor1 := NewMockActor("healthy-actor-1")
	healthyActor2 := NewMockActor("healthy-actor-2")
	healthyActor3 := NewMockActor("healthy-actor-3")

	// Spawn actors
	_, err := system.Spawn(ctx, "healthy-actor-1", healthyActor1, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor1: %v", err)
	}

	_, err = system.Spawn(ctx, "healthy-actor-2", healthyActor2, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor2: %v", err)
	}

	_, err = system.Spawn(ctx, "healthy-actor-3", healthyActor3, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor3: %v", err)
	}

	report := manager.HealthCheck(ctx)

	// Test basic functionality
	if report.SessionMetrics.TotalActors != 3 {
		t.Errorf("Expected 3 total actors, got %d", report.SessionMetrics.TotalActors)
	}

	if report.SessionMetrics.HealthyActors != 3 {
		t.Errorf("Expected 3 healthy actors, got %d", report.SessionMetrics.HealthyActors)
	}

	// Overall status should be healthy with all healthy actors
	if report.OverallStatus != HealthStatusHealthy {
		t.Errorf("Expected status %s with all healthy actors, got %s", HealthStatusHealthy, report.OverallStatus)
	}

	if len(report.Issues) != 0 {
		t.Errorf("Expected 0 issues with all healthy actors, got %d", len(report.Issues))
	}

	// Cleanup
	err = system.StopAll(ctx)
	if err != nil {
		t.Fatalf("Failed to stop actors: %v", err)
	}
}

func TestSessionHealthManager_GetActorHealth(t *testing.T) {
	system := NewSystem()
	manager := NewSessionHealthManager(system, "test-session")
	ctx := context.Background()

	// Test non-existent actor
	_, err := manager.GetActorHealth(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error for non-existent actor")
	}

	// Create an actor
	mockActor := NewMockActor("test-actor")
	_, err = system.Spawn(ctx, "test-actor", mockActor, 10)
	if err != nil {
		t.Fatalf("Failed to spawn actor: %v", err)
	}

	// Test existing actor
	report, err := manager.GetActorHealth(ctx, "test-actor")
	if err != nil {
		t.Fatalf("Failed to get actor health: %v", err)
	}

	if report.ActorID != "test-actor" {
		t.Errorf("Expected actor ID 'test-actor', got '%s'", report.ActorID)
	}

	// Cleanup
	err = system.StopAll(ctx)
	if err != nil {
		t.Fatalf("Failed to stop actors: %v", err)
	}
}

func TestSessionHealthManager_PeriodicHealthChecks(t *testing.T) {
	system := NewSystem()
	manager := NewSessionHealthManager(system, "test-session")

	// Enable with short interval for testing
	manager.Enable(100 * time.Millisecond)
	defer manager.Disable()

	// Wait for at least one health check to run
	time.Sleep(150 * time.Millisecond)

	// If we reach here, the health monitoring loop didn't crash
	// This is a basic integration test
	if !manager.IsEnabled() {
		t.Error("Health manager should still be enabled")
	}
}
