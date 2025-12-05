package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

// executeBackground executes the sandbox code in the background
func (t *SandboxTool) executeBackground(ctx context.Context, code string, timeout int, libraries []string) (interface{}, error) {
	if t.session == nil {
		return nil, fmt.Errorf("background execution requires session support for %s", ToolNameGoSandbox)
	}

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	commandSummary := summarizeSandboxCommand(code)

	job := &session.BackgroundJob{
		ID:         jobID,
		Command:    commandSummary,
		WorkingDir: t.workingDir,
		StartTime:  time.Now(),
		Completed:  false,
		Stdout:     make([]string, 0),
		Stderr:     make([]string, 0),
		Type:       ToolNameGoSandbox,
		Done:       make(chan struct{}),
	}

	execCtx, cancel := context.WithCancel(context.Background())
	job.CancelFunc = cancel

	t.session.AddBackgroundJob(job)

	go func() {
		defer cancel()
		defer close(job.Done)

		result, err := t.executeWithBuilder(execCtx, code, timeout, libraries)
		job.Mu.Lock()
		job.Completed = true

		if err != nil {
			if errors.Is(err, context.Canceled) {
				job.Stderr = append(job.Stderr, "sandbox execution canceled")
			} else {
				job.Stderr = append(job.Stderr, fmt.Sprintf("sandbox error: %v", err))
			}
			job.ExitCode = -1
			job.Mu.Unlock()
			return
		}

		job.ExitCode = 0

		if resMap, ok := result.(map[string]interface{}); ok {
			if exitVal, ok := resMap["exit_code"]; ok {
				job.ExitCode = coerceExitCode(exitVal)
			}
			if stdout, ok := resMap["stdout"].(string); ok && stdout != "" {
				job.Stdout = append(job.Stdout, splitOutputLines(stdout)...)
			}
			if stderrStr, ok := resMap["stderr"].(string); ok && stderrStr != "" {
				job.Stderr = append(job.Stderr, splitOutputLines(stderrStr)...)
			}
			if timeoutFlag, ok := resMap["timeout"].(bool); ok && timeoutFlag {
				job.Stderr = append(job.Stderr, "sandbox execution timed out")
			}
			if errMsg, ok := resMap["error"].(string); ok && errMsg != "" {
				job.Stderr = append(job.Stderr, errMsg)
			}
		} else if result != nil {
			job.Stdout = append(job.Stdout, fmt.Sprintf("result: %v", result))
		}
		job.Mu.Unlock()
	}()

	return map[string]interface{}{
		"job_id":  jobID,
		"message": "Sandbox execution started in background. Use 'status_program' to stream progress, 'wait_program' to block until completion, or 'stop_program' to terminate.",
	}, nil
}

// summarizeSandboxCommand creates a short summary of the sandbox command for display
func summarizeSandboxCommand(code string) string {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return ToolNameGoSandbox
	}

	line := trimmed
	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		line = trimmed[:idx]
	}
	line = strings.TrimSpace(line)
	if len(line) > 80 {
		line = line[:80] + "..."
	}
	return fmt.Sprintf("%s: %s", ToolNameGoSandbox, line)
}

// splitOutputLines splits output into lines for background job tracking
func splitOutputLines(text string) []string {
	lines := strings.Split(text, "\n")
	// Trim trailing empty line that results from ending newline
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// coerceExitCode converts various numeric types to int for exit code handling
func coerceExitCode(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
