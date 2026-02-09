package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// executeWASM runs the WASM binary in an isolated wazero runtime with network interception.
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

	// Register all host functions
	t.registerFetchHostFunction(envBuilder, adapter, callTracker)
	t.registerShellHostFunction(envBuilder, adapter, callTracker)
	t.registerSummarizeHostFunction(envBuilder, callTracker)
	t.registerReadFileHostFunction(envBuilder, callTracker)
	t.registerCreateFileHostFunction(envBuilder, callTracker)
	t.registerWriteFileHostFunction(envBuilder, callTracker)
	t.registerMkdirHostFunction(envBuilder, callTracker)
	t.registerMoveHostFunction(envBuilder, callTracker)
	t.registerListFilesHostFunction(envBuilder, callTracker)
	t.registerRemoveFileHostFunction(envBuilder, callTracker)
	t.registerRemoveDirHostFunction(envBuilder, callTracker)
	t.registerConvertHTMLHostFunction(envBuilder, callTracker)
	t.registerGetLastExitCodeHostFunction(envBuilder)
	t.registerGetLastStdoutHostFunction(envBuilder)
	t.registerGetLastStderrHostFunction(envBuilder)

	_, err = envBuilder.Instantiate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate host functions: %w", err)
	}

	// Compile the module
	mod, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		logger.Debug("WASM compilation failed for sandbox: %v", err)
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
		WithStderr(errFile).
		WithSysWalltime().
		WithSysNanotime()

	// Pass all environment variables to the WASM module
	for _, envVar := range os.Environ() {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) == 2 {
			config = config.WithEnv(parts[0], parts[1])
		}
	}

	// Override TMPDIR/TEMP/TMP with session temp dir so sandboxed code uses it
	if t.session != nil {
		if shellTmpDir := t.session.GetShellTempDir(); shellTmpDir != "" {
			config = config.WithEnv("TMPDIR", shellTmpDir)
			config = config.WithEnv("TEMP", shellTmpDir)
			config = config.WithEnv("TMP", shellTmpDir)
			config = config.WithEnv("SCRIPTSCHNELL_SHELL_TEMP", shellTmpDir)
		}
	}

	// Mount filesystem if available
	if t.filesystem != nil {
		authorizedFS := NewAuthorizedFS(t.filesystem, t.session, t.workingDir)
		fsAdapter := NewFSAdapter(ctx, authorizedFS)
		config = config.WithFS(fsAdapter)
	}

	modInstance, err := r.InstantiateModule(ctx, mod, config)
	if err != nil {
		logger.Debug("WASM instantiation failed for sandbox: %v", err)
		return attachExecutionMetadata(map[string]interface{}{
			"stdout":    "",
			"stderr":    fmt.Sprintf("WASM instantiation failed: %v", err),
			"exit_code": 1,
			"timeout":   false,
			"error":     "instantiation failed",
		}, t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, 1, "", fmt.Sprintf("WASM instantiation failed: %v", err), false, callTracker)), nil
	}
	defer modInstance.Close(ctx)

	// Get the _start function and execute
	startFn := modInstance.ExportedFunction("_start")
	if startFn == nil {
		logger.Debug("WASM module does not export _start function")
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
		logger.Warn("sandbox: killing process due to %s (timeout=%ds): %s", ctx.Err(), timeoutSeconds, commandSummary)
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
			} else {
				logger.Debug("WASM execution exited with code %d: %v", finalExitCode, runtimeErr)
			}
		} else {
			logger.Debug("WASM runtime error: %v", runtimeErr)
		}
	}

	if runtimeErr != nil {
		if stderr == "" {
			stderr = "Runtime error: " + runtimeErr.Error()
		}
		metadata := t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, finalExitCode, stdout, stderr, false, callTracker)

		// Store the output in session even on error
		if t.session != nil {
			t.session.SetLastSandboxOutput(finalExitCode, stdout, stderr)
		}

		return attachExecutionMetadata(map[string]interface{}{
			"stdout":    stdout,
			"stderr":    stderr,
			"exit_code": finalExitCode,
			"timeout":   false,
			"error":     "runtime error",
		}, metadata), nil
	}

	metadata := t.buildSandboxMetadata(startTime, commandSummary, timeoutSeconds, finalExitCode, stdout, stderr, false, callTracker)

	// Store the output in session for the next sandbox execution
	if t.session != nil {
		t.session.SetLastSandboxOutput(finalExitCode, stdout, stderr)
	}

	return attachExecutionMetadata(map[string]interface{}{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": finalExitCode,
		"timeout":   false,
	}, metadata), nil
}

// executeFetch performs an HTTP request from WASM with authorization.
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
		if err != nil {
			return 403 // Forbidden - authorization error
		}
		if decision != nil && decision.RequiresUserInput {
			// Prompt user for approval via the user interaction client
			var uiClient *actor.UserInteractionClient
			if t.userInteractionFunc != nil {
				uiClient = t.userInteractionFunc()
			}
			if uiClient != nil {
				tabID := 0
				if t.tabIDFunc != nil {
					tabID = t.tabIDFunc()
				}
				suggestedDomain := parsedURL.Host
				if decision.SuggestedCommandPrefix != "" {
					suggestedDomain = decision.SuggestedCommandPrefix
				}
				// Pause execution deadline while waiting for user input
				if t.deadline != nil {
					t.deadline.Pause()
				}
				resp, respErr := uiClient.RequestDomainAuthorization(
					t.interactionCtx(ctx),
					parsedURL.Host,
					decision.Reason,
					suggestedDomain,
					tabID,
				)
				// Resume execution deadline after user responds
				if t.deadline != nil {
					t.deadline.Resume()
				}
				if respErr != nil || resp == nil || resp.TimedOut || resp.Cancelled || !resp.Approved {
					return 403 // Forbidden - user denied or error
				}
				// User approved — persist the authorization
				if t.session != nil {
					t.session.AuthorizeDomain(parsedURL.Host)
				}
				if t.authConfig.Config != nil && !t.authConfig.Config.IsDomainAuthorized(parsedURL.Host) {
					t.authConfig.Config.AuthorizeDomain(parsedURL.Host)
					if saveErr := t.authConfig.Config.Save(t.authConfig.ConfigPath); saveErr != nil {
						logger.Warn("Failed to persist authorized domain %q: %v", parsedURL.Host, saveErr)
					}
				}
			} else {
				return 403 // Forbidden - no approval mechanism available
			}
		} else if decision == nil || !decision.Allowed {
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

	// Scan request for secrets (if feature flag is enabled)
	if t.featureFlags != nil && t.featureFlags.IsToolEnabled("web_fetch_secret_detect") && t.detector != nil {
		// Scan URL for secrets
		urlMatches := t.detector.Scan(urlStr)
		// Scan body for secrets
		var bodyMatches []secretdetect.SecretMatch
		if len(bodyBytes) > 0 {
			bodyMatches = t.detector.Scan(string(bodyBytes))
		}

		// Combine all matches
		allMatches := append(urlMatches, bodyMatches...)

		if len(allMatches) > 0 {
			// Log secret detection warning
			logger.Debug("sandbox fetch: detected %d potential secrets in request", len(allMatches))
			// In the sandbox, we log the warning but still allow the request to proceed
			// The warning will be visible in debug logs
			for _, match := range allMatches {
				logger.Debug("sandbox fetch: secret detected - %s: %s", match.PatternName, match.MatchedText)
			}
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
		Timeout: consts.Timeout30Seconds,
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
		respBody = respBody[:responseCap]
	}

	if !memory.Write(responsePtr, respBody) {
		return 500 // Internal server error - failed to write response
	}

	// Return HTTP status code
	return uint32(resp.StatusCode)
}
