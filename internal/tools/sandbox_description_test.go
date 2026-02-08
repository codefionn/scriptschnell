// Unit tests for description parameter functionality
// These tests verify the description field is properly defined and handled

package tools

import (
	"strings"
	"testing"
)

func TestSandboxTool_DescriptionParameter_Exists(t *testing.T) {
	tool := NewSandboxTool("/tmp", "/tmp")

	// Verify description parameter exists in the schema
	params := tool.Parameters()
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties should be a map")
	}

	// Verify description field exists
	_, ok = props["description"]
	if !ok {
		t.Error("description parameter should exist in schema")
	}
}

func TestSandboxTool_DescriptionParameter_CorrectType(t *testing.T) {
	tool := NewSandboxTool("/tmp", "/tmp")

	// Get parameters
	params := tool.Parameters()
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties should be a map")
	}

	// Get description parameter
	descParam, ok := props["description"].(map[string]interface{})
	if !ok {
		t.Fatal("description should be a map with type and description")
	}

	// Check type
	if descType, ok := descParam["type"].(string); !ok || descType != "string" {
		t.Errorf("description should be a string type, got: %v", descType)
	}

	// Check that description parameter has a description explaining its purpose
	if descText, ok := descParam["description"].(string); !ok {
		t.Error("description field should have a description explaining its purpose")
	} else {
		// Verify the description mentions TUI/Web UI usage
		if !strings.Contains(descText, "TUI") && !strings.Contains(descText, "Web") {
			t.Errorf("description of 'description' parameter should mention TUI/Web UI, got: %s", descText)
		}
	}

	// Verify it's marked as optional
	required, ok := params["required"].([]string)
	if ok {
		// description should not be in required list
		for _, r := range required {
			if r == "description" {
				t.Error("description should be an optional parameter")
			}
		}
	}
}

func TestSandboxTool_Description_InUserFacingDocs(t *testing.T) {
	tool := NewSandboxTool("/tmp", "/tmp")

	// Verify the tool's Description mentions the description field
	userDoc := tool.Description()

	// Check that the description mentions the "description" field
	if !strings.Contains(userDoc, "description") {
		t.Error("tool Description should mention the description field")
	}

	// Check that it mentions the purpose (TUI/Web UI explanation)
	if !strings.Contains(userDoc, "TUI") && !strings.Contains(userDoc, "Web") {
		t.Error("tool Description should mention TUI and Web UI")
	}
}

// Note: Integration tests for description execution are in sandbox_test.go
// They require the -integration flag because they involve WASM execution
// The actual integration tests are in the main test file under the naming:
// - TestIntegration_SandboxTool_Execute_WithDescription
// - TestIntegration_SandboxTool_Execute_DescriptionWithSpecialCharacters
// - TestIntegration_SandboxTool_Execute_DescriptionVeryLong