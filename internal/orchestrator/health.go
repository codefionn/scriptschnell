package orchestrator

import (
	"context"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
)

// HealthCheck returns the current health status of all actors in the session
func (o *Orchestrator) HealthCheck(ctx context.Context) actor.SessionHealthReport {
	if o.healthManager == nil {
		return actor.SessionHealthReport{
			SessionID:     "",
			OverallStatus: actor.HealthStatusUnknown,
			SessionMetrics: actor.SessionHealthMetrics{
				LastCheckTime: time.Now(),
			},
			Issues:    []string{"Health monitoring not available"},
			Timestamp: time.Now(),
		}
	}
	return o.healthManager.HealthCheck(ctx)
}

// GetActorHealth returns the health status of a specific actor
func (o *Orchestrator) GetActorHealth(ctx context.Context, actorID string) (actor.HealthReport, error) {
	if o.healthManager == nil {
		return actor.HealthReport{
			ActorID:   actorID,
			Status:    actor.HealthStatusUnknown,
			Message:   "Health monitoring not available",
			Timestamp: time.Now(),
		}, nil
	}
	return o.healthManager.GetActorHealth(ctx, actorID)
}

// EnableHealthMonitoring enables periodic health checks for the session
func (o *Orchestrator) EnableHealthMonitoring(checkIntervalMs int64) {
	if o.healthManager == nil {
		// This should not happen if orchestrator was created properly
		return
	}
	o.healthManager.Enable(time.Duration(checkIntervalMs) * time.Millisecond)
}

// DisableHealthMonitoring disables periodic health checks
func (o *Orchestrator) DisableHealthMonitoring() {
	if o.healthManager != nil {
		o.healthManager.Disable()
	}
}

// HealthCheckInterval returns the current health check interval
func (o *Orchestrator) HealthCheckInterval() int64 {
	if o.healthManager == nil {
		return 0
	}
	return int64(o.healthManager.GetCheckInterval().Milliseconds())
}

// SetHealthCheckInterval updates the health check interval
func (o *Orchestrator) SetHealthCheckInterval(checkIntervalMs int64) {
	if o.healthManager != nil {
		o.healthManager.SetCheckInterval(time.Duration(checkIntervalMs) * time.Millisecond)
	}
}

// HealthMonitoringEnabled returns true if health monitoring is enabled
func (o *Orchestrator) HealthMonitoringEnabled() bool {
	if o.healthManager == nil {
		return false
	}
	return o.healthManager.IsEnabled()
}
