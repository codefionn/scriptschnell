package tui

import (
	"strings"
	"testing"
)

func TestParamsRenderer_FormatCompactParams(t *testing.T) {
	pr := NewParamsRenderer()

	tests := []struct {
		name      string
		params    map[string]interface{}
		toolName  string
		wantCount int // Number of lines expected
	}{
		{
			name:      "empty params",
			params:    map[string]interface{}{},
			toolName:  "test",
			wantCount: 1, // "No parameters"
		},
		{
			name: "single param",
			params: map[string]interface{}{
				"path": "/tmp/test.txt",
			},
			toolName:  "read_file",
			wantCount: 1,
		},
		{
			name: "multiple params",
			params: map[string]interface{}{
				"path":      "/tmp/test.txt",
				"from_line": 1,
				"to_line":   100,
			},
			toolName:  "read_file",
			wantCount: 3,
		},
		{
			name: "boolean param",
			params: map[string]interface{}{
				"recursive": true,
			},
			toolName:  "search_files",
			wantCount: 1,
		},
		{
			name: "array param",
			params: map[string]interface{}{
				"queries": []interface{}{"test1", "test2", "test3"},
			},
			toolName:  "web_search",
			wantCount: 1,
		},
		{
			name: "object param",
			params: map[string]interface{}{
				"options": map[string]interface{}{
					"verbose": true,
					"timeout": 30,
				},
			},
			toolName:  "shell",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pr.FormatCompactParams(tt.params, tt.toolName)
			if result == "" {
				t.Error("FormatCompactParams returned empty string")
			}

			// Count newlines to verify line count
			lineCount := 1
			for _, c := range result {
				if c == '\n' {
					lineCount++
				}
			}

			if lineCount != tt.wantCount {
				t.Errorf("FormatCompactParams expected %d lines, got %d (result: %q)", tt.wantCount, lineCount, result)
			}
		})
	}
}

func TestParamsRenderer_FormatCompactParamsOneLine(t *testing.T) {
	pr := NewParamsRenderer()

	tests := []struct {
		name      string
		params    map[string]interface{}
		toolName  string
		wantEmpty bool
	}{
		{
			name:      "empty params",
			params:    map[string]interface{}{},
			toolName:  "test",
			wantEmpty: true,
		},
		{
			name: "read_file with path",
			params: map[string]interface{}{
				"path": "/tmp/test.txt",
			},
			toolName:  "read_file",
			wantEmpty: false,
		},
		{
			name: "shell with command",
			params: map[string]interface{}{
				"command": []interface{}{"ls", "-la"},
			},
			toolName:  "shell",
			wantEmpty: false,
		},
		{
			name: "web_search with query",
			params: map[string]interface{}{
				"query": "golang testing",
			},
			toolName:  "web_search",
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pr.FormatCompactParamsOneLine(tt.params, tt.toolName)
			if tt.wantEmpty && result != "" {
				t.Errorf("FormatCompactParamsOneLine expected empty, got %q", result)
			}
			if !tt.wantEmpty && result == "" {
				t.Error("FormatCompactParamsOneLine expected non-empty, got empty")
			}
		})
	}
}

func TestParamsRenderer_DetectParamType(t *testing.T) {
	pr := NewParamsRenderer()

	tests := []struct {
		key      string
		value    interface{}
		wantType ParamType
	}{
		{"path", "/tmp/test", ParamTypePath},
		{"file", "test.txt", ParamTypePath},
		{"directory", "/home/user", ParamTypePath},
		{"command", "ls -la", ParamTypeCommand},
		{"cmd", "echo test", ParamTypeCommand},
		{"url", "https://example.com", ParamTypeURL},
		{"query", "search term", ParamTypeQuery},
		{"code", "func main() {}", ParamTypeCode},
		{"count", 42, ParamTypeNumber},
		{"enabled", true, ParamTypeBoolean},
		{"items", []interface{}{1, 2, 3}, ParamTypeArray},
		{"config", map[string]interface{}{"key": "value"}, ParamTypeObject},
		{"description", "a text description", ParamTypeString},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := pr.detectParamType(tt.key, tt.value)
			if got != tt.wantType {
				t.Errorf("detectParamType(%q, %v) = %v, want %v", tt.key, tt.value, got, tt.wantType)
			}
		})
	}
}

func TestParamsRenderer_GetParamColor(t *testing.T) {
	pr := NewParamsRenderer()

	colors := map[ParamType]string{
		ParamTypePath:    ColorParamPath,
		ParamTypeCommand: ColorParamCommand,
		ParamTypeURL:     ColorParamURL,
		ParamTypeQuery:   ColorParamQuery,
		ParamTypeCode:    ColorParamCode,
		ParamTypeNumber:  ColorParamNumber,
		ParamTypeBoolean: ColorParamBoolean,
		ParamTypeArray:   ColorParamArray,
		ParamTypeObject:  ColorParamObject,
		ParamTypeString:  ColorParamString,
	}

	for pt, expectedColor := range colors {
		got := pr.getParamColor(pt)
		if got != expectedColor {
			t.Errorf("getParamColor(%v) = %q, want %q", pt, got, expectedColor)
		}
	}
}

func TestParamsRenderer_FormatParamValue(t *testing.T) {
	pr := NewParamsRenderer()

	tests := []struct {
		name     string
		value    interface{}
		maxLen   int
		contains string
	}{
		{"boolean true", true, 10, "true"},
		{"boolean false", false, 10, "false"},
		{"integer", 42, 10, "42"},
		{"float", 3.14, 10, "3.14"},
		{"string short", "hello", 10, "hello"},
		{"string long", "this is a very long string that should be truncated", 20, "..."},
		{"array", []interface{}{1, 2, 3, 4, 5}, 20, "[5 items]"},
		{"object", map[string]interface{}{"a": 1, "b": 2}, 20, "{2 keys}"},
		{"nil", nil, 10, "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pr.formatParamValue(tt.value, ParamTypeString, tt.maxLen)
			if len(got) > tt.maxLen {
				t.Errorf("formatParamValue result too long: %q (max %d)", got, tt.maxLen)
			}
		})
	}
}

func TestParamsRenderer_FormatParamSummary(t *testing.T) {
	pr := NewParamsRenderer()

	// Empty params
	got := pr.FormatParamSummary(map[string]interface{}{})
	if got != "no params" {
		t.Errorf("FormatParamSummary(empty) = %q, want %q", got, "no params")
	}

	// Single param
	got = pr.FormatParamSummary(map[string]interface{}{"path": "/tmp"})
	if got != "path" {
		t.Errorf("FormatParamSummary(single) = %q, want %q", got, "path")
	}

	// Two params - order may vary due to map iteration
	got = pr.FormatParamSummary(map[string]interface{}{"path": "/tmp", "line": 1})
	if !strings.Contains(got, "path") || !strings.Contains(got, "line") {
		t.Errorf("FormatParamSummary(two) = %q, should contain 'path' and 'line'", got)
	}

	// Three params - order may vary
	got = pr.FormatParamSummary(map[string]interface{}{"a": 1, "b": 2, "c": 3})
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || !strings.Contains(got, "c") {
		t.Errorf("FormatParamSummary(three) = %q, should contain 'a', 'b', and 'c'", got)
	}

	// More than three params
	got = pr.FormatParamSummary(map[string]interface{}{"a": 1, "b": 2, "c": 3, "d": 4})
	// Map iteration order is non-deterministic, so just check format
	if !strings.Contains(got, "+ 1 more") {
		t.Errorf("FormatParamSummary(four) = %q, should contain '+ 1 more'", got)
	}
}
