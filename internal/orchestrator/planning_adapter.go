package orchestrator

import (
	"context"

	"github.com/codefionn/scriptschnell/internal/planning"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// planningAgentAdapter adapts the internal planning agent to the tools.PlanningAgent interface
type planningAgentAdapter struct {
	orch *Orchestrator
}

// newPlanningAgentAdapter creates a new adapter that provides the PlanningAgent interface
func newPlanningAgentAdapter(orch *Orchestrator) tools.PlanningAgent {
	return &planningAgentAdapter{orch: orch}
}

// Plan generates a plan using the planning agent
func (a *planningAgentAdapter) Plan(
	ctx context.Context,
	objective string,
	contextStr string,
	contextFiles []string,
	allowQuestions bool,
	maxQuestions int,
) (*tools.PlanningResult, error) {
	// Create planning request
	req := &planning.PlanningRequest{
		Objective:      objective,
		Context:        contextStr,
		ContextFiles:   contextFiles,
		AllowQuestions: allowQuestions,
		MaxQuestions:   maxQuestions,
	}

	// Create planning agent
	agentID := "planning_tool_" + a.orch.session.ID
	agent := planning.NewPlanningAgent(agentID, a.orch.fs, a.orch.session, a.orch.planningClient, nil)
	defer func() {
		_ = agent.Close(ctx)
	}()

	// Execute planning
	response, err := agent.Plan(ctx, req, nil)
	if err != nil {
		return nil, err
	}

	// Convert planning.PlanningResponse to tools.PlanningResult
	return convertPlanningResponse(response), nil
}

// convertPlanningResponse converts a planning.PlanningResponse to tools.PlanningResult
func convertPlanningResponse(response *planning.PlanningResponse) *tools.PlanningResult {
	result := &tools.PlanningResult{
		Mode:       string(response.Mode),
		Plan:       response.Plan,
		Questions:  response.Questions,
		NeedsInput: response.NeedsInput,
		Complete:   response.Complete,
	}

	if response.Board != nil {
		result.Board = convertPlanningBoard(response.Board)
	}

	return result
}

// convertPlanningBoard converts a planning.PlanningBoard to tools.PlanningBoard
func convertPlanningBoard(board *planning.PlanningBoard) *tools.PlanningBoard {
	return &tools.PlanningBoard{
		Description:  board.Description,
		PrimaryTasks: convertPlanningTasks(board.PrimaryTasks),
	}
}

// convertPlanningTasks converts a slice of planning.PlanningTask to tools.PlanningTask
func convertPlanningTasks(tasks []planning.PlanningTask) []tools.PlanningTask {
	result := make([]tools.PlanningTask, len(tasks))
	for i, task := range tasks {
		result[i] = tools.PlanningTask{
			ID:          task.ID,
			Text:        task.Text,
			Priority:    task.Priority,
			Status:      task.Status,
			Description: task.Description,
			Subtasks:    convertPlanningTasks(task.Subtasks),
		}
	}
	return result
}
