package actor

import (
	"context"
	"fmt"
	"time"
)

// DomainBlockerClient provides a convenient interface for interacting with the domain blocker actor
type DomainBlockerClient struct {
	actorRef *ActorRef
}

// NewDomainBlockerClient creates a new client for the domain blocker actor
func NewDomainBlockerClient(actorRef *ActorRef) *DomainBlockerClient {
	return &DomainBlockerClient{
		actorRef: actorRef,
	}
}

// IsDomainBlocked checks if a domain is blocked
func (c *DomainBlockerClient) IsDomainBlocked(ctx context.Context, domain string) (bool, string, error) {
	if c.actorRef == nil {
		return false, "", fmt.Errorf("domain blocker actor not available")
	}

	responseCh := make(chan DomainBlockResponse, 1)
	req := DomainBlockRequest{
		Domain:     domain,
		ResponseCh: responseCh,
	}

	if err := c.actorRef.Send(req); err != nil {
		return false, "", fmt.Errorf("failed to send domain block request: %w", err)
	}

	select {
	case response := <-responseCh:
		return response.Blocked, response.Reason, nil
	case <-ctx.Done():
		return false, "", ctx.Err()
	case <-time.After(5 * time.Second):
		return false, "", fmt.Errorf("timeout waiting for domain block response")
	}
}

// RefreshBlocklist forces a refresh of the blocklist
func (c *DomainBlockerClient) RefreshBlocklist(ctx context.Context) (bool, int, error) {
	if c.actorRef == nil {
		return false, 0, fmt.Errorf("domain blocker actor not available")
	}

	responseCh := make(chan RefreshBlocklistResponse, 1)
	req := RefreshBlocklistRequest{
		ResponseCh: responseCh,
	}

	if err := c.actorRef.Send(req); err != nil {
		return false, 0, fmt.Errorf("failed to send refresh request: %w", err)
	}

	select {
	case response := <-responseCh:
		return response.Success, response.DomainCount, nil
	case <-ctx.Done():
		return false, 0, ctx.Err()
	case <-time.After(30 * time.Second):
		return false, 0, fmt.Errorf("timeout waiting for refresh response")
	}
}

// GetStats returns current blocklist statistics
func (c *DomainBlockerClient) GetStats(ctx context.Context) (*BlocklistStats, error) {
	if c.actorRef == nil {
		return nil, fmt.Errorf("domain blocker actor not available")
	}

	responseCh := make(chan BlocklistStatsResponse, 1)
	req := GetBlocklistStatsRequest{
		ResponseCh: responseCh,
	}

	if err := c.actorRef.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send stats request: %w", err)
	}

	select {
	case response := <-responseCh:
		return &BlocklistStats{
			DomainCount:     response.DomainCount,
			LastUpdated:     response.LastUpdated,
			BlocklistURL:    response.BlocklistURL,
			RefreshInterval: response.RefreshInterval,
			TTL:             response.TTL,
			Expired:         response.Expired,
			CacheEnabled:    response.CacheEnabled,
			CacheDir:        response.CacheDir,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timeout waiting for stats response")
	}
}

// BlocklistStats contains statistics about the domain blocklist
type BlocklistStats struct {
	DomainCount     int
	LastUpdated     time.Time
	BlocklistURL    string
	RefreshInterval time.Duration
	TTL             time.Duration
	Expired         bool
	CacheEnabled    bool
	CacheDir        string // Empty if cache is disabled
}
