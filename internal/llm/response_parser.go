package llm

import (
	"encoding/json"
	"strings"
)

// CleanLLMJSONResponse removes common formatting from LLM JSON responses.
// It handles:
// - Markdown code blocks (```json or ```)
// - XML-style tags (<tag>content</tag>)
// - Leading/trailing whitespace
func CleanLLMJSONResponse(response string) string {
	response = strings.TrimSpace(response)

	// Remove markdown code blocks
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Remove XML-style tags (e.g., <verification_result>content</verification_result>)
	response = extractFromXMLTags(response)

	return strings.TrimSpace(response)
}

// extractFromXMLTags removes the outermost XML-style tags from content.
// For example: "<tag>content</tag>" becomes "content"
// Handles attributes: "<tag attr="value">content</tag>" becomes "content"
func extractFromXMLTags(content string) string {
	// Find opening tag
	openStart := strings.Index(content, "<")
	if openStart == -1 {
		return content
	}

	// Find the end of the opening tag (first '>' after '<')
	openEnd := strings.Index(content[openStart:], ">")
	if openEnd == -1 {
		return content
	}
	openEnd += openStart + 1

	// Find the tag name by extracting text between '<' and first space or '>'
	openTagContent := content[openStart+1 : openEnd-1]
	spaceIdx := strings.Index(openTagContent, " ")
	var tagName string
	if spaceIdx == -1 {
		tagName = openTagContent
	} else {
		tagName = openTagContent[:spaceIdx]
	}

	// Find closing tag (look for matching tag name)
	closingTag := "</" + tagName + ">"
	closeStart := strings.Index(content, closingTag)

	if closeStart == -1 {
		return content
	}

	// Extract content between the end of opening tag and start of closing tag
	if closeStart > openEnd {
		return content[openEnd:closeStart]
	}

	return content
}

// ParseLLMJSONResponse parses a JSON response from an LLM, cleaning it first.
// Returns an error if parsing fails.
func ParseLLMJSONResponse(response string, target interface{}) error {
	cleaned := CleanLLMJSONResponse(response)
	if err := json.Unmarshal([]byte(cleaned), target); err == nil {
		return nil
	}
	return ExtractJSON(response, target)
}

// ExtractJSONArray attempts to extract and parse a JSON array from a response.
// It tries multiple strategies:
// 1. Direct parse of cleaned response
// 2. Extract content between [ and ] brackets
// Returns the parsed array or an error.
func ExtractJSONArray[T any](response string) ([]T, error) {
	trimmed := strings.TrimSpace(response)

	// Strategy 1: Try direct parsing
	cleaned := CleanLLMJSONResponse(trimmed)
	var result []T
	if err := json.Unmarshal([]byte(cleaned), &result); err == nil {
		return result, nil
	}

	// Strategy 2: Extract content between brackets
	start := strings.Index(trimmed, "[")
	end := strings.LastIndex(trimmed, "]")
	if start >= 0 && end > start {
		snippet := trimmed[start : end+1]
		if err := json.Unmarshal([]byte(snippet), &result); err == nil {
			return result, nil
		}
	}

	return nil, &JSONParseError{Response: response, Message: "could not parse JSON array"}
}

// ExtractJSON extracts a JSON object from a response using flexible strategies.
// It tries:
// 1. Direct parse of cleaned response
// 2. Extract content between { and } braces
// Returns the parsed object or an error.
func ExtractJSON[T any](response string, target T) error {
	trimmed := strings.TrimSpace(response)

	// Strategy 1: Try direct parsing
	cleaned := CleanLLMJSONResponse(trimmed)
	if err := json.Unmarshal([]byte(cleaned), target); err == nil {
		return nil
	}

	// Strategy 2: Extract content between braces
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		snippet := trimmed[start : end+1]
		if err := json.Unmarshal([]byte(snippet), target); err == nil {
			return nil
		}
	}

	return &JSONParseError{Response: response, Message: "could not parse JSON object"}
}

// JSONParseError represents an error that occurred while parsing LLM JSON response.
type JSONParseError struct {
	Response string
	Message  string
}

func (e *JSONParseError) Error() string {
	return e.Message + ": " + TruncateForError(e.Response, 200)
}

// TruncateForError truncates a string for error messages.
func TruncateForError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "..."
}
