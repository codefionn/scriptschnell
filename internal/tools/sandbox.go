package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/llm"
	"github.com/statcode-ai/statcode-ai/internal/session"
	"github.com/statcode-ai/statcode-ai/internal/wasi"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// SandboxTool executes Go code in a sandboxed WebAssembly environment
type SandboxTool struct {
	workingDir      string
	tempDir         string
	filesystem      fs.FileSystem
	session         *session.Session
	authorizer      Authorizer
	tinygoManager   *TinyGoManager
	summarizeClient llm.Client
}

func NewSandboxTool(workingDir, tempDir string) *SandboxTool {
	tinygoMgr, err := NewTinyGoManager()
	if err != nil {
		// Log error - TinyGo is required for WASI compilation
		fmt.Fprintf(os.Stderr, "Error: Failed to initialize TinyGo manager: %v\n", err)
		fmt.Fprintf(os.Stderr, "The go_sandbox tool will not be available until this is resolved.\n")
	}

	return &SandboxTool{
		workingDir:    workingDir,
		tempDir:       tempDir,
		tinygoManager: tinygoMgr,
	}
}

// NewSandboxToolWithFS creates a sandbox with filesystem and session support
func NewSandboxToolWithFS(workingDir, tempDir string, filesystem fs.FileSystem, sess *session.Session) *SandboxTool {
	tinygoMgr, err := NewTinyGoManager()
	if err != nil {
		// Log error - TinyGo is required for WASI compilation
		fmt.Fprintf(os.Stderr, "Error: Failed to initialize TinyGo manager: %v\n", err)
		fmt.Fprintf(os.Stderr, "The go_sandbox tool will not be available until this is resolved.\n")
	}

	return &SandboxTool{
		workingDir:    workingDir,
		tempDir:       tempDir,
		filesystem:    filesystem,
		session:       sess,
		tinygoManager: tinygoMgr,
	}
}

// SetAuthorizer sets the authorizer for domain authorization at runtime
func (t *SandboxTool) SetAuthorizer(auth Authorizer) {
	t.authorizer = auth
}

// SetSummarizeClient sets the summarization LLM client
func (t *SandboxTool) SetSummarizeClient(client llm.Client) {
	t.summarizeClient = client
}

// GetTinyGoManager returns the TinyGo manager instance (can be nil)
func (t *SandboxTool) GetTinyGoManager() *TinyGoManager {
	return t.tinygoManager
}

func (t *SandboxTool) Name() string {
	return "go_sandbox"
}

func (t *SandboxTool) Description() string {
	return `Execute Go code in a sandboxed WebAssembly environment. Standard library packages available. Timeout enforced.

Three custom functions are automatically available in your code:

1. Fetch(method, url, body string) (responseBody string, statusCode int)
   - Make HTTP requests (GET, POST, PUT, DELETE, etc.)
   - Requires domain authorization
   - Example:
     response, status := Fetch("GET", "https://example.com", "")
     fmt.Printf("Status: %d, Body: %s\n", status, response)

2. Shell(command string) (stdout string, stderr string, exitCode int)
   - Execute shell commands
   - Requires command authorization
   - Example:
     out, err, code := Shell("ls -la")
     fmt.Printf("Output: %s\nExit: %d\n", out, code)

3. Summarize(prompt, text string) (summary string)
   - Summarize text using AI (fast summarization model)
   - No authorization required
   - Example:
     summary := Summarize("Extract the main points", longText)
     fmt.Printf("Summary: %s\n", summary)`
}

func (t *SandboxTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "Go code to execute. Must include package main and func main()",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (default 30, max 120)",
			},
			"libraries": map[string]interface{}{
				"type":        "array",
				"description": "Additional Go module dependencies (e.g., ['github.com/foo/bar@v1.0.0'])",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"background": map[string]interface{}{
				"type":        "boolean",
				"description": "Run the sandbox in the background and stream output via the status tool.",
			},
		},
		"required": []string{"code"},
	}
}

func (t *SandboxTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	// Extract parameters
	code := GetStringParam(params, "code", "")
	if code == "" {
		return nil, fmt.Errorf("code is required")
	}

	timeout := GetIntParam(params, "timeout", 30)
	if timeout > 120 {
		timeout = 120
	}

	// Get libraries if specified
	var libraries []string
	if libsRaw, ok := params["libraries"]; ok {
		if libsArr, ok := libsRaw.([]interface{}); ok {
			for _, lib := range libsArr {
				if libStr, ok := lib.(string); ok {
					libraries = append(libraries, libStr)
				}
			}
		}
	}

	background := GetBoolParam(params, "background", false)

	if background {
		return t.executeBackground(ctx, code, timeout, libraries)
	}

	// Use builder to execute
	return t.executeWithBuilder(ctx, code, timeout, libraries)
}

// executeWithBuilder uses the SandboxBuilder to execute code
func (t *SandboxTool) executeWithBuilder(ctx context.Context, code string, timeout int, libraries []string) (interface{}, error) {
	// Create builder with current tool configuration
	builder := NewSandboxBuilder().
		SetCode(code).
		SetTimeout(timeout).
		SetWorkingDir(t.workingDir).
		SetTempDir(t.tempDir)

	// Add libraries if any
	if len(libraries) > 0 {
		builder = builder.AddLibraries(libraries...)
	}

	// Set filesystem and session if available
	if t.filesystem != nil {
		builder = builder.SetFilesystem(t.filesystem)
	}
	if t.session != nil {
		builder = builder.SetSession(t.session)
	}

	// Set authorization if available
	if t.authorizer != nil {
		builder = builder.SetAuthorization(t.authorizer)
	}

	// Build and execute using internal compilation method
	// We need to use the internal method to preserve TinyGo manager connection
	return t.executeInternal(ctx, builder)
}

func (t *SandboxTool) executeBackground(ctx context.Context, code string, timeout int, libraries []string) (interface{}, error) {
	if t.session == nil {
		return nil, fmt.Errorf("background execution requires session support for go_sandbox")
	}

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	commandSummary := summarizeSandboxCommand(code)

	job := &session.BackgroundJob{
		ID:         jobID,
		Command:    commandSummary,
		WorkingDir: t.workingDir,
		StartTime:  time.Now(),
		Completed:  false,
		Stdout:     make([]string, 0),
		Stderr:     make([]string, 0),
		Type:       "go_sandbox",
		Done:       make(chan struct{}),
	}

	execCtx, cancel := context.WithCancel(context.Background())
	job.CancelFunc = cancel

	t.session.AddBackgroundJob(job)

	go func() {
		defer cancel()
		defer close(job.Done)

		result, err := t.executeWithBuilder(execCtx, code, timeout, libraries)
		job.Completed = true

		if err != nil {
			if errors.Is(err, context.Canceled) {
				job.Stderr = append(job.Stderr, "sandbox execution canceled")
			} else {
				job.Stderr = append(job.Stderr, fmt.Sprintf("sandbox error: %v", err))
			}
			job.ExitCode = -1
			return
		}

		job.ExitCode = 0

		if resMap, ok := result.(map[string]interface{}); ok {
			if exitVal, ok := resMap["exit_code"]; ok {
				job.ExitCode = coerceExitCode(exitVal)
			}
			if stdout, ok := resMap["stdout"].(string); ok && stdout != "" {
				job.Stdout = append(job.Stdout, splitOutputLines(stdout)...)
			}
			if stderrStr, ok := resMap["stderr"].(string); ok && stderrStr != "" {
				job.Stderr = append(job.Stderr, splitOutputLines(stderrStr)...)
			}
			if timeoutFlag, ok := resMap["timeout"].(bool); ok && timeoutFlag {
				job.Stderr = append(job.Stderr, "sandbox execution timed out")
			}
			if errMsg, ok := resMap["error"].(string); ok && errMsg != "" {
				job.Stderr = append(job.Stderr, errMsg)
			}
		} else if result != nil {
			job.Stdout = append(job.Stdout, fmt.Sprintf("result: %v", result))
		}
	}()

	return map[string]interface{}{
		"job_id":  jobID,
		"message": "Sandbox execution started in background. Use 'status_program' to stream progress, 'wait_program' to block until completion, or 'stop_program' to terminate.",
	}, nil
}

func summarizeSandboxCommand(code string) string {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "go_sandbox"
	}

	line := trimmed
	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		line = trimmed[:idx]
	}
	line = strings.TrimSpace(line)
	if len(line) > 80 {
		line = line[:80] + "..."
	}
	return fmt.Sprintf("go_sandbox: %s", line)
}

func splitOutputLines(text string) []string {
	lines := strings.Split(text, "\n")
	// Trim trailing empty line that results from ending newline
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func coerceExitCode(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

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

	// Write wrapped code to file
	mainFile := filepath.Join(sandboxDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(wrappedCode), 0644); err != nil {
		return nil, fmt.Errorf("failed to write code file: %w", err)
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Get TinyGo binary path (downloads if necessary)
	// TinyGo is REQUIRED for wasip2 support - standard Go only supports wasip1
	if t.tinygoManager == nil {
		return nil, fmt.Errorf("TinyGo manager not initialized - cannot compile WASM code")
	}

	tinyGoBinary, err := t.tinygoManager.GetTinyGoBinary(execCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get TinyGo binary (required for WASI P2 compilation): %w", err)
	}

	// WASM execution (maximum isolation, controlled network access)
	// Using WASI P1 target because wazero currently exposes mature Preview1 support.
	// Once Preview2 networking APIs stabilize in wazero we can switch to wasip2.
	wasmFile := filepath.Join(sandboxDir, "main.wasm")

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
	buildCmd := exec.CommandContext(execCtx, tinyGoBinary, args...)
	buildCmd.Dir = sandboxDir

	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		return map[string]interface{}{
			"stdout":    string(buildOutput),
			"exit_code": 1,
			"timeout":   false,
			"error":     "compilation failed",
		}, nil
	}

	// Read the compiled WASM binary
	wasmBytes, err := os.ReadFile(wasmFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM binary: %w", err)
	}

	// Execute using wazero
	return t.executeWASM(execCtx, wasmBytes, sandboxDir)
}

// executeWASM runs the WASM binary in an isolated wazero runtime with network interception
func (t *SandboxTool) executeWASM(ctx context.Context, wasmBytes []byte, sandboxDir string) (interface{}, error) {
	// Create wazero runtime
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	// Setup stdout/stderr capture
	stdoutFile := filepath.Join(sandboxDir, "stdout.txt")
	stderrFile := filepath.Join(sandboxDir, "stderr.txt")

	outFile, err := os.Create(stdoutFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout file: %w", err)
	}
	defer outFile.Close()

	errFile, err := os.Create(stderrFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr file: %w", err)
	}
	defer errFile.Close()

	// Create shared state for host functions
	// IMPORTANT: We must ALWAYS provide the authorize_domain host function
	// because our wrapped code declares it with //go:wasmimport
	var adapter *wasiAuthorizerAdapter

	if t.authorizer != nil {
		adapter = &wasiAuthorizerAdapter{authorizer: t.authorizer}
	} else {
		// Use a stub authorizer that allows all domains (for tests without authorization)
		adapter = &wasiAuthorizerAdapter{authorizer: &stubAuthorizer{}}
	}

	// Instantiate WASI with stdout/stderr redirection
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	// Add custom host functions for HTTP and shell
	envBuilder := r.NewHostModuleBuilder("env")

	// fetch(method_ptr, method_len, url_ptr, url_len, body_ptr, body_len, response_ptr, response_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen, responsePtr, responseCap uint32) uint32 {
			return t.executeFetch(ctx, adapter, m, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen, responsePtr, responseCap)
		}).
		Export("fetch")

	// shell(cmd_ptr, cmd_len, stdout_ptr, stdout_cap, stderr_ptr, stderr_cap) -> exit_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, cmdPtr, cmdLen, stdoutPtr, stdoutCap, stderrPtr, stderrCap uint32) int32 {
			// Read command from WASM memory
			memory := m.Memory()
			cmdBytes, ok := memory.Read(cmdPtr, cmdLen)
			if !ok {
				return -1
			}
			command := string(cmdBytes)

			// Check authorization for command
			if adapter != nil && adapter.authorizer != nil {
				decision, err := adapter.Authorize(ctx, "shell", map[string]interface{}{
					"command": command,
				})
				if err != nil || decision == nil || !decision.Allowed {
					return -1 // Not authorized
				}
			}

			// Execute the command with timeout
			cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)

			// Capture stdout and stderr
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			// Execute command
			err := cmd.Run()
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = 1
				}
			}

			// Write stdout to WASM memory
			stdoutBytes := stdout.Bytes()
			if uint32(len(stdoutBytes)) > stdoutCap {
				stdoutBytes = stdoutBytes[:stdoutCap]
			}
			if stdoutCap > 0 {
				memory.Write(stdoutPtr, stdoutBytes)
			}

			// Write stderr to WASM memory
			stderrBytes := stderr.Bytes()
			if uint32(len(stderrBytes)) > stderrCap {
				stderrBytes = stderrBytes[:stderrCap]
			}
			if stderrCap > 0 {
				memory.Write(stderrPtr, stderrBytes)
			}

			return int32(exitCode)
		}).
		Export("shell")

	// summarize(prompt_ptr, prompt_len, text_ptr, text_len, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, promptPtr, promptLen, textPtr, textLen, resultPtr, resultCap uint32) int32 {
			// Read prompt and text from WASM memory
			memory := m.Memory()

			promptBytes, ok := memory.Read(promptPtr, promptLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			prompt := string(promptBytes)

			textBytes, ok := memory.Read(textPtr, textLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			text := string(textBytes)

			// Check if summarization client is available
			if t.summarizeClient == nil {
				errMsg := []byte("Error: Summarization client not available")
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -2 // Error: no summarization client
			}

			// Build the full summarization prompt
			fullPrompt := fmt.Sprintf(`%s

Text to summarize:
%s

Provide a concise summary based on the instructions above.`, prompt, text)

			// Call the summarization LLM
			summary, err := t.summarizeClient.Complete(ctx, fullPrompt)
			if err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Summarization failed: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -3 // Error: summarization failed
			}

			// Write result to WASM memory
			summaryBytes := []byte(summary)
			if uint32(len(summaryBytes)) > resultCap {
				summaryBytes = summaryBytes[:resultCap]
			}
			if resultCap > 0 {
				memory.Write(resultPtr, summaryBytes)
			}

			return 0 // Success
		}).
		Export("summarize")

	_, err = envBuilder.Instantiate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate host functions: %w", err)
	}

	// Compile the module
	mod, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		return map[string]interface{}{
			"stdout":    "",
			"stderr":    fmt.Sprintf("WASM compilation failed: %v", err),
			"exit_code": 1,
			"timeout":   false,
			"error":     "compilation failed",
		}, nil
	}

	// Instantiate the module with stdout/stderr configuration
	config := wazero.NewModuleConfig().
		WithStdout(outFile).
		WithStderr(errFile)

	modInstance, err := r.InstantiateModule(ctx, mod, config)
	if err != nil {
		return map[string]interface{}{
			"stdout":    "",
			"stderr":    fmt.Sprintf("WASM instantiation failed: %v", err),
			"exit_code": 1,
			"timeout":   false,
			"error":     "instantiation failed",
		}, nil
	}
	defer modInstance.Close(ctx)

	// Set memory in host state after instantiation
	if memory := modInstance.Memory(); memory != nil {
		// TODO: wire up host_state helpers for wazero's memory API if needed.
		_ = memory
	}

	// Get the _start function and execute
	startFn := modInstance.ExportedFunction("_start")
	if startFn == nil {
		return map[string]interface{}{
			"stdout":    "",
			"stderr":    "WASM module does not export _start function",
			"exit_code": 1,
			"timeout":   false,
			"error":     "no _start function",
		}, nil
	}

	// Execute with timeout handling
	resultChan := make(chan error, 1)
	var exitCode uint32

	go func() {
		_, err := startFn.Call(ctx)
		if exitErr, ok := err.(*sys.ExitError); ok {
			exitCode = exitErr.ExitCode()
			if exitCode == 0 {
				resultChan <- nil
			} else {
				resultChan <- exitErr
			}
		} else {
			resultChan <- err
		}
	}()

	var runtimeErr error
	select {
	case err := <-resultChan:
		runtimeErr = err
	case <-ctx.Done():
		// Close files before reading on timeout
		outFile.Close()
		errFile.Close()

		return map[string]interface{}{
			"stdout":    "",
			"stderr":    "Execution timeout",
			"exit_code": -1,
			"timeout":   true,
			"error":     "execution timeout",
		}, nil
	}

	// Close files before reading
	outFile.Close()
	errFile.Close()

	// Read stdout/stderr from files
	stdoutBytes, _ := os.ReadFile(stdoutFile)
	stderrBytes, _ := os.ReadFile(stderrFile)

	stdout := string(stdoutBytes)
	stderr := string(stderrBytes)

	finalExitCode := int(exitCode)

	if runtimeErr != nil {
		// Check if it's just a normal exit
		if exitErr, ok := runtimeErr.(*sys.ExitError); ok {
			finalExitCode = int(exitErr.ExitCode())
			if finalExitCode == 0 {
				// Normal exit (exit code 0), not an error
				runtimeErr = nil
			}
		}
	}

	if runtimeErr != nil {
		if stderr == "" {
			stderr = "Runtime error: " + runtimeErr.Error()
		}
		return map[string]interface{}{
			"stdout":    stdout,
			"stderr":    stderr,
			"exit_code": finalExitCode,
			"timeout":   false,
			"error":     "runtime error",
		}, nil
	}

	return map[string]interface{}{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": finalExitCode,
		"timeout":   false,
	}, nil
}

// executeFetch performs an HTTP request from WASM with authorization
func (t *SandboxTool) executeFetch(ctx context.Context, adapter *wasiAuthorizerAdapter, m api.Module, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen, responsePtr, responseCap uint32) uint32 {
	// Read method from WASM memory
	memory := m.Memory()
	methodBytes, ok := memory.Read(methodPtr, methodLen)
	if !ok {
		return 400 // Bad request - invalid memory access
	}
	method := string(methodBytes)

	// Read URL from WASM memory
	urlBytes, ok := memory.Read(urlPtr, urlLen)
	if !ok {
		return 400 // Bad request - invalid memory access
	}
	urlStr := string(urlBytes)

	// Parse URL to extract domain for authorization
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return 400 // Bad request - invalid URL
	}

	// Check authorization for domain
	if adapter != nil && adapter.authorizer != nil {
		decision, err := adapter.Authorize(ctx, "go_sandbox_domain", map[string]interface{}{
			"domain": parsedURL.Host,
		})
		if err != nil || decision == nil || !decision.Allowed {
			return 403 // Forbidden - not authorized
		}
	}

	// Read body from WASM memory
	var bodyBytes []byte
	if bodyLen > 0 {
		bodyBytes, ok = memory.Read(bodyPtr, bodyLen)
		if !ok {
			return 400 // Bad request - invalid memory access
		}
	}

	// Create HTTP request
	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return 400 // Bad request - invalid request
	}

	// Execute HTTP request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return 500 // Internal server error - request failed
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 500 // Internal server error - failed to read response
	}

	// Write response to WASM memory
	if uint32(len(respBody)) > responseCap {
		// Truncate if response is too large
		respBody = respBody[:responseCap]
	}

	if !memory.Write(responsePtr, respBody) {
		return 500 // Internal server error - failed to write response
	}

	// Return HTTP status code
	return uint32(resp.StatusCode)
}
