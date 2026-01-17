package actor

import (
	"context"
	"time"
)

// InteractionType identifies the kind of user interaction
type InteractionType int

const (
	// InteractionTypeAuthorization is for tool/command authorization requests
	InteractionTypeAuthorization InteractionType = iota
	// InteractionTypePlanningQuestion is for questions during planning phase
	InteractionTypePlanningQuestion
	// InteractionTypeUserInputSingle is for single text input from user
	InteractionTypeUserInputSingle
	// InteractionTypeUserInputMultiple is for multiple choice questions
	InteractionTypeUserInputMultiple
)

// String returns a human-readable name for the interaction type
func (t InteractionType) String() string {
	switch t {
	case InteractionTypeAuthorization:
		return "authorization"
	case InteractionTypePlanningQuestion:
		return "planning_question"
	case InteractionTypeUserInputSingle:
		return "user_input_single"
	case InteractionTypeUserInputMultiple:
		return "user_input_multiple"
	default:
		return "unknown"
	}
}

// UserInteractionRequest is the message sent to request user interaction
type UserInteractionRequest struct {
	// RequestID is a unique identifier for tracking this request
	RequestID string
	// InteractionType identifies what kind of interaction is needed
	InteractionType InteractionType
	// Payload contains type-specific data for the interaction
	Payload interface{}
	// RequestCtx is the context for cancellation
	RequestCtx context.Context
	// ResponseChan receives the response when the user completes the interaction
	ResponseChan chan *UserInteractionResponse
	// Timeout is the maximum time to wait for user response (0 = default)
	Timeout time.Duration
	// TabID is used in TUI mode to identify which tab requested the interaction
	TabID int
}

// Type returns the message type for the actor system
func (m *UserInteractionRequest) Type() string { return "user_interaction_request" }

// UserInteractionResponse is the unified response format for all interaction types
type UserInteractionResponse struct {
	// RequestID matches the request this is responding to
	RequestID string
	// Approved is used for authorization requests (true = approved, false = denied)
	Approved bool
	// Answer is used for single-answer questions
	Answer string
	// Answers is used for multiple questions (question -> answer mapping)
	Answers map[string]string
	// Cancelled indicates the user dismissed/cancelled the interaction
	Cancelled bool
	// TimedOut indicates the request timed out waiting for user
	TimedOut bool
	// Error contains any error that occurred during interaction handling
	Error error
	// Acknowledged confirms the handler received and displayed the request
	Acknowledged bool
}

// UserInteractionAck is sent by the handler when it has displayed the interaction UI
type UserInteractionAck struct {
	RequestID string
}

// Type returns the message type for the actor system
func (m *UserInteractionAck) Type() string { return "user_interaction_ack" }

// UserInteractionCancel is sent to cancel a pending interaction
type UserInteractionCancel struct {
	RequestID string
	Reason    string
}

// Type returns the message type for the actor system
func (m *UserInteractionCancel) Type() string { return "user_interaction_cancel" }

// AuthorizationPayload contains data for authorization requests
type AuthorizationPayload struct {
	// ToolName is the name of the tool requesting authorization
	ToolName string
	// Parameters are the tool's parameters
	Parameters map[string]interface{}
	// Reason explains why authorization is needed
	Reason string
	// SuggestedPrefix is a command prefix suggested for permanent authorization
	SuggestedPrefix string
	// SuggestedDomain is a domain pattern suggested for permanent authorization
	SuggestedDomain string
	// IsCommandAuth indicates this is a shell command authorization
	IsCommandAuth bool
	// IsDomainAuth indicates this is a network domain authorization
	IsDomainAuth bool
}

// PlanningQuestionPayload contains data for planning phase questions
type PlanningQuestionPayload struct {
	// Question is the question text
	Question string
	// Context provides additional context for the question
	Context string
}

// UserInputSinglePayload contains data for single text input requests
type UserInputSinglePayload struct {
	// Question is the prompt/question to display
	Question string
	// Default is an optional default answer
	Default string
}

// UserInputMultiplePayload contains data for multiple choice questions
type UserInputMultiplePayload struct {
	// FormattedQuestions is the original formatted questions string
	FormattedQuestions string
	// ParsedQuestions contains the parsed structure for UI rendering
	ParsedQuestions []QuestionWithOptions
}

// QuestionWithOptions represents a single question with its available options
type QuestionWithOptions struct {
	// Question is the question text
	Question string
	// Options are the available choices
	Options []string
	// AllowCustom indicates if custom text input is allowed
	AllowCustom bool
}
