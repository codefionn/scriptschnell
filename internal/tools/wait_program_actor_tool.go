package tools

import (
	"context"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/session"
)

// WaitProgramToolWithActor is the wait program tool that uses ShellActor
type WaitProgramToolWithActor struct {
	session    *session.Session
	shellActor actor.ShellActor
}

func NewWaitProgramToolWithActor(sess *session.Session, shellActor actor.ShellActor) *WaitProgramToolWithActor {
	return &WaitProgramToolWithActor{
		session:    sess,
		shellActor: shellActor,
	}
}

func (t *WaitProgramToolWithActor) Name() string { return ToolNameWaitProgram }
func (t *WaitProgramToolWithActor) Description() string {
	return "Block until a background program completes and return its final output."
}
func (t *WaitProgramToolWithActor) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"job_id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID to wait for",
			},
			"last_n_lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of recent output lines to return (0 means all output)",
			},
		},
		"required": []string{"job_id"},
	}
}

func (t *WaitProgramToolWithActor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	jobID := GetStringParam(params, "job_id", "")
	if jobID == "" {
		return &ToolResult{Error: "job_id is required"}
	}

	exitCode, stdout, stderr, err := t.shellActor.WaitForJob(ctx, jobID)
	if err != nil {
		return &ToolResult{Error: err.Error()}
	}

	return &ToolResult{
		Result: map[string]interface{}{
			"job_id":    jobID,
			"exit_code": exitCode,
			"stdout":    stdout,
			"stderr":    stderr,
			"completed": true,
		},
	}
}
