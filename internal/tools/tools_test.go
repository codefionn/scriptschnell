package tools

import (
	"context"
	"testing"
)

// TestLevenshteinDistance tests the Levenshtein distance algorithm
func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		s1       string
		s2       string
		expected int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"read_file", "read_file", 0},
		{"read_file", "write_file", 4},          // "read" -> "write" = 4 substitutions
		{"read_file", "red_file", 1},            // 1 deletion
		{"read_file", "read_files", 1},          // 1 insertion
		{"shell", "shel", 1},                    // 1 deletion
		{"shell", "shells", 1},                  // 1 insertion
		{"shell", "spell", 1},                   // 1 substitution (h->p)
		{"parallel_tools", "parralel_tools", 2}, // 2 edits
		{"todo", "tudo", 1},                     // 1 substitution
		{"status", "stats", 1},                  // 1 deletion (u)
	}

	for _, tt := range tests {
		result := levenshteinDistance(tt.s1, tt.s2)
		if result != tt.expected {
			t.Errorf("levenshteinDistance(%q, %q) = %d; want %d", tt.s1, tt.s2, result, tt.expected)
		}
	}
}

// TestMin tests the min function
func TestMin(t *testing.T) {
	tests := []struct {
		a, b, c  int
		expected int
	}{
		{1, 2, 3, 1},
		{3, 2, 1, 1},
		{2, 1, 3, 1},
		{5, 5, 5, 5},
		{-1, 0, 1, -1},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b, tt.c)
		if result != tt.expected {
			t.Errorf("min(%d, %d, %d) = %d; want %d", tt.a, tt.b, tt.c, result, tt.expected)
		}
	}
}

// mockTool is a simple tool implementation for testing
type mockTool struct {
	name string
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return "Mock tool for testing"
}

func (m *mockTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (m *mockTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	return &ToolResult{Result: "mock result"}
}

// TestFindSimilarTools tests the similar tool finding functionality
func TestFindSimilarTools(t *testing.T) {
	registry := NewRegistry(nil)

	// Register some common tools
	tools := []string{
		"read_file",
		"edit_file",
		"create_file",
		"shell",
		"go_sandbox",
		"parallel_tools",
		"todo",
		"status",
	}

	for _, name := range tools {
		registry.Register(&mockTool{name: name})
	}

	tests := []struct {
		name            string
		typo            string
		maxSuggestions  int
		maxDistance     int
		expectedContain []string
		minResults      int
	}{
		{
			name:            "Single character typo",
			typo:            "red_file",
			maxSuggestions:  3,
			maxDistance:     5,
			expectedContain: []string{"read_file"},
			minResults:      1,
		},
		{
			name:            "Missing character",
			typo:            "writ_file_diff",
			maxSuggestions:  3,
			maxDistance:     7,
			expectedContain: []string{"edit_file"},
			minResults:      1,
		},
		{
			name:            "Extra character",
			typo:            "shells",
			maxSuggestions:  3,
			maxDistance:     5,
			expectedContain: []string{"shell"},
			minResults:      1,
		},
		{
			name:            "Multiple close matches",
			typo:            "read_fil",
			maxSuggestions:  3,
			maxDistance:     5,
			expectedContain: []string{"read_file"},
			minResults:      1,
		},
		{
			name:           "No close matches",
			typo:           "completely_different_tool",
			maxSuggestions: 3,
			maxDistance:    5,
			minResults:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			similar := registry.findSimilarTools(tt.typo, tt.maxSuggestions, tt.maxDistance)

			if len(similar) < tt.minResults {
				t.Errorf("findSimilarTools(%q) returned %d results; want at least %d",
					tt.typo, len(similar), tt.minResults)
			}

			// Check that expected tools are in the results
			for _, expected := range tt.expectedContain {
				found := false
				for _, suggestion := range similar {
					if suggestion == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("findSimilarTools(%q) missing expected suggestion %q; got %v",
						tt.typo, expected, similar)
				}
			}
		})
	}
}

// TestFormatToolNotFoundError tests the error message formatting
func TestFormatToolNotFoundError(t *testing.T) {
	registry := NewRegistry(nil)

	// Register some tools
	registry.Register(&mockTool{name: "read_file"})
	registry.Register(&mockTool{name: "edit_file"})
	registry.Register(&mockTool{name: "create_file"})
	registry.Register(&mockTool{name: "shell"})

	tests := []struct {
		name          string
		typo          string
		shouldContain []string
	}{
		{
			name:          "Close match",
			typo:          "red_file",
			shouldContain: []string{"tool not found", "red_file", "read_file"},
		},
		{
			name:          "Very different",
			typo:          "xyz123",
			shouldContain: []string{"tool not found", "xyz123"},
		},
		{
			name:          "Shell typo",
			typo:          "shel",
			shouldContain: []string{"tool not found", "shel", "shell"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorMsg := registry.formatToolNotFoundError(tt.typo)

			for _, substr := range tt.shouldContain {
				if !contains(errorMsg, substr) {
					t.Errorf("formatToolNotFoundError(%q) = %q; should contain %q",
						tt.typo, errorMsg, substr)
				}
			}
		})
	}
}

// TestExecuteWithInvalidTool tests that Execute returns proper error with suggestions
func TestExecuteWithInvalidTool(t *testing.T) {
	registry := NewRegistry(nil)

	// Register some tools
	registry.Register(&mockTool{name: "read_file"})
	registry.Register(&mockTool{name: "edit_file"})
	registry.Register(&mockTool{name: "shell"})

	// Try to execute a typo'd tool
	call := &ToolCall{
		ID:         "test-123",
		Name:       "red_file",
		Parameters: map[string]interface{}{},
	}

	result := registry.Execute(context.Background(), call)

	// Should have an error
	if result.Error == "" {
		t.Error("Expected error for non-existent tool, got none")
	}

	// Error should contain suggestion
	if !contains(result.Error, "read_file") {
		t.Errorf("Expected error to suggest 'read_file', got: %s", result.Error)
	}

	// Should have correct ID
	if result.ID != call.ID {
		t.Errorf("Expected result ID %q, got %q", call.ID, result.ID)
	}
}

// TestManualSuggestions tests the manual suggestion system
func TestManualSuggestions(t *testing.T) {
	registry := NewRegistry(nil)

	// Register actual tools that suggestions will point to
	registry.Register(&mockTool{name: "shell"})
	registry.Register(&mockTool{name: "go_sandbox"})
	registry.Register(&mockTool{name: "edit_file"})
	registry.Register(&mockTool{name: "create_file"})
	registry.Register(&mockTool{name: "read_file_summarized"})
	registry.Register(&mockTool{name: "todo"})

	tests := []struct {
		name          string
		toolName      string
		shouldContain []string
	}{
		{
			name:     "bash -> shell/go_sandbox",
			toolName: "bash",
			shouldContain: []string{
				"tool not found: bash",
				"shell",
				"go_sandbox",
			},
		},
		{
			name:     "python -> go_sandbox",
			toolName: "python",
			shouldContain: []string{
				"tool not found: python",
				"go_sandbox",
				"Python execution is not directly supported",
			},
		},
		{
			name:     "edit_file -> write_file_diff",
			toolName: "edit_file",
			shouldContain: []string{
				"tool not found: edit_file",
				"edit_file",
				"modify files",
			},
		},
		{
			name:     "write_file -> create_file/write_file_diff",
			toolName: "write_file",
			shouldContain: []string{
				"tool not found: write_file",
				"create_file",
				"edit_file",
			},
		},
		{
			name:     "summarize_file -> read_file_summarized",
			toolName: "summarize_file",
			shouldContain: []string{
				"tool not found: summarize_file",
				"read_file_summarized",
			},
		},
		{
			name:     "task -> todo",
			toolName: "task",
			shouldContain: []string{
				"tool not found: task",
				"todo",
			},
		},
		{
			name:     "exec -> shell/go_sandbox",
			toolName: "exec",
			shouldContain: []string{
				"tool not found: exec",
				"shell",
				"go_sandbox",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorMsg := registry.formatToolNotFoundError(tt.toolName)

			for _, substr := range tt.shouldContain {
				if !contains(errorMsg, substr) {
					t.Errorf("formatToolNotFoundError(%q) = %q; should contain %q",
						tt.toolName, errorMsg, substr)
				}
			}
		})
	}
}

// TestManualSuggestionsPriority tests that manual suggestions take priority over similarity
func TestManualSuggestionsPriority(t *testing.T) {
	registry := NewRegistry(nil)

	// Register tools
	registry.Register(&mockTool{name: "shell"})
	registry.Register(&mockTool{name: "go_sandbox"})
	registry.Register(&mockTool{name: "shelly"}) // Similar to "shell" but not the right suggestion

	// Try "bash" which should get manual suggestion to shell/go_sandbox
	// not similarity match to "shelly"
	errorMsg := registry.formatToolNotFoundError("bash")

	// Should suggest shell and go_sandbox (manual)
	if !contains(errorMsg, "shell") {
		t.Errorf("Expected manual suggestion to include 'shell', got: %s", errorMsg)
	}

	if !contains(errorMsg, "go_sandbox") {
		t.Errorf("Expected manual suggestion to include 'go_sandbox', got: %s", errorMsg)
	}

	// Should NOT suggest "shelly" (even though it's similar to "bash" -> "shell")
	if contains(errorMsg, "shelly") {
		t.Errorf("Manual suggestions should take priority over similarity, but got 'shelly' in: %s", errorMsg)
	}
}

// TestAddCustomManualSuggestion tests adding custom manual suggestions
func TestAddCustomManualSuggestion(t *testing.T) {
	registry := NewRegistry(nil)

	// Register some tools
	registry.Register(&mockTool{name: "my_custom_tool"})

	// Add a custom manual suggestion
	registry.AddManualSuggestion("custom_pattern", &ManualSuggestion{
		SuggestedTools: []string{"my_custom_tool"},
		Reason:         "This is a custom suggestion for testing",
		MatchPattern:   "exact",
	})

	errorMsg := registry.formatToolNotFoundError("custom_pattern")

	// Should contain custom suggestion
	if !contains(errorMsg, "my_custom_tool") {
		t.Errorf("Expected custom manual suggestion, got: %s", errorMsg)
	}

	if !contains(errorMsg, "custom suggestion for testing") {
		t.Errorf("Expected custom reason, got: %s", errorMsg)
	}
}

// TestManualSuggestionFiltersNonExistentTools tests that manual suggestions only show tools that exist
func TestManualSuggestionFiltersNonExistentTools(t *testing.T) {
	registry := NewRegistry(nil)

	// Only register shell, not go_sandbox
	registry.Register(&mockTool{name: "shell"})

	// Try "bash" which suggests both shell and go_sandbox
	// but only shell exists
	errorMsg := registry.formatToolNotFoundError("bash")

	// Should suggest shell (exists)
	if !contains(errorMsg, "shell") {
		t.Errorf("Expected suggestion to include 'shell', got: %s", errorMsg)
	}

	// Should suggest only 'shell' in the "Consider using" part, not 'go_sandbox'
	if !contains(errorMsg, "Consider using 'shell'") {
		t.Errorf("Expected 'Consider using 'shell'', got: %s", errorMsg)
	}

	// Should not say "Consider using one of:" since only one tool exists
	if contains(errorMsg, "Consider using one of:") {
		t.Errorf("Should only suggest single tool when only one exists, got: %s", errorMsg)
	}
}

// TestExecuteWithManualSuggestion tests that Execute returns proper error with manual suggestions
func TestExecuteWithManualSuggestion(t *testing.T) {
	registry := NewRegistry(nil)

	// Register tools
	registry.Register(&mockTool{name: "shell"})
	registry.Register(&mockTool{name: "go_sandbox"})

	tests := []struct {
		name               string
		invalidToolName    string
		expectedSuggestion string
	}{
		{
			name:               "bash suggests shell",
			invalidToolName:    "bash",
			expectedSuggestion: "shell",
		},
		{
			name:               "edit_file suggests edit_file",
			invalidToolName:    "edit_file",
			expectedSuggestion: "edit_file",
		},
		{
			name:               "python suggests go_sandbox",
			invalidToolName:    "python",
			expectedSuggestion: "go_sandbox",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := &ToolCall{
				ID:         "test-" + tt.invalidToolName,
				Name:       tt.invalidToolName,
				Parameters: map[string]interface{}{},
			}

			result := registry.Execute(context.Background(), call)

			// Should have an error
			if result.Error == "" {
				t.Error("Expected error for non-existent tool, got none")
			}

			// Error should contain manual suggestion
			if !contains(result.Error, tt.expectedSuggestion) {
				t.Errorf("Expected error to suggest %q, got: %s", tt.expectedSuggestion, result.Error)
			}

			// Should have correct ID
			if result.ID != call.ID {
				t.Errorf("Expected result ID %q, got %q", call.ID, result.ID)
			}
		})
	}
}
