package actor

import (
	"context"
)

// UserInteractionHandler is implemented by each mode (TUI, CLI, ACP) to handle
// user interactions in a mode-specific way.
type UserInteractionHandler interface {
	// HandleInteraction processes a user interaction request and returns a response.
	// Must return a response or error - should not block indefinitely.
	// The context can be used for cancellation.
	HandleInteraction(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error)

	// Mode returns the interaction mode name (for logging/debugging)
	Mode() string

	// SupportsInteraction indicates whether this handler can handle the given type
	SupportsInteraction(interactionType InteractionType) bool
}
