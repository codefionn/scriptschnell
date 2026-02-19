package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

// TaskSummaryTool allows the agent to explicitly report task completion summary
type TaskSummaryTool struct {
	session *session.Session
}

// NewTaskSummaryTool creates a new task summary tool
func NewTaskSummaryTool(sess *session.Session) *TaskSummaryTool {
	return &TaskSummaryTool{session: sess}
}

// Name returns the tool name
func (t *TaskSummaryTool) Name() string {
	return "task_summary"
}

// Description returns the tool description
func (t *TaskSummaryTool) Description() string {
	return "Report the summary of work completed during a task. Call this when you have finished your task to provide a structured summary of what was done, including files read/modified, any issues encountered, and the overall outcome."
}

// Parameters returns the tool parameters
func (t *TaskSummaryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "A detailed summary of what was accomplished in this task, including the main changes made and any important outcomes.",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"completed", "partial", "failed"},
				"description": "The status of task completion. Use 'completed' if the task was fully done, 'partial' if some parts were completed but not all, or 'failed' if the task could not be completed.",
			},
			"errors": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional list of errors or issues encountered during the task. Leave empty if no errors.",
			},
		},
		"required": []string{"summary", "status"},
	}
}

// Execute executes the task summary tool
func (t *TaskSummaryTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	// Extract parameters
	summaryText := GetStringParam(params, "summary", "")
	if summaryText == "" {
		return &ToolResult{Error: "missing required parameter 'summary'"}
	}

	status := GetStringParam(params, "status", "")
	if status == "" {
		return &ToolResult{Error: "missing required parameter 'status'"}
	}

	// Validate status
	if status != "completed" && status != "partial" && status != "failed" {
		return &ToolResult{Error: fmt.Sprintf("invalid status '%s', must be one of: completed, partial, failed", status)}
	}

	// Extract optional errors parameter
	var errors []string
	if errsParam, ok := params["errors"]; ok {
		if errsSlice, ok := errsParam.([]interface{}); ok {
			for _, e := range errsSlice {
				if errStr, ok := e.(string); ok {
					errors = append(errors, errStr)
				}
			}
		}
	}

	// Create task execution summary
	taskSummary := &session.TaskExecutionSummary{
		Summary:       summaryText,
		Status:        status,
		Errors:        errors,
		Timestamp:     time.Now(),
		FilesRead:     t.session.GetFilesRead(),
		FilesModified: t.session.GetModifiedFiles(),
		Metadata:      make(map[string]string),
	}

	// Store the summary in the session (for retrieval by orchestrator)
	t.session.SetTaskExecutionSummary(taskSummary)

	// Build response message
	response := fmt.Sprintf("Task summary reported:\n- Status: %s\n", status)
	if len(taskSummary.FilesModified) > 0 {
		response += fmt.Sprintf("- Files modified: %d\n", len(taskSummary.FilesModified))
	}
	if len(errors) > 0 {
		response += fmt.Sprintf("- Errors: %d\n", len(errors))
	}

	return &ToolResult{
		Result: response,
	}
}

// TaskSummaryToolSpec is the static specification for the task summary tool
type TaskSummaryToolSpec struct{}

func (s *TaskSummaryToolSpec) Name() string {
	return "task_summary"
}

func (s *TaskSummaryToolSpec) Description() string {
	return "Report the summary of work completed during a task. Call this when you have finished your task to provide a structured summary of what was done, including files read/modified, any issues encountered, and the overall outcome."
}

func (s *TaskSummaryToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "A detailed summary of what was accomplished in this task, including the main changes made and any important outcomes.",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"completed", "partial", "failed"},
				"description": "The status of task completion. Use 'completed' if the task was fully done, 'partial' if some parts were completed but not all, or 'failed' if the task could not be completed.",
			},
			"errors": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional list of errors or issues encountered during the task. Leave empty if no errors.",
			},
		},
		"required": []string{"summary", "status"},
	}
}

// NewTaskSummaryToolFactory creates a factory function for the task summary tool
func NewTaskSummaryToolFactory(sess *session.Session) func(*Registry) ToolExecutor {
	return func(_ *Registry) ToolExecutor {
		return NewTaskSummaryTool(sess)
	}
}
