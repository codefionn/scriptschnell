// Package loop provides an abstraction for the orchestrator loop,
// enabling testability, modularity, and customizable execution strategies.
//
// # Overview
//
// The loop abstraction separates concerns into distinct interfaces and implementations:
//
//   - State: Manages iteration counters, limits, and loop detection state
//   - Strategy: Determines when and how the loop should continue or terminate
//   - Iteration: Executes a single iteration (LLM call + tool execution)
//   - Loop: Orchestrates multiple iterations using the provided strategy
//
// # Usage
//
// Basic usage with default configuration:
//
//	deps := &loop.Dependencies{
//	    LLMClient:     client,
//	    Session:       session,
//	    ToolRegistry:  registry,
//	    // ... other dependencies
//	}
//
//	config := loop.DefaultConfig()
//	strategy := loop.NewDefaultStrategy(config)
//	iteration := loop.NewIterationExecutor(deps)
//
//	orchestratorLoop := loop.NewOrchestratorLoop(config, strategy, iteration, deps)
//	result, err := orchestratorLoop.Run(ctx, session, progressCallback)
//
// Using the builder pattern:
//
//	loop, err := loop.NewBuilder().
//	    WithConfig(config).
//	    WithStrategy(strategy).
//	    WithIteration(iteration).
//	    WithDependencies(deps).
//	    Build()
//
// Using the factory for pre-configured loops:
//
//	factory := loop.NewLoopFactory(deps, config)
//	conservativeLoop, _ := factory.CreateConservative()
//	aggressiveLoop, _ := factory.CreateAggressive()
//
// # Reusing the Loop Abstraction
//
// The loop abstraction is designed to be reusable for different LLM-driven workflows.
// The PlanningAgent demonstrates how to reuse this package:
//
// 1. Define your custom Iteration implementation:
//
//	type MyIteration struct {
//	    deps *MyDependencies
//	}
//
//	func (i *MyIteration) Execute(ctx context.Context, state loop.State) (*loop.IterationOutcome, error) {
//	    // Your custom iteration logic here
//	    outcome := &loop.IterationOutcome{Result: loop.Continue}
//	    // ... execute LLM call, process tools, etc.
//	    return outcome, nil
//	}
//
// 2. Define your custom Strategy (or use existing ones):
//
//	type MyStrategy struct{}
//
//	func (s *MyStrategy) ShouldContinue(state loop.State, outcome *loop.IterationOutcome) bool {
//	    // Your custom continue logic
//	    return outcome.Result == loop.Continue
//	}
//
//	func (s *MyStrategy) ShouldAutoContinue(state loop.State, content string) bool {
//	    // Your auto-continue logic
//	    return false
//	}
//
//	func (s *MyStrategy) GetResult(state loop.State, lastOutcome *loop.IterationOutcome, terminatedEarly bool) *loop.Result {
//	    // Return final result
//	    return &loop.Result{Success: true}
//	}
//
// 3. Create Session and Message interfaces:
//
//	type MyMessage struct {
//	    Role, Content string
//	}
//
//	func (m *MyMessage) GetRole() string    { return m.Role }
//	func (m *MyMessage) GetContent() string { return m.Content }
//	// ... implement other Message interface methods
//
//	type MySession struct {
//	    messages []loop.Message
//	}
//
//	func (s *MySession) AddMessage(msg loop.Message) { s.messages = append(s.messages, msg) }
//	func (s *MySession) GetMessages() []loop.Message { return s.messages }
//
// 4. Create and run the loop:
//
//	config := loop.DefaultConfig()
//	strategy := &MyStrategy{}
//	iteration := &MyIteration{deps: myDeps}
//
//	myLoop := loop.NewOrchestratorLoop(config, strategy, iteration, &loop.Dependencies{
//	    LLMClient: myClient,
//	    Session:   mySession,
//	    // ...
//	})
//
//	result, err := myLoop.Run(ctx, mySession, progressCallback)
//
// # Strategies
//
// The package provides several built-in strategies:
//
//   - DefaultStrategy: Balanced approach with standard limits
//   - ConservativeStrategy: Stops earlier to prevent excessive token usage
//   - AggressiveStrategy: Continues more aggressively for batch operations
//
// Custom strategies can be implemented by implementing the Strategy interface.
//
// # Testing
//
// The package includes mock implementations for testing:
//
//   - MockState: Configurable mock state for controlled testing
//   - MockIteration: Mock iteration with customizable outcomes
//   - NoOpLoop: No-op loop implementation for testing
//
// Example test:
//
//	state := &loop.MockState{
//	    MockMaxIterations: 5,
//	    MockIteration:     0,
//	}
//
//	iteration := &loop.MockIteration{
//	    MockOutcome: &loop.IterationOutcome{
//	        Result:  loop.Break,
//	        Content: "Test response",
//	    },
//	}
//
//	strategy := loop.NewDefaultStrategy(loop.DefaultConfig())
//	loop := loop.NewOrchestratorLoop(config, strategy, iteration, deps)
//
// # Integration
//
// The loop abstraction is designed to integrate with the existing orchestrator.
// The Orchestrator can use the loop abstraction by:
//
// 1. Creating appropriate dependencies
// 2. Configuring the loop with desired settings
// 3. Calling Run() instead of the inline loop
//
// See the orchestrator package for the integration implementation.
//
// See example_reuse_test.go for complete examples of reusing the loop abstraction.
package loop
