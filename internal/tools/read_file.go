package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/syntax"
)

// ReadFileToolSpec is the static specification for the read_file tool
type ReadFileToolSpec struct{}

func (s *ReadFileToolSpec) Name() string {
	return ToolNameReadFile
}

func (s *ReadFileToolSpec) Description() string {
	return "Read a file from the filesystem. Can read entire file or multiple specific line ranges using the sections parameter. Maximum 2000 lines per read. Files read during the session are tracked for diff operations."
}

func (s *ReadFileToolSpec) Parameters() map[string]interface{} {
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

// ReadFileTool is the executor with runtime dependencies
type ReadFileTool struct {
	fs      fs.FileSystem
	session *session.Session
}

func NewReadFileTool(filesystem fs.FileSystem, sess *session.Session) *ReadFileTool {
	return &ReadFileTool{
		fs:      filesystem,
		session: sess,
	}
}

// Legacy interface implementation for backward compatibility
func (t *ReadFileTool) Name() string        { return ToolNameReadFile }
func (t *ReadFileTool) Description() string { return (&ReadFileToolSpec{}).Description() }
func (t *ReadFileTool) Parameters() map[string]interface{} {
	return (&ReadFileToolSpec{}).Parameters()
}

func (t *ReadFileTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	// Check if file exists
	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("read_file: error checking if file exists: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}
	if !exists {
		logger.Warn("read_file: file not found: %s", path)
		return &ToolResult{Error: fmt.Sprintf("file not found: %s", path)}
	}

	// Check if sections parameter is provided
	sectionsParam, hasSections := params["sections"]
	if hasSections {
		return t.executeMultiSection(ctx, path, sectionsParam)
	}

	// Read entire file
	logger.Debug("read_file: path=%s (entire file)", path)

	data, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error reading file: %v", err)}
	}
	content := string(data)

	// Check line limit
	lineCount := strings.Count(content, "\n") + 1
	wasTruncated := false
	totalLines := lineCount

	if lineCount > 2000 {
		// Read only first 2000 lines
		lines, err := t.fs.ReadFileLines(ctx, path, 1, 2000)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("error reading file lines: %v", err)}
		}
		content = strings.Join(lines, "\n")
		content += fmt.Sprintf("\n\n[... file truncated, %d total lines, showing first 2000 lines. Use sections parameter to read specific ranges]", lineCount)
		wasTruncated = true
	}

	// Track file as read in session
	if t.session != nil {
		t.session.TrackFileRead(path, content)
	}

	actualLineCount := len(strings.Split(content, "\n"))
	logger.Info("read_file: successfully read %s (%d lines)", path, actualLineCount)

	// Generate UI result with syntax highlighting
	// For UI, show a more prominent truncation warning if applicable
	var uiResult string
	if wasTruncated {
		// Remove the text truncation message from content for UI display
		uiContent := strings.Join(strings.Split(content, "\n")[:2000], "\n")
		uiResult = t.formatUIResultWithTruncation(path, uiContent, 2000, totalLines)
	} else {
		uiResult = t.formatUIResult(path, content, actualLineCount)
	}

	return &ToolResult{
		Result: map[string]interface{}{
			"path":    path,
			"content": content,
			"lines":   actualLineCount,
		},
		UIResult: uiResult,
	}
}

// sectionData represents a section of a file to read
type sectionData struct {
	fromLine int
	toLine   int
	content  string
}

func (t *ReadFileTool) executeMultiSection(ctx context.Context, path string, sectionsParam interface{}) *ToolResult {
	sections, ok := sectionsParam.([]interface{})
	if !ok {
		return &ToolResult{Error: "sections parameter must be an array"}
	}

	if len(sections) == 0 {
		return &ToolResult{Error: "sections array cannot be empty"}
	}

	logger.Debug("read_file: path=%s, sections=%d", path, len(sections))

	// Parse and validate sections
	type lineRange struct {
		fromLine int
		toLine   int
	}
	var ranges []lineRange
	totalLines := 0

	for i, sectionParam := range sections {
		section, ok := sectionParam.(map[string]interface{})
		if !ok {
			return &ToolResult{Error: fmt.Sprintf("section %d must be an object", i)}
		}

		fromLine := GetIntParam(section, "from_line", 0)
		toLine := GetIntParam(section, "to_line", 0)

		if fromLine <= 0 || toLine <= 0 {
			return &ToolResult{Error: fmt.Sprintf("section %d: from_line and to_line must be positive integers", i)}
		}

		if fromLine > toLine {
			return &ToolResult{Error: fmt.Sprintf("section %d: from_line (%d) cannot be greater than to_line (%d)", i, fromLine, toLine)}
		}

		sectionLines := toLine - fromLine + 1
		totalLines += sectionLines

		ranges = append(ranges, lineRange{fromLine: fromLine, toLine: toLine})
	}

	if totalLines > 2000 {
		return &ToolResult{Error: fmt.Sprintf("cannot read more than 2000 lines at once (requested %d lines across %d sections)", totalLines, len(sections))}
	}

	// Read all sections
	var sectionsData []sectionData
	var contentParts []string
	var err error

	// Get total file line count for UI display
	fileData, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error reading file: %v", err)}
	}
	totalFileLines := strings.Count(string(fileData), "\n") + 1

	for i, r := range ranges {
		var lines []string
		lines, err = t.fs.ReadFileLines(ctx, path, r.fromLine, r.toLine)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("error reading section %d (lines %d-%d): %v", i, r.fromLine, r.toLine, err)}
		}

		sectionContent := strings.Join(lines, "\n")
		sectionsData = append(sectionsData, sectionData{
			fromLine: r.fromLine,
			toLine:   r.toLine,
			content:  sectionContent,
		})

		// Add section header for clarity (for LLM)
		if len(ranges) > 1 {
			header := fmt.Sprintf("[Section %d: lines %d-%d]", i+1, r.fromLine, r.toLine)
			contentParts = append(contentParts, header, sectionContent)
		} else {
			contentParts = append(contentParts, sectionContent)
		}
	}

	content := strings.Join(contentParts, "\n")

	// Track file as read in session
	if t.session != nil {
		t.session.TrackFileRead(path, content)
	}

	logger.Info("read_file: successfully read %s (%d sections, %d lines total)", path, len(ranges), totalLines)

	// Generate UI result with multi-section formatting
	uiResult := t.formatMultiSectionUIResult(path, sectionsData, totalFileLines)

	return &ToolResult{
		Result: map[string]interface{}{
			"path":     path,
			"content":  content,
			"lines":    totalLines,
			"sections": len(ranges),
		},
		UIResult: uiResult,
	}
}

// formatUIResult creates a UI-formatted result
// Note: Syntax highlighting is handled by Glamour during markdown rendering
func (t *ReadFileTool) formatUIResult(path, content string, lineCount int) string {
	// Detect language from file extension for Glamour
	language := syntax.DetectLanguage(path)

	// Add line numbers to content
	lines := strings.Split(content, "\n")
	numberedContent := addLineNumbers(lines, 1)

	// Create UI result with plain content
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📖 **Read file:** `%s`\n\n", path))

	// Add file info
	if language != "" {
		sb.WriteString(fmt.Sprintf(`📊 **File info:** %d lines | Language: %s\n\n`, lineCount, language))
		// Use language tag for Glamour's syntax highlighting
		sb.WriteString("```" + language + "\n")
	} else {
		sb.WriteString(fmt.Sprintf(`📊 **File info:** %d lines\n\n`, lineCount))
		sb.WriteString("```\n")
	}

	sb.WriteString(numberedContent)
	sb.WriteString("\n```")

	return sb.String()
}

// formatUIResultWithTruncation creates a UI-formatted result for truncated files
// Note: Syntax highlighting is handled by Glamour during markdown rendering
func (t *ReadFileTool) formatUIResultWithTruncation(path, content string, shownLines, totalLines int) string {
	// Detect language from file extension for Glamour
	language := syntax.DetectLanguage(path)

	// Add line numbers to content
	lines := strings.Split(content, "\n")
	numberedContent := addLineNumbers(lines, 1)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📖 **Read file:** `%s`\n\n", path))

	// Add file info with truncation warning
	if language != "" {
		sb.WriteString(fmt.Sprintf(`📊 **File info:** %d lines | Language: %s\n\n`, totalLines, language))
	} else {
		sb.WriteString(fmt.Sprintf(`📊 **File info:** %d lines\n\n`, totalLines))
	}

	// Add prominent truncation warning
	sb.WriteString(fmt.Sprintf("⚠️  **File truncated:** Showing lines 1-%d of %d total lines. Use `sections` parameter to read specific ranges.\n\n", shownLines, totalLines))

	// Add code block with plain content
	if language != "" {
		sb.WriteString("```" + language + "\n")
	} else {
		sb.WriteString("```\n")
	}
	sb.WriteString(numberedContent)
	sb.WriteString("\n```")

	return sb.String()
}

// formatMultiSectionUIResult creates a UI-formatted result for multi-section reads
// Note: Syntax highlighting is handled by Glamour during markdown rendering
func (t *ReadFileTool) formatMultiSectionUIResult(path string, sections []sectionData, totalFileLines int) string {
	// Detect language from file extension for Glamour
	language := syntax.DetectLanguage(path)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📖 **Read file (multiple sections):** `%s`\n\n", path))

	// Add file info
	if language != "" {
		sb.WriteString(fmt.Sprintf(`📊 **File info:** %d total lines | Reading %d sections | Language: %s\n\n`, totalFileLines, len(sections), language))
	} else {
		sb.WriteString(fmt.Sprintf(`📊 **File info:** %d total lines | Reading %d sections\n\n`, totalFileLines, len(sections)))
	}

	// Process each section
	for i, section := range sections {
		if i > 0 {
			// Add horizontal rule separator between sections
			sb.WriteString("\n---\n\n")
		}

		// Add section info
		sb.WriteString(fmt.Sprintf("**Lines %d-%d**\n\n", section.fromLine, section.toLine))

		// Add line numbers to section content
		lines := strings.Split(section.content, "\n")
		numberedContent := addLineNumbers(lines, section.fromLine)

		// Add code block with plain content
		if language != "" {
			sb.WriteString("```" + language + "\n")
		} else {
			sb.WriteString("```\n")
		}
		sb.WriteString(numberedContent)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}

// addLineNumbers adds line numbers to content
func addLineNumbers(lines []string, startLine int) string {
	if len(lines) == 0 {
		return ""
	}

	// Calculate the width needed for line numbers based on the last line
	maxDigits := len(fmt.Sprintf("%d", startLine+len(lines)-1))

	var sb strings.Builder
	for i, line := range lines {
		lineNum := startLine + i
		sb.WriteString(fmt.Sprintf("%*d | %s", maxDigits, lineNum, line))
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// Note: Language detection is now handled by syntax.DetectLanguage()
// See internal/syntax/language.go for the shared implementation

// NewReadFileToolFactory creates a factory for ReadFileTool
func NewReadFileToolFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewReadFileTool(filesystem, sess)
	}
}
