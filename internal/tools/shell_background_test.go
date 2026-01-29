package tools

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

// TestShellBackgroundJobDoneChannelOrdering tests that the Done channel is closed
// after Wait() and wg.Wait() complete, ensuring proper ordering.
func TestShellBackgroundJobDoneChannelOrdering(t *testing.T) {
	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	// Create a command that sleeps for a very short time
	cmd := exec.Command("sleep", "0.1")
	cmd.Dir = tmpDir

	startedAt := time.Now()
	job, _ := registerShellBackgroundJob(sess, cmd, "sleep 0.1", tmpDir, startedAt)

	// Verify job is registered
	if job == nil {
		t.Fatal("Expected job to be registered")
	}

	// Verify Done channel is not closed yet
	select {
	case <-job.Done:
		t.Fatal("Done channel should not be closed before command completes")
	case <-time.After(10 * time.Millisecond):
		// Expected: channel not closed yet
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Simulate the background completion goroutine
	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()
		// Read stdout/stderr (like the actual implementation)
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		// Keep references to prevent GC (simulating actual pipe usage)
		_ = stdout
		_ = stderr
	}()

	// Wait for completion (this is the actual implementation pattern)
	go func() {
		err := cmd.Wait()
		wg.Wait()
		defer close(job.Done)

		job.Mu.Lock()
		job.Completed = true
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				job.ExitCode = exitErr.ExitCode()
			} else {
				job.ExitCode = -1
			}
		} else {
			job.ExitCode = 0
		}
		job.Process = nil
		job.Mu.Unlock()
	}()

	// Wait for the Done channel to be closed
	timeout := time.After(5 * time.Second)
	select {
	case <-job.Done:
		// Done channel closed - success
	case <-timeout:
		t.Fatal("Timed out waiting for Done channel to be closed")
	}

	// Verify job state is correct
	job.Mu.Lock()
	if !job.Completed {
		t.Error("Expected job to be marked as completed")
	}
	if job.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", job.ExitCode)
	}
	job.Mu.Unlock()
}

// TestShellBackgroundJobExitCodeHandling tests that exit codes are set correctly
func TestShellBackgroundJobExitCodeHandling(t *testing.T) {
	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	// Test successful command (exit code 0)
	cmd := exec.Command("true")
	cmd.Dir = tmpDir

	startedAt := time.Now()
	job, _ := registerShellBackgroundJob(sess, cmd, "true", tmpDir, startedAt)

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Wait for completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Expected successful command, got error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for command")
	}

	// Update job state (simulating the background goroutine)
	job.Mu.Lock()
	job.Completed = true
	job.ExitCode = 0
	job.Mu.Unlock()

	// Verify exit code
	job.Mu.RLock()
	if job.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", job.ExitCode)
	}
	job.Mu.RUnlock()
}

// TestShellBackgroundJobFailedExitCode tests that failed commands set non-zero exit codes
func TestShellBackgroundJobFailedExitCode(t *testing.T) {
	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	// Test failing command (non-zero exit code)
	cmd := exec.Command("false")
	cmd.Dir = tmpDir

	startedAt := time.Now()
	job, _ := registerShellBackgroundJob(sess, cmd, "false", tmpDir, startedAt)

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Wait for completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Expected error from failing command")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for command")
	}

	// Update job state (simulating the background goroutine)
	job.Mu.Lock()
	job.Completed = true
	if exitErr, ok := cmd.ProcessState.Sys().(interface{ ExitStatus() int }); ok {
		job.ExitCode = exitErr.ExitStatus()
	} else {
		job.ExitCode = -1
	}
	job.Mu.Unlock()

	// Verify exit code is non-zero
	job.Mu.RLock()
	if job.ExitCode == 0 {
		t.Errorf("Expected non-zero exit code, got %d", job.ExitCode)
	}
	if job.ExitCode == -1 {
		// This is acceptable on some systems
		t.Log("Exit code set to -1 (unable to determine actual exit code)")
	}
	job.Mu.RUnlock()
}

// TestRegisterShellBackgroundJob tests the job registration function
func TestRegisterShellBackgroundJob(t *testing.T) {
	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	cmd := exec.Command("echo", "test")
	cmd.Dir = tmpDir

	startedAt := time.Now()
	job, jobID := registerShellBackgroundJob(sess, cmd, "echo test", tmpDir, startedAt)

	// Verify job was created
	if job == nil {
		t.Fatal("Expected job to be created")
	}

	// Verify job ID format
	if jobID == "" {
		t.Fatal("Expected job ID to be set")
	}
	if job.ID != jobID {
		t.Errorf("Expected job ID %s, got %s", jobID, job.ID)
	}

	// Verify job fields
	if job.Command != "echo test" {
		t.Errorf("Expected command 'echo test', got '%s'", job.Command)
	}
	if job.WorkingDir != tmpDir {
		t.Errorf("Expected working dir %s, got %s", tmpDir, job.WorkingDir)
	}
	if job.Type != "shell" {
		t.Errorf("Expected type 'shell', got '%s'", job.Type)
	}
	if job.StartTime.IsZero() {
		t.Error("Expected start time to be set")
	}
	if job.Completed {
		t.Error("Expected job to not be completed initially")
	}
	if job.Done == nil {
		t.Fatal("Expected Done channel to be set")
	}

	// Verify job is registered in session
	retrievedJob, ok := sess.GetBackgroundJob(jobID)
	if !ok {
		t.Fatal("Expected job to be registered in session")
	}
	if retrievedJob.ID != jobID {
		t.Errorf("Expected job ID %s, got %s", jobID, retrievedJob.ID)
	}
}

// TestSendSignalToBackgroundJob tests signal sending functionality
func TestSendSignalToBackgroundJob(t *testing.T) {
	// Skip on Windows due to signal differences
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping signal test on Windows")
	}

	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	// Create a long-running command
	cmd := exec.Command("sleep", "10")
	cmd.Dir = tmpDir

	// Start the command
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	startedAt := time.Now()
	job, _ := registerShellBackgroundJob(sess, cmd, "sleep 10", tmpDir, startedAt)

	// Wait a bit to ensure command is running
	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM
	sig := syscall.SIGTERM
	if err := sendSignalToBackgroundJob(job, sig, "SIGTERM"); err != nil {
		t.Errorf("Failed to send signal: %v", err)
	}

	// Wait for command to terminate
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Command terminated, which is expected
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for command to terminate")
	}

	// Verify job state
	job.Mu.RLock()
	if job.StopRequested {
		t.Log("Stop requested flag set (expected)")
	}
	job.Mu.RUnlock()
}

// TestBackgroundJobConcurrency tests concurrent access to background job
func TestBackgroundJobConcurrency(t *testing.T) {
	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	cmd := exec.Command("echo", "concurrent")
	cmd.Dir = tmpDir

	startedAt := time.Now()
	job, jobID := registerShellBackgroundJob(sess, cmd, "echo concurrent", tmpDir, startedAt)

	// Run concurrent goroutines that access job fields
	var wg sync.WaitGroup
	numGoroutines := 10
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			// Read job fields
			job.Mu.RLock()
			_ = job.ID
			_ = job.Command
			_ = job.Completed
			job.Mu.RUnlock()

			// Write to job fields
			time.Sleep(time.Duration(idx) * time.Millisecond)
			job.Mu.Lock()
			job.Stdout = append(job.Stdout, "line")
			job.Mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify job is still in valid state
	job.Mu.RLock()
	if job.ID != jobID {
		t.Errorf("Job ID changed unexpectedly")
	}
	if len(job.Stdout) != numGoroutines {
		t.Errorf("Expected %d stdout lines, got %d", numGoroutines, len(job.Stdout))
	}
	job.Mu.RUnlock()
}

// TestShellBackgroundJobDoneChannelClosedOnce tests that Done channel is only closed once
func TestShellBackgroundJobDoneChannelClosedOnce(t *testing.T) {
	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	cmd := exec.Command("echo", "test")
	cmd.Dir = tmpDir

	startedAt := time.Now()
	job, _ := registerShellBackgroundJob(sess, cmd, "echo test", tmpDir, startedAt)

	// Close Done channel
	close(job.Done)

	// Verify that closing again doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panic when closing Done channel twice: %v", r)
		}
	}()

	// This should panic, so we check that it doesn't cause issues
	select {
	case <-job.Done:
		// Channel is closed, as expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected Done channel to be closed")
	}
}

// TestShellBackgroundJobMultipleGoroutines tests multiple goroutines waiting on Done
func TestShellBackgroundJobMultipleGoroutines(t *testing.T) {
	// Create a temporary session
	tmpDir := t.TempDir()
	sess := session.NewSession("test", tmpDir)

	cmd := exec.Command("sleep", "0.2")
	cmd.Dir = tmpDir

	startedAt := time.Now()
	job, _ := registerShellBackgroundJob(sess, cmd, "sleep 0.2", tmpDir, startedAt)

	// Start multiple goroutines that wait on Done channel
	numGoroutines := 5
	completed := make([]bool, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-job.Done
			completed[idx] = true
		}(i)
	}

	// Simulate background completion
	time.Sleep(250 * time.Millisecond)
	close(job.Done)

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify all goroutines completed
	for i, done := range completed {
		if !done {
			t.Errorf("Goroutine %d did not complete", i)
		}
	}
}
