package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// PlanningAgent defines the interface for the planning agent.
// This is implemented by the orchestrator to avoid import cycles.
type PlanningAgent interface {
	// Plan generates a plan for the given objective
	Plan(ctx context.Context, objective, context string, contextFiles []string, allowQuestions bool, maxQuestions int) (*PlanningResult, error)
}

// PlanningResult represents the result of a planning operation
type PlanningResult struct {
	Mode       string         `json:"mode"`
	Plan       []string       `json:"plan,omitempty"`
	Board      *PlanningBoard `json:"board,omitempty"`
	Questions  []string       `json:"questions,omitempty"`
	NeedsInput bool           `json:"needs_input"`
	Complete   bool           `json:"complete"`
}

// PlanningBoard represents a hierarchical planning board
type PlanningBoard struct {
	Description  string         `json:"description,omitempty"`
	PrimaryTasks []PlanningTask `json:"primary_tasks"`
}

// PlanningTask represents a task in the planning board
type PlanningTask struct {
	ID          string         `json:"id"`
	Text        string         `json:"text"`
	Subtasks    []PlanningTask `json:"subtasks,omitempty"`
	Priority    string         `json:"priority,omitempty"`
	Status      string         `json:"status,omitempty"`
	Description string         `json:"description,omitempty"`
}

// PlanningToolSpec is the static specification for the planning_agent tool
type PlanningToolSpec struct{}

func (s *PlanningToolSpec) Name() string {
	return "planning_agent"
}

func (s *PlanningToolSpec) Description() string {
	return "Invokes a planning agent to break down complex tasks into actionable steps. Use this when you need to create a detailed plan, analyze requirements, or break down a complex objective into manageable subtasks. The planning agent can investigate the codebase, ask clarifying questions (if allowed), and produce either a simple task list or a hierarchical planning board with primary tasks and subtasks."
}

func (s *PlanningToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"objective": map[string]interface{}{
				"type":        "string",
				"description": "The main objective or task to plan for. Be specific and clear about what needs to be accomplished.",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Additional context or background information that may help the planning agent understand the task better",
			},
			"context_files": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional array of file paths to include as context for the planning agent",
			},
			"allow_questions": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether the planning agent is allowed to ask clarifying questions. Default is false.",
			},
			"max_questions": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of questions the planning agent can ask (only applies if allow_questions is true). Default is 0 (unlimited).",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Preferred planning mode: 'simple' for flat task lists, 'board' for hierarchical tasks with subtasks, or 'auto' to let the agent decide based on complexity. Default is 'auto'.",
				"enum":        []string{"simple", "board", "auto"},
			},
		},
		"required": []string{"objective"},
	}
}

// PlanningToolExecutor executes the planning agent tool
type PlanningToolExecutor struct {
	agent PlanningAgent
}

// NewPlanningTool creates a new planning tool executor
func NewPlanningTool(agent PlanningAgent) *PlanningToolExecutor {
	return &PlanningToolExecutor{
		agent: agent,
	}
}

func (t *PlanningToolExecutor) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	objective := GetStringParam(params, "objective", "")
	if objective == "" {
		return &ToolResult{Error: "objective is required"}
	}

	context := GetStringParam(params, "context", "")
	allowQuestions := GetBoolParam(params, "allow_questions", false)
	maxQuestions := GetIntParam(params, "max_questions", 0)

	// Extract context files if provided
	var contextFiles []string
	if filesParam, ok := params["context_files"]; ok {
		if filesArray, ok := filesParam.([]interface{}); ok {
			for _, f := range filesArray {
				if filePath, ok := f.(string); ok {
					contextFiles = append(contextFiles, filePath)
				}
			}
		}
	}

	// Execute planning through the agent interface
	response, err := t.agent.Plan(ctx, objective, context, contextFiles, allowQuestions, maxQuestions)
	if err != nil {
		return &ToolResult{
			Error: fmt.Sprintf("planning failed: %v", err),
		}
	}

	// Format the result
	result := t.formatPlanningResult(response)

	return &ToolResult{
		Result:   result,
		UIResult: t.formatUIResult(response),
	}
}

// formatPlanningResult formats the planning response
func (t *PlanningToolExecutor) formatPlanningResult(response *PlanningResult) map[string]interface{} {
	result := map[string]interface{}{
		"mode":        response.Mode,
		"complete":    response.Complete,
		"needs_input": response.NeedsInput,
	}

	if len(response.Questions) > 0 {
		result["questions"] = response.Questions
	}

	switch response.Mode {
	case "simple":
		result["plan"] = response.Plan
	case "board":
		if response.Board != nil {
			boardResult := map[string]interface{}{
				"description":   response.Board.Description,
				"primary_tasks": t.formatPrimaryTasks(response.Board.PrimaryTasks),
			}
			result["board"] = boardResult
		}
	}

	return result
}

// formatPrimaryTasks converts planning tasks to a serializable format
func (t *PlanningToolExecutor) formatPrimaryTasks(tasks []PlanningTask) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tasks))
	for i, task := range tasks {
		taskMap := map[string]interface{}{
			"id":          task.ID,
			"text":        task.Text,
			"status":      task.Status,
			"description": task.Description,
		}
		if task.Priority != "" {
			taskMap["priority"] = task.Priority
		}
		if len(task.Subtasks) > 0 {
			taskMap["subtasks"] = t.formatPrimaryTasks(task.Subtasks)
		}
		result[i] = taskMap
	}
	return result
}

// formatUIResult creates a user-friendly display of the planning result
func (t *PlanningToolExecutor) formatUIResult(response *PlanningResult) string {
	var output string

	switch response.Mode {
	case "simple":
		output = t.formatSimpleModeUI(response)
	case "board":
		output = t.formatBoardModeUI(response)
	default:
		output = t.formatSimpleModeUI(response)
	}

	if response.NeedsInput && len(response.Questions) > 0 {
		output += "\n\n**Questions for clarification:**\n"
		for i, q := range response.Questions {
			output += fmt.Sprintf("%d. %s\n", i+1, q)
		}
	}

	return output
}

func (t *PlanningToolExecutor) formatSimpleModeUI(response *PlanningResult) string {
	var output string

	if response.Complete {
		output = "📋 **Planning Complete**\n\n"
	} else {
		output = "📋 **Partial Plan**\n\n"
	}

	if len(response.Plan) > 0 {
		output += "**Action Plan:**\n"
		for i, step := range response.Plan {
			output += fmt.Sprintf("%d. %s\n", i+1, step)
		}
	} else {
		output += "*No plan steps generated.*\n"
	}

	return output
}

func (t *PlanningToolExecutor) formatBoardModeUI(response *PlanningResult) string {
	var output string

	if response.Complete {
		output = "📊 **Planning Board Complete**\n\n"
	} else {
		output = "📊 **Partial Planning Board**\n\n"
	}

	if response.Board != nil {
		if response.Board.Description != "" {
			output += fmt.Sprintf("**Overview:** %s\n\n", response.Board.Description)
		}

		if len(response.Board.PrimaryTasks) > 0 {
			output += "**Primary Tasks:**\n\n"
			for _, task := range response.Board.PrimaryTasks {
				output += t.formatTaskForUI(task, 0)
			}
		}
	}

	return output
}

func (t *PlanningToolExecutor) formatTaskForUI(task PlanningTask, indent int) string {
	var output string
	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "  "
	}

	// Status indicator
	statusIcon := "⬜"
	switch task.Status {
	case "completed":
		statusIcon = "✅"
	case "in_progress":
		statusIcon = "🔄"
	}

	// Priority indicator
	priorityIcon := ""
	switch task.Priority {
	case "high":
		priorityIcon = "🔴 "
	case "medium":
		priorityIcon = "🟡 "
	case "low":
		priorityIcon = "🟢 "
	}

	output = fmt.Sprintf("%s%s %s%s\n", prefix, statusIcon, priorityIcon, task.Text)

	if task.Description != "" {
		output += fmt.Sprintf("%s   *%s*\n", prefix, task.Description)
	}

	for _, subtask := range task.Subtasks {
		output += t.formatTaskForUI(subtask, indent+1)
	}

	return output
}

// NewPlanningToolFactory creates a factory for PlanningToolExecutor
func NewPlanningToolFactory(agent PlanningAgent) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewPlanningTool(agent)
	}
}

// ExtractPlanResult is a helper to extract just the plan array from a result
func ExtractPlanResult(result interface{}) ([]string, error) {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("result is not a map")
	}

	planInterface, ok := resultMap["plan"]
	if !ok {
		return nil, fmt.Errorf("result does not contain 'plan' key")
	}

	planArray, ok := planInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("plan is not an array")
	}

	plan := make([]string, len(planArray))
	for i, item := range planArray {
		str, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("plan item %d is not a string", i)
		}
		plan[i] = str
	}

	return plan, nil
}

// ExtractBoardResult is a helper to extract the board from a result
func ExtractBoardResult(result interface{}) (*PlanningBoard, error) {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("result is not a map")
	}

	boardInterface, ok := resultMap["board"]
	if !ok {
		return nil, fmt.Errorf("result does not contain 'board' key")
	}

	boardJSON, err := json.Marshal(boardInterface)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal board: %w", err)
	}

	var board PlanningBoard
	if err := json.Unmarshal(boardJSON, &board); err != nil {
		return nil, fmt.Errorf("failed to unmarshal board: %w", err)
	}

	return &board, nil
}
