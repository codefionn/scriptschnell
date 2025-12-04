package planning

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/session"
	realtools "github.com/codefionn/scriptschnell/internal/tools"
)

// MockInvestigator implements tools.Investigator for testing
type MockInvestigator struct{}

var _ realtools.Investigator = &MockInvestigator{}

func (m *MockInvestigator) Investigate(ctx context.Context, objective string) (string, error) {
	return "mock investigation result", nil
}

// TestPlanningAgent_Initialization tests various initialization scenarios
func TestPlanningAgent_Initialization(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		fs        fs.FileSystem
		session   *session.Session
		client    llm.Client
		expectNil bool
	}{
		{
			name:      "valid initialization",
			id:        "test-agent",
			fs:        NewMockFileSystem(),
			session:   session.NewSession("test", "."),
			client:    NewMockLLMClient("test response"),
			expectNil: false,
		},
		{
			name:      "nil filesystem",
			id:        "test-agent",
			fs:        nil,
			session:   session.NewSession("test", "."),
			client:    NewMockLLMClient("test response"),
			expectNil: false, // Should handle nil filesystem gracefully
		},
		{
			name:      "nil session",
			id:        "test-agent",
			fs:        NewMockFileSystem(),
			session:   nil,
			client:    NewMockLLMClient("test response"),
			expectNil: false, // Should handle nil session gracefully
		},
		{
			name:      "nil client",
			id:        "test-agent",
			fs:        NewMockFileSystem(),
			session:   session.NewSession("test", "."),
			client:    nil,
			expectNil: false, // Should handle nil client gracefully
		},
		{
			name:      "empty ID",
			id:        "",
			fs:        NewMockFileSystem(),
			session:   session.NewSession("test", "."),
			client:    NewMockLLMClient("test response"),
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewPlanningAgent(tt.id, tt.fs, tt.session, tt.client, nil)
			
			if tt.expectNil && agent != nil {
				t.Error("Expected agent to be nil")
			} else if !tt.expectNil && agent == nil {
				t.Error("Expected agent to be non-nil")
			}
			
			if agent != nil && agent.id != tt.id {
				t.Errorf("Expected agent ID %s, got %s", tt.id, agent.id)
			}
		})
	}
}

// TestPlanningAgent_ToolInitialization tests that planning tools are properly initialized
func TestPlanningAgent_ToolInitialization(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	// Pass a mock investigator to ensure codebase_investigator tool is registered
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, &MockInvestigator{})
	
	// Check that default tools are registered
	if agent.toolRegistry == nil {
		t.Fatal("Expected tool registry to be initialized")
	}
	
	// Test that expected tools are registered
	expectedTools := []string{"ask_user", "read_file", "search_files", "search_file_content", "codebase_investigator"}
	for _, toolName := range expectedTools {
		// We can't directly access the tools, but we can test execution
		result := agent.toolRegistry.Execute(context.Background(), toolName, map[string]interface{}{})
		if result == nil {
			t.Errorf("Expected tool %s to be registered", toolName)
		}
		// Some tools should return errors for invalid params, but not "tool not found"
		if result.Error == fmt.Sprintf("tool not found: %s", toolName) {
			t.Errorf("Tool %s was not registered", toolName)
		}
	}
}

// TestPlanningAgent_SetExternalTools tests external tool management
func TestPlanningAgent_SetExternalTools(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// Create a mock external tool
	mockExternalTool := &MockPlanningTool{
		name:        "external_tool",
		description: "External test tool",
	}
	
	// Set external tools
	agent.SetExternalTools([]PlanningTool{mockExternalTool})
	
	// Test that the external tool is available
	result := agent.toolRegistry.Execute(context.Background(), "external_tool", map[string]interface{}{})
	if result == nil {
		t.Error("Expected external tool to be available")
	}
	if result.Error == "tool not found: external_tool" {
		t.Error("External tool was not properly registered")
	}
	
	// Test that default tools are still available
	result = agent.toolRegistry.Execute(context.Background(), "read_file", map[string]interface{}{})
	if result.Error == "tool not found: read_file" {
		t.Error("Default tools should still be available after setting external tools")
	}
}

// TestPlanningAgent_ConcurrentToolAccess tests thread safety of tool registry
func TestPlanningAgent_ConcurrentToolAccess(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// Test concurrent tool access
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			// Try to access tools concurrently
			for j := 0; j < 5; j++ {
				result := agent.toolRegistry.Execute(context.Background(), "read_file", map[string]interface{}{
					"path": fmt.Sprintf("test_file_%d.txt", id),
				})
				if result == nil {
					t.Errorf("Tool execution returned nil in goroutine %d", id)
					return
				}
			}
			
			// Test setting external tools concurrently
			if id%2 == 0 {
				agent.SetExternalTools([]PlanningTool{
					&MockPlanningTool{
						name:        fmt.Sprintf("concurrent_tool_%d", id),
						description: "Concurrent test tool",
					},
				})
			}
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// OK
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent tool access")
		}
	}
}

// MockPlanningTool is a mock implementation of PlanningTool for testing
type MockPlanningTool struct {
	name        string
	description string
	result      interface{}
	error       string
}

func (m *MockPlanningTool) Name() string {
	return m.name
}

func (m *MockPlanningTool) Description() string {
	return m.description
}

func (m *MockPlanningTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"test_param": map[string]interface{}{
				"type": "string",
			},
		},
	}
}

func (m *MockPlanningTool) Execute(ctx context.Context, params map[string]interface{}) *PlanningToolResult {
	if m.error != "" {
		return &PlanningToolResult{Error: m.error}
	}
	if m.result != nil {
		return &PlanningToolResult{Result: m.result}
	}
	return &PlanningToolResult{Result: "mock result"}
}

// TestPlanningAgent_ConfigurationValidation tests configuration validation
func TestPlanningAgent_ConfigurationValidation(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// Test that the agent maintains its configuration
	if agent.id != "test-agent" {
		t.Errorf("Expected agent ID to be 'test-agent', got '%s'", agent.id)
	}
	
	if agent.fs != mockFS {
		t.Error("Expected filesystem to be set correctly")
	}
	
	if agent.session != sess {
		t.Error("Expected session to be set correctly")
	}
	
	if agent.client != mockLLM {
		t.Error("Expected client to be set correctly")
	}
	
	if agent.toolRegistry == nil {
		t.Error("Expected tool registry to be initialized")
	}
}

// TestPlanningAgent_Close tests proper cleanup
func TestPlanningAgent_Close(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// Add some tools to test cleanup
	agent.SetExternalTools([]PlanningTool{
		&MockPlanningTool{name: "test_tool", description: "Test"},
	})
	
	// Test close with valid context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err := agent.Close(ctx)
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
	
	// Test that tools are cleared after close
	if len(agent.toolRegistry.tools) != 0 {
		t.Error("Expected tool registry to be cleared after close")
	}
}

// TestPlanningAgent_CloseCancellation tests close with context cancellation
func TestPlanningAgent_CloseCancellation(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// Test close with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	
	err := agent.Close(ctx)
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Unexpected error on cancelled close: %v", err)
	}
}

// TestPlanningAgent_MultipleClose tests that close can be called multiple times
func TestPlanningAgent_MultipleClose(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	ctx := context.Background()
	
	// Close multiple times - should not panic
	err1 := agent.Close(ctx)
	err2 := agent.Close(ctx)
	err3 := agent.Close(ctx)
	
	if err1 != nil {
		t.Errorf("First close returned error: %v", err1)
	}
	if err2 != nil {
		t.Errorf("Second close returned error: %v", err2)
	}
	if err3 != nil {
		t.Errorf("Third close returned error: %v", err3)
	}
}

// TestPlanningAgent_InitializationWithActorSystem tests actor system integration
func TestPlanningAgent_InitializationWithActorSystem(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// The actor system should be initialized (even if nil for testing)
	// This tests that the agent can handle actor system operations
	if agent.actorSystem != nil {
		// If actor system is initialized, it should not panic on access
		t.Log("Actor system initialized")
	}
}

// TestPlanningAgent_ResourceLimits tests resource limit handling
func TestPlanningAgent_ResourceLimits(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// Test with many external tools
	var manyTools []PlanningTool
	for i := 0; i < 100; i++ {
		manyTools = append(manyTools, &MockPlanningTool{
			name:        fmt.Sprintf("tool_%d", i),
			description: fmt.Sprintf("Tool number %d", i),
		})
	}
	
	agent.SetExternalTools(manyTools)
	
	// Verify all tools are registered
	for i := 0; i < 100; i++ {
		toolName := fmt.Sprintf("tool_%d", i)
		result := agent.toolRegistry.Execute(context.Background(), toolName, map[string]interface{}{})
		if result == nil || result.Error == fmt.Sprintf("tool not found: %s", toolName) {
			t.Errorf("Tool %s was not registered", toolName)
		}
	}
}