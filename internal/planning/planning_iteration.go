package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
	"github.com/codefionn/scriptschnell/internal/orchestrator/loop"
	"github.com/codefionn/scriptschnell/internal/progress"
)

// PlanningDependencies contains the dependencies required by PlanningIteration
type PlanningDependencies struct {
	// Agent is the planning agent (provides methods like buildSystemPrompt, extractPlan)
	Agent *PlanningAgent

	// LLMClient is the planning LLM client
	LLMClient llm.Client

	// ToolRegistry provides access to planning tools
	ToolRegistry *PlanningToolRegistry

	// Request is the planning request
	Request *PlanningRequest

	// UserInputCallback is called when user input is needed
	UserInputCallback UserInputCallback

	// ProgressCallback sends progress updates to the UI
	ProgressCallback progress.Callback

	// ToolCallCallback is called when a planning tool is being executed
	ToolCallCallback ToolCallCallback

	// ToolResultCallback is called when a planning tool execution completes
	ToolResultCallback ToolResultCallback

	// LoopDetector detects repetitive patterns
	LoopDetector *loopdetector.LoopDetector

	// Messages holds the conversation messages
	Messages []*llm.Message

	// QuestionsAsked tracks the number of questions asked
	QuestionsAsked int
}

// PlanningIteration implements the loop.Iteration interface for planning workflows
type PlanningIteration struct {
	deps *PlanningDependencies
}

// NewPlanningIteration creates a new PlanningIteration
func NewPlanningIteration(deps *PlanningDependencies) *PlanningIteration {
	return &PlanningIteration{deps: deps}
}

// Execute runs a single iteration of the planning loop
func (p *PlanningIteration) Execute(ctx context.Context, state loop.State) (*loop.IterationOutcome, error) {
	outcome := &loop.IterationOutcome{Result: loop.Continue}

	streamPlanning := func(msg string) {
		if p.deps.ProgressCallback == nil {
			return
		}
		if strings.TrimSpace(msg) == "" {
			return
		}
		if err := progress.Dispatch(p.deps.ProgressCallback, progress.Normalize(progress.Update{
			Message: msg,
			Mode:    progress.ReportNoStatus,
		})); err != nil {
			logger.Debug("planning stream callback error: %v", err)
		}
	}

	updateStatus := func(msg string) {
		if p.deps.ProgressCallback == nil {
			return
		}
		if strings.TrimSpace(msg) == "" {
			return
		}
		if err := progress.Dispatch(p.deps.ProgressCallback, progress.Update{
			Message:   msg,
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		}); err != nil {
			logger.Debug("planning status callback error: %v", err)
		}
	}

	// Build system prompt
	systemPrompt := p.buildSystemPrompt(p.deps.Request)

	// Create completion request
	completeReq := &llm.CompletionRequest{
		Messages:      p.deps.Messages,
		Tools:         p.deps.ToolRegistry.ToJSONSchema(),
		Temperature:   1.0, // Some reasoning models only support temperature=1
		MaxTokens:     consts.DefaultMaxTokens,
		SystemPrompt:  systemPrompt,
		EnableCaching: true,
		CacheTTL:      "5m",
	}

	// Get response from planning model with retry logic
	response, err := p.completeWithRetry(ctx, completeReq, updateStatus, streamPlanning)
	if err != nil {
		outcome.Result = loop.Error
		outcome.Error = fmt.Errorf("planning completion failed: %w", err)
		return outcome, outcome.Error
	}

	// Ensure tool calls always carry an ID
	response.ToolCalls = llm.NormalizeToolCallIDs(response.ToolCalls)

	outcome.Response = response
	outcome.Content = response.Content
	outcome.Reasoning = response.Reasoning
	outcome.ToolCalls = response.ToolCalls
	outcome.HasToolCalls = len(response.ToolCalls) > 0

	// Check for text loops in the response
	if response.Content != "" && p.deps.LoopDetector != nil {
		isLoop, pattern, count := p.deps.LoopDetector.AddText(response.Content)
		if isLoop {
			logger.Warn("Text loop detected in planning: pattern repeated %d times", count)
			displayPattern := pattern
			if len(displayPattern) > 100 {
				displayPattern = displayPattern[:100] + "..."
			}
			if err := progress.Dispatch(p.deps.ProgressCallback, progress.Normalize(progress.Update{
				Message: fmt.Sprintf("\n\n🔁 Loop detected! The planning model is repeating the same text pattern %d times.\nPattern: %s\nStopping planning to prevent infinite loop.\n", count, displayPattern),
				Mode:    progress.ReportNoStatus,
			})); err != nil {
				logger.Debug("planning loop detection callback error: %v", err)
			}
			outcome.Result = loop.BreakLoopDetected
			outcome.Metadata = map[string]interface{}{
				"loop_pattern": pattern,
				"loop_count":   count,
				"partial_plan": true,
			}
			return outcome, nil
		}
	}

	// Stream planning content to the UI if available
	if response.Content != "" {
		streamPlanning(response.Content)
	}

	// Add assistant response to messages
	p.deps.Messages = append(p.deps.Messages, &llm.Message{
		Role:      "assistant",
		Content:   response.Content,
		Reasoning: response.Reasoning,
		ToolCalls: response.ToolCalls,
	})

	// Process tool calls if any
	if len(response.ToolCalls) > 0 {
		toolResults, needsInput, askedCount, err := p.processToolCalls(ctx, response.ToolCalls)
		if err != nil {
			outcome.Result = loop.Error
			outcome.Error = fmt.Errorf("tool execution failed: %w", err)
			return outcome, outcome.Error
		}

		// Add tool results to messages
		p.deps.Messages = append(p.deps.Messages, toolResults...)

		// Update questions asked counter
		p.deps.QuestionsAsked += askedCount

		// If we need user input and have exhausted questions, mark as needing input
		if needsInput && (!p.deps.Request.AllowQuestions ||
			(p.deps.Request.MaxQuestions > 0 && p.deps.QuestionsAsked >= p.deps.Request.MaxQuestions)) {
			logger.Debug("Planning needs user input but max questions reached")
			outcome.Result = loop.Break
			outcome.Metadata = map[string]interface{}{
				"needs_input":         true,
				"questions_exhausted": true,
				"partial_plan":        true,
			}
			return outcome, nil
		}

		// Continue to next iteration to get refined plan
		outcome.Result = loop.Continue
		return outcome, nil
	}

	// No tool calls, check if we have a complete plan
	planResp := p.extractPlan(response.Content)
	if planResp != nil {
		hasPlanContent := len(planResp.Plan) > 0
		hasBoardContent := planResp.Board != nil && len(planResp.Board.PrimaryTasks) > 0
		hasContent := hasPlanContent || hasBoardContent || planResp.Complete
		needsUserInput := planResp.NeedsInput && p.deps.Request.AllowQuestions

		if hasContent || needsUserInput {
			logger.Debug("Planning completed successfully")
			outcome.Result = loop.Break
			outcome.Metadata = map[string]interface{}{
				"plan_response": planResp,
				"needs_input":   planResp.NeedsInput,
				"complete":      planResp.Complete,
				"has_content":   hasContent,
			}
			return outcome, nil
		}
	}

	// If we can't extract a plan, continue to next iteration
	// The orchestrator loop will handle max iterations
	outcome.Result = loop.Continue
	return outcome, nil
}

// toolCallResult represents the result of a single tool execution
type planningToolCallResult struct {
	message        *llm.Message
	needsInput     bool
	questionsAsked int
	err            error
}

// processToolCalls executes the planning agent's tool calls
func (p *PlanningIteration) processToolCalls(ctx context.Context, toolCalls []map[string]interface{}) ([]*llm.Message, bool, int, error) {
	var (
		wg                    sync.WaitGroup
		results               = make([]*planningToolCallResult, len(toolCalls))
		askUserMu             sync.Mutex
		questionsAskedInBatch int
	)

	for i, toolCall := range toolCalls {
		wg.Add(1)
		go func(idx int, call map[string]interface{}) {
			defer wg.Done()

			res := &planningToolCallResult{}
			results[idx] = res

			if ctx.Err() != nil {
				res.err = ctx.Err()
				return
			}

			toolID, _ := call["id"].(string)
			toolType, _ := call["type"].(string)

			if toolType != "function" {
				errorMsg := fmt.Sprintf("Invalid tool type: %s", toolType)
				res.message = &llm.Message{
					Role:     "tool",
					Content:  errorMsg,
					ToolID:   toolID,
					ToolName: "unknown",
				}
				if p.deps.ToolResultCallback != nil {
					if err := p.deps.ToolResultCallback("unknown", toolID, errorMsg, errorMsg); err != nil {
						logger.Warn("Failed to send planning tool result message: %v", err)
					}
				}
				return
			}

			function, ok := call["function"].(map[string]interface{})
			if !ok {
				errorMsg := "Invalid function format in tool call"
				res.message = &llm.Message{
					Role:     "tool",
					Content:  errorMsg,
					ToolID:   toolID,
					ToolName: "unknown",
				}
				if p.deps.ToolResultCallback != nil {
					if err := p.deps.ToolResultCallback("unknown", toolID, errorMsg, errorMsg); err != nil {
						logger.Warn("Failed to send planning tool result message: %v", err)
					}
				}
				return
			}

			toolName, _ := function["name"].(string)
			argsJSON, _ := function["arguments"].(string)

			logger.Debug("Planning agent executing tool: %s", toolName)

			var args map[string]interface{}
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				res.message = &llm.Message{
					Role:     "tool",
					Content:  fmt.Sprintf("Error parsing tool arguments: %v", err),
					ToolID:   toolID,
					ToolName: toolName,
				}
				return
			}

			// Notify UI about tool call details
			if p.deps.ToolCallCallback != nil {
				if err := p.deps.ToolCallCallback(toolName, toolID, args); err != nil {
					logger.Warn("Failed to send planning tool call message: %v", err)
				}
			}

			var toolResult string

			switch toolName {
			case "ask_user":
				askUserMu.Lock()
				func() {
					defer askUserMu.Unlock()

					if !p.deps.Request.AllowQuestions {
						toolResult = "User questions are not allowed for this planning request"
					} else if p.deps.UserInputCallback == nil {
						toolResult = "No user input callback available"
						res.needsInput = true
					} else if p.deps.Request.MaxQuestions > 0 &&
						(p.deps.QuestionsAsked+questionsAskedInBatch) >= p.deps.Request.MaxQuestions {
						toolResult = "Maximum number of questions reached, cannot ask more questions"
						res.needsInput = true
					} else {
						question, _ := args["question"].(string)
						if question == "" {
							toolResult = "No question provided"
						} else {
							// Check if options are provided
							var questionText string
							if opts, hasOptions := args["options"]; hasOptions {
								if optsArray, ok := opts.([]interface{}); ok && len(optsArray) == 3 {
									var questionsText strings.Builder
									questionsText.WriteString(fmt.Sprintf("1. %s\n", question))
									for j, opt := range optsArray {
										if optStr, ok := opt.(string); ok {
											questionsText.WriteString(fmt.Sprintf("   %c. %s\n", 'a'+j, optStr))
										}
									}
									questionText = questionsText.String()
								} else {
									questionText = question
								}
							} else {
								questionText = question
							}

							userResponse, err := p.deps.UserInputCallback(questionText)
							if err != nil {
								toolResult = fmt.Sprintf("Failed to get user input: %v", err)
								res.needsInput = true
							} else {
								toolResult = userResponse
								res.questionsAsked = 1
								questionsAskedInBatch++
							}
						}
					}
				}()

			case "ask_user_multiple":
				askUserMu.Lock()
				func() {
					defer askUserMu.Unlock()

					if !p.deps.Request.AllowQuestions {
						toolResult = "User questions are not allowed for this planning request"
					} else if p.deps.UserInputCallback == nil {
						toolResult = "No user input callback available"
						res.needsInput = true
					} else if p.deps.Request.MaxQuestions > 0 &&
						(p.deps.QuestionsAsked+questionsAskedInBatch) >= p.deps.Request.MaxQuestions {
						toolResult = "Maximum number of questions reached, cannot ask more questions"
						res.needsInput = true
					} else {
						questions, _ := args["questions"].([]interface{})
						if len(questions) == 0 {
							toolResult = "No questions provided"
						} else {
							// Format questions for user callback
							var questionsText strings.Builder
							for i, q := range questions {
								if questionMap, ok := q.(map[string]interface{}); ok {
									question, _ := questionMap["question"].(string)
									options, _ := questionMap["options"].([]interface{})
									questionsText.WriteString(fmt.Sprintf("%d. %s\n", i+1, question))
									for j, opt := range options {
										if optStr, ok := opt.(string); ok {
											questionsText.WriteString(fmt.Sprintf("   %c. %s\n", 'a'+j, optStr))
										}
									}
									questionsText.WriteString("\n")
								}
							}

							userResponse, err := p.deps.UserInputCallback(questionsText.String())
							if err != nil {
								toolResult = fmt.Sprintf("Failed to get user input: %v", err)
								res.needsInput = true
							} else {
								toolResult = userResponse
								res.questionsAsked = len(questions)
								questionsAskedInBatch += len(questions)
							}
						}
					}
				}()

			default:
				result := p.deps.ToolRegistry.Execute(ctx, toolName, args)
				if result.Error != "" {
					toolResult = fmt.Sprintf("Error: %s", result.Error)
				} else {
					if resultMap, ok := result.Result.(map[string]interface{}); ok {
						if jsonBytes, err := json.Marshal(resultMap); err == nil {
							toolResult = string(jsonBytes)
						} else {
							toolResult = fmt.Sprintf("%v", result.Result)
						}
					} else {
						toolResult = fmt.Sprintf("%v", result.Result)
					}
				}
			}

			// Notify UI about tool result
			if p.deps.ToolResultCallback != nil {
				var errorMsg string
				if strings.HasPrefix(toolResult, "Error:") {
					errorMsg = toolResult
				}
				if err := p.deps.ToolResultCallback(toolName, toolID, toolResult, errorMsg); err != nil {
					logger.Warn("Failed to send planning tool result message: %v", err)
				}
			}

			res.message = &llm.Message{
				Role:     "tool",
				Content:  toolResult,
				ToolID:   toolID,
				ToolName: toolName,
			}
		}(i, toolCall)
	}

	wg.Wait()

	var finalResults []*llm.Message
	var needsInput bool
	var questionsAsked int

	for _, res := range results {
		if res == nil {
			continue
		}
		if res.err != nil {
			return nil, false, 0, res.err
		}
		if res.message != nil {
			finalResults = append(finalResults, res.message)
		}
		if res.needsInput {
			needsInput = true
		}
		questionsAsked += res.questionsAsked
	}

	return finalResults, needsInput, questionsAsked, nil
}

// completeWithRetry wraps LLM completion with retry logic
func (p *PlanningIteration) completeWithRetry(ctx context.Context, req *llm.CompletionRequest, statusCb func(string), streamCb func(string)) (*llm.CompletionResponse, error) {
	maxAttempts := consts.PlanningMaxRetries

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		response, err := p.deps.LLMClient.CompleteWithRequest(ctx, req)
		if err == nil {
			return response, nil
		}

		logger.Warn("Planning completion error (attempt %d/%d): %v", attempt, maxAttempts, err)

		// Last attempt - return the error
		if attempt >= maxAttempts {
			return nil, err
		}

		// Check for context cancellation (both actual context and error-wrapped)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		// Determine sleep duration with exponential backoff.
		// Only retry errors that look transient; return immediately for others.
		errStr := strings.ToLower(err.Error())
		var sleepSeconds int
		isTransient := false
		switch {
		case strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "429"):
			sleepSeconds = 5 * (1 << uint(attempt-1))
			if sleepSeconds > 120 {
				sleepSeconds = 120
			}
			isTransient = true
		case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline"):
			sleepSeconds = attempt * 3
			isTransient = true
		case strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
			strings.Contains(errStr, "503") || strings.Contains(errStr, "overloaded"):
			sleepSeconds = attempt * 3
			isTransient = true
		}

		// Don't retry errors that aren't known to be transient
		if !isTransient {
			return nil, err
		}

		// Notify user about retry
		if streamCb != nil {
			streamCb(fmt.Sprintf("\n⏳ Planning: retrying in %d seconds... (attempt %d/%d)\n", sleepSeconds, attempt, maxAttempts))
		}
		if statusCb != nil {
			statusCb(fmt.Sprintf("Planning: retrying in %ds...", sleepSeconds))
		}

		// Sleep before retry
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(sleepSeconds) * time.Second):
			// Continue to retry
		}

		if statusCb != nil {
			statusCb(fmt.Sprintf("Planning: retrying (attempt %d/%d)...", attempt+1, maxAttempts))
		}
	}

	return nil, fmt.Errorf("planning: max retry attempts (%d) exceeded", maxAttempts)
}

// buildSystemPrompt creates the system prompt for the planning agent
func (p *PlanningIteration) buildSystemPrompt(req *PlanningRequest) string {
	if p.deps.Agent != nil {
		return p.deps.Agent.buildSystemPrompt(req)
	}
	return ""
}

// extractPlan extracts a structured plan from the LLM response
func (p *PlanningIteration) extractPlan(content string) *PlanningResponse {
	if p.deps.Agent != nil {
		return p.deps.Agent.extractPlan(content)
	}
	return nil
}

// Ensure PlanningIteration implements loop.Iteration
var _ loop.Iteration = (*PlanningIteration)(nil)
