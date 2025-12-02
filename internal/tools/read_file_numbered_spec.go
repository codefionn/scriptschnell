package tools

// ReadFileNumberedSpec represents the static specification of the read_file tool.
// This spec is used for LLM schema generation and does not require runtime dependencies.
type ReadFileNumberedSpec struct{}

func (s *ReadFileNumberedSpec) Name() string {
	return ToolNameReadFile
}

func (s *ReadFileNumberedSpec) Description() string {
	return "Read a file from the filesystem (format: [padded line number][space][line]). Can read entire file or multiple specific line ranges using the sections parameter (max 2000 lines)."
}

func (s *ReadFileNumberedSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read (relative to working directory)",
			},
			"sections": map[string]interface{}{
				"type":        "array",
				"description": "Array of line range sections to read (e.g., [{\"from_line\": 1, \"to_line\": 10}, {\"from_line\": 50, \"to_line\": 60}]). Total lines across all sections cannot exceed 2000. Omit to read entire file (up to 2000 lines).",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"from_line": map[string]interface{}{
							"type":        "integer",
							"description": "Starting line number (1-indexed)",
						},
						"to_line": map[string]interface{}{
							"type":        "integer",
							"description": "Ending line number (1-indexed)",
						},
					},
					"required": []string{"from_line", "to_line"},
				},
			},
		},
		"required": []string{"path"},
	}
}
