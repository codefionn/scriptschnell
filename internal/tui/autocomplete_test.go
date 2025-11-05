package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/statcode-ai/statcode-ai/internal/fs"
)

func TestGetCommandSuggestions(t *testing.T) {
	totalCommands := len(availableCommandSuggestions())
	tests := []struct {
		name     string
		input    string
		expected int // number of expected suggestions
		contains []string
	}{
		{
			name:     "empty input shows all commands",
			input:    "",
			expected: totalCommands, // total number of commands
			contains: []string{"/help", "/models", "/provider", "/init", "/clear", "/quit"},
		},
		{
			name:     "slash only shows all commands",
			input:    "/",
			expected: totalCommands,
			contains: []string{"/help", "/models", "/provider", "/init", "/clear", "/quit"},
		},
		{
			name:     "partial /mod matches models commands",
			input:    "/mod",
			expected: 2, // /models, /models refresh
			contains: []string{"/models", "/models refresh"},
		},
		{
			name:     "partial /prov matches provider commands",
			input:    "/prov",
			expected: 1, // /provider
			contains: []string{"/provider"},
		},
		{
			name:     "exact match /help",
			input:    "/help",
			expected: 1,
			contains: []string{"/help"},
		},
		{
			name:     "no match",
			input:    "/xyz",
			expected: 0,
			contains: []string{},
		},
		{
			name:     "case insensitive matching",
			input:    "/MOD",
			expected: 2,
			contains: []string{"/models"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions := getCommandSuggestions(tt.input)

			if len(suggestions) != tt.expected {
				t.Errorf("expected %d suggestions, got %d: %v", tt.expected, len(suggestions), suggestions)
			}

			for _, expected := range tt.contains {
				found := false
				for _, sugg := range suggestions {
					if sugg == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected suggestion %q not found in %v", expected, suggestions)
				}
			}
		})
	}
}

func TestUpdateSuggestions(t *testing.T) {
	tests := []struct {
		name          string
		commandMode   bool
		textareaValue string
		expectSuggs   bool
	}{
		{
			name:          "command mode triggers suggestions",
			commandMode:   true,
			textareaValue: "/mod",
			expectSuggs:   true,
		},
		{
			name:          "slash prefix triggers suggestions",
			commandMode:   false,
			textareaValue: "/help",
			expectSuggs:   true,
		},
		{
			name:          "no command mode and no slash shows no suggestions",
			commandMode:   false,
			textareaValue: "hello",
			expectSuggs:   false,
		},
		{
			name:          "empty input in command mode shows suggestions",
			commandMode:   true,
			textareaValue: "",
			expectSuggs:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New("test-model", "", false)
			m.commandMode = tt.commandMode
			m.textarea.SetValue(tt.textareaValue)

			m.updateSuggestions()

			hasSuggestions := len(m.suggestions) > 0
			if hasSuggestions != tt.expectSuggs {
				t.Errorf("expected suggestions: %v, got: %v (suggestions: %v)", tt.expectSuggs, hasSuggestions, m.suggestions)
			}
		})
	}
}

func TestTabKeyAutocomplete(t *testing.T) {
	m := New("test-model", "", false)
	m.commandMode = true
	m.textarea.SetValue("/mod")

	// Update suggestions
	m.updateSuggestions()

	if len(m.suggestions) == 0 {
		t.Fatal("expected suggestions for /mod")
	}

	initialSuggestions := make([]string, len(m.suggestions))
	copy(initialSuggestions, m.suggestions)

	// First Tab press - should apply first suggestion
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	if m.textarea.Value() != initialSuggestions[0] {
		t.Errorf("expected textarea value to be %q, got %q", initialSuggestions[0], m.textarea.Value())
	}

	if m.selectedSuggIndex != 0 {
		t.Errorf("expected selectedSuggIndex to remain at first suggestion, got %d", m.selectedSuggIndex)
	}

	// Should still have suggestions for cycling
	if len(m.suggestions) == 0 {
		t.Error("expected suggestions to remain for cycling")
	}

	// Second Tab press - should cycle to next suggestion
	if len(initialSuggestions) > 1 {
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = model.(*Model)

		if m.textarea.Value() != initialSuggestions[1] {
			t.Errorf("expected textarea value to cycle to %q, got %q", initialSuggestions[1], m.textarea.Value())
		}
	}
}

func TestTabKeyCycling(t *testing.T) {
	m := New("test-model", "", false)
	m.commandMode = true
	m.textarea.SetValue("/models")

	// Update suggestions - should have multiple /models commands
	m.updateSuggestions()

	if len(m.suggestions) < 2 {
		t.Fatalf("expected at least 2 suggestions for /models, got %d", len(m.suggestions))
	}

	initialSuggestions := make([]string, len(m.suggestions))
	copy(initialSuggestions, m.suggestions)

	// First tab - should apply first suggestion
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	if m.textarea.Value() != initialSuggestions[0] {
		t.Errorf("first tab: expected %q, got %q", initialSuggestions[0], m.textarea.Value())
	}
	if m.selectedSuggIndex != 0 {
		t.Errorf("expected highlight to remain on first suggestion after first tab, got %d", m.selectedSuggIndex)
	}

	// Second tab - should cycle to second suggestion
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	if m.textarea.Value() != initialSuggestions[1] {
		t.Errorf("second tab: expected %q, got %q", initialSuggestions[1], m.textarea.Value())
	}
	if m.selectedSuggIndex != 1 {
		t.Errorf("expected highlight to move to second suggestion after second tab, got %d", m.selectedSuggIndex)
	}

	// Third tab - should wrap back to first (since we only have 2 suggestions now)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	if m.textarea.Value() != initialSuggestions[0] {
		t.Errorf("wrap around: expected %q, got %q", initialSuggestions[0], m.textarea.Value())
	}
	if m.selectedSuggIndex != 0 {
		t.Errorf("expected highlight to wrap back to first suggestion, got %d", m.selectedSuggIndex)
	}
}

func TestShiftTabCyclesBackward(t *testing.T) {
	m := New("test-model", "", false)
	m.commandMode = true
	m.textarea.SetValue("/models")

	m.updateSuggestions()

	if len(m.suggestions) < 2 {
		t.Fatalf("expected at least 2 suggestions for /models, got %d", len(m.suggestions))
	}

	initialSuggestions := make([]string, len(m.suggestions))
	copy(initialSuggestions, m.suggestions)

	// Move forward twice.
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	if m.textarea.Value() != initialSuggestions[1] {
		t.Fatalf("expected textarea value to be %q after two tabs, got %q", initialSuggestions[1], m.textarea.Value())
	}

	// Shift+Tab should move back to the previous suggestion.
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = model.(*Model)

	if m.textarea.Value() != initialSuggestions[0] {
		t.Errorf("expected shift+tab to move back to %q, got %q", initialSuggestions[0], m.textarea.Value())
	}
	if m.selectedSuggIndex != 0 {
		t.Errorf("expected selectedSuggIndex to move back to 0, got %d", m.selectedSuggIndex)
	}
	if m.tabCycleIndex != m.selectedSuggIndex {
		t.Errorf("expected tabCycleIndex to match selectedSuggIndex after shift+tab, got %d", m.tabCycleIndex)
	}
}

func TestShiftTabStartsFromLastSuggestion(t *testing.T) {
	m := New("test-model", "", false)
	m.commandMode = true
	m.textarea.SetValue("/models")

	m.updateSuggestions()

	if len(m.suggestions) < 2 {
		t.Fatalf("expected at least two suggestions for /models, got %d", len(m.suggestions))
	}

	initialSuggestions := make([]string, len(m.suggestions))
	copy(initialSuggestions, m.suggestions)

	// Shift+Tab as the first cycling action should pick the last suggestion.
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = model.(*Model)

	lastIdx := len(initialSuggestions) - 1

	if m.textarea.Value() != initialSuggestions[lastIdx] {
		t.Errorf("expected initial shift+tab to select last suggestion %q, got %q", initialSuggestions[lastIdx], m.textarea.Value())
	}
	if m.selectedSuggIndex != lastIdx {
		t.Errorf("expected selectedSuggIndex to be %d after shift+tab, got %d", lastIdx, m.selectedSuggIndex)
	}
	if m.tabCycleIndex != lastIdx {
		t.Errorf("expected tabCycleIndex to be %d after shift+tab, got %d", lastIdx, m.tabCycleIndex)
	}
}

func TestTabKeyStartsFromFirstSuggestion(t *testing.T) {
	m := New("test-model", "", false)
	m.commandMode = true
	m.textarea.SetValue("/models")

	m.updateSuggestions()

	if len(m.suggestions) < 2 {
		t.Fatalf("expected multiple suggestions for /models, got %d", len(m.suggestions))
	}

	initialSuggestions := make([]string, len(m.suggestions))
	copy(initialSuggestions, m.suggestions)

	// Simulate navigating to a different suggestion before pressing Tab
	m.selectedSuggIndex = 1

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	if m.textarea.Value() != initialSuggestions[0] {
		t.Errorf("expected first tab to apply first suggestion %q, got %q", initialSuggestions[0], m.textarea.Value())
	}
	if m.selectedSuggIndex != 0 {
		t.Errorf("expected highlight to return to first suggestion, got %d", m.selectedSuggIndex)
	}
}

func TestArrowKeyNavigation(t *testing.T) {
	m := New("test-model", "", false)
	m.commandMode = true
	m.textarea.SetValue("/")

	// Update suggestions manually (simulating what happens in Update())
	m.suggestions = getCommandSuggestions("/")
	m.selectedSuggIndex = 0

	if len(m.suggestions) < 3 {
		t.Fatal("expected at least 3 suggestions")
	}

	initialIndex := m.selectedSuggIndex

	// Simulate Down arrow - create proper KeyMsg
	keyMsg := tea.KeyMsg{Type: tea.KeyDown, Alt: false}
	model, _ := m.Update(keyMsg)
	m = model.(*Model)

	if m.selectedSuggIndex != initialIndex+1 {
		t.Errorf("expected selectedSuggIndex to increment from %d to %d, got %d", initialIndex, initialIndex+1, m.selectedSuggIndex)
	}
	if m.tabCycleIndex != m.selectedSuggIndex {
		t.Errorf("expected tabCycleIndex to follow selected suggestion after down arrow, got %d", m.tabCycleIndex)
	}

	// Simulate Up arrow
	keyMsg = tea.KeyMsg{Type: tea.KeyUp, Alt: false}
	model, _ = m.Update(keyMsg)
	m = model.(*Model)

	if m.selectedSuggIndex != initialIndex {
		t.Errorf("expected selectedSuggIndex to return to %d, got %d", initialIndex, m.selectedSuggIndex)
	}
	if m.tabCycleIndex != m.selectedSuggIndex {
		t.Errorf("expected tabCycleIndex to match selected suggestion after up arrow, got %d", m.tabCycleIndex)
	}

	// Test wrapping at end
	m.selectedSuggIndex = len(m.suggestions) - 1
	keyMsg = tea.KeyMsg{Type: tea.KeyDown, Alt: false}
	model, _ = m.Update(keyMsg)
	m = model.(*Model)

	if m.selectedSuggIndex != 0 {
		t.Errorf("expected selectedSuggIndex to wrap to 0, got %d", m.selectedSuggIndex)
	}
	if m.tabCycleIndex != 0 {
		t.Errorf("expected tabCycleIndex to wrap to 0, got %d", m.tabCycleIndex)
	}

	// Test wrapping at beginning
	m.selectedSuggIndex = 0
	keyMsg = tea.KeyMsg{Type: tea.KeyUp, Alt: false}
	model, _ = m.Update(keyMsg)
	m = model.(*Model)

	if m.selectedSuggIndex != len(m.suggestions)-1 {
		t.Errorf("expected selectedSuggIndex to wrap to %d, got %d", len(m.suggestions)-1, m.selectedSuggIndex)
	}
	if m.tabCycleIndex != m.selectedSuggIndex {
		t.Errorf("expected tabCycleIndex to follow selected suggestion after wrapping up, got %d", m.tabCycleIndex)
	}
}

func TestEscClearsSuggestions(t *testing.T) {
	m := New("test-model", "", false)
	m.commandMode = true
	m.textarea.SetValue("/mod")

	// Update suggestions
	m.updateSuggestions()

	if len(m.suggestions) == 0 {
		t.Fatal("expected suggestions for /mod")
	}

	// Simulate ESC key press
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(*Model)

	// Check that suggestions were cleared
	if len(m.suggestions) != 0 {
		t.Errorf("expected suggestions to be cleared after ESC, got %v", m.suggestions)
	}

	if m.selectedSuggIndex != 0 {
		t.Errorf("expected selectedSuggIndex to be reset to 0, got %d", m.selectedSuggIndex)
	}
}

func TestRenderSuggestions(t *testing.T) {
	m := New("test-model", "", false)
	m.suggestions = []string{"/help", "/models", "/provider"}
	m.selectedSuggIndex = 1

	rendered := m.renderSuggestions()

	if rendered == "" {
		t.Error("expected non-empty rendered suggestions")
	}

	// Should contain help text
	if !containsString(rendered, "Suggestions") {
		t.Error("expected rendered output to contain 'Suggestions' header")
	}

	// Should contain the suggestions
	if !containsString(rendered, "/help") {
		t.Error("expected rendered output to contain '/help'")
	}

	if !containsString(rendered, "/models") {
		t.Error("expected rendered output to contain '/models'")
	}

	if !containsString(rendered, "/provider") {
		t.Error("expected rendered output to contain '/provider'")
	}
}

func TestRenderSuggestionsEmpty(t *testing.T) {
	m := New("test-model", "", false)
	m.suggestions = []string{}

	rendered := m.renderSuggestions()

	if rendered != "" {
		t.Errorf("expected empty string for no suggestions, got %q", rendered)
	}
}

func TestRenderSuggestionsMany(t *testing.T) {
	m := New("test-model", "", false)
	// Create more than 5 suggestions to test scrolling
	m.suggestions = []string{
		"/help",
		"/models",
		"/models refresh",
		"/provider",
		"/init",
		"/clear",
		"/quit",
	}
	m.selectedSuggIndex = 4 // Middle of list

	rendered := m.renderSuggestions()

	// Should show indicator that there are more items
	if !containsString(rendered, "...") {
		t.Error("expected rendered output to contain '...' indicator for pagination")
	}

	// Should show count
	if !containsString(rendered, "5/7") { // 4th index = 5th item
		t.Error("expected rendered output to contain position indicator '5/7'")
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Tests for filepath autocomplete

func TestExtractFilepathContext(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		cursorPos   int
		expectAtPos int
		expectPath  string
		expectFound bool
	}{
		{
			name:        "no @ symbol",
			input:       "hello world",
			cursorPos:   11,
			expectAtPos: -1,
			expectPath:  "",
			expectFound: false,
		},
		{
			name:        "@ at start with no path",
			input:       "@",
			cursorPos:   1,
			expectAtPos: 0,
			expectPath:  "",
			expectFound: true,
		},
		{
			name:        "@ with partial filename",
			input:       "@main",
			cursorPos:   5,
			expectAtPos: 0,
			expectPath:  "main",
			expectFound: true,
		},
		{
			name:        "@ with partial path",
			input:       "@internal/tui",
			cursorPos:   13,
			expectAtPos: 0,
			expectPath:  "internal/tui",
			expectFound: true,
		},
		{
			name:        "@ in middle of text",
			input:       "read file @cmd/statcode",
			cursorPos:   23,
			expectAtPos: 10,
			expectPath:  "cmd/statcode",
			expectFound: true,
		},
		{
			name:        "@ after space",
			input:       "check @internal/",
			cursorPos:   16,
			expectAtPos: 6,
			expectPath:  "internal/",
			expectFound: true,
		},
		{
			name:        "cursor before @ (space boundary)",
			input:       "hello @world",
			cursorPos:   5,
			expectAtPos: -1,
			expectPath:  "",
			expectFound: false,
		},
		{
			name:        "multiple @ symbols, use closest",
			input:       "@foo @bar/baz",
			cursorPos:   13,
			expectAtPos: 5,
			expectPath:  "bar/baz",
			expectFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			atPos, path, found := extractFilepathContext(tt.input, tt.cursorPos)

			if atPos != tt.expectAtPos {
				t.Errorf("expected atPos %d, got %d", tt.expectAtPos, atPos)
			}
			if path != tt.expectPath {
				t.Errorf("expected path %q, got %q", tt.expectPath, path)
			}
			if found != tt.expectFound {
				t.Errorf("expected found %v, got %v", tt.expectFound, found)
			}
		})
	}
}

func TestFilepathAutocompleteSuggestions(t *testing.T) {
	// Create a mock filesystem with some test files
	mockFS := &MockFS{
		files: map[string][]byte{
			"/test/main.go":         []byte("package main"),
			"/test/README.md":       []byte("# Test"),
			"/test/internal/app.go": []byte("package internal"),
			"/test/internal/tui.go": []byte("package internal"),
			"/test/cmd/cli/main.go": []byte("package main"),
			"/test/.gitignore":      []byte("*.log"),
		},
		dirs: map[string][]string{
			"/test":          {"main.go", "README.md", "internal", "cmd", ".gitignore"},
			"/test/internal": {"app.go", "tui.go"},
			"/test/cmd":      {"cli"},
			"/test/cmd/cli":  {"main.go"},
		},
	}

	m := New("test-model", "", false)
	m.SetFilesystem(mockFS, "/test")

	tests := []struct {
		name          string
		partialPath   string
		expectContain []string
		expectExclude []string
	}{
		{
			name:          "empty path lists current dir (no hidden)",
			partialPath:   "",
			expectContain: []string{"main.go", "README.md", "internal/", "cmd/"},
			expectExclude: []string{".gitignore"}, // Hidden files excluded by default
		},
		{
			name:          "partial filename searches recursively",
			partialPath:   "ma",
			expectContain: []string{"main.go", "cmd/cli/main.go"},
			expectExclude: []string{"README.md", "internal/"},
		},
		{
			name:          "partial filename matches in subdirs",
			partialPath:   "tui",
			expectContain: []string{"internal/tui.go"},
			expectExclude: []string{"main.go", "app.go"},
		},
		{
			name:          "partial filename matches in current and subdirs",
			partialPath:   "app",
			expectContain: []string{"internal/app.go"},
			expectExclude: []string{"main.go"},
		},
		{
			name:          "directory with slash",
			partialPath:   "internal/",
			expectContain: []string{"internal/app.go", "internal/tui.go"},
			expectExclude: []string{"main.go"},
		},
		{
			name:          "nested path prefix",
			partialPath:   "cmd/",
			expectContain: []string{"cmd/cli/"},
			expectExclude: []string{"main.go"},
		},
		{
			name:          "nested path with filename",
			partialPath:   "cmd/cli/ma",
			expectContain: []string{"cmd/cli/main.go"},
			expectExclude: []string{"README.md"},
		},
		{
			name:          "dot prefix includes hidden",
			partialPath:   ".",
			expectContain: []string{".gitignore"},
			expectExclude: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions := m.getFilepathSuggestions(tt.partialPath)

			for _, expected := range tt.expectContain {
				found := false
				for _, sugg := range suggestions {
					if sugg == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected suggestion %q not found in %v", expected, suggestions)
				}
			}

			for _, excluded := range tt.expectExclude {
				for _, sugg := range suggestions {
					if sugg == excluded {
						t.Errorf("unexpected suggestion %q found in %v", excluded, suggestions)
					}
				}
			}
		})
	}
}

func TestFilepathAutocompleteIntegration(t *testing.T) {
	mockFS := &MockFS{
		files: map[string][]byte{
			"/test/main.go":     []byte("package main"),
			"/test/config.yaml": []byte("key: value"),
		},
		dirs: map[string][]string{
			"/test": {"main.go", "config.yaml"},
		},
	}

	m := New("test-model", "", false)
	m.SetFilesystem(mockFS, "/test")
	m.textarea.SetValue("read @ma")

	// Update suggestions
	m.updateSuggestions()

	// Should have suggestions for files starting with "ma"
	if len(m.suggestions) == 0 {
		t.Fatal("expected suggestions for @ma")
	}

	// Should include main.go
	found := false
	for _, sugg := range m.suggestions {
		if sugg == "main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'main.go' in suggestions, got %v", m.suggestions)
	}

	// Simulate Tab key to autocomplete
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	// Check that textarea value was updated with @main.go
	if m.textarea.Value() != "read @main.go" {
		t.Errorf("expected textarea value 'read @main.go', got %q", m.textarea.Value())
	}

	// Suggestions should still be available for cycling
	if len(m.suggestions) == 0 {
		t.Error("expected suggestions to remain for cycling")
	}
}

func TestFilepathAutocompleteCycling(t *testing.T) {
	mockFS := &MockFS{
		files: map[string][]byte{
			"/test/main.go":    []byte("package main"),
			"/test/models.go":  []byte("package models"),
			"/test/manager.go": []byte("package manager"),
		},
		dirs: map[string][]string{
			"/test": {"main.go", "models.go", "manager.go"},
		},
	}

	m := New("test-model", "", false)
	m.SetFilesystem(mockFS, "/test")
	m.textarea.SetValue("check @ma")

	// Update suggestions
	m.updateSuggestions()

	if len(m.suggestions) < 2 {
		t.Fatalf("expected at least 2 suggestions for @ma, got %d: %v", len(m.suggestions), m.suggestions)
	}

	expectedFiles := []string{"main.go", "manager.go", "models.go"}

	// First tab - should apply first match
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	firstValue := m.textarea.Value()
	if !containsAnyFile(firstValue, expectedFiles) {
		t.Errorf("first tab: expected one of %v in %q", expectedFiles, firstValue)
	}

	// Second tab - should cycle to next match
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	secondValue := m.textarea.Value()
	if secondValue == firstValue {
		t.Error("second tab should cycle to different file")
	}
	if !containsAnyFile(secondValue, expectedFiles) {
		t.Errorf("second tab: expected one of %v in %q", expectedFiles, secondValue)
	}

	// Third tab - should cycle (might wrap around if only 2 suggestions)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(*Model)

	thirdValue := m.textarea.Value()
	// If we have 3+ suggestions, third should be different from first two
	// If we have exactly 2 suggestions, third will wrap to first
	if len(m.originalSuggestions) >= 3 {
		if thirdValue == firstValue || thirdValue == secondValue {
			t.Error("third tab should cycle to different file when 3+ suggestions exist")
		}
	} else if len(m.originalSuggestions) == 2 {
		if thirdValue != firstValue {
			t.Errorf("third tab should wrap to first file with 2 suggestions, got %q instead of %q", thirdValue, firstValue)
		}
	}
}

func TestEnterKeyAppliesSelectedSuggestion(t *testing.T) {
	mockFS := &MockFS{
		files: map[string][]byte{
			"/test/main.go": []byte("package main"),
		},
		dirs: map[string][]string{
			"/test": {"main.go"},
		},
	}

	m := New("test-model", "", false)
	m.SetFilesystem(mockFS, "/test")

	// Set up the scenario properly by simulating what would happen in a real interaction
	m.textarea.SetValue("read @ma")

	// Update suggestions to populate them
	m.updateSuggestions()

	if len(m.suggestions) == 0 {
		t.Fatal("expected suggestions for @ma")
	}

	// Print debug info
	t.Logf("Input: %q", m.textarea.Value())
	t.Logf("Suggestions: %v", m.suggestions)
	t.Logf("Selected index: %d", m.selectedSuggIndex)

	// Select the first suggestion
	m.selectedSuggIndex = 0

	// Simulate Enter key press to apply the selected suggestion
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(*Model)

	// Print debug info after Enter
	t.Logf("After Enter - Input: %q", m.textarea.Value())
	t.Logf("After Enter - Suggestions: %v", m.suggestions)

	// Check that textarea value was updated with main.go and suggestions cleared
	if m.textarea.Value() != "read @main.go" {
		t.Errorf("expected textarea value 'read @main.go', got %q", m.textarea.Value())
	}

	if len(m.suggestions) != 0 {
		t.Errorf("expected suggestions to be cleared after Enter, got %v", m.suggestions)
	}
}

func containsAnyFile(s string, files []string) bool {
	for _, f := range files {
		if stringContains(s, f) {
			return true
		}
	}
	return false
}

// MockFS is a simple in-memory filesystem for testing
type MockFS struct {
	files map[string][]byte
	dirs  map[string][]string
}

func (m *MockFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if data, ok := m.files[path]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *MockFS) ReadFileLines(ctx context.Context, path string, from, to int) ([]string, error) {
	return nil, nil
}

func (m *MockFS) WriteFile(ctx context.Context, path string, data []byte) error {
	m.files[path] = data
	return nil
}

func (m *MockFS) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	return nil, nil
}

func (m *MockFS) ListDir(ctx context.Context, path string) ([]*fs.FileInfo, error) {
	entries, ok := m.dirs[path]
	if !ok {
		return nil, os.ErrNotExist
	}

	var result []*fs.FileInfo
	for _, name := range entries {
		fullPath := filepath.Join(path, name)
		isDir := false
		if _, ok := m.dirs[fullPath]; ok {
			isDir = true
		}
		result = append(result, &fs.FileInfo{
			Path:  fullPath,
			IsDir: isDir,
		})
	}
	return result, nil
}

func (m *MockFS) Exists(ctx context.Context, path string) (bool, error) {
	_, ok := m.files[path]
	if ok {
		return true, nil
	}
	_, ok = m.dirs[path]
	return ok, nil
}

func (m *MockFS) Delete(ctx context.Context, path string) error {
	delete(m.files, path)
	return nil
}

func (m *MockFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return nil
}
