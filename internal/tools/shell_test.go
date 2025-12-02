package tools

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

func TestStatusProgramTool_ListAndDetail(t *testing.T) {
	t.Parallel()

	sess := session.NewSession("test", t.TempDir())

	job := &session.BackgroundJob{
		ID:            "job-1",
		Command:       "echo test",
		StartTime:     time.Now().Add(-2 * time.Second),
		Completed:     true,
		ExitCode:      0,
		Stdout:        []string{"line1", "line2"},
		Stderr:        []string{"err1"},
		PID:           123,
		Type:          ToolNameShell,
		StopRequested: true,
		LastSignal:    "SIGTERM",
		Done:          make(chan struct{}),
	}
	close(job.Done)
	sess.AddBackgroundJob(job)

	tool := NewStatusProgramTool(sess)
	ctx := context.Background()

	// List existing jobs
	res := tool.Execute(ctx, map[string]interface{}{})
	if res.Error != "" {
		t.Fatalf("status list failed: %s", res.Error)
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
	if jobEntry["job_id"] != job.ID {
		t.Errorf("expected job_id %s, got %v", job.ID, jobEntry["job_id"])
	}
	if jobEntry["stop_requested"] != true {
		t.Errorf("expected stop_requested true, got %v", jobEntry["stop_requested"])
	}
	if jobEntry["last_signal"] != job.LastSignal {
		t.Errorf("expected last_signal %s, got %v", job.LastSignal, jobEntry["last_signal"])
	}

	// Fetch detailed information with tailing
	detailRes := tool.Execute(ctx, map[string]interface{}{
		"job_id":       job.ID,
		"last_n_lines": 1,
	})
	if detailRes.Error != "" {
		t.Fatalf("status detail failed: %s", detailRes.Error)
	}

	detailMap, ok := detailRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected detail map result, got %T", detailRes.Result)
	}

	if detailMap["job_id"] != job.ID {
		t.Fatalf("expected job_id %s, got %v", job.ID, detailMap["job_id"])
	}
	if detailMap["stdout"] != "line2" {
		t.Errorf("expected stdout 'line2', got %v", detailMap["stdout"])
	}
	if detailMap["stderr"] != "err1" {
		t.Errorf("expected stderr 'err1', got %v", detailMap["stderr"])
	}
	if detailMap["stop_requested"] != true {
		t.Errorf("expected stop_requested true, got %v", detailMap["stop_requested"])
	}
	if detailMap["last_signal"] != job.LastSignal {
		t.Errorf("expected last_signal %s, got %v", job.LastSignal, detailMap["last_signal"])
	}
}

func TestWaitProgramTool_WaitsForCompletion(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell-based tests require sh on non-Windows platforms")
	}

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	shellTool := NewShellTool(sess, workingDir)

	ctx := context.Background()
	cmdParams := map[string]interface{}{
		"command":    "printf 'hello world'",
		"background": true,
	}

	res := shellTool.Execute(ctx, cmdParams)
	if res.Error != "" {
		t.Fatalf("shell execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}
	jobID, ok := resMap["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected job_id string, got %v", resMap["job_id"])
	}

	waitTool := NewWaitProgramTool(sess)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	waitRes := waitTool.Execute(waitCtx, map[string]interface{}{"job_id": jobID})
	if waitRes.Error != "" {
		t.Fatalf("wait_program failed: %s", waitRes.Error)
	}

	waitMap, ok := waitRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map from wait_program, got %T", waitRes.Result)
	}

	if waitMap["completed"] != true {
		t.Errorf("expected completed true, got %v", waitMap["completed"])
	}
	if waitMap["waited"] != true {
		t.Errorf("expected waited true, got %v", waitMap["waited"])
	}
	if waitMap["exit_code"] != 0 {
		t.Errorf("expected exit_code 0, got %v", waitMap["exit_code"])
	}
	stdout, ok := waitMap["stdout"].(string)
	if !ok {
		t.Fatalf("expected stdout string, got %T", waitMap["stdout"])
	}
	if !strings.Contains(stdout, "hello world") {
		t.Errorf("expected stdout to contain 'hello world', got %q", stdout)
	}
}

func TestStopProgramTool_TerminatesProcess(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell-based tests require sh on non-Windows platforms")
	}

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	shellTool := NewShellTool(sess, workingDir)
	stopTool := NewStopProgramTool(sess)
	waitTool := NewWaitProgramTool(sess)

	ctx := context.Background()

	res := shellTool.Execute(ctx, map[string]interface{}{
		"command":    "sleep 5",
		"background": true,
	})
	if res.Error != "" {
		t.Fatalf("shell execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}
	jobID := resMap["job_id"].(string)

	job, ok := sess.GetBackgroundJob(jobID)
	if !ok {
		t.Fatalf("background job not found: %s", jobID)
	}
	if job.ProcessGroupID == 0 {
		t.Fatalf("expected non-zero process group id for job %s", jobID)
	}

	// Give the sleep process a brief moment to start
	time.Sleep(100 * time.Millisecond)

	stopRes := stopTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
		"signal": "SIGKILL",
	})
	if stopRes.Error != "" {
		t.Fatalf("stop_program failed: %s", stopRes.Error)
	}

	stopMap, ok := stopRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map stop result, got %T", stopRes.Result)
	}
	if stopMap["signal"] != "SIGKILL" {
		t.Errorf("expected signal SIGKILL, got %v", stopMap["signal"])
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	waitRes := waitTool.Execute(waitCtx, map[string]interface{}{"job_id": jobID})
	if waitRes.Error != "" {
		t.Fatalf("wait_program after stop failed: %s", waitRes.Error)
	}

	waitMap, ok := waitRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected wait map result, got %T", waitRes.Result)
	}

	if waitMap["completed"] != true {
		t.Errorf("expected completed true after stop, got %v", waitMap["completed"])
	}
	if waitMap["stop_requested"] != true {
		t.Errorf("expected stop_requested true, got %v", waitMap["stop_requested"])
	}
	if waitMap["last_signal"] != "SIGKILL" {
		t.Errorf("expected last_signal SIGKILL, got %v", waitMap["last_signal"])
	}
	exitCode, ok := waitMap["exit_code"].(int)
	if !ok {
		t.Fatalf("expected exit_code int, got %T", waitMap["exit_code"])
	}
	if exitCode == 0 {
		t.Errorf("expected non-zero exit code after SIGKILL, got %d", exitCode)
	}
}

func TestStopProgramTool_TerminatesProcessGroup(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("process-group signalling is not available on Windows")
	}

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	shellTool := NewShellTool(sess, workingDir)
	stopTool := NewStopProgramTool(sess)
	waitTool := NewWaitProgramTool(sess)

	ctx := context.Background()

	res := shellTool.Execute(ctx, map[string]interface{}{
		"command":    "sh -c 'sleep 30'",
		"background": true,
	})
	if res.Error != "" {
		t.Fatalf("shell execute failed: %s", res.Error)
	}

	resMap, ok := res.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", res.Result)
	}
	jobID := resMap["job_id"].(string)

	time.Sleep(100 * time.Millisecond)

	stopRes := stopTool.Execute(ctx, map[string]interface{}{
		"job_id": jobID,
	})
	if stopRes.Error != "" {
		t.Fatalf("stop_program failed: %s", stopRes.Error)
	}

	stopMap, ok := stopRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected stop map, got %T", stopRes.Result)
	}
	if stopMap["signal"] != "SIGTERM" {
		t.Errorf("expected default SIGTERM signal, got %v", stopMap["signal"])
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	waitRes := waitTool.Execute(waitCtx, map[string]interface{}{"job_id": jobID})
	if waitRes.Error != "" {
		t.Fatalf("wait_program after stop failed: %s", waitRes.Error)
	}

	waitMap, ok := waitRes.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected wait map, got %T", waitRes.Result)
	}
	if waitMap["completed"] != true {
		t.Errorf("expected completed true after stop, got %v", waitMap["completed"])
	}
	if waitMap["stop_requested"] != true {
		t.Errorf("expected stop_requested true, got %v", waitMap["stop_requested"])
	}
	if waitMap["last_signal"] != "SIGTERM" {
		t.Errorf("expected last_signal SIGTERM, got %v", waitMap["last_signal"])
	}
	exitCode, ok := waitMap["exit_code"].(int)
	if !ok {
		t.Fatalf("expected exit_code int, got %T", waitMap["exit_code"])
	}
	if exitCode == 0 {
		t.Errorf("expected non-zero exit code after SIGTERM, got %d", exitCode)
	}
}

func TestShellTool_BackgroundShortcut(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell-based tests require sh on non-Windows platforms")
	}

	workingDir := t.TempDir()
	sess := session.NewSession("test", workingDir)
	tool := NewShellTool(sess, workingDir)

	backgroundChan := make(chan struct{}, 1)
	ctx := ContextWithShellBackground(context.Background(), backgroundChan)

	resultCh := make(chan *ToolResult, 1)

	go func() {
		res := tool.Execute(ctx, map[string]interface{}{"command": "sleep 1"})
		resultCh <- res
	}()

	time.Sleep(50 * time.Millisecond)
	backgroundChan <- struct{}{}

	var result *ToolResult
	select {
	case result = <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shell execution result")
	}

	if result.Error != "" {
		t.Fatalf("shell execute returned error: %s", result.Error)
	}

	resMap, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result.Result)
	}

	jobID, ok := resMap["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected job_id string, got %v", resMap["job_id"])
	}

	job, ok := sess.GetBackgroundJob(jobID)
	if !ok {
		t.Fatalf("failed to locate background job %s", jobID)
	}

	select {
	case <-job.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("background job %s did not complete", jobID)
	}

	if !job.Completed {
		t.Errorf("expected job to be marked completed")
	}
}
