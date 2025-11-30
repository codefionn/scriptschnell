package actor

import (
	"context"
	"fmt"
	"time"
)

// ShellActorClient provides a client interface to the ShellActor
type ShellActorClient struct {
	ref *ActorRef
}

// NewShellActorClient creates a new client for a ShellActor
func NewShellActorClient(ref *ActorRef) *ShellActorClient {
	return &ShellActorClient{ref: ref}
}

// ExecuteCommand executes a shell command synchronously
func (c *ShellActorClient) ExecuteCommand(ctx context.Context, command, workingDir string, timeout time.Duration, stdin string) (string, string, int, error) {
	responseCh := make(chan ShellExecuteResponse, 1)

	msg := ShellExecuteRequest{
		Command:    command,
		WorkingDir: workingDir,
		Timeout:    timeout,
		Background: false,
		Stdin:      stdin,
		ResponseCh: responseCh,
	}

	if err := c.ref.Send(msg); err != nil {
		return "", "", 0, err
	}

	select {
	case response := <-responseCh:
		if response.Error != "" {
			return response.Stdout, response.Stderr, response.ExitCode, fmt.Errorf("%s", response.Error)
		}
		return response.Stdout, response.Stderr, response.ExitCode, nil
	case <-ctx.Done():
		return "", "", 0, ctx.Err()
	}
}

// ExecuteCommandBackground executes a shell command in background and returns job ID
func (c *ShellActorClient) ExecuteCommandBackground(ctx context.Context, command, workingDir string) (string, int, error) {
	responseCh := make(chan ShellExecuteResponse, 1)

	msg := ShellExecuteRequest{
		Command:    command,
		WorkingDir: workingDir,
		Background: true,
		ResponseCh: responseCh,
	}

	if err := c.ref.Send(msg); err != nil {
		return "", 0, err
	}

	select {
	case response := <-responseCh:
		if response.Error != "" {
			return "", 0, fmt.Errorf("%s", response.Error)
		}
		return response.JobID, response.PID, nil
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}
}

// GetJobStatus returns the status of a background job
func (c *ShellActorClient) GetJobStatus(ctx context.Context, jobID string) (bool, int, string, string, bool, error) {
	responseCh := make(chan ShellStatusResponse, 1)

	msg := ShellStatusRequest{
		JobID:      jobID,
		ResponseCh: responseCh,
	}

	if err := c.ref.Send(msg); err != nil {
		return false, 0, "", "", false, err
	}

	select {
	case response := <-responseCh:
		if response.Error != "" {
			return false, 0, "", "", false, fmt.Errorf("%s", response.Error)
		}
		return response.Running, response.ExitCode, response.Stdout, response.Stderr, response.Completed, nil
	case <-ctx.Done():
		return false, 0, "", "", false, ctx.Err()
	}
}

// WaitForJob waits for a background job to complete
func (c *ShellActorClient) WaitForJob(ctx context.Context, jobID string) (int, string, string, error) {
	responseCh := make(chan ShellWaitResponse, 1)

	msg := ShellWaitRequest{
		JobID:      jobID,
		ResponseCh: responseCh,
	}

	if err := c.ref.Send(msg); err != nil {
		return 0, "", "", err
	}

	select {
	case response := <-responseCh:
		if response.Error != "" {
			return 0, "", "", fmt.Errorf("%s", response.Error)
		}
		return response.ExitCode, response.Stdout, response.Stderr, nil
	case <-ctx.Done():
		return 0, "", "", ctx.Err()
	}
}

// StopJob stops a background job
func (c *ShellActorClient) StopJob(ctx context.Context, jobID string, signal string) error {
	responseCh := make(chan ShellStopResponse, 1)

	msg := ShellStopRequest{
		JobID:      jobID,
		Signal:     signal,
		ResponseCh: responseCh,
	}

	if err := c.ref.Send(msg); err != nil {
		return err
	}

	select {
	case response := <-responseCh:
		if response.Error != "" {
			return fmt.Errorf("%s", response.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
