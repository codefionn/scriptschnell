package tools

import (
	"context"
)

// Investigator defines the interface for the codebase investigation agent.
type Investigator interface {
	Investigate(ctx context.Context, objective string) (string, error)
}

// InvestigatorWithCallback is an extended investigator interface that supports status callbacks
type InvestigatorWithCallback interface {
	Investigator
	InvestigateWithCallback(ctx context.Context, objective string, statusCb func(string) error) (string, error)
}

// CodebaseInvestigatorTool exposes the investigation agent as a tool.
type CodebaseInvestigatorTool struct {
	investigator Investigator
}

func NewCodebaseInvestigatorTool(investigator Investigator) *CodebaseInvestigatorTool {
	return &CodebaseInvestigatorTool{
		investigator: investigator,
	}
}

func (t *CodebaseInvestigatorTool) Name() string {
	return ToolNameCodebaseInvestigator
}

func (t *CodebaseInvestigatorTool) Description() string {
	return "Investigates the codebase to answer a specific query or goal. Uses a separate agent with access to read_file, search_files, and search_file_content."
}

func (t *CodebaseInvestigatorTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"objective": map[string]interface{}{
				"type":        "string",
				"description": "The goal or question to investigate in the codebase.",
			},
		},
		"required": []string{"objective"},
	}
}

func (t *CodebaseInvestigatorTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	objective := GetStringParam(params, "objective", "")
	if objective == "" {
		return &ToolResult{Error: "objective is required"}
	}

	result, err := t.investigator.Investigate(ctx, objective)
	if err != nil {
		return &ToolResult{Error: err.Error()}
	}
	return &ToolResult{Result: result}
}
