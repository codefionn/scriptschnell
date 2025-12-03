package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

type shellBackgroundKey struct{}

const shellBackgroundMessage = "Command started in background. Use 'status_program' to stream progress, 'wait_program' to block until completion, or 'stop_program' to terminate."

// ShellToolSpec is the static specification for the shell tool
type ShellToolSpec struct{}

func (s *ShellToolSpec) Name() string {
	return ToolNameShell
}

func (s *ShellToolSpec) Description() string {
	return "Execute shell commands. Working directory defaults to current directory. Supports background execution."
}

func (s *ShellToolSpec) Parameters() map[string]interface{} {
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

// ShellTool is the executor with runtime dependencies
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

// Legacy interface implementation for backward compatibility
func (t *ShellTool) Name() string        { return ToolNameShell }
func (t *ShellTool) Description() string { return (&ShellToolSpec{}).Description() }
func (t *ShellTool) Parameters() map[string]interface{} {
	return (&ShellToolSpec{}).Parameters()
}

func (t *ShellTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
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

	// Parse and execute command
	if background {
		return t.executeBackground(ctx, cmdStr, workingDir)
	}

	return t.executeForeground(ctx, cmdStr, workingDir, timeout)
}

func (t *ShellTool) executeForeground(ctx context.Context, cmdStr, workingDir string, timeoutSecs int) *ToolResult {
	bgChan := backgroundChanFromContext(ctx)
	runner := newShellCommandRunner(t, cmdStr, workingDir, timeoutSecs, bgChan)
	return runner.run(ctx)
}

func (t *ShellTool) executeBackground(ctx context.Context, cmdStr, workingDir string) *ToolResult {
	logger.Debug("shell: starting background job (explicit request)")

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = workingDir
	cmd.Env = os.Environ()
	configureProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("shell: failed to create stdout pipe: %v", err)
		return &ToolResult{Error: fmt.Sprintf("failed to create stdout pipe: %v", err)}
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.Error("shell: failed to create stderr pipe: %v", err)
		return &ToolResult{Error: fmt.Sprintf("failed to create stderr pipe: %v", err)}
	}

	if err := cmd.Start(); err != nil {
		logger.Error("shell: failed to start background command: %v", err)
		return &ToolResult{Error: fmt.Sprintf("failed to start command: %v", err)}
	}

	startedAt := time.Now()
	job, jobID := registerShellBackgroundJob(t.session, cmd, cmdStr, workingDir, startedAt)
	job.ProcessGroupID = getProcessGroupID(cmd)
	logger.Info("shell: background job started: %s (pid=%d)", jobID, job.PID)

	var wg sync.WaitGroup

	// Read output in goroutines
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			job.Mu.Lock()
			job.Stdout = append(job.Stdout, line)
			job.Mu.Unlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			job.Mu.Lock()
			job.Stderr = append(job.Stderr, line)
			job.Mu.Unlock()
		}
	}()

	// Wait for completion in background
	go func() {
		defer close(job.Done)
		err := cmd.Wait()
		wg.Wait()
		job.Mu.Lock()
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
		job.Mu.Unlock()
	}()

	return &ToolResult{
		Result: map[string]interface{}{
			"job_id":  jobID,
			"pid":     job.PID,
			"message": shellBackgroundMessage,
		},
	}
}

// StatusProgramToolSpec is the static specification for the status_program tool
type StatusProgramToolSpec struct{}

func (s *StatusProgramToolSpec) Name() string {
	return ToolNameStatusProgram
}

func (s *StatusProgramToolSpec) Description() string {
	return "Check status of background programs launched by the shell or sandbox tools."
}

func (s *StatusProgramToolSpec) Parameters() map[string]interface{} {
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

// StatusProgramTool is the executor with runtime dependencies
type StatusProgramTool struct {
	session *session.Session
}

func NewStatusProgramTool(sess *session.Session) *StatusProgramTool {
	return &StatusProgramTool{
		session: sess,
	}
}

// Legacy interface implementation for backward compatibility
func (t *StatusProgramTool) Name() string        { return ToolNameStatusProgram }
func (t *StatusProgramTool) Description() string { return (&StatusProgramToolSpec{}).Description() }
func (t *StatusProgramTool) Parameters() map[string]interface{} {
	return (&StatusProgramToolSpec{}).Parameters()
}

func (t *StatusProgramTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
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
				"process_group":  job.ProcessGroupID,
				"type":           job.Type,
				"stop_requested": job.StopRequested,
				"last_signal":    job.LastSignal,
				"open_ports":     collectOpenPorts(ctx, job.PID),
			}
		}
		return &ToolResult{Result: map[string]interface{}{
			"jobs": jobList,
		}}
	}

	// Get specific job
	job, ok := t.session.GetBackgroundJob(jobID)
	if !ok {
		return &ToolResult{Error: fmt.Sprintf("job not found: %s", jobID)}
	}

	return &ToolResult{Result: buildJobSnapshot(ctx, job, lastNLines)}
}

// WaitProgramToolSpec is the static specification for the wait_program tool
type WaitProgramToolSpec struct{}

func (s *WaitProgramToolSpec) Name() string {
	return ToolNameWaitProgram
}

func (s *WaitProgramToolSpec) Description() string {
	return "Block until a background program completes and return its final output."
}

func (s *WaitProgramToolSpec) Parameters() map[string]interface{} {
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

// WaitProgramTool is the executor with runtime dependencies
type WaitProgramTool struct {
	session *session.Session
}

func NewWaitProgramTool(sess *session.Session) *WaitProgramTool {
	return &WaitProgramTool{session: sess}
}

// Legacy interface implementation for backward compatibility
func (t *WaitProgramTool) Name() string        { return ToolNameWaitProgram }
func (t *WaitProgramTool) Description() string { return (&WaitProgramToolSpec{}).Description() }
func (t *WaitProgramTool) Parameters() map[string]interface{} {
	return (&WaitProgramToolSpec{}).Parameters()
}

func (t *WaitProgramTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	jobID := GetStringParam(params, "job_id", "")
	if jobID == "" {
		return &ToolResult{Error: "job_id is required"}
	}

	lastNLines := GetIntParam(params, "last_n_lines", 0)
	if lastNLines < 0 {
		lastNLines = 0
	}

	job, ok := t.session.GetBackgroundJob(jobID)
	if !ok {
		return &ToolResult{Error: fmt.Sprintf("job not found: %s", jobID)}
	}

	jobCompleted := func() bool {
		job.Mu.RLock()
		defer job.Mu.RUnlock()
		return job.Completed
	}

	done := job.Done
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return &ToolResult{Error: ctx.Err().Error()}
		}
	} else if !jobCompleted() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if jobCompleted() {
				break
			}
			select {
			case <-ctx.Done():
				return &ToolResult{Error: ctx.Err().Error()}
			case <-ticker.C:
			}
		}
	}

	result := buildJobSnapshot(ctx, job, lastNLines)
	result["waited"] = true
	return &ToolResult{Result: result}
}

func buildJobSnapshot(ctx context.Context, job *session.BackgroundJob, lastNLines int) map[string]interface{} {
	job.Mu.RLock()
	stdoutLines := append([]string(nil), job.Stdout...)
	stderrLines := append([]string(nil), job.Stderr...)
	completed := job.Completed
	exitCode := job.ExitCode
	startTime := job.StartTime
	pid := job.PID
	processGroup := job.ProcessGroupID
	jobType := job.Type
	stopRequested := job.StopRequested
	lastSignal := job.LastSignal
	command := job.Command
	job.Mu.RUnlock()

	computeStart := func(length int) int {
		if lastNLines <= 0 || length <= lastNLines {
			return 0
		}
		return length - lastNLines
	}

	combined := make([]string, 0, len(stdoutLines)+len(stderrLines))
	combined = append(combined, stdoutLines...)
	combined = append(combined, stderrLines...)

	start := computeStart(len(combined))
	stdoutStart := computeStart(len(stdoutLines))
	stderrStart := computeStart(len(stderrLines))

	output := strings.Join(combined[start:], "\n")
	stdout := strings.Join(stdoutLines[stdoutStart:], "\n")
	stderr := strings.Join(stderrLines[stderrStart:], "\n")

	snapshot := map[string]interface{}{
		"job_id":         job.ID,
		"command":        command,
		"completed":      completed,
		"exit_code":      exitCode,
		"runtime":        time.Since(startTime).String(),
		"pid":            pid,
		"process_group":  processGroup,
		"type":           jobType,
		"stop_requested": stopRequested,
		"last_signal":    lastSignal,
		"output":         output,
		"stdout":         stdout,
		"stderr":         stderr,
	}

	snapshot["open_ports"] = collectOpenPorts(ctx, job.PID)

	return snapshot
}

func collectOpenPorts(ctx context.Context, pid int) []string {
	if pid <= 0 || runtime.GOOS == "windows" {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "lsof", "-Pan", "-p", strconv.Itoa(pid), "-i")
	cmd.Env = os.Environ()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			logger.Debug("status_program: timed out collecting open ports for pid=%d", pid)
		} else if !errors.Is(err, context.Canceled) && !errors.Is(err, exec.ErrNotFound) {
			logger.Debug("status_program: failed to collect open ports for pid=%d: %v", pid, err)
		}
		return nil
	}

	ports := parsePortsFromLsof(stdout.Bytes())
	if len(ports) == 0 {
		return nil
	}
	return ports
}

func parsePortsFromLsof(data []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	nameColumn := -1
	seen := make(map[string]struct{})

	for scanner.Scan() {
		line := scanner.Text()
		if nameColumn == -1 {
			nameColumn = strings.Index(line, "NAME")
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if nameColumn >= len(line) {
			continue
		}
		nameField := strings.TrimSpace(line[nameColumn:])
		if nameField == "" {
			continue
		}
		extractPortsFromName(nameField, seen)
	}

	if len(seen) == 0 {
		return nil
	}

	ports := make([]string, 0, len(seen))
	for value := range seen {
		ports = append(ports, value)
	}
	sort.Strings(ports)
	return ports
}

func extractPortsFromName(name string, seen map[string]struct{}) {
	for _, token := range strings.Fields(name) {
		if strings.HasPrefix(token, "TCP") || strings.HasPrefix(token, "UDP") {
			continue
		}
		if strings.Contains(token, "->") {
			parts := strings.Split(token, "->")
			if len(parts) > 0 {
				addPortToken(parts[0], seen)
			}
			continue
		}
		addPortToken(token, seen)
	}
}

func addPortToken(token string, seen map[string]struct{}) {
	colonIdx := strings.LastIndex(token, ":")
	if colonIdx == -1 {
		return
	}

	host := strings.TrimSpace(strings.Trim(token[:colonIdx], "[]"))
	port := strings.TrimSpace(token[colonIdx+1:])

	if idx := strings.Index(port, "("); idx != -1 {
		port = strings.TrimSpace(port[:idx])
	}

	if port == "" {
		return
	}

	if _, err := strconv.Atoi(port); err != nil {
		return
	}

	if host == "" {
		host = "*"
	}

	key := fmt.Sprintf("%s:%s", host, port)
	seen[key] = struct{}{}
}

func ContextWithShellBackground(ctx context.Context, ch chan struct{}) context.Context {
	if ch == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, shellBackgroundKey{}, ch)
}

func backgroundChanFromContext(ctx context.Context) chan struct{} {
	if ctx == nil {
		return nil
	}
	if val := ctx.Value(shellBackgroundKey{}); val != nil {
		if ch, ok := val.(chan struct{}); ok {
			return ch
		}
	}
	return nil
}

func registerShellBackgroundJob(sess *session.Session, cmd *exec.Cmd, cmdStr, workingDir string, startedAt time.Time) (*session.BackgroundJob, string) {
	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())

	job := &session.BackgroundJob{
		ID:         jobID,
		Command:    cmdStr,
		WorkingDir: workingDir,
		StartTime:  startedAt,
		Completed:  false,
		Stdout:     make([]string, 0),
		Stderr:     make([]string, 0),
		Type:       ToolNameShell,
		Done:       make(chan struct{}),
	}

	if cmd != nil && cmd.Process != nil {
		job.Process = cmd.Process
		job.PID = cmd.Process.Pid
	}

	sess.AddBackgroundJob(job)
	return job, jobID
}

func sendSignalToBackgroundJob(job *session.BackgroundJob, sig syscall.Signal, signalName string) error {
	job.Mu.RLock()
	processGroupID := job.ProcessGroupID
	proc := job.Process
	pid := job.PID
	cancel := job.CancelFunc
	jobID := job.ID
	job.Mu.RUnlock()

	var groupErr error

	if processGroupID > 0 {
		if err := signalProcessGroup(processGroupID, sig); err == nil || isIgnorableSignalError(err) {
			if err == nil {
				return nil
			}
		} else {
			groupErr = fmt.Errorf("failed to signal process group %d: %w", processGroupID, err)
		}
	}

	if proc != nil {
		var err error
		if signalName == "SIGKILL" {
			err = proc.Kill()
		} else {
			err = proc.Signal(sig)
		}
		if err == nil || isIgnorableSignalError(err) {
			return nil
		}
		return fmt.Errorf("failed to signal process %d: %w", pid, err)
	}

	if cancel != nil {
		cancel()
		return nil
	}

	if groupErr != nil {
		return groupErr
	}

	return fmt.Errorf("no active process to signal for job %s", jobID)
}

func isIgnorableSignalError(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

// StopProgramToolSpec is the static specification for the stop_program tool
type StopProgramToolSpec struct{}

func (s *StopProgramToolSpec) Name() string {
	return ToolNameStopProgram
}

func (s *StopProgramToolSpec) Description() string {
	return "Stop a background program by sending SIGTERM or SIGKILL."
}

func (s *StopProgramToolSpec) Parameters() map[string]interface{} {
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

// StopProgramTool is the executor with runtime dependencies
type StopProgramTool struct {
	session *session.Session
}

func NewStopProgramTool(sess *session.Session) *StopProgramTool {
	return &StopProgramTool{session: sess}
}

// Legacy interface implementation for backward compatibility
func (t *StopProgramTool) Name() string        { return ToolNameStopProgram }
func (t *StopProgramTool) Description() string { return (&StopProgramToolSpec{}).Description() }
func (t *StopProgramTool) Parameters() map[string]interface{} {
	return (&StopProgramToolSpec{}).Parameters()
}

func (t *StopProgramTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	jobID := GetStringParam(params, "job_id", "")
	if jobID == "" {
		return &ToolResult{Error: "job_id is required"}
	}

	job, ok := t.session.GetBackgroundJob(jobID)
	if !ok {
		return &ToolResult{Error: fmt.Sprintf("job not found: %s", jobID)}
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
		return &ToolResult{Error: fmt.Sprintf("unsupported signal: %s", signalInput)}
	}

	job.Mu.RLock()
	alreadyCompleted := job.Completed
	exitCode := job.ExitCode
	job.Mu.RUnlock()

	if alreadyCompleted {
		return &ToolResult{Result: map[string]interface{}{
			"job_id":    job.ID,
			"message":   "Job already completed.",
			"completed": true,
			"exit_code": exitCode,
		}}
	}

	if err := sendSignalToBackgroundJob(job, sig, signalName); err != nil {
		logger.Error("stop_program: failed to send %s to job %s (pid=%d, pgid=%d): %v", signalName, job.ID, job.PID, job.ProcessGroupID, err)
		return &ToolResult{Error: err.Error()}
	}

	job.Mu.Lock()
	job.StopRequested = true
	job.LastSignal = signalName
	job.Mu.Unlock()

	logger.Info("stop_program: sent %s to job %s (pid=%d)", signalName, job.ID, job.PID)

	return &ToolResult{Result: map[string]interface{}{
		"job_id":  job.ID,
		"signal":  signalName,
		"message": fmt.Sprintf("Signal %s sent to background job.", signalName),
	}}
}

// NewStatusProgramToolFactory creates a factory for StatusProgramTool
func NewStatusProgramToolFactory(sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewStatusProgramTool(sess)
	}
}

// NewWaitProgramToolFactory creates a factory for WaitProgramTool
func NewWaitProgramToolFactory(sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewWaitProgramTool(sess)
	}
}

// NewStopProgramToolFactory creates a factory for StopProgramTool
func NewStopProgramToolFactory(sess *session.Session) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewStopProgramTool(sess)
	}
}

// NewShellToolFactory creates a factory for ShellTool
func NewShellToolFactory(sess *session.Session, workingDir string) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewShellTool(sess, workingDir)
	}
}
