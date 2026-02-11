package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteCompilationError_Success(t *testing.T) {
	fixedCode := `package main

import "fmt"

func main() {
	fmt.Println("hello world")
}`

	mock := &MockClient{
		CompleteWithRequestFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			// Verify the prompt contains both the original code and errors
			userMsg := req.Messages[len(req.Messages)-1].Content
			assert.Contains(t, userMsg, "undefined: fmtt")
			assert.Contains(t, userMsg, "fmtt.Println")
			return &llm.CompletionResponse{Content: fixedCode}, nil
		},
	}

	rewriter := &SandboxCodeRewriter{summarizeClient: mock, enabled: true}

	result, err := rewriter.RewriteCompilationError(
		context.Background(),
		`package main

import "fmtt"

func main() {
	fmtt.Println("hello world")
}`,
		`/tmp/sandbox/main.go:803:2: undefined: fmtt`,
	)

	require.NoError(t, err)
	assert.Equal(t, fixedCode, result)
}

func TestRewriteCompilationError_StripsFences(t *testing.T) {
	fixedCode := `package main

func main() {
	println("hello")
}`

	mock := &MockClient{
		CompleteWithRequestFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: "```go\n" + fixedCode + "\n```",
			}, nil
		},
	}

	rewriter := &SandboxCodeRewriter{summarizeClient: mock, enabled: true}

	result, err := rewriter.RewriteCompilationError(context.Background(), "broken code", "some error")
	require.NoError(t, err)
	assert.Equal(t, fixedCode, result)
}

func TestRewriteCompilationError_MissingMain(t *testing.T) {
	mock := &MockClient{
		CompleteWithRequestFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: `package main

func helper() {
	println("no main here")
}`,
			}, nil
		},
	}

	rewriter := &SandboxCodeRewriter{summarizeClient: mock, enabled: true}

	_, err := rewriter.RewriteCompilationError(context.Background(), "code", "error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing func main()")
}

func TestRewriteCompilationError_MissingPackageMain(t *testing.T) {
	mock := &MockClient{
		CompleteWithRequestFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: `package helper

func main() {
	println("wrong package")
}`,
			}, nil
		},
	}

	rewriter := &SandboxCodeRewriter{summarizeClient: mock, enabled: true}

	_, err := rewriter.RewriteCompilationError(context.Background(), "code", "error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing package main")
}

func TestRewriteCompilationError_LLMError(t *testing.T) {
	mock := &MockClient{
		CompleteWithRequestFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return nil, fmt.Errorf("rate limited")
		},
	}

	rewriter := &SandboxCodeRewriter{summarizeClient: mock, enabled: true}

	_, err := rewriter.RewriteCompilationError(context.Background(), "code", "error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
}

func TestRewriteCompilationError_NilClient(t *testing.T) {
	rewriter := &SandboxCodeRewriter{summarizeClient: nil, enabled: true}

	_, err := rewriter.RewriteCompilationError(context.Background(), "code", "error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarization client not available")
}

func TestIsCompilationError(t *testing.T) {
	tests := []struct {
		name     string
		result   interface{}
		expected bool
	}{
		{
			name: "compilation failed error",
			result: map[string]interface{}{
				"stdout":    "undefined: foo",
				"exit_code": 1,
				"error":     "compilation failed",
			},
			expected: true,
		},
		{
			name: "successful execution",
			result: map[string]interface{}{
				"stdout":    "hello world",
				"exit_code": 0,
			},
			expected: false,
		},
		{
			name: "runtime error (not compilation)",
			result: map[string]interface{}{
				"stdout":    "panic: index out of range",
				"exit_code": 2,
				"error":     "runtime error",
			},
			expected: false,
		},
		{
			name:     "non-map result",
			result:   "some string",
			expected: false,
		},
		{
			name:     "nil result",
			result:   nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCompilationError(tt.result)
			assert.Equal(t, tt.expected, got)
		})
	}
}
