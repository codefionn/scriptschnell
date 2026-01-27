package tools

import (
	"testing"
)

// TestHasEmbeddedArchive tests that HasEmbeddedArchive returns the expected value
// based on the build tags.
func TestHasEmbeddedArchive(t *testing.T) {
	// When building without tinygo_embed tag, this should return false
	result := HasEmbeddedArchive()
	if result != false {
		t.Errorf("Expected HasEmbeddedArchive to return false without tinygo_embed tag, got %v", result)
	}
}

// TestGetEmbeddedArchive tests that GetEmbeddedArchive returns nil
// when building without the tinygo_embed tag.
func TestGetEmbeddedArchive(t *testing.T) {
	// When building without tinygo_embed tag, this should return nil
	result := GetEmbeddedArchive()
	if result != nil {
		t.Errorf("Expected GetEmbeddedArchive to return nil without tinygo_embed tag, got %v", result)
	}
}

// TestStubConsistency verifies that stub implementations are consistent
// between the two stub files (embed_tinygo.go and embed_tinygo_tinygo_embed.go).
func TestStubConsistency(t *testing.T) {
	// Both stub implementations should return the same values
	hasArchive := HasEmbeddedArchive()
	archiveData := GetEmbeddedArchive()

	if hasArchive != false {
		t.Errorf("Expected stub HasEmbeddedArchive to always return false, got %v", hasArchive)
	}

	if archiveData != nil {
		t.Errorf("Expected stub GetEmbeddedArchive to always return nil, got %v", archiveData)
	}
}
