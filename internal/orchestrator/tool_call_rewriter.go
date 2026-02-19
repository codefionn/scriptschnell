package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/features"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// ToolCallRewriter rewrites invalid tool calls using the summarization model.
type ToolCallRewriter struct {
	summarizeClient llm.Client
	toolRegistry    *tools.Registry
	enabled         bool
}

// NewToolCallRewriter creates a new tool call rewriter.
func NewToolCallRewriter(summarizeClient llm.Client, toolRegistry *tools.Registry) *ToolCallRewriter {
	enabled := features.Enabled["tool_call_rewrite"]
	return &ToolCallRewriter{
		summarizeClient: summarizeClient,
		toolRegistry:    toolRegistry,
		enabled:         enabled,
	}
}

// ToolSpec represents a tool specification for the rewrite prompt.
type ToolSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// RewriteRequest is the input for the rewrite prompt.
type RewriteRequest struct {
	InvalidToolName string                 `json:"invalid_tool_name"`
	InvalidParams   map[string]interface{} `json:"invalid_params"`
	Reason          string                 `json:"reason"`
	AvailableTools  []ToolSpec             `json:"available_tools"`
}

// RewriteResponse is the expected JSON output from the summarization model.
type RewriteResponse struct {
	RewrittenToolName string                 `json:"rewritten_tool_name"`
	RewrittenParams   map[string]interface{} `json:"rewritten_params"`
	Explanation       string                 `json:"explanation"`
	ShouldRewrite     bool                   `json:"should_rewrite"`
}

// RewriteToolCall attempts to rewrite an invalid tool call to a valid one.
// Returns the rewritten tool call or nil if rewriting is not possible or not applicable.
func (r *ToolCallRewriter) RewriteToolCall(ctx context.Context, invalidToolName string, invalidParams map[string]interface{}, reason string) (string, map[string]interface{}, string, error) {
	if !r.enabled {
		return "", nil, "", fmt.Errorf("tool call rewriting is disabled")
	}

	if r.summarizeClient == nil {
		return "", nil, "", fmt.Errorf("summarization client not available for tool call rewriting")
	}

	if r.toolRegistry == nil {
		return "", nil, "", fmt.Errorf("tool registry not available for tool call rewriting")
	}

	// Get available tools as specs
	availableTools := r.getAvailableToolsSpecs()
	if len(availableTools) == 0 {
		return "", nil, "", fmt.Errorf("no available tools for rewrite reference")
	}

	// Build the rewrite request
	request := RewriteRequest{
		InvalidToolName: invalidToolName,
		InvalidParams:   invalidParams,
		Reason:          reason,
		AvailableTools:  availableTools,
	}

	// Marshal request to JSON
	requestJSON, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to marshal rewrite request: %w", err)
	}

	// Build the system prompt
	systemPrompt := `You are a helpful assistant that rewrites invalid tool calls to valid ones.

Your task is to analyze the invalid tool call provided and rewrite it to use a valid tool from the available tools list.

Rules:
1. Only rewrite if a valid equivalent tool exists.
2. Preserve the original intent as much as possible.
3. Adjust parameters to match the target tool's schema.
4. Return JSON in the exact format specified.
5. If no valid rewrite is possible, set "should_rewrite" to false.

Common rewrite examples:
- "shell" → "go_sandbox" for code execution
- "bash", "sh", "exec", "execute", "run" → "shell" or "go_sandbox"
- "python", "python3" → "go_sandbox" or "shell" with python command
- File operations: use "create_file", "edit_file", "replace_file" as appropriate
- Summarization: "summarize_file" → "read_file_summarized"
- Task management: "add_todo", "task" → "todo"

Return ONLY valid JSON in the following format:
{
  "rewritten_tool_name": "<valid tool name>",
  "rewritten_params": {<adjusted parameters>},
  "explanation": "<brief explanation of the rewrite>",
  "should_rewrite": true/false
}`

	// Build the user prompt
	userPrompt := `Rewrite the following invalid tool call:

` + string(requestJSON)

	// Call the summarization model
	logger.Debug("ToolCallRewriter: requesting rewrite for tool %s (reason: %s)", invalidToolName, reason)

	response, err := r.summarizeClient.CompleteWithRequest(ctx, &llm.CompletionRequest{
		Messages: []*llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:   4096,
		Temperature: 1.0,
	})
	if err != nil {
		return "", nil, "", fmt.Errorf("summarization model request failed: %w", err)
	}

	// Parse the response
	var rewriteResp RewriteResponse
	if err := json.Unmarshal([]byte(response.Content), &rewriteResp); err != nil {
		logger.Warn("ToolCallRewriter: failed to parse rewrite response as JSON: %v (content: %s)", err, response.Content)
		return "", nil, "", fmt.Errorf("failed to parse rewrite response: %w", err)
	}

	// Check if rewriting is recommended
	if !rewriteResp.ShouldRewrite {
		logger.Debug("ToolCallRewriter: model recommends not rewriting tool %s", invalidToolName)
		return "", nil, "", fmt.Errorf("no valid rewrite available")
	}

	// Validate the rewritten tool exists
	if _, ok := r.toolRegistry.GetExecutor(rewriteResp.RewrittenToolName); !ok {
		logger.Warn("ToolCallRewriter: model suggested non-existent tool %s", rewriteResp.RewrittenToolName)
		return "", nil, "", fmt.Errorf("rewritten tool %s does not exist", rewriteResp.RewrittenToolName)
	}

	logger.Info("ToolCallRewriter: successfully rewrote tool %s to %s (explanation: %s)",
		invalidToolName, rewriteResp.RewrittenToolName, rewriteResp.Explanation)

	return rewriteResp.RewrittenToolName, rewriteResp.RewrittenParams, rewriteResp.Explanation, nil
}

// getAvailableToolsSpecs returns all available tool specifications.
func (r *ToolCallRewriter) getAvailableToolsSpecs() []ToolSpec {
	specs := r.toolRegistry.ListSpecs()
	result := make([]ToolSpec, 0, len(specs))

	for _, spec := range specs {
		result = append(result, ToolSpec{
			Name:        spec.Name(),
			Description: spec.Description(),
			Parameters:  spec.Parameters(),
		})
	}

	return result
}

// CanRewrite checks if the rewriter can attempt to rewrite a tool call.
func (r *ToolCallRewriter) CanRewrite() bool {
	return r.enabled && r.summarizeClient != nil && r.toolRegistry != nil
}
