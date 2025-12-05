package tools

import (
	"fmt"
	"strings"
	"sync"
	"time"
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

	if stdout != "" {
		sections = append(sections, fmt.Sprintf("stdout:\n%s", stdout))
	}
	if stderr != "" {
		sections = append(sections, fmt.Sprintf("stderr:\n%s", stderr))
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
