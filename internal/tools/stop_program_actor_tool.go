package tools

import (
	"context"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/actor"
)

// StopProgramToolWithActor is the stop program tool that uses ShellActor
type StopProgramToolWithActor struct {
	shellActor actor.ShellActor
}

func NewStopProgramToolWithActor(shellActor actor.ShellActor) *StopProgramToolWithActor {
	return &StopProgramToolWithActor{
		shellActor: shellActor,
	}
}

func (t *StopProgramToolWithActor) Name() string { return ToolNameStopProgram }
func (t *StopProgramToolWithActor) Description() string {
	return "Stop a background program by sending SIGTERM or SIGKILL."
}
func (t *StopProgramToolWithActor) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"job_id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID to stop",
			},
			"signal": map[string]interface{}{
				"type":        "string",
				"description": "Signal to send (SIGTERM or SIGKILL, defaults to SIGTERM)",
			},
		},
		"required": []string{"job_id"},
	}
}

func (t *StopProgramToolWithActor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	jobID := GetStringParam(params, "job_id", "")
	if jobID == "" {
		return &ToolResult{Error: "job_id is required"}
	}

	signal := GetStringParam(params, "signal", "SIGTERM")
	if signal != "SIGTERM" && signal != "SIGKILL" {
		signal = "SIGTERM"
	}

	err := t.shellActor.StopJob(ctx, jobID, signal)
	if err != nil {
		return &ToolResult{Error: err.Error()}
	}

	return &ToolResult{
		Result: map[string]interface{}{
			"job_id":  jobID,
			"signal":  signal,
			"message": fmt.Sprintf("Signal %s sent to job %s", signal, jobID),
		},
	}
}
