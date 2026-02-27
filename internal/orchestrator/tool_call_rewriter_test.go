package orchestrator

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewToolCallRewriter(t *testing.T) {
	tests := []struct {
		name               string
		summarizeClient    llm.Client
		toolRegistry       *tools.Registry
		expectedEnabled    bool
		expectedCanRewrite bool
	}{
		{
			name:               "with client and registry",
			summarizeClient:    &MockClient{},
			toolRegistry:       tools.NewRegistry(nil),
			expectedEnabled:    true,
			expectedCanRewrite: true,
		},
		{
			name:               "without client",
			summarizeClient:    nil,
			toolRegistry:       tools.NewRegistry(nil),
			expectedEnabled:    true,
			expectedCanRewrite: false,
		},
		{
			name:               "without registry",
			summarizeClient:    &MockClient{},
			toolRegistry:       nil,
			expectedEnabled:    true,
			expectedCanRewrite: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := NewToolCallRewriter(tt.summarizeClient, tt.toolRegistry)
			assert.NotNil(t, rewriter)
			assert.Equal(t, tt.expectedCanRewrite, rewriter.CanRewrite())
		})
	}
}

func TestToolCallRewriter_RewriteToolCall(t *testing.T) {
	// Create a simple mock tool registry
	registry := tools.NewRegistry(nil)

	// Register a simple mock tool
	mockTool := &mockToolSpec{name: "go_sandbox"}
	registry.Register(mockTool)

	tests := []struct {
		name              string
		invalidToolName   string
		invalidParams     map[string]interface{}
		reason            string
		mockResponse      string
		mockErr           error
		expectRewrite     bool
		expectNewTool     string
		expectExplanation string
		expectErr         bool
	}{
		{
			name:            "successful rewrite shell to go_sandbox",
			invalidToolName: "shell",
			invalidParams:   map[string]interface{}{"code": "fmt.Println(\"hello\")"},
			reason:          "tool not found",
			mockResponse: `{
				"rewritten_tool_name": "go_sandbox",
				"rewritten_params": {
					"code": "fmt.Println(\"hello\")"
				},
				"explanation": "shell is not available, using go_sandbox for code execution",
				"should_rewrite": true
			}`,
			mockErr:           nil,
			expectRewrite:     true,
			expectNewTool:     "go_sandbox",
			expectExplanation: "shell is not available, using go_sandbox for code execution",
			expectErr:         false,
		},
		{
			name:            "model recommends no rewrite",
			invalidToolName: "bash",
			invalidParams:   map[string]interface{}{"command": "ls -la"},
			reason:          "tool not found",
			mockResponse: `{
				"rewritten_tool_name": "",
				"rewritten_params": {},
				"explanation": "no suitable alternative found",
				"should_rewrite": false
			}`,
			mockErr:       nil,
			expectRewrite: false,
			expectErr:     true,
		},
		{
			name:            "fenced JSON response",
			invalidToolName: "shell",
			invalidParams:   map[string]interface{}{"code": "fmt.Println(\"hello\")"},
			reason:          "tool not found",
			mockResponse: "```json\n{\n  \"rewritten_tool_name\": \"go_sandbox\",\n  \"rewritten_params\": {\"code\": \"fmt.Println(\\\"hello\\\")\"},\n  \"explanation\": \"shell is not available, using go_sandbox for code execution\",\n  \"should_rewrite\": true\n}\n```",
			mockErr:           nil,
			expectRewrite:     true,
			expectNewTool:     "go_sandbox",
			expectExplanation: "shell is not available, using go_sandbox for code execution",
			expectErr:         false,
		},
		{
			name:            "invalid JSON response",
			invalidToolName: "python",
			invalidParams:   map[string]interface{}{"code": "print('hello')"},
			reason:          "tool not found",
			mockResponse:    "this is not json",
			mockErr:         nil,
			expectRewrite:   false,
			expectErr:       true,
		},
		{
			name:            "client error",
			invalidToolName: "sh",
			invalidParams:   map[string]interface{}{"command": "echo test"},
			reason:          "tool not found",
			mockResponse:    "",
			mockErr:         assert.AnError,
			expectRewrite:   false,
			expectErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{
				CompleteWithRequestFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
					if tt.mockErr != nil {
						return nil, tt.mockErr
					}
					return &llm.CompletionResponse{Content: tt.mockResponse}, nil
				},
			}
			rewriter := NewToolCallRewriter(mockClient, registry)

			newTool, newParams, explanation, err := rewriter.RewriteToolCall(
				context.Background(),
				tt.invalidToolName,
				tt.invalidParams,
				tt.reason,
			)

			if tt.expectErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.expectRewrite {
				assert.Equal(t, tt.expectNewTool, newTool)
				assert.Equal(t, tt.expectExplanation, explanation)
				assert.NotNil(t, newParams)
			}
		})
	}
}

func TestToolCallRewriter_CanRewrite(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		hasClient   bool
		hasRegistry bool
		expect      bool
	}{
		{
			name:        "all enabled",
			enabled:     true,
			hasClient:   true,
			hasRegistry: true,
			expect:      true,
		},
		{
			name:        "disabled via feature flag",
			enabled:     false,
			hasClient:   true,
			hasRegistry: true,
			expect:      false,
		},
		{
			name:        "no client",
			enabled:     true,
			hasClient:   false,
			hasRegistry: true,
			expect:      false,
		},
		{
			name:        "no registry",
			enabled:     true,
			hasClient:   true,
			hasRegistry: false,
			expect:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := &ToolCallRewriter{
				enabled: tt.enabled,
			}

			if tt.hasClient {
				rewriter.summarizeClient = &MockClient{}
			}
			if tt.hasRegistry {
				rewriter.toolRegistry = tools.NewRegistry(nil)
			}

			assert.Equal(t, tt.expect, rewriter.CanRewrite())
		})
	}
}

// mockToolSpec is a mock tool for testing
type mockToolSpec struct {
	name string
}

func (m *mockToolSpec) Name() string        { return m.name }
func (m *mockToolSpec) Description() string { return "mock tool" }
func (m *mockToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "code to execute",
			},
		},
	}
}
func (m *mockToolSpec) Execute(ctx context.Context, params map[string]interface{}) *tools.ToolResult {
	return &tools.ToolResult{Result: "mock executed"}
}
