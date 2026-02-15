// Copyright 2024-2025 Fionn Langhans
// SPDX-License-Identifier: MIT

package loop_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/orchestrator/loop"
	"github.com/codefionn/scriptschnell/internal/progress"
)

// Example_customLoop demonstrates how to create a custom loop for a specific use case.
// This shows the reusability of the loop abstraction for different LLM-driven workflows.
func Example_customLoop() {
	// This example shows how to create a custom iteration for a specific workflow
	// For demonstration purposes, we'll create a simple "echo" iteration

	// Step 1: Define your custom dependencies
	myDependencies := &loop.Dependencies{
		LLMClient:            &myCustomLLMClient{},
		Session:              &myCustomSession{},
		ToolRegistry:         &myCustomToolRegistry{},
		SystemPromptProvider: &myCustomSystemPromptProvider{},
		ProgressCallback: func(update progress.Update) error {
			fmt.Printf("Progress: %s\n", update.Message)
			return nil
		},
	}

	// Step 2: Create a custom configuration
	config := &loop.Config{
		MaxIterations:                     10,
		MaxAutoContinueAttempts:           3,
		EnableLoopDetection:               true,
		EnableAutoContinue:                true,
		ContextCompactionThresholdPercent: 80,
		MaxConsecutiveCompactions:         2,
	}

	// Step 3: Create a custom strategy (or use a default one)
	strategy := loop.NewDefaultStrategy(config)

	// Step 4: Create a custom iteration (this is where your business logic goes)
	iteration := &myCustomIteration{deps: myDependencies}

	// Step 5: Create the loop
	myLoop := loop.NewOrchestratorLoop(config, strategy, iteration, myDependencies)

	// Step 6: Run the loop
	ctx := context.Background()
	result, err := myLoop.Run(ctx, myDependencies.Session, myDependencies.ProgressCallback)

	if err != nil {
		fmt.Printf("Loop failed: %v\n", err)
		return
	}

	fmt.Printf("Loop completed: %d iterations, success: %v\n", result.IterationsExecuted, result.Success)
}

// myCustomIteration implements the loop.Iteration interface for custom workflows
type myCustomIteration struct {
	deps *loop.Dependencies
}

func (i *myCustomIteration) Execute(ctx context.Context, state loop.State) (*loop.IterationOutcome, error) {
	// Implement your custom iteration logic here
	// This is called once per loop iteration

	outcome := &loop.IterationOutcome{
		Result:  loop.Continue,
		Content: "Custom iteration executed",
	}

	// Example: Check if we should break based on some condition
	if state.Iteration() >= 3 {
		outcome.Result = loop.Break
		outcome.Metadata = map[string]interface{}{
			"custom_field": "custom_value",
		}
	}

	return outcome, nil
}

// myCustomLLMClient is a mock LLM client for the example
type myCustomLLMClient struct{}

func (c *myCustomLLMClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: "Custom response"}, nil
}
func (c *myCustomLLMClient) GetModelName() string      { return "custom-model" }
func (c *myCustomLLMClient) GetLastResponseID() string { return "" }
func (c *myCustomLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	return "Custom completion", nil
}
func (c *myCustomLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}
func (c *myCustomLLMClient) SetPreviousResponseID(id string) {}

// myCustomSession is a mock session for the example
type myCustomSession struct {
	messages []loop.Message
}

func (s *myCustomSession) AddMessage(msg loop.Message) {
	s.messages = append(s.messages, msg)
}
func (s *myCustomSession) GetMessages() []loop.Message {
	return s.messages
}

// myCustomToolRegistry is a mock tool registry for the example
type myCustomToolRegistry struct{}

func (r *myCustomToolRegistry) ToJSONSchema() []map[string]interface{} {
	return nil
}

// myCustomSystemPromptProvider is a mock system prompt provider for the example
type myCustomSystemPromptProvider struct{}

func (p *myCustomSystemPromptProvider) GetSystemPrompt(ctx context.Context) (string, error) {
	return "Custom system prompt", nil
}
func (p *myCustomSystemPromptProvider) GetModelID() string {
	return "custom-model"
}

// Ensure custom types implement the interfaces
var (
	_ loop.Iteration            = (*myCustomIteration)(nil)
	_ llm.Client                = (*myCustomLLMClient)(nil)
	_ loop.Session              = (*myCustomSession)(nil)
	_ loop.ToolRegistry         = (*myCustomToolRegistry)(nil)
	_ loop.SystemPromptProvider = (*myCustomSystemPromptProvider)(nil)
)

// TestCustomLoopReuse demonstrates testing a custom loop implementation
func TestCustomLoopReuse(t *testing.T) {
	t.Run("Custom iteration executes correctly", func(t *testing.T) {
		config := loop.DefaultConfig()
		config.MaxIterations = 3

		session := &myCustomSession{}
		deps := &loop.Dependencies{
			Session: session,
		}

		iteration := &myCustomIteration{deps: deps}
		strategy := loop.NewDefaultStrategy(config)
		myLoop := loop.NewOrchestratorLoop(config, strategy, iteration, deps)

		result, err := myLoop.Run(context.Background(), session, nil)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !result.Success {
			t.Error("Expected successful result")
		}

		// The custom iteration breaks after 3 iterations
		if result.IterationsExecuted != 3 {
			t.Errorf("Expected 3 iterations, got %d", result.IterationsExecuted)
		}
	})
}

// Example_loopFactory demonstrates using the LoopFactory for creating different loop configurations
func Example_loopFactory() {
	deps := &loop.Dependencies{
		LLMClient:    &myCustomLLMClient{},
		Session:      &myCustomSession{},
		ToolRegistry: &myCustomToolRegistry{},
	}

	config := loop.DefaultConfig()
	factory := loop.NewLoopFactory(deps, config)

	// Create different types of loops
	defaultLoop, _ := factory.CreateDefault()
	conservativeLoop, _ := factory.CreateConservative()
	aggressiveLoop, _ := factory.CreateAggressive()

	fmt.Printf("Created %d different loop configurations\n", 3)

	// Use the loops
	_ = defaultLoop
	_ = conservativeLoop
	_ = aggressiveLoop

	// Output: Created 3 different loop configurations
}

// Example_planningAgentLoop demonstrates how the PlanningAgent uses the loop abstraction
// This is a conceptual example showing how planning uses the loop
func Example_planningAgentLoop() {
	// The PlanningAgent uses the loop abstraction like this:

	// 1. Create planning-specific dependencies
	// planningDeps := &planning.PlanningDependencies{
	//     Agent:        planningAgent,
	//     LLMClient:    client,
	//     ToolRegistry: toolRegistry,
	//     Request:      request,
	//     ...
	// }

	// 2. Create planning strategy with question settings
	// strategy := planning.NewPlanningStrategy(config, allowQuestions, maxQuestions)

	// 3. Create planning iteration
	// iteration := planning.NewPlanningIteration(planningDeps)

	// 4. Create and run the loop
	// planningLoop := loop.NewOrchestratorLoop(config, strategy, iteration, deps)
	// result, err := planningLoop.Run(ctx, session, progressCallback)

	// 5. Extract planning result
	// planningResult := planning.ExtractPlanningResult(result)

	fmt.Println("PlanningAgent loop abstraction example")
	// Output: PlanningAgent loop abstraction example
}
