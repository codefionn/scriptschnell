package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/statcode-ai/statcode-ai/internal/llm"
)

// ToolSummarizeTool wraps another tool call and summarizes its output using LLM
type ToolSummarizeTool struct {
	registry        *Registry
	summarizeClient llm.Client
}

func NewToolSummarizeTool(registry *Registry, summarizeClient llm.Client) *ToolSummarizeTool {
	return &ToolSummarizeTool{
		registry:        registry,
		summarizeClient: summarizeClient,
	}
}

func (t *ToolSummarizeTool) Name() string {
	return ToolNameToolSummarize
}

func (t *ToolSummarizeTool) Description() string {
	return `Execute another tool and summarize its output based on a specific goal.

This is useful when:
- A tool returns large amounts of data but you only need specific information
- You want to extract key points from tool output
- You need to answer a specific question using tool results

Examples:
- Execute read_file and summarize "what authentication method is used?"
- Execute shell command (in sandbox) and summarize "how many errors and what errors occurred?"
- Execute a build/lint or test tool and summarize "what failed specifically?"
- Execute read_file and summarize "list all exported functions"

The tool will:
1. Execute the specified tool with provided arguments
2. Use the summarization LLM to extract relevant information based on your goal
3. Return only the summarized information`
}

func (t *ToolSummarizeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tool_name": map[string]interface{}{
				"type":        "string",
				"description": "The name of the tool to execute (e.g., 'read_file', 'shell', 'read_file_summarized')",
			},
			"tool_args": map[string]interface{}{
				"type":        "object",
				"description": "The arguments to pass to the tool (as a JSON object)",
			},
			"summary_goal": map[string]interface{}{
				"type":        "string",
				"description": "What you want to extract or learn from the tool output. Be specific (e.g., 'List all function names', 'What is the main error?', 'How many tests failed?')",
			},
		},
		"required": []string{"tool_name", "tool_args", "summary_goal"},
	}
}

func (t *ToolSummarizeTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	// Extract parameters
	toolName := GetStringParam(args, "tool_name", "")
	if toolName == "" {
		return &ToolResult{Error: "tool_name is required"}
	}

	toolArgs, ok := args["tool_args"].(map[string]interface{})
	if !ok {
		return &ToolResult{Error: "tool_args must be an object"}
	}

	summaryGoal := GetStringParam(args, "summary_goal", "")
	if summaryGoal == "" {
		return &ToolResult{Error: "summary_goal is required"}
	}

	// Get the tool from registry
	tool, ok := t.registry.Get(toolName)
	if !ok {
		return &ToolResult{Error: fmt.Sprintf("tool not found: %s", toolName)}
	}

	// Execute the tool
	result := tool.Execute(ctx, toolArgs)
	if result == nil {
		return &ToolResult{Error: "tool returned nil result"}
	}

	if result.Error != "" {
		return &ToolResult{Error: fmt.Sprintf("tool execution failed: %s", result.Error)}
	}

	// Convert result to string for summarization
	resultStr := fmt.Sprintf("%v", result.Result)

	// Check if summarization client is available
	if t.summarizeClient == nil {
		// If no summarization client, just return the raw result
		return &ToolResult{Result: fmt.Sprintf("Tool output (no summarization available):\n\n%s", resultStr)}
	}

	// Build summarization prompt
	prompt := fmt.Sprintf(`You are analyzing the output of a tool execution. The user wants specific information extracted.

Tool executed: %s
Tool arguments: %s
User's goal: %s

Tool output:
%s

Based on the user's goal, extract and return ONLY the relevant information. Be concise and direct. If the information isn't in the output, say so clearly.`,
		toolName,
		formatArgs(toolArgs),
		summaryGoal,
		resultStr,
	)

	// Summarize using LLM
	summary, err := t.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		// If summarization fails, return raw result with error note
		return &ToolResult{Result: fmt.Sprintf("Note: Summarization failed (%v)\n\nRaw tool output:\n%s", err, resultStr)}
	}

	return &ToolResult{Result: summary}
}

// formatArgs formats tool arguments for display
func formatArgs(args map[string]interface{}) string {
	bytes, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", args)
	}
	return string(bytes)
}
