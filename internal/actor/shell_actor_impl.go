package actor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

// shellActorImpl implements the ShellActor interface
type shellActorImpl struct {
	id      string
	session *session.Session
	jobs    map[string]*shellJob
	mu      sync.RWMutex
	health  *HealthCheckable
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
	actor := &shellActorImpl{
		id:      id,
		session: sess,
		jobs:    make(map[string]*shellJob),
	}

	// Initialize health monitoring
	actor.health = NewHealthCheckable(id, make(chan Message, 100), actor.getShellMetrics)

	return actor
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
			_ = a.stopJobInternal(job, "SIGTERM")
		}
	}

	return nil
}

func (a *shellActorImpl) Receive(ctx context.Context, msg Message) error {
	// Record activity for health monitoring
	if a.health != nil {
		a.health.RecordActivity()
	}

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
		// Try health check handler first
		if a.health != nil {
			if err := a.health.HealthCheckHandler(ctx, msg); err == nil {
				return nil // Health check message handled
			}
		}
		return fmt.Errorf("unknown message type: %T", msg)
	}
	return nil
}

func (a *shellActorImpl) ExecuteCommand(ctx context.Context, args []string, workingDir string, timeout time.Duration, stdin string) (string, string, int, error) {
	if len(args) == 0 {
		return "", "", -1, fmt.Errorf("no command provided")
	}

	resolvedArgs, err := a.resolveCommand(args, workingDir)
	if err != nil {
		return "", "", -1, err
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, resolvedArgs[0], resolvedArgs[1:]...)
	cmd.Dir = workingDir
	cmd.Env = os.Environ()

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return stdout.String(), stderr.String(), exitCode, err
}

func (a *shellActorImpl) ExecuteCommandBackground(ctx context.Context, args []string, workingDir string) (string, int, error) {
	if len(args) == 0 {
		return "", 0, fmt.Errorf("no command provided")
	}

	resolvedArgs, err := a.resolveCommand(args, workingDir)
	if err != nil {
		return "", 0, err
	}

	cmd := exec.Command(resolvedArgs[0], resolvedArgs[1:]...)
	cmd.Dir = workingDir
	cmd.Env = os.Environ()
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
	commandDisplay := strings.Join(resolvedArgs, " ")
	jobID, job := a.registerBackgroundJob(cmd, commandDisplay, workingDir, startedAt)

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
		var jobID string
		var pid int
		var err error

		jobID, pid, err = a.ExecuteCommandBackground(ctx, req.Command, req.WorkingDir)
		if err != nil {
			return ShellExecuteResponse{Error: err.Error()}
		}
		return ShellExecuteResponse{
			JobID:   jobID,
			PID:     pid,
			Message: "Command started in background. Use status_program to stream progress, wait_program to block until completion, or stop_program to terminate.",
		}
	}

	var (
		stdout   string
		stderr   string
		exitCode int
		err      error
	)

	stdout, stderr, exitCode, err = a.ExecuteCommand(ctx, req.Command, req.WorkingDir, req.Timeout, req.Stdin)
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

func (a *shellActorImpl) resolveCommand(args []string, workingDir string) ([]string, error) {
	resolvedArgs := append([]string(nil), args...)
	resolvedPath, err := resolveProgramPath(args[0], workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve program %q: %w", args[0], err)
	}
	resolvedArgs[0] = resolvedPath
	return resolvedArgs, nil
}

func resolveProgramPath(program string, workingDir string) (string, error) {
	if program == "" {
		return "", fmt.Errorf("no program provided")
	}

	candidates := make([]string, 0, 3)
	if filepath.IsAbs(program) {
		candidates = append(candidates, program)
	} else {
		if workingDir != "" {
			candidates = append(candidates, filepath.Join(workingDir, program))
		}
		candidates = append(candidates, program)
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			if filepath.IsAbs(candidate) {
				return candidate, nil
			}
			if abs, absErr := filepath.Abs(candidate); absErr == nil {
				return abs, nil
			}
			return candidate, nil
		}
	}

	resolved, err := exec.LookPath(program)
	if err != nil {
		return "", err
	}

	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved, nil
	}
	return abs, nil
}

// Health Check methods

// GetHealthMetrics returns current health metrics for shell actor
func (a *shellActorImpl) GetHealthMetrics() HealthMetrics {
	return a.health.GetHealthMetrics()
}

// IsHealthy returns true if the shell actor is healthy
func (a *shellActorImpl) IsHealthy() bool {
	return a.health.IsHealthy()
}

// GetShellMetrics provides custom metrics for shell actor
func (a *shellActorImpl) getShellMetrics() interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Count running vs completed jobs
	runningJobs := 0
	completedJobs := 0
	for _, job := range a.jobs {
		if job.Completed {
			completedJobs++
		} else {
			runningJobs++
		}
	}

	metrics := map[string]interface{}{
		"total_jobs":     len(a.jobs),
		"running_jobs":   runningJobs,
		"completed_jobs": completedJobs,
		"job_management": "active",
	}

	return metrics
}

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), time.Now().Unix())
}
