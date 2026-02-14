package tools

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

// SearchContextFilesToolSpec is the static specification for the search_context_files tool
type SearchContextFilesToolSpec struct{}

func (s *SearchContextFilesToolSpec) Name() string {
	return ToolNameSearchContextFiles
}

func (s *SearchContextFilesToolSpec) Description() string {
	return "Search for files by name pattern (glob) in configured context directories (external documentation, external library sources, etc.). Supports searching compressed files (.gz, .bz2). Returns list of matching file paths. Use this to find files in context directories before reading them."
}

func (s *SearchContextFilesToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to match file names (e.g., '*.md', '**/*.py', 'docs/**/*.html'). Use '**' for recursive directory matching.",
			},
			"content_regex": map[string]interface{}{
				"type":        "string",
				"description": "Optional regex pattern to search within file contents. Only files matching both name pattern and content will be returned.",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 50, max: 500)",
			},
		},
		"required": []string{"pattern"},
	}
}

// SearchContextFilesTool searches files in context directories
type SearchContextFilesTool struct {
	fs      fs.FileSystem
	config  *config.Config
	session *session.Session
}

func NewSearchContextFilesTool(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) *SearchContextFilesTool {
	return &SearchContextFilesTool{
		fs:      filesystem,
		config:  cfg,
		session: sess,
	}
}

func (t *SearchContextFilesTool) Name() string { return ToolNameSearchContextFiles }
func (t *SearchContextFilesTool) Description() string {
	return (&SearchContextFilesToolSpec{}).Description()
}
func (t *SearchContextFilesTool) Parameters() map[string]interface{} {
	return (&SearchContextFilesToolSpec{}).Parameters()
}

func (t *SearchContextFilesTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	pattern := GetStringParam(params, "pattern", "")
	if pattern == "" {
		return &ToolResult{Error: "pattern is required"}
	}

	// Get workspace from session
	workspace := t.session.WorkingDir
	if workspace == "" {
		workspace = "."
	}

	contextDirs := t.config.GetContextDirectories(workspace)
	if len(contextDirs) == 0 {
		return &ToolResult{Error: "No context directories configured for this workspace. Use /context add <directory> to add context directories."}
	}

	contentRegex := GetStringParam(params, "content_regex", "")
	maxResults := GetIntParam(params, "max_results", 50)

	// Validate max results
	if maxResults > 500 {
		maxResults = 500
	}
	if maxResults < 1 {
		maxResults = 1
	}

	// Compile content regex if provided
	var contentRe *regexp.Regexp
	var err error
	if contentRegex != "" {
		contentRe, err = regexp.Compile(contentRegex)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("invalid content_regex: %v", err)}
		}
	}

	// Search all context directories
	var allMatches []string
	var searchErrors []string

	for _, contextDir := range contextDirs {
		matches, err := t.searchFiles(ctx, contextDir, pattern, contentRe, maxResults-len(allMatches))
		if err != nil {
			searchErrors = append(searchErrors, fmt.Sprintf("%s: %v", contextDir, err))
			continue
		}
		allMatches = append(allMatches, matches...)
		if len(allMatches) >= maxResults {
			break
		}
	}

	// Format as markdown
	var result strings.Builder

	// Header
	result.WriteString("## Context Directory Search Results\n\n")
	result.WriteString(fmt.Sprintf("**Pattern:** `%s`\n", pattern))
	if contentRegex != "" {
		result.WriteString(fmt.Sprintf("**Content Filter:** `%s`\n", contentRegex))
	}
	result.WriteString(fmt.Sprintf("**Context Directories:** %d\n", len(contextDirs)))
	for _, dir := range contextDirs {
		result.WriteString(fmt.Sprintf("  - `%s`\n", dir))
	}
	result.WriteString(fmt.Sprintf("**Found:** %d file(s)", len(allMatches)))
	if len(allMatches) >= maxResults {
		result.WriteString(fmt.Sprintf(" (limited to %d results)", maxResults))
	}
	result.WriteString("\n\n")

	// Results
	if len(allMatches) == 0 {
		result.WriteString("*No files found matching the criteria.*\n")
	} else {
		result.WriteString("### Matching Files\n\n")
		for _, match := range allMatches {
			result.WriteString(fmt.Sprintf("- `%s`\n", match))
		}
	}

	// Errors
	if len(searchErrors) > 0 {
		result.WriteString("\n### Errors\n\n")
		for _, errMsg := range searchErrors {
			result.WriteString(fmt.Sprintf("- %s\n", errMsg))
		}
	}

	return &ToolResult{Result: result.String()}
}

func (t *SearchContextFilesTool) searchFiles(ctx context.Context, basePath, pattern string, contentRe *regexp.Regexp, maxResults int) ([]string, error) {
	var matches []string

	// Check if base path exists
	exists, err := t.fs.Exists(ctx, basePath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("directory not found: %s", basePath)
	}

	// Determine if this is a simple pattern or a complex one
	hasRecursive := strings.Contains(pattern, "**")
	hasSlash := strings.Contains(pattern, "/")

	// Walk the directory tree
	err = t.walkDir(ctx, basePath, func(path string, info *fs.FileInfo) error {
		// Check if we've hit the max results
		if len(matches) >= maxResults {
			return fmt.Errorf("max_results_reached")
		}

		// Skip directories
		if info.IsDir {
			return nil
		}

		var matched bool
		var err error

		if hasRecursive || hasSlash {
			// For patterns with ** or /, match against full path
			matched, err = t.matchGlobPattern(path, pattern)
			if err != nil {
				return err
			}
		} else {
			// Simple pattern - match against basename anywhere in tree
			matched, err = filepath.Match(pattern, filepath.Base(path))
			if err != nil {
				return err
			}
		}

		if !matched {
			return nil
		}

		// If content regex is specified, check file contents
		if contentRe != nil {
			hasMatch, err := t.fileContainsPattern(ctx, path, contentRe)
			if err != nil {
				// Skip files that can't be read
				return nil
			}
			if !hasMatch {
				return nil
			}
		}

		matches = append(matches, path)
		return nil
	})

	if err != nil && err.Error() != "max_results_reached" {
		return nil, err
	}

	return matches, nil
}

func (t *SearchContextFilesTool) walkDir(ctx context.Context, dir string, fn func(path string, info *fs.FileInfo) error) error {
	// Get directory info
	info, err := t.fs.Stat(ctx, dir)
	if err != nil {
		return err
	}

	// If it's a file, process it directly
	if !info.IsDir {
		return fn(dir, info)
	}

	// List directory contents
	entries, err := t.fs.ListDir(ctx, dir)
	if err != nil {
		return err
	}

	// Process each entry
	for _, entry := range entries {
		if entry.IsDir {
			// Recursively walk subdirectories
			if err := t.walkDir(ctx, entry.Path, fn); err != nil {
				return err
			}
		} else {
			if err := fn(entry.Path, entry); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *SearchContextFilesTool) fileContainsPattern(ctx context.Context, path string, re *regexp.Regexp) (bool, error) {
	data, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return false, err
	}

	// Attempt decompression if it's a compressed file
	decompressed, err := decompressData(data, path)
	if err != nil {
		return false, err
	}
	data = decompressed

	// Check if file is likely binary
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return false, nil
		}
	}

	return re.Match(data), nil
}

func (t *SearchContextFilesTool) matchGlobPattern(path, pattern string) (bool, error) {
	// Convert glob pattern to regex
	regexPattern := regexp.QuoteMeta(pattern)

	// Handle ** first
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*/`, "(.*/)?")
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*`, ".*")

	// Handle single *
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, "[^/]*")

	// Handle ?
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, "[^/]")

	// Anchor the pattern
	regexPattern = "^" + regexPattern + "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false, err
	}

	return re.MatchString(path), nil
}

// decompressData attempts to decompress data based on file extension
// Supports: .gz (gzip), .bz2 (bzip2)
// Returns original data if not compressed or unknown format
func decompressData(data []byte, filename string) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".gz":
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return data, nil // Not actually gzipped, return original
		}
		defer reader.Close()

		decompressed, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress gzip: %w", err)
		}
		return decompressed, nil

	case ".bz2":
		reader := bzip2.NewReader(bytes.NewReader(data))
		decompressed, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress bzip2: %w", err)
		}
		return decompressed, nil

	default:
		// Not a compressed format we recognize
		return data, nil
	}
}

// NewSearchContextFilesToolFactory creates a factory for SearchContextFilesTool
func NewSearchContextFilesToolFactory(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewSearchContextFilesTool(filesystem, cfg, sess)
	}
}

// GrepContextFilesToolSpec is the static specification for the grep_context_files tool
type GrepContextFilesToolSpec struct{}

func (s *GrepContextFilesToolSpec) Name() string {
	return ToolNameGrepContextFiles
}

func (s *GrepContextFilesToolSpec) Description() string {
	return "Search for text patterns in files within configured context directories. Returns file paths and matching lines with context. Automatically decompresses .gz and .bz2 files (e.g., man pages). Use this to find specific content in external documentation or library sources."
}

func (s *GrepContextFilesToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Regex pattern to search for in file contents",
			},
			"file_pattern": map[string]interface{}{
				"type":        "string",
				"description": "Optional glob pattern to filter files (e.g., '*.md', '**/*.py'). Default: search all files.",
			},
			"context_lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of lines to show before and after each match (default: 2)",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of matching files to return (default: 20, max: 100)",
			},
		},
		"required": []string{"pattern"},
	}
}

// GrepContextFilesTool searches content in context directories
type GrepContextFilesTool struct {
	fs      fs.FileSystem
	config  *config.Config
	session *session.Session
}

func NewGrepContextFilesTool(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) *GrepContextFilesTool {
	return &GrepContextFilesTool{
		fs:      filesystem,
		config:  cfg,
		session: sess,
	}
}

func (t *GrepContextFilesTool) Name() string { return ToolNameGrepContextFiles }
func (t *GrepContextFilesTool) Description() string {
	return (&GrepContextFilesToolSpec{}).Description()
}
func (t *GrepContextFilesTool) Parameters() map[string]interface{} {
	return (&GrepContextFilesToolSpec{}).Parameters()
}

func (t *GrepContextFilesTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	pattern := GetStringParam(params, "pattern", "")
	if pattern == "" {
		return &ToolResult{Error: "pattern is required"}
	}

	// Get workspace from session
	workspace := t.session.WorkingDir
	if workspace == "" {
		workspace = "."
	}

	contextDirs := t.config.GetContextDirectories(workspace)
	if len(contextDirs) == 0 {
		return &ToolResult{Error: "No context directories configured for this workspace. Use /context add <directory> to add context directories."}
	}

	filePattern := GetStringParam(params, "file_pattern", "")
	contextLines := GetIntParam(params, "context_lines", 2)
	maxResults := GetIntParam(params, "max_results", 20)

	// Validate parameters
	if maxResults > 100 {
		maxResults = 100
	}
	if maxResults < 1 {
		maxResults = 1
	}
	if contextLines < 0 {
		contextLines = 0
	}
	if contextLines > 10 {
		contextLines = 10
	}

	// Compile regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("invalid pattern: %v", err)}
	}

	// Search all context directories
	type fileMatch struct {
		path    string
		matches []grepMatch
	}

	var allMatches []fileMatch
	var searchErrors []string
	totalFiles := 0

	for _, contextDir := range contextDirs {
		if totalFiles >= maxResults {
			break
		}

		err := t.grepDirectory(ctx, contextDir, filePattern, re, contextLines, maxResults-totalFiles, func(path string, matches []grepMatch) {
			allMatches = append(allMatches, fileMatch{path: path, matches: matches})
			totalFiles++
		})

		if err != nil {
			searchErrors = append(searchErrors, fmt.Sprintf("%s: %v", contextDir, err))
		}
	}

	// Format results
	var result strings.Builder

	result.WriteString("## Context Directory Grep Results\n\n")
	result.WriteString(fmt.Sprintf("**Pattern:** `%s`\n", pattern))
	if filePattern != "" {
		result.WriteString(fmt.Sprintf("**File Pattern:** `%s`\n", filePattern))
	}
	result.WriteString(fmt.Sprintf("**Context Lines:** %d\n", contextLines))
	result.WriteString(fmt.Sprintf("**Found:** %d file(s) with matches", len(allMatches)))
	if len(allMatches) >= maxResults {
		result.WriteString(fmt.Sprintf(" (limited to %d files)", maxResults))
	}
	result.WriteString("\n\n")

	if len(allMatches) == 0 {
		result.WriteString("*No matches found.*\n")
	} else {
		for _, fileMatch := range allMatches {
			result.WriteString(fmt.Sprintf("### `%s`\n\n", fileMatch.path))
			result.WriteString(fmt.Sprintf("Found %d match(es):\n\n", len(fileMatch.matches)))

			for _, match := range fileMatch.matches {
				result.WriteString("```\n")
				// Show before context
				for i, line := range match.beforeLines {
					lineNum := match.lineNum - len(match.beforeLines) + i
					result.WriteString(fmt.Sprintf("%4d  %s\n", lineNum, line))
				}
				// Show matching line
				result.WriteString(fmt.Sprintf("%4d: %s\n", match.lineNum, match.line))
				// Show after context
				for i, line := range match.afterLines {
					lineNum := match.lineNum + i + 1
					result.WriteString(fmt.Sprintf("%4d  %s\n", lineNum, line))
				}
				result.WriteString("```\n\n")
			}
		}
	}

	if len(searchErrors) > 0 {
		result.WriteString("\n### Errors\n\n")
		for _, errMsg := range searchErrors {
			result.WriteString(fmt.Sprintf("- %s\n", errMsg))
		}
	}

	return &ToolResult{Result: result.String()}
}

func (t *GrepContextFilesTool) grepDirectory(ctx context.Context, dir, filePattern string, re *regexp.Regexp, contextLines, maxFiles int, callback func(string, []grepMatch)) error {
	filesProcessed := 0

	return t.walkDir(ctx, dir, func(path string, info *fs.FileInfo) error {
		if filesProcessed >= maxFiles {
			return fmt.Errorf("max_files_reached")
		}

		if info.IsDir {
			return nil
		}

		// Filter by file pattern if specified
		if filePattern != "" {
			matched, err := filepath.Match(filePattern, filepath.Base(path))
			if err != nil {
				return nil
			}
			if !matched {
				// Check for glob pattern
				if strings.Contains(filePattern, "**") || strings.Contains(filePattern, "/") {
					matched, err = t.matchGlobPattern(path, filePattern)
					if err != nil {
						return nil
					}
					if !matched {
						return nil
					}
				} else {
					return nil
				}
			}
		}

		// Read and search file
		matches, err := t.grepFile(ctx, path, re, contextLines)
		if err != nil {
			// Skip files that can't be read
			return nil
		}

		if len(matches) > 0 {
			callback(path, matches)
			filesProcessed++
		}

		return nil
	})
}

type grepMatch struct {
	lineNum     int
	line        string
	beforeLines []string
	afterLines  []string
}

func (t *GrepContextFilesTool) grepFile(ctx context.Context, path string, re *regexp.Regexp, contextLines int) ([]grepMatch, error) {
	data, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}

	// Attempt decompression if it's a compressed file
	decompressed, err := decompressData(data, path)
	if err != nil {
		return nil, err
	}
	data = decompressed

	// Check if binary
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return nil, nil // Skip binary files
		}
	}

	lines := strings.Split(string(data), "\n")
	var matches []grepMatch

	for i, line := range lines {
		if re.MatchString(line) {
			match := grepMatch{
				lineNum:     i + 1,
				line:        line,
				beforeLines: []string{},
				afterLines:  []string{},
			}

			// Get before context
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			for j := start; j < i; j++ {
				match.beforeLines = append(match.beforeLines, lines[j])
			}

			// Get after context
			end := i + contextLines + 1
			if end > len(lines) {
				end = len(lines)
			}
			for j := i + 1; j < end; j++ {
				match.afterLines = append(match.afterLines, lines[j])
			}

			matches = append(matches, match)
		}
	}

	return matches, nil
}

func (t *GrepContextFilesTool) walkDir(ctx context.Context, dir string, fn func(path string, info *fs.FileInfo) error) error {
	info, err := t.fs.Stat(ctx, dir)
	if err != nil {
		return err
	}

	if !info.IsDir {
		return fn(dir, info)
	}

	entries, err := t.fs.ListDir(ctx, dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir {
			if err := t.walkDir(ctx, entry.Path, fn); err != nil {
				if err.Error() == "max_files_reached" {
					return err
				}
				continue
			}
		} else {
			if err := fn(entry.Path, entry); err != nil {
				if err.Error() == "max_files_reached" {
					return err
				}
			}
		}
	}

	return nil
}

func (t *GrepContextFilesTool) matchGlobPattern(path, pattern string) (bool, error) {
	regexPattern := regexp.QuoteMeta(pattern)
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*/`, "(.*/)?")
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*`, ".*")
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, "[^/]*")
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, "[^/]")
	regexPattern = "^" + regexPattern + "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false, err
	}

	return re.MatchString(path), nil
}

// NewGrepContextFilesToolFactory creates a factory for GrepContextFilesTool
func NewGrepContextFilesToolFactory(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewGrepContextFilesTool(filesystem, cfg, sess)
	}
}

// ReadContextFileToolSpec is the static specification for the read_context_file tool
type ReadContextFileToolSpec struct{}

func (s *ReadContextFileToolSpec) Name() string {
	return ToolNameReadContextFile
}

func (s *ReadContextFileToolSpec) Description() string {
	return "Read a file from configured context directories. The file path must be within one of the context directories. Automatically decompresses .gz and .bz2 files (e.g., man pages). Returns file contents with line numbers."
}

func (s *ReadContextFileToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read (must be within a context directory)",
			},
			"from_line": map[string]interface{}{
				"type":        "integer",
				"description": "Start reading from this line number (1-indexed, default: 1)",
			},
			"to_line": map[string]interface{}{
				"type":        "integer",
				"description": "Stop reading at this line number (inclusive, default: read all)",
			},
		},
		"required": []string{"path"},
	}
}

// ReadContextFileTool reads files from context directories
type ReadContextFileTool struct {
	fs      fs.FileSystem
	config  *config.Config
	session *session.Session
}

func NewReadContextFileTool(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) *ReadContextFileTool {
	return &ReadContextFileTool{
		fs:      filesystem,
		config:  cfg,
		session: sess,
	}
}

func (t *ReadContextFileTool) Name() string        { return ToolNameReadContextFile }
func (t *ReadContextFileTool) Description() string { return (&ReadContextFileToolSpec{}).Description() }
func (t *ReadContextFileTool) Parameters() map[string]interface{} {
	return (&ReadContextFileToolSpec{}).Parameters()
}

func (t *ReadContextFileTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	// Get workspace from session
	workspace := t.session.WorkingDir
	if workspace == "" {
		workspace = "."
	}

	contextDirs := t.config.GetContextDirectories(workspace)
	if len(contextDirs) == 0 {
		return &ToolResult{Error: "No context directories configured for this workspace. Use /context add <directory> to add context directories."}
	}

	// Check if path is within any context directory
	var isInContextDir bool
	absPath := path
	if !filepath.IsAbs(path) {
		// Try to resolve relative to each context directory
		for _, contextDir := range contextDirs {
			testPath := filepath.Join(contextDir, path)
			exists, err := t.fs.Exists(ctx, testPath)
			if err == nil && exists {
				absPath = testPath
				isInContextDir = true
				break
			}
		}
	} else {
		// Check if absolute path is within any context directory
		for _, contextDir := range contextDirs {
			if strings.HasPrefix(absPath, contextDir) {
				isInContextDir = true
				break
			}
		}
	}

	if !isInContextDir {
		return &ToolResult{Error: fmt.Sprintf("path %s is not within any configured context directory", path)}
	}

	// Read the file
	data, err := t.fs.ReadFile(ctx, absPath)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to read file: %v", err)}
	}

	// Attempt decompression if it's a compressed file (e.g., man pages)
	decompressed, err := decompressData(data, absPath)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to decompress file: %v", err)}
	}
	data = decompressed

	// Split into lines
	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	// Get line range parameters
	fromLine := GetIntParam(params, "from_line", 1)
	toLine := GetIntParam(params, "to_line", totalLines)

	// Validate and adjust line numbers
	if fromLine < 1 {
		fromLine = 1
	}
	if toLine > totalLines {
		toLine = totalLines
	}
	if fromLine > toLine {
		fromLine = toLine
	}

	// Extract requested lines
	var result strings.Builder
	result.WriteString(fmt.Sprintf("## File: %s\n\n", absPath))
	result.WriteString(fmt.Sprintf("Lines %d-%d of %d total\n\n", fromLine, toLine, totalLines))
	result.WriteString("```\n")

	for i := fromLine - 1; i < toLine; i++ {
		result.WriteString(fmt.Sprintf("%4d: %s\n", i+1, lines[i]))
	}

	result.WriteString("```\n")

	return &ToolResult{Result: result.String()}
}

// NewReadContextFileToolFactory creates a factory for ReadContextFileTool
func NewReadContextFileToolFactory(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewReadContextFileTool(filesystem, cfg, sess)
	}
}

// AddContextDirectoryToolSpec is the static specification for the add_context_directory tool
type AddContextDirectoryToolSpec struct{}

func (s *AddContextDirectoryToolSpec) Name() string {
	return ToolNameAddContextDirectory
}

func (s *AddContextDirectoryToolSpec) Description() string {
	return "Add a directory to the context directories configuration. This allows the LLM to access files from this directory for documentation, library sources, and other external resources. Requires user authorization before execution."
}

func (s *AddContextDirectoryToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"directory": map[string]interface{}{
				"type":        "string",
				"description": "Path to the directory to add to context directories (required). The directory must exist.",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Optional explanation for why this directory should be added to context. This will be shown to the user during authorization.",
			},
		},
		"required": []string{"directory"},
	}
}

// AddContextDirectoryTool adds directories to the context configuration
type AddContextDirectoryTool struct {
	fs      fs.FileSystem
	config  *config.Config
	session *session.Session
}

func NewAddContextDirectoryTool(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) *AddContextDirectoryTool {
	return &AddContextDirectoryTool{
		fs:      filesystem,
		config:  cfg,
		session: sess,
	}
}

func (t *AddContextDirectoryTool) Name() string { return ToolNameAddContextDirectory }
func (t *AddContextDirectoryTool) Description() string {
	return (&AddContextDirectoryToolSpec{}).Description()
}
func (t *AddContextDirectoryTool) Parameters() map[string]interface{} {
	return (&AddContextDirectoryToolSpec{}).Parameters()
}

func (t *AddContextDirectoryTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	directory := GetStringParam(params, "directory", "")
	if directory == "" {
		return &ToolResult{Error: "directory is required"}
	}

	reason := GetStringParam(params, "reason", "")

	// Get workspace from session
	workspace := t.session.WorkingDir
	if workspace == "" {
		workspace = "."
	}

	// Convert to absolute path if relative
	absDir := directory
	if !filepath.IsAbs(directory) {
		absDir = filepath.Join(workspace, directory)
	}

	// Check if directory exists
	exists, err := t.fs.Exists(ctx, absDir)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to check directory existence: %v", err)}
	}
	if !exists {
		return &ToolResult{Error: fmt.Sprintf("directory does not exist: %s", absDir)}
	}

	// Verify it's actually a directory
	info, err := t.fs.Stat(ctx, absDir)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to stat directory: %v", err)}
	}
	if !info.IsDir {
		return &ToolResult{Error: fmt.Sprintf("path is not a directory: %s", absDir)}
	}

	// Check if already configured
	contextDirs := t.config.GetContextDirectories(workspace)
	for _, dir := range contextDirs {
		if dir == absDir {
			return &ToolResult{Result: fmt.Sprintf("Directory already in context directories: %s", absDir)}
		}
	}

	// Add to configuration
	t.config.AddContextDirectory(workspace, absDir)

	// Format result
	var result strings.Builder
	result.WriteString("## Context Directory Added\n\n")
	result.WriteString(fmt.Sprintf("**Directory:** `%s`\n", absDir))
	if reason != "" {
		result.WriteString(fmt.Sprintf("**Reason:** %s\n", reason))
	}
	result.WriteString("\nThe directory has been added to the context directories configuration and will be available for searching and reading.")

	return &ToolResult{Result: result.String()}
}

// NewAddContextDirectoryToolFactory creates a factory for AddContextDirectoryTool
func NewAddContextDirectoryToolFactory(filesystem fs.FileSystem, cfg *config.Config, sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewAddContextDirectoryTool(filesystem, cfg, sess)
	}
}
