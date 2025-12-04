package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

// TestPlanningToolRegistry_Registration tests tool registration functionality
func TestPlanningToolRegistry_Registration(t *testing.T) {
	registry := NewPlanningToolRegistry()
	
	// Test empty registry
	if len(registry.tools) != 0 {
		t.Error("Expected empty registry to have no tools")
	}
	
	// Test tool registration
	mockTool := &MockPlanningTool{
		name:        "test_tool",
		description: "Test tool for registry",
	}
	
	registry.Register(mockTool)
	
	if len(registry.tools) != 1 {
		t.Error("Expected registry to have 1 tool after registration")
	}
	
	// Test duplicate registration (should overwrite)
	mockTool2 := &MockPlanningTool{
		name:        "test_tool",
		description: "Updated test tool",
	}
	
	registry.Register(mockTool2)
	
	if len(registry.tools) != 1 {
		t.Error("Expected registry to still have 1 tool after duplicate registration")
	}
}

// TestPlanningToolRegistry_Execute tests tool execution
func TestPlanningToolRegistry_Execute(t *testing.T) {
	registry := NewPlanningToolRegistry()
	
	// Test executing non-existent tool
	result := registry.Execute(context.Background(), "non_existent", map[string]interface{}{})
	if result.Error != "tool not found: non_existent" {
		t.Errorf("Expected tool not found error, got: %s", result.Error)
	}
	
	// Register a tool and test execution
	successTool := &MockPlanningTool{
		name:        "success_tool",
		description: "Tool that succeeds",
		result:      "success result",
	}
	
	registry.Register(successTool)
	
	result = registry.Execute(context.Background(), "success_tool", map[string]interface{}{})
	if result.Error != "" {
		t.Errorf("Expected no error, got: %s", result.Error)
	}
	if result.Result != "success result" {
		t.Errorf("Expected 'success result', got: %v", result.Result)
	}
	
	// Test tool that returns error
	errorTool := &MockPlanningTool{
		name:        "error_tool",
		description: "Tool that errors",
		error:       "tool execution error",
	}
	
	registry.Register(errorTool)
	
	result = registry.Execute(context.Background(), "error_tool", map[string]interface{}{})
	if result.Error != "tool execution error" {
		t.Errorf("Expected 'tool execution error', got: %s", result.Error)
	}
	if result.Result != nil {
		t.Errorf("Expected nil result on error, got: %v", result.Result)
	}
}

// TestPlanningToolRegistry_ToJSONSchema tests JSON schema generation
func TestPlanningToolRegistry_ToJSONSchema(t *testing.T) {
	registry := NewPlanningToolRegistry()
	
	// Test empty registry
	schemas := registry.ToJSONSchema()
	if len(schemas) != 0 {
		t.Error("Expected empty schema list for empty registry")
	}
	
	// Register tools with different parameter types
	simpleTool := &MockPlanningTool{
		name:        "simple_tool",
		description: "Simple test tool",
	}
	
	complexTool := &MockPlanningTool{
		name:        "complex_tool",
		description: "Complex test tool with parameters",
	}
	
	registry.Register(simpleTool)
	registry.Register(complexTool)
	
	schemas = registry.ToJSONSchema()
	if len(schemas) != 2 {
		t.Errorf("Expected 2 schemas, got %d", len(schemas))
	}
	
	// Validate schema structure
	for _, schema := range schemas {
		if schema["type"] != "function" {
			t.Error("Expected schema type to be 'function'")
		}
		
		function, ok := schema["function"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected function to be a map")
		}
		
		if function["name"] == nil {
			t.Error("Expected function to have a name")
		}
		
		if function["description"] == nil {
			t.Error("Expected function to have a description")
		}
		
		if function["parameters"] == nil {
			t.Error("Expected function to have parameters")
		}
	}
}

// TestPlanningToolRegistry_ConcurrentAccess tests concurrent registry operations
func TestPlanningToolRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewPlanningToolRegistry()
	
	const numGoroutines = 10
	const numOperations = 50
	
	done := make(chan bool, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			for j := 0; j < numOperations; j++ {
				// Register tool
				tool := &MockPlanningTool{
					name:        fmt.Sprintf("tool_%d_%d", id, j),
					description: fmt.Sprintf("Tool %d.%d", id, j),
				}
				registry.Register(tool)
				
				// Execute tool
				result := registry.Execute(context.Background(), tool.Name(), map[string]interface{}{})
				if result == nil {
					t.Errorf("Tool execution returned nil for %s", tool.Name())
					return
				}
			}
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// OK
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for concurrent registry operations")
		}
	}
}

// TestDefaultPlanningTools tests all default planning tools
func TestDefaultPlanningTools(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	
	agent := NewPlanningAgent("test-agent", mockFS, sess, mockLLM, nil)
	
	// Test that default tools are registered
	expectedTools := []string{"ask_user", "read_file", "search_files", "search_file_content"}
	for _, name := range expectedTools {
		if _, ok := agent.toolRegistry.tools[name]; !ok {
			t.Errorf("Expected tool %s to be registered", name)
		}
	}
	
	// Test AskUserTool specifically
	askUserTool := NewAskUserTool()
	if askUserTool.Name() != "ask_user" {
		t.Errorf("Expected ask_user tool name, got: %s", askUserTool.Name())
	}
}

// TestAskUserTool_Execution tests the ask_user tool execution
func TestAskUserTool_Execution(t *testing.T) {
	tool := NewAskUserTool()
	
	// Test with valid question
	params := map[string]interface{}{
		"question": "What is your favorite color?",
	}
	
	result := tool.Execute(context.Background(), params)
	if result.Error != "" {
		t.Errorf("Expected no error, got: %s", result.Error)
	}
	
	if result.Result == nil {
		t.Error("Expected result to be non-nil")
	}
	
	// Test with missing question
	params = map[string]interface{}{}
	result = tool.Execute(context.Background(), params)
	if result.Error != "question parameter is required and must be a string" {
		t.Errorf("Expected question parameter error, got: %s", result.Error)
	}
	
	// Test with invalid question type
	params = map[string]interface{}{
		"question": 123,
	}
	result = tool.Execute(context.Background(), params)
	if result.Error != "question parameter is required and must be a string" {
		t.Errorf("Expected question parameter error, got: %s", result.Error)
	}
}

// TestPlanningToolResult_JSONSerialization tests that PlanningToolResult can be serialized to JSON
func TestPlanningToolResult_JSONSerialization(t *testing.T) {
	// Test successful result
	successResult := &PlanningToolResult{
		Result: map[string]interface{}{
			"content": "test content",
			"lines":   10,
		},
		Error: "",
	}
	
	data, err := json.Marshal(successResult)
	if err != nil {
		t.Errorf("Failed to marshal success result: %v", err)
	}
	
	var unmarshaled PlanningToolResult
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Errorf("Failed to unmarshal success result: %v", err)
	}
	
	if unmarshaled.Error != "" {
		t.Error("Expected empty error after unmarshaling")
	}
	
	// Test error result
	errorResult := &PlanningToolResult{
		Result: nil,
		Error:  "test error",
	}
	
	data, err = json.Marshal(errorResult)
	if err != nil {
		t.Errorf("Failed to marshal error result: %v", err)
	}
	
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Errorf("Failed to unmarshal error result: %v", err)
	}
	
	if unmarshaled.Error != "test error" {
		t.Errorf("Expected 'test error', got: %s", unmarshaled.Error)
	}
}
