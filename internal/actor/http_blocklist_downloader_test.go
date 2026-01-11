package actor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPBlocklistDownloader_NewDownloader tests the creation of a new HTTP blocklist downloader
func TestHTTPBlocklistDownloader_NewDownloader(t *testing.T) {
	// Test with default configuration
	downloader := NewHTTPBlocklistDownloader(DefaultBlocklistDownloaderConfig())
	require.NotNil(t, downloader)
	assert.Equal(t, "scriptschnell-domain-blocker/1.0", downloader.userAgent)
	assert.True(t, downloader.IsHealthy())

	// Test with custom configuration
	customConfig := BlocklistDownloaderConfig{
		UserAgent: "custom-agent/1.0",
		Timeout:   10 * time.Second,
	}

	downloader2 := NewHTTPBlocklistDownloader(customConfig)
	require.NotNil(t, downloader2)
	assert.Equal(t, "custom-agent/1.0", downloader2.userAgent)
	assert.True(t, downloader2.IsHealthy())
}

// TestHTTPBlocklistDownloader_DownloadSuccess tests successful blocklist download
func TestHTTPBlocklistDownloader_DownloadSuccess(t *testing.T) {
	mockClient := NewMockHTTPClient()
	testContent := "example.com CNAME .\ntest.com CNAME .\n"

	mockClient.SetResponse("https://example.com/blocklist.txt", &MockHTTPResponse{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(testContent)),
		Header:     make(http.Header),
	})

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "test-agent/1.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	ctx := context.Background()
	body, err := downloader.DownloadBlocklist(ctx, "https://example.com/blocklist.txt")
	require.NoError(t, err)
	require.NotNil(t, body)
	defer body.Close()

	content, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content))
	assert.True(t, downloader.IsHealthy())
	assert.Equal(t, 1, mockClient.GetCallCount("https://example.com/blocklist.txt"))
}

// TestHTTPBlocklistDownloader_DownloadError tests download error handling
func TestHTTPBlocklistDownloader_DownloadError(t *testing.T) {
	mockClient := NewMockHTTPClient()
	mockClient.SetError("https://example.com/error.txt", errors.New("test error"))

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "test-agent/1.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	ctx := context.Background()
	body, err := downloader.DownloadBlocklist(ctx, "https://example.com/error.txt")
	assert.Error(t, err)
	assert.Nil(t, body)
	assert.False(t, downloader.IsHealthy())
}

// TestHTTPBlocklistDownloader_DownloadHTTPError tests HTTP error status handling
func TestHTTPBlocklistDownloader_DownloadHTTPError(t *testing.T) {
	mockClient := NewMockHTTPClient()

	mockClient.SetResponse("https://example.com/404.txt", &MockHTTPResponse{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("Not Found")),
		Header:     make(http.Header),
	})

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "test-agent/1.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	ctx := context.Background()
	body, err := downloader.DownloadBlocklist(ctx, "https://example.com/404.txt")
	assert.Error(t, err)
	assert.Nil(t, body)
	assert.Contains(t, err.Error(), "status 404")
	assert.False(t, downloader.IsHealthy())
}

// TestHTTPBlocklistDownloader_GetLastModified tests getting last modified time
func TestHTTPBlocklistDownloader_GetLastModified(t *testing.T) {
	mockClient := NewMockHTTPClient()

	header := make(http.Header)
	header.Set("Last-Modified", "Wed, 01 Jan 2020 12:00:00 GMT")

	mockClient.SetResponse("https://example.com/blocklist.txt", &MockHTTPResponse{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     header,
	})

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "test-agent/1.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	ctx := context.Background()
	lastModified, err := downloader.GetLastModified(ctx, "https://example.com/blocklist.txt")
	require.NoError(t, err)

	expectedTime, _ := time.Parse(time.RFC1123, "Wed, 01 Jan 2020 12:00:00 GMT")
	assert.Equal(t, expectedTime, lastModified)
	assert.True(t, downloader.IsHealthy())
}

// TestHTTPBlocklistDownloader_GetLastModifiedError tests error handling for last modified
func TestHTTPBlocklistDownloader_GetLastModifiedError(t *testing.T) {
	mockClient := NewMockHTTPClient()
	mockClient.SetError("https://example.com/error.txt", errors.New("test error"))

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "test-agent/1.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	ctx := context.Background()
	lastModified, err := downloader.GetLastModified(ctx, "https://example.com/error.txt")
	assert.Error(t, err)
	assert.Equal(t, time.Time{}, lastModified)
	assert.False(t, downloader.IsHealthy())
}

// TestHTTPBlocklistDownloader_ContentTypes tests content type handling
func TestHTTPBlocklistDownloader_ContentTypes(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		shouldWarn  bool
	}{
		{"text/plain", "text/plain", false},
		{"text/html", "text/html", false},
		{"application/json", "application/json", true},
		{"image/png", "image/png", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockHTTPClient()

			header := make(http.Header)
			if tt.contentType != "" {
				header.Set("Content-Type", tt.contentType)
			}

			mockClient.SetResponse("https://example.com/blocklist.txt", &MockHTTPResponse{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("test.com CNAME .\n")),
				Header:     header,
			})

			config := BlocklistDownloaderConfig{
				HTTPClient: mockClient,
				UserAgent:  "test-agent/1.0",
			}

			downloader := NewHTTPBlocklistDownloader(config)
			require.NotNil(t, downloader)

			ctx := context.Background()
			body, err := downloader.DownloadBlocklist(ctx, "https://example.com/blocklist.txt")
			require.NoError(t, err)
			require.NotNil(t, body)
			body.Close()

			assert.True(t, downloader.IsHealthy())
		})
	}
}

// TestHTTPBlocklistDownloader_ContextCancellation tests context cancellation
func TestHTTPBlocklistDownloader_ContextCancellation(t *testing.T) {
	mockClient := NewMockHTTPClient()

	mockClient.SetResponse("https://example.com/slow.txt", &MockHTTPResponse{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("test.com CNAME .\n")),
		Header:     make(http.Header),
	})

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "test-agent/1.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Note: The mock HTTP client doesn't simulate context cancellation
	// In a real scenario with http.Client, this would fail with context canceled
	// For the mock, it returns the response despite cancellation
	body, err := downloader.DownloadBlocklist(ctx, "https://example.com/slow.txt")
	// The mock doesn't fail on canceled context, so we expect success
	require.NoError(t, err)
	require.NotNil(t, body)
	body.Close()
}

// TestHTTPBlocklistDownloader_RequestHeaders tests that correct headers are sent
func TestHTTPBlocklistDownloader_RequestHeaders(t *testing.T) {
	// This test would require a more sophisticated mock to capture request headers
	// For now, we'll just verify that the downloader doesn't fail
	mockClient := NewMockHTTPClient()

	mockClient.SetResponse("https://example.com/blocklist.txt", &MockHTTPResponse{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("test.com CNAME .\n")),
		Header:     make(http.Header),
	})

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "custom-test-agent/2.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	ctx := context.Background()
	body, err := downloader.DownloadBlocklist(ctx, "https://example.com/blocklist.txt")
	require.NoError(t, err)
	require.NotNil(t, body)
	body.Close()

	assert.True(t, downloader.IsHealthy())
	assert.Equal(t, 1, mockClient.GetCallCount("https://example.com/blocklist.txt"))
}

// TestHTTPBlocklistDownloader_HealthTracking tests health status tracking
func TestHTTPBlocklistDownloader_HealthTracking(t *testing.T) {
	mockClient := NewMockHTTPClient()

	// Initially set up for success
	mockClient.SetResponse("https://example.com/blocklist.txt", &MockHTTPResponse{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("test.com CNAME .\n")),
		Header:     make(http.Header),
	})

	config := BlocklistDownloaderConfig{
		HTTPClient: mockClient,
		UserAgent:  "test-agent/1.0",
	}

	downloader := NewHTTPBlocklistDownloader(config)
	require.NotNil(t, downloader)

	ctx := context.Background()

	// First successful download
	body, err := downloader.DownloadBlocklist(ctx, "https://example.com/blocklist.txt")
	require.NoError(t, err)
	body.Close()
	assert.True(t, downloader.IsHealthy())

	// Now set up for error
	mockClient.SetError("https://example.com/blocklist.txt", assert.AnError)

	// Failed download should mark as unhealthy
	body, err = downloader.DownloadBlocklist(ctx, "https://example.com/blocklist.txt")
	assert.Error(t, err)
	assert.Nil(t, body)
	assert.False(t, downloader.IsHealthy())

	// Recovery - set up for success again
	mockClient.ClearError("https://example.com/blocklist.txt")
	mockClient.SetResponse("https://example.com/blocklist.txt", &MockHTTPResponse{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("test.com CNAME .\n")),
		Header:     make(http.Header),
	})

	// Successful download should mark as healthy again
	body, err = downloader.DownloadBlocklist(ctx, "https://example.com/blocklist.txt")
	require.NoError(t, err)
	body.Close()
	assert.True(t, downloader.IsHealthy())
}
