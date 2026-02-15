package planning

import (
	"errors"
	"testing"

	"github.com/codefionn/scriptschnell/internal/orchestrator/loop"
)

func TestPlanningStrategyShouldContinue(t *testing.T) {
	config := loop.DefaultConfig()
	config.MaxIterations = 5

	t.Run("Continue with tool calls", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result:       loop.Continue,
			HasToolCalls: true,
		}

		if !strategy.ShouldContinue(state, outcome) {
			t.Error("Should continue when there are tool calls")
		}
	})

	t.Run("Stop on Break result", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result: loop.Break,
			Metadata: map[string]interface{}{
				"complete": true,
			},
		}

		if strategy.ShouldContinue(state, outcome) {
			t.Error("Should not continue when result is Break")
		}
	})

	t.Run("Stop on loop detected", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result: loop.BreakLoopDetected,
			Metadata: map[string]interface{}{
				"loop_pattern": "repeating text",
				"loop_count":   5,
			},
		}

		if strategy.ShouldContinue(state, outcome) {
			t.Error("Should not continue when loop is detected")
		}
	})

	t.Run("Stop on error", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result: loop.Error,
			Error:  errors.New("test error"),
		}

		if strategy.ShouldContinue(state, outcome) {
			t.Error("Should not continue on error")
		}
	})

	t.Run("Stop at iteration limit", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		// Increment to max
		for i := 0; i < config.MaxIterations; i++ {
			state.Increment()
		}

		outcome := &loop.IterationOutcome{Result: loop.Continue}

		if strategy.ShouldContinue(state, outcome) {
			t.Error("Should not continue at iteration limit")
		}
	})

	t.Run("Continue after compaction", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{Result: loop.CompactionNeeded}

		if !strategy.ShouldContinue(state, outcome) {
			t.Error("Should continue after compaction")
		}
	})
}

func TestPlanningStrategyShouldAutoContinue(t *testing.T) {
	strategy := NewPlanningStrategy(loop.DefaultConfig(), true, 0)
	state := loop.NewDefaultState(loop.DefaultConfig())

	// Planning strategy should not use auto-continue
	if strategy.ShouldAutoContinue(state, "Incomplete response...") {
		t.Error("Planning strategy should not auto-continue")
	}

	if strategy.ShouldAutoContinue(state, "Complete response") {
		t.Error("Planning strategy should not auto-continue")
	}
}

func TestPlanningStrategyGetResult(t *testing.T) {
	config := loop.DefaultConfig()

	t.Run("Break with complete plan", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)
		state.Increment()

		outcome := &loop.IterationOutcome{
			Result: loop.Break,
			Metadata: map[string]interface{}{
				"complete": true,
			},
		}

		result := strategy.GetResult(state, outcome, false)

		if !result.Success {
			t.Error("Result should be successful")
		}

		if result.TerminationReason != "plan completed successfully" {
			t.Errorf("Expected 'plan completed successfully', got %q", result.TerminationReason)
		}

		if result.IterationsExecuted != 1 {
			t.Errorf("Expected 1 iteration, got %d", result.IterationsExecuted)
		}
	})

	t.Run("Break with needs input", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result: loop.Break,
			Metadata: map[string]interface{}{
				"needs_input": true,
			},
		}

		result := strategy.GetResult(state, outcome, false)

		if !result.Success {
			t.Error("Result should be successful")
		}

		if result.TerminationReason != "needs user input" {
			t.Errorf("Expected 'needs user input', got %q", result.TerminationReason)
		}
	})

	t.Run("Break with questions exhausted", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 3)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result: loop.Break,
			Metadata: map[string]interface{}{
				"needs_input":         true,
				"questions_exhausted": true,
			},
		}

		result := strategy.GetResult(state, outcome, false)

		if !result.Success {
			t.Error("Result should be successful")
		}

		if result.TerminationReason != "questions exhausted, partial plan returned" {
			t.Errorf("Expected 'questions exhausted, partial plan returned', got %q", result.TerminationReason)
		}
	})

	t.Run("Loop detected", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result: loop.BreakLoopDetected,
			Metadata: map[string]interface{}{
				"loop_pattern": "repeating text",
				"loop_count":   5,
			},
		}

		result := strategy.GetResult(state, outcome, false)

		if result.Success {
			t.Error("Result should not be successful when loop detected")
		}

		if !result.LoopDetected {
			t.Error("LoopDetected should be true")
		}

		if result.Metadata["loop_pattern"] != "repeating text" {
			t.Error("Loop pattern should be in metadata")
		}
	})

	t.Run("Error result", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		testErr := errors.New("test error")
		outcome := &loop.IterationOutcome{
			Result: loop.Error,
			Error:  testErr,
		}

		result := strategy.GetResult(state, outcome, false)

		if result.Success {
			t.Error("Result should not be successful on error")
		}

		if result.Error != testErr {
			t.Error("Error should be preserved")
		}
	})

	t.Run("Max iterations reached", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		outcome := &loop.IterationOutcome{
			Result: loop.BreakMaxIterations,
		}

		result := strategy.GetResult(state, outcome, false)

		if !result.Success {
			t.Error("Result should be successful (partial plan)")
		}

		if !result.HitIterationLimit {
			t.Error("HitIterationLimit should be true")
		}
	})

	t.Run("Nil outcome", func(t *testing.T) {
		strategy := NewPlanningStrategy(config, true, 0)
		state := loop.NewDefaultState(config)

		result := strategy.GetResult(state, nil, false)

		if !result.Success {
			t.Error("Result should be successful")
		}

		if result.TerminationReason != "completed" {
			t.Errorf("Expected 'completed', got %q", result.TerminationReason)
		}
	})
}

func TestExtractPlanningResult(t *testing.T) {
	t.Run("Extract from metadata with plan response", func(t *testing.T) {
		loopResult := &loop.Result{
			Success:           true,
			TerminationReason: "plan completed",
			Metadata: map[string]interface{}{
				"plan_response": &PlanningResponse{
					Mode:       PlanningModeSimple,
					Plan:       []string{"Step 1", "Step 2"},
					NeedsInput: false,
					Complete:   true,
				},
			},
		}

		pr := ExtractPlanningResult(loopResult)

		if !pr.Success {
			t.Error("Should be successful")
		}

		if len(pr.Plan) != 2 {
			t.Errorf("Expected 2 plan steps, got %d", len(pr.Plan))
		}

		if !pr.Complete {
			t.Error("Should be complete")
		}
	})

	t.Run("Extract with board mode", func(t *testing.T) {
		loopResult := &loop.Result{
			Success: true,
			Metadata: map[string]interface{}{
				"plan_response": &PlanningResponse{
					Mode: PlanningModeBoard,
					Board: &PlanningBoard{
						Description: "Test board",
						PrimaryTasks: []PlanningTask{
							{ID: "1", Text: "Task 1"},
							{ID: "2", Text: "Task 2"},
						},
					},
					Complete: true,
				},
			},
		}

		pr := ExtractPlanningResult(loopResult)

		if pr.Board == nil {
			t.Fatal("Board should not be nil")
		}

		if len(pr.Board.PrimaryTasks) != 2 {
			t.Errorf("Expected 2 primary tasks, got %d", len(pr.Board.PrimaryTasks))
		}
	})

	t.Run("Extract needs input", func(t *testing.T) {
		loopResult := &loop.Result{
			Success: true,
			Metadata: map[string]interface{}{
				"needs_input": true,
				"complete":    false,
			},
		}

		pr := ExtractPlanningResult(loopResult)

		if !pr.NeedsInput {
			t.Error("NeedsInput should be true")
		}

		if pr.Complete {
			t.Error("Complete should be false")
		}
	})

	t.Run("IsPartialPlan with loop detected", func(t *testing.T) {
		loopResult := &loop.Result{
			Success:      false,
			LoopDetected: true,
		}

		pr := ExtractPlanningResult(loopResult)

		if !pr.IsPartialPlan() {
			t.Error("Should be partial plan when loop detected")
		}
	})

	t.Run("IsPartialPlan with iteration limit", func(t *testing.T) {
		loopResult := &loop.Result{
			Success:           true,
			HitIterationLimit: true,
		}

		pr := ExtractPlanningResult(loopResult)

		if !pr.IsPartialPlan() {
			t.Error("Should be partial plan when iteration limit hit")
		}
	})

	t.Run("IsPartialPlan with complete result", func(t *testing.T) {
		loopResult := &loop.Result{
			Success: true,
		}

		pr := ExtractPlanningResult(loopResult)

		if pr.IsPartialPlan() {
			t.Error("Should not be partial plan for complete result")
		}
	})

	t.Run("HasContent with plan", func(t *testing.T) {
		pr := &PlanningResult{
			Plan: []string{"Step 1", "Step 2"},
		}

		if !pr.HasContent() {
			t.Error("Should have content with plan")
		}
	})

	t.Run("HasContent with board", func(t *testing.T) {
		pr := &PlanningResult{
			Board: &PlanningBoard{
				PrimaryTasks: []PlanningTask{{ID: "1", Text: "Task"}},
			},
		}

		if !pr.HasContent() {
			t.Error("Should have content with board")
		}
	})

	t.Run("HasContent with empty board", func(t *testing.T) {
		pr := &PlanningResult{
			Board: &PlanningBoard{
				PrimaryTasks: []PlanningTask{},
			},
		}

		if pr.HasContent() {
			t.Error("Should not have content with empty board")
		}
	})

	t.Run("HasContent with no content", func(t *testing.T) {
		pr := &PlanningResult{}

		if pr.HasContent() {
			t.Error("Should not have content when empty")
		}
	})
}

func TestPlanningStrategyImplementsInterface(t *testing.T) {
	// Verify PlanningStrategy implements loop.Strategy
	var _ loop.Strategy = (*PlanningStrategy)(nil)
}
