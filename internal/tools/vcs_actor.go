package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/vcs"
)

// VCSActorInterface defines the interface for VCS actors
type VCSActorInterface interface {
	actor.Actor
}

// VCSMessage types for actor communication
type (
	// VCSGetCurrentBranchMsg requests the current branch
	VCSGetCurrentBranchMsg struct {
		ResponseChan chan string
	}

	// VCSRefreshBranchMsg refreshes the current branch from VCS
	VCSRefreshBranchMsg struct {
		ResponseChan chan string
	}
)

// Implement actor.Message interface for all message types
func (m VCSGetCurrentBranchMsg) Type() string { return "VCSGetCurrentBranchMsg" }
func (m VCSRefreshBranchMsg) Type() string    { return "VCSRefreshBranchMsg" }

// VCSActor manages VCS state as an actor
type VCSActor struct {
	name   string
	vcs    vcs.VCS
	ctx    context.Context
	mu     sync.RWMutex
	branch string
}

// NewVCSActor creates a new VCSActor
func NewVCSActor(name string, vcsInstance vcs.VCS) *VCSActor {
	return &VCSActor{
		name:   name,
		vcs:    vcsInstance,
		branch: "",
	}
}

// ID returns the actor's unique identifier
func (a *VCSActor) ID() string {
	return a.name
}

// Start initializes the actor and loads initial branch
func (a *VCSActor) Start(ctx context.Context) error {
	a.ctx = ctx

	// Load the current branch on startup
	if a.vcs != nil {
		branch, err := a.vcs.CurrentBranch(ctx)
		if err != nil {
			// Log but don't fail - we'll just return empty branch
			return fmt.Errorf("failed to get current branch: %w", err)
		}
		a.mu.Lock()
		a.branch = branch
		a.mu.Unlock()
	}

	return nil
}

// Stop stops the actor gracefully
func (a *VCSActor) Stop(ctx context.Context) error {
	return nil
}

// Receive handles incoming messages
func (a *VCSActor) Receive(ctx context.Context, msg actor.Message) error {
	switch m := msg.(type) {
	case VCSGetCurrentBranchMsg:
		a.mu.RLock()
		branch := a.branch
		a.mu.RUnlock()
		m.ResponseChan <- branch
		return nil

	case VCSRefreshBranchMsg:
		if a.vcs != nil {
			branch, err := a.vcs.CurrentBranch(ctx)
			if err != nil {
				// Return empty string on error
				m.ResponseChan <- ""
				return fmt.Errorf("failed to refresh current branch: %w", err)
			}
			a.mu.Lock()
			a.branch = branch
			a.mu.Unlock()
			m.ResponseChan <- branch
		} else {
			m.ResponseChan <- ""
		}
		return nil

	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

// VCSActorClient provides a convenient interface to interact with VCSActor
type VCSActorClient struct {
	actorRef interface {
		Send(msg actor.Message) error
	}
}

// NewVCSActorClient creates a new client for interacting with VCSActor
func NewVCSActorClient(actorRef interface{ Send(msg actor.Message) error }) *VCSActorClient {
	return &VCSActorClient{actorRef: actorRef}
}

// GetCurrentBranch returns the current branch
func (c *VCSActorClient) GetCurrentBranch() (string, error) {
	respChan := make(chan string, 1)
	if err := c.actorRef.Send(VCSGetCurrentBranchMsg{ResponseChan: respChan}); err != nil {
		return "", err
	}
	return <-respChan, nil
}

// RefreshBranch refreshes the current branch from VCS and returns it
func (c *VCSActorClient) RefreshBranch() (string, error) {
	respChan := make(chan string, 1)
	if err := c.actorRef.Send(VCSRefreshBranchMsg{ResponseChan: respChan}); err != nil {
		return "", err
	}
	return <-respChan, nil
}
