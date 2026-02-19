package session

import (
	"time"
)

// TaskExecutionSummary represents a summary of work completed during a task execution
type TaskExecutionSummary struct {
	TaskID        string            `json:"task_id"`
	TaskText      string            `json:"task_text"`
	Status        string            `json:"status"` // "completed", "failed", "partial"
	Summary       string            `json:"summary"`
	FilesRead     []string          `json:"files_read,omitempty"`
	FilesModified []string          `json:"files_modified,omitempty"`
	Errors        []string          `json:"errors,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// TaskExecutionContext provides context for executing a task
type TaskExecutionContext struct {
	OriginalObjective string                 `json:"original_objective"`
	PreviousSummaries []TaskExecutionSummary `json:"previous_summaries,omitempty"`
	TaskIndex         int                    `json:"task_index"`
	TotalTasks        int                    `json:"total_tasks"`
}
