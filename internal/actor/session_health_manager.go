package actor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// SessionHealthManager manages health checks for all actors in a session
type SessionHealthManager struct {
	actorSystem   *System
	sessionID     string
	mu            sync.RWMutex
	enabled       bool
	checkInterval time.Duration
	stopChan      chan struct{}
}

// SessionHealthReport contains health status of all actors in a session
type SessionHealthReport struct {
	SessionID      string                  `json:"session_id"`
	OverallStatus  HealthStatus            `json:"overall_status"`
	ActorReports   map[string]HealthReport `json:"actor_reports"`
	SessionMetrics SessionHealthMetrics    `json:"session_metrics"`
	Timestamp      time.Time               `json:"timestamp"`
	Issues         []string                `json:"issues,omitempty"`
}

// SessionHealthMetrics contains session-level health metrics
type SessionHealthMetrics struct {
	TotalActors     int           `json:"total_actors"`
	HealthyActors   int           `json:"healthy_actors"`
	DegradedActors  int           `json:"degraded_actors"`
	UnhealthyActors int           `json:"unhealthy_actors"`
	UnknownActors   int           `json:"unknown_actors"`
	LastCheckTime   time.Time     `json:"last_check_time"`
	CheckDuration   time.Duration `json:"check_duration"`
}

// NewSessionHealthManager creates a new session health manager
func NewSessionHealthManager(actorSystem *System, sessionID string) *SessionHealthManager {
	return &SessionHealthManager{
		actorSystem:   actorSystem,
		sessionID:     sessionID,
		enabled:       false,
		checkInterval: 30 * time.Second, // Default check interval
		stopChan:      make(chan struct{}),
	}
}

// Enable enables the health manager with specified check interval
func (shm *SessionHealthManager) Enable(checkInterval time.Duration) {
	shm.mu.Lock()
	defer shm.mu.Unlock()

	if shm.enabled {
		return // Already enabled
	}

	shm.enabled = true
	shm.checkInterval = checkInterval

	// Start health monitoring goroutine
	go shm.healthMonitoringLoop()

	logger.Debug("Session health manager enabled for session %s (interval: %v)", shm.sessionID, checkInterval)
}

// Disable stops health monitoring
func (shm *SessionHealthManager) Disable() {
	shm.mu.Lock()
	defer shm.mu.Unlock()

	if !shm.enabled {
		return // Already disabled
	}

	shm.enabled = false
	close(shm.stopChan)
	shm.stopChan = make(chan struct{}) // Create new channel for potential restart

	logger.Debug("Session health manager disabled for session %s", shm.sessionID)
}

// IsEnabled returns true if health monitoring is enabled
func (shm *SessionHealthManager) IsEnabled() bool {
	shm.mu.RLock()
	defer shm.mu.RUnlock()
	return shm.enabled
}

// GetCheckInterval returns the current health check interval
func (shm *SessionHealthManager) GetCheckInterval() time.Duration {
	shm.mu.RLock()
	defer shm.mu.RUnlock()
	return shm.checkInterval
}

// SetCheckInterval updates the health check interval
func (shm *SessionHealthManager) SetCheckInterval(interval time.Duration) {
	shm.mu.Lock()
	defer shm.mu.Unlock()
	shm.checkInterval = interval
}

// HealthCheck performs an immediate health check of all actors in the session
func (shm *SessionHealthManager) HealthCheck(ctx context.Context) SessionHealthReport {
	startTime := time.Now()

	// Get health reports from all actors
	actorReports := shm.actorSystem.HealthCheck(ctx)

	// Calculate session-level metrics
	sessionMetrics := shm.calculateSessionMetrics(actorReports)

	// Determine overall status
	overallStatus, issues := shm.calculateOverallStatus(actorReports)

	report := SessionHealthReport{
		SessionID:      shm.sessionID,
		OverallStatus:  overallStatus,
		ActorReports:   actorReports,
		SessionMetrics: sessionMetrics,
		Timestamp:      time.Now(),
		Issues:         issues,
	}

	// Add check duration
	sessionMetrics.CheckDuration = time.Since(startTime)
	sessionMetrics.LastCheckTime = time.Now()
	report.SessionMetrics = sessionMetrics

	logger.Debug("Session health check completed for %s: status=%s, actors=%d (healthy=%d, degraded=%d, unhealthy=%d)",
		shm.sessionID, overallStatus, sessionMetrics.TotalActors,
		sessionMetrics.HealthyActors, sessionMetrics.DegradedActors, sessionMetrics.UnhealthyActors)

	return report
}

// GetActorHealth returns health report for a specific actor
func (shm *SessionHealthManager) GetActorHealth(ctx context.Context, actorID string) (HealthReport, error) {
	return shm.actorSystem.GetActorHealth(ctx, actorID)
}

// calculateSessionMetrics calculates aggregate metrics from actor reports
func (shm *SessionHealthManager) calculateSessionMetrics(actorReports map[string]HealthReport) SessionHealthMetrics {
	metrics := SessionHealthMetrics{
		TotalActors:   len(actorReports),
		LastCheckTime: time.Now(),
	}

	for _, report := range actorReports {
		switch report.Status {
		case HealthStatusHealthy:
			metrics.HealthyActors++
		case HealthStatusDegraded:
			metrics.DegradedActors++
		case HealthStatusUnhealthy:
			metrics.UnhealthyActors++
		case HealthStatusUnknown:
			metrics.UnknownActors++
		}
	}

	return metrics
}

// calculateOverallStatus determines the overall session health status
func (shm *SessionHealthManager) calculateOverallStatus(actorReports map[string]HealthReport) (HealthStatus, []string) {
	if len(actorReports) == 0 {
		return HealthStatusUnknown, []string{"No actors found"}
	}

	var issues []string
	statusCounts := map[HealthStatus]int{
		HealthStatusHealthy:   0,
		HealthStatusDegraded:  0,
		HealthStatusUnhealthy: 0,
		HealthStatusUnknown:   0,
	}

	for actorID, report := range actorReports {
		statusCounts[report.Status]++

		if report.Status != HealthStatusHealthy {
			issues = append(issues, fmt.Sprintf("%s: %s", actorID, report.Message))
		}
	}

	// Determine overall status based on worst case
	if statusCounts[HealthStatusUnhealthy] > 0 {
		return HealthStatusUnhealthy, issues
	} else if statusCounts[HealthStatusDegraded] > 0 {
		return HealthStatusDegraded, issues
	} else if statusCounts[HealthStatusUnknown] > 0 {
		return HealthStatusUnknown, issues
	} else {
		return HealthStatusHealthy, issues
	}
}

// healthMonitoringLoop runs periodic health checks if enabled
func (shm *SessionHealthManager) healthMonitoringLoop() {
	// Read checkInterval and stopChan under lock to avoid race condition
	shm.mu.RLock()
	checkInterval := shm.checkInterval
	stopChan := shm.stopChan
	shm.mu.RUnlock()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	logger.Debug("Starting health monitoring loop for session %s", shm.sessionID)

	for {
		select {
		case <-stopChan:
			logger.Debug("Health monitoring loop stopped for session %s", shm.sessionID)
			return
		case <-ticker.C:
			// Check enabled state under lock to avoid race with Disable()
			shm.mu.RLock()
			enabled := shm.enabled
			shm.mu.RUnlock()

			if !enabled {
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			report := shm.HealthCheck(ctx)
			cancel()

			// Log health status
			if report.OverallStatus != HealthStatusHealthy {
				logger.Warn("Session %s health check: status=%s, issues: %v",
					shm.sessionID, report.OverallStatus, report.Issues)
			} else {
				logger.Debug("Session %s health check: status=%s", shm.sessionID, report.OverallStatus)
			}
		}
	}
}

// Stop gracefully stops the health manager
func (shm *SessionHealthManager) Stop() {
	shm.Disable()
	logger.Debug("Session health manager stopped for session %s", shm.sessionID)
}
