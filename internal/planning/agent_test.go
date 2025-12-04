package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/session"
)

// MockLLMClient implements a simple mock LLM for testing
type MockLLMClient struct {
	responses []string
	index     int
	lastReq   *llm.CompletionRequest
}

func NewMockLLMClient(responses ...string) *MockLLMClient {
	return &MockLLMClient{
		responses: responses,
		index:     0,
	}
}

func (m *MockLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if m.index >= len(m.responses) {
		return "Mock response", nil
	}
	response := m.responses[m.index]
	m.index++
	return response, nil
}

func (m *MockLLMClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.lastReq = req
	content, err := m.Complete(ctx, "")
	if err != nil {
		return nil, err
	}

	// Check if the content looks like a tool call response
	var response struct {
		ToolCalls []map[string]interface{} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(content), &response); err == nil && len(response.ToolCalls) > 0 {
		return &llm.CompletionResponse{
			Content:    content,
			ToolCalls:  response.ToolCalls,
			StopReason: "tool_calls",
		}, nil
	}

	return &llm.CompletionResponse{
		Content:    content,
		StopReason: "stop",
	}, nil
}

func (m *MockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	response, err := m.CompleteWithRequest(ctx, req)
	if err != nil {
		return err
	}
	return callback(response.Content)
}

func (m *MockLLMClient) GetModelName() string {
	return "mock-planning-model"
}

func (m *MockLLMClient) LastRequest() *llm.CompletionRequest {
	return m.lastReq
}

// MockFileSystem implements a simple mock filesystem for testing
type MockFileSystem struct {
	files map[string]string
}

func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		files: make(map[string]string),
	}
}

func (m *MockFileSystem) AddFile(path, content string) {
	m.files[path] = content
}

func (m *MockFileSystem) ReadFile(ctx context.Context, path string) ([]byte, error) {
	content, exists := m.files[path]
	if !exists {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return []byte(content), nil
}

func (m *MockFileSystem) ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error) {
	content, err := m.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(content), "\n")

	if from < 1 {
		from = 1
	}
	if to > len(lines) {
		to = len(lines)
	}
	if from > to {
		return nil, fmt.Errorf("invalid line range")
	}

	return lines[from-1 : to], nil
}

// Implement other required methods with basic stubs
func (m *MockFileSystem) WriteFile(ctx context.Context, path string, data []byte) error {
	m.files[path] = string(data)
	return nil
}

func (m *MockFileSystem) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	content, exists := m.files[path]
	if !exists {
		return nil, fmt.Errorf("file not found")
	}
	return &fs.FileInfo{
		Path:    path,
		Size:    int64(len(content)),
		ModTime: time.Now(),
		IsDir:   false,
	}, nil
}

func (m *MockFileSystem) ListDir(ctx context.Context, path string) ([]*fs.FileInfo, error) {
	var files []*fs.FileInfo
	for filePath := range m.files {
		if strings.HasPrefix(filePath, path) {
			files = append(files, &fs.FileInfo{
				Path:    filePath,
				Size:    int64(len(m.files[filePath])),
				ModTime: time.Now(),
				IsDir:   false,
			})
		}
	}
	return files, nil
}

func (m *MockFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	_, exists := m.files[path]
	return exists, nil
}

func (m *MockFileSystem) Delete(ctx context.Context, path string) error {
	delete(m.files, path)
	return nil
}

func (m *MockFileSystem) DeleteAll(ctx context.Context, path string) error {
	for filePath := range m.files {
		if strings.HasPrefix(filePath, path) {
			delete(m.files, filePath)
		}
	}
	return nil
}

func (m *MockFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	// No-op for mock
	return nil
}

func (m *MockFileSystem) Move(ctx context.Context, src, dst string) error {
	content, exists := m.files[src]
	if !exists {
		return fmt.Errorf("source file not found")
	}
	m.files[dst] = content
	delete(m.files, src)
	return nil
}

func TestPlanningAgent_BasicPlan(t *testing.T) {
	// Setup mock filesystem with sample files
	mockFS := NewMockFileSystem()
	mockFS.AddFile("main.go", "package main\n\nfunc main() {\n    fmt.Println(\"Hello, World!\")\n}")
	mockFS.AddFile("README.md", "# Test Project\n\nThis is a test project.")

	// Setup mock LLM with planning response
	mockLLM := NewMockLLMClient(`{
  "plan": [
    "Step 1: Analyze the existing codebase structure",
    "Step 2: Identify the main components and dependencies",
    "Step 3: Create a detailed implementation plan",
    "Step 4: Implement the changes step by step"
  ],
  "questions": [],
  "needs_input": false,
  "complete": true
}`)

	// Create session and planning agent
	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	// Create planning request
	req := &PlanningRequest{
		Objective:      "Analyze this Go project and create a plan to add logging",
		Context:        "The project needs structured logging for debugging",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	// Execute planning
	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify response
	if !response.Complete {
		t.Error("Expected plan to be complete")
	}
	if response.NeedsInput {
		t.Error("Expected plan to not need input")
	}
	if len(response.Plan) != 4 {
		t.Errorf("Expected 4 plan steps, got %d", len(response.Plan))
	}
	if len(response.Questions) != 0 {
		t.Errorf("Expected 0 questions, got %d", len(response.Questions))
	}

	// Cleanup
	err = agent.Close(ctx)
	if err != nil {
		t.Errorf("Failed to close agent: %v", err)
	}
}

func TestPlanningAgent_IncludesContextFiles(t *testing.T) {
	ctx := context.Background()
	mockFS := NewMockFileSystem()
	mockFS.AddFile("ctx/info.txt", "important context here")

	mockLLM := NewMockLLMClient(`{
  "plan": ["Step 1: Done"],
  "complete": true
}`)

	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)
	defer agent.Close(ctx)

	req := &PlanningRequest{
		Objective:    "use context",
		ContextFiles: []string{"ctx/info.txt"},
	}

	if _, err := agent.Plan(ctx, req, nil); err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	lastReq := mockLLM.LastRequest()
	if lastReq == nil {
		t.Fatal("expected last request to be captured")
	}

	found := false
	for _, msg := range lastReq.Messages {
		if strings.Contains(msg.Content, "ctx/info.txt") && strings.Contains(msg.Content, "important context here") {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected context file contents in messages, got %+v", lastReq.Messages)
	}
}

func TestPlanningAgent_WithQuestions(t *testing.T) {
	// Setup mock filesystem
	mockFS := NewMockFileSystem()

	// Setup mock LLM with questions
	mockLLM := NewMockLLMClient(`{
  "plan": [
    "Step 1: Understand the requirements"
  ],
  "questions": [
    "What logging framework should be used?",
    "What level of detail is needed in the logs?"
  ],
  "needs_input": true,
  "complete": false
}`)

	// Create session and planning agent
	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	// Create planning request
	req := &PlanningRequest{
		Objective:      "Add logging to the application",
		Context:        "Need to add structured logging",
		AllowQuestions: true,
		MaxQuestions:   3,
	}

	// Mock user input callback
	userInputCb := func(question string) (string, error) {
		return "User response for: " + question, nil
	}

	// Execute planning
	ctx := context.Background()
	response, err := agent.Plan(ctx, req, userInputCb)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify response
	if response.Complete {
		t.Error("Expected plan to not be complete")
	}
	if !response.NeedsInput {
		t.Error("Expected plan to need input")
	}
	if len(response.Plan) != 1 {
		t.Errorf("Expected 1 plan step, got %d", len(response.Plan))
	}
	if len(response.Questions) != 2 {
		t.Errorf("Expected 2 questions, got %d", len(response.Questions))
	}

	// Cleanup
	err = agent.Close(ctx)
	if err != nil {
		t.Errorf("Failed to close agent: %v", err)
	}
}

func TestPlanningAgent_ToolExecution(t *testing.T) {
	// Setup mock filesystem
	mockFS := NewMockFileSystem()
	mockFS.AddFile("config.yaml", "database:\n  host: localhost\n  port: 5432")

	// Setup mock LLM that uses tools
	mockLLM := NewMockLLMClient(
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\": \"config.yaml\"}"}}]}`,
		`{
  "plan": [
    "Step 1: Read the existing configuration",
    "Step 2: Add logging configuration",
    "Step 3: Update the application to use logging"
  ],
  "questions": [],
  "needs_input": false,
  "complete": true
}`,
	)

	// Create session and planning agent
	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	// Create planning request
	req := &PlanningRequest{
		Objective:      "Add logging configuration to the project",
		Context:        "Need to integrate logging with existing config",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	// Execute planning
	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}

	// Verify response
	if !response.Complete {
		t.Error("Expected plan to be complete")
	}
	if len(response.Plan) != 3 {
		t.Errorf("Expected 3 plan steps, got %d", len(response.Plan))
	}

	// Cleanup
	err = agent.Close(ctx)
	if err != nil {
		t.Errorf("Failed to close agent: %v", err)
	}
}

func TestPlanningAgent_NilRequest(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	ctx := context.Background()
	_, err := agent.Plan(ctx, nil, nil)

	if err == nil {
		t.Error("Expected error for nil request")
	}

	expectedErr := "planning request cannot be nil"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %s, got %s", expectedErr, err.Error())
	}

	agent.Close(ctx)
}

func TestPlanningAgent_EmptyObjective(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	_, err := agent.Plan(ctx, req, nil)

	if err == nil {
		t.Error("Expected error for empty objective")
	}

	expectedErr := "planning objective cannot be empty"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %s, got %s", expectedErr, err.Error())
	}

	agent.Close(ctx)
}

func TestPlanningAgent_NilClient(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	agent := NewPlanningAgent("test-planning", mockFS, sess, nil, nil)

	req := &PlanningRequest{
		Objective:      "test objective",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	_, err := agent.Plan(ctx, req, nil)

	if err == nil {
		t.Error("Expected error for nil client")
	}

	expectedErr := "no planning client available"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %s, got %s", expectedErr, err.Error())
	}

	agent.Close(ctx)
}

func TestPlanningAgent_MaxIterationsReached(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Mock LLM that keeps returning incomplete responses
	mockLLM := NewMockLLMClient(
		"I need more information",
		"Still not enough context",
		"Could you clarify?",
		"Let me think about this",
		"Still working on it",
		"This is taking too long",
	)

	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "complex task that needs many iterations",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should return a partial plan after max iterations
	if len(response.Plan) == 0 {
		t.Error("Expected at least one plan step after max iterations")
	}

	agent.Close(ctx)
}

func TestPlanningAgent_QuestionsDisabled(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient(`{
  "plan": ["Step 1: Basic analysis"],
  "questions": ["This question should be ignored"],
  "needs_input": true,
  "complete": false
}`)

	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "analyze without questions",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should still return a response even when questions are disabled
	if len(response.Plan) == 0 {
		t.Error("Expected plan steps even when questions disabled")
	}

	agent.Close(ctx)
}

func TestPlanningAgent_ToolRegistry(t *testing.T) {
	registry := NewPlanningToolRegistry()

	// Test empty registry
	if len(registry.tools) != 0 {
		t.Errorf("Expected empty registry, got %d tools", len(registry.tools))
	}

	// Test registering tools
	askTool := NewAskUserTool()
	readTool := &MockPlanningTool{name: "read_file"}

	registry.Register(askTool)
	registry.Register(readTool)

	if len(registry.tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(registry.tools))
	}

	// Test JSON schema generation
	schemas := registry.ToJSONSchema()
	if len(schemas) != 2 {
		t.Errorf("Expected 2 schemas, got %d", len(schemas))
	}

	// Test tool execution
	result := registry.Execute(context.Background(), "ask_user", map[string]interface{}{
		"question": "test question",
	})

	if result.Error != "" {
		t.Errorf("Unexpected error executing ask_user tool: %s", result.Error)
	}

	// Test non-existent tool
	result = registry.Execute(context.Background(), "non_existent", nil)
	if result.Error == "" {
		t.Error("Expected error for non-existent tool")
	}
}

func TestPlanningAgent_PlanExtraction(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	tests := []struct {
		name     string
		content  string
		expected *PlanningResponse
	}{
		{
			name:    "JSON with answer tags",
			content: `<answer>{"plan": ["Step 1", "Step 2"], "complete": true}</answer>`,
			expected: &PlanningResponse{
				Plan:     []string{"Step 1", "Step 2"},
				Complete: true,
			},
		},
		{
			name:    "JSON without tags",
			content: `{"plan": ["Step 1"], "complete": false}`,
			expected: &PlanningResponse{
				Plan:     []string{"Step 1"},
				Complete: false,
			},
		},
		{
			name:    "Plain text numbered steps",
			content: "1. First step\n2. Second step\n3. Third step",
			expected: &PlanningResponse{
				Plan:     []string{"1. First step", "2. Second step", "3. Third step"},
				Complete: true,
			},
		},
		{
			name:    "Plain text bullet points",
			content: "- First item\n- Second item\n- Third item",
			expected: &PlanningResponse{
				Plan:     []string{"- First item", "- Second item", "- Third item"},
				Complete: true,
			},
		},
		{
			name:    "Mixed content with questions",
			content: "1. Analyze requirements\nWhat framework should we use?\n2. Design solution",
			expected: &PlanningResponse{
				Plan:       []string{"1. Analyze requirements", "2. Design solution"},
				Questions:  []string{"What framework should we use?"},
				NeedsInput: false, // The current implementation doesn't set this to true for plain text
				Complete:   true,  // The current implementation sets complete to true if there are plan steps
			},
		},
		{
			name:    "Single line fallback",
			content: "Simple planning step",
			expected: &PlanningResponse{
				Plan:     []string{"Simple planning step"},
				Complete: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.extractPlan(tt.content)

			if result == nil {
				t.Fatal("extractPlan returned nil")
			}

			if len(result.Plan) != len(tt.expected.Plan) {
				t.Errorf("Expected %d plan steps, got %d", len(tt.expected.Plan), len(result.Plan))
			}

			for i, step := range result.Plan {
				if i < len(tt.expected.Plan) && step != tt.expected.Plan[i] {
					t.Errorf("Expected step %d to be %s, got %s", i, tt.expected.Plan[i], step)
				}
			}

			if result.Complete != tt.expected.Complete {
				t.Errorf("Expected complete to be %v, got %v", tt.expected.Complete, result.Complete)
			}

			if result.NeedsInput != tt.expected.NeedsInput {
				t.Errorf("Expected needs_input to be %v, got %v", tt.expected.NeedsInput, result.NeedsInput)
			}
		})
	}

	agent.Close(context.Background())
}

func TestPlanningAgent_PartialPlanExtraction(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")
	mockLLM := NewMockLLMClient("test response")
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	messages := []*llm.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "create a plan"},
		{Role: "assistant", Content: `<answer>{"plan": ["Partial step 1", "Partial step 2"], "complete": true}</answer>`},
	}

	partial := agent.extractPartialPlan(messages)

	if partial == nil {
		t.Fatal("extractPartialPlan returned nil")
	}

	if len(partial.Plan) == 0 {
		t.Error("Expected at least one plan step in partial extraction")
	}

	if !partial.NeedsInput {
		t.Error("Expected partial plan to indicate needs input")
	}

	if partial.Complete {
		t.Error("Expected partial plan to not be complete")
	}

	agent.Close(context.Background())
}

func TestPlanningAgent_ToolExecutionError(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Mock LLM that returns invalid tool call
	mockLLM := NewMockLLMClient(
		`{"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "invalid_tool", "arguments": "{}"}}]}`,
		`{"plan": ["Fallback plan"], "complete": true}`,
	)

	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "test with tool errors",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	ctx := context.Background()
	response, err := agent.Plan(ctx, req, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should still get a plan even with tool errors
	if len(response.Plan) == 0 {
		t.Error("Expected plan steps even with tool errors")
	}

	agent.Close(ctx)
}

func TestPlanningAgent_ContextCancellation(t *testing.T) {
	mockFS := NewMockFileSystem()
	sess := session.NewSession("test", ".")

	// Create a mock LLM that respects context cancellation
	mockLLM := &MockLLMClientWithCancellation{}
	agent := NewPlanningAgent("test-planning", mockFS, sess, mockLLM, nil)

	req := &PlanningRequest{
		Objective:      "test cancellation",
		AllowQuestions: false,
		MaxQuestions:   0,
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := agent.Plan(ctx, req, nil)

	if err == nil {
		t.Error("Expected error due to context cancellation")
	}

	agent.Close(context.Background())
}

// MockLLMClientWithCancellation respects context cancellation
type MockLLMClientWithCancellation struct{}

func (m *MockLLMClientWithCancellation) Complete(ctx context.Context, prompt string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return "test response", nil
	}
}

func (m *MockLLMClientWithCancellation) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return &llm.CompletionResponse{
			Content:    "test response",
			StopReason: "stop",
		}, nil
	}
}

func (m *MockLLMClientWithCancellation) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return callback("test response")
	}
}

func (m *MockLLMClientWithCancellation) GetModelName() string {
	return "mock-cancellation-model"
}
