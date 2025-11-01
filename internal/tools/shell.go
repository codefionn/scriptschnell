package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
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
	return "Execute shell commands. Working directory defaults to current directory. Supports background execution."
}

func (t *ShellTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Shell command to execute.",
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for command execution (optional, defaults to session working directory)",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (optional, default 30, max 300)",
			},
			"background": map[string]interface{}{
				"type":        "boolean",
				"description": "Run command in background and return job identifier.",
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

	backgroundParam := GetBoolParam(params, "background", false)

	trimmed := strings.TrimSpace(cmdStr)
	trailingAmpersand := strings.HasSuffix(trimmed, "&")
	if trailingAmpersand {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "&"))
	}

	if trimmed == "" {
		return nil, fmt.Errorf("command is empty after processing")
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

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	logger.Info("shell: background job started: %s (pid=%d)", jobID, pid)

	job := &session.BackgroundJob{
		ID:         jobID,
		Command:    cmdStr,
		WorkingDir: workingDir,
		StartTime:  time.Now(),
		Completed:  false,
		Stdout:     make([]string, 0),
		Stderr:     make([]string, 0),
		Type:       "shell",
		Done:       make(chan struct{}),
	}

	job.Process = cmd.Process
	job.PID = pid

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
		defer close(job.Done)
		err := cmd.Wait()
		job.Completed = true
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				job.ExitCode = exitErr.ExitCode()
			} else {
				job.ExitCode = -1
				job.Stderr = append(job.Stderr, fmt.Sprintf("command error: %v", err))
			}
		} else {
			job.ExitCode = 0
		}
		job.Process = nil
	}()

	return map[string]interface{}{
		"job_id":  jobID,
		"pid":     job.PID,
		"message": "Command started in background. Use 'status_program' to stream progress, 'wait_program' to block until completion, or 'stop_program' to terminate.",
	}, nil
}

// StatusProgramTool checks status of background jobs
type StatusProgramTool struct {
	session *session.Session
}

func NewStatusProgramTool(sess *session.Session) *StatusProgramTool {
	return &StatusProgramTool{
		session: sess,
	}
}

func (t *StatusProgramTool) Name() string {
	return "status_program"
}

func (t *StatusProgramTool) Description() string {
	return "Check status of background programs launched by the shell or sandbox tools."
}

func (t *StatusProgramTool) Parameters() map[string]interface{} {
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

func (t *StatusProgramTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	jobID := GetStringParam(params, "job_id", "")
	lastNLines := GetIntParam(params, "last_n_lines", 50)

	if jobID == "" {
		// List all jobs
		jobs := t.session.ListBackgroundJobs()
		jobList := make([]map[string]interface{}, len(jobs))
		for i, job := range jobs {
			jobList[i] = map[string]interface{}{
				"job_id":         job.ID,
				"command":        job.Command,
				"completed":      job.Completed,
				"exit_code":      job.ExitCode,
				"runtime":        time.Since(job.StartTime).String(),
				"pid":            job.PID,
				"type":           job.Type,
				"stop_requested": job.StopRequested,
				"last_signal":    job.LastSignal,
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

	return buildJobSnapshot(job, lastNLines), nil
}

// WaitProgramTool blocks until a background job finishes and returns its output
type WaitProgramTool struct {
	session *session.Session
}

func NewWaitProgramTool(sess *session.Session) *WaitProgramTool {
	return &WaitProgramTool{session: sess}
}

func (t *WaitProgramTool) Name() string {
	return "wait_program"
}

func (t *WaitProgramTool) Description() string {
	return "Block until a background program completes and return its final output."
}

func (t *WaitProgramTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"job_id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID to wait for.",
			},
			"last_n_lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of recent output lines to return (0 means all output).",
			},
		},
		"required": []string{"job_id"},
	}
}

func (t *WaitProgramTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	jobID := GetStringParam(params, "job_id", "")
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	lastNLines := GetIntParam(params, "last_n_lines", 0)
	if lastNLines < 0 {
		lastNLines = 0
	}

	job, ok := t.session.GetBackgroundJob(jobID)
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	if !job.Completed {
		done := job.Done
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				if job.Completed {
					break
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ticker.C:
				}
			}
		}
	}

	result := buildJobSnapshot(job, lastNLines)
	result["waited"] = true
	return result, nil
}

func buildJobSnapshot(job *session.BackgroundJob, lastNLines int) map[string]interface{} {
	computeStart := func(length int) int {
		if lastNLines <= 0 || length <= lastNLines {
			return 0
		}
		return length - lastNLines
	}

	combined := make([]string, 0, len(job.Stdout)+len(job.Stderr))
	combined = append(combined, job.Stdout...)
	combined = append(combined, job.Stderr...)

	start := computeStart(len(combined))
	stdoutStart := computeStart(len(job.Stdout))
	stderrStart := computeStart(len(job.Stderr))

	output := strings.Join(combined[start:], "\n")
	stdout := strings.Join(job.Stdout[stdoutStart:], "\n")
	stderr := strings.Join(job.Stderr[stderrStart:], "\n")

	return map[string]interface{}{
		"job_id":         job.ID,
		"command":        job.Command,
		"completed":      job.Completed,
		"exit_code":      job.ExitCode,
		"runtime":        time.Since(job.StartTime).String(),
		"pid":            job.PID,
		"type":           job.Type,
		"stop_requested": job.StopRequested,
		"last_signal":    job.LastSignal,
		"output":         output,
		"stdout":         stdout,
		"stderr":         stderr,
	}
}

// StopProgramTool sends termination signals to background processes
type StopProgramTool struct {
	session *session.Session
}

func NewStopProgramTool(sess *session.Session) *StopProgramTool {
	return &StopProgramTool{session: sess}
}

func (t *StopProgramTool) Name() string {
	return "stop_program"
}

func (t *StopProgramTool) Description() string {
	return "Stop a background program by sending SIGTERM or SIGKILL."
}

func (t *StopProgramTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"job_id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID returned from a background execution.",
			},
			"signal": map[string]interface{}{
				"type":        "string",
				"description": "Signal to send (SIGTERM or SIGKILL). Defaults to SIGTERM.",
			},
		},
		"required": []string{"job_id"},
	}
}

func (t *StopProgramTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	jobID := GetStringParam(params, "job_id", "")
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	job, ok := t.session.GetBackgroundJob(jobID)
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	signalInput := strings.ToUpper(strings.TrimSpace(GetStringParam(params, "signal", "SIGTERM")))
	var (
		sig        syscall.Signal
		signalName string
	)
	switch signalInput {
	case "", "TERM", "SIGTERM":
		sig = syscall.SIGTERM
		signalName = "SIGTERM"
	case "KILL", "SIGKILL":
		sig = syscall.SIGKILL
		signalName = "SIGKILL"
	default:
		return nil, fmt.Errorf("unsupported signal: %s", signalInput)
	}

	if job.Completed {
		return map[string]interface{}{
			"job_id":    job.ID,
			"message":   "Job already completed.",
			"completed": true,
			"exit_code": job.ExitCode,
		}, nil
	}

	var err error
	if job.Process != nil {
		if signalName == "SIGKILL" {
			err = job.Process.Kill()
		} else {
			err = job.Process.Signal(sig)
		}
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			logger.Error("stop_program: failed to send %s to job %s (pid=%d): %v", signalName, job.ID, job.PID, err)
			return nil, fmt.Errorf("failed to send %s: %w", signalName, err)
		}
	} else if job.CancelFunc != nil {
		job.CancelFunc()
	} else {
		return nil, fmt.Errorf("no active process to signal for job %s", job.ID)
	}

	job.StopRequested = true
	job.LastSignal = signalName

	logger.Info("stop_program: sent %s to job %s (pid=%d)", signalName, job.ID, job.PID)

	return map[string]interface{}{
		"job_id":  job.ID,
		"signal":  signalName,
		"message": fmt.Sprintf("Signal %s sent to background job.", signalName),
	}, nil
}
