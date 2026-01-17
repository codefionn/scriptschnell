package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

// WriteFileReplaceToolSpec is the static specification for the write_file_replace tool
type WriteFileReplaceToolSpec struct{}

func (s *WriteFileReplaceToolSpec) Name() string {
	return ToolNameEditFile
}

func (s *WriteFileReplaceToolSpec) Description() string {
	return `Update an existing file by replacing text. Either provide a single old_string/new_string pair, or supply an edits array to batch multiple replacements in the same file. 
Ensure every old_string matches exactly (including whitespace and indentation). 
Be careful around opening and closing brackets (e.g. '{' and '}' in C like languages) when editing code.`
}

func (s *WriteFileReplaceToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to update (relative to working directory)",
			},
			"edits": map[string]interface{}{
				"type":        "array",
				"description": "Optional list of replacements to apply in order. Use this to perform multiple edits in one call.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"old_string": map[string]interface{}{
							"type":        "string",
							"description": "Exact text to replace. Must be present in the file for this edit.",
						},
						"new_string": map[string]interface{}{
							"type":        "string",
							"description": "Replacement text. Empty string deletes the match.",
						},
						"expected_replacements": map[string]interface{}{
							"type":        "integer",
							"description": "How many times old_string should appear for this edit. Defaults to 1 if omitted.",
						},
					},
					"required": []string{"old_string", "new_string"},
				},
			},
		},
		"required": []string{"path", "edits"},
	}
}

func (s *WriteFileReplaceToolSpec) RequiresExclusiveExecution() bool { return true }

// WriteFileReplaceTool is the executor with runtime dependencies.
type WriteFileReplaceTool struct {
	fs      fs.FileSystem
	session *session.Session
}

type replacementEdit struct {
	OldString            string `json:"old_string"`
	NewString            string `json:"new_string"`
	ExpectedReplacements int    `json:"expected_replacements"`
}

func NewWriteFileReplaceTool(filesystem fs.FileSystem, sess *session.Session) *WriteFileReplaceTool {
	return &WriteFileReplaceTool{
		fs:      filesystem,
		session: sess,
	}
}

func (t *WriteFileReplaceTool) Name() string { return ToolNameEditFile }
func (t *WriteFileReplaceTool) Description() string {
	return (&WriteFileReplaceToolSpec{}).Description()
}
func (t *WriteFileReplaceTool) Parameters() map[string]interface{} {
	return (&WriteFileReplaceToolSpec{}).Parameters()
}

func (t *WriteFileReplaceTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "tool call validation failed for write_file_replace: missing required parameter 'path'"}
	}

	edits, err := parseReplacementEdits(params)
	if err != nil {
		return &ToolResult{Error: err.Error()}
	}

	logger.Debug("edit_file(replace): path=%s replacements_requested=%d", path, len(edits))

	if t.fs == nil {
		return &ToolResult{Error: "file system is not configured"}
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("edit_file(replace): error checking if file exists: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}

	if !exists {
		return &ToolResult{Error: fmt.Sprintf("cannot update non-existent file: %s", path)}
	}

	if t.session != nil && !t.session.WasFileRead(path) {
		return &ToolResult{Error: fmt.Sprintf("file %s was not read in this session; read it before updating", path)}
	}

	currentData, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error reading current file: %v", err)}
	}

	content := string(currentData)

	if len(content) == 0 {
		if len(edits) != 1 {
			return &ToolResult{Error: "cannot apply multiple edits to an empty file"}
		}

		if err := t.fs.WriteFile(ctx, path, []byte(edits[0].NewString)); err != nil {
			logger.Error("edit_file(replace): error writing file: %v", err)
			return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
		}

		if t.session != nil {
			t.session.TrackFileModified(path)
		}

		logger.Info("edit_file(replace): updated empty file %s", path)

		return &ToolResult{
			Result: map[string]interface{}{
				"path":          path,
				"replacements":  0,
				"edits_applied": len(edits),
				"updated":       true,
			},
			UIResult: generateGitDiff(path, "", edits[0].NewString),
		}
	}

	for i, edit := range edits {
		if edit.OldString == "" {
			return &ToolResult{Error: fmt.Sprintf("old_string is required for non-empty files (edit %d)", i+1)}
		}
	}

	finalContent := content
	totalReplacements := 0

	// Detect the file's newline style for normalizing new_string values
	fileNewlineStyle := detectNewlineStyle(content)

	for i, edit := range edits {
		expected := edit.ExpectedReplacements
		if expected == 0 {
			expected = 1
		}

		oldStr := edit.OldString
		newStr := normalizeNewlines(edit.NewString, fileNewlineStyle)

		count := strings.Count(finalContent, oldStr)
		if count == 0 {
			// Try normalizing old_string to the file's newline style
			normalizedOld := normalizeNewlines(oldStr, fileNewlineStyle)
			if normalizedOld != oldStr {
				count = strings.Count(finalContent, normalizedOld)
				if count > 0 {
					oldStr = normalizedOld
					logger.Debug("edit_file(replace): normalized newlines in old_string for edit %d", i+1)
				}
			}
		}

		if count == 0 {
			// Check for tab/space mismatch (common with Mistral models)
			if hint := checkWhitespaceMismatch(finalContent, oldStr); hint != "" {
				return &ToolResult{Error: fmt.Sprintf("old_string not found in file (edit %d). %s", i+1, hint)}
			}
			return &ToolResult{Error: fmt.Sprintf("old_string not found in file (edit %d). Try to read the file again and redo the edit.", i+1)}
		}
		if count != expected {
			// Find all match locations with context to help the user
			matchLocations := findMatchLocations(finalContent, oldStr)
			return &ToolResult{Error: fmt.Sprintf("found %d occurrences of old_string in edit %d, but expected %d. Try to read more surrounding text and redo the edit.\n\nFound matches at:\n%s", count, i+1, expected, matchLocations)}
		}

		finalContent = strings.Replace(finalContent, oldStr, newStr, -1)
		totalReplacements += count
	}

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("edit_file(replace): error writing file: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error writing file: %v", err)}
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("edit_file(replace): updated %s (%d replacements)", path, totalReplacements)

	return &ToolResult{
		Result: map[string]interface{}{
			"path":          path,
			"replacements":  totalReplacements,
			"edits_applied": len(edits),
			"updated":       true,
		},
		UIResult: generateGitDiff(path, content, finalContent),
	}
}

func parseReplacementEdits(params map[string]interface{}) ([]replacementEdit, error) {
	rawEdits, hasEdits := params["edits"]
	if !hasEdits || rawEdits == nil {
		return nil, fmt.Errorf("tool call validation failed for write_file_replace: missing required parameter 'edits'")
	}

	data, err := json.Marshal(rawEdits)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal edits: %v", err)
	}

	var edits []replacementEdit
	if err := json.Unmarshal(data, &edits); err != nil {
		if str, ok := rawEdits.(string); ok {
			if err := json.Unmarshal([]byte(str), &edits); err != nil {
				return nil, fmt.Errorf("failed to parse edits: %v", err)
			}
		} else {
			return nil, fmt.Errorf("failed to parse edits: %v", err)
		}
	}

	if len(edits) == 0 {
		return nil, fmt.Errorf("edits cannot be empty")
	}

	for i := range edits {
		if edits[i].ExpectedReplacements == 0 {
			edits[i].ExpectedReplacements = 1
		}
	}

	return edits, nil
}

// findMatchLocations finds all occurrences of a pattern in content and returns their locations with context
func findMatchLocations(content, pattern string) string {
	lines := strings.Split(content, "\n")
	var matches []string

	// Find all line numbers where the pattern occurs
	for i, line := range lines {
		if strings.Contains(line, pattern) {
			// Calculate surrounding line range (4 lines before and after)
			start := i - 4
			if start < 0 {
				start = 0
			}
			end := i + 4
			if end >= len(lines) {
				end = len(lines) - 1
			}

			// Build context display
			var contextLines []string
			for j := start; j <= end; j++ {
				lineNum := j + 1 // Convert to 1-indexed line numbers
				prefix := "  "
				if j == i {
					prefix = "> " // Mark the matching line
				}
				contextLines = append(contextLines, fmt.Sprintf("%s%d: %s", prefix, lineNum, lines[j]))
			}

			matches = append(matches, strings.Join(contextLines, "\n"))
		}
	}

	if len(matches) == 0 {
		return "No matches found (this should not happen)"
	}

	return strings.Join(matches, "\n---\n")
}

// detectNewlineStyle returns the newline style used in content: "\r\n" for CRLF, "\n" for LF.
// If the content has no newlines or mixed styles, defaults to LF.
func detectNewlineStyle(content string) string {
	crlfCount := strings.Count(content, "\r\n")
	lfCount := strings.Count(content, "\n") - crlfCount // LF not preceded by CR

	if crlfCount > lfCount {
		return "\r\n"
	}
	return "\n"
}

// normalizeNewlines converts all newlines in s to the target style.
func normalizeNewlines(s, targetStyle string) string {
	// First normalize to LF, then convert to target
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	if targetStyle == "\r\n" {
		normalized = strings.ReplaceAll(normalized, "\n", "\r\n")
	}
	return normalized
}

// checkWhitespaceMismatch checks if an old_string would match if tabs/spaces were normalized.
// Returns a hint string if a whitespace mismatch is detected, empty string otherwise.
func checkWhitespaceMismatch(content, oldString string) string {
	// Check if old_string contains tabs or leading spaces that might be mismatched
	hasTabs := strings.Contains(oldString, "\t")
	hasLeadingSpaces := false
	for _, line := range strings.Split(oldString, "\n") {
		if len(line) > 0 && line[0] == ' ' {
			hasLeadingSpaces = true
			break
		}
	}

	if !hasTabs && !hasLeadingSpaces {
		return ""
	}

	// Try replacing tabs with spaces (2, 4, and 8 spaces are common)
	if hasTabs {
		for _, spaceCount := range []int{2, 4, 8} {
			normalized := strings.ReplaceAll(oldString, "\t", strings.Repeat(" ", spaceCount))
			if strings.Contains(content, normalized) {
				return fmt.Sprintf("The file uses %d spaces for indentation, but old_string uses tabs. Replace tabs with %d spaces and retry.", spaceCount, spaceCount)
			}
		}
	}

	// Try replacing leading spaces with tabs
	if hasLeadingSpaces {
		// Try common space counts that might be tabs
		for _, spaceCount := range []int{2, 4, 8} {
			spaces := strings.Repeat(" ", spaceCount)
			// Replace leading spaces on each line
			lines := strings.Split(oldString, "\n")
			var normalized []string
			for _, line := range lines {
				newLine := line
				for strings.HasPrefix(newLine, spaces) {
					newLine = "\t" + newLine[spaceCount:]
				}
				normalized = append(normalized, newLine)
			}
			normalizedStr := strings.Join(normalized, "\n")
			if normalizedStr != oldString && strings.Contains(content, normalizedStr) {
				return fmt.Sprintf("The file uses tabs for indentation, but old_string uses spaces. Replace %d-space indentation with tabs and retry.", spaceCount)
			}
		}
	}

	return ""
}

// NewWriteFileReplaceToolFactory creates a factory for WriteFileReplaceTool
func NewWriteFileReplaceToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWriteFileReplaceTool(filesystem, sess)
	}
}
