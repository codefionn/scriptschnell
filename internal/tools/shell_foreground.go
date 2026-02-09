package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

type shellCommandRunner struct {
	tool        *ShellTool
	command     string
	workingDir  string
	timeout     time.Duration
	backgroundC <-chan struct{}
	output      *shellOutput
	wg          sync.WaitGroup
	done        chan error
	startedAt   time.Time
}

func newShellCommandRunner(tool *ShellTool, command, workingDir string, timeoutSecs int, background chan struct{}) *shellCommandRunner {
	timeout := time.Duration(timeoutSecs) * time.Second
	if timeoutSecs <= 0 {
		timeout = 0
	}

	var backgroundC <-chan struct{}
	if background != nil {
		backgroundC = background
	}

	return &shellCommandRunner{
		tool:        tool,
		command:     command,
		workingDir:  workingDir,
		timeout:     timeout,
		backgroundC: backgroundC,
		output:      newShellOutput(),
	}
}

func (r *shellCommandRunner) run(ctx context.Context) *ToolResult {
	cmd := exec.Command("sh", "-c", r.command)
	cmd.Dir = r.workingDir
	cmd.Env = os.Environ()

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

	r.startedAt = time.Now()
	if err := cmd.Start(); err != nil {
		logger.Error("shell: failed to start command: %v", err)
		return &ToolResult{Error: fmt.Sprintf("failed to start command: %v", err)}
	}

	r.startReaders(stdout, stderr)

	r.done = make(chan error, 1)
	go func() {
		r.done <- cmd.Wait()
	}()

	var (
		timer    *time.Timer
		timerC   <-chan time.Time
		timedOut bool
	)

	if r.timeout > 0 {
		timer = time.NewTimer(r.timeout)
		timerC = timer.C
	}

	for {
		select {
		case err := <-r.done:
			if timer != nil {
				timer.Stop()
			}
			r.wg.Wait()
			if job := r.output.backgroundJob(); job != nil {
				return &ToolResult{
					Result: map[string]interface{}{
						"job_id":  job.ID,
						"pid":     job.PID,
						"message": shellBackgroundMessage,
					},
				}
			}
			return r.buildForegroundResult(err, timedOut)

		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			if cmd.Process != nil {
				logger.Warn("shell: killing process (pid=%d) due to context cancellation: %s", cmd.Process.Pid, ctx.Err())
				_ = cmd.Process.Kill()
			}
			<-r.done
			r.wg.Wait()
			return &ToolResult{Error: ctx.Err().Error()}

		case <-timerC:
			timedOut = true
			if cmd.Process != nil {
				logger.Warn("shell: killing process (pid=%d) due to timeout after %s", cmd.Process.Pid, r.timeout)
				_ = cmd.Process.Kill()
			}
			timerC = nil

		case <-r.backgroundC:
			if timer != nil {
				timer.Stop()
			}
			job, jobID := r.output.convertToBackground(r.tool.session, cmd, r.command, r.workingDir, r.startedAt)
			if job == nil {
				continue
			}
			go r.handleBackgroundCompletion(job)
			logger.Info("shell: converted foreground command to background job: %s (pid=%d)", jobID, job.PID)
			return &ToolResult{
				Result: map[string]interface{}{
					"job_id":  jobID,
					"pid":     job.PID,
					"message": shellBackgroundMessage,
				},
			}
		}
	}
}

func (r *shellCommandRunner) startReaders(stdout io.Reader, stderr io.Reader) {
	r.startStreamReader(stdout, r.output.handleStdoutChunk)
	r.startStreamReader(stderr, r.output.handleStderrChunk)
}

func (r *shellCommandRunner) startStreamReader(reader io.Reader, handler func([]byte)) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				handler(chunk)
			}
			if err != nil {
				if err != io.EOF && !errors.Is(err, io.ErrClosedPipe) {
					logger.Debug("shell: stream read error: %v", err)
				}
				break
			}
		}
	}()
}

func (r *shellCommandRunner) buildForegroundResult(err error, timedOut bool) *ToolResult {
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			logger.Warn("shell: command exited with code %d", exitCode)
		} else if timedOut {
			logger.Warn("shell: command timed out after %s", r.timeout)
		} else {
			logger.Error("shell: failed to execute command: %v", err)
			return &ToolResult{Error: fmt.Sprintf("failed to execute command: %v", err)}
		}
	} else {
		logger.Info("shell: command completed successfully (exit_code=%d, output_bytes=%d)", exitCode, r.output.bytesWritten())
	}

	// Build the basic result
	result := map[string]interface{}{
		"stdout":    r.output.combinedOutput(),
		"exit_code": exitCode,
		"timeout":   timedOut,
	}

	// Add enhanced metadata for better summaries
	output := r.output.combinedOutput()
	outputBytes, outputLines := CalculateOutputStats(output)

	metadata := &ExecutionMetadata{
		StartTime:       &r.startedAt,
		EndTime:         timePtr(time.Now()),
		DurationMs:      time.Since(r.startedAt).Milliseconds(),
		Command:         r.command,
		ExitCode:        exitCode,
		OutputSizeBytes: outputBytes,
		OutputLineCount: outputLines,
		WorkingDir:      r.workingDir,
		TimeoutSeconds:  int(r.timeout.Seconds()),
		WasTimedOut:     timedOut,
		WasBackgrounded: false,
		ToolType:        ToolNameShell,
		HasStderr:       r.output.hasStderrContent(),
	}

	// Add stderr-specific stats if available
	if stderrOutput := r.output.stderrOutput(); stderrOutput != "" {
		stderrBytes, stderrLines := CalculateOutputStats(stderrOutput)
		metadata.StderrSizeBytes = stderrBytes
		metadata.StderrLineCount = stderrLines
	}

	// Store metadata in the result for the TUI to use
	result["_execution_metadata"] = metadata

	return &ToolResult{
		Result:            result,
		ExecutionMetadata: metadata,
	}
}

func timePtr(t time.Time) *time.Time {
	return &t

}
func (r *shellCommandRunner) handleBackgroundCompletion(job *session.BackgroundJob) {
	err := <-r.done
	r.wg.Wait()
	exitCode := 0
	var nonExitErr error
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			nonExitErr = err
		}
	}
	r.output.finalizeBackgroundJob(job, exitCode, nonExitErr)
}

type shellOutput struct {
	mu            sync.Mutex
	stdoutLines   []string
	stderrLines   []string
	stdoutPending string
	stderrPending string
	combined      strings.Builder
	job           *session.BackgroundJob
}

func newShellOutput() *shellOutput {
	return &shellOutput{
		stdoutLines: make([]string, 0),
		stderrLines: make([]string, 0),
	}
}

func (o *shellOutput) handleStdoutChunk(chunk []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.processChunk(chunk, true)
}

func (o *shellOutput) handleStderrChunk(chunk []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.processChunk(chunk, false)
}

func (o *shellOutput) processChunk(chunk []byte, isStdout bool) {
	text := string(chunk)
	o.combined.WriteString(text)

	if isStdout {
		o.stdoutPending += text
		lines, remainder := splitLines(o.stdoutPending)
		o.stdoutPending = remainder
		if len(lines) > 0 {
			o.stdoutLines = append(o.stdoutLines, lines...)
			if o.job != nil {
				o.job.Mu.Lock()
				o.job.Stdout = append(o.job.Stdout, lines...)
				o.job.Mu.Unlock()
			}
		}
	} else {
		o.stderrPending += text
		lines, remainder := splitLines(o.stderrPending)
		o.stderrPending = remainder
		if len(lines) > 0 {
			o.stderrLines = append(o.stderrLines, lines...)
			if o.job != nil {
				o.job.Mu.Lock()
				o.job.Stderr = append(o.job.Stderr, lines...)
				o.job.Mu.Unlock()
			}
		}
	}
}

func (o *shellOutput) convertToBackground(sess *session.Session, cmd *exec.Cmd, command, workingDir string, startedAt time.Time) (*session.BackgroundJob, string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.job != nil {
		return o.job, o.job.ID
	}

	job, jobID := registerShellBackgroundJob(sess, cmd, command, workingDir, startedAt)
	job.Mu.Lock()
	job.Stdout = append(job.Stdout, o.stdoutLines...)
	job.Stderr = append(job.Stderr, o.stderrLines...)
	job.Mu.Unlock()
	o.job = job
	return job, jobID
}

func (o *shellOutput) finalizeBackgroundJob(job *session.BackgroundJob, exitCode int, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.stdoutPending != "" {
		job.Mu.Lock()
		job.Stdout = append(job.Stdout, trimTrailingCarriage(o.stdoutPending))
		job.Mu.Unlock()
		o.stdoutPending = ""
	}
	if o.stderrPending != "" {
		job.Mu.Lock()
		job.Stderr = append(job.Stderr, trimTrailingCarriage(o.stderrPending))
		job.Mu.Unlock()
		o.stderrPending = ""
	}

	if err != nil && exitCode == -1 {
		job.Mu.Lock()
		job.Stderr = append(job.Stderr, fmt.Sprintf("command error: %v", err))
		job.Mu.Unlock()
	}

	job.Mu.Lock()
	job.ExitCode = exitCode
	job.Completed = true
	job.Process = nil
	job.Mu.Unlock()
	close(job.Done)
}

func (o *shellOutput) combinedOutput() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.combined.String()
}

func (o *shellOutput) bytesWritten() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.combined.Len()
}

func (o *shellOutput) backgroundJob() *session.BackgroundJob {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.job
}

func (o *shellOutput) hasStderrContent() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.stderrLines) > 0 || o.stderrPending != ""
}

func (o *shellOutput) stderrOutput() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.stderrLines) == 0 {
		return o.stderrPending
	}
	return strings.Join(o.stderrLines, "\n") + o.stderrPending

}
func splitLines(input string) ([]string, string) {
	if input == "" {
		return nil, ""
	}
	lines := strings.Split(input, "\n")
	if len(lines) == 1 {
		return nil, trimTrailingCarriage(lines[0])
	}
	remainder := trimTrailingCarriage(lines[len(lines)-1])
	lines = lines[:len(lines)-1]
	for i := range lines {
		lines[i] = trimTrailingCarriage(lines[i])
	}
	return lines, remainder
}

func trimTrailingCarriage(s string) string {
	return strings.TrimSuffix(s, "\r")
}
