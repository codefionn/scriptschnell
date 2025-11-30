package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/actor"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// ShellToolWithActor is the shell tool implementation that uses ShellActor
type ShellToolWithActor struct {
	session    *session.Session
	workingDir string
	shellActor actor.ShellActor
}

// NewShellToolWithActor creates a new shell tool that uses ShellActor
func NewShellToolWithActor(sess *session.Session, workingDir string, shellActor actor.ShellActor) *ShellToolWithActor {
	return &ShellToolWithActor{
		session:    sess,
		workingDir: workingDir,
		shellActor: shellActor,
	}
}

// Legacy interface implementation for backward compatibility
func (t *ShellToolWithActor) Name() string        { return ToolNameShell }
func (t *ShellToolWithActor) Description() string { return (&ShellToolSpec{}).Description() }
func (t *ShellToolWithActor) Parameters() map[string]interface{} {
	return (&ShellToolSpec{}).Parameters()
}

func (t *ShellToolWithActor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	cmdStr := GetStringParam(params, "command", "")
	if cmdStr == "" {
		return &ToolResult{Error: "command is required"}
	}

	backgroundParam := GetBoolParam(params, "background", false)

	trimmed := strings.TrimSpace(cmdStr)
	trailingAmpersand := strings.HasSuffix(trimmed, "&")
	if trailingAmpersand {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "&"))
	}

	if trimmed == "" {
		return &ToolResult{Error: "command is empty after processing"}
	}

	cmdStr = trimmed
	background := backgroundParam || trailingAmpersand

	workingDir := GetStringParam(params, "working_dir", "")
	if workingDir == "" {
		workingDir = t.workingDir
	}

	timeout := GetIntParam(params, "timeout", 30)
	if timeout > 300 {
		timeout = 300
	}

	logger.Debug("shell: command='%s', working_dir=%s, background=%v, timeout=%d", cmdStr, workingDir, background, timeout)

	if background {
		return t.executeBackground(ctx, cmdStr, workingDir)
	}

	return t.executeForeground(ctx, cmdStr, workingDir, timeout)
}

func (t *ShellToolWithActor) executeForeground(ctx context.Context, cmdStr, workingDir string, timeoutSecs int) *ToolResult {
	timeout := time.Duration(timeoutSecs) * time.Second
	if timeoutSecs <= 0 {
		timeout = 0
	}

	stdout, stderr, exitCode, err := t.shellActor.ExecuteCommand(ctx, cmdStr, workingDir, timeout, "")
	if err != nil {
		return &ToolResult{
			Result: map[string]interface{}{
				"stdout":    stdout,
				"stderr":    stderr,
				"exit_code": exitCode,
			},
			Error: err.Error(),
		}
	}

	return &ToolResult{
		Result: map[string]interface{}{
			"stdout":    stdout,
			"stderr":    stderr,
			"exit_code": exitCode,
		},
	}
}

func (t *ShellToolWithActor) executeBackground(ctx context.Context, cmdStr, workingDir string) *ToolResult {
	logger.Debug("shell: starting background job (explicit request)")

	jobID, pid, err := t.shellActor.ExecuteCommandBackground(ctx, cmdStr, workingDir)
	if err != nil {
		logger.Error("shell: failed to start background command: %v", err)
		return &ToolResult{Error: fmt.Sprintf("failed to start command: %v", err)}
	}

	logger.Info("shell: background job started: %s (pid=%d)", jobID, pid)

	return &ToolResult{
		Result: map[string]interface{}{
			"job_id":  jobID,
			"pid":     pid,
			"message": shellBackgroundMessage,
		},
	}
}
