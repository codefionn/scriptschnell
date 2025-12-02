package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/statcode-ai/scriptschnell/internal/session"
)

// skipIfTinyGoUnavailable avoids triggering a TinyGo download during tests.
// These sandbox tests require TinyGo to already be installed or cached.
func skipIfTinyGoUnavailable(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("tinygo"); err == nil {
		return
	}

	cacheDir, err := getTinyGoCacheDir()
	if err != nil {
		t.Skipf("TinyGo cache dir unavailable: %v", err)
	}

	binary := filepath.Join(cacheDir, tinyGoVersion, "bin", "tinygo")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	if _, err := os.Stat(binary); err == nil {
		return
	}

	t.Skip("TinyGo not installed or cached; skipping sandbox execution tests")
}

var sandboxConcurrencyLimit = make(chan struct{}, 1)

// limitSandboxConcurrency ensures only one TinyGo-backed sandbox test runs at a time.
// TinyGo builds are CPU-heavy; running them in parallel can exceed timeouts in CI.
func limitSandboxConcurrency(t *testing.T) {
	t.Helper()
	sandboxConcurrencyLimit <- struct{}{}
	t.Cleanup(func() {
		<-sandboxConcurrencyLimit
	})
}

// TestStatusProgramTool_SandboxBackgroundJob tests status_program with sandbox background jobs
func TestStatusProgramTool_SandboxBackgroundJob(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	// Create a sandbox background job manually
	jobID := "sandbox-job-123"
	job := &session.BackgroundJob{
		ID:        jobID,
		Command:   "go_sandbox: fmt.Println(\"test\")",
		StartTime: time.Now().Add(-1 * time.Second),
		Completed: false,
		ExitCode:  0,
		Stdout:    []string{"output line 1", "output line 2", "output line 3"},
		Stderr:    []string{"stderr line 1"},
		PID:       0, // Sandbox jobs don't have PIDs
		Type:      ToolNameGoSandbox,
		Done:      make(chan struct{}),
	}
	sess.AddBackgroundJob(job)

	statusTool := NewStatusProgramTool(sess)
	ctx := context.Background()

	// Test listing all jobs
	t.Run("ListAllJobs", func(t *testing.T) {
		res := statusTool.Execute(ctx, map[string]interface{}{})
		if res.Error != "" {
			t.Fatalf("status_program list failed: %s", res.Error)
		}

		resMap, ok := res.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map result, got %T", res.Result)
		}

		jobsRaw, ok := resMap["jobs"].([]map[string]interface{})
		if !ok {
			t.Fatalf("expected jobs slice, got %T", resMap["jobs"])
		}

		if len(jobsRaw) != 1 {
			t.Fatalf("expected 1 job in list, got %d", len(jobsRaw))
		}

		jobEntry := jobsRaw[0]
		if jobEntry["job_id"] != jobID {
			t.Errorf("expected job_id %s, got %v", jobID, jobEntry["job_id"])
		}
		if jobEntry["type"] != ToolNameGoSandbox {
			t.Errorf("expected type %s, got %v", ToolNameGoSandbox, jobEntry["type"])
		}
		if jobEntry["completed"] != false {
			t.Errorf("expected completed false, got %v", jobEntry["completed"])
		}
		if jobEntry["pid"] != 0 {
			t.Errorf("expected pid 0 for sandbox job, got %v", jobEntry["pid"])
		}
	})

	// Test getting specific job details with line limiting
	t.Run("GetJobDetailsWithLineLimiting", func(t *testing.T) {
		res := statusTool.Execute(ctx, map[string]interface{}{
			"job_id":       jobID,
			"last_n_lines": 2,
		})
		if res.Error != "" {
			t.Fatalf("status_program detail failed: %s", res.Error)
		}

		detailMap, ok := res.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map result, got %T", res.Result)
		}

		if detailMap["job_id"] != jobID {
			t.Errorf("expected job_id %s, got %v", jobID, detailMap["job_id"])
		}

		// Check stdout is limited to last 2 lines
		stdout, ok := detailMap["stdout"].(string)
		if !ok {
			t.Fatalf("expected stdout string, got %T", detailMap["stdout"])
		}
		expectedStdout := "output line 2\noutput line 3"
		if stdout != expectedStdout {
			t.Errorf("expected stdout %q, got %q", expectedStdout, stdout)
		}

		// Check stderr
		stderr, ok := detailMap["stderr"].(string)
		if !ok {
			t.Fatalf("expected stderr string, got %T", detailMap["stderr"])
		}
		if stderr != "stderr line 1" {
			t.Errorf("expected stderr 'stderr line 1', got %q", stderr)
		}
	})

	// Test getting nonexistent job
	t.Run("GetNonexistentJob", func(t *testing.T) {
		res := statusTool.Execute(ctx, map[string]interface{}{
			"job_id": "nonexistent-job",
		})
		if res.Error == "" {
			t.Fatal("expected error for nonexistent job, got nil")
		}
		if !strings.Contains(res.Error, "not found") {
			t.Errorf("expected 'not found' error, got: %s", res.Error)
		}
	})
}

// TestStatusProgramTool_CompletedSandboxJob tests status_program with completed sandbox jobs
func TestStatusProgramTool_CompletedSandboxJob(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	jobID := "completed-sandbox-job"
	job := &session.BackgroundJob{
		ID:        jobID,
		Command:   "go_sandbox: package main",
		StartTime: time.Now().Add(-5 * time.Second),
		Completed: true,
		ExitCode:  0,
		Stdout:    []string{"execution successful"},
		Stderr:    []string{},
		PID:       0,
		Type:      ToolNameGoSandbox,
		Done:      make(chan struct{}),
	}
	close(job.Done)
	sess.AddBackgroundJob(job)

	statusTool := NewStatusProgramTool(sess)
	ctx := context.Background()

	res := statusTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
	})
	if res.Error != "" {
		t.Fatalf("status_program failed: %s", res.Error)
	}

	detailMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	if detailMap["completed"] != true {
		t.Errorf("expected completed true, got %v", detailMap["completed"])
	}
	if detailMap["exit_code"] != 0 {
		t.Errorf("expected exit_code 0, got %v", detailMap["exit_code"])
	}
	if detailMap["type"] != ToolNameGoSandbox {
		t.Errorf("expected type %s, got %v", ToolNameGoSandbox, detailMap["type"])
	}
}

// TestWaitProgramTool_SandboxCompletion tests wait_program with sandbox background execution
func TestWaitProgramTool_SandboxCompletion(t *testing.T) {
	t.Parallel()
	skipIfTinyGoUnavailable(t)
	limitSandboxConcurrency(t)

	workingDir := t.TempDir()
	tempDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	sandboxTool := NewSandboxToolWithFS(workingDir, tempDir, nil, sess, nil)

	ctx := context.Background()

	// Execute sandbox in background
	code := `package main
import "fmt"
func main() {
	fmt.Println("Hello from sandbox")
}`

	res := sandboxTool.Execute(ctx, map[string]interface{}{
		"code":       code,
		"background": true,
	})
	if res.Error != "" {
		t.Fatalf("sandbox execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	jobID, ok := resMap["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected job_id string, got %v", resMap["job_id"])
	}

	// Wait for completion
	waitTool := NewWaitProgramTool(sess)
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	waitRes := waitTool.Execute(waitCtx, map[string]interface{}{
		"job_id": jobID,
	})
	if waitRes.Error != "" {
		t.Fatalf("wait_program failed: %s", waitRes.Error)
	}

	waitMap, ok := waitRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map from wait_program, got %T", waitRes.Result)
	}

	// Verify completion
	if waitMap["completed"] != true {
		t.Errorf("expected completed true, got %v", waitMap["completed"])
	}
	if waitMap["waited"] != true {
		t.Errorf("expected waited true, got %v", waitMap["waited"])
	}
	if waitMap["type"] != ToolNameGoSandbox {
		t.Errorf("expected type %s, got %v", ToolNameGoSandbox, waitMap["type"])
	}

	// Check output
	stdout, ok := waitMap["stdout"].(string)
	if !ok {
		t.Fatalf("expected stdout string, got %T", waitMap["stdout"])
	}
	if !strings.Contains(stdout, "Hello from sandbox") {
		t.Errorf("expected stdout to contain 'Hello from sandbox', got %q", stdout)
	}
}

// TestWaitProgramTool_SandboxTimeout tests wait_program with context timeout
func TestWaitProgramTool_SandboxTimeout(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	// Create a job that never completes
	jobID := "never-completing-job"
	job := &session.BackgroundJob{
		ID:        jobID,
		Command:   "go_sandbox: infinite loop",
		StartTime: time.Now(),
		Completed: false,
		ExitCode:  0,
		Stdout:    []string{},
		Stderr:    []string{},
		PID:       0,
		Type:      ToolNameGoSandbox,
		Done:      make(chan struct{}),
	}
	sess.AddBackgroundJob(job)

	waitTool := NewWaitProgramTool(sess)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result := waitTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
	})

	// Should timeout
	if result.Error == "" {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(result.Error, "context deadline exceeded") {
		t.Errorf("expected context deadline exceeded error, got: %s", result.Error)
	}
}

// TestWaitProgramTool_AlreadyCompleted tests wait_program with already completed job
func TestWaitProgramTool_AlreadyCompleted(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	jobID := "already-completed-job"
	job := &session.BackgroundJob{
		ID:        jobID,
		Command:   "go_sandbox: quick execution",
		StartTime: time.Now().Add(-2 * time.Second),
		Completed: true,
		ExitCode:  0,
		Stdout:    []string{"done immediately"},
		Stderr:    []string{},
		PID:       0,
		Type:      ToolNameGoSandbox,
		Done:      make(chan struct{}),
	}
	close(job.Done)
	sess.AddBackgroundJob(job)

	waitTool := NewWaitProgramTool(sess)
	ctx := context.Background()

	// Should return immediately since job is already completed
	start := time.Now()
	res := waitTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
	})
	elapsed := time.Since(start)

	if res.Error != "" {
		t.Fatalf("wait_program failed: %s", res.Error)
	}

	// Should not have waited long
	if elapsed > 100*time.Millisecond {
		t.Errorf("wait_program took too long for completed job: %v", elapsed)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	if resMap["completed"] != true {
		t.Errorf("expected completed true, got %v", resMap["completed"])
	}
	if resMap["waited"] != true {
		t.Errorf("expected waited true, got %v", resMap["waited"])
	}
}

// TestWaitProgramTool_LineLimiting tests wait_program with last_n_lines parameter
func TestWaitProgramTool_LineLimiting(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	jobID := "line-limiting-job"
	job := &session.BackgroundJob{
		ID:        jobID,
		Command:   "go_sandbox: multiple output lines",
		StartTime: time.Now(),
		Completed: true,
		ExitCode:  0,
		Stdout:    []string{"line 1", "line 2", "line 3", "line 4", "line 5"},
		Stderr:    []string{"error 1", "error 2", "error 3"},
		PID:       0,
		Type:      ToolNameGoSandbox,
		Done:      make(chan struct{}),
	}
	close(job.Done)
	sess.AddBackgroundJob(job)

	waitTool := NewWaitProgramTool(sess)
	ctx := context.Background()

	// Test with last_n_lines = 2
	res := waitTool.Execute(ctx, map[string]interface{}{
		"job_id":       jobID,
		"last_n_lines": 2,
	})
	if res.Error != "" {
		t.Fatalf("wait_program failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	stdout, ok := resMap["stdout"].(string)
	if !ok {
		t.Fatalf("expected stdout string, got %T", resMap["stdout"])
	}

	// Should only contain last 2 lines
	expectedStdout := "line 4\nline 5"
	if stdout != expectedStdout {
		t.Errorf("expected stdout %q, got %q", expectedStdout, stdout)
	}

	stderr, ok := resMap["stderr"].(string)
	if !ok {
		t.Fatalf("expected stderr string, got %T", resMap["stderr"])
	}

	// Should only contain last 2 lines
	expectedStderr := "error 2\nerror 3"
	if stderr != expectedStderr {
		t.Errorf("expected stderr %q, got %q", expectedStderr, stderr)
	}
}

// TestStopProgramTool_SandboxJob tests stop_program with sandbox background jobs
func TestStopProgramTool_SandboxJob(t *testing.T) {
	t.Parallel()
	skipIfTinyGoUnavailable(t)
	limitSandboxConcurrency(t)

	workingDir := t.TempDir()
	tempDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	sandboxTool := NewSandboxToolWithFS(workingDir, tempDir, nil, sess, nil)

	ctx := context.Background()

	// Execute long-running sandbox in background
	code := `package main
import "time"
func main() {
	time.Sleep(60 * time.Second)
	println("Should not reach here")
}`

	res := sandboxTool.Execute(ctx, map[string]interface{}{
		"code":       code,
		"background": true,
		"timeout":    90,
	})
	if res.Error != "" {
		t.Fatalf("sandbox execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	jobID, ok := resMap["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected job_id string, got %v", resMap["job_id"])
	}

	// Give the sandbox a moment to start
	time.Sleep(200 * time.Millisecond)

	// Stop the job
	stopTool := NewStopProgramTool(sess)
	stopRes := stopTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
	})
	if stopRes.Error != "" {
		t.Fatalf("stop_program failed: %s", stopRes.Error)
	}

	stopMap, ok := stopRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", stopRes.Result)
	}

	if stopMap["job_id"] != jobID {
		t.Errorf("expected job_id %s, got %v", jobID, stopMap["job_id"])
	}

	// Wait for completion
	waitTool := NewWaitProgramTool(sess)
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	waitRes := waitTool.Execute(waitCtx, map[string]interface{}{
		"job_id": jobID,
	})
	if waitRes.Error != "" {
		t.Fatalf("wait_program after stop failed: %s", waitRes.Error)
	}

	waitMap, ok := waitRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", waitRes.Result)
	}

	// Verify the job was stopped
	if waitMap["completed"] != true {
		t.Errorf("expected completed true, got %v", waitMap["completed"])
	}
	if waitMap["stop_requested"] != true {
		t.Errorf("expected stop_requested true, got %v", waitMap["stop_requested"])
	}

	// Sandbox jobs should complete when stopped
	stdout, ok := waitMap["stdout"].(string)
	if !ok {
		t.Fatalf("expected stdout string, got %T", waitMap["stdout"])
	}
	if strings.Contains(stdout, "Should not reach here") {
		t.Errorf("sandbox should have been stopped before completion, but got output: %q", stdout)
	}
}

// TestStopProgramTool_AlreadyCompleted tests stopping an already completed sandbox job
func TestStopProgramTool_AlreadyCompleted(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	jobID := "completed-before-stop"
	job := &session.BackgroundJob{
		ID:        jobID,
		Command:   "go_sandbox: already done",
		StartTime: time.Now().Add(-3 * time.Second),
		Completed: true,
		ExitCode:  0,
		Stdout:    []string{"finished"},
		Stderr:    []string{},
		PID:       0,
		Type:      ToolNameGoSandbox,
		Done:      make(chan struct{}),
	}
	close(job.Done)
	sess.AddBackgroundJob(job)

	stopTool := NewStopProgramTool(sess)
	ctx := context.Background()

	res := stopTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
	})
	if res.Error != "" {
		t.Fatalf("stop_program failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	// Should indicate job already completed
	if resMap["completed"] != true {
		t.Errorf("expected completed true, got %v", resMap["completed"])
	}

	message, ok := resMap["message"].(string)
	if !ok {
		t.Fatalf("expected message string, got %T", resMap["message"])
	}
	if !strings.Contains(message, "already completed") {
		t.Errorf("expected 'already completed' message, got: %q", message)
	}
}

// TestStopProgramTool_NonexistentJob tests stopping a nonexistent job
func TestStopProgramTool_NonexistentJob(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	stopTool := NewStopProgramTool(sess)
	ctx := context.Background()

	result := stopTool.Execute(ctx, map[string]interface{}{
		"job_id": "does-not-exist",
	})

	if result.Error == "" {
		t.Fatal("expected error for nonexistent job, got nil")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected 'not found' error, got: %s", result.Error)
	}
}

// TestStopProgramTool_SIGKILLSignal tests stop_program with SIGKILL signal
func TestStopProgramTool_SIGKILLSignal(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)

	jobID := "sigkill-job"
	cancelCalled := false
	cancelFunc := func() {
		cancelCalled = true
	}

	job := &session.BackgroundJob{
		ID:         jobID,
		Command:    "go_sandbox: long running",
		StartTime:  time.Now(),
		Completed:  false,
		ExitCode:   0,
		Stdout:     []string{},
		Stderr:     []string{},
		PID:        0,
		Type:       ToolNameGoSandbox,
		Done:       make(chan struct{}),
		CancelFunc: cancelFunc,
	}
	sess.AddBackgroundJob(job)

	stopTool := NewStopProgramTool(sess)
	ctx := context.Background()

	res := stopTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
		"signal": "SIGKILL",
	})
	if res.Error != "" {
		t.Fatalf("stop_program failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	if resMap["signal"] != "SIGKILL" {
		t.Errorf("expected signal SIGKILL, got %v", resMap["signal"])
	}

	// Verify the cancel function was called
	if !cancelCalled {
		t.Error("expected CancelFunc to be called")
	}

	// Verify the job was marked with the signal
	job, ok = sess.GetBackgroundJob(jobID)
	if !ok {
		t.Fatal("job not found after stop")
	}

	if job.LastSignal != "SIGKILL" {
		t.Errorf("expected LastSignal SIGKILL, got %q", job.LastSignal)
	}
	if !job.StopRequested {
		t.Error("expected StopRequested to be true")
	}
}

// TestWaitProgramTool_MissingJobID tests wait_program with missing job_id parameter
func TestWaitProgramTool_MissingJobID(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	waitTool := NewWaitProgramTool(sess)
	ctx := context.Background()

	result := waitTool.Execute(ctx, map[string]interface{}{})
	if result.Error == "" {
		t.Fatal("expected error for missing job_id, got nil")
	}
	if !strings.Contains(result.Error, "required") {
		t.Errorf("expected 'required' error, got: %s", result.Error)
	}
}

// TestStopProgramTool_MissingJobID tests stop_program with missing job_id parameter
func TestStopProgramTool_MissingJobID(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	stopTool := NewStopProgramTool(sess)
	ctx := context.Background()

	result := stopTool.Execute(ctx, map[string]interface{}{})
	if result.Error == "" {
		t.Fatal("expected error for missing job_id, got nil")
	}
	if !strings.Contains(result.Error, "required") {
		t.Errorf("expected 'required' error, got: %s", result.Error)
	}
}

// TestIntegration_SandboxTimeout_Foreground tests sandbox timeout in foreground execution
func TestIntegration_SandboxTimeout_Foreground(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}

	t.Parallel()
	limitSandboxConcurrency(t)

	workingDir := t.TempDir()
	tempDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	sandboxTool := NewSandboxToolWithFS(workingDir, tempDir, nil, sess, nil)

	ctx := context.Background()

	// Code that runs longer than timeout
	code := `package main
import "time"
func main() {
	time.Sleep(30 * time.Second)
	println("Should not reach here")
}`

	start := time.Now()
	res := sandboxTool.Execute(ctx, map[string]interface{}{
		"code":    code,
		"timeout": 2, // 2 second timeout
	})
	elapsed := time.Since(start)

	if res.Error != "" {
		t.Fatalf("sandbox execute failed: %s", res.Error)
	}

	// Should complete within reasonable time (not wait full 30 seconds)
	if elapsed > 15*time.Second {
		t.Errorf("timeout took too long: %v (expected ~2 seconds)", elapsed)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	// Log the full result for debugging
	t.Logf("Result: %+v", resMap)

	// Check timeout flag - may be in result depending on how timeout occurred
	if timeoutVal, ok := resMap["timeout"]; ok {
		if timeout, ok := timeoutVal.(bool); ok && timeout {
			// Timeout occurred as expected
			t.Logf("Timeout flag set to true")

			// Check exit code when timeout occurred
			if exitCode, ok := resMap["exit_code"].(int); ok && exitCode == -1 {
				t.Logf("Exit code is -1 as expected for timeout")
			}

			return // Test passed
		}
	}

	// Alternative: check if stderr or error indicates timeout/termination
	if stderr, ok := resMap["stderr"].(string); ok {
		if strings.Contains(strings.ToLower(stderr), "timeout") ||
			strings.Contains(strings.ToLower(stderr), "killed") ||
			strings.Contains(strings.ToLower(stderr), "terminated") {
			t.Logf("Timeout indicated in stderr: %q", stderr)
			return // Test passed
		}
	}

	// Check if error field indicates timeout
	if errStr, ok := resMap["error"].(string); ok {
		if strings.Contains(strings.ToLower(errStr), "timeout") {
			t.Logf("Timeout indicated in error: %q", errStr)
			return // Test passed
		}
	}

	// If we get here, no timeout was detected
	t.Errorf("Expected timeout but got result: %+v", resMap)
}

// TestIntegration_SandboxTimeout_Background tests sandbox timeout with background execution
func TestIntegration_SandboxTimeout_Background(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}

	t.Parallel()
	limitSandboxConcurrency(t)

	workingDir := t.TempDir()
	tempDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	sandboxTool := NewSandboxToolWithFS(workingDir, tempDir, nil, sess, nil)

	ctx := context.Background()

	// Code that runs longer than timeout
	code := `package main
import "time"
func main() {
	println("Starting long operation")
	time.Sleep(30 * time.Second)
	println("Should not reach here")
}`

	// Start in background with short timeout
	res := sandboxTool.Execute(ctx, map[string]interface{}{
		"code":       code,
		"timeout":    2, // 2 second timeout
		"background": true,
	})
	if res.Error != "" {
		t.Fatalf("sandbox execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	jobID, ok := resMap["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected job_id string, got %v", resMap["job_id"])
	}

	// Wait for completion
	waitTool := NewWaitProgramTool(sess)
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	start := time.Now()
	waitRes := waitTool.Execute(waitCtx, map[string]interface{}{
		"job_id": jobID,
	})
	elapsed := time.Since(start)

	if waitRes.Error != "" {
		t.Fatalf("wait_program failed: %s", waitRes.Error)
	}

	// Should complete within reasonable time (not full 30 seconds)
	if elapsed > 15*time.Second {
		t.Errorf("wait took too long: %v (expected ~2-5 seconds)", elapsed)
	}

	waitMap, ok := waitRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", waitRes.Result)
	}

	// Check job completed
	if waitMap["completed"] != true {
		t.Errorf("expected completed true, got %v", waitMap["completed"])
	}

	// Check for timeout indicator in stderr
	stderr, ok := waitMap["stderr"].(string)
	if !ok {
		t.Fatalf("expected stderr string, got %T", waitMap["stderr"])
	}
	if !strings.Contains(strings.ToLower(stderr), "timeout") {
		t.Errorf("expected stderr to contain 'timeout', got: %q", stderr)
	}
}

// TestIntegration_SandboxTimeout_StatusCheck tests checking status of timed-out sandbox job
func TestIntegration_SandboxTimeout_StatusCheck(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}

	t.Parallel()
	limitSandboxConcurrency(t)

	workingDir := t.TempDir()
	tempDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	sandboxTool := NewSandboxToolWithFS(workingDir, tempDir, nil, sess, nil)

	ctx := context.Background()

	// Code that will timeout
	code := `package main
import "time"
func main() {
	println("Before timeout")
	time.Sleep(30 * time.Second)
	println("After timeout")
}`

	// Start in background with short timeout
	res := sandboxTool.Execute(ctx, map[string]interface{}{
		"code":       code,
		"timeout":    2,
		"background": true,
	})
	if res.Error != "" {
		t.Fatalf("sandbox execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	jobID, ok := resMap["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected job_id string, got %v", resMap["job_id"])
	}

	// Wait a bit for timeout to occur
	time.Sleep(5 * time.Second)

	// Check status
	statusTool := NewStatusProgramTool(sess)
	statusRes := statusTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
	})
	if statusRes.Error != "" {
		t.Fatalf("status_program failed: %s", statusRes.Error)
	}

	statusMap, ok := statusRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", statusRes.Result)
	}

	// Job should be completed due to timeout
	if statusMap["completed"] != true {
		t.Errorf("expected completed true after timeout, got %v", statusMap["completed"])
	}

	// Check stderr for timeout indication
	stderr, ok := statusMap["stderr"].(string)
	if !ok {
		t.Fatalf("expected stderr string, got %T", statusMap["stderr"])
	}
	if !strings.Contains(strings.ToLower(stderr), "timeout") {
		t.Errorf("expected stderr to contain 'timeout', got: %q", stderr)
	}

	// Output should contain partial results (before timeout)
	stdout, ok := statusMap["stdout"].(string)
	if !ok {
		t.Fatalf("expected stdout string, got %T", statusMap["stdout"])
	}
	// May or may not have captured output before timeout
	_ = stdout // Just verify it exists
}

// TestIntegration_SandboxTimeout_QuickCompletion tests that quick jobs don't falsely timeout
func TestIntegration_SandboxTimeout_QuickCompletion(t *testing.T) {
	if !*runIntegrationTests {
		t.Skip("Skipping WASM integration test - run with: go test -run Integration ./internal/tools -integration")
	}

	t.Parallel()
	limitSandboxConcurrency(t)

	workingDir := t.TempDir()
	tempDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	sandboxTool := NewSandboxToolWithFS(workingDir, tempDir, nil, sess, nil)

	ctx := context.Background()

	// Code that completes quickly
	code := `package main
import "fmt"
func main() {
	fmt.Println("Quick execution")
}`

	res := sandboxTool.Execute(ctx, map[string]interface{}{
		"code":    code,
		"timeout": 30, // Plenty of time
	})
	if res.Error != "" {
		t.Fatalf("sandbox execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}

	// Should NOT timeout
	timeout, ok := resMap["timeout"].(bool)
	if !ok {
		t.Fatalf("expected timeout bool, got %T", resMap["timeout"])
	}
	if timeout {
		t.Error("expected timeout to be false for quick execution")
	}

	// Exit code should be 0
	exitCode, ok := resMap["exit_code"].(int)
	if !ok {
		t.Fatalf("expected exit_code int, got %T", resMap["exit_code"])
	}
	if exitCode != 0 {
		t.Errorf("expected exit_code 0, got %d", exitCode)
	}

	// Check output
	stdout, ok := resMap["stdout"].(string)
	if !ok {
		t.Fatalf("expected stdout string, got %T", resMap["stdout"])
	}
	if !strings.Contains(stdout, "Quick execution") {
		t.Errorf("expected stdout to contain 'Quick execution', got: %q", stdout)
	}
}
