package tools

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codefionn/scriptschnell/internal/fs"
)

// SearchFileContentToolSpec is the static specification for the search_file_content tool
type SearchFileContentToolSpec struct{}

func (s *SearchFileContentToolSpec) Name() string {
	return ToolNameSearchFileContent
}

func (s *SearchFileContentToolSpec) Description() string {
	return "Search for a regex pattern in files, returning matches with line numbers and context. Similar to grep/rg."
}

func (s *SearchFileContentToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Regex pattern to search for in file content.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory or file to search in (default: '.').",
			},
			"glob": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to filter file names (e.g., '*.go', '**/*.js').",
			},
			"context": map[string]interface{}{
				"type":        "integer",
				"description": "Number of context lines to show around the match (default: 2).",
			},
		},
		"required": []string{"pattern"},
	}
}

// SearchFileContentTool is the executor with runtime dependencies
type SearchFileContentTool struct {
	fs fs.FileSystem
}

func NewSearchFileContentTool(filesystem fs.FileSystem) *SearchFileContentTool {
	return &SearchFileContentTool{
		fs: filesystem,
	}
}

// Legacy interface implementation for backward compatibility
func (t *SearchFileContentTool) Name() string { return ToolNameSearchFileContent }
func (t *SearchFileContentTool) Description() string {
	return (&SearchFileContentToolSpec{}).Description()
}
func (t *SearchFileContentTool) Parameters() map[string]interface{} {
	return (&SearchFileContentToolSpec{}).Parameters()
}

func (t *SearchFileContentTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	pattern := GetStringParam(params, "pattern", "")
	if pattern == "" {
		return &ToolResult{Error: "pattern is required"}
	}

	searchPath := GetStringParam(params, "path", ".")
	globPattern := GetStringParam(params, "glob", "")
	contextLines := GetIntParam(params, "context", 2)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("invalid regex pattern: %v", err)}
	}

	var results strings.Builder
	matchCount := 0
	fileCount := 0

	// Start with header
	results.WriteString("## Content Search Results\n\n")
	results.WriteString(fmt.Sprintf("**Pattern:** `%s`\n", pattern))
	if globPattern != "" {
		results.WriteString(fmt.Sprintf("**File Filter:** `%s`\n", globPattern))
	}
	if searchPath != "." {
		results.WriteString(fmt.Sprintf("**Search Path:** `%s`\n", searchPath))
	}
	results.WriteString(fmt.Sprintf("**Context Lines:** %d\n\n", contextLines))

	err = t.walkDir(ctx, searchPath, func(path string, info *fs.FileInfo) error {
		if info.IsDir {
			return nil
		}

		// Filter by glob if provided
		if globPattern != "" {
			matched, err := t.matchGlobPattern(path, globPattern)
			if err != nil {
				// If glob is invalid, maybe just return error or log?
				// For now, return error to be safe
				return err
			}
			if !matched {
				return nil
			}
		}

		// Check if binary (simple check)
		// We need to read the file anyway
		contentBytes, err := t.fs.ReadFile(ctx, path)
		if err != nil {
			return nil // Skip unreadable files
		}

		// Skip binary files
		if isLikelyBinaryFile(path, contentBytes) {
			return nil
		}

		content := string(contentBytes)
		content = strings.TrimSuffix(content, "\n")
		lines := strings.Split(content, "\n")

		// Find matches
		var matchedLineIndices []int
		for i, line := range lines {
			if re.MatchString(line) {
				matchedLineIndices = append(matchedLineIndices, i)
			}
		}

		if len(matchedLineIndices) == 0 {
			return nil
		}

		fileCount++
		matchCount += len(matchedLineIndices)

		// Calculate padding
		// "padded by max line number length + 1"
		// If lines=100 (3 digits), padding=4
		maxLineNum := len(lines)
		digits := 0
		if maxLineNum > 0 {
			digits = int(math.Log10(float64(maxLineNum))) + 1
		}
		padding := digits + 1

		// Determine which lines to print (with context) and group them into blocks
		// Each block represents a continuous range of lines (including context) for a match.
		// Overlapping or adjacent ranges are merged into a single block.
		type MatchBlock struct {
			start int // inclusive line index (0‑based)
			end   int // inclusive line index (0‑based)
		}
		var blocks []MatchBlock
		for _, idx := range matchedLineIndices {
			start := idx - contextLines
			if start < 0 {
				start = 0
			}
			end := idx + contextLines
			if end >= len(lines) {
				end = len(lines) - 1
			}
			// Merge with previous block if overlapping or directly adjacent
			if len(blocks) > 0 && start <= blocks[len(blocks)-1].end+1 {
				if end > blocks[len(blocks)-1].end {
					blocks[len(blocks)-1].end = end
				}
			} else {
				blocks = append(blocks, MatchBlock{start: start, end: end})
			}
		}

		// Write file header with markdown formatting
		results.WriteString(fmt.Sprintf("### `%s`\n\n", path))
		results.WriteString(fmt.Sprintf("*%d match(es)*\n\n", len(matchedLineIndices)))
		results.WriteString("```\n")

		for bIdx, blk := range blocks {
			for i := blk.start; i <= blk.end; i++ {
				lineNum := i + 1
				results.WriteString(fmt.Sprintf("%*d: %s\n", padding, lineNum, lines[i]))
			}
			// Add separator between blocks (except after the last block)
			if bIdx < len(blocks)-1 {
				results.WriteString(fmt.Sprintf("%*s\n", padding+1, "--"))
			}
		}
		results.WriteString("```\n\n")

		return nil
	})

	if err != nil {
		return &ToolResult{Error: err.Error()}
	}

	if matchCount == 0 {
		results.WriteString("*No matches found.*\n")
	} else {
		// Add summary at the end
		results.WriteString("---\n\n")
		results.WriteString(fmt.Sprintf("**Summary:** Found %d match(es) in %d file(s)\n", matchCount, fileCount))
	}

	return &ToolResult{Result: results.String()}
}

// walkDir is a helper to walk directory, similar to SearchFilesTool
// We could deduplicate this if we refactor, but for now copy-paste-adapt is safer/faster
func (t *SearchFileContentTool) walkDir(ctx context.Context, dir string, fn func(path string, info *fs.FileInfo) error) error {
	exists, err := t.fs.Exists(ctx, dir)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("path not found: %s", dir)
	}

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
		// Skip hidden directories/files (starting with .)
		// Unless explicitly requested? For now, standard skip.
		baseName := filepath.Base(entry.Path)
		if strings.HasPrefix(baseName, ".") && baseName != "." && baseName != ".." {
			continue
		}

		if entry.IsDir {
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

func (t *SearchFileContentTool) matchGlobPattern(path, pattern string) (bool, error) {
	// Simple glob implementation using filepath.Match on the base name
	// OR full path match if it contains separators.
	// Similar to SearchFilesTool logic

	hasRecursive := strings.Contains(pattern, "**")
	hasSlash := strings.Contains(pattern, "/")

	if hasRecursive || hasSlash {
		// Complex matching not fully implemented here without converting to regex
		// For simplicity, let's use the same regex conversion as SearchFilesTool
		return t.matchComplexGlob(path, pattern)
	}

	return filepath.Match(pattern, filepath.Base(path))
}

func (t *SearchFileContentTool) matchComplexGlob(path, pattern string) (bool, error) {
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

// NewSearchFileContentToolFactory creates a factory for SearchFileContentTool
func NewSearchFileContentToolFactory(filesystem fs.FileSystem) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewSearchFileContentTool(filesystem)
	}
}
