package actor

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDomainBlockerActor_NewActor tests the creation of a new domain blocker actor
func TestDomainBlockerActor_NewActor(t *testing.T) {
	// Test with default configuration
	actor := NewDomainBlockerActor("test", DomainBlockerConfig{})
	require.NotNil(t, actor)
	assert.Equal(t, "test", actor.ID())
	assert.Equal(t, DefaultRPZURL, actor.blocklistURL)
	assert.Equal(t, 6*time.Hour, actor.refreshInterval)
	assert.Equal(t, 24*time.Hour, actor.ttl)
	assert.NotNil(t, actor.downloader)

	// Test with custom configuration
	mockDownloader := NewMockBlocklistDownloader()
	customConfig := DomainBlockerConfig{
		BlocklistURL:    "https://example.com/blocklist.txt",
		RefreshInterval: 1 * time.Hour,
		TTL:             12 * time.Hour,
		CacheDir:        "/tmp/cache",
		Downloader:      mockDownloader,
	}

	actor2 := NewDomainBlockerActor("test2", customConfig)
	require.NotNil(t, actor2)
	assert.Equal(t, "test2", actor2.ID())
	assert.Equal(t, "https://example.com/blocklist.txt", actor2.blocklistURL)
	assert.Equal(t, 1*time.Hour, actor2.refreshInterval)
	assert.Equal(t, 12*time.Hour, actor2.ttl)
	assert.Equal(t, "/tmp/cache", actor2.cacheDir)
	assert.Equal(t, mockDownloader, actor2.downloader)
}

// TestDomainBlockerActor_StartStop tests starting and stopping the actor
func TestDomainBlockerActor_StartStop(t *testing.T) {
	mockDownloader := NewMockBlocklistDownloader()

	config := DomainBlockerConfig{
		Downloader: mockDownloader,
		TTL:        1 * time.Hour, // Long TTL to avoid background refresh
	}

	actor := NewDomainBlockerActor("test", config)
	require.NotNil(t, actor)

	ctx := context.Background()

	// Test starting the actor
	err := actor.Start(ctx)
	require.NoError(t, err)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Test stopping the actor
	err = actor.Stop(ctx)
	require.NoError(t, err)
}

// TestDomainBlockerActor_BlockDomain tests domain blocking functionality
func TestDomainBlockerActor_BlockDomain(t *testing.T) {
	// Create a mock downloader with test data
	mockDownloader := NewMockBlocklistDownloader()
	testDomains := []string{"malware.com", "phishing.net", "bad.site"}
	mockDownloader.SetDomains(testDomains)

	config := DomainBlockerConfig{
		Downloader: mockDownloader,
		TTL:        1 * time.Hour,
	}

	actor := NewDomainBlockerActor("test", config)
	require.NotNil(t, actor)

	ctx := context.Background()

	// Start the actor
	err := actor.Start(ctx)
	require.NoError(t, err)

	// Wait for initial load
	time.Sleep(200 * time.Millisecond)

	tests := []struct {
		domain  string
		blocked bool
	}{
		{"malware.com", true},
		{"phishing.net", true},
		{"bad.site", true},
		{"good.com", false},
		{"sub.malware.com", false}, // Should not block subdomains by default
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			blocked, reason := actor.IsDomainBlocked(tt.domain)
			if tt.blocked {
				assert.True(t, blocked, "Domain %s should be blocked", tt.domain)
				assert.NotEmpty(t, reason)
			} else {
				assert.False(t, blocked, "Domain %s should not be blocked", tt.domain)
			}
		})
	}

	// Stop the actor
	err = actor.Stop(ctx)
	require.NoError(t, err)
}

// TestDomainBlockerActor_BackgroundRefresh tests background refresh functionality
func TestDomainBlockerActor_BackgroundRefresh(t *testing.T) {
	mockDownloader := NewMockBlocklistDownloader()

	// Initial domains
	initialDomains := []string{"old1.com", "old2.com"}
	mockDownloader.SetDomains(initialDomains)

	config := DomainBlockerConfig{
		Downloader:      mockDownloader,
		TTL:             1 * time.Millisecond, // Very short TTL to force refresh
		RefreshInterval: 1 * time.Hour,        // Long refresh interval
	}

	actor := NewDomainBlockerActor("test", config)
	require.NotNil(t, actor)

	ctx := context.Background()

	// Start the actor - should trigger background refresh due to expired TTL
	err := actor.Start(ctx)
	require.NoError(t, err)

	// Wait for background refresh
	time.Sleep(300 * time.Millisecond)

	// Update mock with new domains (simulating a fresh download)
	newDomains := []string{"new1.com", "new2.com"}
	mockDownloader.SetDomains(newDomains)

	// Manually trigger refresh to test the mechanism
	err = actor.refreshBlocklist()
	require.NoError(t, err)

	// Check that new domains are loaded
	blocked, _ := actor.IsDomainBlocked("new1.com")
	assert.True(t, blocked)

	blocked, _ = actor.IsDomainBlocked("old1.com")
	assert.False(t, blocked)

	// Stop the actor
	err = actor.Stop(ctx)
	require.NoError(t, err)
}

// TestDomainBlockerActor_Messages tests message handling
func TestDomainBlockerActor_Messages(t *testing.T) {
	mockDownloader := NewMockBlocklistDownloader()
	testDomains := []string{"test.com"}
	mockDownloader.SetDomains(testDomains)

	config := DomainBlockerConfig{
		Downloader: mockDownloader,
		TTL:        1 * time.Hour,
	}

	actor := NewDomainBlockerActor("test", config)
	require.NotNil(t, actor)

	ctx := context.Background()

	// Start the actor
	err := actor.Start(ctx)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	// Test DomainBlockRequest
	responseCh := make(chan DomainBlockResponse, 1)
	req := DomainBlockRequest{
		Domain:     "test.com",
		ResponseCh: responseCh,
	}

	err = actor.Receive(ctx, req)
	require.NoError(t, err)

	select {
	case response := <-responseCh:
		assert.True(t, response.Blocked)
		assert.NotEmpty(t, response.Reason)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for domain block response")
	}

	// Test RefreshBlocklistRequest
	refreshResponseCh := make(chan RefreshBlocklistResponse, 1)
	refreshReq := RefreshBlocklistRequest{
		ResponseCh: refreshResponseCh,
	}

	err = actor.Receive(ctx, refreshReq)
	require.NoError(t, err)

	select {
	case response := <-refreshResponseCh:
		assert.True(t, response.Success)
		assert.Greater(t, response.DomainCount, 0)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for refresh response")
	}

	// Test GetBlocklistStatsRequest
	statsResponseCh := make(chan BlocklistStatsResponse, 1)
	statsReq := GetBlocklistStatsRequest{
		ResponseCh: statsResponseCh,
	}

	err = actor.Receive(ctx, statsReq)
	require.NoError(t, err)

	select {
	case response := <-statsResponseCh:
		assert.Greater(t, response.DomainCount, 0)
		assert.True(t, response.CacheEnabled)
		assert.False(t, response.Expired)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for stats response")
	}

	// Stop the actor
	err = actor.Stop(ctx)
	require.NoError(t, err)
}

// TestDomainBlockerActor_ConcurrentAccess tests concurrent access to the actor
func TestDomainBlockerActor_ConcurrentAccess(t *testing.T) {
	mockDownloader := NewMockBlocklistDownloader()
	testDomains := []string{"concurrent.com"}
	mockDownloader.SetDomains(testDomains)

	config := DomainBlockerConfig{
		Downloader: mockDownloader,
		TTL:        1 * time.Hour,
	}

	actor := NewDomainBlockerActor("test", config)
	require.NotNil(t, actor)

	ctx := context.Background()

	// Start the actor
	err := actor.Start(ctx)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	// Test concurrent domain checks
	var wg sync.WaitGroup
	numGoroutines := 10
	numChecks := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numChecks; j++ {
				blocked, _ := actor.IsDomainBlocked("concurrent.com")
				assert.True(t, blocked)
			}
		}()
	}

	wg.Wait()

	// Stop the actor
	err = actor.Stop(ctx)
	require.NoError(t, err)
}

// MockBlocklistDownloader is a mock implementation of BlocklistDownloader for testing
type MockBlocklistDownloader struct {
	domains   []string
	healthy   bool
	callCount int
	mu        sync.Mutex
}

// NewMockBlocklistDownloader creates a new mock blocklist downloader
func NewMockBlocklistDownloader() *MockBlocklistDownloader {
	return &MockBlocklistDownloader{
		domains: []string{},
		healthy: true,
	}
}

// SetDomains sets the domains to be returned by the mock downloader
func (m *MockBlocklistDownloader) SetDomains(domains []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.domains = domains
}

// SetHealthy sets the health status of the mock downloader
func (m *MockBlocklistDownloader) SetHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthy = healthy
}

// GetCallCount returns the number of times DownloadBlocklist was called
func (m *MockBlocklistDownloader) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// DownloadBlocklist implements BlocklistDownloader interface
func (m *MockBlocklistDownloader) DownloadBlocklist(ctx context.Context, url string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++

	if !m.healthy {
		return nil, assert.AnError
	}

	// Create RPZ format content
	var content strings.Builder
	for _, domain := range m.domains {
		content.WriteString(domain)
		content.WriteString(" CNAME .\n")
	}

	return io.NopCloser(strings.NewReader(content.String())), nil
}

// GetLastModified implements BlocklistDownloader interface
func (m *MockBlocklistDownloader) GetLastModified(ctx context.Context, url string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.healthy {
		return time.Time{}, assert.AnError
	}

	return time.Now().Add(-1 * time.Hour), nil
}

// IsHealthy implements BlocklistDownloader interface
func (m *MockBlocklistDownloader) IsHealthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}
