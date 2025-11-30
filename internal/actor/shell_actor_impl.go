package actor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

// shellActorImpl implements the ShellActor interface
type shellActorImpl struct {
	id      string
	session *session.Session
	jobs    map[string]*shellJob
	mu      sync.RWMutex
}

// shellJob represents a running background job
type shellJob struct {
	ID           string
	Command      string
	WorkingDir   string
	PID          int
	Process      *exec.Cmd
	ProcessGroup int
	Stdout       []string
	Stderr       []string
	ExitCode     int
	Completed    bool
	StartedAt    time.Time
	Done         chan struct{}
}

// NewShellActor creates a new shell actor
func NewShellActor(id string, sess *session.Session) ShellActor {
	return &shellActorImpl{
		id:      id,
		session: sess,
		jobs:    make(map[string]*shellJob),
	}
}

func (a *shellActorImpl) ID() string {
	return a.id
}

func (a *shellActorImpl) Start(ctx context.Context) error {
	logger.Debug("shell actor %s: starting", a.id)
	return nil
}

func (a *shellActorImpl) Stop(ctx context.Context) error {
	logger.Debug("shell actor %s: stopping", a.id)

	a.mu.Lock()
	defer a.mu.Unlock()

	// Stop all running jobs
	for _, job := range a.jobs {
		if !job.Completed && job.Process != nil {
			a.stopJobInternal(job, "SIGTERM")
		}
	}

	return nil
}

func (a *shellActorImpl) Receive(ctx context.Context, msg Message) error {
	switch m := msg.(type) {
	case ShellExecuteRequest:
		response := a.handleExecuteRequest(ctx, m)
		select {
		case m.ResponseCh <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	case ShellStatusRequest:
		response := a.handleStatusRequest(ctx, m)
		select {
		case m.ResponseCh <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	case ShellWaitRequest:
		response := a.handleWaitRequest(ctx, m)
		select {
		case m.ResponseCh <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	case ShellStopRequest:
		response := a.handleStopRequest(ctx, m)
		select {
		case m.ResponseCh <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
	return nil
}

func (a *shellActorImpl) ExecuteCommand(ctx context.Context, command, workingDir string, timeout time.Duration, stdin string) (string, string, int, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Dir = workingDir

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitCode int
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	} else {
		exitCode = 0
	}

	return stdout.String(), stderr.String(), exitCode, err
}

func (a *shellActorImpl) ExecuteCommandBackground(ctx context.Context, command, workingDir string) (string, int, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workingDir
	configureProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", 0, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", 0, fmt.Errorf("failed to start command: %w", err)
	}

	startedAt := time.Now()
	jobID, job := a.registerBackgroundJob(cmd, command, workingDir, startedAt)

	// Read output in goroutines
	go a.readOutput(stdout, job, false)
	go a.readOutput(stderr, job, true)

	// Wait for completion in background
	go a.waitForCompletion(job)

	logger.Info("shell actor %s: background job started: %s (pid=%d)", a.id, jobID, job.PID)
	return jobID, job.PID, nil
}

func (a *shellActorImpl) GetJobStatus(ctx context.Context, jobID string) (bool, int, string, string, bool, error) {
	a.mu.RLock()
	job, exists := a.jobs[jobID]
	a.mu.RUnlock()

	if !exists {
		return false, 0, "", "", false, fmt.Errorf("job %s not found", jobID)
	}

	a.mu.RLock()
	running := !job.Completed
	exitCode := job.ExitCode
	stdout := strings.Join(job.Stdout, "\n")
	stderr := strings.Join(job.Stderr, "\n")
	completed := job.Completed
	a.mu.RUnlock()

	return running, exitCode, stdout, stderr, completed, nil
}

func (a *shellActorImpl) WaitForJob(ctx context.Context, jobID string) (int, string, string, error) {
	a.mu.RLock()
	job, exists := a.jobs[jobID]
	a.mu.RUnlock()

	if !exists {
		return 0, "", "", fmt.Errorf("job %s not found", jobID)
	}

	select {
	case <-job.Done:
		a.mu.RLock()
		exitCode := job.ExitCode
		stdout := strings.Join(job.Stdout, "\n")
		stderr := strings.Join(job.Stderr, "\n")
		a.mu.RUnlock()
		return exitCode, stdout, stderr, nil
	case <-ctx.Done():
		return 0, "", "", ctx.Err()
	}
}

func (a *shellActorImpl) StopJob(ctx context.Context, jobID string, signal string) error {
	a.mu.Lock()
	job, exists := a.jobs[jobID]
	if !exists {
		a.mu.Unlock()
		return fmt.Errorf("job %s not found", jobID)
	}
	err := a.stopJobInternal(job, signal)
	a.mu.Unlock()
	return err
}

// Message handlers
func (a *shellActorImpl) handleExecuteRequest(ctx context.Context, req ShellExecuteRequest) ShellExecuteResponse {
	if req.Background {
		jobID, pid, err := a.ExecuteCommandBackground(ctx, req.Command, req.WorkingDir)
		if err != nil {
			return ShellExecuteResponse{Error: err.Error()}
		}
		return ShellExecuteResponse{
			JobID:   jobID,
			PID:     pid,
			Message: "Command started in background. Use status_program to stream progress, wait_program to block until completion, or stop_program to terminate.",
		}
	}

	stdout, stderr, exitCode, err := a.ExecuteCommand(ctx, req.Command, req.WorkingDir, req.Timeout, req.Stdin)
	if err != nil {
		return ShellExecuteResponse{
			ExitCode: exitCode,
			Stdout:   stdout,
			Stderr:   stderr,
			Error:    err.Error(),
			Done:     true,
		}
	}

	return ShellExecuteResponse{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Done:     true,
	}
}

func (a *shellActorImpl) handleStatusRequest(ctx context.Context, req ShellStatusRequest) ShellStatusResponse {
	running, exitCode, stdout, stderr, completed, err := a.GetJobStatus(ctx, req.JobID)
	if err != nil {
		return ShellStatusResponse{Error: err.Error()}
	}

	return ShellStatusResponse{
		JobID:     req.JobID,
		Running:   running,
		ExitCode:  exitCode,
		Stdout:    stdout,
		Stderr:    stderr,
		Completed: completed,
	}
}

func (a *shellActorImpl) handleWaitRequest(ctx context.Context, req ShellWaitRequest) ShellWaitResponse {
	exitCode, stdout, stderr, err := a.WaitForJob(ctx, req.JobID)
	if err != nil {
		return ShellWaitResponse{Error: err.Error()}
	}

	return ShellWaitResponse{
		JobID:    req.JobID,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	}
}

func (a *shellActorImpl) handleStopRequest(ctx context.Context, req ShellStopRequest) ShellStopResponse {
	err := a.StopJob(ctx, req.JobID, req.Signal)
	if err != nil {
		return ShellStopResponse{Error: err.Error()}
	}

	return ShellStopResponse{Success: true}
}

// Helper methods
func (a *shellActorImpl) registerBackgroundJob(cmd *exec.Cmd, command, workingDir string, startedAt time.Time) (string, *shellJob) {
	job := &shellJob{
		ID:         generateJobID(),
		Command:    command,
		WorkingDir: workingDir,
		PID:        cmd.Process.Pid,
		Process:    cmd,
		StartedAt:  startedAt,
		Done:       make(chan struct{}),
		Stdout:     make([]string, 0),
		Stderr:     make([]string, 0),
	}

	job.ProcessGroup = getProcessGroupID(cmd)

	a.mu.Lock()
	a.jobs[job.ID] = job
	a.mu.Unlock()

	// Also register with session for legacy compatibility
	if a.session != nil {
		sessionJob := &session.BackgroundJob{
			ID:             job.ID,
			Command:        command,
			PID:            job.PID,
			Process:        cmd.Process,
			ProcessGroupID: job.ProcessGroup,
			StartTime:      startedAt,
			Done:           job.Done,
			Stdout:         make([]string, 0),
			Stderr:         make([]string, 0),
			Type:           "shell",
		}
		a.session.AddBackgroundJob(sessionJob)
	}

	return job.ID, job
}

func (a *shellActorImpl) readOutput(reader io.ReadCloser, job *shellJob, isStderr bool) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		a.mu.Lock()
		if isStderr {
			job.Stderr = append(job.Stderr, line)
		} else {
			job.Stdout = append(job.Stdout, line)
		}
		a.mu.Unlock()
	}
}

func (a *shellActorImpl) waitForCompletion(job *shellJob) {
	defer close(job.Done)

	err := job.Process.Wait()

	a.mu.Lock()
	defer a.mu.Unlock()

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
}

func (a *shellActorImpl) stopJobInternal(job *shellJob, signal string) error {
	if job.Completed {
		return fmt.Errorf("job %s is already completed", job.ID)
	}

	if job.Process == nil {
		return fmt.Errorf("job %s has no process to stop", job.ID)
	}

	if runtime.GOOS != "windows" && job.ProcessGroup > 0 {
		if err := signalProcessGroup(job.ProcessGroup, signal); err != nil {
			logger.Warn("shell actor %s: failed to signal process group %d: %v", a.id, job.ProcessGroup, err)
		}
	}

	if signal == "SIGKILL" {
		return job.Process.Process.Kill()
	}
	return job.Process.Process.Signal(syscall.SIGTERM)
}

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), time.Now().Unix())
}
