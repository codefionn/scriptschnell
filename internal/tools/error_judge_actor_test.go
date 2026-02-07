package tools

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHeuristicJudge_RateLimitError(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	tests := []struct {
		name          string
		error         error
		attemptNumber int
		wantRetry     bool
		minSleep      int
		maxSleep      int
	}{
		{
			name:          "rate limit first attempt",
			error:         errors.New("rate limit exceeded (429)"),
			attemptNumber: 1,
			wantRetry:     true,
			minSleep:      5,
			maxSleep:      5,
		},
		{
			name:          "rate limit second attempt",
			error:         errors.New("too many requests"),
			attemptNumber: 2,
			wantRetry:     true,
			minSleep:      10,
			maxSleep:      10,
		},
		{
			name:          "rate limit third attempt",
			error:         errors.New("Rate limit exceeded"),
			attemptNumber: 3,
			wantRetry:     true,
			minSleep:      20,
			maxSleep:      20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ErrorJudgeMessage{
				Error:         tt.error,
				AttemptNumber: tt.attemptNumber,
				MaxAttempts:   5,
				ModelID:       "gpt-4",
			}

			decision := actor.heuristicJudge(msg)

			if decision.ShouldRetry != tt.wantRetry {
				t.Errorf("ShouldRetry = %v, want %v", decision.ShouldRetry, tt.wantRetry)
			}

			if decision.SleepSeconds < tt.minSleep || decision.SleepSeconds > tt.maxSleep {
				t.Errorf("SleepSeconds = %d, want between %d and %d",
					decision.SleepSeconds, tt.minSleep, tt.maxSleep)
			}
		})
	}
}

func TestHeuristicJudge_ServiceErrors(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	tests := []struct {
		name      string
		error     error
		wantRetry bool
	}{
		{
			name:      "500 internal server error",
			error:     errors.New("500 internal server error"),
			wantRetry: true,
		},
		{
			name:      "503 service unavailable",
			error:     errors.New("503 service unavailable"),
			wantRetry: true,
		},
		{
			name:      "timeout error",
			error:     errors.New("request timeout"),
			wantRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ErrorJudgeMessage{
				Error:         tt.error,
				AttemptNumber: 1,
				MaxAttempts:   5,
				ModelID:       "gpt-4",
			}

			decision := actor.heuristicJudge(msg)

			if decision.ShouldRetry != tt.wantRetry {
				t.Errorf("ShouldRetry = %v, want %v", decision.ShouldRetry, tt.wantRetry)
			}

			if decision.ShouldRetry && decision.SleepSeconds <= 0 {
				t.Errorf("Expected positive sleep seconds for retryable error")
			}
		})
	}
}

func TestHeuristicJudge_FatalErrors(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	tests := []struct {
		name      string
		error     error
		wantRetry bool
	}{
		{
			name:      "authentication error",
			error:     errors.New("401 unauthorized - invalid api key"),
			wantRetry: false,
		},
		{
			name:      "bad request",
			error:     errors.New("400 bad request - invalid parameter"),
			wantRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ErrorJudgeMessage{
				Error:         tt.error,
				AttemptNumber: 1,
				MaxAttempts:   5,
				ModelID:       "gpt-4",
			}

			decision := actor.heuristicJudge(msg)

			if decision.ShouldRetry != tt.wantRetry {
				t.Errorf("ShouldRetry = %v, want %v", decision.ShouldRetry, tt.wantRetry)
			}

			if decision.Reason == "" {
				t.Error("Expected reason to be set for non-retryable error")
			}
		})
	}
}

func TestHeuristicJudge_ContextSizeExceeded(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	tests := []struct {
		name  string
		error error
	}{
		{
			name:  "context length exceeded",
			error: errors.New("context length exceeded maximum tokens"),
		},
		{
			name:  "context_length_exceeded error code",
			error: errors.New("error: context_length_exceeded"),
		},
		{
			name:  "max tokens exceeded",
			error: errors.New("max_tokens limit exceeded"),
		},
		{
			name:  "input too long",
			error: errors.New("input too long for model"),
		},
		{
			name:  "prompt is too long",
			error: errors.New("prompt is too long"),
		},
		{
			name:  "request too large",
			error: errors.New("request too large"),
		},
		{
			name:  "exceeds the model limit",
			error: errors.New("exceeds the model context window"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ErrorJudgeMessage{
				Error:         tt.error,
				AttemptNumber: 1,
				MaxAttempts:   5,
				ModelID:       "gpt-4",
			}

			decision := actor.heuristicJudge(msg)

			if !decision.ShouldRetry {
				t.Errorf("ShouldRetry = %v, want true for context size error", decision.ShouldRetry)
			}

			if !decision.TriggerCompaction {
				t.Errorf("TriggerCompaction = %v, want true for context size error", decision.TriggerCompaction)
			}

			if decision.Reason == "" {
				t.Error("Expected reason to be set for context size error")
			}
		})
	}
}

func TestHeuristicJudge_MaxAttemptsReached(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	msg := &ErrorJudgeMessage{
		Error:         errors.New("rate limit exceeded"),
		AttemptNumber: 5,
		MaxAttempts:   5,
		ModelID:       "gpt-4",
	}

	decision := actor.heuristicJudge(msg)

	if decision.ShouldRetry {
		t.Error("Should not retry when max attempts reached")
	}

	if decision.Reason == "" {
		t.Error("Expected reason to be set when max attempts reached")
	}
}

func TestHeuristicJudge_NetworkErrors(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	tests := []struct {
		name  string
		error error
	}{
		{
			name:  "connection refused",
			error: errors.New("connection refused"),
		},
		{
			name:  "network timeout",
			error: errors.New("network timeout"),
		},
		{
			name:  "dial error",
			error: errors.New("dial tcp: connection error"),
		},
		{
			name:  "eof error",
			error: errors.New("unexpected EOF"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ErrorJudgeMessage{
				Error:         tt.error,
				AttemptNumber: 1,
				MaxAttempts:   5,
				ModelID:       "gpt-4",
			}

			decision := actor.heuristicJudge(msg)

			if !decision.ShouldRetry {
				t.Error("Network errors should be retryable")
			}

			if decision.SleepSeconds <= 0 || decision.SleepSeconds > 10 {
				t.Errorf("Expected short delay for network errors, got %d seconds", decision.SleepSeconds)
			}
		})
	}
}

func TestHeuristicJudge_MinSleepFloor(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	tests := []struct {
		name          string
		error         error
		attemptNumber int
	}{
		{
			name:          "context size error uses MIN_SLEEP_SECONDS",
			error:         errors.New("context length exceeded"),
			attemptNumber: 1,
		},
		{
			name:          "unknown error with attempt 0 should have min sleep",
			error:         errors.New("some unknown error"),
			attemptNumber: 0,
		},
		{
			name:          "unknown error with attempt 1 should have positive sleep",
			error:         errors.New("some unknown error"),
			attemptNumber: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ErrorJudgeMessage{
				Error:         tt.error,
				AttemptNumber: tt.attemptNumber,
				MaxAttempts:   5,
				ModelID:       "gpt-4",
			}

			decision := actor.heuristicJudge(msg)

			if decision.ShouldRetry && decision.SleepSeconds < MIN_SLEEP_SECONDS {
				t.Errorf("Expected at least %d sleep seconds for retryable error, got %d",
					MIN_SLEEP_SECONDS, decision.SleepSeconds)
			}
		})
	}
}

func TestHeuristicJudge_UnknownError(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	// Unknown error should retry for first few attempts
	for attempt := 1; attempt <= 3; attempt++ {
		msg := &ErrorJudgeMessage{
			Error:         errors.New("some unknown error"),
			AttemptNumber: attempt,
			MaxAttempts:   5,
			ModelID:       "gpt-4",
		}

		decision := actor.heuristicJudge(msg)

		if attempt < 3 {
			if !decision.ShouldRetry {
				t.Errorf("Attempt %d: should retry unknown error", attempt)
			}
		} else {
			if decision.ShouldRetry {
				t.Errorf("Attempt %d: should not retry unknown error after 3 attempts", attempt)
			}
		}
	}
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt    int
		base       int
		max        int
		wantResult int
	}{
		{1, 5, 60, 5},
		{2, 5, 60, 10},
		{3, 5, 60, 20},
		{4, 5, 60, 40},
		{5, 5, 60, 60}, // capped at max
		{6, 5, 60, 60}, // capped at max
	}

	for _, tt := range tests {
		result := calculateExponentialBackoff(tt.attempt, tt.base, tt.max)
		if result != tt.wantResult {
			t.Errorf("calculateExponentialBackoff(%d, %d, %d) = %d, want %d",
				tt.attempt, tt.base, tt.max, result, tt.wantResult)
		}
	}
}

func TestParseDecision_MinSleepFloor(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	tests := []struct {
		name           string
		response       string
		wantRetry      bool
		wantMinSleep   int
		wantCompaction bool
	}{
		{
			name: "RETRY with 0 sleep should be floored to MIN_SLEEP_SECONDS",
			response: `DECISION: RETRY
SLEEP_SECONDS: 0
TRIGGER_COMPACTION: NO
REASON: Testing zero sleep`,
			wantRetry:      true,
			wantMinSleep:   MIN_SLEEP_SECONDS,
			wantCompaction: false,
		},
		{
			name: "RETRY with negative sleep should be floored to MIN_SLEEP_SECONDS",
			response: `DECISION: RETRY
SLEEP_SECONDS: -5
TRIGGER_COMPACTION: NO
REASON: Testing negative sleep`,
			wantRetry:      true,
			wantMinSleep:   MIN_SLEEP_SECONDS,
			wantCompaction: false,
		},
		{
			name: "HALT with 0 sleep should remain 0",
			response: `DECISION: HALT
SLEEP_SECONDS: 0
TRIGGER_COMPACTION: NO
REASON: Authentication error`,
			wantRetry:      false,
			wantMinSleep:   0,
			wantCompaction: false,
		},
		{
			name: "RETRY with positive sleep should be preserved",
			response: `DECISION: RETRY
SLEEP_SECONDS: 10
TRIGGER_COMPACTION: NO
REASON: Rate limit`,
			wantRetry:      true,
			wantMinSleep:   10,
			wantCompaction: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &ErrorJudgeMessage{
				Error:         errors.New("test error"),
				AttemptNumber: 1,
				MaxAttempts:   5,
				ModelID:       "test-model",
			}

			decision, err := actor.parseDecision(tt.response, msg)
			if err != nil {
				t.Fatalf("parseDecision failed: %v", err)
			}

			if decision.ShouldRetry != tt.wantRetry {
				t.Errorf("ShouldRetry = %v, want %v", decision.ShouldRetry, tt.wantRetry)
			}

			if decision.SleepSeconds < tt.wantMinSleep {
				t.Errorf("SleepSeconds = %d, want at least %d", decision.SleepSeconds, tt.wantMinSleep)
			}

			if decision.TriggerCompaction != tt.wantCompaction {
				t.Errorf("TriggerCompaction = %v, want %v", decision.TriggerCompaction, tt.wantCompaction)
			}
		})
	}
}

func TestLinearBackoff(t *testing.T) {
	tests := []struct {
		attempt    int
		base       int
		max        int
		wantResult int
	}{
		{1, 2, 10, 2},
		{2, 2, 10, 4},
		{3, 2, 10, 6},
		{4, 2, 10, 8},
		{5, 2, 10, 10}, // capped at max
		{6, 2, 10, 10}, // capped at max
	}

	for _, tt := range tests {
		result := calculateLinearBackoff(tt.attempt, tt.base, tt.max)
		if result != tt.wantResult {
			t.Errorf("calculateLinearBackoff(%d, %d, %d) = %d, want %d",
				tt.attempt, tt.base, tt.max, result, tt.wantResult)
		}
	}
}

func TestErrorJudgeActor_Receive(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	response := make(chan ErrorJudgeDecision, 1)
	msg := &ErrorJudgeMessage{
		Error:         errors.New("rate limit exceeded"),
		AttemptNumber: 1,
		MaxAttempts:   5,
		ModelID:       "gpt-4",
		RequestCtx:    context.Background(),
		ResponseChan:  response,
	}

	err := actor.Receive(context.Background(), msg)
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	select {
	case decision := <-response:
		if !decision.ShouldRetry {
			t.Error("Expected rate limit error to be retryable")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

// InvalidTestMessage is a mock message type for testing
type InvalidTestMessage struct{}

// Type implements actor.Message for InvalidTestMessage
func (m *InvalidTestMessage) Type() string { return "invalid" }

func TestErrorJudgeActor_Receive_InvalidMessage(t *testing.T) {
	actor := NewErrorJudgeActor("test", nil)

	invalidMsg := &InvalidTestMessage{}

	err := actor.Receive(context.Background(), invalidMsg)
	if err == nil {
		t.Error("Expected error for invalid message type")
	}
}
