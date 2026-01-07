package tools

import (
	"context"
	"testing"
	"time"
)

func TestToolHealthMonitor_BasicFlow(t *testing.T) {
	monitor := NewToolHealthMonitor()

	toolID := "test-tool-1"
	toolName := "test_command"

	// Start execution
	monitor.StartExecution(toolID, toolName)

	// Check health
	health, exists := monitor.GetHealth(toolID)
	if !exists {
		t.Fatal("Expected execution to exist")
	}

	if health.ToolID != toolID {
		t.Errorf("Expected toolID %s, got %s", toolID, health.ToolID)
	}

	if health.State != StateStarting {
		t.Errorf("Expected state %s, got %s", StateStarting, health.State)
	}

	// Update state
	monitor.UpdateState(toolID, StateRunning)

	health, _ = monitor.GetHealth(toolID)
	if health.State != StateRunning {
		t.Errorf("Expected state %s, got %s", StateRunning, health.State)
	}

	// Send heartbeat
	time.Sleep(100 * time.Millisecond)
	beforeHeartbeat := time.Now()
	monitor.Heartbeat(toolID)

	health, _ = monitor.GetHealth(toolID)
	if health.LastHeartbeat.Before(beforeHeartbeat) {
		t.Error("Expected heartbeat to be updated")
	}

	// Complete execution
	monitor.CompleteExecution(toolID, true)

	health, _ = monitor.GetHealth(toolID)
	if health.State != StateCompleted {
		t.Errorf("Expected state %s, got %s", StateCompleted, health.State)
	}
}

func TestToolHealthMonitor_StuckDetection(t *testing.T) {
	monitor := NewToolHealthMonitor()
	monitor.SetStuckThreshold(100 * time.Millisecond)

	toolID := "test-tool-stuck"
	toolName := "slow_command"

	// Start execution
	monitor.StartExecution(toolID, toolName)
	monitor.UpdateState(toolID, StateRunning)

	// Wait for stuck threshold
	time.Sleep(150 * time.Millisecond)

	// Check for stuck executions
	stuck := monitor.CheckForStuckExecutions()

	if len(stuck) != 1 {
		t.Fatalf("Expected 1 stuck execution, got %d", len(stuck))
	}

	if stuck[0].ToolID != toolID {
		t.Errorf("Expected stuck toolID %s, got %s", toolID, stuck[0].ToolID)
	}

	if !stuck[0].IsStuck {
		t.Error("Expected execution to be marked as stuck")
	}
}

func TestToolHealthMonitor_MultipleExecutions(t *testing.T) {
	monitor := NewToolHealthMonitor()

	// Start multiple executions
	monitor.StartExecution("tool-1", "command1")
	monitor.StartExecution("tool-2", "command2")
	monitor.StartExecution("tool-3", "command3")

	allHealth := monitor.GetAllHealth()
	if len(allHealth) != 3 {
		t.Fatalf("Expected 3 executions, got %d", len(allHealth))
	}

	// Mark one as running, one as completed
	monitor.UpdateState("tool-1", StateRunning)
	monitor.CompleteExecution("tool-2", true)

	activeCount := monitor.GetActiveExecutionsCount()
	if activeCount != 2 {
		t.Errorf("Expected 2 active executions, got %d", activeCount)
	}
}

func TestToolHealthMonitor_CancelExecution(t *testing.T) {
	monitor := NewToolHealthMonitor()

	toolID := "test-tool-cancel"
	toolName := "cancellable_command"

	monitor.StartExecution(toolID, toolName)
	monitor.UpdateState(toolID, StateRunning)

	// Cancel execution
	monitor.CancelExecution(toolID)

	health, exists := monitor.GetHealth(toolID)
	if !exists {
		t.Fatal("Expected execution to exist")
	}

	if health.State != StateCancelled {
		t.Errorf("Expected state %s, got %s", StateCancelled, health.State)
	}

	if !health.IsCancelled {
		t.Error("Expected IsCancelled to be true")
	}
}

func TestToolHealthMonitor_CustomData(t *testing.T) {
	monitor := NewToolHealthMonitor()

	toolID := "test-tool-custom"
	toolName := "custom_command"

	monitor.StartExecution(toolID, toolName)

	// Set custom data
	customData := &SandboxProcessInfo{
		PID:       12345,
		StartTime: time.Now(),
		IsRunning: true,
		Command:   "go run test.go",
	}

	monitor.SetCustomData(toolID, customData)

	health, exists := monitor.GetHealth(toolID)
	if !exists {
		t.Fatal("Expected execution to exist")
	}

	if health.CustomData == nil {
		t.Fatal("Expected custom data to be set")
	}

	procInfo, ok := health.CustomData.(*SandboxProcessInfo)
	if !ok {
		t.Fatal("Expected custom data to be SandboxProcessInfo")
	}

	if procInfo.PID != 12345 {
		t.Errorf("Expected PID 12345, got %d", procInfo.PID)
	}
}

func TestToolHealthMonitor_HealthReport(t *testing.T) {
	monitor := NewToolHealthMonitor()
	monitor.SetStuckThreshold(50 * time.Millisecond)

	// Create various execution states
	monitor.StartExecution("tool-1", "running_tool")
	monitor.UpdateState("tool-1", StateRunning)
	// Keep tool-1 alive with heartbeats
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(30 * time.Millisecond)
			monitor.Heartbeat("tool-1")
		}
	}()

	monitor.StartExecution("tool-2", "completed_tool")
	monitor.CompleteExecution("tool-2", true)

	monitor.StartExecution("tool-3", "stuck_tool")
	monitor.UpdateState("tool-3", StateRunning)
	// Don't send heartbeats for tool-3 so it becomes stuck

	// Wait for tool-3 to become stuck
	time.Sleep(100 * time.Millisecond)

	report := monitor.GenerateHealthReport()

	if report.TotalExecutions == 0 {
		t.Error("Expected total executions > 0")
	}

	if report.ActiveCount == 0 {
		t.Error("Expected active executions > 0")
	}

	if report.StuckCount != 1 {
		t.Errorf("Expected 1 stuck execution, got %d", report.StuckCount)
	}

	formatted := report.FormatHealthReport()
	if formatted == "" {
		t.Error("Expected non-empty formatted report")
	}

	t.Logf("Health Report:\n%s", formatted)
}

func TestMonitorContext_Integration(t *testing.T) {
	monitor := NewToolHealthMonitor()

	toolID := "test-context-tool"
	toolName := "context_command"

	// Create monitored context
	ctx, cancel := NewMonitorContext(context.Background(), monitor, toolID, toolName)
	defer cancel()

	// Check that execution was started
	health, exists := monitor.GetHealth(toolID)
	if !exists {
		t.Fatal("Expected execution to be tracked")
	}

	if health.ToolID != toolID {
		t.Errorf("Expected toolID %s, got %s", toolID, health.ToolID)
	}

	// Send heartbeat through context
	ctx.Heartbeat()

	// Update state through context
	ctx.UpdateState(StateRunning)

	health, _ = monitor.GetHealth(toolID)
	if health.State != StateRunning {
		t.Errorf("Expected state %s, got %s", StateRunning, health.State)
	}

	// Set custom data through context
	customData := map[string]string{"key": "value"}
	ctx.SetCustomData(customData)

	health, _ = monitor.GetHealth(toolID)
	if health.CustomData == nil {
		t.Error("Expected custom data to be set")
	}

	// Complete through context
	ctx.Complete(true)

	health, _ = monitor.GetHealth(toolID)
	if health.State != StateCompleted {
		t.Errorf("Expected state %s, got %s", StateCompleted, health.State)
	}
}

func TestToolHealthMonitor_MaxExecutionTime(t *testing.T) {
	monitor := NewToolHealthMonitor()
	monitor.SetMaxExecutionTime(100 * time.Millisecond)
	monitor.SetStuckThreshold(50 * time.Millisecond)

	toolID := "test-tool-timeout"
	toolName := "long_running_command"

	monitor.StartExecution(toolID, toolName)
	monitor.UpdateState(toolID, StateRunning)

	// Keep sending heartbeats but exceed max execution time
	for i := 0; i < 3; i++ {
		time.Sleep(50 * time.Millisecond)
		monitor.Heartbeat(toolID)
	}

	// Check for stuck executions
	stuck := monitor.CheckForStuckExecutions()

	if len(stuck) != 1 {
		t.Fatalf("Expected 1 stuck execution (exceeded max time), got %d", len(stuck))
	}

	if stuck[0].ToolID != toolID {
		t.Errorf("Expected stuck toolID %s, got %s", toolID, stuck[0].ToolID)
	}
}
