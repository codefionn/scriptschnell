package tools

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// ShellTool executes shell commands
type ShellTool struct {
	session    *session.Session
	workingDir string
}

func NewShellTool(sess *session.Session, workingDir string) *ShellTool {
	return &ShellTool{
		session:    sess,
		workingDir: workingDir,
	}
}

func (t *ShellTool) Name() string {
	return "shell"
}

func (t *ShellTool) Description() string {
	return "Execute shell commands. Use '&' suffix to run in background. Working directory defaults to current directory."
}

func (t *ShellTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Shell command to execute. Append '&' to run in background.",
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for command execution (optional, defaults to session working directory)",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (optional, default 30, max 300)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ShellTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	cmdStr := GetStringParam(params, "command", "")
	if cmdStr == "" {
		return nil, fmt.Errorf("command is required")
	}

	workingDir := GetStringParam(params, "working_dir", "")
	if workingDir == "" {
		workingDir = t.workingDir
	}

	timeout := GetIntParam(params, "timeout", 30)
	if timeout > 300 {
		timeout = 300
	}

	// Check if command should run in background
	background := strings.HasSuffix(strings.TrimSpace(cmdStr), "&")
	if background {
		cmdStr = strings.TrimSuffix(strings.TrimSpace(cmdStr), "&")
		cmdStr = strings.TrimSpace(cmdStr)
	}

	logger.Debug("shell: command='%s', working_dir=%s, background=%v, timeout=%d", cmdStr, workingDir, background, timeout)

	// Parse and execute command
	if background {
		return t.executeBackground(ctx, cmdStr, workingDir)
	}

	return t.executeForeground(ctx, cmdStr, workingDir, timeout)
}

func (t *ShellTool) executeForeground(ctx context.Context, cmdStr, workingDir string, timeoutSecs int) (interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workingDir

	output, err := cmd.CombinedOutput()
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			logger.Warn("shell: command exited with code %d", exitCode)
		} else {
			logger.Error("shell: failed to execute command: %v", err)
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	timedOut := ctx.Err() == context.DeadlineExceeded
	if timedOut {
		logger.Warn("shell: command timed out after %ds", timeoutSecs)
	} else {
		logger.Info("shell: command completed successfully (exit_code=%d, output_bytes=%d)", exitCode, len(output))
	}

	return map[string]interface{}{
		"stdout":    string(output),
		"exit_code": exitCode,
		"timeout":   timedOut,
	}, nil
}

func (t *ShellTool) executeBackground(ctx context.Context, cmdStr, workingDir string) (interface{}, error) {
	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	logger.Debug("shell: starting background job: %s", jobID)

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = workingDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("shell: failed to create stdout pipe: %v", err)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.Error("shell: failed to create stderr pipe: %v", err)
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		logger.Error("shell: failed to start background command: %v", err)
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	logger.Info("shell: background job started: %s", jobID)

	job := &session.BackgroundJob{
		ID:         jobID,
		Command:    cmdStr,
		WorkingDir: workingDir,
		StartTime:  time.Now(),
		Completed:  false,
		Stdout:     make([]string, 0),
		Stderr:     make([]string, 0),
	}

	t.session.AddBackgroundJob(job)

	// Read output in goroutines
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			job.Stdout = append(job.Stdout, scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			job.Stderr = append(job.Stderr, scanner.Text())
		}
	}()

	// Wait for completion in background
	go func() {
		err := cmd.Wait()
		job.Completed = true
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				job.ExitCode = exitErr.ExitCode()
			}
		}
	}()

	return map[string]interface{}{
		"job_id":  jobID,
		"message": "Command started in background. Use 'status' tool to check progress.",
	}, nil
}

// StatusTool checks status of background jobs
type StatusTool struct {
	session *session.Session
}

func NewStatusTool(sess *session.Session) *StatusTool {
	return &StatusTool{
		session: sess,
	}
}

func (t *StatusTool) Name() string {
	return "status"
}

func (t *StatusTool) Description() string {
	return "Check status of background shell processes and retrieve their output."
}

func (t *StatusTool) Parameters() map[string]interface{} {
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

func (t *StatusTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	jobID := GetStringParam(params, "job_id", "")
	lastNLines := GetIntParam(params, "last_n_lines", 50)

	if jobID == "" {
		// List all jobs
		jobs := t.session.ListBackgroundJobs()
		jobList := make([]map[string]interface{}, len(jobs))
		for i, job := range jobs {
			jobList[i] = map[string]interface{}{
				"job_id":    job.ID,
				"command":   job.Command,
				"completed": job.Completed,
				"exit_code": job.ExitCode,
				"runtime":   time.Since(job.StartTime).String(),
			}
		}
		return map[string]interface{}{
			"jobs": jobList,
		}, nil
	}

	// Get specific job
	job, ok := t.session.GetBackgroundJob(jobID)
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	// Combine stdout and stderr
	combined := make([]string, 0, len(job.Stdout)+len(job.Stderr))
	combined = append(combined, job.Stdout...)
	combined = append(combined, job.Stderr...)

	// Get last N lines
	start := 0
	if len(combined) > lastNLines {
		start = len(combined) - lastNLines
	}
	output := combined[start:]

	return map[string]interface{}{
		"job_id":    job.ID,
		"command":   job.Command,
		"completed": job.Completed,
		"exit_code": job.ExitCode,
		"runtime":   time.Since(job.StartTime).String(),
		"output":    strings.Join(output, "\n"),
	}, nil
}
