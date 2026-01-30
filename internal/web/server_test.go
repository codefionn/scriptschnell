package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codefionn/scriptschnell/internal/securemem"
)

func TestStaticFileContentType(t *testing.T) {
	// Create server
	secrets := securemem.NewString("")
	defer secrets.Destroy()
	srv, err := NewServer(context.TODO(), nil, nil, secrets, false)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	mux := srv.newMux()
	req := httptest.NewRequest(http.MethodGet, "/static/js/web-client.js?token="+srv.authToken, nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)

	resp := recorder.Result()
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
