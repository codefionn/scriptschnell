//go:build !tinygo_embed

package tools

// This file is used when NOT building with tinygo_embed tag.
// It provides stub implementations of the embed functions.

var embeddedTinyGoArchive []byte //nolint:unused // Stub for consistency with generated embed files

// HasEmbeddedArchive returns true if a TinyGo archive is embedded in the binary
func HasEmbeddedArchive() bool {
	return false
}

// GetEmbeddedArchive returns the embedded TinyGo archive data
func GetEmbeddedArchive() []byte {
	return nil
}
