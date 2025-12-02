package acp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/statcode-ai/scriptschnell/internal/actor"
	"github.com/statcode-ai/scriptschnell/internal/logger"
)

// Terminal represents a running terminal session
type Terminal struct {
	ID              string
	SessionID       acp.SessionId
	JobID           string // Background job ID from shell actor
	Command         string
	Args            []string
	Cwd             string
	OutputByteLimit int
	CreatedAt       time.Time
	mu              sync.RWMutex
	released        bool
}

// TerminalManager manages terminal sessions using the shell actor
type TerminalManager struct {
	shellActor actor.ShellActor
	terminals  map[string]*Terminal
	mu         sync.RWMutex
}

// NewTerminalManager creates a new terminal manager
func NewTerminalManager(shellActor actor.ShellActor) *TerminalManager {
	return &TerminalManager{
		shellActor: shellActor,
		terminals:  make(map[string]*Terminal),
	}
}

// Create creates a new terminal and starts the command
func (tm *TerminalManager) Create(ctx context.Context, req acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	// Build command string from command + args
	cmdStr := req.Command
	for _, arg := range req.Args {
		cmdStr += " " + shellEscape(arg)
	}

	// Set working directory (default to current if not specified)
	cwd := "."
	if req.Cwd != nil && *req.Cwd != "" {
		cwd = *req.Cwd
	}

	// Start command in background using shell actor
	jobID, pid, err := tm.shellActor.ExecuteCommandBackground(ctx, cmdStr, cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to start terminal command: %w", err)
	}

	// Generate terminal ID
	terminalID := fmt.Sprintf("term_%d", time.Now().UnixNano())

	// Get output byte limit (default to 0 for unlimited)
	outputByteLimit := 0
	if req.OutputByteLimit != nil {
		outputByteLimit = *req.OutputByteLimit
	}

	// Create terminal record
	terminal := &Terminal{
		ID:              terminalID,
		SessionID:       req.SessionId,
		JobID:           jobID,
		Command:         req.Command,
		Args:            req.Args,
		Cwd:             cwd,
		OutputByteLimit: outputByteLimit,
		CreatedAt:       time.Now(),
	}

	tm.mu.Lock()
	tm.terminals[terminalID] = terminal
	tm.mu.Unlock()

	logger.Debug("terminal: created terminal %s for job %s (pid=%d)", terminalID, jobID, pid)

	return &acp.CreateTerminalResponse{
		TerminalId: terminalID,
	}, nil
}

// Output retrieves the current output of a terminal
func (tm *TerminalManager) Output(ctx context.Context, req acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	terminal, err := tm.getTerminal(req.TerminalId)
	if err != nil {
		return nil, err
	}

	// Get job status from shell actor
	running, exitCode, stdout, stderr, completed, err := tm.shellActor.GetJobStatus(ctx, terminal.JobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get terminal output: %w", err)
	}

	// Combine stdout and stderr
	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}

	// Apply byte limit if specified
	truncated := false
	if terminal.OutputByteLimit > 0 && len(output) > terminal.OutputByteLimit {
		// Truncate from beginning to stay within limit
		// Ensure we truncate at character boundary
		output = truncateAtCharBoundary(output, terminal.OutputByteLimit)
		truncated = true
	}

	// Build response
	response := &acp.TerminalOutputResponse{
		Output:    output,
		Truncated: truncated,
	}

	// Add exit status if command has completed
	if completed || !running {
		response.ExitStatus = &acp.TerminalExitStatus{
			ExitCode: &exitCode,
			Signal:   nil, // TODO: Detect signal termination
		}
	}

	return response, nil
}

// WaitForExit waits for a terminal command to complete
func (tm *TerminalManager) WaitForExit(ctx context.Context, req acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
	terminal, err := tm.getTerminal(req.TerminalId)
	if err != nil {
		return nil, err
	}

	// Wait for job completion using shell actor
	exitCode, _, _, err := tm.shellActor.WaitForJob(ctx, terminal.JobID)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for terminal: %w", err)
	}

	return &acp.WaitForTerminalExitResponse{
		ExitCode: &exitCode,
		Signal:   nil, // TODO: Detect signal termination
	}, nil
}

// Kill terminates a terminal command
func (tm *TerminalManager) Kill(ctx context.Context, req acp.KillTerminalCommandRequest) (*acp.KillTerminalCommandResponse, error) {
	terminal, err := tm.getTerminal(req.TerminalId)
	if err != nil {
		return nil, err
	}

	// Send SIGTERM to job using shell actor
	if err := tm.shellActor.StopJob(ctx, terminal.JobID, "SIGTERM"); err != nil {
		// Try SIGKILL if SIGTERM fails
		logger.Debug("terminal: SIGTERM failed for %s, trying SIGKILL", req.TerminalId)
		if killErr := tm.shellActor.StopJob(ctx, terminal.JobID, "SIGKILL"); killErr != nil {
			return nil, fmt.Errorf("failed to kill terminal: %w", killErr)
		}
	}

	logger.Debug("terminal: killed terminal %s (job %s)", req.TerminalId, terminal.JobID)
	return &acp.KillTerminalCommandResponse{}, nil
}

// Release kills the command if still running and releases terminal resources
func (tm *TerminalManager) Release(ctx context.Context, req acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
	terminal, err := tm.getTerminal(req.TerminalId)
	if err != nil {
		return nil, err
	}

	terminal.mu.Lock()
	if terminal.released {
		terminal.mu.Unlock()
		return nil, fmt.Errorf("terminal already released: %s", req.TerminalId)
	}
	terminal.released = true
	terminal.mu.Unlock()

	// Try to kill the job if still running (ignore errors)
	_ = tm.shellActor.StopJob(ctx, terminal.JobID, "SIGKILL")

	// Remove terminal from map
	tm.mu.Lock()
	delete(tm.terminals, req.TerminalId)
	tm.mu.Unlock()

	logger.Debug("terminal: released terminal %s", req.TerminalId)
	return &acp.ReleaseTerminalResponse{}, nil
}

// getTerminal retrieves a terminal by ID
func (tm *TerminalManager) getTerminal(terminalID string) (*Terminal, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	terminal, exists := tm.terminals[terminalID]
	if !exists {
		return nil, fmt.Errorf("terminal not found: %s", terminalID)
	}

	terminal.mu.RLock()
	defer terminal.mu.RUnlock()

	if terminal.released {
		return nil, fmt.Errorf("terminal has been released: %s", terminalID)
	}

	return terminal, nil
}

// shellEscape escapes a shell argument (basic implementation)
func shellEscape(arg string) string {
	// Simple escaping: wrap in single quotes and escape existing single quotes
	escaped := ""
	for _, c := range arg {
		if c == '\'' {
			escaped += "'\\''"
		} else {
			escaped += string(c)
		}
	}
	return "'" + escaped + "'"
}

// truncateAtCharBoundary truncates a string to the specified byte limit
// while ensuring we don't break in the middle of a UTF-8 character
func truncateAtCharBoundary(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	// Start from the end and work backwards to stay within limit
	result := s[len(s)-limit:]

	// Find the first valid UTF-8 character boundary
	for i := 0; i < len(result); i++ {
		if (result[i] & 0xC0) != 0x80 {
			// Found a character boundary
			return result[i:]
		}
	}

	// If we couldn't find a boundary, return empty (shouldn't happen in practice)
	return ""
}
