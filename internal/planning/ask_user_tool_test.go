package planning

import (
	"context"
	"testing"
)

func TestAskUserTool_WithOptions(t *testing.T) {
	tool := NewAskUserTool()

	// Test parameters schema includes options
	params := tool.Parameters()
	if props, ok := params["properties"].(map[string]interface{}); ok {
		if _, hasQuestion := props["question"]; !hasQuestion {
			t.Error("parameters should include 'question' property")
		}
		if _, hasOptions := props["options"]; !hasOptions {
			t.Error("parameters should include 'options' property")
		}
	} else {
		t.Error("parameters should have properties map")
	}

	// Test execution without options (backward compatibility)
	ctx := context.Background()
	result := tool.Execute(ctx, map[string]interface{}{
		"question": "What is your favorite color?",
	})
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if resultMap, ok := result.Result.(map[string]interface{}); ok {
		if resultMap["question"] != "What is your favorite color?" {
			t.Error("question should be preserved in result")
		}
		if _, hasOptions := resultMap["options"]; hasOptions {
			t.Error("options should not be present when not provided")
		}
	} else {
		t.Error("result should be a map")
	}

	// Test execution with valid options
	result = tool.Execute(ctx, map[string]interface{}{
		"question": "What is your favorite color?",
		"options":  []interface{}{"Red", "Green", "Blue"},
	})
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if resultMap, ok := result.Result.(map[string]interface{}); ok {
		if resultMap["question"] != "What is your favorite color?" {
			t.Error("question should be preserved in result")
		}
		if options, ok := resultMap["options"].([]string); ok {
			if len(options) != 3 || options[0] != "Red" || options[1] != "Green" || options[2] != "Blue" {
				t.Error("options should be preserved correctly in result")
			}
		} else {
			t.Error("options should be present as string array")
		}
	} else {
		t.Error("result should be a map")
	}

	// Test validation errors
	testCases := []struct {
		name   string
		params map[string]interface{}
		error  string
	}{
		{
			name: "missing question",
			params: map[string]interface{}{
				"options": []interface{}{"A", "B", "C"},
			},
			error: "question parameter is required and must be a string",
		},
		{
			name: "options not an array",
			params: map[string]interface{}{
				"question": "Test question",
				"options":  "not an array",
			},
			error: "options parameter must be an array of strings",
		},
		{
			name: "wrong number of options",
			params: map[string]interface{}{
				"question": "Test question",
				"options":  []interface{}{"A", "B"}, // Only 2 options
			},
			error: "options parameter must contain exactly 3 options",
		},
		{
			name: "non-string option",
			params: map[string]interface{}{
				"question": "Test question",
				"options":  []interface{}{"A", 123, "C"},
			},
			error: "option 1 must be a string",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Execute(ctx, tc.params)
			if result.Error != tc.error {
				t.Errorf("expected error '%s', got '%s'", tc.error, result.Error)
			}
		})
	}
}