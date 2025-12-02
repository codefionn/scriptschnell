package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

func TestReadFileNumberedSpec_Name(t *testing.T) {
	spec := &ReadFileNumberedSpec{}
	if spec.Name() != ToolNameReadFile {
		t.Errorf("expected name %s, got %s", ToolNameReadFile, spec.Name())
	}
}

func TestReadFileNumberedExecutor_ReadEntireFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	executor := NewReadFileNumberedExecutor(mockFS, sess)

	content := "line 1\nline 2\nline 3"
	mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultContent := result.Result.(map[string]interface{})["content"].(string)

	// Should contain format notice
	if !strings.Contains(resultContent, "[Line format: [padded line number] [line]]") {
		t.Error("expected format notice")
	}

	// Should contain numbered lines
	if !strings.Contains(resultContent, "1 line 1") {
		t.Error("expected numbered line 1")
	}
	if !strings.Contains(resultContent, "2 line 2") {
		t.Error("expected numbered line 2")
	}
	if !strings.Contains(resultContent, "3 line 3") {
		t.Error("expected numbered line 3")
	}

	lines := result.Result.(map[string]interface{})["lines"].(int)
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d", lines)
	}
}

func TestReadFileNumberedExecutor_ReadSingleSection(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	executor := NewReadFileNumberedExecutor(mockFS, sess)

	content := "line 1\nline 2\nline 3\nline 4\nline 5"
	mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"sections": []interface{}{
			map[string]interface{}{
				"from_line": 2,
				"to_line":   4,
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultContent := result.Result.(map[string]interface{})["content"].(string)

	// Should contain numbered lines starting from 2
	if !strings.Contains(resultContent, "2 line 2") {
		t.Error("expected numbered line 2")
	}
	if !strings.Contains(resultContent, "3 line 3") {
		t.Error("expected numbered line 3")
	}
	if !strings.Contains(resultContent, "4 line 4") {
		t.Error("expected numbered line 4")
	}

	// Should NOT contain line 1 or line 5
	if strings.Contains(resultContent, "1 line 1") {
		t.Error("should not contain line 1")
	}
	if strings.Contains(resultContent, "5 line 5") {
		t.Error("should not contain line 5")
	}

	lines := result.Result.(map[string]interface{})["lines"].(int)
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d", lines)
	}
}

func TestReadFileNumberedTool_ReadMultipleSections(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	executor := NewReadFileNumberedExecutor(mockFS, sess)

	content := strings.Join([]string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
		"line 6",
		"line 7",
		"line 8",
		"line 9",
		"line 10",
	}, "\n")
	mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"sections": []interface{}{
			map[string]interface{}{
				"from_line": 1,
				"to_line":   2,
			},
			map[string]interface{}{
				"from_line": 5,
				"to_line":   6,
			},
			map[string]interface{}{
				"from_line": 9,
				"to_line":   10,
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultContent := result.Result.(map[string]interface{})["content"].(string)

	// Should contain section headers
	if !strings.Contains(resultContent, "[Section 1: lines 1-2]") {
		t.Error("expected section 1 header")
	}
	if !strings.Contains(resultContent, "[Section 2: lines 5-6]") {
		t.Error("expected section 2 header")
	}
	if !strings.Contains(resultContent, "[Section 3: lines 9-10]") {
		t.Error("expected section 3 header")
	}

	// Should contain numbered lines
	if !strings.Contains(resultContent, "1 line 1") {
		t.Error("expected numbered line 1")
	}
	if !strings.Contains(resultContent, "5 line 5") {
		t.Error("expected numbered line 5")
	}
	if !strings.Contains(resultContent, "9 line 9") {
		t.Error("expected numbered line 9")
	}

	lines := result.Result.(map[string]interface{})["lines"].(int)
	if lines != 6 {
		t.Errorf("expected 6 lines, got %d", lines)
	}

	sections := result.Result.(map[string]interface{})["sections"].(int)
	if sections != 3 {
		t.Errorf("expected 3 sections, got %d", sections)
	}
}

func TestReadFileNumberedTool_LinePadding(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	executor := NewReadFileNumberedExecutor(mockFS, sess)

	// Create a file with 100 lines to test padding
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, "content")
	}
	content := strings.Join(lines, "\n")
	mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
		"sections": []interface{}{
			map[string]interface{}{
				"from_line": 1,
				"to_line":   100,
			},
		},
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultContent := result.Result.(map[string]interface{})["content"].(string)

	// Line numbers should be padded to 3 digits (100 is max)
	// First line should have padding: "  1 content"
	if !strings.Contains(resultContent, "  1 content") {
		t.Error("expected padded line 1")
	}

	// Line 100 should not have padding: "100 content"
	if !strings.Contains(resultContent, "100 content") {
		t.Error("expected line 100")
	}
}

func TestReadFileNumberedTool_TruncateLargeFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	executor := NewReadFileNumberedExecutor(mockFS, sess)

	// Create a file with 2500 lines
	var lines []string
	for i := 1; i <= 2500; i++ {
		lines = append(lines, "line content")
	}
	content := strings.Join(lines, "\n")
	mockFS.WriteFile(context.Background(), "large.txt", []byte(content))

	result := executor.Execute(context.Background(), map[string]interface{}{
		"path": "large.txt",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultContent := result.Result.(map[string]interface{})["content"].(string)
	if !strings.Contains(resultContent, "file truncated") {
		t.Error("expected truncation message")
	}

	if !strings.Contains(resultContent, "2500 total lines") {
		t.Error("expected total line count in truncation message")
	}

	if !strings.Contains(resultContent, "Use sections parameter") {
		t.Error("expected sections parameter suggestion in truncation message")
	}
}

func TestReadFileNumberedTool_TracksFileInSession(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	executor := NewReadFileNumberedExecutor(mockFS, sess)

	content := "test content"
	mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	executor.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})

	if !sess.WasFileRead("test.txt") {
		t.Error("expected file to be tracked in session")
	}
}

func TestFormatLinesWithNumbers(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		start    int
		expected string
	}{
		{
			name:     "single digit line numbers",
			lines:    []string{"first", "second", "third"},
			start:    1,
			expected: "1 first\n2 second\n3 third",
		},
		{
			name:     "double digit line numbers",
			lines:    []string{"line a", "line b"},
			start:    10,
			expected: "10 line a\n11 line b",
		},
		{
			name:     "padding for alignment",
			lines:    []string{"a", "b", "c"},
			start:    98,
			expected: " 98 a\n 99 b\n100 c",
		},
		{
			name:     "empty lines array",
			lines:    []string{},
			start:    1,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLinesWithNumbers(tt.lines, tt.start)
			if result != tt.expected {
				t.Errorf("expected:\n%q\ngot:\n%q", tt.expected, result)
			}
		})
	}
}

func TestSplitPreserveLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "normal content",
			content:  "line 1\nline 2\nline 3",
			expected: []string{"line 1", "line 2", "line 3"},
		},
		{
			name:     "empty string",
			content:  "",
			expected: []string{""},
		},
		{
			name:     "single line",
			content:  "only one line",
			expected: []string{"only one line"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitPreserveLines(tt.content)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d lines, got %d", len(tt.expected), len(result))
			}
			for i, line := range result {
				if line != tt.expected[i] {
					t.Errorf("line %d: expected %q, got %q", i, tt.expected[i], line)
				}
			}
		})
	}
}

func TestPrependFormatNotice(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "normal content",
			content:  "1 line one",
			expected: "[Line format: [padded line number] [line]]\n1 line one",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "[Line format: [padded line number] [line]]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prependFormatNotice(tt.content)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
