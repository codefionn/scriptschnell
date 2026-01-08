package actor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// HTTPBlocklistDownloader implements BlocklistDownloader using HTTP
type HTTPBlocklistDownloader struct {
	client    HTTPClient
	timeout   time.Duration
	userAgent string
	healthy   bool
}

// NewHTTPBlocklistDownloader creates a new HTTP blocklist downloader
func NewHTTPBlocklistDownloader(config BlocklistDownloaderConfig) *HTTPBlocklistDownloader {
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: config.Timeout}
	}
	if config.UserAgent == "" {
		config.UserAgent = "scriptschnell-domain-blocker/1.0"
	}

	return &HTTPBlocklistDownloader{
		client:    config.HTTPClient,
		timeout:   config.Timeout,
		userAgent: config.UserAgent,
		healthy:   true,
	}
}

// DownloadBlocklist downloads the blocklist from the specified URL
func (h *HTTPBlocklistDownloader) DownloadBlocklist(ctx context.Context, url string) (io.ReadCloser, error) {
	logger.Debug("HTTPBlocklistDownloader: downloading blocklist from %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		h.setHealthy(false)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", h.userAgent)
	req.Header.Set("Accept", "text/plain, */*")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := h.client.Do(req)
	if err != nil {
		h.setHealthy(false)
		return nil, fmt.Errorf("failed to download blocklist: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		h.setHealthy(false)
		return nil, fmt.Errorf("blocklist download failed with status %d", resp.StatusCode)
	}

	// Check if the response content type is appropriate
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "text") && !strings.Contains(contentType, "plain") {
		logger.Warn("HTTPBlocklistDownloader: unexpected content type: %s", contentType)
	}

	h.setHealthy(true)
	logger.Debug("HTTPBlocklistDownloader: successfully downloaded blocklist from %s", url)
	return resp.Body, nil
}

// GetLastModified returns the last modified time of the blocklist
func (h *HTTPBlocklistDownloader) GetLastModified(ctx context.Context, url string) (time.Time, error) {
	logger.Debug("HTTPBlocklistDownloader: checking last modified for %s", url)

	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		h.setHealthy(false)
		return time.Time{}, fmt.Errorf("failed to create HEAD request: %w", err)
	}

	req.Header.Set("User-Agent", h.userAgent)

	resp, err := h.client.Do(req)
	if err != nil {
		h.setHealthy(false)
		return time.Time{}, fmt.Errorf("failed to get HEAD response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.setHealthy(false)
		return time.Time{}, fmt.Errorf("HEAD request failed with status %d", resp.StatusCode)
	}

	// Try to parse Last-Modified header
	lastModifiedStr := resp.Header.Get("Last-Modified")
	if lastModifiedStr != "" {
		if lastModified, err := time.Parse(time.RFC1123, lastModifiedStr); err == nil {
			h.setHealthy(true)
			logger.Debug("HTTPBlocklistDownloader: last modified for %s is %s", url, lastModified.Format(time.RFC3339))
			return lastModified, nil
		}
	}

	// Fallback: try to parse other common date formats
	formats := []string{
		time.RFC850,
		"Mon, 2 Jan 2006 15:04:05 MST",
		time.ANSIC,
	}

	for _, format := range formats {
		if lastModified, err := time.Parse(format, lastModifiedStr); err == nil {
			h.setHealthy(true)
			logger.Debug("HTTPBlocklistDownloader: parsed last modified for %s as %s", url, lastModified.Format(time.RFC3339))
			return lastModified, nil
		}
	}

	h.setHealthy(true)
	logger.Debug("HTTPBlocklistDownloader: no valid Last-Modified header found for %s", url)
	return time.Time{}, nil
}

// IsHealthy returns true if the downloader is functioning properly
func (h *HTTPBlocklistDownloader) IsHealthy() bool {
	return h.healthy
}

// setHealthy updates the health status of the downloader
func (h *HTTPBlocklistDownloader) setHealthy(healthy bool) {
	if h.healthy != healthy {
		h.healthy = healthy
		if !healthy {
			logger.Warn("HTTPBlocklistDownloader: marked as unhealthy")
		} else {
			logger.Debug("HTTPBlocklistDownloader: marked as healthy")
		}
	}
}

// MockHTTPResponse represents a mock HTTP response for testing
type MockHTTPResponse struct {
	StatusCode int
	Body       io.ReadCloser
	Header     http.Header
}

// MockHTTPClient implements HTTPClient for testing
type MockHTTPClient struct {
	responses map[string]*MockHTTPResponse
	errors    map[string]error
	callCount map[string]int
}

// NewMockHTTPClient creates a new mock HTTP client
func NewMockHTTPClient() *MockHTTPClient {
	return &MockHTTPClient{
		responses: make(map[string]*MockHTTPResponse),
		errors:    make(map[string]error),
		callCount: make(map[string]int),
	}
}

// SetResponse sets a mock response for a URL
func (m *MockHTTPClient) SetResponse(url string, response *MockHTTPResponse) {
	m.responses[url] = response
}

// SetError sets a mock error for a URL
func (m *MockHTTPClient) SetError(url string, err error) {
	m.errors[url] = err
}

// GetCallCount returns the number of times a URL was called
func (m *MockHTTPClient) GetCallCount(url string) int {
	return m.callCount[url]
}

// Do implements the HTTPClient interface
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	m.callCount[url]++

	if err, exists := m.errors[url]; exists {
		return nil, err
	}

	if resp, exists := m.responses[url]; exists {
		return &http.Response{
			StatusCode: resp.StatusCode,
			Body:       resp.Body,
			Header:     resp.Header,
		}, nil
	}

	// Default 404 response
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("Not Found")),
		Header:     make(http.Header),
	}, nil
}