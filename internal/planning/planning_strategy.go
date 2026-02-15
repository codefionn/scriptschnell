package planning

import (
	"github.com/codefionn/scriptschnell/internal/orchestrator/loop"
)

// PlanningStrategy implements loop.Strategy for planning-specific control flow.
// It handles planning-specific termination conditions like:
// - Plan completion detection
// - User input requirements
// - Question limit exhaustion
// - Text loop detection
type PlanningStrategy struct {
	config *loop.Config
	// AllowQuestions indicates if the planning agent is allowed to ask questions
	AllowQuestions bool
	// MaxQuestions is the maximum number of questions allowed (0 = unlimited)
	MaxQuestions int
}

// NewPlanningStrategy creates a new PlanningStrategy
func NewPlanningStrategy(config *loop.Config, allowQuestions bool, maxQuestions int) *PlanningStrategy {
	if config == nil {
		config = loop.DefaultConfig()
	}
	return &PlanningStrategy{
		config:         config,
		AllowQuestions: allowQuestions,
		MaxQuestions:   maxQuestions,
	}
}

// ShouldContinue determines if the loop should continue after an iteration.
// For planning, we continue if:
// - No errors occurred
// - We haven't hit the iteration limit
// - The iteration had tool calls (need to continue for LLM to process results)
// - The iteration wasn't a final result (plan complete, needs input, or loop detected)
func (s *PlanningStrategy) ShouldContinue(state loop.State, outcome *loop.IterationOutcome) bool {
	// Check iteration limit
	if state.HasReachedLimit() {
		return false
	}

	// If there was an error, stop
	if outcome != nil && outcome.Result == loop.Error {
		return false
	}

	// If loop was detected, stop
	if outcome != nil && outcome.Result == loop.BreakLoopDetected {
		return false
	}

	// If we got a final result (complete plan or needs input), stop
	if outcome != nil && outcome.Result == loop.Break {
		return false
	}

	// If there were tool calls, continue to let LLM process results
	if outcome != nil && outcome.HasToolCalls {
		return true
	}

	// If compaction was needed and handled, continue
	if outcome != nil && outcome.Result == loop.CompactionNeeded {
		return true
	}

	// Default: continue if we haven't reached a terminal state
	return true
}

// ShouldAutoContinue determines if auto-continue should be triggered.
// Planning typically doesn't use auto-continue since it relies on tool calls
// or explicit plan completion signals.
func (s *PlanningStrategy) ShouldAutoContinue(state loop.State, content string) bool {
	// Planning agent doesn't typically use auto-continue
	// It either gets tool calls, a complete plan, or needs user input
	return false
}

// GetResult returns the final LoopResult based on the loop's termination state.
// For planning, this includes plan-specific metadata.
func (s *PlanningStrategy) GetResult(state loop.State, lastOutcome *loop.IterationOutcome, terminatedEarly bool) *loop.Result {
	result := &loop.Result{
		IterationsExecuted:   state.Iteration(),
		AutoContinueAttempts: state.AutoContinueAttempts(),
		Metadata:             make(map[string]interface{}),
	}

	// Handle nil outcome
	if lastOutcome == nil {
		result.Success = true
		result.TerminationReason = "completed"
		return result
	}

	// Copy metadata from outcome
	for k, v := range lastOutcome.Metadata {
		result.Metadata[k] = v
	}

	switch lastOutcome.Result {
	case loop.Break:
		result.Success = true
		// Determine specific termination reason based on metadata
		if needsInput, ok := lastOutcome.Metadata["needs_input"].(bool); ok && needsInput {
			result.TerminationReason = "needs user input"
			if questionsExhausted, ok := lastOutcome.Metadata["questions_exhausted"].(bool); ok && questionsExhausted {
				result.TerminationReason = "questions exhausted, partial plan returned"
			}
		} else if complete, ok := lastOutcome.Metadata["complete"].(bool); ok && complete {
			result.TerminationReason = "plan completed successfully"
		} else {
			result.TerminationReason = "plan extraction complete"
		}

	case loop.BreakWithAutoContinue:
		// Planning shouldn't typically hit this, but handle it
		result.Success = true
		result.TerminationReason = "auto-continue limit reached"

	case loop.BreakMaxIterations:
		result.Success = true
		result.HitIterationLimit = true
		result.TerminationReason = "maximum iteration limit reached"
		// Include partial plan info if available
		if lastOutcome.Metadata != nil {
			for k, v := range lastOutcome.Metadata {
				result.Metadata[k] = v
			}
		}

	case loop.BreakLoopDetected:
		result.Success = false
		result.LoopDetected = true
		result.TerminationReason = "text loop detected in planning response"
		if pattern, ok := lastOutcome.Metadata["loop_pattern"].(string); ok {
			result.Metadata["loop_pattern"] = pattern
		}
		if count, ok := lastOutcome.Metadata["loop_count"].(int); ok {
			result.Metadata["loop_count"] = count
		}

	case loop.Error:
		result.Success = false
		result.Error = lastOutcome.Error
		result.TerminationReason = "error occurred during planning"
		if lastOutcome.Error != nil {
			result.TerminationReason += ": " + lastOutcome.Error.Error()
		}

	case loop.CompactionNeeded:
		// This shouldn't happen as a final state
		result.Success = true
		result.TerminationReason = "context compaction required"

	case loop.Continue:
		// Loop was interrupted while it should have continued
		if terminatedEarly {
			result.Success = true
			result.TerminationReason = "terminated by external signal"
		} else {
			result.Success = true
			result.TerminationReason = "planning completed"
		}

	default:
		result.Success = true
		result.TerminationReason = "planning completed"
	}

	return result
}

// PlanningResult extends loop.Result with planning-specific fields
type PlanningResult struct {
	*loop.Result
	// Plan is the extracted plan (simple mode)
	Plan []string
	// Board is the extracted planning board (board mode)
	Board *PlanningBoard
	// NeedsInput indicates if user input is required
	NeedsInput bool
	// Questions contains any questions from the planning agent
	Questions []string
	// Complete indicates if the planning is complete
	Complete bool
}

// ExtractPlanningResult extracts a PlanningResult from a loop.Result
func ExtractPlanningResult(result *loop.Result) *PlanningResult {
	pr := &PlanningResult{
		Result: result,
	}

	if result.Metadata == nil {
		return pr
	}

	// Extract plan response if available
	if planResp, ok := result.Metadata["plan_response"].(*PlanningResponse); ok {
		pr.Plan = planResp.Plan
		pr.Board = planResp.Board
		pr.NeedsInput = planResp.NeedsInput
		pr.Questions = planResp.Questions
		pr.Complete = planResp.Complete
	}

	// Extract individual fields if available
	if needsInput, ok := result.Metadata["needs_input"].(bool); ok {
		pr.NeedsInput = needsInput
	}

	if complete, ok := result.Metadata["complete"].(bool); ok {
		pr.Complete = complete
	}

	return pr
}

// IsPartialPlan returns true if the result contains a partial plan
// (e.g., due to loop detection or iteration limit)
func (pr *PlanningResult) IsPartialPlan() bool {
	if pr.Result == nil {
		return false
	}
	return pr.Result.LoopDetected || pr.Result.HitIterationLimit
}

// HasContent returns true if the result has actual plan content
func (pr *PlanningResult) HasContent() bool {
	return len(pr.Plan) > 0 || (pr.Board != nil && len(pr.Board.PrimaryTasks) > 0)
}

// Ensure PlanningStrategy implements loop.Strategy
var _ loop.Strategy = (*PlanningStrategy)(nil)
