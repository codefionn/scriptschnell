package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// RefactoringAgent manages parallel refactoring by spawning multiple child orchestrators
type RefactoringAgent struct {
	orch *Orchestrator
}

// NewRefactoringAgent creates a new refactoring agent
func NewRefactoringAgent(orch *Orchestrator) *RefactoringAgent {
	return &RefactoringAgent{
		orch: orch,
	}
}

// Refactor executes multiple refactoring objectives in parallel by spawning child orchestrators
func (a *RefactoringAgent) Refactor(ctx context.Context, objectives []string) ([]string, error) {
	if len(objectives) == 0 {
		return nil, fmt.Errorf("at least one objective is required")
	}

	// For a single objective, run directly
	if len(objectives) == 1 {
		result, err := a.refactorInternal(ctx, objectives[0], nil)
		if err != nil {
			return nil, err
		}
		return []string{result}, nil
	}

	// For multiple objectives, run concurrently
	type refactoringResult struct {
		index  int
		result string
		err    error
	}

	resultChan := make(chan refactoringResult, len(objectives))
	var wg sync.WaitGroup

	for i, objective := range objectives {
		wg.Add(1)
		go func(idx int, obj string) {
			defer wg.Done()
			result, err := a.refactorInternal(ctx, obj, nil)
			resultChan <- refactoringResult{index: idx, result: result, err: err}
		}(i, objective)
	}

	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make([]string, len(objectives))
	var firstErr error
	for res := range resultChan {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		results[res.index] = res.result
	}

	if firstErr != nil {
		return results, firstErr
	}

	return results, nil
}

// refactorInternal handles a single refactoring objective by creating a child orchestrator
func (a *RefactoringAgent) refactorInternal(ctx context.Context, objective string, progressCb progress.Callback) (string, error) {
	// Get progress callback from orchestrator if not provided
	if progressCb == nil {
		progressCb = a.orch.GetCurrentProgressCallback()
	}

	// Create status and stream callbacks
	sendStatus := func(msg string) {
		dispatchProgress(progressCb, progress.Update{
			Message:   msg,
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		})
	}

	sendStream := func(msg string) {
		dispatchProgress(progressCb, progress.Update{
			Message: msg,
			Mode:    progress.ReportNoStatus,
		})
	}

	// Send initial progress message
	sendStream(fmt.Sprintf("\n\n🔧 **Refactoring task**: %s\n\n", objective))
	sendStatus(fmt.Sprintf("→ Starting refactoring: %s", objective))

	// Create a child orchestrator with shared resources
	childOrch, err := a.createChildOrchestrator()
	if err != nil {
		return "", fmt.Errorf("failed to create child orchestrator: %w", err)
	}
	defer childOrch.Close()

	// Create a progress callback wrapper
	childProgressCb := func(update progress.Update) error {
		if update.Message == "" && !update.ShouldStatus() {
			return nil
		}
		if update.ShouldStatus() {
			update.Ephemeral = true
		}
		return progress.Dispatch(progressCb, progress.Normalize(update))
	}

	// Execute the refactoring using the child orchestrator's process method
	sendStatus(fmt.Sprintf("→ Executing refactoring: %s", objective))

	err = childOrch.ProcessPrompt(ctx, objective, childProgressCb, nil, nil, nil, nil, nil)
	if err != nil {
		return fmt.Sprintf("Error during refactoring: %v", err), err
	}

	sendStatus(fmt.Sprintf("✓ Completed refactoring: %s", objective))

	// Get the session and return the messages as result
	var output strings.Builder
	output.WriteString(fmt.Sprintf("## Refactoring Result: %s\n\n", objective))

	// Get the assistant's response from the child session
	messages := childOrch.session.GetMessages()
	for _, msg := range messages {
		if msg.Role == "assistant" {
			output.WriteString(msg.Content)
			break
		}
	}

	return output.String(), nil
}

// createChildOrchestrator creates a child orchestrator with a filtered tool registry
// The child orchestrator does not have access to the refactoring_agent tool to prevent recursive spawning
func (a *RefactoringAgent) createChildOrchestrator() (*Orchestrator, error) {
	// Create a deep copy of the config using JSON marshaling to avoid copying mutex fields
	var cfg *config.Config
	if a.orch.config != nil {
		data, err := json.Marshal(a.orch.config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}
		cfg = &config.Config{}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	} else {
		cfg = config.DefaultConfig()
	}

	// Create a session for this child
	childSession := session.NewSession(session.GenerateID(), a.orch.workingDir)

	// Create child orchestrator with shared resources (filesystem, session storage, domain blocker)
	childOrch, err := NewOrchestratorWithSharedResources(
		cfg,
		a.orch.providerMgr,
		a.orch.cliMode,
		a.orch.fs,
		childSession,
		a.orch.sessionStorageRef,
		a.orch.domainBlockerRef,
		false, // requireSandboxAuth
	)
	if err != nil {
		return nil, err
	}

	// Filter out the refactoring_agent tool from the child's tool registry
	childOrch.filterRefactoringAgentTool()

	return childOrch, nil
}

// filterRefactoringAgentTool removes the refactoring_agent tool from the orchestrator's tool registry
// to prevent recursive spawning of refactoring agents
func (o *Orchestrator) filterRefactoringAgentTool() {
	if o.toolRegistry != nil {
		o.toolRegistry.RemoveByPrefix(tools.ToolNameRefactoringAgent)
	}
}
