package tools

import (
	"context"
	"strings"
)

// RefactoringAgent defines the interface for the refactoring agent.
type RefactoringAgent interface {
	Refactor(ctx context.Context, objectives []string) ([]string, error)
}

// RefactoringAgentToolSpec is the static specification for the refactoring_agent tool
type RefactoringAgentToolSpec struct{}

func (s *RefactoringAgentToolSpec) Name() string {
	return ToolNameRefactoringAgent
}

func (s *RefactoringAgentToolSpec) Description() string {
	return `Executes a large refactoring task by spawning multiple orchestrator agents to work on different aspects of the refactoring concurrently.
Each objective is handled by a separate orchestrator agent with full tool access (except the refactoring_agent tool itself to prevent recursive spawning).
Use cases:
- Breaking down large refactorings into manageable pieces
- Parallelizing independent refactoring tasks
- Coordinating multi-step refactorings across multiple files
- Refactoring related but independent components simultaneously

The child orchestrators have access to read, search, and edit tools to perform the actual refactoring work.
This tool is ideal for big refactorings that would benefit from parallel processing and independent task execution.`
}

func (s *RefactoringAgentToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"objectives": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "List of independent refactoring tasks to execute in parallel. Each objective will be handled by a separate orchestrator agent. Each task should be self-contained and not depend on other tasks.",
			},
		},
		"required": []string{"objectives"},
	}
}

// RefactoringAgentTool is the executor with runtime dependencies
type RefactoringAgentTool struct {
	agent RefactoringAgent
}

func NewRefactoringAgentTool(agent RefactoringAgent) *RefactoringAgentTool {
	return &RefactoringAgentTool{
		agent: agent,
	}
}

// Legacy interface implementation for backward compatibility
func (t *RefactoringAgentTool) Name() string { return ToolNameRefactoringAgent }
func (t *RefactoringAgentTool) Description() string {
	return (&RefactoringAgentToolSpec{}).Description()
}
func (t *RefactoringAgentTool) Parameters() map[string]interface{} {
	return (&RefactoringAgentToolSpec{}).Parameters()
}

func (t *RefactoringAgentTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
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
		if s, ok := obj.(string); ok {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" {
				objectives = append(objectives, trimmed)
			}
		}
	}

	if len(objectives) == 0 {
		return &ToolResult{Error: "at least one non-empty objective is required"}
	}

	results, err := t.agent.Refactor(ctx, objectives)
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

// NewRefactoringAgentToolFactory creates a factory for RefactoringAgentTool
func NewRefactoringAgentToolFactory(agent RefactoringAgent) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewRefactoringAgentTool(agent)
	}
}
