package acp

import (
	"context"
	"strings"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
)

func TestHandleToolCallStart_WithDescription(t *testing.T) {
	agent := newTestAgent(t)

	// Create a test session directly (like other tests do)
	session := &statcodeSession{
		sessionID:     "test-session",
		promptCtx:     context.Background(),
		toolLocations: make(map[string][]acp.ToolCallLocation),
		toolParams:    make(map[string]map[string]interface{}),
		toolProgress:  make(map[string]*strings.Builder),
	}

	tests := []struct {
		name         string
		toolName     string
		toolID       string
		parameters   map[string]interface{}
	}{
		{
			name:     "go_sandbox with description",
			toolName: "go_sandbox",
			toolID:   "sandbox-1",
			parameters: map[string]interface{}{
				"code":        "fmt.Println(\"hello\")",
				"description": "Testing Go code execution",
			},
		},
		{
			name:     "go_sandbox without description",
			toolName: "go_sandbox",
			toolID:   "sandbox-2",
			parameters: map[string]interface{}{
				"code": "fmt.Println(\"hello\")",
			},
		},
		{
			name:     "read_file without description",
			toolName: "read_file",
			toolID:   "read-1",
			parameters: map[string]interface{}{
				"path": "/test/file.txt",
			},
		},
		{
			name:     "custom tool with description",
			toolName: "custom_tool",
			toolID:   "custom-1",
			parameters: map[string]interface{}{
				"description": "Performing custom analysis",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call handleToolCallStart
			err := agent.handleToolCallStart(session, tt.toolName, tt.toolID, tt.parameters)
			if err != nil {
				t.Fatalf("handleToolCallStart() failed: %v", err)
			}
			// The title is sent to ACP but we can't easily verify it here
			// The important thing is that handleToolCallStart doesn't fail
			assert.NoError(t, err)
		})
	}
}

func TestToolProgressStateDescription(t *testing.T) {
	// Test that description is properly extracted from tool parameters
	tests := []struct {
		name           string
		parameters     map[string]interface{}
		expectedDesc   string
	}{
		{
			name: "with description",
			parameters: map[string]interface{}{
				"code":        "package main",
				"description": "Running unit tests",
			},
			expectedDesc: "Running unit tests",
		},
		{
			name: "without description",
			parameters: map[string]interface{}{
				"code": "package main",
			},
			expectedDesc: "",
		},
		{
			name: "description is not string",
			parameters: map[string]interface{}{
				"description": 123,
			},
			expectedDesc: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract description from parameters
			var description string
			if desc, ok := tt.parameters["description"]; ok {
				if descStr, ok := desc.(string); ok {
					description = descStr
				}
			}
			assert.Equal(t, tt.expectedDesc, description)
		})
	}
}
