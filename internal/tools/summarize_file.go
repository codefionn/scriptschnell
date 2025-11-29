package tools

import (
	"context"
	"fmt"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/llm"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// SummarizeFileTool summarizes file content using LLM
type SummarizeFileTool struct {
	fs              fs.FileSystem
	session         *session.Session
	summarizeClient llm.Client
}

func NewSummarizeFileTool(filesystem fs.FileSystem, sess *session.Session, summarizeClient llm.Client) *SummarizeFileTool {
	return &SummarizeFileTool{
		fs:              filesystem,
		session:         sess,
		summarizeClient: summarizeClient,
	}
}

func (t *SummarizeFileTool) Name() string {
	return ToolNameReadFileSummarized
}

func (t *SummarizeFileTool) Description() string {
	return "Read and summarize a file using AI. Useful for large files or when you need a targeted summary based on specific goals."
}

func (t *SummarizeFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read and summarize",
			},
			"goal": map[string]interface{}{
				"type":        "string",
				"description": "What the summary should focus on (e.g., 'identify all exported functions', 'explain the main algorithm', 'list all API endpoints')",
			},
		},
		"required": []string{"path", "goal"},
	}
}

func (t *SummarizeFileTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: fmt.Sprintf("path is required")}
	}

	goal := GetStringParam(params, "goal", "")
	if goal == "" {
		return &ToolResult{Error: fmt.Sprintf("goal is required")}
	}

	// Check if file exists
	exists, err := t.fs.Exists(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error checking file: %v", err)}
	}
	if !exists {
		return &ToolResult{Error: fmt.Sprintf("file not found: %s", path)}
	}

	// Read file
	data, err := t.fs.ReadFile(ctx, path)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error reading file: %v", err)}
	}

	content := string(data)

	// Create prompt for summarization
	prompt := fmt.Sprintf(`Please summarize the following file based on this goal: %s

File: %s
Content:
%s

Provide a concise summary focusing on the specified goal.`, goal, path, content)

	// Call summarize LLM
	response, err := t.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("error generating summary: %v", err)}
	}

	// Track file as read in session
	if t.session != nil {
		t.session.TrackFileRead(path, content)
	}

	return &ToolResult{Result: map[string]interface{}{
		"path":    path,
		"goal":    goal,
		"summary": response,
	}}
}
