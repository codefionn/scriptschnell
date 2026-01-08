package actor

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/ahocorasick"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// DomainBlockerMessage represents different message types for the domain blocker actor
type DomainBlockerMessage interface {
	isDomainBlockerMessage()
	Type() string
}

// DomainBlockRequest asks whether a domain is blocked
type DomainBlockRequest struct {
	Domain      string
	ResponseCh  chan DomainBlockResponse
}

func (DomainBlockRequest) isDomainBlockerMessage() {}
func (DomainBlockRequest) Type() string { return "DomainBlockRequest" }

// DomainBlockResponse contains the result of a domain block check
type DomainBlockResponse struct {
	Blocked bool
	Reason  string
}

// RefreshBlocklistRequest forces a refresh of the blocklist
type RefreshBlocklistRequest struct {
	ResponseCh chan RefreshBlocklistResponse
}

func (RefreshBlocklistRequest) isDomainBlockerMessage() {}
func (RefreshBlocklistRequest) Type() string { return "RefreshBlocklistRequest" }

// RefreshBlocklistResponse contains the result of a blocklist refresh
type RefreshBlocklistResponse struct {
	Success   bool
	DomainCount int
	Error     string
}

// GetBlocklistStatsRequest requests current blocklist statistics
type GetBlocklistStatsRequest struct {
	ResponseCh chan BlocklistStatsResponse
}

func (GetBlocklistStatsRequest) isDomainBlockerMessage() {}
func (GetBlocklistStatsRequest) Type() string { return "GetBlocklistStatsRequest" }

// BlocklistStatsResponse contains statistics about the current blocklist
type BlocklistStatsResponse struct {
	DomainCount      int
	LastUpdated      time.Time
	BlocklistURL     string
	RefreshInterval  time.Duration
	TTL              time.Duration
	Expired          bool
	CacheEnabled     bool
	CacheDir         string // Empty if cache is disabled
}

// DomainBlockerActor handles domain blocking based on RPZ lists
type DomainBlockerActor struct {
	id                string
	mu                sync.RWMutex
	matcher           *ahocorasick.Matcher
	domainCount       int
	lastUpdated       time.Time
	blocklistURL      string
	refreshInterval   time.Duration
	ttl               time.Duration
	cacheDir          string
	downloader        BlocklistDownloader
	health            *HealthCheckable
	stopCh            chan struct{}
	refreshTicker     *time.Ticker
}

// DomainBlockerConfig holds configuration for the domain blocker actor
type DomainBlockerConfig struct {
	BlocklistURL     string
	RefreshInterval  time.Duration
	TTL              time.Duration // TTL for the blocklist, after which it's considered stale
	CacheDir         string        // Directory to cache blocklist files
	Downloader       BlocklistDownloader
	HTTPClient       *http.Client  // Deprecated: use Downloader instead
}

// DefaultRPZURL is the default RPZ blocklist URL
const DefaultRPZURL = "https://raw.githubusercontent.com/hagezi/dns-blocklists/refs/heads/main/rpz/tif.txt"

// MirrorRPZURL is the mirror URL for the RPZ blocklist
const MirrorRPZURL = "https://codeberg.org/hagezi/mirror2/raw/branch/main/dns-blocklists/rpz/tif.txt"

// domainBlocklistCacheDir returns the platform-specific cache directory for domain blocklists
func domainBlocklistCacheDir() (string, error) {
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
		return filepath.Join(cacheDir, "scriptschnell", "domain_blocklist"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".cache", "scriptschnell", "domain_blocklist"), nil
}

// getCacheFilePath returns the file path for the cached blocklist
func (a *DomainBlockerActor) getCacheFilePath() string {
	if a.cacheDir == "" {
		return ""
	}
	// Use a hash of the blocklist URL to create a unique filename
	hash := fmt.Sprintf("%x", md5.Sum([]byte(a.blocklistURL)))
	return filepath.Join(a.cacheDir, hash+".cache")
}

// loadCachedBlocklist attempts to load the blocklist from cache
func (a *DomainBlockerActor) loadCachedBlocklist() ([]string, error) {
	if a.cacheDir == "" {
		return nil, fmt.Errorf("cache not configured")
	}

	cacheFile := a.getCacheFilePath()
	if cacheFile == "" {
		return nil, fmt.Errorf("cache file path unavailable")
	}

	file, err := os.Open(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("cache file not found")
		}
		return nil, fmt.Errorf("failed to open cache file: %w", err)
	}
	defer file.Close()

	// Check if cached data is still valid (not expired than TTL)
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat cache file: %w", err)
	}

	if time.Since(info.ModTime()) > a.ttl {
		return nil, fmt.Errorf("cached blocklist expired")
	}

	// Read and decode the cached domains
	var domains []string
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&domains); err != nil {
		return nil, fmt.Errorf("failed to decode cached blocklist: %w", err)
	}

	logger.Debug("domain blocker actor %s: loaded %d domains from cache", a.id, len(domains))
	return domains, nil
}

// saveCachedBlocklist saves the blocklist to cache
func (a *DomainBlockerActor) saveCachedBlocklist(domains []string) error {
	if a.cacheDir == "" {
		return fmt.Errorf("cache not configured")
	}

	cacheFile := a.getCacheFilePath()
	if cacheFile == "" {
		return fmt.Errorf("cache file path unavailable")
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(a.cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Write to temporary file first, then rename for atomicity
	tempFile := cacheFile + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp cache file: %w", err)
	}

	// Write the domains as JSON
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(domains); err != nil {
		file.Close()
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to encode blocklist to cache: %w", err)
	}

	// Close and rename to final location
	if err := file.Close(); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to close temp cache file: %w", err)
	}

	if err := os.Rename(tempFile, cacheFile); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to rename temp cache file: %w", err)
	}

	logger.Debug("domain blocker actor %s: cached %d domains", a.id, len(domains))
	return nil
}

// NewDomainBlockerActor creates a new domain blocker actor
func NewDomainBlockerActor(id string, config DomainBlockerConfig) *DomainBlockerActor {
	if config.BlocklistURL == "" {
		config.BlocklistURL = DefaultRPZURL
	}
	if config.RefreshInterval == 0 {
		config.RefreshInterval = 6 * time.Hour // Default refresh every 6 hours
	}
	if config.TTL == 0 {
		config.TTL = 24 * time.Hour // Default TTL is 24 hours
	}
	if config.CacheDir == "" {
		cacheDir, err := domainBlocklistCacheDir()
		if err != nil {
			logger.Warn("failed to determine cache directory: %v", err)
			config.CacheDir = "" // Disable caching if we can't determine cache dir
		} else {
			config.CacheDir = cacheDir
		}
	}
	
	// Initialize downloader if not provided
	if config.Downloader == nil {
		if config.HTTPClient != nil {
			// Use legacy HTTPClient for backward compatibility
			httpClientConfig := BlocklistDownloaderConfig{
				HTTPClient: config.HTTPClient,
				UserAgent:  "scriptschnell-domain-blocker/1.0",
			}
			config.Downloader = NewHTTPBlocklistDownloader(httpClientConfig)
		} else {
			// Use default downloader
			config.Downloader = NewHTTPBlocklistDownloader(DefaultBlocklistDownloaderConfig())
		}
	}

	actor := &DomainBlockerActor{
		id:              id,
		blocklistURL:    config.BlocklistURL,
		refreshInterval: config.RefreshInterval,
		ttl:             config.TTL,
		cacheDir:        config.CacheDir,
		downloader:      config.Downloader,
		stopCh:          make(chan struct{}),
	}

	// Initialize health monitoring
	actor.health = NewHealthCheckable(id, make(chan Message, 100), func() interface{} {
		return actor.getDomainBlockerMetrics()
	})

	return actor
}

// ID returns the actor's ID
func (a *DomainBlockerActor) ID() string {
	return a.id
}

// Start initializes the actor and loads the initial blocklist
func (a *DomainBlockerActor) Start(ctx context.Context) error {
	logger.Debug("domain blocker actor %s: starting with blocklist URL: %s", a.id, a.blocklistURL)

	// Try to load from cache first, then check TTL
	var loadedFromCache bool
	if domains, err := a.loadCachedBlocklist(); err == nil {
		// Successfully loaded from cache
		a.mu.Lock()
		a.matcher = ahocorasick.NewStringMatcher(domains)
		a.domainCount = len(domains)
		a.lastUpdated = time.Now() // Update lastUpdated to indicate we just loaded it
		a.mu.Unlock()
		loadedFromCache = true
		logger.Info("domain blocker actor %s: loaded %d domains from cache", a.id, len(domains))
	} else {
		logger.Debug("domain blocker actor %s: cache load failed: %v", a.id, err)
	}

	// Check if we need to refresh (either no cache or expired cache)
	if a.isBlocklistExpired() {
		// Blocklist is expired or doesn't exist, refresh in background
		logger.Info("domain blocker actor %s: blocklist is expired or missing, starting background refresh", a.id)
		go a.backgroundRefresh()
	}

	// Start periodic refresh
	a.refreshTicker = time.NewTicker(a.refreshInterval)
	go a.refreshLoop(ctx)

	cacheStatus := "disabled"
	if a.cacheDir != "" {
		if loadedFromCache {
			cacheStatus = "loaded"
		} else {
			cacheStatus = "enabled"
		}
	}
	
	logger.Info("domain blocker actor %s: started with %d domains, refresh interval: %v, TTL: %v, cache: %s", 
		a.id, a.getDomainCount(), a.refreshInterval, a.ttl, cacheStatus)
	return nil
}

// Stop stops the actor and cancels any ongoing operations
func (a *DomainBlockerActor) Stop(ctx context.Context) error {
	logger.Debug("domain blocker actor %s: stopping", a.id)

	close(a.stopCh)
	if a.refreshTicker != nil {
		a.refreshTicker.Stop()
	}

	logger.Info("domain blocker actor %s: stopped", a.id)
	return nil
}

// Receive handles incoming messages
func (a *DomainBlockerActor) Receive(ctx context.Context, msg Message) error {
	// Record activity for health monitoring
	if a.health != nil {
		a.health.RecordActivity()
	}

	switch m := msg.(type) {
	case DomainBlockRequest:
		response := a.handleDomainBlockRequest(ctx, m)
		select {
		case m.ResponseCh <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	case RefreshBlocklistRequest:
		response := a.handleRefreshBlocklistRequest(ctx, m)
		select {
		case m.ResponseCh <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	case GetBlocklistStatsRequest:
		response := a.handleGetBlocklistStatsRequest(ctx, m)
		select {
		case m.ResponseCh <- response:
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		// Try health check handler first
		if a.health != nil {
			if err := a.health.HealthCheckHandler(ctx, msg); err == nil {
				return nil // Health check message handled
			}
		}
		return fmt.Errorf("unknown message type: %T", msg)
	}
	return nil
}

// IsDomainBlocked checks if a domain is in the blocklist
func (a *DomainBlockerActor) IsDomainBlocked(domain string) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.matcher == nil {
		return false, "blocklist not loaded"
	}

	// Check for exact domain match or subdomain match
	matches := a.matcher.Match([]byte(strings.ToLower(domain)))
	if len(matches) > 0 {
		return true, "domain found in RPZ blocklist"
	}

	// Also check if any parent domain is blocked
	parts := strings.Split(domain, ".")
	for i := 1; i < len(parts); i++ {
		parentDomain := strings.Join(parts[i:], ".")
		parentMatches := a.matcher.Match([]byte(strings.ToLower(parentDomain)))
		if len(parentMatches) > 0 {
			return true, "parent domain found in RPZ blocklist"
		}
	}

	return false, ""
}

// handleDomainBlockRequest processes a domain block check request
func (a *DomainBlockerActor) handleDomainBlockRequest(ctx context.Context, req DomainBlockRequest) DomainBlockResponse {
	blocked, reason := a.IsDomainBlocked(req.Domain)
	return DomainBlockResponse{
		Blocked: blocked,
		Reason:  reason,
	}
}

// handleRefreshBlocklistRequest processes a blocklist refresh request
func (a *DomainBlockerActor) handleRefreshBlocklistRequest(ctx context.Context, req RefreshBlocklistRequest) RefreshBlocklistResponse {
	err := a.refreshBlocklist()
	if err != nil {
		return RefreshBlocklistResponse{
			Success: false,
			Error:   err.Error(),
		}
	}

	return RefreshBlocklistResponse{
		Success:    true,
		DomainCount: a.getDomainCount(),
	}
}

// handleGetBlocklistStatsRequest processes a statistics request
func (a *DomainBlockerActor) handleGetBlocklistStatsRequest(ctx context.Context, req GetBlocklistStatsRequest) BlocklistStatsResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return BlocklistStatsResponse{
		DomainCount:     a.getDomainCount(),
		LastUpdated:     a.lastUpdated,
		BlocklistURL:    a.blocklistURL,
		RefreshInterval: a.refreshInterval,
		TTL:             a.ttl,
		Expired:         !a.lastUpdated.IsZero() && time.Since(a.lastUpdated) > a.ttl,
		CacheEnabled:    a.cacheDir != "",
		CacheDir:        a.cacheDir,
	}
}

// isBlocklistExpired checks if the current blocklist has expired based on TTL
func (a *DomainBlockerActor) isBlocklistExpired() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	// If we haven't loaded any blocklist yet, it's considered expired
	if a.lastUpdated.IsZero() {
		return true
	}
	
	// Check if the blocklist age exceeds the TTL
	return time.Since(a.lastUpdated) > a.ttl
}

// backgroundRefresh performs a non-blocking refresh of the blocklist
func (a *DomainBlockerActor) backgroundRefresh() {
	logger.Debug("domain blocker actor %s: starting background refresh", a.id)
	if err := a.refreshBlocklist(); err != nil {
		logger.Warn("domain blocker actor %s: background refresh failed: %v", a.id, err)
	} else {
		logger.Info("domain blocker actor %s: background refresh completed with %d domains", a.id, a.getDomainCount())
	}
}

// refreshLoop periodically refreshes the blocklist
func (a *DomainBlockerActor) refreshLoop(ctx context.Context) {
	for {
		select {
		case <-a.refreshTicker.C:
			if err := a.refreshBlocklist(); err != nil {
				logger.Warn("domain blocker actor %s: failed to refresh blocklist: %v", a.id, err)
			}
		case <-a.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// refreshBlocklist downloads and parses the latest RPZ blocklist
func (a *DomainBlockerActor) refreshBlocklist() error {
	logger.Debug("domain blocker actor %s: refreshing blocklist from %s", a.id, a.blocklistURL)

	// Use the downloader interface to download the blocklist
	body, err := a.downloader.DownloadBlocklist(context.Background(), a.blocklistURL)
	if err != nil {
		return fmt.Errorf("failed to download blocklist: %w", err)
	}
	defer body.Close()

	// Parse RPZ format and extract domains
	domains, err := a.ParseRPZResponse(body)
	if err != nil {
		return fmt.Errorf("failed to parse RPZ blocklist: %w", err)
	}

	logger.Debug("domain blocker actor %s: parsed %d domains from RPZ", a.id, len(domains))

	// Update the matcher with new domains
	a.mu.Lock()
	a.matcher = ahocorasick.NewStringMatcher(domains)
	a.domainCount = len(domains)
	a.lastUpdated = time.Now()
	a.mu.Unlock()

	// Save to cache (non-blocking, don't fail if caching fails)
	if a.cacheDir != "" {
		if err := a.saveCachedBlocklist(domains); err != nil {
			logger.Warn("domain blocker actor %s: failed to cache blocklist: %v", a.id, err)
		} else {
			logger.Debug("domain blocker actor %s: successfully cached blocklist", a.id)
		}
	}

	logger.Info("domain blocker actor %s: refreshed blocklist with %d domains", a.id, len(domains))
	return nil
}

// parseRPZResponse parses an RPZ format response and extracts domains
func (a *DomainBlockerActor) ParseRPZResponse(body io.Reader) ([]string, error) {
	var domains []string
	domainSet := make(map[string]struct{})
	scanner := bufio.NewScanner(body)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		
		// Skip DNS SOA records, TTL settings, and other non-CNAME records
		if strings.HasPrefix(line, "$") || 
		   strings.HasPrefix(line, "@") || 
		   (!strings.Contains(line, "CNAME") && 
		    (strings.Contains(line, "SOA") || 
		     strings.Contains(line, "NS") || 
		     strings.Contains(line, "A") || 
		     strings.Contains(line, "AAAA"))) {
			continue
		}
		
		// Parse RPZ format lines
		// RPZ format typically looks like: blocked.domain.com CNAME .
		// or: *.blocked.domain.com CNAME .
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == "CNAME" {
			domain := fields[0]
			
			// Remove wildcard prefix if present
			if strings.HasPrefix(domain, "*.") {
				domain = domain[2:] // Remove "*."
			}
			
			// Skip if it's an IP address, localhost, or invalid format
			if strings.Contains(domain, "/") || 
			   strings.HasPrefix(domain, "!") || 
			   strings.Contains(domain, " ") ||
			   domain == "localhost" ||
			   strings.HasSuffix(domain, ".localhost") ||
			   strings.HasPrefix(domain, "127.") ||
			   strings.HasPrefix(domain, "10.") ||
			   strings.HasPrefix(domain, "192.168.") ||
			   strings.HasPrefix(domain, "172.") {
				continue
			}
			
			// Validate domain format
			if strings.Contains(domain, ".") && 
			   domain != "." && 
			   len(domain) > 3 && // minimum domain length like a.bc
			   !strings.HasPrefix(domain, ".") && // doesn't start with dot
			   !strings.HasSuffix(domain, ".") { // doesn't end with dot
				// Add to set for deduplication
				lowerDomain := strings.ToLower(domain)
				if _, exists := domainSet[lowerDomain]; !exists {
					domainSet[lowerDomain] = struct{}{}
				}
			}
		}
	}
	
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read RPZ data: %w", err)
	}
	
	// Convert set to slice
	domains = make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	
	return domains, nil
}

// getDomainCount returns the current number of domains in the blocklist
func (a *DomainBlockerActor) getDomainCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	return a.domainCount
}

// getDomainBlockerMetrics returns health metrics for the domain blocker
func (a *DomainBlockerActor) getDomainBlockerMetrics() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	metrics := map[string]interface{}{
		"domains_loaded":    a.getDomainCount(),
		"last_updated":      a.lastUpdated.Format(time.RFC3339),
		"blocklist_url":     a.blocklistURL,
		"refresh_interval":  a.refreshInterval.String(),
		"ttl":               a.ttl.String(),
		"cache_enabled":     a.cacheDir != "",
		"downloader_healthy": a.downloader.IsHealthy(),
		"status":            "healthy",
	}
	
	// Add cache directory if enabled
	if a.cacheDir != "" {
		metrics["cache_dir"] = a.cacheDir
	}
	
	// Check if blocklist is stale (more than 2x refresh interval old)
	if !a.lastUpdated.IsZero() && time.Since(a.lastUpdated) > 2*a.refreshInterval {
		metrics["status"] = "stale"
	}
	
	// Check if blocklist is expired (older than TTL)
	if !a.lastUpdated.IsZero() && time.Since(a.lastUpdated) > a.ttl {
		metrics["expired"] = true
	} else {
		metrics["expired"] = false
	}
	
	// Update status based on downloader health
	if !a.downloader.IsHealthy() {
		metrics["status"] = "downloader_unhealthy"
	}
	
	return metrics
}

// GetDownloader returns the blocklist downloader (for testing)
func (a *DomainBlockerActor) GetDownloader() BlocklistDownloader {
	return a.downloader
}