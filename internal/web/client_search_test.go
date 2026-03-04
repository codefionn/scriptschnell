package web

import (
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
)

func TestSetSearchConfig_NormalizesGoogleProviderAndSetsActive(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := config.DefaultConfig()
	client := &Client{
		cfg:  cfg,
		send: make(chan *WebMessage, 1),
	}

	msg := &WebMessage{
		Data: map[string]interface{}{
			"provider":  "google",
			"api_key":   "google-key",
			"google_cx": "search-engine-id",
		},
	}

	if err := client.setSearchConfig(msg); err != nil {
		t.Fatalf("setSearchConfig returned error: %v", err)
	}

	if got, want := cfg.Search.Provider, "google_pse"; got != want {
		t.Fatalf("active provider = %q, want %q", got, want)
	}
	if got, want := cfg.Search.GooglePSE.APIKey, "google-key"; got != want {
		t.Fatalf("google API key = %q, want %q", got, want)
	}
	if got, want := cfg.Search.GooglePSE.CX, "search-engine-id"; got != want {
		t.Fatalf("google CX = %q, want %q", got, want)
	}
}

func TestSetSearchConfig_EmptyKeyDoesNotOverwriteExisting(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := config.DefaultConfig()
	cfg.Search.Exa.APIKey = "existing-exa-key"

	client := &Client{
		cfg:  cfg,
		send: make(chan *WebMessage, 1),
	}

	msg := &WebMessage{
		Data: map[string]interface{}{
			"provider": "exa",
			"api_key":  "",
		},
	}

	if err := client.setSearchConfig(msg); err != nil {
		t.Fatalf("setSearchConfig returned error: %v", err)
	}

	if got, want := cfg.Search.Provider, "exa"; got != want {
		t.Fatalf("active provider = %q, want %q", got, want)
	}
	if got, want := cfg.Search.Exa.APIKey, "existing-exa-key"; got != want {
		t.Fatalf("exa API key was overwritten: got %q, want %q", got, want)
	}
}
