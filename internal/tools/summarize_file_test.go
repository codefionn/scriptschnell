package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/session"
)

// MockSummarizeClient is a mock LLM client for testing
type MockSummarizeClient struct {
	response string
	err      error
}

func (m *MockSummarizeClient) Complete(ctx context.Context, prompt string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func (m *MockSummarizeClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockSummarizeClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return fmt.Errorf("not implemented")
}

func (m *MockSummarizeClient) GetModelName() string {
	return "mock-model"
}

func (m *MockSummarizeClient) GetLastResponseID() string {
	return ""
}

func (m *MockSummarizeClient) SetPreviousResponseID(responseID string) {
}

func TestSummarizeFileToolSpec_Name(t *testing.T) {
	spec := &SummarizeFileToolSpec{}
	if spec.Name() != ToolNameReadFileSummarized {
		t.Errorf("expected name %s, got %s", ToolNameReadFileSummarized, spec.Name())
	}
}

func TestSummarizeFileToolSpec_Description(t *testing.T) {
	spec := &SummarizeFileToolSpec{}
	desc := spec.Description()
	if !strings.Contains(desc, "Read and summarize a file") {
		t.Error("description should mention reading and summarizing")
	}
	if !strings.Contains(desc, "AI") {
		t.Error("description should mention AI")
	}
}

func TestSummarizeFileToolSpec_Parameters(t *testing.T) {
	spec := &SummarizeFileToolSpec{}
	params := spec.Parameters()

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in parameters")
	}

	if _, ok := props["path"]; !ok {
		t.Error("expected 'path' in properties")
	}

	if _, ok := props["goal"]; !ok {
		t.Error("expected 'goal' in properties")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}

	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(required))
	}
}

func TestSummarizeFileTool_Name(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	if tool.Name() != ToolNameReadFileSummarized {
		t.Errorf("expected name %s, got %s", ToolNameReadFileSummarized, tool.Name())
	}
}

func TestSummarizeFileTool_MissingPath(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when path is missing")
	}

	if !strings.Contains(result.Error, "path is required") {
		t.Errorf("expected path error, got: %s", result.Error)
	}
}

func TestSummarizeFileTool_MissingGoal(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})

	if result.Error == "" {
		t.Fatal("expected error when goal is missing")
	}

	if !strings.Contains(result.Error, "goal is required") {
		t.Errorf("expected goal error, got: %s", result.Error)
	}
}

func TestSummarizeFileTool_FileNotFound(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "nonexistent.txt",
		"goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when file doesn't exist")
	}

	if !strings.Contains(result.Error, "file not found") {
		t.Errorf("expected file not found error, got: %s", result.Error)
	}
}

func TestSummarizeFileTool_RejectsBinaryFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	_ = mockFS.WriteFile(context.Background(), "lib.dylib", []byte{0x7f, 'E', 'L', 'F', 0x00})

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "lib.dylib",
		"goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error for binary file")
	}

	if !strings.Contains(result.Error, "binary") {
		t.Errorf("expected binary error, got: %s", result.Error)
	}
}

func TestSummarizeFileTool_SuccessfulSummarization(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "This is a summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	content := "line 1\nline 2\nline 3"
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"goal": "identify main points",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})

	if resultMap["path"] != "test.txt" {
		t.Errorf("expected path 'test.txt', got %v", resultMap["path"])
	}

	if resultMap["goal"] != "identify main points" {
		t.Errorf("expected goal 'identify main points', got %v", resultMap["goal"])
	}

	if resultMap["summary"] != "This is a summary" {
		t.Errorf("expected summary 'This is a summary', got %v", resultMap["summary"])
	}
}

func TestSummarizeFileTool_TracksFileInSession(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	content := "test content"
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"goal": "summarize",
	})

	if !sess.WasFileRead("test.txt") {
		t.Error("expected file to be tracked in session")
	}
}

func TestSummarizeFileTool_LLMError(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{err: fmt.Errorf("LLM error")}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"goal": "summarize",
	})

	if result.Error == "" {
		t.Fatal("expected error when LLM fails")
	}

	if !strings.Contains(result.Error, "error generating summary") {
		t.Errorf("expected summary generation error, got: %s", result.Error)
	}
}

func TestSummarizeFileTool_PromptContainsGoalAndContent(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")

	// Create a client that captures the prompt
	var capturedPrompt string
	client := &CapturingMockClient{
		onComplete: func(ctx context.Context, prompt string) (string, error) {
			capturedPrompt = prompt
			return "summary", nil
		},
	}

	tool := NewSummarizeFileTool(mockFS, sess, client)

	content := "important content"
	goal := "extract key points"
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"goal": goal,
	})

	if !strings.Contains(capturedPrompt, goal) {
		t.Error("expected prompt to contain goal")
	}

	if !strings.Contains(capturedPrompt, content) {
		t.Error("expected prompt to contain file content")
	}

	if !strings.Contains(capturedPrompt, "test.txt") {
		t.Error("expected prompt to contain file path")
	}
}

func TestSummarizeFileTool_NilSession(t *testing.T) {
	mockFS := fs.NewMockFS()
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, nil, client)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	// Should not panic with nil session
	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"goal": "summarize",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestSummarizeFileTool_LargeFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary of large file"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	// Create a large file
	var lines []string
	for i := 0; i < 10000; i++ {
		lines = append(lines, fmt.Sprintf("line %d with some content", i))
	}
	content := strings.Join(lines, "\n")
	_ = mockFS.WriteFile(context.Background(), "large.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "large.txt",
		"goal": "summarize structure",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	if resultMap["summary"] != "summary of large file" {
		t.Errorf("expected summary, got %v", resultMap["summary"])
	}
}

func TestSummarizeFileTool_EmptyFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "empty file summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	_ = mockFS.WriteFile(context.Background(), "empty.txt", []byte(""))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "empty.txt",
		"goal": "describe",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestSummarizeFileTool_SpecialCharactersInGoal(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}
	tool := NewSummarizeFileTool(mockFS, sess, client)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("content"))

	goal := "find all functions matching pattern: func.*Test"
	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"goal": goal,
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultMap := result.Result.(map[string]interface{})
	if resultMap["goal"] != goal {
		t.Errorf("expected goal to be preserved, got %v", resultMap["goal"])
	}
}

func TestNewSummarizeFileToolFactory(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	client := &MockSummarizeClient{response: "summary"}

	factory := NewSummarizeFileToolFactory(mockFS, sess, client)
	registry := NewRegistry(nil)

	executor := factory(registry)
	if executor == nil {
		t.Fatal("expected executor from factory")
	}

	// Verify it's the right type
	tool, ok := executor.(*SummarizeFileTool)
	if !ok {
		t.Fatalf("expected *SummarizeFileTool, got %T", executor)
	}

	if tool.Name() != ToolNameReadFileSummarized {
		t.Errorf("expected name %s, got %s", ToolNameReadFileSummarized, tool.Name())
	}
}

// Helper mock client that captures the prompt
type CapturingMockClient struct {
	onComplete func(ctx context.Context, prompt string) (string, error)
}

func (c *CapturingMockClient) Complete(ctx context.Context, prompt string) (string, error) {
	if c.onComplete != nil {
		return c.onComplete(ctx, prompt)
	}
	return "", nil
}

func (c *CapturingMockClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if c.onComplete != nil {
		resp, err := c.onComplete(ctx, "")
		if err != nil {
			return nil, err
		}
		return &llm.CompletionResponse{Content: resp}, nil
	}
	return &llm.CompletionResponse{}, nil
}

func (c *CapturingMockClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}

func (c *CapturingMockClient) GetModelName() string {
	return "capturing-mock"
}

func (c *CapturingMockClient) GetLastResponseID() string {
	return ""
}

func (c *CapturingMockClient) SetPreviousResponseID(responseID string) {
}
