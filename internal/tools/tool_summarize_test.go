package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestToolSummarizeTool_Name(t *testing.T) {
	tool := NewToolSummarizeTool(nil, nil)
	if tool.Name() != ToolNameToolSummarize {
		t.Errorf("expected name %s, got %s", ToolNameToolSummarize, tool.Name())
	}
}

func TestToolSummarizeTool_Description(t *testing.T) {
	tool := NewToolSummarizeTool(nil, nil)
	desc := tool.Description()
	if !strings.Contains(desc, "Execute another tool and summarize") {
		t.Error("description should mention executing and summarizing")
	}
}

func TestToolSummarizeTool_Parameters(t *testing.T) {
	tool := NewToolSummarizeTool(nil, nil)
	params := tool.Parameters()

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in parameters")
	}

	if _, ok := props["tool_name"]; !ok {
		t.Error("expected 'tool_name' in properties")
	}

	if _, ok := props["tool_args"]; !ok {
		t.Error("expected 'tool_args' in properties")
	}

	if _, ok := props["summary_goal"]; !ok {
		t.Error("expected 'summary_goal' in properties")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}

	if len(required) != 3 {
		t.Errorf("expected 3 required fields, got %d", len(required))
	}
}

func TestToolSummarizeTool_MissingToolName(t *testing.T) {
	registry := NewRegistry(nil)
	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_args":    map[string]interface{}{},
		"summary_goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when tool_name is missing")
	}

	if !strings.Contains(result.Error, "tool_name is required") {
		t.Errorf("expected tool_name error, got: %s", result.Error)
	}
}

func TestToolSummarizeTool_MissingToolArgs(t *testing.T) {
	registry := NewRegistry(nil)
	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name":    "read_file",
		"summary_goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when tool_args is missing")
	}

	if !strings.Contains(result.Error, "tool_args must be an object") {
		t.Errorf("expected tool_args error, got: %s", result.Error)
	}
}

func TestToolSummarizeTool_InvalidToolArgs(t *testing.T) {
	registry := NewRegistry(nil)
	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name":    "read_file",
		"tool_args":    "not an object",
		"summary_goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when tool_args is not an object")
	}

	if !strings.Contains(result.Error, "tool_args must be an object") {
		t.Errorf("expected tool_args error, got: %s", result.Error)
	}
}

func TestToolSummarizeTool_MissingSummaryGoal(t *testing.T) {
	registry := NewRegistry(nil)
	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{},
	})

	if result.Error == "" {
		t.Fatal("expected error when summary_goal is missing")
	}

	if !strings.Contains(result.Error, "summary_goal is required") {
		t.Errorf("expected summary_goal error, got: %s", result.Error)
	}
}

func TestToolSummarizeTool_ToolNotFound(t *testing.T) {
	registry := NewRegistry(nil)
	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name":    "nonexistent_tool",
		"tool_args":    map[string]interface{}{},
		"summary_goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when tool not found")
	}

	if !strings.Contains(result.Error, "tool not found") {
		t.Errorf("expected tool not found error, got: %s", result.Error)
	}
}

func TestToolSummarizeTool_ToolExecutionError(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	// Don't create the file - should cause read_file to error
	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{
			"path": "nonexistent.txt",
		},
		"summary_goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when tool execution fails")
	}

	if !strings.Contains(result.Error, "tool execution failed") {
		t.Errorf("expected tool execution error, got: %s", result.Error)
	}
}

func TestToolSummarizeTool_SuccessfulExecution(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	client := &MockSummarizeClient{response: "This is the summary"}
	tool := NewToolSummarizeTool(registry, client)

	content := "line 1\nline 2\nline 3"
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{
			"path": "test.txt",
		},
		"summary_goal": "count lines",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	summary := result.Result.(string)
	if summary != "This is the summary" {
		t.Errorf("expected 'This is the summary', got: %s", summary)
	}
}

func TestToolSummarizeTool_NoSummarizeClient(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	tool := NewToolSummarizeTool(registry, nil)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{
			"path": "test.txt",
		},
		"summary_goal": "summarize",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	summary := result.Result.(string)
	if !strings.Contains(summary, "no summarization available") {
		t.Error("expected message about no summarization available")
	}

	if !strings.Contains(summary, "Tool output") {
		t.Error("expected raw tool output")
	}
}

func TestToolSummarizeTool_SummarizationFailure(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	client := &MockSummarizeClient{err: fmt.Errorf("LLM error")}
	tool := NewToolSummarizeTool(registry, client)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{
			"path": "test.txt",
		},
		"summary_goal": "summarize",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	summary := result.Result.(string)
	if !strings.Contains(summary, "Summarization failed") {
		t.Error("expected message about summarization failure")
	}

	if !strings.Contains(summary, "Raw tool output") {
		t.Error("expected raw tool output")
	}
}

func TestToolSummarizeTool_PromptContainsAllInfo(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	var capturedPrompt string
	client := &CapturingMockClient{
		onComplete: func(ctx context.Context, prompt string) (string, error) {
			capturedPrompt = prompt
			return "summary", nil
		},
	}

	tool := NewToolSummarizeTool(registry, client)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("test content"))

	tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{
			"path": "test.txt",
		},
		"summary_goal": "extract key points",
	})

	if !strings.Contains(capturedPrompt, "read_file") {
		t.Error("expected prompt to contain tool name")
	}

	if !strings.Contains(capturedPrompt, "test.txt") {
		t.Error("expected prompt to contain tool args")
	}

	if !strings.Contains(capturedPrompt, "extract key points") {
		t.Error("expected prompt to contain summary goal")
	}

	if !strings.Contains(capturedPrompt, "Tool output:") {
		t.Error("expected prompt to contain tool output section")
	}
}

func TestToolSummarizeTool_ComplexToolArgs(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{
			"path": "test.txt",
			"sections": []interface{}{
				map[string]interface{}{
					"from_line": 1,
					"to_line":   1,
				},
			},
		},
		"summary_goal": "summarize",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestToolSummarizeTool_ToolReturnsNil(t *testing.T) {
	registry := NewRegistry(nil)

	// Register a tool that returns nil
	nilTool := &NilResultTool{}
	registry.Register(nilTool)

	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name":    "nil_tool",
		"tool_args":    map[string]interface{}{},
		"summary_goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when tool returns nil")
	}

	if !strings.Contains(result.Error, "tool returned nil result") {
		t.Errorf("expected nil result error, got: %s", result.Error)
	}
}

func TestToolSummarizeTool_EmptyToolArgs(t *testing.T) {
	registry := NewRegistry(nil)

	// Register a simple tool that accepts no args
	simpleToolExecuted := false
	simpleTool := &SimpleTestTool{
		onExecute: func(ctx context.Context, params map[string]interface{}) *ToolResult {
			simpleToolExecuted = true
			return &ToolResult{Result: "done"}
		},
	}
	registry.Register(simpleTool)

	client := &MockSummarizeClient{response: "summary"}
	tool := NewToolSummarizeTool(registry, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name":    "simple_tool",
		"tool_args":    map[string]interface{}{},
		"summary_goal": "what happened?",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	if !simpleToolExecuted {
		t.Error("expected simple tool to be executed")
	}
}

func TestToolSummarizeTool_LongToolOutput(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	registry := NewRegistry(nil)

	readTool := NewReadFileTool(mockFS, sess)
	registry.Register(readTool)

	client := &MockSummarizeClient{response: "concise summary"}
	tool := NewToolSummarizeTool(registry, client)

	// Create a large file
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("line %d with content", i))
	}
	content := strings.Join(lines, "\n")
	_ = mockFS.WriteFile(context.Background(), "large.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"tool_name": "read_file",
		"tool_args": map[string]interface{}{
			"path": "large.txt",
		},
		"summary_goal": "how many lines?",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	summary := result.Result.(string)
	if summary != "concise summary" {
		t.Errorf("expected 'concise summary', got: %s", summary)
	}
}

func TestFormatArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		contains []string
	}{
		{
			name: "simple args",
			args: map[string]interface{}{
				"path": "test.txt",
			},
			contains: []string{"path", "test.txt"},
		},
		{
			name: "nested args",
			args: map[string]interface{}{
				"path": "test.txt",
				"sections": []interface{}{
					map[string]interface{}{"from_line": 1},
				},
			},
			contains: []string{"path", "sections", "from_line"},
		},
		{
			name:     "empty args",
			args:     map[string]interface{}{},
			contains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatArgs(tt.args)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("expected result to contain %q, got: %s", expected, result)
				}
			}
		})
	}
}

// Helper test tools

type NilResultTool struct{}

func (t *NilResultTool) Name() string {
	return "nil_tool"
}

func (t *NilResultTool) Description() string {
	return "A tool that returns nil"
}

func (t *NilResultTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *NilResultTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	return nil
}

type SimpleTestTool struct {
	onExecute func(ctx context.Context, params map[string]interface{}) *ToolResult
}

func (t *SimpleTestTool) Name() string {
	return "simple_tool"
}

func (t *SimpleTestTool) Description() string {
	return "A simple test tool"
}

func (t *SimpleTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SimpleTestTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	if t.onExecute != nil {
		return t.onExecute(ctx, params)
	}
	return &ToolResult{Result: "done"}
}
