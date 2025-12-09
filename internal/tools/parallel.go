package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/progress"
)

type ParallelTool struct {
	registry *Registry
}

func NewParallelTool(registry *Registry) *ParallelTool {
	return &ParallelTool{registry: registry}
}

func (t *ParallelTool) Name() string {
	return ToolNameParallel
}

func (t *ParallelTool) Description() string {
	return `Execute multiple tools concurrently for faster operation. Use cases:
- Read multiple files at once (parallel read_file calls)
- Search multiple patterns simultaneously (parallel search_file_content with different patterns)
- Search across different directories (parallel search_file_content with different directory parameters)
- Mix operations (combine read_file and search operations in one parallel call)
- Edit multiple files at once
- Investigate different parts of the codebase simultaneously with codebase investigator
Each tool runs independently and results are collected when all complete.`
}

func (t *ParallelTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tool_calls": map[string]interface{}{
				"type":        "array",
				"description": "List of tool invocations to execute in parallel. Example: [{\"name\": \"read_file\", \"parameters\": {\"path\": \"file1.go\"}}, {\"name\": \"read_file\", \"parameters\": {\"path\": \"file2.go\"}}]",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Name of the registered tool to execute.",
						},
						"parameters": map[string]interface{}{
							"type":                 "object",
							"description":          "Parameters to pass to the tool (optional).",
							"additionalProperties": true,
						},
					},
					"required": []string{"name"},
				},
			},
		},
		"required": []string{"tool_calls"},
	}
}

func (t *ParallelTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	if t.registry == nil {
		return &ToolResult{Error: "parallel tool registry is not configured"}
	}

	rawCalls, ok := params["tool_calls"]
	if !ok {
		return &ToolResult{Error: "tool_calls is required"}
	}

	callSlice, ok := rawCalls.([]interface{})
	if !ok {
		return &ToolResult{Error: "tool_calls must be an array"}
	}

	if len(callSlice) == 0 {
		return &ToolResult{
			Result: map[string]interface{}{
				"results":     []map[string]interface{}{},
				"duration_ms": int64(0),
			},
		}
	}

	type callSpec struct {
		index  int
		name   string
		params map[string]interface{}
	}

	parsedCalls := make([]callSpec, 0, len(callSlice))
	for i, raw := range callSlice {
		callMap, ok := raw.(map[string]interface{})
		if !ok {
			return &ToolResult{Error: fmt.Sprintf("tool_calls[%d] must be an object", i)}
		}

		nameVal, ok := callMap["name"].(string)
		if !ok || nameVal == "" {
			return &ToolResult{Error: fmt.Sprintf("tool_calls[%d].name must be a non-empty string", i)}
		}

		paramsVal := map[string]interface{}{}
		if rawParams, exists := callMap["parameters"]; exists && rawParams != nil {
			castParams, ok := rawParams.(map[string]interface{})
			if !ok {
				return &ToolResult{Error: fmt.Sprintf("tool_calls[%d].parameters must be an object", i)}
			}
			paramsVal = castParams
		}

		parsedCalls = append(parsedCalls, callSpec{
			index:  i,
			name:   nameVal,
			params: paramsVal,
		})
	}

	totalStart := time.Now()
	results := make([]map[string]interface{}, len(parsedCalls))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, call := range parsedCalls {
		wg.Add(1)
		go func(call callSpec) {
			defer wg.Done()
			start := time.Now()

			result := map[string]interface{}{
				"index": call.index,
				"tool":  call.name,
			}

			select {
			case <-ctx.Done():
				result["error"] = ctx.Err().Error()
				result["duration_ms"] = time.Since(start).Milliseconds()
				mu.Lock()
				results[call.index] = result
				mu.Unlock()
				return
			default:
			}

			toolResult := t.registry.Execute(ctx, &ToolCall{
				ID:         fmt.Sprintf("parallel_%d", call.index),
				Name:       call.name,
				Parameters: call.params,
			})

			if toolResult.Error != "" {
				result["error"] = toolResult.Error
			} else {
				result["result"] = toolResult.Result
			}

			result["duration_ms"] = time.Since(start).Milliseconds()

			mu.Lock()
			results[call.index] = result
			mu.Unlock()
		}(call)
	}

	wg.Wait()

	elapsed := time.Since(totalStart).Milliseconds()

	return &ToolResult{Result: map[string]interface{}{
		"results":     results,
		"duration_ms": elapsed,
	}}
}

// ExecuteWithCallbacks implements the callback-aware execution interface
func (t *ParallelTool) ExecuteWithCallbacks(ctx context.Context, params map[string]interface{}, progressCb progress.Callback, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error) *ToolResult {
	if t.registry == nil {
		return &ToolResult{Error: "parallel tool registry is not configured"}
	}

	rawCalls, ok := params["tool_calls"]
	if !ok {
		return &ToolResult{Error: "tool_calls is required"}
	}

	callSlice, ok := rawCalls.([]interface{})
	if !ok {
		return &ToolResult{Error: "tool_calls must be an array"}
	}

	if len(callSlice) == 0 {
		return &ToolResult{
			Result: map[string]interface{}{
				"results":     []map[string]interface{}{},
				"duration_ms": int64(0),
			},
		}
	}

	type callSpec struct {
		index  int
		name   string
		params map[string]interface{}
	}

	parsedCalls := make([]callSpec, 0, len(callSlice))
	for i, raw := range callSlice {
		callMap, ok := raw.(map[string]interface{})
		if !ok {
			return &ToolResult{Error: fmt.Sprintf("tool_calls[%d] must be an object", i)}
		}

		nameVal, ok := callMap["name"].(string)
		if !ok || nameVal == "" {
			return &ToolResult{Error: fmt.Sprintf("tool_calls[%d].name must be a non-empty string", i)}
		}

		paramsVal := map[string]interface{}{}
		if rawParams, exists := callMap["parameters"]; exists && rawParams != nil {
			castParams, ok := rawParams.(map[string]interface{})
			if !ok {
				return &ToolResult{Error: fmt.Sprintf("tool_calls[%d].parameters must be an object", i)}
			}
			paramsVal = castParams
		}

		parsedCalls = append(parsedCalls, callSpec{
			index:  i,
			name:   nameVal,
			params: paramsVal,
		})
	}

	// Send initial progress message showing all tools being executed
	sendStream := func(msg string) {
		if err := progress.Dispatch(progressCb, progress.Update{
			Message: msg,
			Mode:    progress.ReportNoStatus,
		}); err != nil {
			// Ignore progress dispatch errors to avoid interrupting parallel execution
			return
		}
	}

	// Show which tools are being executed in parallel
	toolNames := make([]string, len(parsedCalls))
	for i, call := range parsedCalls {
		details := extractParallelToolDetails(call.name, call.params)
		if details != "" {
			toolNames[i] = fmt.Sprintf("%s(%s)", call.name, details)
		} else {
			toolNames[i] = call.name
		}
	}
	if len(toolNames) > 0 {
		sendStream(fmt.Sprintf("→ **parallel_tools** [%d]: %s\n", len(toolNames), joinToolNames(toolNames)))
	}

	totalStart := time.Now()
	results := make([]map[string]interface{}, len(parsedCalls))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, call := range parsedCalls {
		wg.Add(1)
		go func(call callSpec) {
			defer wg.Done()
			start := time.Now()

			result := map[string]interface{}{
				"index": call.index,
				"tool":  call.name,
			}

			select {
			case <-ctx.Done():
				result["error"] = ctx.Err().Error()
				result["duration_ms"] = time.Since(start).Milliseconds()
				mu.Lock()
				results[call.index] = result
				mu.Unlock()
				return
			default:
			}

			toolResult := t.registry.Execute(ctx, &ToolCall{
				ID:         fmt.Sprintf("parallel_%d", call.index),
				Name:       call.name,
				Parameters: call.params,
			})

			if toolResult.Error != "" {
				result["error"] = toolResult.Error
			} else {
				result["result"] = toolResult.Result
			}

			result["duration_ms"] = time.Since(start).Milliseconds()

			mu.Lock()
			results[call.index] = result
			mu.Unlock()
		}(call)
	}

	wg.Wait()

	elapsed := time.Since(totalStart).Milliseconds()

	return &ToolResult{Result: map[string]interface{}{
		"results":     results,
		"duration_ms": elapsed,
	}}
}

// extractParallelToolDetails extracts concise details from tool parameters for parallel execution display
func extractParallelToolDetails(toolName string, params map[string]interface{}) string {
	switch toolName {
	case "read_file":
		if path, ok := params["path"].(string); ok {
			return filepath.Base(path)
		}
	case "search_files":
		if pattern, ok := params["pattern"].(string); ok {
			return pattern
		}
	case "search_file_content":
		if pattern, ok := params["pattern"].(string); ok {
			return pattern
		}
	case "codebase_investigator":
		if objective, ok := params["objective"].(string); ok {
			// Truncate long objectives
			if len(objective) > 40 {
				return objective[:37] + "..."
			}
			return objective
		}
	}
	return ""
}

// joinToolNames joins tool names with proper formatting
func joinToolNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	if len(names) == 2 {
		return names[0] + ", " + names[1]
	}
	// For more than 2, show first few and count
	const maxShow = 3
	if len(names) <= maxShow {
		result := ""
		for i, name := range names {
			if i > 0 {
				result += ", "
			}
			result += name
		}
		return result
	}
	// Show first maxShow and indicate there are more
	result := ""
	for i := 0; i < maxShow; i++ {
		if i > 0 {
			result += ", "
		}
		result += names[i]
	}
	return fmt.Sprintf("%s, +%d more", result, len(names)-maxShow)
}
