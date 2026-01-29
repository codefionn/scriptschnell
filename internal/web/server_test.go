package web

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestStaticFileContentType(t *testing.T) {
	// Create server
	srv, err := NewServer(context.TODO(), nil, nil, "", false)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start the server
	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	}()

	// Wait for server to be ready by polling the endpoint with GET
	// Note: The root path requires authentication, so we expect 401 if server is up
	var getResp *http.Response
	for i := 0; i < 10; i++ {
		getResp, err = http.Get("http://" + srv.addr + "?token=invalid-token")
		if err == nil && (getResp.StatusCode == http.StatusUnauthorized || getResp.StatusCode == http.StatusOK) {
			getResp.Body.Close()
			break
		}
		if getResp != nil {
			getResp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Server never became ready: %v", err)
	}
	if getResp != nil && !(getResp.StatusCode == http.StatusUnauthorized || getResp.StatusCode == http.StatusOK) {
		t.Fatalf("Server not ready: unexpected status %d", getResp.StatusCode)
	}
	if getResp != nil {
		getResp.Body.Close()
	}

	// Test JavaScript file using the server's actual address
	resp, err := http.Get("http://" + srv.addr + "/static/js/web-client.js?token=" + srv.authToken)
	if err != nil {
		t.Fatalf("Failed to GET /static/js/web-client.js: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body to ensure it's working
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	// Read the response body to ensure it's working
	// body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	expected := "application/javascript"
	if contentType != expected {
		t.Errorf("Expected Content-Type %s, got %s", expected, contentType)
	}
}
