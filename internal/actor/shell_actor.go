package actor

import (
	"context"
	"time"
)

// ShellMessage represents different types of shell execution requests
type ShellMessage interface {
	Type() string
}

// ShellExecuteRequest is a message to execute a shell command
type ShellExecuteRequest struct {
	Command    string
	WorkingDir string
	Timeout    time.Duration
	Background bool
	Stdin      string
	ResponseCh chan ShellExecuteResponse
}

func (m ShellExecuteRequest) Type() string {
	return "ShellExecuteRequest"
}

// ShellStatusRequest is a message to check status of background jobs
type ShellStatusRequest struct {
	JobID      string
	ResponseCh chan ShellStatusResponse
}

func (m ShellStatusRequest) Type() string {
	return "ShellStatusRequest"
}

// ShellWaitRequest is a message to wait for background job completion
type ShellWaitRequest struct {
	JobID      string
	ResponseCh chan ShellWaitResponse
}

func (m ShellWaitRequest) Type() string {
	return "ShellWaitRequest"
}

// ShellStopRequest is a message to stop a background job
type ShellStopRequest struct {
	JobID      string
	Signal     string // "SIGTERM" or "SIGKILL"
	ResponseCh chan ShellStopResponse
}

func (m ShellStopRequest) Type() string {
	return "ShellStopRequest"
}

// ShellExecuteResponse contains the result of a shell execution
type ShellExecuteResponse struct {
	JobID    string
	PID      int
	ExitCode int
	Stdout   string
	Stderr   string
	Error    string
	Done     bool
	Message  string // For background jobs
}

// ShellStatusResponse contains status information about a background job
type ShellStatusResponse struct {
	JobID     string
	PID       int
	Running   bool
	ExitCode  int
	Stdout    string
	Stderr    string
	Completed bool
	Error     string
}

// ShellWaitResponse contains the result of waiting for a job to complete
type ShellWaitResponse struct {
	JobID    string
	ExitCode int
	Stdout   string
	Stderr   string
	Error    string
}

// ShellStopResponse contains the result of stopping a job
type ShellStopResponse struct {
	Success bool
	Error   string
}

// ShellActor is an interface for shell execution actors
type ShellActor interface {
	Actor

	// ExecuteCommand executes a shell command (synchronous)
	ExecuteCommand(ctx context.Context, command, workingDir string, timeout time.Duration, stdin string) (string, string, int, error)

	// ExecuteCommandBackground executes a shell command in background and returns job ID
	ExecuteCommandBackground(ctx context.Context, command, workingDir string) (string, int, error)

	// GetJobStatus returns the status of a background job
	GetJobStatus(ctx context.Context, jobID string) (running bool, exitCode int, stdout, stderr string, completed bool, err error)

	// WaitForJob waits for a background job to complete
	WaitForJob(ctx context.Context, jobID string) (exitCode int, stdout, stderr string, err error)

	// StopJob stops a background job
	StopJob(ctx context.Context, jobID string, signal string) error
}
