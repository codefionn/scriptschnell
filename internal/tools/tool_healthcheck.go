package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// ToolExecutionHealth represents the health status of a tool execution
type ToolExecutionHealth struct {
	ToolID        string         `json:"tool_id"`
	ToolName      string         `json:"tool_name"`
	State         ExecutionState `json:"state"`
	StartTime     time.Time      `json:"start_time"`
	LastHeartbeat time.Time      `json:"last_heartbeat"`
	ElapsedTime   time.Duration  `json:"elapsed_time"`
	IsStuck       bool           `json:"is_stuck"`
	IsCancelled   bool           `json:"is_cancelled"`
	CustomData    interface{}    `json:"custom_data,omitempty"`
}

// ExecutionState represents the current state of tool execution
type ExecutionState string

const (
	StateStarting  ExecutionState = "starting"
	StateRunning   ExecutionState = "running"
	StateWaiting   ExecutionState = "waiting" // Waiting for user input
	StateCompleted ExecutionState = "completed"
	StateFailed    ExecutionState = "failed"
	StateCancelled ExecutionState = "cancelled"
	StateTimeout   ExecutionState = "timeout"
)

// ToolHealthMonitor tracks the health of ongoing tool executions
type ToolHealthMonitor struct {
	mu         sync.RWMutex
	executions map[string]*ToolExecutionHealth

	// Thresholds for detecting stuck executions
	stuckThreshold   time.Duration // Time without heartbeat to consider stuck
	maxExecutionTime time.Duration // Maximum allowed execution time
}

// NewToolHealthMonitor creates a new tool health monitor
func NewToolHealthMonitor() *ToolHealthMonitor {
	return &ToolHealthMonitor{
		executions:       make(map[string]*ToolExecutionHealth),
		stuckThreshold:   consts.Timeout30Seconds, // No heartbeat for 30s = stuck
		maxExecutionTime: consts.Timeout10Minutes, // Max 10 minutes per tool
	}
}

// SetStuckThreshold updates the threshold for detecting stuck executions
func (m *ToolHealthMonitor) SetStuckThreshold(threshold time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stuckThreshold = threshold
}

// SetMaxExecutionTime updates the maximum allowed execution time
func (m *ToolHealthMonitor) SetMaxExecutionTime(maxTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxExecutionTime = maxTime
}

// StartExecution registers a new tool execution
func (m *ToolHealthMonitor) StartExecution(toolID, toolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.executions[toolID] = &ToolExecutionHealth{
		ToolID:        toolID,
		ToolName:      toolName,
		State:         StateStarting,
		StartTime:     now,
		LastHeartbeat: now,
		ElapsedTime:   0,
		IsStuck:       false,
		IsCancelled:   false,
	}

	logger.Debug("ToolHealthMonitor: Started tracking execution %s (%s)", toolID, toolName)
}

// UpdateState updates the state of a tool execution
func (m *ToolHealthMonitor) UpdateState(toolID string, state ExecutionState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if exec, exists := m.executions[toolID]; exists {
		exec.State = state
		exec.LastHeartbeat = time.Now()
		logger.Debug("ToolHealthMonitor: Updated state for %s to %s", toolID, state)
	}
}

// Heartbeat updates the last heartbeat time for a tool execution
func (m *ToolHealthMonitor) Heartbeat(toolID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if exec, exists := m.executions[toolID]; exists {
		exec.LastHeartbeat = time.Now()
		exec.ElapsedTime = time.Since(exec.StartTime)
	}
}

// SetCustomData sets custom data for a tool execution (e.g., process info, dialog state)
func (m *ToolHealthMonitor) SetCustomData(toolID string, data interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if exec, exists := m.executions[toolID]; exists {
		exec.CustomData = data
	}
}

// CompleteExecution marks a tool execution as completed
func (m *ToolHealthMonitor) CompleteExecution(toolID string, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if exec, exists := m.executions[toolID]; exists {
		if success {
			exec.State = StateCompleted
		} else {
			exec.State = StateFailed
		}
		exec.LastHeartbeat = time.Now()
		exec.ElapsedTime = time.Since(exec.StartTime)

		logger.Debug("ToolHealthMonitor: Completed execution %s with success=%v", toolID, success)

		// Clean up after a delay (keep history for a bit)
		go func(id string) {
			time.Sleep(consts.Timeout30Seconds)
			m.RemoveExecution(id)
		}(toolID)
	}
}

// CancelExecution marks a tool execution as cancelled
func (m *ToolHealthMonitor) CancelExecution(toolID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if exec, exists := m.executions[toolID]; exists {
		exec.State = StateCancelled
		exec.IsCancelled = true
		exec.LastHeartbeat = time.Now()
		exec.ElapsedTime = time.Since(exec.StartTime)

		logger.Debug("ToolHealthMonitor: Cancelled execution %s", toolID)
	}
}

// RemoveExecution removes a tool execution from tracking
func (m *ToolHealthMonitor) RemoveExecution(toolID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.executions, toolID)
	logger.Debug("ToolHealthMonitor: Removed execution %s from tracking", toolID)
}

// GetHealth returns the health status of a specific tool execution
func (m *ToolHealthMonitor) GetHealth(toolID string) (*ToolExecutionHealth, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exec, exists := m.executions[toolID]
	if !exists {
		return nil, false
	}

	// Update elapsed time and check if stuck
	now := time.Now()
	execCopy := *exec
	execCopy.ElapsedTime = now.Sub(exec.StartTime)

	// Check if stuck (no heartbeat for stuckThreshold)
	timeSinceHeartbeat := now.Sub(exec.LastHeartbeat)
	execCopy.IsStuck = timeSinceHeartbeat > m.stuckThreshold &&
		exec.State != StateCompleted &&
		exec.State != StateFailed &&
		exec.State != StateCancelled

	return &execCopy, true
}

// GetAllHealth returns health status of all tracked executions
func (m *ToolHealthMonitor) GetAllHealth() []*ToolExecutionHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ToolExecutionHealth, 0, len(m.executions))
	now := time.Now()

	for _, exec := range m.executions {
		execCopy := *exec
		execCopy.ElapsedTime = now.Sub(exec.StartTime)

		// Check if stuck
		timeSinceHeartbeat := now.Sub(exec.LastHeartbeat)
		execCopy.IsStuck = timeSinceHeartbeat > m.stuckThreshold &&
			exec.State != StateCompleted &&
			exec.State != StateFailed &&
			exec.State != StateCancelled

		result = append(result, &execCopy)
	}

	return result
}

// CheckForStuckExecutions returns a list of executions that appear to be stuck
func (m *ToolHealthMonitor) CheckForStuckExecutions() []*ToolExecutionHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stuck := make([]*ToolExecutionHealth, 0)
	now := time.Now()

	for _, exec := range m.executions {
		// Skip completed/failed/cancelled executions
		if exec.State == StateCompleted || exec.State == StateFailed || exec.State == StateCancelled {
			continue
		}

		timeSinceHeartbeat := now.Sub(exec.LastHeartbeat)
		elapsedTime := now.Sub(exec.StartTime)

		// Check if stuck (no heartbeat) or exceeded max execution time
		if timeSinceHeartbeat > m.stuckThreshold || elapsedTime > m.maxExecutionTime {
			execCopy := *exec
			execCopy.ElapsedTime = elapsedTime
			execCopy.IsStuck = true
			stuck = append(stuck, &execCopy)
		}
	}

	return stuck
}

// GetActiveExecutionsCount returns the number of currently active executions
func (m *ToolHealthMonitor) GetActiveExecutionsCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, exec := range m.executions {
		if exec.State != StateCompleted && exec.State != StateFailed && exec.State != StateCancelled {
			count++
		}
	}
	return count
}

// MonitorContext wraps a context with health monitoring capabilities
type MonitorContext struct {
	context.Context
	monitor    *ToolHealthMonitor
	toolID     string
	cancelFunc context.CancelFunc
}

// NewMonitorContext creates a monitored context for tool execution
func NewMonitorContext(ctx context.Context, monitor *ToolHealthMonitor, toolID, toolName string) (*MonitorContext, context.CancelFunc) {
	monitoredCtx, cancel := context.WithCancel(ctx)

	// Start monitoring
	if monitor != nil {
		monitor.StartExecution(toolID, toolName)
	}

	mctx := &MonitorContext{
		Context:    monitoredCtx,
		monitor:    monitor,
		toolID:     toolID,
		cancelFunc: cancel,
	}

	return mctx, func() {
		if monitor != nil {
			monitor.CancelExecution(toolID)
		}
		cancel()
	}
}

// Heartbeat sends a heartbeat for this execution
func (m *MonitorContext) Heartbeat() {
	if m.monitor != nil {
		m.monitor.Heartbeat(m.toolID)
	}
}

// UpdateState updates the execution state
func (m *MonitorContext) UpdateState(state ExecutionState) {
	if m.monitor != nil {
		m.monitor.UpdateState(m.toolID, state)
	}
}

// SetCustomData sets custom data for this execution
func (m *MonitorContext) SetCustomData(data interface{}) {
	if m.monitor != nil {
		m.monitor.SetCustomData(m.toolID, data)
	}
}

// Complete marks the execution as completed
func (m *MonitorContext) Complete(success bool) {
	if m.monitor != nil {
		m.monitor.CompleteExecution(m.toolID, success)
	}
}

// SandboxProcessInfo contains information about a running sandbox process
type SandboxProcessInfo struct {
	PID       int       `json:"pid,omitempty"`
	StartTime time.Time `json:"start_time"`
	IsRunning bool      `json:"is_running"`
	Command   string    `json:"command"`
}

// UserDialogInfo contains information about a user dialog
type UserDialogInfo struct {
	DialogType    string    `json:"dialog_type"` // "ask_user", "ask_user_multiple", "authorization"
	IsDisplayed   bool      `json:"is_displayed"`
	QuestionCount int       `json:"question_count,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// HealthCheckReport provides a detailed health report for tool executions
type HealthCheckReport struct {
	TotalExecutions int                    `json:"total_executions"`
	ActiveCount     int                    `json:"active_count"`
	StuckCount      int                    `json:"stuck_count"`
	Executions      []*ToolExecutionHealth `json:"executions"`
	StuckExecutions []*ToolExecutionHealth `json:"stuck_executions"`
	Timestamp       time.Time              `json:"timestamp"`
}

// GenerateHealthReport creates a comprehensive health report
func (m *ToolHealthMonitor) GenerateHealthReport() *HealthCheckReport {
	allHealth := m.GetAllHealth()
	stuck := m.CheckForStuckExecutions()

	return &HealthCheckReport{
		TotalExecutions: len(allHealth),
		ActiveCount:     m.GetActiveExecutionsCount(),
		StuckCount:      len(stuck),
		Executions:      allHealth,
		StuckExecutions: stuck,
		Timestamp:       time.Now(),
	}
}

// FormatHealthReport returns a human-readable health report
func (r *HealthCheckReport) FormatHealthReport() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Tool Execution Health Report (%s)\n", r.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Total Executions: %d\n", r.TotalExecutions))
	sb.WriteString(fmt.Sprintf("Active Executions: %d\n", r.ActiveCount))
	sb.WriteString(fmt.Sprintf("Stuck Executions: %d\n", r.StuckCount))

	if r.StuckCount > 0 {
		sb.WriteString("\nStuck Executions:\n")
		for _, exec := range r.StuckExecutions {
			sb.WriteString(fmt.Sprintf("  - %s (%s): %s, elapsed: %s, last heartbeat: %s ago\n",
				exec.ToolName,
				exec.ToolID,
				exec.State,
				exec.ElapsedTime.Round(time.Second),
				time.Since(exec.LastHeartbeat).Round(time.Second),
			))
		}
	}

	return sb.String()
}
