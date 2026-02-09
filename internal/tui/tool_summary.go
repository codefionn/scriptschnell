package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codefionn/scriptschnell/internal/tools"
)

// ToolSummaryGenerator creates one-line summaries for tool calls
type ToolSummaryGenerator struct {
	paramsRenderer *ParamsRenderer
}

// NewToolSummaryGenerator creates a new summary generator
func NewToolSummaryGenerator() *ToolSummaryGenerator {
	return &ToolSummaryGenerator{
		paramsRenderer: NewParamsRenderer(),
	}
}

// GenerateSummary creates a one-line summary for a tool call
// Format: "action target (details)" or just "action target"
func (g *ToolSummaryGenerator) GenerateSummary(toolName string, params map[string]interface{}, state ToolState) string {
	toolType := GetToolTypeFromName(toolName)
	icon := GetIconForToolType(toolType)
	stateIndicator := GetStateIndicator(state)

	// Get tool-specific summary
	summary := g.generateToolSpecificSummary(toolName, toolType, params)

	// Build final summary line
	if summary != "" {
		return fmt.Sprintf("%s %s %s %s", stateIndicator, icon, toolName, summary)
	}
	return fmt.Sprintf("%s %s %s", stateIndicator, icon, toolName)
}

// GenerateCompactSummary creates a very compact one-line summary for headers
func (g *ToolSummaryGenerator) GenerateCompactSummary(toolName string, params map[string]interface{}) string {
	toolType := GetToolTypeFromName(toolName)

	// Get primary parameter value
	primaryParam, primaryValue := g.getPrimaryParamDisplay(toolName, toolType, params)

	if primaryValue != "" {
		if primaryParam != "" {
			return fmt.Sprintf("%s: %s", primaryParam, primaryValue)
		}
		return primaryValue
	}

	// Count parameters
	if len(params) > 0 {
		return fmt.Sprintf("(%d params)", len(params))
	}

	return ""
}

// generateToolSpecificSummary creates a summary based on tool type
func (g *ToolSummaryGenerator) generateToolSpecificSummary(toolName string, toolType ToolType, params map[string]interface{}) string {
	switch toolName {
	case tools.ToolNameReadFile:
		return g.formatFileReadSummary(params)
	case tools.ToolNameCreateFile:
		return g.formatFileCreateSummary(params)
	case tools.ToolNameEditFile:
		return g.formatFileEditSummary(params)
	case tools.ToolNameShell:
		return g.formatShellSummary(params)
	case tools.ToolNameGoSandbox:
		return g.formatSandboxSummary(params)
	case tools.ToolNameWebSearch:
		return g.formatWebSearchSummary(params)
	case tools.ToolNameWebFetch:
		return g.formatWebFetchSummary(params)
	case tools.ToolNameTodo:
		return g.formatTodoSummary(params)
	case tools.ToolNameParallel:
		return g.formatParallelSummary(params)
	default:
		return g.formatGenericSummary(toolType, params)
	}
}

// getPrimaryParamDisplay returns the primary parameter name and its display value
func (g *ToolSummaryGenerator) getPrimaryParamDisplay(toolName string, toolType ToolType, params map[string]interface{}) (string, string) {
	// Handle by tool type
	switch toolType {
	case ToolTypeReadFile, ToolTypeCreateFile, ToolTypeEditFile, ToolTypeReplaceFile:
		if path, ok := params["path"].(string); ok {
			return "path", g.shortenPath(path)
		}
	case ToolTypeShell:
		if cmd, ok := params["command"].([]interface{}); ok && len(cmd) > 0 {
			if firstCmd, ok := cmd[0].(string); ok {
				return "cmd", firstCmd
			}
		}
	case ToolTypeWebSearch:
		if queries, ok := params["queries"].([]interface{}); ok && len(queries) > 0 {
			if firstQ, ok := queries[0].(string); ok {
				return "query", g.truncateString(firstQ, 40)
			}
		}
	case ToolTypeWebFetch:
		if url, ok := params["url"].(string); ok {
			return "url", g.shortenURL(url)
		}
	case ToolTypeGoSandbox:
		if desc, ok := params["description"].(string); ok && desc != "" {
			return "desc", g.truncateString(desc, 40)
		}
	}

	return "", ""
}

// Summary formatters for specific tools

func (g *ToolSummaryGenerator) formatFileReadSummary(params map[string]interface{}) string {
	path, _ := params["path"].(string)
	if path == "" {
		return "reading file..."
	}

	var details []string

	// Add line range if specified
	if fromLine, ok := params["from_line"].(float64); ok {
		if toLine, ok := params["to_line"].(float64); ok {
			details = append(details, fmt.Sprintf("lines %d-%d", int(fromLine), int(toLine)))
		} else {
			details = append(details, fmt.Sprintf("from line %d", int(fromLine)))
		}
	}

	shortPath := g.shortenPath(path)
	if len(details) > 0 {
		return fmt.Sprintf("read `%s` (%s)", shortPath, strings.Join(details, ", "))
	}
	return fmt.Sprintf("read `%s`", shortPath)
}

func (g *ToolSummaryGenerator) formatFileCreateSummary(params map[string]interface{}) string {
	path, _ := params["path"].(string)
	if path == "" {
		return "creating file..."
	}

	// Try to get content length
	contentLen := 0
	if content, ok := params["content"].(string); ok {
		contentLen = len(content)
	}

	shortPath := g.shortenPath(path)
	if contentLen > 0 {
		return fmt.Sprintf("create `%s` (%d chars)", shortPath, contentLen)
	}
	return fmt.Sprintf("create `%s`", shortPath)
}

func (g *ToolSummaryGenerator) formatFileEditSummary(params map[string]interface{}) string {
	path, _ := params["path"].(string)
	if path == "" {
		return "editing file..."
	}

	var details []string

	// Count edits
	if edits, ok := params["edits"].([]interface{}); ok {
		details = append(details, fmt.Sprintf("%d edits", len(edits)))
	}

	shortPath := g.shortenPath(path)
	if len(details) > 0 {
		return fmt.Sprintf("edit `%s` (%s)", shortPath, strings.Join(details, ", "))
	}
	return fmt.Sprintf("edit `%s`", shortPath)
}

func (g *ToolSummaryGenerator) formatShellSummary(params map[string]interface{}) string {
	cmd, _ := params["command"].([]interface{})
	if len(cmd) == 0 {
		return "running command..."
	}

	// Get first command part
	var cmdStr string
	if first, ok := cmd[0].(string); ok {
		cmdStr = first
	}

	// Build short command display
	var args string
	if len(cmd) > 1 {
		if second, ok := cmd[1].(string); ok {
			args = g.truncateString(second, 20)
		}
		if len(cmd) > 2 {
			args += "..."
		}
	}

	if args != "" {
		return fmt.Sprintf("`%s %s`", cmdStr, args)
	}
	return fmt.Sprintf("`%s`", cmdStr)
}

func (g *ToolSummaryGenerator) formatSandboxSummary(params map[string]interface{}) string {
	desc, _ := params["description"].(string)
	if desc != "" {
		return fmt.Sprintf("Go: %s", g.truncateString(desc, 50))
	}

	// Check for function calls in code
	if code, ok := params["code"].(string); ok {
		funcCalls := g.extractFunctionCalls(code)
		if len(funcCalls) > 0 {
			return fmt.Sprintf("Go: uses %s", strings.Join(funcCalls[:minInt(3, len(funcCalls))], ", "))
		}
	}

	return "executing Go code..."
}

func (g *ToolSummaryGenerator) formatWebSearchSummary(params map[string]interface{}) string {
	queries, _ := params["queries"].([]interface{})
	if len(queries) == 0 {
		return "searching web..."
	}

	var queryStrs []string
	for i, q := range queries {
		if i >= 3 {
			queryStrs = append(queryStrs, "...")
			break
		}
		if qs, ok := q.(string); ok {
			queryStrs = append(queryStrs, fmt.Sprintf("\"%s\"", g.truncateString(qs, 30)))
		}
	}

	return fmt.Sprintf("search %s", strings.Join(queryStrs, ", "))
}

func (g *ToolSummaryGenerator) formatWebFetchSummary(params map[string]interface{}) string {
	url, _ := params["url"].(string)
	if url == "" {
		return "fetching URL..."
	}

	return fmt.Sprintf("fetch `%s`", g.shortenURL(url))
}

func (g *ToolSummaryGenerator) formatTodoSummary(params map[string]interface{}) string {
	action, _ := params["action"].(string)
	if action == "" {
		return "managing todos..."
	}

	switch action {
	case "list":
		return "listing todos"
	case "add":
		if text, ok := params["text"].(string); ok {
			return fmt.Sprintf("add: %s", g.truncateString(text, 40))
		}
		return "adding todo"
	case "add_many":
		if todos, ok := params["todos"].([]interface{}); ok {
			return fmt.Sprintf("add %d todos", len(todos))
		}
		return "adding todos"
	case "check":
		if id, ok := params["id"].(string); ok {
			return fmt.Sprintf("check %s", id)
		}
		return "checking todo"
	case "uncheck":
		if id, ok := params["id"].(string); ok {
			return fmt.Sprintf("uncheck %s", id)
		}
		return "unchecking todo"
	case "set_status":
		id, _ := params["id"].(string)
		status, _ := params["status"].(string)
		if id != "" && status != "" {
			return fmt.Sprintf("%s → %s", id, status)
		}
		return "setting status"
	case "delete":
		if id, ok := params["id"].(string); ok {
			return fmt.Sprintf("delete %s", id)
		}
		return "deleting todo"
	case "clear":
		return "clearing all todos"
	default:
		return fmt.Sprintf("todo: %s", action)
	}
}

func (g *ToolSummaryGenerator) formatParallelSummary(params map[string]interface{}) string {
	calls, _ := params["tool_calls"].([]interface{})
	if len(calls) == 0 {
		return "parallel execution..."
	}

	return fmt.Sprintf("parallel: %d tools", len(calls))
}

func (g *ToolSummaryGenerator) formatGenericSummary(toolType ToolType, params map[string]interface{}) string {
	// Try to find a meaningful primary parameter
	for _, key := range []string{"path", "url", "query", "command", "message", "text", "name"} {
		if val, ok := params[key]; ok {
			strVal := fmt.Sprintf("%v", val)
			if len(strVal) > 50 {
				strVal = strVal[:47] + "..."
			}
			return fmt.Sprintf("%s: %s", key, strVal)
		}
	}

	return ""
}

// Helper functions

func (g *ToolSummaryGenerator) shortenPath(path string) string {
	if len(path) <= 40 {
		return path
	}

	// Get filename
	filename := filepath.Base(path)
	dir := filepath.Dir(path)

	// If filename is long enough, just show it
	if len(filename) >= 35 {
		return "..." + filename[len(filename)-35:]
	} else if len(filename) >= 30 {
		return "..." + filename
	}

	// Show shortened dir + filename
	if len(dir) > 20 {
		dir = "..." + dir[len(dir)-17:]
	}

	return filepath.Join(dir, filename)
}

func (g *ToolSummaryGenerator) shortenURL(url string) string {
	if len(url) <= 50 {
		return url
	}

	// Try to keep the domain and path start
	parts := strings.SplitN(url, "/", 4)
	if len(parts) >= 3 {
		domain := parts[2]
		if len(parts) > 3 {
			path := parts[3]
			if len(path) > 20 {
				path = path[:17] + "..."
			}
			return fmt.Sprintf("...%s/%s", domain, path)
		}
		return "..." + domain
	}

	return url[:47] + "..."
}

func (g *ToolSummaryGenerator) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func (g *ToolSummaryGenerator) extractFunctionCalls(code string) []string {
	// Simple extraction of function names used in the code
	var funcs []string

	// Look for ExecuteCommand, Fetch, ReadFile, WriteFile, etc.
	hostFuncs := []string{"ExecuteCommand", "Fetch", "ReadFile", "WriteFile", "CreateFile", "RemoveFile", "Mkdir", "Move", "GrepFile", "ListFiles", "Summarize", "ConvertHTML"}

	for _, fn := range hostFuncs {
		if strings.Contains(code, fn+"(") {
			funcs = append(funcs, fn)
		}
	}

	return funcs
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
