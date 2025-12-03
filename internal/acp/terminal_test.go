package acp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/coder/acp-go-sdk"
)

type stubShellActor struct {
	jobs map[string]bool
}

func newStubShellActor() *stubShellActor {
	return &stubShellActor{jobs: make(map[string]bool)}
}

func (s *stubShellActor) Receive(context.Context, actor.Message) error { return nil }
func (s *stubShellActor) Start(context.Context) error                  { return nil }
func (s *stubShellActor) Stop(context.Context) error                   { return nil }
func (s *stubShellActor) ID() string                                   { return "stub-shell" }

func (s *stubShellActor) ExecuteCommand(ctx context.Context, command []string, workingDir string, timeout time.Duration, stdin string) (string, string, int, error) {
	return "", "", 0, nil
}

func (s *stubShellActor) ExecuteCommandBackground(ctx context.Context, command []string, workingDir string) (string, int, error) {
	jobID := fmt.Sprintf("job-%d", len(s.jobs)+1)
	s.jobs[jobID] = true
	return jobID, 1234, nil
}

func (s *stubShellActor) GetJobStatus(ctx context.Context, jobID string) (bool, int, string, string, bool, error) {
	if !s.jobs[jobID] {
		return false, 0, "", "", false, fmt.Errorf("job not found")
	}
	return false, 0, "out", "", true, nil
}

func (s *stubShellActor) WaitForJob(ctx context.Context, jobID string) (int, string, string, error) {
	if !s.jobs[jobID] {
		return 0, "", "", fmt.Errorf("job not found")
	}
	return 0, "out", "", nil
}

func (s *stubShellActor) StopJob(ctx context.Context, jobID string, signal string) error {
	if !s.jobs[jobID] {
		return fmt.Errorf("job not found")
	}
	return nil
}

func TestTerminalManagerCreateAndOutput(t *testing.T) {
	shellActor := newStubShellActor()
	manager := NewTerminalManager(shellActor)

	ctx := context.Background()
	cwd := "/tmp"
	req := acp.CreateTerminalRequest{
		Command: "echo",
		Args:    []string{"hello"},
		Cwd:     &cwd,
	}

	resp, err := manager.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if resp.TerminalId == "" {
		t.Fatal("Create returned empty terminal ID")
	}

	outResp, err := manager.Output(ctx, acp.TerminalOutputRequest{
		SessionId:  "session",
		TerminalId: resp.TerminalId,
	})
	if err != nil {
		t.Fatalf("Output returned error: %v", err)
	}
	if outResp.ExitStatus == nil {
		t.Errorf("expected exit status")
	}
}

func TestTerminalManagerInvalidIDs(t *testing.T) {
	shellActor := newStubShellActor()
	manager := NewTerminalManager(shellActor)
	ctx := context.Background()

	if _, err := manager.Output(ctx, acp.TerminalOutputRequest{SessionId: "s", TerminalId: "missing"}); err == nil {
		t.Error("expected error for missing terminal")
	}

	if _, err := manager.WaitForExit(ctx, acp.WaitForTerminalExitRequest{SessionId: "s", TerminalId: "missing"}); err == nil {
		t.Error("expected error for missing terminal")
	}

	if _, err := manager.Kill(ctx, acp.KillTerminalCommandRequest{SessionId: "s", TerminalId: "missing"}); err == nil {
		t.Error("expected error for missing terminal")
	}

	if _, err := manager.Release(ctx, acp.ReleaseTerminalRequest{SessionId: "s", TerminalId: "missing"}); err == nil {
		t.Error("expected error for missing terminal")
	}
}
