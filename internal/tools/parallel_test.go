package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/statcode-ai/scriptschnell/internal/fs"
	"github.com/statcode-ai/scriptschnell/internal/session"
)

func TestParallelTool_Name(t *testing.T) {
	tool := NewParallelTool(nil)
	if tool.Name() != ToolNameParallel {
		t.Errorf("expected name %s, got %s", ToolNameParallel, tool.Name())
	}
}

func TestParallelTool_Description(t *testing.T) {
	tool := NewParallelTool(nil)
	desc := tool.Description()
	if !strings.Contains(desc, "Execute multiple tools concurrently") {
		t.Error("description should mention concurrent execution")
	}
}

func TestParallelTool_Parameters(t *testing.T) {
	tool := NewParallelTool(nil)
	params := tool.Parameters()

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in parameters")
	}

	toolCalls, ok := props["tool_calls"].(map[string]interface{})
	if !ok {
		t.Fatal("expected tool_calls in properties")
	}

	if toolCalls["type"] != "array" {
		t.Error("tool_calls should be an array")
	}
}

func TestParallelTool_NoRegistry(t *testing.T) {
	tool := NewParallelTool(nil)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "test.txt"},
			},
		},
	})

	if result.Error == "" {
		t.Fatal("expected error when registry is nil")
	}

	if !strings.Contains(result.Error, "registry is not configured") {
		t.Errorf("expected registry error, got: %s", result.Error)
	}
}

func TestParallelTool_MissingToolCalls(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{})

	if result.Error == "" {
		t.Fatal("expected error when tool_calls is missing")
	}

	if !strings.Contains(result.Error, "tool_calls is required") {
		t.Errorf("expected tool_calls error, got: %s", result.Error)
	}
}

func TestParallelTool_InvalidToolCallsType(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": "not an array",
	})

	if result.Error == "" {
		t.Fatal("expected error when tool_calls is not an array")
	}

	if !strings.Contains(result.Error, "must be an array") {
		t.Errorf("expected array error, got: %s", result.Error)
	}
}

func TestParallelTool_EmptyToolCalls(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}

	duration := resultMap["duration_ms"].(int64)
	if duration != 0 {
		t.Errorf("expected 0 duration, got %d", duration)
	}
}

func TestParallelTool_InvalidCallElement(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			"not an object",
		},
	})

	if result.Error == "" {
		t.Fatal("expected error when call element is not an object")
	}

	if !strings.Contains(result.Error, "must be an object") {
		t.Errorf("expected object error, got: %s", result.Error)
	}
}

func TestParallelTool_MissingToolName(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"parameters": map[string]interface{}{"path": "test.txt"},
			},
		},
	})

	if result.Error == "" {
		t.Fatal("expected error when tool name is missing")
	}

	if !strings.Contains(result.Error, "name must be a non-empty string") {
		t.Errorf("expected name error, got: %s", result.Error)
	}
}

func TestParallelTool_InvalidParameters(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "read_file",
				"parameters": "not an object",
			},
		},
	})

	if result.Error == "" {
		t.Fatal("expected error when parameters is not an object")
	}

	if !strings.Contains(result.Error, "parameters must be an object") {
		t.Errorf("expected parameters error, got: %s", result.Error)
	}
}

func TestParallelTool_SingleToolCall(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	tool := NewParallelTool(registry)

	// Create test file
	mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name": "read_file",
				"parameters": map[string]interface{}{
					"path": "test.txt",
				},
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0]["error"] != nil {
		t.Errorf("unexpected error in result: %v", results[0]["error"])
	}

	if results[0]["tool"] != "read_file" {
		t.Errorf("expected tool name 'read_file', got %v", results[0]["tool"])
	}

	if results[0]["index"] != 0 {
		t.Errorf("expected index 0, got %v", results[0]["index"])
	}
}

func TestParallelTool_MultipleToolCalls(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	tool := NewParallelTool(registry)

	// Create test files
	mockFS.WriteFile(context.Background(), "file1.txt", []byte("content1"))
	mockFS.WriteFile(context.Background(), "file2.txt", []byte("content2"))
	mockFS.WriteFile(context.Background(), "file3.txt", []byte("content3"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "file1.txt"},
			},
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "file2.txt"},
			},
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "file3.txt"},
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Check that all results have proper indices
	for i := 0; i < 3; i++ {
		if results[i]["index"] != i {
			t.Errorf("expected index %d, got %v", i, results[i]["index"])
		}
		if results[i]["error"] != nil {
			t.Errorf("unexpected error in result %d: %v", i, results[i]["error"])
		}
	}
}

func TestParallelTool_ToolError(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	tool := NewParallelTool(registry)

	// Don't create the file - it should error
	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "nonexistent.txt"},
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should have an error in the result
	if results[0]["error"] == nil {
		t.Error("expected error in result for nonexistent file")
	}

	errorStr := results[0]["error"].(string)
	if !strings.Contains(errorStr, "file not found") {
		t.Errorf("expected 'file not found' error, got: %s", errorStr)
	}
}

func TestParallelTool_MixedSuccessAndError(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	tool := NewParallelTool(registry)

	// Create only one file
	mockFS.WriteFile(context.Background(), "exists.txt", []byte("content"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "exists.txt"},
			},
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "missing.txt"},
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First should succeed
	if results[0]["error"] != nil {
		t.Errorf("unexpected error in first result: %v", results[0]["error"])
	}

	// Second should error
	if results[1]["error"] == nil {
		t.Error("expected error in second result")
	}
}

func TestParallelTool_UnknownTool(t *testing.T) {
	registry := NewRegistry(nil)
	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "nonexistent_tool",
				"parameters": map[string]interface{}{},
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should have an error for unknown tool
	if results[0]["error"] == nil {
		t.Error("expected error for unknown tool")
	}

	errorStr := results[0]["error"].(string)
	if !strings.Contains(errorStr, "tool not found") {
		t.Errorf("expected 'tool not found' error, got: %s", errorStr)
	}
}

func TestParallelTool_ContextCancellation(t *testing.T) {
	registry := NewRegistry(nil)

	// Create a slow tool
	slowTool := &SlowTestTool{delay: 100 * time.Millisecond}
	registry.Register(slowTool)

	tool := NewParallelTool(registry)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result := tool.Execute(ctx, map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "slow_tool",
				"parameters": map[string]interface{}{},
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should have context cancellation error
	if results[0]["error"] == nil {
		t.Error("expected error for cancelled context")
	}

	errorStr := results[0]["error"].(string)
	if !strings.Contains(errorStr, "context canceled") {
		t.Errorf("expected 'context canceled' error, got: %s", errorStr)
	}
}

func TestParallelTool_DurationTracking(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	tool := NewParallelTool(registry)

	mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name":       "read_file",
				"parameters": map[string]interface{}{"path": "test.txt"},
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	// Check that duration is tracked
	if results[0]["duration_ms"] == nil {
		t.Error("expected duration_ms in result")
	}

	duration := results[0]["duration_ms"].(int64)
	if duration < 0 {
		t.Errorf("expected non-negative duration, got %d", duration)
	}

	// Check total duration
	totalDuration := resultMap["duration_ms"].(int64)
	if totalDuration < 0 {
		t.Errorf("expected non-negative total duration, got %d", totalDuration)
	}
}

func TestParallelTool_NoParameters(t *testing.T) {
	registry := NewRegistry(nil)

	// Create a tool that doesn't require parameters
	noParamTool := &NoParamTestTool{}
	registry.Register(noParamTool)

	tool := NewParallelTool(registry)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"name": "no_param_tool",
				// No parameters field
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	results := resultMap["results"].([]map[string]interface{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0]["error"] != nil {
		t.Errorf("unexpected error: %v", results[0]["error"])
	}
}

// Helper test tools

type SlowTestTool struct {
	delay time.Duration
}

func (t *SlowTestTool) Name() string {
	return "slow_tool"
}

func (t *SlowTestTool) Description() string {
	return "A slow tool for testing"
}

func (t *SlowTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SlowTestTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	select {
	case <-ctx.Done():
		return &ToolResult{Error: ctx.Err().Error()}
	case <-time.After(t.delay):
		return &ToolResult{Result: "done"}
	}
}

type NoParamTestTool struct{}

func (t *NoParamTestTool) Name() string {
	return "no_param_tool"
}

func (t *NoParamTestTool) Description() string {
	return "A tool with no parameters"
}

func (t *NoParamTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *NoParamTestTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	return &ToolResult{Result: fmt.Sprintf("executed with %d params", len(params))}
}
