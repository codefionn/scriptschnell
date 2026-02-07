package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// MIN_SLEEP_SECONDS is the minimum sleep duration for retries
const MIN_SLEEP_SECONDS = 1

// ErrorJudgeDecision represents the error judge's decision
type ErrorJudgeDecision struct {
	ShouldRetry       bool
	SleepSeconds      int
	Reason            string
	TriggerCompaction bool // If true, the error is likely due to context size exceeded and compaction should be triggered
}

// ErrorJudgeMessage contains information for the error judge
type ErrorJudgeMessage struct {
	Error         error
	AttemptNumber int
	MaxAttempts   int
	ModelID       string
	RequestCtx    context.Context
	ResponseChan  chan ErrorJudgeDecision
}

// Type implements actor.Message interface
func (m *ErrorJudgeMessage) Type() string {
	return "tools_error_judge"
}

// ErrorJudgeActor analyzes LLM errors and decides whether to retry
type ErrorJudgeActor struct {
	id        string
	llmClient llm.Client
}

// NewErrorJudgeActor creates a new error judge actor
func NewErrorJudgeActor(id string, llmClient llm.Client) *ErrorJudgeActor {
	return &ErrorJudgeActor{
		id:        id,
		llmClient: llmClient,
	}
}

// ID implements actor.Actor interface
func (a *ErrorJudgeActor) ID() string {
	return a.id
}

// Start implements actor.Actor interface
func (a *ErrorJudgeActor) Start(ctx context.Context) error {
	logger.Debug("Error judge actor %s started", a.id)
	return nil
}

// Stop implements actor.Actor interface
func (a *ErrorJudgeActor) Stop(ctx context.Context) error {
	logger.Debug("Error judge actor %s stopped", a.id)
	return nil
}

// Receive implements actor.Actor interface
func (a *ErrorJudgeActor) Receive(ctx context.Context, message actor.Message) error {
	msg, ok := message.(*ErrorJudgeMessage)
	if !ok {
		return fmt.Errorf("invalid message type: expected ErrorJudgeMessage, got %T", message)
	}

	// Process the error and make a decision
	var decision ErrorJudgeDecision

	// If no LLM client available, use heuristic fallback
	if a.llmClient == nil {
		logger.Debug("Error judge using heuristic fallback (no LLM client)")
		decision = a.heuristicJudge(msg)
	} else {
		// Use LLM to analyze the error
		var err error
		decision, err = a.llmJudge(msg.RequestCtx, msg)
		if err != nil {
			logger.Warn("Error judge LLM analysis failed, falling back to heuristics: %v", err)
			decision = a.heuristicJudge(msg)
		}
	}

	// Send response back
	select {
	case msg.ResponseChan <- decision:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-msg.RequestCtx.Done():
		return msg.RequestCtx.Err()
	}
}

func (a *ErrorJudgeActor) llmJudge(ctx context.Context, msg *ErrorJudgeMessage) (ErrorJudgeDecision, error) {
	// Build prompt for error analysis
	prompt := a.buildErrorJudgePrompt(msg)

	// Call LLM with timeout
	judgeCtx, cancel := context.WithTimeout(ctx, consts.Timeout10Seconds)
	defer cancel()

	result, err := a.llmClient.Complete(judgeCtx, prompt)
	if err != nil {
		return ErrorJudgeDecision{}, err
	}

	// Parse the decision
	return a.parseDecision(result, msg)
}

func (a *ErrorJudgeActor) buildErrorJudgePrompt(msg *ErrorJudgeMessage) string {
	var sb strings.Builder

	sb.WriteString("You are an error recovery judge for an LLM-powered application.\n")
	sb.WriteString("Analyze the error and decide whether to retry the request or halt.\n\n")

	sb.WriteString("Your response must be in this exact format:\n")
	sb.WriteString("DECISION: RETRY or HALT\n")
	sb.WriteString("SLEEP_SECONDS: <number>\n")
	sb.WriteString("TRIGGER_COMPACTION: YES or NO\n")
	sb.WriteString("REASON: <brief explanation>\n\n")

	sb.WriteString("Guidelines:\n")
	sb.WriteString("- Rate limit errors: RETRY with exponential backoff (5s, 15s, 30s, 60s)\n")
	sb.WriteString("- Temporary service errors (500, 503, timeout): RETRY with moderate delays (2s, 5s, 10s, 20s, 30s)\n")
	sb.WriteString("- Network errors: RETRY with short delays (1s, 3s, 5s)\n")
	sb.WriteString("- Token/context limit errors (context_length_exceeded, max tokens, prompt too long, input too long): RETRY with TRIGGER_COMPACTION=YES\n")
	sb.WriteString("- Authentication errors: HALT (invalid credentials)\n")
	sb.WriteString("- Invalid request/parameter errors: HALT (bad input)\n")
	sb.WriteString("- Unknown errors after 3+ attempts: HALT (prevent infinite loops)\n\n")

	sb.WriteString(fmt.Sprintf("Current attempt: %d of %d\n", msg.AttemptNumber, msg.MaxAttempts))
	sb.WriteString(fmt.Sprintf("Model: %s\n", msg.ModelID))
	sb.WriteString(fmt.Sprintf("Error: %v\n\n", msg.Error))

	sb.WriteString("Analyze this error and provide your decision in the exact format above.")

	return sb.String()
}

func (a *ErrorJudgeActor) parseDecision(response string, msg *ErrorJudgeMessage) (ErrorJudgeDecision, error) {
	lines := strings.Split(response, "\n")
	decision := ErrorJudgeDecision{}

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "DECISION:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "DECISION:"))
			decision.ShouldRetry = strings.EqualFold(value, "RETRY")
		} else if strings.HasPrefix(line, "SLEEP_SECONDS:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "SLEEP_SECONDS:"))
			if _, err := fmt.Sscanf(value, "%d", &decision.SleepSeconds); err != nil {
				decision.SleepSeconds = 0
			}
		} else if strings.HasPrefix(line, "TRIGGER_COMPACTION:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "TRIGGER_COMPACTION:"))
			decision.TriggerCompaction = strings.EqualFold(value, "YES")
		} else if strings.HasPrefix(line, "REASON:") {
			decision.Reason = strings.TrimSpace(strings.TrimPrefix(line, "REASON:"))
		}
	}

	// Validate and cap sleep duration
	if decision.SleepSeconds < 0 {
		decision.SleepSeconds = 0
	}
	if decision.SleepSeconds > 120 {
		decision.SleepSeconds = 120 // Max 2 minutes
	}

	// Enforce minimum sleep for retries
	if decision.ShouldRetry && decision.SleepSeconds < MIN_SLEEP_SECONDS {
		decision.SleepSeconds = MIN_SLEEP_SECONDS
	}

	// Don't retry if we've exceeded max attempts
	if msg.AttemptNumber >= msg.MaxAttempts {
		decision.ShouldRetry = false
		if decision.Reason == "" {
			decision.Reason = "Maximum retry attempts reached"
		}
	}

	return decision, nil
}

func (a *ErrorJudgeActor) heuristicJudge(msg *ErrorJudgeMessage) ErrorJudgeDecision {
	errMsg := strings.ToLower(msg.Error.Error())

	// Check if we've exceeded max attempts
	if msg.AttemptNumber >= msg.MaxAttempts {
		return ErrorJudgeDecision{
			ShouldRetry:  false,
			SleepSeconds: 0,
			Reason:       "Maximum retry attempts reached",
		}
	}

	// Rate limit errors - exponential backoff
	if strings.Contains(errMsg, "rate limit") ||
		strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "too many requests") {
		sleepSeconds := calculateExponentialBackoff(msg.AttemptNumber, 5, 60)
		return ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: sleepSeconds,
			Reason:       "Rate limit error, retrying with exponential backoff",
		}
	}

	// Temporary service errors
	if strings.Contains(errMsg, "500") ||
		strings.Contains(errMsg, "503") ||
		strings.Contains(errMsg, "service unavailable") ||
		strings.Contains(errMsg, "internal server error") ||
		strings.Contains(errMsg, "timeout") {
		sleepSeconds := calculateLinearBackoff(msg.AttemptNumber, 2, 30)
		return ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: sleepSeconds,
			Reason:       "Temporary service error, retrying with backoff",
		}
	}

	// Network errors
	if strings.Contains(errMsg, "connection") ||
		strings.Contains(errMsg, "network") ||
		strings.Contains(errMsg, "dial") ||
		strings.Contains(errMsg, "eof") {
		sleepSeconds := calculateLinearBackoff(msg.AttemptNumber, 1, 5)
		return ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: sleepSeconds,
			Reason:       "Network error, retrying with short delay",
		}
	}

	// Token/context limit errors - trigger compaction and retry
	if strings.Contains(errMsg, "token") ||
		strings.Contains(errMsg, "context length") ||
		strings.Contains(errMsg, "context_length") ||
		strings.Contains(errMsg, "max_tokens") ||
		strings.Contains(errMsg, "maximum context") ||
		strings.Contains(errMsg, "input too long") ||
		strings.Contains(errMsg, "prompt is too long") ||
		strings.Contains(errMsg, "request too large") ||
		strings.Contains(errMsg, "exceeds the model") {
		return ErrorJudgeDecision{
			ShouldRetry:       true,
			SleepSeconds:      MIN_SLEEP_SECONDS,
			Reason:            "Context size exceeded, triggering compaction",
			TriggerCompaction: true,
		}
	}

	// Authentication errors
	if strings.Contains(errMsg, "auth") ||
		strings.Contains(errMsg, "401") ||
		strings.Contains(errMsg, "api key") ||
		strings.Contains(errMsg, "unauthorized") {
		return ErrorJudgeDecision{
			ShouldRetry:  false,
			SleepSeconds: 0,
			Reason:       "Authentication error, check API key configuration",
		}
	}

	// Invalid request errors
	if strings.Contains(errMsg, "400") ||
		strings.Contains(errMsg, "bad request") ||
		strings.Contains(errMsg, "invalid") {
		return ErrorJudgeDecision{
			ShouldRetry:  false,
			SleepSeconds: 0,
			Reason:       "Invalid request error, cannot retry",
		}
	}

	// OpenRouter: model does not support tool use
	if strings.Contains(errMsg, "no endpoints found") && strings.Contains(errMsg, "tool use") {
		return ErrorJudgeDecision{
			ShouldRetry:  false,
			SleepSeconds: 0,
			Reason:       "Model does not support tool use via OpenRouter",
		}
	}

	// Unknown error - retry a few times with moderate delay
	if msg.AttemptNumber < 3 {
		sleepSeconds := msg.AttemptNumber*3 + MIN_SLEEP_SECONDS
		return ErrorJudgeDecision{
			ShouldRetry:  true,
			SleepSeconds: sleepSeconds,
			Reason:       "Unknown error, retrying with caution",
		}
	}

	// After 3 attempts of unknown errors, halt
	return ErrorJudgeDecision{
		ShouldRetry:  false,
		SleepSeconds: 0,
		Reason:       "Unknown error persisted after multiple attempts",
	}
}

func calculateExponentialBackoff(attempt int, baseSeconds int, maxSeconds int) int {
	sleep := baseSeconds * (1 << uint(attempt-1)) // 2^(attempt-1)
	if sleep > maxSeconds {
		sleep = maxSeconds
	}
	return sleep
}

func calculateLinearBackoff(attempt int, baseSeconds int, maxSeconds int) int {
	sleep := baseSeconds * attempt
	if sleep > maxSeconds {
		sleep = maxSeconds
	}
	return sleep
}

// ErrorJudgeActorClient provides a client interface to the error judge actor
type ErrorJudgeActorClient struct {
	ref *actor.ActorRef
}

// NewErrorJudgeActorClient creates a new error judge actor client
func NewErrorJudgeActorClient(ref *actor.ActorRef) *ErrorJudgeActorClient {
	return &ErrorJudgeActorClient{ref: ref}
}

// Judge sends an error to the judge and returns the decision
func (c *ErrorJudgeActorClient) Judge(ctx context.Context, err error, attemptNumber int, maxAttempts int, modelID string) (ErrorJudgeDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	response := make(chan ErrorJudgeDecision, 1)

	msg := &ErrorJudgeMessage{
		Error:         err,
		AttemptNumber: attemptNumber,
		MaxAttempts:   maxAttempts,
		ModelID:       modelID,
		RequestCtx:    ctx,
		ResponseChan:  response,
	}

	if sendErr := c.ref.Send(msg); sendErr != nil {
		return ErrorJudgeDecision{}, sendErr
	}

	select {
	case <-ctx.Done():
		return ErrorJudgeDecision{}, ctx.Err()
	case decision := <-response:
		return decision, nil
	case <-time.After(consts.Timeout10Seconds + consts.Timeout5Seconds):
		return ErrorJudgeDecision{}, fmt.Errorf("error judge timeout")
	}
}
