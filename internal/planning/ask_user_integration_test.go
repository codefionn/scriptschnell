package planning

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAskUserFormattingWithOptions(t *testing.T) {
	// Test that ask_user with options formats the question correctly
	tool := NewAskUserTool()

	// Test that the tool accepts options
	ctx := context.Background()
	result := tool.Execute(ctx, map[string]interface{}{
		"question": "What is your favorite color?",
		"options":  []interface{}{"Red", "Green", "Blue"},
	})

	if result.Error != "" {
		t.Fatalf("Tool execution failed: %s", result.Error)
	}

	// Verify the result contains the options
	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result should be a map")
	}

	if resultMap["question"] != "What is your favorite color?" {
		t.Error("Question not preserved correctly")
	}

	options, ok := resultMap["options"].([]string)
	if !ok {
		t.Fatal("Options should be a string array")
	}

	if len(options) != 3 || options[0] != "Red" || options[1] != "Green" || options[2] != "Blue" {
		t.Error("Options not preserved correctly")
	}
}

func TestAskUserIntegration(t *testing.T) {
	// Test the formatting logic directly
	question := "What color scheme do you prefer?"
	options := []interface{}{"Red", "Green", "Blue"}

	// Format like the processToolCalls method does
	var questionsText strings.Builder
	questionsText.WriteString(fmt.Sprintf("1. %s\n", question))
	for j, opt := range options {
		if optStr, ok := opt.(string); ok {
			questionsText.WriteString(fmt.Sprintf("   %c. %s\n", 'a'+j, optStr))
		}
	}

	formattedQuestion := questionsText.String()

	// Verify the formatting
	if !strings.Contains(formattedQuestion, "1. What color scheme do you prefer?") {
		t.Error("Question should be numbered")
	}
	if !strings.Contains(formattedQuestion, "   a. Red") {
		t.Error("Should include option a")
	}
	if !strings.Contains(formattedQuestion, "   b. Green") {
		t.Error("Should include option b")
	}
	if !strings.Contains(formattedQuestion, "   c. Blue") {
		t.Error("Should include option c")
	}
}

func TestAskUserBackwardCompatibility(t *testing.T) {
	// Test that ask_user without options still works (backward compatibility)
	tool := NewAskUserTool()

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]interface{}{
		"question": "What is your name?",
		// No options provided - should work fine
	})

	if result.Error != "" {
		t.Fatalf("Tool execution failed: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Result should be a map")
	}

	if resultMap["question"] != "What is your name?" {
		t.Error("Question not preserved correctly")
	}

	// Options should not be present when not provided
	if _, hasOptions := resultMap["options"]; hasOptions {
		t.Error("Options should not be present when not provided")
	}
}
