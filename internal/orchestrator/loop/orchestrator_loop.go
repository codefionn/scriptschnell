package loop

import (
	"context"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/progress"
)

// OrchestratorLoop is the main implementation of the Loop interface.
// It orchestrates multiple iterations using a strategy and handles
// the overall loop lifecycle.
type OrchestratorLoop struct {
	config     *Config
	state      State
	strategy   Strategy
	iteration  Iteration
	deps       *Dependencies
	ctxManager ContextManager
}

// NewOrchestratorLoop creates a new OrchestratorLoop
func NewOrchestratorLoop(config *Config, strategy Strategy, iteration Iteration, deps *Dependencies) *OrchestratorLoop {
	if config == nil {
		config = DefaultConfig()
	}

	return &OrchestratorLoop{
		config:    config,
		state:     NewDefaultState(config),
		strategy:  strategy,
		iteration: iteration,
		deps:      deps,
	}
}

// Run executes the orchestration loop until completion or termination.
func (l *OrchestratorLoop) Run(ctx context.Context, session Session, progressCb progress.Callback) (*Result, error) {
	// Reset state for fresh run
	l.state.ResetLoopDetection()
	l.state.ResetAutoContinue()
	l.state.ResetCompactionAttempts()

	var lastOutcome *IterationOutcome
	var terminatedEarly bool

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			terminatedEarly = true
			lastOutcome = &IterationOutcome{Result: Continue}
			goto done
		default:
		}

		// Increment iteration counter
		iteration := l.state.Increment()

		// Check iteration limit before execution
		if l.state.HasReachedLimit() {
			lastOutcome = &IterationOutcome{Result: BreakMaxIterations}
			goto done
		}

		// Send progress update
		if progressCb != nil {
			_ = progressCb(progress.Update{
				Message:   fmt.Sprintf("Thinking... (iteration %d)", iteration),
				Mode:      progress.ReportJustStatus,
				Ephemeral: true,
			})
		}

		// Execute single iteration
		outcome, err := l.iteration.Execute(ctx, l.state)
		if err != nil {
			outcome = &IterationOutcome{
				Result: Error,
				Error:  err,
			}
		}

		lastOutcome = outcome

		// Handle compaction needed
		if outcome.Result == CompactionNeeded {
			if l.ctxManager != nil {
				systemPrompt, _ := l.deps.SystemPromptProvider.GetSystemPrompt(ctx)
				compactErr := l.ctxManager.Compact(ctx, l.deps.SystemPromptProvider.GetModelID(), systemPrompt, progressCb)
				if compactErr != nil {
					// If compaction fails, return error
					outcome.Result = Error
					outcome.Error = fmt.Errorf("compaction failed: %w", compactErr)
					goto done
				}
				l.state.RecordCompaction()
				// Continue to next iteration after compaction
				continue
			}
		}

		// Check if we should continue
		if !l.strategy.ShouldContinue(l.state, outcome) {
			goto done
		}

		// Handle auto-continue for incomplete responses
		if outcome.Result == Break && outcome.Content != "" {
			if l.strategy.ShouldAutoContinue(l.state, outcome.Content) {
				// Add continue message to session
				session.AddMessage(&SimpleMessage{
					Role:    "user",
					Content: "continue",
				})

				l.state.IncrementAutoContinue()
				outcome.Result = BreakWithAutoContinue

				// Notify progress
				if progressCb != nil {
					_ = progressCb(progress.Update{
						Message:    "⏭ Auto-continue.\n",
						AddNewLine: false,
						Mode:       progress.ReportNoStatus,
					})
				}

				// Continue to next iteration
				continue
			}
		}
	}
done:
	// Build final result
	result := l.strategy.GetResult(l.state, lastOutcome, terminatedEarly)

	// Add final progress update
	if progressCb != nil {
		if result.HitIterationLimit {
			_ = progressCb(progress.Update{
				Message:    fmt.Sprintf("\n⚠️  Reached maximum iteration limit (%d).\n", l.config.MaxIterations),
				AddNewLine: false,
				Mode:       progress.ReportNoStatus,
			})
		} else if result.LoopDetected {
			pattern := ""
			if lastOutcome != nil && lastOutcome.Metadata != nil {
				if p, ok := lastOutcome.Metadata["loop_pattern"].(string); ok {
					pattern = p
				}
			}
			_ = progressCb(progress.Update{
				Message:    fmt.Sprintf("\n\n🔁 Loop detected! Pattern '%s' detected. Stopping.\n", pattern),
				AddNewLine: false,
				Mode:       progress.ReportNoStatus,
			})
		}
	}

	// Return context cancellation as an error
	if terminatedEarly && ctx.Err() != nil {
		return result, ctx.Err()
	}

	// Return iteration errors
	if result.Error != nil {
		return result, result.Error
	}

	return result, nil
}

// RunIteration executes a single iteration and returns the outcome.
func (l *OrchestratorLoop) RunIteration(ctx context.Context, state State) (*IterationOutcome, error) {
	return l.iteration.Execute(ctx, state)
}

// GetState returns the current loop state
func (l *OrchestratorLoop) GetState() State {
	return l.state
}

// SetContextManager sets the context manager for the loop
func (l *OrchestratorLoop) SetContextManager(cm ContextManager) {
	l.ctxManager = cm
}

// Builder provides a fluent interface for constructing OrchestratorLoop instances
type Builder struct {
	config    *Config
	strategy  Strategy
	iteration Iteration
	deps      *Dependencies
	state     State
}

// NewBuilder creates a new Builder with default configuration
func NewBuilder() *Builder {
	return &Builder{
		config: DefaultConfig(),
	}
}

// WithConfig sets the configuration
func (b *Builder) WithConfig(config *Config) *Builder {
	b.config = config
	return b
}

// WithStrategy sets the strategy
func (b *Builder) WithStrategy(strategy Strategy) *Builder {
	b.strategy = strategy
	return b
}

// WithIteration sets the iteration executor
func (b *Builder) WithIteration(iteration Iteration) *Builder {
	b.iteration = iteration
	return b
}

// WithDependencies sets the dependencies
func (b *Builder) WithDependencies(deps *Dependencies) *Builder {
	b.deps = deps
	return b
}

// WithState sets a custom state (optional, defaults to DefaultState)
func (b *Builder) WithState(state State) *Builder {
	b.state = state
	return b
}

// Build constructs the OrchestratorLoop
func (b *Builder) Build() (*OrchestratorLoop, error) {
	if b.config == nil {
		b.config = DefaultConfig()
	}

	if b.strategy == nil {
		b.strategy = NewDefaultStrategy(b.config)
	}

	if b.iteration == nil {
		return nil, fmt.Errorf("iteration executor is required")
	}

	loop := &OrchestratorLoop{
		config:    b.config,
		strategy:  b.strategy,
		iteration: b.iteration,
		deps:      b.deps,
	}

	if b.state != nil {
		loop.state = b.state
	} else {
		loop.state = NewDefaultState(b.config)
	}

	return loop, nil
}

// MustBuild constructs the OrchestratorLoop and panics on error
func (b *Builder) MustBuild() *OrchestratorLoop {
	loop, err := b.Build()
	if err != nil {
		panic(err)
	}
	return loop
}

// LoopFactory creates pre-configured Loop instances
type LoopFactory struct {
	defaultDeps   *Dependencies
	defaultConfig *Config
}

// NewLoopFactory creates a new LoopFactory
func NewLoopFactory(deps *Dependencies, config *Config) *LoopFactory {
	return &LoopFactory{
		defaultDeps:   deps,
		defaultConfig: config,
	}
}

// CreateDefault creates a default loop
func (f *LoopFactory) CreateDefault() (Loop, error) {
	strategy := NewDefaultStrategy(f.defaultConfig)
	iteration := NewIterationExecutor(f.defaultDeps)

	return NewOrchestratorLoop(f.defaultConfig, strategy, iteration, f.defaultDeps), nil
}

// CreateConservative creates a conservative loop
func (f *LoopFactory) CreateConservative() (Loop, error) {
	strategy := NewConservativeStrategy(f.defaultConfig)
	iteration := NewIterationExecutor(f.defaultDeps)

	return NewOrchestratorLoop(f.defaultConfig, strategy, iteration, f.defaultDeps), nil
}

// CreateAggressive creates an aggressive loop
func (f *LoopFactory) CreateAggressive() (Loop, error) {
	strategy := NewAggressiveStrategy(f.defaultConfig)
	iteration := NewIterationExecutor(f.defaultDeps)

	return NewOrchestratorLoop(f.defaultConfig, strategy, iteration, f.defaultDeps), nil
}

// CreateLLMJudge creates a loop with LLM judge strategy
func (f *LoopFactory) CreateLLMJudge(llmClient llm.Client, modelID string) (Loop, error) {
	factory := NewStrategyFactory(f.defaultConfig)
	strategy := factory.CreateWithLLMJudge("llm-judge", f.defaultConfig, llmClient, modelID, f.defaultDeps.Session)
	iteration := NewIterationExecutor(f.defaultDeps)

	return NewOrchestratorLoop(f.defaultConfig, strategy, iteration, f.defaultDeps), nil
}

// CreateCustom creates a loop with custom configuration
func (f *LoopFactory) CreateCustom(config *Config, strategy Strategy, iteration Iteration) Loop {
	return NewOrchestratorLoop(config, strategy, iteration, f.defaultDeps)
}

// NoOpLoop is a loop that does nothing (useful for testing)
type NoOpLoop struct {
	MockResult *Result
}

// Run returns the mock result or a default success result
func (n *NoOpLoop) Run(ctx context.Context, session Session, progressCb progress.Callback) (*Result, error) {
	if n.MockResult != nil {
		return n.MockResult, nil
	}
	return &Result{
		Success:           true,
		TerminationReason: "no-op",
	}, nil
}

// RunIteration returns a continue outcome
func (n *NoOpLoop) RunIteration(ctx context.Context, state State) (*IterationOutcome, error) {
	return &IterationOutcome{Result: Continue}, nil
}

// GetState returns a new mock state
func (n *NoOpLoop) GetState() State {
	return &MockState{}
}
