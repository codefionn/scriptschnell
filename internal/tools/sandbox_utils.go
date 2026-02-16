package tools

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// sandboxCall tracks individual sandbox function calls for metadata
type sandboxCall struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// sandboxCallTracker tracks sandbox function calls for execution metadata
type sandboxCallTracker struct {
	mu    sync.Mutex
	calls []sandboxCall
}

// newSandboxCallTracker creates a new call tracker instance
func newSandboxCallTracker() *sandboxCallTracker {
	return &sandboxCallTracker{
		calls: make([]sandboxCall, 0),
	}
}

// record records a function call with optional details
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

// metadataDetails returns the call details as metadata
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

// buildSandboxMetadata creates execution metadata for sandbox runs
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

	// Add adaptive timeout statistics if available
	if adaptiveDeadline, ok := t.deadline.(*adaptiveExecDeadline); ok {
		stats := adaptiveDeadline.GetStats()
		metadata.AdaptiveTimeoutOriginalSeconds = int(stats["original_timeout_seconds"].(float64))
		metadata.AdaptiveTimeoutExtensions = stats["extensions"].(int)
		metadata.AdaptiveTimeoutTotalSeconds = stats["total_timeout_seconds"].(float64)
		metadata.AdaptiveTimeoutMaxExtensions = stats["max_extensions"].(int)

		// Log adaptive timeout summary
		logger.Debug("sandbox: adaptive timeout summary - original: %ds, extensions: %d, total: %.1fs, max: %d",
			metadata.AdaptiveTimeoutOriginalSeconds,
			metadata.AdaptiveTimeoutExtensions,
			metadata.AdaptiveTimeoutTotalSeconds,
			metadata.AdaptiveTimeoutMaxExtensions)
	}

	return metadata
}

// attachExecutionMetadata attaches metadata to a result map
func attachExecutionMetadata(result map[string]interface{}, metadata *ExecutionMetadata) map[string]interface{} {
	if metadata == nil {
		return result
	}
	result["_execution_metadata"] = metadata
	return result
}

// formatSandboxUIResult builds a human-readable UI string from sandbox execution output
func formatSandboxUIResult(result map[string]interface{}) string {
	stdout := strings.TrimSpace(getStringValue(result, "stdout"))
	stderr := strings.TrimSpace(getStringValue(result, "stderr"))
	errorMsg := strings.TrimSpace(getStringValue(result, "error"))
	message := strings.TrimSpace(getStringValue(result, "message"))
	exitCode := coerceExitCode(result["exit_code"])
	timedOut := false
	if timeoutVal, ok := result["timeout"].(bool); ok {
		timedOut = timeoutVal
	}

	var sections []string

	// Apply 4096 line limit to stdout and stderr
	const maxLines = 4096

	if stdout != "" {
		truncatedStdout, stdoutTruncMsg := truncateToLines(stdout, maxLines)
		sections = append(sections, fmt.Sprintf("stdout:\n%s", truncatedStdout))
		if stdoutTruncMsg != "" {
			sections = append(sections, stdoutTruncMsg)
		}
	}
	if stderr != "" {
		truncatedStderr, stderrTruncMsg := truncateToLines(stderr, maxLines)
		sections = append(sections, fmt.Sprintf("stderr:\n%s", truncatedStderr))
		if stderrTruncMsg != "" {
			sections = append(sections, stderrTruncMsg)
		}
	}
	if errorMsg != "" {
		sections = append(sections, fmt.Sprintf("error: %s", errorMsg))
	}
	if timedOut {
		sections = append(sections, "timeout: execution timed out")
	}
	if message != "" {
		sections = append(sections, message)
	}

	// Always include exit code for clarity, especially when there is no other output
	if exitCode != 0 || len(sections) == 0 {
		sections = append(sections, fmt.Sprintf("exit code: %d", exitCode))
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

// getStringValue safely extracts a string value from a map
func getStringValue(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// CalculateOutputStats calculates byte and line statistics for output strings
func CalculateOutputStats(output string) (int, int) {
	if output == "" {
		return 0, 0
	}

	bytes := len(output)

	lines := 0
	if output != "" {
		lines = 1 // At least one line if content exists
		for _, char := range output {
			if char == '\n' {
				lines++
			}
		}
	}

	return bytes, lines
}

// truncateToLines limits text to a maximum number of lines.
// If truncated, returns the truncated text and a truncation message.
func truncateToLines(text string, maxLines int) (string, string) {
	if text == "" {
		return "", ""
	}

	// Count lines
	lineCount := 0
	for _, char := range text {
		if char == '\n' {
			lineCount++
		}
	}

	// Count the last line if it doesn't end with newline
	if len(text) > 0 && text[len(text)-1] != '\n' {
		lineCount++
	}

	// If under the limit, return as-is
	if lineCount <= maxLines {
		return text, ""
	}

	// Find the truncation point (end of maxLines-th line)
	currentLine := 0
	truncIndex := len(text) // default to end of string
	for i, char := range text {
		if char == '\n' {
			currentLine++
			if currentLine >= maxLines {
				truncIndex = i
				break
			}
		}
	}

	// If we didn't find enough newlines, truncate at maxLines character count
	if currentLine < maxLines {
		truncIndex = len(text)
	}

	truncated := text[:truncIndex]
	truncationMsg := fmt.Sprintf("\n\n[Output truncated: showed %d of %d lines. The full output is preserved in last_stdout/last_stderr variables for the next sandbox execution. Consider parsing specific parts with Go to reduce output.]", maxLines, lineCount)

	return truncated, truncationMsg
}
