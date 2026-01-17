package actor

import (
	"context"
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// UserInteractionClient provides typed access to the UserInteractionActor
type UserInteractionClient struct {
	ref            *ActorRef
	defaultTimeout time.Duration
}

// NewUserInteractionClient creates a new client for the user interaction actor
func NewUserInteractionClient(ref *ActorRef) *UserInteractionClient {
	return &UserInteractionClient{
		ref:            ref,
		defaultTimeout: 2 * time.Minute,
	}
}

// SetDefaultTimeout sets the default timeout for requests
func (c *UserInteractionClient) SetDefaultTimeout(timeout time.Duration) {
	c.defaultTimeout = timeout
}

// RequestAuthorization requests user authorization for a tool operation
func (c *UserInteractionClient) RequestAuthorization(
	ctx context.Context,
	toolName string,
	params map[string]interface{},
	reason string,
	suggestedPrefix string,
	tabID int,
) (*UserInteractionResponse, error) {
	requestID := fmt.Sprintf("auth-%d", time.Now().UnixNano())
	responseChan := make(chan *UserInteractionResponse, 1)

	req := &UserInteractionRequest{
		RequestID:       requestID,
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName:        toolName,
			Parameters:      params,
			Reason:          reason,
			SuggestedPrefix: suggestedPrefix,
			IsCommandAuth:   toolName == "shell" || toolName == "command",
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
		Timeout:      c.defaultTimeout,
		TabID:        tabID,
	}

	logger.Debug("UserInteractionClient: sending authorization request %s for tool %s", requestID, toolName)

	if err := c.ref.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send authorization request: %w", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Error != nil && !resp.TimedOut && !resp.Cancelled {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RequestDomainAuthorization requests user authorization for network domain access
func (c *UserInteractionClient) RequestDomainAuthorization(
	ctx context.Context,
	domain string,
	reason string,
	suggestedDomain string,
	tabID int,
) (*UserInteractionResponse, error) {
	requestID := fmt.Sprintf("domain-auth-%d", time.Now().UnixNano())
	responseChan := make(chan *UserInteractionResponse, 1)

	req := &UserInteractionRequest{
		RequestID:       requestID,
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName:        "network_access",
			Parameters:      map[string]interface{}{"domain": domain},
			Reason:          reason,
			SuggestedDomain: suggestedDomain,
			IsDomainAuth:    true,
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
		Timeout:      c.defaultTimeout,
		TabID:        tabID,
	}

	logger.Debug("UserInteractionClient: sending domain authorization request %s for %s", requestID, domain)

	if err := c.ref.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send domain authorization request: %w", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Error != nil && !resp.TimedOut && !resp.Cancelled {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RequestUserInput requests single text input from the user
func (c *UserInteractionClient) RequestUserInput(
	ctx context.Context,
	question string,
	defaultAnswer string,
	tabID int,
) (*UserInteractionResponse, error) {
	requestID := fmt.Sprintf("input-%d", time.Now().UnixNano())
	responseChan := make(chan *UserInteractionResponse, 1)

	req := &UserInteractionRequest{
		RequestID:       requestID,
		InteractionType: InteractionTypeUserInputSingle,
		Payload: &UserInputSinglePayload{
			Question: question,
			Default:  defaultAnswer,
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
		Timeout:      c.defaultTimeout,
		TabID:        tabID,
	}

	logger.Debug("UserInteractionClient: sending user input request %s", requestID)

	if err := c.ref.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send user input request: %w", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Error != nil && !resp.TimedOut && !resp.Cancelled {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RequestMultipleAnswers requests answers to multiple questions from the user
func (c *UserInteractionClient) RequestMultipleAnswers(
	ctx context.Context,
	formattedQuestions string,
	parsedQuestions []QuestionWithOptions,
	tabID int,
) (*UserInteractionResponse, error) {
	requestID := fmt.Sprintf("multi-%d", time.Now().UnixNano())
	responseChan := make(chan *UserInteractionResponse, 1)

	req := &UserInteractionRequest{
		RequestID:       requestID,
		InteractionType: InteractionTypeUserInputMultiple,
		Payload: &UserInputMultiplePayload{
			FormattedQuestions: formattedQuestions,
			ParsedQuestions:    parsedQuestions,
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
		Timeout:      c.defaultTimeout,
		TabID:        tabID,
	}

	logger.Debug("UserInteractionClient: sending multiple questions request %s", requestID)

	if err := c.ref.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send multiple questions request: %w", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Error != nil && !resp.TimedOut && !resp.Cancelled {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RequestPlanningQuestion requests an answer to a planning phase question
func (c *UserInteractionClient) RequestPlanningQuestion(
	ctx context.Context,
	question string,
	questionContext string,
	tabID int,
) (*UserInteractionResponse, error) {
	requestID := fmt.Sprintf("planning-%d", time.Now().UnixNano())
	responseChan := make(chan *UserInteractionResponse, 1)

	req := &UserInteractionRequest{
		RequestID:       requestID,
		InteractionType: InteractionTypePlanningQuestion,
		Payload: &PlanningQuestionPayload{
			Question: question,
			Context:  questionContext,
		},
		RequestCtx:   ctx,
		ResponseChan: responseChan,
		Timeout:      c.defaultTimeout,
		TabID:        tabID,
	}

	logger.Debug("UserInteractionClient: sending planning question request %s", requestID)

	if err := c.ref.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send planning question request: %w", err)
	}

	select {
	case resp := <-responseChan:
		if resp.Error != nil && !resp.TimedOut && !resp.Cancelled {
			return resp, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Cancel sends a cancellation request for a pending interaction
func (c *UserInteractionClient) Cancel(requestID string, reason string) error {
	return c.ref.Send(&UserInteractionCancel{
		RequestID: requestID,
		Reason:    reason,
	})
}

// Acknowledge sends an acknowledgment that a dialog was displayed
func (c *UserInteractionClient) Acknowledge(requestID string) error {
	return c.ref.Send(&UserInteractionAck{
		RequestID: requestID,
	})
}
