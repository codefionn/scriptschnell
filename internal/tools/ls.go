package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LsToolSpec is the static specification for the ls tool
type LsToolSpec struct{}

func (s *LsToolSpec) Name() string {
	return ToolNameLs
}

func (s *LsToolSpec) Description() string {
	return "List directory contents. Provides detailed file listings with permissions, sizes, and timestamps."
}

func (s *LsToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory path to list (optional, defaults to current directory)",
			},
			"long_format": map[string]interface{}{
				"type":        "boolean",
				"description": "Show detailed information including permissions, size, and modification time",
			},
			"all": map[string]interface{}{
				"type":        "boolean",
				"description": "Show hidden files (files starting with .)",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "List subdirectories recursively",
			},
			"sort_by_time": map[string]interface{}{
				"type":        "boolean",
				"description": "Sort by modification time (newest first)",
			},
			"reverse": map[string]interface{}{
				"type":        "boolean",
				"description": "Reverse sort order",
			},
		},
	}
}

// LsTool is the executor with runtime dependencies
type LsTool struct {
	workingDir string
}

func NewLsTool(workingDir string) *LsTool {
	return &LsTool{
		workingDir: workingDir,
	}
}

// Legacy interface implementation for backward compatibility
func (t *LsTool) Name() string        { return ToolNameLs }
func (t *LsTool) Description() string { return (&LsToolSpec{}).Description() }
func (t *LsTool) Parameters() map[string]interface{} {
	return (&LsToolSpec{}).Parameters()
}

func (t *LsTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	longFormat := GetBoolParam(params, "long_format", false)
	all := GetBoolParam(params, "all", false)
	recursive := GetBoolParam(params, "recursive", false)
	sortByTime := GetBoolParam(params, "sort_by_time", false)
	reverse := GetBoolParam(params, "reverse", false)

	if path == "" {
		path = t.workingDir
	}

	startTime := time.Now()

	if recursive {
		return t.executeRecursive(ctx, path, longFormat, all, sortByTime, reverse, startTime)
	}

	return t.executeSingleDir(ctx, path, longFormat, all, sortByTime, reverse, startTime)
}

func (t *LsTool) executeSingleDir(ctx context.Context, path string, longFormat, all, sortByTime, reverse bool, startTime time.Time) *ToolResult {
	// Normalize the path
	if path == "" {
		path = t.workingDir
	}

	// Check if path is relative and resolve it
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.workingDir, path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return &ToolResult{
			Error: fmt.Sprintf("failed to read directory %s: %v", path, err),
			ExecutionMetadata: &ExecutionMetadata{
				StartTime:  &startTime,
				EndTime:    pointerToTime(time.Now()),
				DurationMs: time.Since(startTime).Milliseconds(),
				WorkingDir: path,
				ErrorType:  "not_found",
			},
		}
	}

	var files []os.FileInfo
	for _, entry := range entries {
		// Get FileInfo for compatibility with existing code
		fileInfo, err := entry.Info()
		if err != nil {
			continue // Skip this entry if we can't get info
		}

		if !all && strings.HasPrefix(fileInfo.Name(), ".") {
			continue
		}
		files = append(files, fileInfo)
	}

	// Sort files
	if sortByTime {
		sort.Slice(files, func(i, j int) bool {
			result := files[i].ModTime().After(files[j].ModTime())
			if reverse {
				return !result
			}
			return result
		})
	} else {
		sort.Slice(files, func(i, j int) bool {
			result := strings.ToLower(files[i].Name()) < strings.ToLower(files[j].Name())
			if reverse {
				return !result
			}
			return result
		})
	}

	var result []interface{}
	var outputLines []string

	for _, file := range files {
		fullPath := filepath.Join(path, file.Name())

		if longFormat {
			fileInfo := t.formatLongEntry(file, fullPath)
			result = append(result, fileInfo)
			outputLines = append(outputLines, t.longFormatLine(fileInfo))
		} else {
			result = append(result, t.formatShortEntry(file))
			outputLines = append(outputLines, file.Name())
		}
	}

	endTime := time.Now()
	outputStr := strings.Join(outputLines, "\n")

	// Calculate line count
	lineCount := strings.Count(outputStr, "\n")
	if outputStr != "" {
		lineCount++
	}

	metadata := &ExecutionMetadata{
		StartTime:       &startTime,
		EndTime:         &endTime,
		DurationMs:      time.Since(startTime).Milliseconds(),
		WorkingDir:      path,
		OutputSizeBytes: len(outputStr),
		OutputLineCount: lineCount,
		ToolType:        ToolNameLs,
	}

	return &ToolResult{
		Result:            result,
		UIResult:          outputStr,
		ExecutionMetadata: metadata,
	}
}

func (t *LsTool) executeRecursive(ctx context.Context, path string, longFormat, all, sortByTime, reverse bool, startTime time.Time) *ToolResult {
	// Normalize the path
	if path == "" {
		path = t.workingDir
	}

	// Check if path is relative and resolve it
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.workingDir, path)
	}
	var allFiles []map[string]interface{}
	var allOutput []string

	err := filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip error but continue walking
		}

		if walkPath == path {
			return nil // Skip root directory
		}

		relPath, _ := filepath.Rel(path, walkPath)
		if relPath == "." {
			return nil
		}

		// Skip hidden files/dirs if not showing all
		if !all && strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir // Skip this directory and its contents
			}
			return nil // Skip this file
		}

		var fileData interface{}
		if longFormat {
			fileData = t.formatLongEntry(info, walkPath)
			allOutput = append(allOutput, t.longFormatLine(fileData.(map[string]interface{})))
		} else {
			fileData = t.formatShortEntry(info)
			allOutput = append(allOutput, relPath)
		}

		allFiles = append(allFiles, map[string]interface{}{
			"path":     relPath,
			"file":     fileData,
			"modified": info.ModTime(),
		})
		return nil
	})

	if err != nil {
		return &ToolResult{
			Error: fmt.Sprintf("error walking directory %s: %v", path, err),
			ExecutionMetadata: &ExecutionMetadata{
				StartTime:  &startTime,
				EndTime:    pointerToTime(time.Now()),
				DurationMs: time.Since(startTime).Milliseconds(),
				WorkingDir: path,
				ErrorType:  "unknown",
			},
		}
	}

	// Sort if needed
	if sortByTime {
		sort.Slice(allFiles, func(i, j int) bool {
			iTime := allFiles[i]["modified"].(time.Time)
			jTime := allFiles[j]["modified"].(time.Time)
			result := iTime.After(jTime)
			if reverse {
				return !result
			}
			return result
		})

		// Update output lines to match sorted order
		allOutput = make([]string, 0, len(allFiles))
		for _, file := range allFiles {
			if longFormat {
				fileData := file["file"].(map[string]interface{})
				allOutput = append(allOutput, t.longFormatLine(fileData))
			} else {
				allOutput = append(allOutput, file["path"].(string))
			}
		}
	} else {
		sort.Slice(allFiles, func(i, j int) bool {
			iName := allFiles[i]["path"].(string)
			jName := allFiles[j]["path"].(string)
			result := strings.ToLower(iName) < strings.ToLower(jName)
			if reverse {
				return !result
			}
			return result
		})

		// Update output lines
		allOutput = make([]string, 0, len(allFiles))
		for _, file := range allFiles {
			allOutput = append(allOutput, file["path"].(string))
		}
	}

	endTime := time.Now()
	outputStr := strings.Join(allOutput, "\n")

	metadata := &ExecutionMetadata{
		StartTime:       &startTime,
		EndTime:         &endTime,
		DurationMs:      time.Since(startTime).Milliseconds(),
		WorkingDir:      path,
		OutputSizeBytes: len(outputStr),
		OutputLineCount: len(allFiles),
		ToolType:        ToolNameLs,
	}

	return &ToolResult{
		Result:            allFiles,
		UIResult:          outputStr,
		ExecutionMetadata: metadata,
	}
}

func (t *LsTool) formatShortEntry(file os.FileInfo) map[string]interface{} {
	return map[string]interface{}{
		"name":     file.Name(),
		"size":     file.Size(),
		"is_dir":   file.IsDir(),
		"mod_time": file.ModTime(),
	}
}

func (t *LsTool) formatLongEntry(file os.FileInfo, fullPath string) map[string]interface{} {
	info, err := os.Stat(fullPath)
	if err != nil {
		info = file // Fallback to provided file info
	}

	permissions := info.Mode().String()

	return map[string]interface{}{
		"name":        file.Name(),
		"size":        file.Size(),
		"permissions": permissions,
		"is_dir":      file.IsDir(),
		"mod_time":    file.ModTime().Format("Jan 02 15:04"),
		"full_size":   formatFileSize(file.Size()),
	}
}

func (t *LsTool) longFormatLine(fileInfo map[string]interface{}) string {
	return fmt.Sprintf("%s %3s %s  %s",
		fileInfo["permissions"],
		"1", // Hard coded link count to match typical ls format
		fileInfo["full_size"],
		fileInfo["name"],
	)
}

func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%d%c", size/div, "KMGTP"[exp])
}

func pointerToTime(t time.Time) *time.Time {
	return &t
}

// NewLsToolFactory creates a factory for LsTool
func NewLsToolFactory(workingDir string) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewLsTool(workingDir)
	}
}
