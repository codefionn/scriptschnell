package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/statcode-ai/scriptschnell/internal/fs"
	"github.com/statcode-ai/scriptschnell/internal/session"
)

func TestReadFileTool_Name(t *testing.T) {
	tool := NewReadFileTool(fs.NewMockFS(), nil)
	if tool.Name() != ToolNameReadFile {
		t.Errorf("expected name %s, got %s", ToolNameReadFile, tool.Name())
	}
}

func TestReadFileTool_ReadEntireFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

	content := "line 1\nline 2\nline 3"
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	resultContent := result.Result.(map[string]interface{})["content"].(string)
	if resultContent != content {
		t.Errorf("expected content %q, got %q", content, resultContent)
	}

	lines := result.Result.(map[string]interface{})["lines"].(int)
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d", lines)
	}
}

func TestReadFileTool_ReadSingleSection(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

	content := "line 1\nline 2\nline 3\nline 4\nline 5"
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
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
	expected := "line 2\nline 3\nline 4"
	if resultContent != expected {
		t.Errorf("expected content %q, got %q", expected, resultContent)
	}

	lines := result.Result.(map[string]interface{})["lines"].(int)
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d", lines)
	}
}

func TestReadFileTool_ReadMultipleSections(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

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
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
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
	expected := "[Section 1: lines 1-2]\nline 1\nline 2\n[Section 2: lines 5-6]\nline 5\nline 6\n[Section 3: lines 9-10]\nline 9\nline 10"
	if resultContent != expected {
		t.Errorf("expected content:\n%q\ngot:\n%q", expected, resultContent)
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

func TestReadFileTool_TruncateLargeFile(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

	// Create a file with 2500 lines
	var lines []string
	for i := 1; i <= 2500; i++ {
		lines = append(lines, "line content")
	}
	content := strings.Join(lines, "\n")
	_ = mockFS.WriteFile(context.Background(), "large.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
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

func TestReadFileTool_ExceedLineLimitAcrossSections(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

	// Create a file with enough lines
	var lines []string
	for i := 1; i <= 2500; i++ {
		lines = append(lines, "line content")
	}
	content := strings.Join(lines, "\n")
	_ = mockFS.WriteFile(context.Background(), "large.txt", []byte(content))

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "large.txt",
		"sections": []interface{}{
			map[string]interface{}{
				"from_line": 1,
				"to_line":   1500,
			},
			map[string]interface{}{
				"from_line": 2000,
				"to_line":   2501,
			},
		},
	})

	if result.Error == "" {
		t.Fatal("expected error for exceeding line limit")
	}

	if !strings.Contains(result.Error, "cannot read more than 2000 lines") {
		t.Errorf("expected line limit error, got: %s", result.Error)
	}
}

func TestReadFileTool_InvalidSections(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte("line 1\nline 2"))

	tests := []struct {
		name     string
		sections interface{}
		errMsg   string
	}{
		{
			name:     "empty sections array",
			sections: []interface{}{},
			errMsg:   "sections array cannot be empty",
		},
		{
			name:     "sections not an array",
			sections: "invalid",
			errMsg:   "sections parameter must be an array",
		},
		{
			name: "from_line greater than to_line",
			sections: []interface{}{
				map[string]interface{}{
					"from_line": 5,
					"to_line":   2,
				},
			},
			errMsg: "from_line (5) cannot be greater than to_line (2)",
		},
		{
			name: "invalid from_line",
			sections: []interface{}{
				map[string]interface{}{
					"from_line": 0,
					"to_line":   2,
				},
			},
			errMsg: "from_line and to_line must be positive integers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), map[string]interface{}{
				"path":     "test.txt",
				"sections": tt.sections,
			})

			if result.Error == "" {
				t.Fatal("expected error")
			}

			if !strings.Contains(result.Error, tt.errMsg) {
				t.Errorf("expected error containing %q, got: %s", tt.errMsg, result.Error)
			}
		})
	}
}

func TestReadFileTool_FileNotFound(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "nonexistent.txt",
	})

	if result.Error == "" {
		t.Fatal("expected error for nonexistent file")
	}

	if !strings.Contains(result.Error, "file not found") {
		t.Errorf("expected 'file not found' error, got: %s", result.Error)
	}
}

func TestReadFileTool_TracksFileInSession(t *testing.T) {
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test-session", ".")
	tool := NewReadFileTool(mockFS, sess)

	content := "test content"
	_ = mockFS.WriteFile(context.Background(), "test.txt", []byte(content))

	tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})

	if !sess.WasFileRead("test.txt") {
		t.Error("expected file to be tracked in session")
	}
}
