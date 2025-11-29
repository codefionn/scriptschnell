package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// WriteFileJSONTool applies changes to a file based on a JSON payload.
type WriteFileJSONTool struct {
	fs      fs.FileSystem
	session *session.Session
}

// NewWriteFileJSONTool creates a new instance of the JSON file writing tool.
func NewWriteFileJSONTool(filesystem fs.FileSystem, sess *session.Session) *WriteFileJSONTool {
	return &WriteFileJSONTool{
		fs:      filesystem,
		session: sess,
	}
}

func (t *WriteFileJSONTool) Name() string {
	return ToolNameWriteFileJSON
}

func (t *WriteFileJSONTool) Description() string {
	return `Update an existing file using a JSON structure of operations.
The operations array contains a sequence of modifications to be applied to the file.
Each operation is one of:
- update: replaces the content of a specific line.
- insert_before: inserts a new line before a specific line.
- insert_after: inserts a new line after a specific line.`
}

func (t *WriteFileJSONTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to update (relative to working directory)",
			},
			"operations": map[string]interface{}{
				"type":        "array",
				"description": "A list of operations to apply to the file. The operations are applied in the given order.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"method": map[string]interface{}{
							"type":        "string",
							"description": "The operation to perform.",
							"enum":        []string{"update", "insert_before", "insert_after"},
						},
						"line": map[string]interface{}{
							"type":        "integer",
							"description": "The 1-based line number to apply the operation to.",
						},
						"line_content": map[string]interface{}{
							"type":        "string",
							"description": "The content to use for the operation.",
						},
					},
					"required": []string{"method", "line", "line_content"},
				},
			},
		},
		"required": []string{"path", "operations"},
	}
}

type fileOperation struct {
	Method      string `json:"method"`
	Line        int    `json:"line"`
	LineContent string `json:"line_content"`
}

func (t *WriteFileJSONTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	operationsParam, ok := params["operations"]
	if !ok {
		return nil, fmt.Errorf("operations is required")
	}

	// Re-marshal and unmarshal to decode into the struct. This is a common Go trick for map[string]interface{}
	operationsData, err := json.Marshal(operationsParam)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal operations: %w", err)
	}

	var operations []fileOperation
	if err := json.Unmarshal(operationsData, &operations); err != nil {
		// If it fails, maybe it's a string of JSON.
		opsStr, ok := operationsParam.(string)
		if !ok {
			return nil, fmt.Errorf("operations must be an array of objects or a JSON string, failed to unmarshal: %w", err)
		}
		if err := json.Unmarshal([]byte(opsStr), &operations); err != nil {
			return nil, fmt.Errorf("failed to parse operations JSON string: %w", err)
		}
	}

	if len(operations) == 0 {
		return nil, fmt.Errorf("operations cannot be empty")
	}

	logger.Debug("write_file_json: path=%s", path)

	if t.fs == nil {
		return nil, fmt.Errorf("file system is not configured")
	}

	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		logger.Error("write_file_json: error checking if file exists: %v", err)
		return nil, fmt.Errorf("error checking file: %w", err)
	}

	if !exists {
		return nil, fmt.Errorf("cannot apply operations to non-existent file: %s (use create_file instead)", path)
	}

	if t.session != nil && !t.session.WasFileRead(path) {
		return nil, fmt.Errorf("file %s was not read in this session; read it before applying operations", path)
	}

	currentData, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("error reading current file: %w", err)
	}

	finalContent, err := applyJSONOperations(string(currentData), operations)
	if err != nil {
		return nil, fmt.Errorf("error applying operations: %w", err)
	}

	if err := t.fs.WriteFile(ctx, path, []byte(finalContent)); err != nil {
		logger.Error("write_file_json: error writing file: %v", err)
		return nil, fmt.Errorf("error writing file: %w", err)
	}

	if t.session != nil {
		t.session.TrackFileModified(path)
	}

	logger.Info("write_file_json: updated %s (%d bytes)", path, len(finalContent))

	return map[string]interface{}{
		"path":          path,
		"bytes_written": len(finalContent),
		"updated":       true,
	}, nil
}

type lineWithAdditions struct {
	originalContent string
	insertBefore    []string
	insertAfter     []string
	isUpdated       bool
}

func applyJSONOperations(original string, operations []fileOperation) (string, error) {
	originalLines := strings.Split(original, "\n")

	// Create a temporary structure to hold the changes
	augmentedLines := make([]*lineWithAdditions, len(originalLines))
	for i, line := range originalLines {
		augmentedLines[i] = &lineWithAdditions{originalContent: line}
	}

	for _, op := range operations {
		if op.Line < 1 || op.Line > len(originalLines) {
			return "", fmt.Errorf("line number %d is out of bounds for the original file (1-%d)", op.Line, len(originalLines))
		}
		lineIdx := op.Line - 1

		switch op.Method {
		case "update":
			// An update operation replaces the line. If the line was already updated by a previous operation, it will be overwritten.
			augmentedLines[lineIdx].originalContent = op.LineContent
			augmentedLines[lineIdx].isUpdated = true
		case "insert_before":
			// If a line is updated, inserts are relative to the new content of the line, but happen at the same position.
			augmentedLines[lineIdx].insertBefore = append(augmentedLines[lineIdx].insertBefore, op.LineContent)
		case "insert_after":
			augmentedLines[lineIdx].insertAfter = append(augmentedLines[lineIdx].insertAfter, op.LineContent)
		default:
			return "", fmt.Errorf("unknown operation method: %s", op.Method)
		}
	}

	// Build the final content
	var resultLines []string
	for _, augLine := range augmentedLines {
		resultLines = append(resultLines, augLine.insertBefore...)
		resultLines = append(resultLines, augLine.originalContent)
		resultLines = append(resultLines, augLine.insertAfter...)
	}

	return strings.Join(resultLines, "\n"), nil
}
