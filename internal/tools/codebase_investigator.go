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

// CodebaseInvestigatorToolSpec is the static specification for the codebase_investigator tool
type CodebaseInvestigatorToolSpec struct{}

func (s *CodebaseInvestigatorToolSpec) Name() string {
	return ToolNameCodebaseInvestigator
}

func (s *CodebaseInvestigatorToolSpec) Description() string {
	return `Investigates the codebase to answer a specific query or goal. Starts a separate agent that as only access to reading and searching the codebase.
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
			"objective": map[string]interface{}{
				"type":        "string",
				"description": "The goal or question to investigate in the codebase.",
			},
		},
		"required": []string{"objective"},
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

// NewCodebaseInvestigatorToolFactory creates a factory for CodebaseInvestigatorTool
func NewCodebaseInvestigatorToolFactory(investigator Investigator) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewCodebaseInvestigatorTool(investigator)
	}
}
