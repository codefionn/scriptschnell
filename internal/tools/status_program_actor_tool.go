package tools

import (
	"context"
	"strings"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/session"
)

// StatusProgramToolWithActor is the status program tool that uses ShellActor
type StatusProgramToolWithActor struct {
	session    *session.Session
	shellActor actor.ShellActor
}

func NewStatusProgramToolWithActor(sess *session.Session, shellActor actor.ShellActor) *StatusProgramToolWithActor {
	return &StatusProgramToolWithActor{
		session:    sess,
		shellActor: shellActor,
	}
}

func (t *StatusProgramToolWithActor) Name() string { return ToolNameStatusProgram }
func (t *StatusProgramToolWithActor) Description() string {
	return "Check status of background programs launched by the shell tool."
}
func (t *StatusProgramToolWithActor) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"job_id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID to check status for (optional, if not provided lists all jobs)",
			},
			"last_n_lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of recent output lines to return (default 50)",
			},
		},
	}
}

func (t *StatusProgramToolWithActor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	jobID := GetStringParam(params, "job_id", "")

	if jobID != "" {
		return t.getJobStatus(ctx, jobID)
	}

	return t.listAllJobs(ctx)
}

func (t *StatusProgramToolWithActor) getJobStatus(ctx context.Context, jobID string) *ToolResult {
	running, exitCode, stdout, stderr, completed, err := t.shellActor.GetJobStatus(ctx, jobID)
	if err != nil {
		return &ToolResult{Error: err.Error()}
	}

	return &ToolResult{
		Result: map[string]interface{}{
			"job_id":    jobID,
			"running":   running,
			"completed": completed,
			"exit_code": exitCode,
			"stdout":    stdout,
			"stderr":    stderr,
		},
	}
}

func (t *StatusProgramToolWithActor) listAllJobs(ctx context.Context) *ToolResult {
	// Get all jobs from session for backward compatibility
	jobs := t.session.ListBackgroundJobs()

	result := make(map[string]interface{})
	for _, job := range jobs {
		// Get fresh status from shell actor
		running, exitCode, stdout, stderr, completed, err := t.shellActor.GetJobStatus(ctx, job.ID)
		if err != nil {
			job.Mu.RLock()
			command := job.Command
			pid := job.PID
			completedVal := job.Completed
			exit := job.ExitCode
			stdoutLines := strings.Join(job.Stdout, "\n")
			stderrLines := strings.Join(job.Stderr, "\n")
			job.Mu.RUnlock()
			// Fallback to session data if actor doesn't have the job
			result[job.ID] = map[string]interface{}{
				"job_id":    job.ID,
				"command":   command,
				"pid":       pid,
				"running":   !completedVal,
				"completed": completedVal,
				"exit_code": exit,
				"stdout":    stdoutLines,
				"stderr":    stderrLines,
			}
		} else {
			result[job.ID] = map[string]interface{}{
				"job_id":    job.ID,
				"command":   job.Command,
				"pid":       job.PID,
				"running":   running,
				"completed": completed,
				"exit_code": exitCode,
				"stdout":    stdout,
				"stderr":    stderr,
			}
		}
	}

	return &ToolResult{Result: result}
}
