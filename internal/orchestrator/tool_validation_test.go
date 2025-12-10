package orchestrator

import (
	"errors"
	"testing"
)

func TestParseToolCallValidationError(t *testing.T) {
	tests := []struct {
		name           string
		errMsg         string
		expectedResult bool
		expectedTool   string
		expectedParam  string
	}{
		{
			name:           "missing required parameter - old style",
			errMsg:         "tool call validation failed: missing required parameter 'path' in 'write_file_replace'",
			expectedResult: true,
			expectedTool:   "write_file_replace",
			expectedParam:  "path",
		},
		{
			name:           "missing required parameter - with quotes",
			errMsg:         `tool call validation failed: missing required parameter "path" in "write_file_replace"`,
			expectedResult: true,
			expectedTool:   "write_file_replace",
			expectedParam:  "path",
		},
		{
			name:           "tool not in request.tools - shell tool",
			errMsg:         `tool call validation failed: attempted to call tool 'shell' which was not in request.tools`,
			expectedResult: true,
			expectedTool:   "shell",
			expectedParam:  "tool 'shell' is not available (not in request.tools)",
		},
		{
			name:           "tool not in request.tools - read_file tool",
			errMsg:         `tool call validation failed: attempted to call tool 'read_file' which was not in request.tools`,
			expectedResult: true,
			expectedTool:   "read_file",
			expectedParam:  "tool 'read_file' is not available (not in request.tools)",
		},
		{
			name:           "no match - different error format",
			errMsg:         "some other error message",
			expectedResult: false,
		},
		{
			name:           "no match - null error",
			errMsg:         "",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = errors.New(tt.errMsg)
			}

			result := parseToolCallValidationError(err)

			if tt.expectedResult {
				if result == nil {
					t.Errorf("Expected validation error, got nil")
					return
				}
				if result.toolName != tt.expectedTool {
					t.Errorf("Expected tool name %s, got %s", tt.expectedTool, result.toolName)
				}
				if result.missingParam != tt.expectedParam {
					t.Errorf("Expected missing param %s, got %s", tt.expectedParam, result.missingParam)
				}
			} else {
				if result != nil {
					t.Errorf("Expected nil result, got %v", result)
				}
			}
		})
	}
}

func TestToolCallValidationErrorFormat(t *testing.T) {
	// Test the Error() method formatting
	err := &toolCallValidationError{
		toolName:     "test_tool",
		missingParam: "test_param",
		inner:        errors.New("inner error msg"),
	}

	expected := "tool call validation failed for test_tool: missing test_param: inner error msg"
	if err.Error() != expected {
		t.Errorf("Expected error message: %s, got: %s", expected, err.Error())
	}

	// Test without inner error
	err = &toolCallValidationError{
		toolName:     "test_tool",
		missingParam: "test_param",
		inner:        nil,
	}

	expected = "tool call validation failed for test_tool: missing test_param"
	if err.Error() != expected {
		t.Errorf("Expected error message: %s, got: %s", expected, err.Error())
	}
}

func TestToolCallValidationErrorNil(t *testing.T) {
	// Test that Error() method handles nil receiver
	var err *toolCallValidationError
	if err.Error() != "" {
		t.Errorf("Expected empty string for nil error, got: %s", err.Error())
	}
}
