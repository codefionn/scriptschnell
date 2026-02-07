package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// PlanningDecision represents the decision made by the summarization model regarding planning
type PlanningDecision struct {
	ShouldRun   bool     `json:"run_planning"`
	Reason      string   `json:"reason"`
	AllowedMCPs []string `json:"allowed_mcps"`
	Notes       string   `json:"notes"`
}

// decidePlanningConfiguration uses the summarization model to decide if planning should run and which MCPs to use.
func (o *Orchestrator) decidePlanningConfiguration(ctx context.Context, prompt string) (*PlanningDecision, error) {
	// Default decision (if model unavailable or fails)
	defaultDecision := &PlanningDecision{
		ShouldRun:   false,
		Reason:      "Default fallback",
		AllowedMCPs: nil,
	}

	// First run the heuristic check. If it's definitely simple, we might skip the LLM call entirely?
	// The plan says "consolidates the current classifyPromptSimplicity logic".
	// However, classifyPromptSimplicity had a heuristic fallback.
	// We should probably preserve the heuristic check for very simple prompts to save tokens/latency.
	isSimple, heuristicReason := heuristicPromptSimplicity(prompt)
	if isSimple {
		logger.Debug("Skipping planning decision LLM: prompt marked simple by heuristic (%s)", heuristicReason)
		defaultDecision.Reason = heuristicReason
		return defaultDecision, nil
	}

	if o.summarizeClient == nil {
		logger.Debug("No summarize client available for planning decision, using heuristics")
		// If complex by heuristic but no client, we default to running planning with all MCPs?
		// Or maybe just run planning without extra MCPs?
		// The original logic ran planning if not simple.
		defaultDecision.ShouldRun = true
		defaultDecision.Reason = heuristicReason
		return defaultDecision, nil
	}

	// Build list of available MCP servers for context
	var mcpContext strings.Builder
	if o.config != nil && o.config.MCP.Servers != nil && len(o.config.MCP.Servers) > 0 {
		mcpContext.WriteString("Available MCP Servers (external tools):\n")
		for name, cfg := range o.config.MCP.Servers {
			desc := cfg.Description
			if desc == "" {
				desc = "No description provided"
			}
			mcpContext.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
		}
	} else {
		mcpContext.WriteString("No external MCP servers available.\n")
	}

	systemPrompt := fmt.Sprintf(`You are a planning configuration assistant.
Your goal is to decide if the user's request requires a planning phase and which external tools (MCP servers) are relevant.

1. **Should Planning Run?**
   - YES if the task is complex, multi-step, architectural, or requires research/investigation.
   - NO if the task is simple, a single small change, or a direct question that doesn't need a plan.

2. **Which MCPs are Relevant?**
   - Select ONLY the MCP servers that are directly relevant to the user's request.
   - If the request is about searching documentation, select documentation MCPs.
   - If the request is about database schema, select database MCPs.
   - If no MCPs are relevant, return an empty list.

%s

Respond ONLY with a JSON object in the following format:
{
  "run_planning": true/false,
  "reason": "Short explanation of your decision",
  "allowed_mcps": ["server_name1", "server_name2"],
  "notes": "Optional notes"
}
`, mcpContext.String())

	messages := []*llm.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	// Use higher MaxTokens to accommodate thinking/reasoning models (e.g., Qwen 3, DeepSeek)
	// that consume tokens for internal reasoning before producing the actual JSON response.
	const maxTokens = 16384
	const maxContinuations = 3

	var contentBuilder strings.Builder

	for turn := 0; turn <= maxContinuations; turn++ {
		req := &llm.CompletionRequest{
			Messages:    messages,
			Temperature: 0,
			MaxTokens:   maxTokens,
		}

		resp, err := o.summarizeClient.CompleteWithRequest(ctx, req)
		if err != nil {
			logger.Warn("Planning decision LLM failed: %v", err)
			defaultDecision.ShouldRun = true
			defaultDecision.Reason = fmt.Sprintf("LLM error (%v), fallback to heuristic complex", err)
			return defaultDecision, nil
		}

		contentBuilder.WriteString(resp.Content)

		// If the response wasn't truncated, we're done
		if resp.StopReason != "length" {
			break
		}

		// Response was truncated — continue the conversation
		logger.Debug("Planning decision response truncated (turn %d), continuing", turn+1)
		messages = append(messages,
			&llm.Message{
				Role:    "assistant",
				Content: resp.Content,
			},
			&llm.Message{
				Role:    "user",
				Content: "Continue your response from where you left off. Output only the remaining content.",
			},
		)
	}

	// Parse the accumulated response
	decision := &PlanningDecision{}
	content := contentBuilder.String()

	// Strip <think> tags (reasoning models like DeepSeek output these)
	content = stripThinkTags(content)

	// Try to extract JSON if wrapped in markdown code blocks
	if start := strings.Index(content, "```json"); start != -1 {
		content = content[start+7:]
		if end := strings.Index(content, "```"); end != -1 {
			content = content[:end]
		}
	} else if start := strings.Index(content, "{"); start != -1 {
		// Try to find raw JSON object
		if end := strings.LastIndex(content, "}"); end != -1 {
			content = content[start : end+1]
		}
	}

	// Validate that we have clean JSON
	content = strings.TrimSpace(content)

	// Fix multiline JSON strings: LLMs sometimes output actual newlines inside string values
	// instead of escaped \n. We need to escape newlines that appear inside JSON string values.
	content = normalizeJSONStrings(content)

	var jsonCheck interface{}
	if err := json.Unmarshal([]byte(content), &jsonCheck); err != nil {
		logger.Warn("Summary model decision does not equal exactly what was asked for: %q", content)
	}

	if err := json.Unmarshal([]byte(content), decision); err != nil {
		logger.Warn("Failed to parse planning decision JSON: %v. Content: %s", err, content)
		defaultDecision.ShouldRun = true
		defaultDecision.Reason = "JSON parse error, fallback to heuristic complex"
		return defaultDecision, nil
	}

	return decision, nil
}

// normalizeJSONStrings escapes newlines that appear inside JSON string values.
// LLMs sometimes output multiline strings with actual newlines instead of escaped \n.
func normalizeJSONStrings(content string) string {
	var result strings.Builder
	result.Grow(len(content))

	inString := false
	escaped := false

	for i, r := range content {
		if escaped {
			result.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			result.WriteRune(r)
			escaped = true
			continue
		}

		if r == '"' {
			inString = !inString
			result.WriteRune(r)
			continue
		}

		if inString && (r == '\n' || r == '\r') {
			// Replace newline with escaped newline, but be careful not to
			// add escapes inside already-valid JSON structure
			if r == '\n' {
				result.WriteString("\\n")
			}
			// Skip \r to normalize line endings
			if r == '\r' && i+1 < len(content) && content[i+1] != '\n' {
				result.WriteString("\\n")
			}
		} else {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// stripThinkTags removes <think>...</think> blocks from content.
// Reasoning models like DeepSeek wrap their internal reasoning in these tags.
func stripThinkTags(content string) string {
	for {
		start := strings.Index(content, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], "</think>")
		if end == -1 {
			// Unclosed tag, just remove the opening tag and continue
			content = strings.TrimSpace(content[:start] + content[start+len("<think>"):])
			break
		}
		end += start + len("</think>")
		content = strings.TrimSpace(content[:start] + content[end:])
	}
	return content
}
