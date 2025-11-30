package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/llm"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
	"github.com/statcode-ai/statcode-ai/internal/wasi"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// ShellExecutor is an interface for executing shell commands
// This allows the sandbox to use either direct execution or actor-based execution
type ShellExecutor interface {
	// ExecuteCommand executes a shell command and returns stdout, stderr, and exit code
	ExecuteCommand(ctx context.Context, command string, workingDir string, timeout time.Duration, stdin string) (stdout string, stderr string, exitCode int, err error)
}

// Note: The actor.ShellActor interface already matches this signature perfectly,
// so we can use it directly as a ShellExecutor without needing an adapter

// SandboxTool executes Go code in a sandboxed WebAssembly environment
type SandboxTool struct {
	workingDir      string
	tempDir         string
	filesystem      fs.FileSystem
	session         *session.Session
	authorizer      Authorizer
	tinygoManager   *TinyGoManager
	summarizeClient llm.Client
	shellExecutor   ShellExecutor
}

type sandboxCall struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

type sandboxCallTracker struct {
	mu    sync.Mutex
	calls []sandboxCall
}

func newSandboxCallTracker() *sandboxCallTracker {
	return &sandboxCallTracker{
		calls: make([]sandboxCall, 0),
	}
}

func (t *sandboxCallTracker) record(name, detail string) {
	if name == "" {
		return
	}

	if detail != "" && len(detail) > 120 {
		detail = detail[:117] + "..."
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, sandboxCall{Name: name, Detail: detail})
}

func (t *sandboxCallTracker) metadataDetails() map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.calls) == 0 {
		return nil
	}

	details := make([]map[string]string, 0, len(t.calls))
	counts := make(map[string]int)

	for _, call := range t.calls {
		entry := map[string]string{"name": call.Name}
		if call.Detail != "" {
			entry["detail"] = call.Detail
		}
		details = append(details, entry)
		counts[call.Name]++
	}

	return map[string]interface{}{
		"function_calls":       details,
		"function_call_counts": counts,
	}
}

func NewSandboxTool(workingDir, tempDir string) *SandboxTool {
	tinygoMgr, err := NewTinyGoManager()
	if err != nil {
		// Log error - TinyGo is required for WASI compilation
		fmt.Fprintf(os.Stderr, "Error: Failed to initialize TinyGo manager: %v\n", err)
		fmt.Fprintf(os.Stderr, "The %s tool will not be available until this is resolved.\n", ToolNameGoSandbox)
	}

	return &SandboxTool{
		workingDir:    workingDir,
		tempDir:       tempDir,
		tinygoManager: tinygoMgr,
	}
}

// NewSandboxToolWithFS creates a sandbox with filesystem and session support
func NewSandboxToolWithFS(workingDir, tempDir string, filesystem fs.FileSystem, sess *session.Session, shellExecutor ShellExecutor) *SandboxTool {
	tinygoMgr, err := NewTinyGoManager()
	if err != nil {
		// Log error - TinyGo is required for WASI compilation
		fmt.Fprintf(os.Stderr, "Error: Failed to initialize TinyGo manager: %v\n", err)
		fmt.Fprintf(os.Stderr, "The %s tool will not be available until this is resolved.\n", ToolNameGoSandbox)
	}

	return &SandboxTool{
		workingDir:    workingDir,
		tempDir:       tempDir,
		filesystem:    filesystem,
		session:       sess,
		tinygoManager: tinygoMgr,
		shellExecutor: shellExecutor,
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

// SetShellExecutor sets the shell executor for command execution
func (t *SandboxTool) SetShellExecutor(executor ShellExecutor) {
	t.shellExecutor = executor
}

// GetTinyGoManager returns the TinyGo manager instance (can be nil)
func (t *SandboxTool) GetTinyGoManager() *TinyGoManager {
	return t.tinygoManager
}

func (t *SandboxTool) Name() string {
	return ToolNameGoSandbox
}

func (t *SandboxTool) Description() string {
	var b strings.Builder
	b.WriteString("Execute Go code in a strongly sandboxed WebAssembly environment. ")
	b.WriteString("Standard library packages available (try to not use the `os` package instead, but methods provided below). Timeout enforced.\n")
	b.WriteString("Every program **must** declare `package main`, define `func main()`, and print results (e.g., via `fmt.Println`) so the orchestrator receives the output.\n\n")
	b.WriteString("Seven custom functions are automatically available in your code:\n\n")

	b.WriteString("1. Fetch(method, url, body string) (responseBody string, statusCode int)\n")
	b.WriteString("   - Make HTTP requests (GET, POST, PUT, DELETE, etc.)\n")
	b.WriteString("   - Requires domain authorization\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         response, status := Fetch(\"GET\", \"https://example.com\", \"\")\n")
	b.WriteString("         fmt.Printf(\"Status: %d, Body: %s\\n\", status, response)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("2. Shell(command []string, stdin string) (stdout string, stderr string, exitCode int)\n")
	b.WriteString("   - Execute shell commands with optional stdin input\n")
	b.WriteString("   - Requires command authorization\n")
	b.WriteString("   - Pass empty string for stdin if not needed\n")
	b.WriteString("   - Examples:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         out, err, code := Shell([]string{\"ls\", \"-la\"}, \"\")\n")
	b.WriteString("         fmt.Printf(\"ls output: %s, stderr: %s, exit: %d\\n\", out, err, code)\n\n")
	b.WriteString("         out, err, code = Shell([]string{\"grep\", \"pattern\"}, \"line1\\nline2\\npattern here\\n\")\n")
	b.WriteString("         fmt.Printf(\"grep output: %s, stderr: %s, exit: %d\\n\", out, err, code)\n\n")
	b.WriteString("         out, err, code = Shell([]string{\"go\", \"build\", \"./cmd/statcode-ai\"}, \"\")\n")
	b.WriteString("         fmt.Printf(\"go build output: %s, stderr: %s, exit: %d\\n\", out, err, code)\n\n")
	b.WriteString("         _, err, code = Shell([]string{\"mkdir\", \"-p\", \"tmp/build/cache\"}, \"\")\n")
	b.WriteString("         fmt.Printf(\"mkdir stderr: %s, exit: %d\\n\", err, code)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("3. Summarize(prompt, text string) (summary string)\n")
	b.WriteString("   - Summarize text using AI (fast summarization model)\n")
	b.WriteString("   - No authorization required\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         summary := Summarize(\"Extract the main points\", longText)\n")
	b.WriteString("         fmt.Printf(\"Summary: %s\\n\", summary)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("4. ReadFile(path string, fromLine, toLine int) (content string)\n")
	b.WriteString("   - Read file from filesystem (relative to working directory)\n")
	b.WriteString("   - fromLine and toLine are 1-indexed (use 0, 0 to read entire file)\n")
	b.WriteString("   - Automatically tracked for session (enables write operations)\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         content := ReadFile(\"config.json\", 0, 0)\n")
	b.WriteString("         fmt.Println(content)\n\n")
	b.WriteString("         lines := ReadFile(\"main.go\", 10, 20)\n")
	b.WriteString("         fmt.Println(lines)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("5. CreateFile(path, content string) (errorMsg string)\n")
	b.WriteString("   - Create a new file with given content\n")
	b.WriteString("   - Returns empty string on success, error message on failure\n")
	b.WriteString("   - Fails if file already exists\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         if err := CreateFile(\"output.txt\", \"Hello, World!\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"create error:\", err)\n")
	b.WriteString("             return\n")
	b.WriteString("         }\n")
	b.WriteString("         fmt.Println(\"file created\")\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("6. WriteFile(path, operation string, lineNum int, content string) (errorMsg string)\n")
	b.WriteString("   - Modify existing file with line-based operations (must be read first)\n")
	b.WriteString("   - Operations: \"insert_after\", \"insert_before\", \"update\", \"replace_all\"\n")
	b.WriteString("   - lineNum is 1-indexed (ignored for \"replace_all\")\n")
	b.WriteString("   - Enforces read-before-write safety rule\n")
	b.WriteString("   - Returns empty string on success, error message on failure\n")
	b.WriteString("   - Examples:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         if err := WriteFile(\"file.txt\", \"insert_after\", 5, \"new line\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"insert error:\", err)\n")
	b.WriteString("         }\n\n")
	b.WriteString("         if err := WriteFile(\"file.txt\", \"update\", 3, \"updated line 3\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"update error:\", err)\n")
	b.WriteString("         }\n\n")
	b.WriteString("         if err := WriteFile(\"file.txt\", \"replace_all\", 0, \"new content\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"replace error:\", err)\n")
	b.WriteString("         }\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("7. ListFiles(pattern string) (files string)\n")
	b.WriteString("   - List files matching glob pattern (e.g., \"*.go\", \"**/*.txt\")\n")
	b.WriteString("   - Returns newline-separated list of file paths\n")
	b.WriteString("   - Paths are relative to working directory\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import (\n")
	b.WriteString("         \"fmt\"\n")
	b.WriteString("         \"strings\"\n")
	b.WriteString("     )\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         files := ListFiles(\"*.go\")\n")
	b.WriteString("         for _, f := range strings.Split(files, \"\\n\") {\n")
	b.WriteString("             if f == \"\" {\n")
	b.WriteString("                 continue\n")
	b.WriteString("             }\n")
	b.WriteString("             fmt.Println(f)\n")
	b.WriteString("         }\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n")

	b.WriteString("Example Build Go Program:\n")
	b.WriteString("package main\n")
	b.WriteString("\n")
	b.WriteString("import (\n")
	b.WriteString("	\"fmt\"\n")
	b.WriteString(")\n")
	b.WriteString("\n")
	b.WriteString("func main() {\n")
	b.WriteString("  stdout, stderr, code := Shell([]string{\"go\", \"build\", \"./...\"}, \"\")\n")
	b.WriteString("  if err == 0 {\n")
	b.WriteString("    fmt.Println(\"Compiled successfully\")\n")
	b.WriteString("  } else {\n")
	b.WriteString("    fmt.Println(\"Compilation error: %d %s\", code, stdout)\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")

	b.WriteString("8. GrepFile(pattern, path, glob string, context int) (content string)\n")
	b.WriteString("   - Search for a regex pattern in files, returning matches with line numbers and context.\n")
	b.WriteString("   - `pattern`: Regex pattern to search for.\n")
	b.WriteString("   - `path`: Directory or file to search in (default: '.').\n")
	b.WriteString("   - `glob`: Glob pattern to filter file names (e.g., '*.go', '**/*.js').\n")
	b.WriteString("   - `context`: Number of context lines to show around the match (default: 2).\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         results := GrepFile(\"func main\", \".\", \"*.go\", 1)\n")
	b.WriteString("         fmt.Println(results)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n")

	return b.String()
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
				"description": "Timeout in seconds (default 30, max 3600)",
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

func (t *SandboxTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	// Extract parameters
	code := GetStringParam(params, "code", "")
	if code == "" {
		return &ToolResult{Error: "code is required"}
	}

	logger.Debug("go_sandbox code:\n%s", code)

	timeout := GetIntParam(params, "timeout", 30)
	if timeout > 3600 {
		timeout = 3600
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
		result, err := t.executeBackground(ctx, code, timeout, libraries)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}
	}

	// Use builder to execute
	result, err := t.executeWithBuilder(ctx, code, timeout, libraries)
	if err != nil {
		return &ToolResult{Error: err.Error()}
	}
	return &ToolResult{Result: result}
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
		return nil, fmt.Errorf("background execution requires session support for %s", ToolNameGoSandbox)
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
		Type:       ToolNameGoSandbox,
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
		return ToolNameGoSandbox
	}

	line := trimmed
	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		line = trimmed[:idx]
	}
	line = strings.TrimSpace(line)
	if len(line) > 80 {
		line = line[:80] + "..."
	}
	return fmt.Sprintf("%s: %s", ToolNameGoSandbox, line)
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
	commandSummary := summarizeSandboxCommand(code)

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
	return t.executeWASM(execCtx, wasmBytes, sandboxDir, commandSummary, timeout)
}

// executeWASM runs the WASM binary in an isolated wazero runtime with network interception
func (t *SandboxTool) executeWASM(ctx context.Context, wasmBytes []byte, sandboxDir, commandSummary string, timeoutSeconds int) (interface{}, error) {
	startTime := time.Now()
	callTracker := newSandboxCallTracker()

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
			return t.executeFetch(ctx, adapter, callTracker, m, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen, responsePtr, responseCap)
		}).
		Export("fetch")

	// shell(command_json_ptr, command_json_len, stdin_ptr, stdin_len, stdout_ptr, stdout_cap, stderr_ptr, stderr_cap) -> exit_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, cmdPtr, cmdLen, stdinPtr, stdinLen, stdoutPtr, stdoutCap, stderrPtr, stderrCap uint32) int32 {
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
			var stdinData []byte
			if stdinLen > 0 {
				stdinData, ok = memory.Read(stdinPtr, stdinLen)
				if !ok {
					return -1
				}
			}

			// Check authorization for command
			if adapter != nil && adapter.authorizer != nil {
				decision, err := adapter.Authorize(ctx, ToolNameShell, map[string]interface{}{
					"command":      commandDisplay,
					"command_args": commandArgs,
				})
				if err != nil || decision == nil || !decision.Allowed {
					return -1 // Not authorized
				}
			}

			callTracker.record("shell", commandDisplay)

			// Execute the command using the shell executor
			var stdoutStr, stderrStr string
			var exitCode int
			var err error

			if t.shellExecutor != nil {
				// Use the shell executor (actor-based)
				stdoutStr, stderrStr, exitCode, err = t.shellExecutor.ExecuteCommand(ctx, commandDisplay, "", 30*time.Second, string(stdinData))
			} else {
				// Fallback to direct execution if no executor is set
				cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()

				cmd := exec.CommandContext(cmdCtx, commandArgs[0], commandArgs[1:]...)

				// Set stdin if provided
				if len(stdinData) > 0 {
					cmd.Stdin = bytes.NewReader(stdinData)
				}

				// Capture stdout and stderr
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr

				// Execute command
				err = cmd.Run()
				if err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						exitCode = exitErr.ExitCode()
					} else {
						exitCode = 1
					}
				} else {
					exitCode = 0
				}

				stdoutStr = stdout.String()
				stderrStr = stderr.String()
			}

			// Write stdout to WASM memory
			stdoutBytes := []byte(stdoutStr)
			if uint32(len(stdoutBytes)) > stdoutCap {
				stdoutBytes = stdoutBytes[:stdoutCap]
			}
			if stdoutCap > 0 {
				memory.Write(stdoutPtr, stdoutBytes)
			}

			// Write stderr to WASM memory
			stderrBytes := []byte(stderrStr)
			if uint32(len(stderrBytes)) > stderrCap {
				stderrBytes = stderrBytes[:stderrCap]
			}
			if stderrCap > 0 {
				memory.Write(stderrPtr, stderrBytes)
			}

			return int32(exitCode)
		}).
		Export(ToolNameShell)

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

			callTracker.record("summarize", fmt.Sprintf("prompt:%s", strings.TrimSpace(prompt)))

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

	// read_file(path_ptr, path_len, from_line, to_line, content_ptr, content_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, pathPtr, pathLen uint32, fromLine, toLine int32, contentPtr, contentCap uint32) int32 {
			memory := m.Memory()

			// Read path from WASM memory
			pathBytes, ok := memory.Read(pathPtr, pathLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			path := string(pathBytes)

			// Check if filesystem is available
			if t.filesystem == nil {
				errMsg := []byte("Error: Filesystem not available")
				if uint32(len(errMsg)) <= contentCap {
					memory.Write(contentPtr, errMsg)
				}
				return -2 // Error: no filesystem
			}

			// Read file content
			var content string

			if fromLine > 0 && toLine > 0 {
				// Read specific line range
				lines, readErr := t.filesystem.ReadFileLines(ctx, path, int(fromLine), int(toLine))
				if readErr != nil {
					errMsg := []byte(fmt.Sprintf("Error: Failed to read file: %v", readErr))
					if uint32(len(errMsg)) <= contentCap {
						memory.Write(contentPtr, errMsg)
					}
					return -3 // Error: read failed
				}
				content = strings.Join(lines, "\n")
			} else {
				// Read entire file
				data, readErr := t.filesystem.ReadFile(ctx, path)
				if readErr != nil {
					errMsg := []byte(fmt.Sprintf("Error: Failed to read file: %v", readErr))
					if uint32(len(errMsg)) <= contentCap {
						memory.Write(contentPtr, errMsg)
					}
					return -3 // Error: read failed
				}
				content = string(data)
			}

			// Track file as read in session
			if t.session != nil {
				t.session.TrackFileRead(path, content)
			}

			detail := path
			if fromLine > 0 || toLine > 0 {
				detail = fmt.Sprintf("%s:%d-%d", path, fromLine, toLine)
			}
			callTracker.record("read_file", detail)

			// Write content to WASM memory
			contentBytes := []byte(content)
			if uint32(len(contentBytes)) > contentCap {
				contentBytes = contentBytes[:contentCap]
			}
			if contentCap > 0 {
				memory.Write(contentPtr, contentBytes)
			}

			return 0 // Success
		}).
		Export("read_file")

	// create_file(path_ptr, path_len, content_ptr, content_len) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, pathPtr, pathLen, contentPtr, contentLen uint32) int32 {
			memory := m.Memory()

			// Read path from WASM memory
			pathBytes, ok := memory.Read(pathPtr, pathLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			path := string(pathBytes)

			// Read content from WASM memory
			var content string
			if contentLen > 0 {
				contentBytes, ok := memory.Read(contentPtr, contentLen)
				if !ok {
					return -1 // Error: invalid memory access
				}
				content = string(contentBytes)
			}

			// Check if filesystem is available
			if t.filesystem == nil {
				return -2 // Error: no filesystem
			}

			// Check if file already exists
			exists, err := t.filesystem.Exists(ctx, path)
			if err != nil {
				return -3 // Error: check failed
			}
			if exists {
				return -4 // Error: file already exists
			}

			// Write file
			if err := t.filesystem.WriteFile(ctx, path, []byte(content)); err != nil {
				return -5 // Error: write failed
			}

			// Track file modification in session
			if t.session != nil {
				t.session.TrackFileModified(path)
				t.session.TrackFileRead(path, content)
			}

			callTracker.record("create_file", path)

			return 0 // Success
		}).
		Export("create_file")

	// write_file(path_ptr, path_len, operation_ptr, operation_len, line_num, content_ptr, content_len, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, pathPtr, pathLen, operationPtr, operationLen uint32, lineNum int32, contentPtr, contentLen, resultPtr, resultCap uint32) int32 {
			memory := m.Memory()

			// Read path from WASM memory
			pathBytes, ok := memory.Read(pathPtr, pathLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			path := string(pathBytes)

			// Read operation from WASM memory
			operationBytes, ok := memory.Read(operationPtr, operationLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			operation := string(operationBytes)

			// Read content from WASM memory
			var content string
			if contentLen > 0 {
				contentBytes, ok := memory.Read(contentPtr, contentLen)
				if !ok {
					return -1 // Error: invalid memory access
				}
				content = string(contentBytes)
			}

			// Check if filesystem is available
			if t.filesystem == nil {
				errMsg := []byte("Error: Filesystem not available")
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -2 // Error: no filesystem
			}

			// Check if file exists
			exists, err := t.filesystem.Exists(ctx, path)
			if err != nil || !exists {
				errMsg := []byte(fmt.Sprintf("Error: File not found: %s", path))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -3 // Error: file not found
			}

			// Check read-before-write rule
			if t.session != nil && !t.session.WasFileRead(path) {
				errMsg := []byte(fmt.Sprintf("Error: File %s was not read in this session", path))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -4 // Error: not read before write
			}

			// Read current file content
			currentData, err := t.filesystem.ReadFile(ctx, path)
			if err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Failed to read file: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -5 // Error: read failed
			}

			// Parse file into lines
			lines := strings.Split(string(currentData), "\n")

			// Perform operation
			var finalLines []string
			switch operation {
			case "replace_all":
				// Replace entire file content
				finalLines = strings.Split(content, "\n")

			case "insert_after":
				// Insert content after specified line (1-indexed)
				if lineNum < 1 || int(lineNum) > len(lines) {
					errMsg := []byte(fmt.Sprintf("Error: Line number %d out of range (1-%d)", lineNum, len(lines)))
					if uint32(len(errMsg)) <= resultCap {
						memory.Write(resultPtr, errMsg)
					}
					return -6 // Error: invalid line number
				}
				newLines := strings.Split(content, "\n")
				finalLines = append(finalLines, lines[:lineNum]...)
				finalLines = append(finalLines, newLines...)
				finalLines = append(finalLines, lines[lineNum:]...)

			case "insert_before":
				// Insert content before specified line (1-indexed)
				if lineNum < 1 || int(lineNum) > len(lines) {
					errMsg := []byte(fmt.Sprintf("Error: Line number %d out of range (1-%d)", lineNum, len(lines)))
					if uint32(len(errMsg)) <= resultCap {
						memory.Write(resultPtr, errMsg)
					}
					return -6 // Error: invalid line number
				}
				newLines := strings.Split(content, "\n")
				finalLines = append(finalLines, lines[:lineNum-1]...)
				finalLines = append(finalLines, newLines...)
				finalLines = append(finalLines, lines[lineNum-1:]...)

			case "update":
				// Replace specified line (1-indexed)
				if lineNum < 1 || int(lineNum) > len(lines) {
					errMsg := []byte(fmt.Sprintf("Error: Line number %d out of range (1-%d)", lineNum, len(lines)))
					if uint32(len(errMsg)) <= resultCap {
						memory.Write(resultPtr, errMsg)
					}
					return -6 // Error: invalid line number
				}
				finalLines = make([]string, len(lines))
				copy(finalLines, lines)
				finalLines[lineNum-1] = content

			default:
				errMsg := []byte(fmt.Sprintf("Error: Unknown operation: %s", operation))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -7 // Error: unknown operation
			}

			// Join lines back into content
			finalContent := strings.Join(finalLines, "\n")

			// Write file
			if err := t.filesystem.WriteFile(ctx, path, []byte(finalContent)); err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Failed to write file: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -8 // Error: write failed
			}

			// Track file modification in session
			if t.session != nil {
				t.session.TrackFileModified(path)
				t.session.TrackFileRead(path, finalContent)
			}

			callTracker.record("write_file", fmt.Sprintf("%s (%s at %d)", path, operation, lineNum))

			return 0 // Success
		}).
		Export("write_file")

	// list_files(pattern_ptr, pattern_len, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, patternPtr, patternLen, resultPtr, resultCap uint32) int32 {
			memory := m.Memory()

			// Read pattern from WASM memory
			patternBytes, ok := memory.Read(patternPtr, patternLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			pattern := string(patternBytes)

			// Check if filesystem is available
			if t.filesystem == nil {
				errMsg := []byte("Error: Filesystem not available")
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -2 // Error: no filesystem
			}

			// Use filepath.Glob to match files
			// Pattern should be relative to working directory
			fullPattern := filepath.Join(t.workingDir, pattern)
			matches, err := filepath.Glob(fullPattern)
			if err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Invalid pattern: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -3 // Error: invalid pattern
			}

			// Convert absolute paths back to relative paths
			var relativePaths []string
			for _, match := range matches {
				relPath, err := filepath.Rel(t.workingDir, match)
				if err != nil {
					relPath = match // fallback to absolute path
				}
				relativePaths = append(relativePaths, relPath)
			}

			// Join files with newlines
			result := strings.Join(relativePaths, "\n")
			resultBytes := []byte(result)
			if uint32(len(resultBytes)) > resultCap {
				resultBytes = resultBytes[:resultCap]
			}
			if resultCap > 0 {
				memory.Write(resultPtr, resultBytes)
			}

			callTracker.record("list_files", pattern)

			return 0 // Success
		}).
		Export("list_files")

	_, err = envBuilder.Instantiate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate host functions: %w", err)
	}

	// Compile the module
	mod, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		return attachExecutionMetadata(map[string]interface{}{
			"stdout":    "",
			"stderr":    fmt.Sprintf("WASM compilation failed: %v", err),
			"exit_code": 1,
			"timeout":   false,
			"error":     "compilation failed",
		}, t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, 1, "", fmt.Sprintf("WASM compilation failed: %v", err), false, callTracker)), nil
	}

	// Instantiate the module with stdout/stderr configuration
	config := wazero.NewModuleConfig().
		WithStdout(outFile).
		WithStderr(errFile)

	modInstance, err := r.InstantiateModule(ctx, mod, config)
	if err != nil {
		return attachExecutionMetadata(map[string]interface{}{
			"stdout":    "",
			"stderr":    fmt.Sprintf("WASM instantiation failed: %v", err),
			"exit_code": 1,
			"timeout":   false,
			"error":     "instantiation failed",
		}, t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, 1, "", fmt.Sprintf("WASM instantiation failed: %v", err), false, callTracker)), nil
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
		return attachExecutionMetadata(map[string]interface{}{
			"stdout":    "",
			"stderr":    "WASM module does not export _start function",
			"exit_code": 1,
			"timeout":   false,
			"error":     "no _start function",
		}, t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, 1, "", "WASM module does not export _start function", false, callTracker)), nil
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

		return attachExecutionMetadata(map[string]interface{}{
			"stdout":    "",
			"stderr":    "Execution timeout",
			"exit_code": -1,
			"timeout":   true,
			"error":     "execution timeout",
		}, t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, -1, "", "Execution timeout", true, callTracker)), nil
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
		metadata := t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, finalExitCode, stdout, stderr, false, callTracker)
		return attachExecutionMetadata(map[string]interface{}{
			"stdout":    stdout,
			"stderr":    stderr,
			"exit_code": finalExitCode,
			"timeout":   false,
			"error":     "runtime error",
		}, metadata), nil
	}

	metadata := t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, finalExitCode, stdout, stderr, false, callTracker)
	return attachExecutionMetadata(map[string]interface{}{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": finalExitCode,
		"timeout":   false,
	}, metadata), nil
}

// executeFetch performs an HTTP request from WASM with authorization
func (t *SandboxTool) executeFetch(ctx context.Context, adapter *wasiAuthorizerAdapter, tracker *sandboxCallTracker, m api.Module, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen, responsePtr, responseCap uint32) uint32 {
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
		decision, err := adapter.Authorize(ctx, ToolNameGoSandboxDomain, map[string]interface{}{
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

	if tracker != nil {
		host := parsedURL.Host
		if host == "" {
			host = urlStr
		}
		tracker.record("fetch", fmt.Sprintf("%s %s", strings.ToUpper(method), host))
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

func (t *SandboxTool) buildSandboxMetadata(startTime time.Time, commandSummary string, timeoutSeconds int, exitCode int, stdout, stderr string, timedOut bool, tracker *sandboxCallTracker) *ExecutionMetadata {
	endTime := time.Now()

	outputBytes, outputLines := CalculateOutputStats(stdout)
	stderrBytes, stderrLines := CalculateOutputStats(stderr)

	metadata := &ExecutionMetadata{
		StartTime:       &startTime,
		EndTime:         &endTime,
		DurationMs:      endTime.Sub(startTime).Milliseconds(),
		Command:         commandSummary,
		ExitCode:        exitCode,
		OutputSizeBytes: outputBytes,
		OutputLineCount: outputLines,
		HasStderr:       stderr != "",
		StderrSizeBytes: stderrBytes,
		StderrLineCount: stderrLines,
		WorkingDir:      t.workingDir,
		TimeoutSeconds:  timeoutSeconds,
		WasTimedOut:     timedOut,
		WasBackgrounded: false,
		ToolType:        ToolNameGoSandbox,
	}

	if tracker != nil {
		if details := tracker.metadataDetails(); details != nil {
			metadata.Details = details
		}
	}

	return metadata
}

func attachExecutionMetadata(result map[string]interface{}, metadata *ExecutionMetadata) map[string]interface{} {
	if metadata == nil {
		return result
	}
	result["_execution_metadata"] = metadata
	return result
}
