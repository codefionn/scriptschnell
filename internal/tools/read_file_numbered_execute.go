package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// ReadFileNumberedExecutor handles the actual execution of the read_file tool with specific runtime dependencies.
type ReadFileNumberedExecutor struct {
	fs      fs.FileSystem
	session *session.Session
}

// NewReadFileNumberedExecutor creates a new executor for the read_file tool.
func NewReadFileNumberedExecutor(filesystem fs.FileSystem, sess *session.Session) *ReadFileNumberedExecutor {
	return &ReadFileNumberedExecutor{
		fs:      filesystem,
		session: sess,
	}
}

// NewReadFileNumberedFactory creates a factory for the read_file tool executor.
// This allows the same tool spec to be instantiated with different dependencies.
func NewReadFileNumberedFactory(filesystem fs.FileSystem, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewReadFileNumberedExecutor(filesystem, sess)
	}
}

func (e *ReadFileNumberedExecutor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: fmt.Sprintf("path is required")}
	}

	// Check if file exists
	exists, err := e.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("read_file (numbered): error checking if file exists: %v", err)
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}
	if !exists {
		logger.Warn("read_file (numbered): file not found: %s", path)
		return &ToolResult{Error: fmt.Sprintf("file not found: %s", path)}
	}

	// Check if sections parameter is provided
	sectionsParam, hasSections := params["sections"]
	if hasSections {
		return e.executeMultiSection(ctx, path, sectionsParam)
	}

	// Read entire file
	logger.Debug("read_file (numbered): path=%s (entire file)", path)

	data, err := e.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error reading file: %v", err)}
	}
	rawContent := string(data)
	totalLineCount := strings.Count(rawContent, "\n") + 1

	var lines []string
	var truncationMessage string

	if totalLineCount > 2000 {
		// Read only first 2000 lines
		lines, err = e.fs.ReadFileLines(ctx, path, 1, 2000)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("error reading file lines: %v", err)}
		}
		truncationMessage = fmt.Sprintf("[... file truncated, %d total lines, showing first 2000 lines. Use sections parameter to read specific ranges]", totalLineCount)
	} else {
		lines = splitPreserveLines(rawContent)
	}

	content := prependFormatNotice(formatLinesWithNumbers(lines, 1))
	if truncationMessage != "" {
		content += "\n\n" + truncationMessage
	}

	// Track file as read in session
	if e.session != nil {
		e.session.TrackFileRead(path, content)
	}

	lineCount := len(lines)
	logger.Info("read_file (numbered): successfully read %s (%d lines)", path, lineCount)

	return &ToolResult{
		Result: map[string]interface{}{
			"path":    path,
			"content": content,
			"lines":   lineCount,
			"format":  "[padded line number] [line]",
		},
	}
}

func (e *ReadFileNumberedExecutor) executeMultiSection(ctx context.Context, path string, sectionsParam interface{}) *ToolResult {
	sections, ok := sectionsParam.([]interface{})
	if !ok {
		return &ToolResult{Error: "sections parameter must be an array"}
	}

	if len(sections) == 0 {
		return &ToolResult{Error: "sections array cannot be empty"}
	}

	logger.Debug("read_file (numbered): path=%s, sections=%d", path, len(sections))

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
	var contentParts []string
	var err error

	for i, r := range ranges {
		var lines []string
		lines, err = e.fs.ReadFileLines(ctx, path, r.fromLine, r.toLine)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("error reading section %d (lines %d-%d): %v", i, r.fromLine, r.toLine, err)}
		}

		// Format this section with line numbers
		numberedContent := formatLinesWithNumbers(lines, r.fromLine)

		// Add section header for clarity when multiple sections
		if len(ranges) > 1 {
			header := fmt.Sprintf("[Section %d: lines %d-%d]", i+1, r.fromLine, r.toLine)
			contentParts = append(contentParts, header, numberedContent)
		} else {
			contentParts = append(contentParts, numberedContent)
		}
	}

	content := prependFormatNotice(strings.Join(contentParts, "\n"))

	// Track file as read in session
	if e.session != nil {
		e.session.TrackFileRead(path, content)
	}

	logger.Info("read_file (numbered): successfully read %s (%d sections, %d lines total)", path, len(ranges), totalLines)

	return &ToolResult{
		Result: map[string]interface{}{
			"path":     path,
			"content":  content,
			"lines":    totalLines,
			"sections": len(ranges),
			"format":   "[padded line number] [line]",
		},
	}
}

// Helper functions moved from the original tool

func splitPreserveLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}

func formatLinesWithNumbers(lines []string, start int) string {
	if len(lines) == 0 {
		return ""
	}

	endLine := start + len(lines) - 1
	width := len(fmt.Sprintf("%d", endLine))
	formatted := make([]string, len(lines))

	for i, line := range lines {
		lineNumber := start + i
		formatted[i] = fmt.Sprintf("%*d %s", width, lineNumber, line)
	}

	return strings.Join(formatted, "\n")
}

func prependFormatNotice(content string) string {
	const notice = "[Line format: [padded line number] [line]]"
	if content == "" {
		return notice
	}
	return notice + "\n" + content
}