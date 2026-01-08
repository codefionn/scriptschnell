package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/secretdetect"
)

// paramsToString converts tool parameters to a scannable string format.
// This is used for secret detection before tool execution.
func paramsToString(params map[string]interface{}) string {
	if len(params) == 0 {
		return ""
	}

	var parts []string
	for key, value := range params {
		parts = append(parts, fmt.Sprintf("%s=%s", key, valueToString(value)))
	}

	return strings.Join(parts, " ")
}

// valueToString converts a parameter value to string for scanning.
// Handles nested structures, arrays, and special types.
func valueToString(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return fmt.Sprintf("%v", v)
	case []interface{}:
		var parts []string
		for _, item := range v {
			parts = append(parts, valueToString(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]interface{}:
		var parts []string
		for k, val := range v {
			parts = append(parts, fmt.Sprintf("%s:%s", k, valueToString(val)))
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		// For complex types, use JSON serialization as fallback
		if jsonBytes, err := json.Marshal(value); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("%v", value)
	}
}

// hasSecrets checks if the given content contains any secrets using the detector.
func hasSecrets(detector secretdetect.Detector, content string) bool {
	if detector == nil || content == "" {
		return false
	}

	matches := detector.Scan(content)
	return len(matches) > 0
}

// extractSecrets extracts and returns detected secrets from content.
func extractSecrets(detector secretdetect.Detector, content string) []secretdetect.SecretMatch {
	if detector == nil || content == "" {
		return nil
	}

	return detector.Scan(content)
}

// shouldScanTool determines if a tool should be scanned for secrets.
// By default, all tools are scanned, but this can be refined based on tool type.
func shouldScanTool(toolName string) bool {
	// Define tools that are less likely to contain legitimate secrets
	// and can be skipped to reduce false positives
	skipTools := map[string]bool{
		"read_file":           true, // File content is scanned separately if needed
		"list_files":          true, // Just lists filenames
		"todo":                true, // Task management, unlikely to have secrets
		"status":              true, // Job status, unlikely to have secrets
		"help":                true, // Help content, no secrets
		"parallel_tools":      true, // Just coordinates other tools
		"tool_summarize":      true, // Just summarizes output
		"read_file_summarized": true, // Just summarizes files
	}

	return !skipTools[toolName]
}