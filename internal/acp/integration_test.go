package acp

import (
	"testing"

	"github.com/coder/acp-go-sdk"
)

// TestACPAgentSlashCommandWorkflow exercises a basic slash-command flow.
func TestACPAgentSlashCommandWorkflow(t *testing.T) {
	agent := newTestAgent(t)

	initParams := acp.InitializeRequest{
		ClientInfo:      &acp.Implementation{Name: "test-client", Version: "1.0.0"},
		ProtocolVersion: acp.ProtocolVersionNumber,
	}

	if _, err := agent.Initialize(agent.ctx, initParams); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	sessionResp, err := agent.NewSession(agent.ctx, acp.NewSessionRequest{
		Cwd:        agent.config.WorkingDir,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	promptResp, err := agent.Prompt(agent.ctx, acp.PromptRequest{
		SessionId: sessionResp.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock("/help")},
	})
	if err != nil {
		t.Fatalf("Prompt returned error: %v", err)
	}

	if promptResp.StopReason != acp.StopReasonEndTurn {
		t.Errorf("unexpected stop reason: %s", promptResp.StopReason)
	}
}
