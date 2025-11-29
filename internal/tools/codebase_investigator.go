package tools

import (
	"context"
	"fmt"
)

// Investigator defines the interface for the codebase investigation agent.
type Investigator interface {
	Investigate(ctx context.Context, objective string) (string, error)
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

func (t *CodebaseInvestigatorTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	objective := GetStringParam(params, "objective", "")
	if objective == "" {
		return nil, fmt.Errorf("objective is required")
	}

	return t.investigator.Investigate(ctx, objective)
}
