package eval

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ContainerEngine represents the detected container runtime
type ContainerEngine string

const (
	EngineDocker ContainerEngine = "docker"
	EnginePodman ContainerEngine = "podman"
)

// ContainerConfig holds configuration for container execution
type ContainerConfig struct {
	Image        string
	WorkspaceDir string
	Timeout      time.Duration
	Env          map[string]string
}

// ContainerExecutor handles Docker/Podman operations
type ContainerExecutor struct {
	engine    ContainerEngine
	engineCmd string
}

// NewContainerExecutor auto-detects available container engine
func NewContainerExecutor() (*ContainerExecutor, error) {
	// Try Podman first (matches lib.sh behavior)
	if _, err := exec.LookPath("podman"); err == nil {
		return &ContainerExecutor{
			engine:    EnginePodman,
			engineCmd: "podman",
		}, nil
	}

	// Fallback to Docker
	if _, err := exec.LookPath("docker"); err == nil {
		return &ContainerExecutor{
			engine:    EngineDocker,
			engineCmd: "docker",
		}, nil
	}

	return nil, fmt.Errorf("no container engine found (tried podman, docker)")
}

// BuildImage builds the scriptschnell image with multi-stage Dockerfile in a temp directory
func (ce *ContainerExecutor) BuildImage(ctx context.Context, imageName string, baseImage string) error {
	// Ensure base directory exists
	baseDir := "/tmp/scriptschnell-eval"
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Create temp directory for this build (per eval definition)
	tmpDir, err := os.MkdirTemp(baseDir, "build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate entrypoint script
	entrypointScript := ce.generateEntrypointScript()
	entrypointPath := filepath.Join(tmpDir, "scriptschnell-entrypoint.sh")
	if err := os.WriteFile(entrypointPath, []byte(entrypointScript), 0755); err != nil {
		return fmt.Errorf("failed to write entrypoint script: %w", err)
	}

	// Generate Dockerfile that copies the entrypoint script
	dockerfile := ce.generateDockerfile(baseImage)
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Build image from repository root (context includes source code)
	repoRoot := getRepositoryRoot()
	cmd := exec.CommandContext(ctx, ce.engineCmd, "build",
		"-f", dockerfilePath,
		"-t", imageName,
		"--build-context", fmt.Sprintf("build-scripts=%s", tmpDir),
		repoRoot,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// RunEval executes eval in container with LLM integration using environment variables
func (ce *ContainerExecutor) RunEval(ctx context.Context, config *ContainerConfig, prompt, modelID, provider string, runID int64) (*ContainerResult, error) {
	log.Printf("DEBUG: RunEval called with prompt (len=%d): %q", len(prompt), prompt)
	log.Printf("DEBUG: RunEval model=%s provider=%s runID=%d", modelID, provider, runID)

	// Apply timeout from config to context
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	// Container volumes and environment setup
	containerName := fmt.Sprintf("eval-%d-%d", runID, time.Now().Unix())

	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"-v", fmt.Sprintf("%s:/workspace:Z", config.WorkspaceDir),
		"-w", "/workspace",
	}

	// Add all environment variables
	for key, val := range config.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, val))
	}

	// Add model and provider via environment variables
	if provider != "" {
		args = append(args, "-e", fmt.Sprintf("SCRIPTSCHNELL_PROVIDER=%s", provider))
	}
	if modelID != "" {
		args = append(args, "-e", fmt.Sprintf("SCRIPTSCHNELL_MODEL=%s", modelID))
	}

	// Execute with the prompt as argument (entrypoint script handles the rest)
	args = append(args, config.Image, prompt)

	log.Printf("DEBUG: About to execute container command: %s %v", ce.engineCmd, args)
	log.Printf("DEBUG: Prompt argument (final): %q", prompt)

	cmd := exec.CommandContext(ctx, ce.engineCmd, args...)

	// Capture both stdout and stderr for better debugging
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &ContainerResult{
		Output:   stdout.String() + stderr.String(), // Combine both for debugging
		ExitCode: 0,
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Add error details if execution failed
	if err != nil {
		result.Output = fmt.Sprintf("Container execution failed (container: %s, exit %d): %v\nSTDOUT:\n%s\nSTDERR:\n%s",
			containerName, result.ExitCode, err, stdout.String(), stderr.String())

		// Try fallback: run scriptschnell directly if entrypoint failed (exit code 125)
		if result.ExitCode == 125 {
			log.Printf("Entrypoint failed, trying fallback: running scriptschnell directly...")
			fallbackResult, fallbackErr := ce.runFallbackDirect(ctx, config, prompt, modelID, provider, runID)
			if fallbackErr == nil {
				return fallbackResult, nil // Fallback succeeded
			}
			// Fallback failed, return original error
			result.Output += fmt.Sprintf("\nFallback also failed: %v", fallbackErr)
		}
	}

	return result, err
}

// RunCLITest executes a single CLI test case in container
func (ce *ContainerExecutor) RunCLITest(ctx context.Context, config *ContainerConfig, execPath string, args []string, timeout time.Duration) (*TestResult, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	testCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build command: docker run --rm --entrypoint /workspace/calculator -v workspace:/workspace image "8 * 10 + 2"
	cmdArgs := []string{
		"run",
		"--rm",
		"--entrypoint", execPath,
		"-v", fmt.Sprintf("%s:/workspace:Z", config.WorkspaceDir),
		"-w", "/workspace",
		config.Image,
	}
	cmdArgs = append(cmdArgs, args...)

	startTime := time.Now()
	cmd := exec.CommandContext(testCtx, ce.engineCmd, cmdArgs...)

	output, err := cmd.CombinedOutput()
	executionTime := time.Since(startTime)

	result := &TestResult{
		Output:        strings.TrimSpace(string(output)),
		ExitCode:      0,
		ExecutionTime: int(executionTime.Milliseconds()),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if err != nil && testCtx.Err() == context.DeadlineExceeded {
		result.Error = fmt.Sprintf("timeout after %v", timeout)
	}

	return result, nil
}

// generateEntrypointScript creates the bash entrypoint script
func (ce *ContainerExecutor) generateEntrypointScript() string {
	return `#!/bin/bash
set -e

# Build scriptschnell command args
ARGS=(scriptschnell --dangerous-allow-all --json)

# Support extended JSON output for eval tracking
if [ -n "$SCRIPTSCHNELL_JSON_EXTENDED" ]; then
    ARGS+=(--json-extended)
fi

if [ -n "$SCRIPTSCHNELL_PROVIDER" ]; then
    ARGS+=(-provider "$SCRIPTSCHNELL_PROVIDER")
fi

if [ -n "$SCRIPTSCHNELL_MODEL" ]; then
    ARGS+=(-model "$SCRIPTSCHNELL_MODEL")
fi

# Execute with all arguments properly quoted
exec "${ARGS[@]}" "$@"
`
}

// generateDockerfile creates multi-stage Dockerfile with custom entrypoint script
func (ce *ContainerExecutor) generateDockerfile(baseImage string) string {
	return fmt.Sprintf(`FROM docker.io/tinygo/tinygo:0.39.0 AS tinygo

FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y git gcc libc6-dev && rm -rf /var/lib/apt/lists/*

COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
ENV PATH="/usr/local/tinygo/bin:${PATH}"

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY ./internal ./internal
COPY ./cmd ./cmd

RUN go build -o scriptschnell ./cmd/scriptschnell

FROM %s

RUN apt-get update && apt-get install -y bash ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=tinygo /usr/local/tinygo /usr/local/tinygo
ENV PATH="/usr/local/tinygo/bin:${PATH}"

COPY --from=builder /app/scriptschnell /usr/local/bin/scriptschnell

RUN mkdir -p /root/.config/scriptschnell

# Copy entrypoint script from build context
COPY --from=build-scripts scriptschnell-entrypoint.sh /usr/local/bin/scriptschnell-entrypoint.sh

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/scriptschnell-entrypoint.sh"]
`, baseImage)
}

// ContainerResult holds result of container execution
type ContainerResult struct {
	Output   string
	ExitCode int
}

// TestResult holds result of CLI test execution
type TestResult struct {
	Output        string
	ExitCode      int
	ExecutionTime int
	Error         string
}

// runFallbackDirect runs scriptschnell directly when entrypoint fails
func (ce *ContainerExecutor) runFallbackDirect(ctx context.Context, config *ContainerConfig, prompt, modelID, provider string, runID int64) (*ContainerResult, error) {
	log.Printf("Running fallback: scriptschnell directly...")

	containerName := fmt.Sprintf("eval-fallback-%d-%d", runID, time.Now().Unix())

	// Build args for direct execution
	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"-v", fmt.Sprintf("%s:/workspace:Z", config.WorkspaceDir),
		"-w", "/workspace",
	}

	// Add environment variables
	for key, val := range config.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, val))
	}

	// Add model and provider
	if provider != "" {
		args = append(args, "-e", fmt.Sprintf("SCRIPTSCHNELL_PROVIDER=%s", provider))
	}
	if modelID != "" {
		args = append(args, "-e", fmt.Sprintf("SCRIPTSCHNELL_MODEL=%s", modelID))
	}

	// Override entrypoint to run scriptschnell directly
	args = append(args, "--entrypoint", "/usr/local/bin/scriptschnell")
	args = append(args, config.Image, "--dangerous-allow-all", prompt)

	cmd := exec.CommandContext(ctx, ce.engineCmd, args...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &ContainerResult{
		Output:   fmt.Sprintf("FALLBACK EXECUTION:\nSTDOUT:\n%s\nSTDERR:\n%s", stdout.String(), stderr.String()),
		ExitCode: 0,
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if err != nil {
		result.Output = fmt.Sprintf("Fallback execution also failed (exit %d): %v", result.ExitCode, err)
		return result, err
	}

	return result, nil
}

// Helper function to get repository root
func getRepositoryRoot() string {
	// Use git to find repository root
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		// Fallback: assume we're in internal/eval and go up two levels
		return filepath.Join("..", "..")
	}
	return strings.TrimSpace(string(output))
}
