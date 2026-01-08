package actor

import (
	"context"
	"io"
	"net/http"
	"time"
)

// HTTPClient defines the interface for HTTP clients (for testing)
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// BlocklistDownloader defines the interface for downloading blocklists
type BlocklistDownloader interface {
	// DownloadBlocklist downloads the blocklist from the configured URL
	DownloadBlocklist(ctx context.Context, url string) (io.ReadCloser, error)

	// GetLastModified returns the last modified time of the blocklist (if available)
	GetLastModified(ctx context.Context, url string) (time.Time, error)

	// IsHealthy returns true if the downloader is functioning properly
	IsHealthy() bool
}

// BlocklistDownloaderConfig holds configuration for a blocklist downloader
type BlocklistDownloaderConfig struct {
	HTTPClient HTTPClient
	Timeout    time.Duration
	UserAgent  string
}

// DefaultBlocklistDownloaderConfig returns the default configuration
func DefaultBlocklistDownloaderConfig() BlocklistDownloaderConfig {
	return BlocklistDownloaderConfig{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Timeout:    30 * time.Second,
		UserAgent:  "scriptschnell-domain-blocker/1.0",
	}
}
