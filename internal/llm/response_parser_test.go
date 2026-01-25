package llm

import (
	"strings"
	"testing"
)

func TestCleanLLMJSONResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain JSON",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with json markdown block",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with plain markdown block",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with surrounding whitespace",
			input:    "  \n  {\"key\": \"value\"}  \n  ",
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with both markdown and whitespace",
			input:    "  ```json  \n  {\"key\": \"value\"}  \n  ```  ",
			expected: `{"key": "value"}`,
		},
		{
			name:     "XML-style tags",
			input:    "<result>{\"key\": \"value\"}</result>",
			expected: `{"key": "value"}`,
		},
		{
			name:     "XML-style tags with markdown",
			input:    "```json\n<result>{\"key\": \"value\"}</result>\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "complex nested tags",
			input:    "<verification_result>{\"success\": true}</verification_result>",
			expected: `{"success": true}`,
		},
		{
			name:     "tags with attributes",
			input:    "<result type=\"json\">{\"key\": \"value\"}</result>",
			expected: `{"key": "value"}`,
		},
		{
			name:     "multiple tags - extracts first outermost",
			input:    "<outer><inner>{\"key\": \"value\"}</inner></outer>",
			expected: `<inner>{"key": "value"}</inner>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CleanLLMJSONResponse(tt.input)
			if result != tt.expected {
				t.Errorf("CleanLLMJSONResponse() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseLLMJSONResponse(t *testing.T) {
	type Result struct {
		Key   string `json:"key"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name    string
		input   string
		want    *Result
		wantErr bool
	}{
		{
			name:  "valid plain JSON",
			input: `{"key": "test", "value": 42}`,
			want:  &Result{Key: "test", Value: 42},
		},
		{
			name:  "valid JSON with markdown",
			input: "```json\n{\"key\": \"test\", \"value\": 42}\n```",
			want:  &Result{Key: "test", Value: 42},
		},
		{
			name:  "valid JSON with XML tags",
			input: "<result>{\"key\": \"test\", \"value\": 42}</result>",
			want:  &Result{Key: "test", Value: 42},
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result Result
			err := ParseLLMJSONResponse(tt.input, &result)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLLMJSONResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != *tt.want {
				t.Errorf("ParseLLMJSONResponse() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestExtractJSONArray(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "plain array",
			input: `["a", "b", "c"]`,
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "array with markdown",
			input: "```json\n[\"a\", \"b\", \"c\"]\n```",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "array with text before and after",
			input: "Here's the list: [\"a\", \"b\", \"c\"] and that's it",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "empty array",
			input: "[]",
			want:  []string{},
		},
		{
			name:    "invalid array",
			input:   "not an array",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractJSONArray[string](tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractJSONArray() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(result) != len(tt.want) {
					t.Errorf("ExtractJSONArray() = %v, want %v", result, tt.want)
					return
				}
				for i, v := range result {
					if v != tt.want[i] {
						t.Errorf("ExtractJSONArray()[%d] = %v, want %v", i, v, tt.want[i])
					}
				}
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	type Result struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	tests := []struct {
		name    string
		input   string
		want    *Result
		wantErr bool
	}{
		{
			name:  "plain object",
			input: `{"name": "test", "count": 5}`,
			want:  &Result{Name: "test", Count: 5},
		},
		{
			name:  "object with markdown",
			input: "```json\n{\"name\": \"test\", \"count\": 5}\n```",
			want:  &Result{Name: "test", Count: 5},
		},
		{
			name:  "object with text before and after",
			input: "Result: {\"name\": \"test\", \"count\": 5} done",
			want:  &Result{Name: "test", Count: 5},
		},
		{
			name:  "object with XML tags",
			input: "<data>{\"name\": \"test\", \"count\": 5}</data>",
			want:  &Result{Name: "test", Count: 5},
		},
		{
			name:    "invalid object",
			input:   "not an object",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result Result
			err := ExtractJSON(tt.input, &result)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != *tt.want {
				t.Errorf("ExtractJSON() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestJSONParseError(t *testing.T) {
	longResponse := strings.Repeat("a", 300)
	err := &JSONParseError{Response: longResponse, Message: "parse failed"}

	errorStr := err.Error()

	// The error should start with the message
	if !strings.HasPrefix(errorStr, "parse failed: ") {
		t.Errorf("JSONParseError.Error() should start with message, got %q", errorStr)
	}

	// The error should end with "..." (truncation marker)
	if !strings.HasSuffix(errorStr, "...") {
		t.Errorf("JSONParseError.Error() should end with truncation marker, got %q", errorStr)
	}

	// The total length should be reasonable (message + ": " + 200 + "...")
	expectedMaxLen := len("parse failed: ") + 200 + len("...")
	if len(errorStr) > expectedMaxLen {
		t.Errorf("JSONParseError.Error() too long: got %d, want <= %d", len(errorStr), expectedMaxLen)
	}
}

func TestExtractFromXMLTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple tags",
			input:    "<tag>content</tag>",
			expected: "content",
		},
		{
			name:     "tags with attributes",
			input:    `<tag attr="value">content</tag>`,
			expected: "content",
		},
		{
			name:     "nested tags - extracts outermost",
			input:    "<outer><inner>content</inner></outer>",
			expected: "<inner>content</inner>",
		},
		{
			name:     "no tags",
			input:    "plain content",
			expected: "plain content",
		},
		{
			name:     "incomplete tags",
			input:    "<tag>content",
			expected: "<tag>content",
		},
		{
			name:     "only opening tag",
			input:    "<tag>",
			expected: "<tag>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFromXMLTags(tt.input)
			if result != tt.expected {
				t.Errorf("extractFromXMLTags() = %q, want %q", result, tt.expected)
			}
		})
	}
}
