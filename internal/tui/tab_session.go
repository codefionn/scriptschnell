package tui

import (
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

// TabSession represents a tab with its associated session and metadata
type TabSession struct {
	ID           int              // Unique tab ID (incrementing counter)
	Session      *session.Session // The actual session data
	Name         string           // User-provided name (optional)
	WorktreePath string           // Git worktree path (empty if no worktree)
	Messages     []message        // TUI-specific message display cache
	CreatedAt    time.Time
	LastActiveAt time.Time
	// Per-tab usage data
	ContextFreePercent int
	ContextWindow      int
	OpenRouterUsage    map[string]interface{}
	ThinkingTokens     int

	// Per-tab runtime state
	Runtime        *TabRuntime // Orchestrator runtime for this tab (lazy-loaded)
	Generating     bool        // Is this tab currently generating?
	WaitingForAuth bool        // Is this tab waiting for user authorization?
}

// DisplayName returns the name to show in the tab bar
func (ts *TabSession) DisplayName() string {
	if ts.Name != "" {
		return ts.Name
	}
	return fmt.Sprintf("Tab %d", ts.ID)
}

// HasMessages returns true if the session has any messages
func (ts *TabSession) HasMessages() bool {
	return len(ts.Messages) > 0
}

// IsGenerating returns true if this tab is currently generating
func (ts *TabSession) IsGenerating() bool {
	return ts.Generating
}

// NeedsAuthorization returns true if this tab is waiting for authorization
func (ts *TabSession) NeedsAuthorization() bool {
	return ts.WaitingForAuth
}

// SetGenerating sets the generating state for this tab
func (ts *TabSession) SetGenerating(generating bool) {
	ts.Generating = generating
}
