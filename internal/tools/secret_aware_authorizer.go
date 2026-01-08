package tools

import (
	"context"

	"github.com/codefionn/scriptschnell/internal/secretdetect"
)

// SecretAwareAuthorizer wraps an Authorizer to add secret-based authorization capabilities.
type SecretAwareAuthorizer struct {
	authorizer       Authorizer
	authorizationActor *AuthorizationActor
}

// NewSecretAwareAuthorizer creates a new SecretAwareAuthorizer that wraps an existing authorizer.
func NewSecretAwareAuthorizer(authorizer Authorizer, actor *AuthorizationActor) *SecretAwareAuthorizer {
	return &SecretAwareAuthorizer{
		authorizer:        authorizer,
		authorizationActor: actor,
	}
}

// Authorize implements the Authorizer interface for normal authorization without secrets.
func (s *SecretAwareAuthorizer) Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error) {
	if s.authorizer != nil {
		return s.authorizer.Authorize(ctx, toolName, params)
	}
	return &AuthorizationDecision{Allowed: true}, nil
}

// AuthorizeWithSecrets handles authorization when secrets are detected.
func (s *SecretAwareAuthorizer) AuthorizeWithSecrets(ctx context.Context, toolName string, params map[string]interface{}, secrets []secretdetect.SecretMatch) (*AuthorizationDecision, error) {
	if s.authorizationActor != nil {
		return s.authorizationActor.judgeToolCallWithSecrets(ctx, toolName, params, secrets)
	}
	
	// Fallback: require user input if we have no way to judge secrets
	return &AuthorizationDecision{
		Allowed:           false,
		Reason:            "Tool contains detected secrets and requires user approval",
		RequiresUserInput: true,
	}, nil
}