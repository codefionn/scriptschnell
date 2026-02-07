package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/htmlconv"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// registerFetchHostFunction registers the fetch host function
func (t *SandboxTool) registerFetchHostFunction(envBuilder HostModuleBuilder, adapter *wasiAuthorizerAdapter, tracker *sandboxCallTracker) {
	// fetch(method_ptr, method_len, url_ptr, url_len, body_ptr, body_len, response_ptr, response_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen, responsePtr, responseCap uint32) uint32 {
			return t.executeFetch(ctx, adapter, tracker, m, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen, responsePtr, responseCap)
		}).
		Export("fetch")
}

// registerShellHostFunction registers the shell/execute command host function
func (t *SandboxTool) registerShellHostFunction(envBuilder HostModuleBuilder, adapter *wasiAuthorizerAdapter, tracker *sandboxCallTracker) {
	// shell(command_json_ptr, command_json_len, stdin_ptr, stdin_len, stdout_ptr, stdout_cap, stderr_ptr, stderr_cap) -> exit_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, cmdPtr, cmdLen, stdinPtr, stdinLen, stdoutPtr, stdoutCap, stderrPtr, stderrCap uint32) int32 {
			// writeStderr writes an error message to the stderr WASM buffer
			writeStderr := func(memory api.Memory, msg string) {
				msgBytes := []byte(msg)
				if uint32(len(msgBytes)) > stderrCap {
					msgBytes = msgBytes[:stderrCap]
				}
				if stderrCap > 0 {
					memory.Write(stderrPtr, msgBytes)
				}
			}

			// Read command from WASM memory
			memory := m.Memory()
			cmdBytes, ok := memory.Read(cmdPtr, cmdLen)
			if !ok {
				writeStderr(memory, "Error: failed to read command from memory")
				return -1
			}
			var commandArgs []string
			if err := json.Unmarshal(cmdBytes, &commandArgs); err != nil {
				writeStderr(memory, fmt.Sprintf("Error: failed to parse command JSON: %v", err))
				return -1
			}
			if len(commandArgs) == 0 {
				writeStderr(memory, "Error: command must include at least one argument")
				return -1
			}
			commandDisplay := strings.Join(commandArgs, " ")

			// Read stdin from WASM memory (if provided)
			var stdinData []byte
			if stdinLen > 0 {
				stdinData, ok = memory.Read(stdinPtr, stdinLen)
				if !ok {
					writeStderr(memory, "Error: failed to read stdin from memory")
					return -1
				}
			}

			// Check authorization for command
			if adapter != nil && adapter.authorizer != nil {
				decision, err := adapter.Authorize(ctx, ToolNameCommand, map[string]interface{}{
					"command":      commandDisplay,
					"command_args": commandArgs,
				})
				if err != nil {
					writeStderr(memory, fmt.Sprintf("Error: command authorization failed: %v", err))
					return -1
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
						// Pause execution deadline while waiting for user input
						if t.deadline != nil {
							t.deadline.Pause()
						}
						resp, err := uiClient.RequestAuthorization(
							t.interactionCtx(ctx),
							ToolNameCommand,
							map[string]interface{}{
								"command":      commandDisplay,
								"command_args": commandArgs,
							},
							decision.Reason,
							decision.SuggestedCommandPrefix,
							tabID,
						)
						// Resume execution deadline after user responds
						if t.deadline != nil {
							t.deadline.Resume()
						}
						if err != nil || resp == nil || resp.TimedOut || resp.Cancelled || !resp.Approved {
							reason := "not authorized"
							if decision.Reason != "" {
								reason = decision.Reason
							}
							if err != nil {
								reason = fmt.Sprintf("authorization error: %v", err)
							} else if resp != nil && resp.TimedOut {
								reason = "authorization timed out"
							} else if resp != nil && resp.Cancelled {
								reason = "authorization cancelled by user"
							}
							writeStderr(memory, fmt.Sprintf("Error: command '%s' was denied: %s", commandDisplay, reason))
							return -1
						}
						// User approved — persist the authorization
						prefix := decision.SuggestedCommandPrefix
						if prefix != "" {
							if t.session != nil {
								t.session.AuthorizeCommand(prefix)
							}
							if t.authConfig.Config != nil && !t.authConfig.Config.IsCommandAuthorized(prefix) {
								t.authConfig.Config.AuthorizeCommand(prefix)
								if err := t.authConfig.Config.Save(t.authConfig.ConfigPath); err != nil {
									logger.Warn("Failed to persist authorized command prefix %q: %v", prefix, err)
								}
							}
						}
					} else {
						reason := "not authorized"
						if decision.Reason != "" {
							reason = decision.Reason
						}
						writeStderr(memory, fmt.Sprintf("Error: command '%s' was denied: %s (no approval mechanism available)", commandDisplay, reason))
						return -1
					}
				} else if decision == nil || !decision.Allowed {
					reason := "not authorized"
					if decision != nil && decision.Reason != "" {
						reason = decision.Reason
					}
					writeStderr(memory, fmt.Sprintf("Error: command '%s' was denied: %s", commandDisplay, reason))
					return -1
				}
			}

			tracker.record("shell", commandDisplay)

			// Execute the command using the shell executor
			var stdoutStr, stderrStr string
			var exitCode int

			if t.shellExecutor != nil {
				// Use the shell executor (actor-based) with direct argv execution
				var err error
				stdoutStr, stderrStr, exitCode, err = t.shellExecutor.ExecuteCommand(ctx, commandArgs, "", consts.Timeout30Seconds, string(stdinData))
				// When the executor returns an error (e.g. command not found, timeout)
				// and stderr is empty, include the error message so the caller gets context
				if err != nil && stderrStr == "" {
					stderrStr = fmt.Sprintf("Error: %v", err)
				}
			} else {
				// Fallback to direct execution if no executor is set
				stdoutStr, stderrStr, exitCode = t.executeDirectCommand(ctx, commandArgs, stdinData)
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
		Export(ToolNameCommand)
}

// registerSummarizeHostFunction registers the summarize host function
func (t *SandboxTool) registerSummarizeHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
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

			tracker.record("summarize", fmt.Sprintf("prompt:%s", strings.TrimSpace(prompt)))

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
}

// registerReadFileHostFunction registers the read_file host function
func (t *SandboxTool) registerReadFileHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
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
			tracker.record("read_file", detail)

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
}

// registerCreateFileHostFunction registers the create_file host function
func (t *SandboxTool) registerCreateFileHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
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

			tracker.record("create_file", path)

			return 0 // Success
		}).
		Export("create_file")
}

// registerWriteFileHostFunction registers the write_file host function
func (t *SandboxTool) registerWriteFileHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
	// write_file(path_ptr, path_len, append_mode, content_ptr, content_len, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, pathPtr, pathLen uint32, appendMode int32, contentPtr, contentLen, resultPtr, resultCap uint32) int32 {
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

			var finalContent string
			if appendMode == 1 {
				// Append mode: read current content and append
				currentData, err := t.filesystem.ReadFile(ctx, path)
				if err != nil {
					errMsg := []byte(fmt.Sprintf("Error: Failed to read file: %v", err))
					if uint32(len(errMsg)) <= resultCap {
						memory.Write(resultPtr, errMsg)
					}
					return -5 // Error: read failed
				}
				finalContent = string(currentData) + content
			} else {
				// Overwrite mode: use content as-is
				finalContent = content
			}

			// Write file
			if err := t.filesystem.WriteFile(ctx, path, []byte(finalContent)); err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Failed to write file: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -6 // Error: write failed
			}

			// Track file modification in session
			if t.session != nil {
				t.session.TrackFileModified(path)
				t.session.TrackFileRead(path, finalContent)
			}

			mode := "write"
			if appendMode == 1 {
				mode = "append"
			}
			tracker.record("write_file", fmt.Sprintf("%s (%s)", path, mode))

			return 0 // Success
		}).
		Export("write_file")
}

// registerMkdirHostFunction registers the mkdir host function
func (t *SandboxTool) registerMkdirHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
	// mkdir(path_ptr, path_len, recursive, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, pathPtr, pathLen uint32, recursive int32, resultPtr, resultCap uint32) int32 {
			memory := m.Memory()

			pathBytes, ok := memory.Read(pathPtr, pathLen)
			if !ok {
				return -1
			}
			path := string(pathBytes)

			if t.filesystem == nil {
				errMsg := []byte("Error: Filesystem not available")
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -2
			}

			if path == "" {
				errMsg := []byte("Error: Path is required")
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -3
			}

			if recursive != 1 {
				parent := filepath.Dir(path)
				exists, err := t.filesystem.Exists(ctx, parent)
				if err != nil || !exists {
					errMsg := []byte(fmt.Sprintf("Error: Parent directory not found: %s", parent))
					if uint32(len(errMsg)) <= resultCap {
						memory.Write(resultPtr, errMsg)
					}
					return -4
				}
			}

			if err := t.filesystem.MkdirAll(ctx, path, 0755); err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Failed to create directory: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -5
			}

			if t.session != nil {
				t.session.TrackFileModified(path)
			}

			detail := path
			if recursive == 1 {
				detail = fmt.Sprintf("%s (recursive)", path)
			}
			tracker.record("mkdir", detail)

			return 0
		}).
		Export("mkdir")
}

// registerMoveHostFunction registers the move host function
func (t *SandboxTool) registerMoveHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
	// move(src_ptr, src_len, dst_ptr, dst_len, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, srcPtr, srcLen, dstPtr, dstLen, resultPtr, resultCap uint32) int32 {
			memory := m.Memory()

			srcBytes, ok := memory.Read(srcPtr, srcLen)
			if !ok {
				return -1
			}
			dstBytes, ok := memory.Read(dstPtr, dstLen)
			if !ok {
				return -1
			}

			src := string(srcBytes)
			dst := string(dstBytes)

			if t.filesystem == nil {
				errMsg := []byte("Error: Filesystem not available")
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -2
			}

			if src == "" || dst == "" {
				errMsg := []byte("Error: Source and destination are required")
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -3
			}

			info, err := t.filesystem.Stat(ctx, src)
			if err != nil || info == nil {
				errMsg := []byte(fmt.Sprintf("Error: Source not found: %s", src))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -4
			}

			if info.IsDir {
				cleanSrc := filepath.Clean(src)
				cleanDst := filepath.Clean(dst)
				if cleanDst == cleanSrc || strings.HasPrefix(cleanDst, cleanSrc+string(os.PathSeparator)) {
					errMsg := []byte("Error: Cannot move a directory into itself")
					if uint32(len(errMsg)) <= resultCap {
						memory.Write(resultPtr, errMsg)
					}
					return -5
				}
			} else if t.session != nil && !t.session.WasFileRead(src) {
				errMsg := []byte(fmt.Sprintf("Error: File %s was not read in this session", src))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -6
			}

			parent := filepath.Dir(dst)
			exists, err := t.filesystem.Exists(ctx, parent)
			if err != nil || !exists {
				errMsg := []byte(fmt.Sprintf("Error: Destination parent not found: %s", parent))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -7
			}

			if err := t.filesystem.Move(ctx, src, dst); err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Failed to move: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -8
			}

			if t.session != nil {
				t.session.TrackFileModified(src)
				t.session.TrackFileModified(dst)
			}

			tracker.record("move", fmt.Sprintf("%s -> %s", src, dst))

			return 0
		}).
		Export("move")
}

// registerListFilesHostFunction registers the list_files host function
func (t *SandboxTool) registerListFilesHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
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

			// Normalize requests for the current directory (e.g., ".", "./", "")
			if filepath.Clean(pattern) == "." {
				pattern = "." + string(os.PathSeparator) + "*"
			}

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

			tracker.record("list_files", pattern)

			return 0 // Success
		}).
		Export("list_files")
}

// registerRemoveFileHostFunction registers the remove_file host function
func (t *SandboxTool) registerRemoveFileHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
	// remove_file(path_ptr, path_len, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, pathPtr, pathLen, resultPtr, resultCap uint32) int32 {
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

			// Remove the file
			if err := t.filesystem.Delete(ctx, path); err != nil {
				errMsg := []byte(fmt.Sprintf("Error: Failed to remove file: %v", err))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -5 // Error: remove failed
			}

			// Track file modification in session
			if t.session != nil {
				t.session.TrackFileModified(path)
			}

			tracker.record("remove_file", path)

			return 0 // Success
		}).
		Export("remove_file")
}

// registerRemoveDirHostFunction registers the remove_dir host function
func (t *SandboxTool) registerRemoveDirHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
	// remove_dir(path_ptr, path_len, recursive, result_ptr, result_cap) -> status_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, pathPtr, pathLen uint32, recursive int32, resultPtr, resultCap uint32) int32 {
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
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -2 // Error: no filesystem
			}

			// Check if directory exists
			exists, err := t.filesystem.Exists(ctx, path)
			if err != nil || !exists {
				errMsg := []byte(fmt.Sprintf("Error: Directory not found: %s", path))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -3 // Error: directory not found
			}

			// Determine the appropriate removal method
			var removeErr error
			if recursive == 1 {
				// Recursive removal
				removeErr = t.filesystem.DeleteAll(ctx, path)
			} else {
				// Non-recursive removal (only empty directories)
				removeErr = t.filesystem.Delete(ctx, path)
			}

			if removeErr != nil {
				errMsg := []byte(fmt.Sprintf("Error: Failed to remove directory: %v", removeErr))
				if uint32(len(errMsg)) <= resultCap {
					memory.Write(resultPtr, errMsg)
				}
				return -4 // Error: remove failed
			}

			// Track directory modification in session
			if t.session != nil {
				t.session.TrackFileModified(path)
			}

			detail := path
			if recursive == 1 {
				detail = fmt.Sprintf("%s (recursive)", path)
			}
			tracker.record("remove_dir", detail)

			return 0 // Success
		}).
		Export("remove_dir")
}

// registerGetLastExitCodeHostFunction registers the get_last_exit_code host function
func (t *SandboxTool) registerGetLastExitCodeHostFunction(envBuilder HostModuleBuilder) {
	// get_last_exit_code() -> exit_code
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module) int32 {
			if t.session == nil {
				return 0 // No session, return 0
			}

			exitCode, _, _ := t.session.GetLastSandboxOutput()
			return int32(exitCode)
		}).
		Export("get_last_exit_code")
}

// registerGetLastStdoutHostFunction registers the get_last_stdout host function
func (t *SandboxTool) registerGetLastStdoutHostFunction(envBuilder HostModuleBuilder) {
	// get_last_stdout(buffer_ptr, buffer_cap) -> length
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, bufferPtr, bufferCap uint32) int32 {
			if t.session == nil {
				return 0 // No session, return empty
			}

			_, stdout, _ := t.session.GetLastSandboxOutput()
			stdoutBytes := []byte(stdout)

			memory := m.Memory()
			if uint32(len(stdoutBytes)) > bufferCap {
				stdoutBytes = stdoutBytes[:bufferCap]
			}

			if bufferCap > 0 && len(stdoutBytes) > 0 {
				memory.Write(bufferPtr, stdoutBytes)
			}

			return int32(len(stdoutBytes))
		}).
		Export("get_last_stdout")
}

// registerGetLastStderrHostFunction registers the get_last_stderr host function
func (t *SandboxTool) registerGetLastStderrHostFunction(envBuilder HostModuleBuilder) {
	// get_last_stderr(buffer_ptr, buffer_cap) -> length
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, bufferPtr, bufferCap uint32) int32 {
			if t.session == nil {
				return 0 // No session, return empty
			}

			_, _, stderr := t.session.GetLastSandboxOutput()
			stderrBytes := []byte(stderr)

			memory := m.Memory()
			if uint32(len(stderrBytes)) > bufferCap {
				stderrBytes = stderrBytes[:bufferCap]
			}

			if bufferCap > 0 && len(stderrBytes) > 0 {
				memory.Write(bufferPtr, stderrBytes)
			}

			return int32(len(stderrBytes))
		}).
		Export("get_last_stderr")
}

// registerConvertHTMLHostFunction registers the convert_html host function
func (t *SandboxTool) registerConvertHTMLHostFunction(envBuilder HostModuleBuilder, tracker *sandboxCallTracker) {
	// convert_html(html_ptr, html_len, markdown_ptr, markdown_cap) -> int32
	envBuilder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, htmlPtr, htmlLen, markdownPtr, markdownCap uint32) int32 {
			memory := m.Memory()

			// Read HTML from WASM memory
			htmlBytes, ok := memory.Read(htmlPtr, htmlLen)
			if !ok {
				return -1 // Error: invalid memory access
			}
			html := string(htmlBytes)

			// Perform conversion using htmlconv package
			markdown, _ := htmlconv.ConvertIfHTML(html)
			// Note: We ignore the 'converted' boolean because we always want to return
			// the result (either converted markdown or original text if not HTML)

			// Track operation
			tracker.record("convert_html", fmt.Sprintf("%d bytes -> %d bytes", htmlLen, len(markdown)))

			// Write result to WASM memory
			markdownBytes := []byte(markdown)
			if uint32(len(markdownBytes)) > markdownCap {
				markdownBytes = markdownBytes[:markdownCap]
			}
			if markdownCap > 0 && len(markdownBytes) > 0 {
				memory.Write(markdownPtr, markdownBytes)
			}

			return 0 // Success
		}).
		Export("convert_html")
}
