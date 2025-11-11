package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
)

// SearchFilesTool allows searching for files by name pattern and optionally by content
type SearchFilesTool struct {
	fs fs.FileSystem
}

func NewSearchFilesTool(filesystem fs.FileSystem) *SearchFilesTool {
	return &SearchFilesTool{
		fs: filesystem,
	}
}

func (t *SearchFilesTool) Name() string {
	return ToolNameSearchFiles
}

func (t *SearchFilesTool) Description() string {
	return "Search for files by name pattern (glob) and optionally filter by content. Returns list of matching file paths. Use this to find files in the project before reading them."
}

func (t *SearchFilesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to match file names (e.g., '*.go', '**/*.md', 'internal/**/*.go'). Use '**' for recursive directory matching.",
			},
			"content_regex": map[string]interface{}{
				"type":        "string",
				"description": "Optional regex pattern to search within file contents. Only files matching both name pattern and content will be returned.",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 50, max: 500)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Base directory to search in (relative to working directory, default: '.')",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *SearchFilesTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	pattern := GetStringParam(params, "pattern", "")
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	contentRegex := GetStringParam(params, "content_regex", "")
	maxResults := GetIntParam(params, "max_results", 50)
	basePath := GetStringParam(params, "path", ".")

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
			return nil, fmt.Errorf("invalid content_regex: %w", err)
		}
	}

	// Find matching files
	matches, err := t.searchFiles(ctx, basePath, pattern, contentRe, maxResults)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	result := map[string]interface{}{
		"pattern":     pattern,
		"matches":     matches,
		"count":       len(matches),
		"max_results": maxResults,
		"has_more":    len(matches) >= maxResults,
	}

	if contentRegex != "" {
		result["content_regex"] = contentRegex
	}

	return result, nil
}

func (t *SearchFilesTool) searchFiles(ctx context.Context, basePath, pattern string, contentRe *regexp.Regexp, maxResults int) ([]string, error) {
	var matches []string

	// Determine if this is a simple pattern or a complex one
	hasRecursive := strings.Contains(pattern, "**")
	hasSlash := strings.Contains(pattern, "/")

	// Walk the directory tree
	err := t.walkDir(ctx, basePath, func(path string, info *fs.FileInfo) error {
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
				// Skip files that can't be read (binary files, permission errors, etc.)
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

func (t *SearchFilesTool) walkDir(ctx context.Context, dir string, fn func(path string, info *fs.FileInfo) error) error {
	// Check if directory exists
	exists, err := t.fs.Exists(ctx, dir)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("directory not found: %s", dir)
	}

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

func (t *SearchFilesTool) fileContainsPattern(ctx context.Context, path string, re *regexp.Regexp) (bool, error) {
	data, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return false, err
	}

	// Check if file is likely binary (contains null bytes in first 512 bytes)
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return false, nil // Skip binary files
		}
	}

	return re.Match(data), nil
}

func (t *SearchFilesTool) matchGlobPattern(path, pattern string) (bool, error) {
	// Convert glob pattern to regex
	regexPattern := regexp.QuoteMeta(pattern)

	// Handle ** first (matches any number of directories)
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*/`, "(.*/)?")
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*`, ".*")

	// Handle single * (matches anything except /)
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, "[^/]*")

	// Handle ? (matches any single character except /)
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, "[^/]")

	// Anchor the pattern
	regexPattern = "^" + regexPattern + "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false, err
	}

	return re.MatchString(path), nil
}
