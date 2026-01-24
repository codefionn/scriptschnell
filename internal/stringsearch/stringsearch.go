// Package stringsearch provides string search algorithms
package stringsearch

// StringMatcher interface for string matching algorithms
type StringMatcher interface {
	Match(text []byte) []int
	MatchString(text string) []int
	FindAll(text string) []string
	Contains(text string) bool
	GetPatterns() []string
	PatternCount() int
}

// NewStringMatcher creates a new string matcher using Hybrid Chunked AC
func NewStringMatcher(patterns []string) StringMatcher {
	// Use a reasonable chunk size for memory efficiency
	// Smaller chunk size = more chunks but better precision
	// Larger chunk size = fewer chunks but less precision
	chunkSize := 4 // Good balance for most use cases
	return NewHybridChunkedAC(patterns, chunkSize)
}
