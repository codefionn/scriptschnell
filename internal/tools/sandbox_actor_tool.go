package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/actor"
	"github.com/statcode-ai/statcode-ai/internal/session"
	"github.com/tetratelabs/wazero/api"
)

// SandboxToolWithActor is the sandbox tool that uses ShellActor for shell operations
type SandboxToolWithActor struct {
	session    *session.Session
	shellActor actor.ShellActor
	authorizer Authorizer
}

func NewSandboxToolWithActor(sess *session.Session, shellActor actor.ShellActor, authorizer Authorizer) *SandboxToolWithActor {
	return &SandboxToolWithActor{
		session:    sess,
		shellActor: shellActor,
		authorizer: authorizer,
	}
}

func (t *SandboxToolWithActor) Name() string        { return ToolNameGoSandbox }
func (t *SandboxToolWithActor) Description() string { return (&SandboxTool{}).Description() }
func (t *SandboxToolWithActor) Parameters() map[string]interface{} {
	return (&SandboxTool{}).Parameters()
}

func (t *SandboxToolWithActor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	// This would be a full implementation of the sandbox tool
	// For now, we'll focus on providing the shell host function
	return &ToolResult{Error: "SandboxToolWithActor.Execute not fully implemented - use shellHostFunction method"}
}

// shellHostFunction provides the shell host function for WASM that uses ShellActor
func (t *SandboxToolWithActor) shellHostFunction(ctx context.Context, m api.Module, cmdPtr, cmdLen, stdinPtr, stdinLen, stdoutPtr, stdoutCap, stderrPtr, stderrCap uint32) int32 {
	// Read command from WASM memory
	memory := m.Memory()
	cmdBytes, ok := memory.Read(cmdPtr, cmdLen)
	if !ok {
		return -1
	}

	var commandArgs []string
	if err := json.Unmarshal(cmdBytes, &commandArgs); err != nil {
		return -1
	}
	if len(commandArgs) == 0 {
		return -1
	}
	commandDisplay := strings.Join(commandArgs, " ")

	// Read stdin from WASM memory (if provided)
	var stdinData string
	if stdinLen > 0 {
		stdinBytes, ok := memory.Read(stdinPtr, stdinLen)
		if !ok {
			return -1
		}
		stdinData = string(stdinBytes)
	}

	// Check authorization for command
	if t.authorizer != nil {
		decision, err := t.authorizer.Authorize(ctx, ToolNameShell, map[string]interface{}{
			"command":      commandDisplay,
			"command_args": commandArgs,
		})
		if err != nil || decision == nil || !decision.Allowed {
			return -1 // Not authorized
		}
	}

	// Execute the command using ShellActor
	stdout, stderr, exitCode, err := t.shellActor.ExecuteCommand(ctx, commandDisplay, "", 30*time.Second, stdinData)
	if err != nil {
		// Even if there's an error, we might still have output
		if exitCode == 0 {
			exitCode = -1
		}
	}

	// Write stdout back to WASM memory
	if len(stdout) > 0 {
		if !t.writeToWASMMemory(memory, stdout, stdoutPtr, stdoutCap) {
			return -1
		}
	}

	// Write stderr back to WASM memory
	if len(stderr) > 0 {
		if !t.writeToWASMMemory(memory, stderr, stderrPtr, stderrCap) {
			return -1
		}
	}

	return int32(exitCode)
}

// writeToWASMMemory writes data to WASM memory and returns true on success
func (t *SandboxToolWithActor) writeToWASMMemory(memory api.Memory, data string, ptr, capacity uint32) bool {
	dataBytes := []byte(data)
	if len(dataBytes) > int(capacity) {
		// Truncate if it doesn't fit
		dataBytes = dataBytes[:capacity]
	}

	ok := memory.Write(ptr, dataBytes)
	return ok
}

// GetShellHostFunction returns a function that can be used as the shell host function in WASM
func (t *SandboxToolWithActor) GetShellHostFunction() func(context.Context, api.Module, uint32, uint32, uint32, uint32, uint32, uint32, uint32, uint32) int32 {
	return t.shellHostFunction
}
