package tools

import (
	"testing"
)

func TestApplyJSONOperations(t *testing.T) {
	tests := []struct {
		name        string
		original    string
		operations  []fileOperation
		expected    string
		expectError bool
	}{
		{
			name:     "simple update",
			original: "hello\nworld",
			operations: []fileOperation{
				{Method: "update", Line: 2, LineContent: "gopher"},
			},
			expected: "hello\ngopher",
		},
		{
			name:     "simple insert_before",
			original: "hello\nworld",
			operations: []fileOperation{
				{Method: "insert_before", Line: 2, LineContent: "beautiful"},
			},
			expected: "hello\nbeautiful\nworld",
		},
		{
			name:     "simple insert_after",
			original: "hello\nworld",
			operations: []fileOperation{
				{Method: "insert_after", Line: 1, LineContent: "beautiful"},
			},
			expected: "hello\nbeautiful\nworld",
		},
		{
			name:     "multiple operations",
			original: "a\nb\nc",
			operations: []fileOperation{
				{Method: "insert_after", Line: 1, LineContent: "a.5"},
				{Method: "update", Line: 2, LineContent: "B"},
				{Method: "insert_before", Line: 3, LineContent: "c-pre"},
			},
			expected: "a\na.5\nB\nc-pre\nc",
		},
		{
			name:     "multiple inserts on same line",
			original: "a\nb",
			operations: []fileOperation{
				{Method: "insert_before", Line: 2, LineContent: "pre1"},
				{Method: "insert_before", Line: 2, LineContent: "pre2"},
				{Method: "insert_after", Line: 1, LineContent: "post1"},
				{Method: "insert_after", Line: 1, LineContent: "post2"},
			},
			expected: "a\npost1\npost2\npre1\npre2\nb",
		},
		{
			name:     "update and insert",
			original: "a\nb",
			operations: []fileOperation{
				{Method: "insert_before", Line: 2, LineContent: "pre"},
				{Method: "update", Line: 2, LineContent: "B"},
				{Method: "insert_after", Line: 2, LineContent: "post"},
			},
			expected: "a\npre\nB\npost",
		},
		{
			name:        "line out of bounds",
			original:    "a\nb",
			operations:  []fileOperation{{Method: "update", Line: 3, LineContent: "C"}},
			expectError: true,
		},
		{
			name:        "line out of bounds (zero)",
			original:    "a\nb",
			operations:  []fileOperation{{Method: "update", Line: 0, LineContent: "C"}},
			expectError: true,
		},
		{
			name:        "unknown method",
			original:    "a\nb",
			operations:  []fileOperation{{Method: "delete", Line: 1, LineContent: ""}},
			expectError: true,
		},
		{
			name:     "insert at first line",
			original: "a\nb",
			operations: []fileOperation{
				{Method: "insert_before", Line: 1, LineContent: "line 0"},
			},
			expected: "line 0\na\nb",
		},
		{
			name:     "insert at last line",
			original: "a\nb",
			operations: []fileOperation{
				{Method: "insert_after", Line: 2, LineContent: "line 3"},
			},
			expected: "a\nb\nline 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := applyJSONOperations(tt.original, tt.operations)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("expected %q, but got %q", tt.expected, result)
				}
			}
		})
	}
}
