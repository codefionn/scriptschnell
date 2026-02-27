package tools

import (
	"context"
)

// Investigator defines the interface for the codebase investigation agent.
type Investigator interface {
	Investigate(ctx context.Context, objectives []string) ([]string, error)
}

// CodebaseInvestigatorToolSpec is the static specification for the codebase_investigator tool
type CodebaseInvestigatorToolSpec struct{}

func (s *CodebaseInvestigatorToolSpec) Name() string {
	return ToolNameCodebaseInvestigator
}

func (s *CodebaseInvestigatorToolSpec) Description() string {
	return `Investigates the codebase to answer a specific query or goal. Starts a separate agent that has only access to reading and searching the codebase (no create/update file tools).
Use cases:
- Gather context about specific parts of the codebase
- Find where certain logic is implemented
- Understand how different parts of the codebase interact
- Answer questions about the codebase structure or content
- Explore relevant files before making modifications

Don't use for:
- Making direct code changes (use file editing tools instead)
- Testing/Building the project`
}

func (s *CodebaseInvestigatorToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"objectives": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "List of goals or questions to investigate in the codebase. Each objective is investigated concurrently. Please also add basic tooling information (programming language, major frameworks used, etc.) to each objective.",
			},
		},
		"required": []string{"objectives"},
	}
}

// CodebaseInvestigatorTool is the executor with runtime dependencies
type CodebaseInvestigatorTool struct {
	investigator Investigator
}

func NewCodebaseInvestigatorTool(investigator Investigator) *CodebaseInvestigatorTool {
	return &CodebaseInvestigatorTool{
		investigator: investigator,
	}
}

// Legacy interface implementation for backward compatibility
func (t *CodebaseInvestigatorTool) Name() string { return ToolNameCodebaseInvestigator }
func (t *CodebaseInvestigatorTool) Description() string {
	return (&CodebaseInvestigatorToolSpec{}).Description()
}
func (t *CodebaseInvestigatorTool) Parameters() map[string]interface{} {
	return (&CodebaseInvestigatorToolSpec{}).Parameters()
}

func (t *CodebaseInvestigatorTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	objectivesRaw, ok := params["objectives"]
	if !ok {
		return &ToolResult{Error: "objectives is required"}
	}

	objectivesArr, ok := objectivesRaw.([]interface{})
	if !ok {
		return &ToolResult{Error: "objectives must be an array"}
	}

	if len(objectivesArr) == 0 {
		return &ToolResult{Error: "at least one objective is required"}
	}

	objectives := make([]string, 0, len(objectivesArr))
	for _, obj := range objectivesArr {
		if s, ok := obj.(string); ok && s != "" {
			objectives = append(objectives, s)
		}
	}

	if len(objectives) == 0 {
		return &ToolResult{Error: "at least one non-empty objective is required"}
	}

	results, err := t.investigator.Investigate(ctx, objectives)
	if err != nil {
		return &ToolResult{Error: err.Error()}
	}

	// Format results with objective headers
	var combined string
	for i, result := range results {
		if len(objectives) > 1 {
			combined += "## Objective: " + objectives[i] + "\n\n"
		}
		combined += result
		if i < len(results)-1 {
			combined += "\n\n---\n\n"
		}
	}

	return &ToolResult{Result: combined}
}

// NewCodebaseInvestigatorToolFactory creates a factory for CodebaseInvestigatorTool
func NewCodebaseInvestigatorToolFactory(investigator Investigator) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewCodebaseInvestigatorTool(investigator)
	}
}
