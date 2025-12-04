package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/codefionn/scriptschnell/internal/wasi"
)

// executeInternal performs the actual WASM compilation and execution
// This method is used by the builder to maintain access to TinyGoManager
func (t *SandboxTool) executeInternal(ctx context.Context, builder *SandboxBuilder) (interface{}, error) {
	// Validate builder
	if err := builder.Validate(); err != nil {
		return nil, fmt.Errorf("invalid builder configuration: %w", err)
	}

	code := builder.code
	timeout := builder.timeout
	libraries := builder.libraries

	// Create temporary directory for sandbox
	sandboxDir := filepath.Join(t.tempDir, fmt.Sprintf("sandbox_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(sandboxDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sandbox directory: %w", err)
	}
	defer os.RemoveAll(sandboxDir)

	// Wrap code with authorization layer
	wrappedCode, detectedDomains := wasi.GenerateWASMStub(code)

	// Store detected domains for potential pre-authorization
	_ = detectedDomains // TODO: Could pre-authorize or warn about these
	commandSummary := summarizeSandboxCommand(code)

	// Write wrapped code to file
	mainFile := filepath.Join(sandboxDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(wrappedCode), 0644); err != nil {
		return nil, fmt.Errorf("failed to write code file: %w", err)
	}

	// Get TinyGo binary path (downloads if necessary)
	// TinyGo is REQUIRED for wasip2 support - standard Go only supports wasip1
	// Use a separate context with longer timeout for downloading TinyGo (~50MB)
	if t.tinygoManager == nil {
		return nil, fmt.Errorf("TinyGo manager not initialized - cannot compile WASM code")
	}

	// Use parent context for TinyGo download (not the execution timeout)
	// TinyGo download can take time on slow connections and should not be limited by sandbox timeout
	downloadCtx, downloadCancel := context.WithTimeout(ctx, 10*time.Minute)
	tinyGoBinary, err := t.tinygoManager.GetTinyGoBinary(downloadCtx)
	downloadCancel()
	if err != nil {
		return nil, fmt.Errorf("failed to get TinyGo binary (required for WASI P2 compilation): %w", err)
	}

	// Create context with timeout for actual compilation and execution
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// WASM execution (maximum isolation, controlled network access)
	// Using WASI P1 target because wazero currently exposes mature Preview1 support.
	// Once Preview2 networking APIs stabilize in wazero we can switch to wasip2.
	wasmFile := filepath.Join(sandboxDir, "main.wasm")

	// Build arguments for TinyGo
	args := t.buildTinyGoArgs(wasmFile, libraries)

	// Execute TinyGo compilation
	compileResult, err := t.compileWithTinyGo(execCtx, tinyGoBinary, args, sandboxDir)
	if err != nil {
		return compileResult, nil // Return compilation error as result
	}

	// Read the compiled WASM binary
	wasmBytes, err := os.ReadFile(wasmFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM binary: %w", err)
	}

	// Execute using wazero
	return t.executeWASM(execCtx, wasmBytes, sandboxDir, commandSummary, timeout)
}

// buildTinyGoArgs constructs the command line arguments for TinyGo compilation
func (t *SandboxTool) buildTinyGoArgs(wasmFile string, libraries []string) []string {
	// TinyGo build command
	// TinyGo handles dependencies automatically and doesn't need go.mod
	// Note: Using wasip1 for now. wasip2 would enable HTTP but requires Component Model
	// support that we can't currently provide via wazero host functions.
	// TODO: Switch to wasip2 once wazero's Preview2 support meets our requirements
	args := []string{"build", "-o", wasmFile, "-target=wasip1"}

	// Include scheduler to support basic goroutines (timers, channels, etc.)
	// Note: HTTP still won't work in wasip1 due to missing network APIs
	args = append(args, "--no-debug")

	// Note: TinyGo doesn't support all external libraries yet
	// If libraries are specified, warn the user
	if len(libraries) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: External libraries may not be fully supported by TinyGo: %v\n", libraries)
	}

	args = append(args, "main.go")
	return args
}

// compileWithTinyGo executes the TinyGo compilation process
func (t *SandboxTool) compileWithTinyGo(ctx context.Context, tinyGoBinary string, args []string, sandboxDir string) (interface{}, error) {
	buildCmd := exec.CommandContext(ctx, tinyGoBinary, args...)
	buildCmd.Dir = sandboxDir
	buildCmd.Env = os.Environ()

	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		// Return compilation error as a result map, not an error
		// This allows the sandbox to report compilation errors to the user
		return map[string]interface{}{
			"stdout":    string(buildOutput),
			"exit_code": 1,
			"timeout":   false,
			"error":     "compilation failed",
		}, nil
	}

	return nil, nil // Success, continue with execution
}

// executeDirectCommand executes a command directly without using the shell executor
// This is a fallback when no shell executor is configured
func (t *SandboxTool) executeDirectCommand(ctx context.Context, commandArgs []string, stdinData []byte) (string, string, int) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, commandArgs[0], commandArgs[1:]...)
	cmd.Env = os.Environ()

	// Set stdin if provided
	if len(stdinData) > 0 {
		cmd.Stdin = bytes.NewReader(stdinData)
	}

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout.String(), stderr.String(), exitErr.ExitCode()
		} else {
			return stdout.String(), stderr.String(), 1
		}
	}

	return stdout.String(), stderr.String(), 0
}