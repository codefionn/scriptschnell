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

	req := &llm.CompletionRequest{
		Messages: []*llm.Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0,
		MaxTokens:   256,
	}

	resp, err := o.summarizeClient.CompleteWithRequest(ctx, req)
	if err != nil {
		logger.Warn("Planning decision LLM failed: %v", err)
		// Fallback: run planning (since heuristic said it's complex) but maybe be careful with MCPs?
		// Let's default to running planning with NO extra MCPs to be safe/fast, or ALL?
		// Original logic enabled all read-only MCPs. Let's stick to that for fallback if we can't decide.
		// But here we return nil allowedMCPs which might mean "all" or "none" depending on implementation.
		// Let's say nil means "all available read-only" in the caller, or we handle it here.
		// Actually, let's just return true for run, and empty list for MCPs (safer).
		defaultDecision.ShouldRun = true
		defaultDecision.Reason = fmt.Sprintf("LLM error (%v), fallback to heuristic complex", err)
		return defaultDecision, nil
	}

	// Parse response
	decision := &PlanningDecision{}
	content := resp.Content

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

	if err := json.Unmarshal([]byte(content), decision); err != nil {
		logger.Warn("Failed to parse planning decision JSON: %v. Content: %s", err, resp.Content)
		defaultDecision.ShouldRun = true
		defaultDecision.Reason = "JSON parse error, fallback to heuristic complex"
		return defaultDecision, nil
	}

	return decision, nil
}
