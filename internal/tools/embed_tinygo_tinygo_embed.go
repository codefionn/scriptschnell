//go:build tinygo_embed && !tinygo_has_embed_data

package tools

// This file provides stub implementations of the embed functions.
// When building with the tinygo_embed tag, the embed_tinygo_gen.go tool generates
// platform-specific embed files (e.g., embed_tinygo_linux_amd64.go) that contain
// the actual compressed TinyGo archive data and implement HasEmbeddedArchive/GetEmbeddedArchive.
// Those generated files include the 'tinygo_has_embed_data' build tag.
// This file is only included when building with tinygo_embed but WITHOUT tinygo_has_embed_data,
// which happens when no platform-specific embed file exists.

// HasEmbeddedArchive returns true if a TinyGo archive is embedded in the binary
func HasEmbeddedArchive() bool {
	return false
}

// GetEmbeddedArchive returns the embedded TinyGo archive data
func GetEmbeddedArchive() []byte {
	return nil
}
