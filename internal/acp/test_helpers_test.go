package acp

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/coder/acp-go-sdk"
)

// newTestProviderManager creates a real provider manager backed by a temp file.
func newTestProviderManager(t *testing.T) *provider.Manager {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "providers.json")
	mgr, err := provider.NewManager(cfgPath, "")
	if err != nil {
		t.Fatalf("failed to create provider manager: %v", err)
	}
	return mgr
}

// newNoopConnection wires an agent-side connection that discards all outbound traffic.
func newNoopConnection(t *testing.T, agent acp.Agent) *acp.AgentSideConnection {
	t.Helper()
	return acp.NewAgentSideConnection(agent, io.Discard, strings.NewReader(""))
}

// newTestAgent builds a ScriptschnellAIAgent with a discard ACP connection.
func newTestAgent(t *testing.T) *ScriptschnellAIAgent {
	t.Helper()
	cfg := &config.Config{WorkingDir: t.TempDir()}
	mgr := newTestProviderManager(t)
	agent, err := NewScriptschnellAIAgent(context.Background(), cfg, mgr)
	if err != nil {
		t.Fatalf("NewScriptschnellAIAgent returned error: %v", err)
	}
	conn := newNoopConnection(t, agent)
	agent.SetAgentConnection(conn)
	return agent
}
