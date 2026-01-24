// Package stringsearch provides string search algorithms including Hybrid Chunked AC
package stringsearch

import (
	"sort"
	"strings"
	"sync"
)

// HybridChunkedAC implements a memory-efficient string matching algorithm
// that combines chunking with pattern matching to reduce memory usage
type HybridChunkedAC struct {
	chunkSize      int
	chunkIndex     map[string][]int
	patterns       []string
	patternIndices map[string]int
	mu             sync.RWMutex
}

// NewHybridChunkedAC creates a new Hybrid Chunked AC matcher
func NewHybridChunkedAC(patterns []string, chunkSize int) *HybridChunkedAC {
	if chunkSize <= 0 {
		chunkSize = 3 // Default chunk size
	}

	m := &HybridChunkedAC{
		chunkSize:      chunkSize,
		chunkIndex:     make(map[string][]int),
		patterns:       patterns,
		patternIndices: make(map[string]int),
	}

	m.buildIndex()
	return m
}

// buildIndex builds the chunk-based index for efficient searching
func (m *HybridChunkedAC) buildIndex() {
	// Create pattern index for quick lookup
	for i, pattern := range m.patterns {
		m.patternIndices[pattern] = i
	}

	// Build chunk index
	for i, pattern := range m.patterns {
		lowerPattern := strings.ToLower(pattern)
		// Generate chunks for this pattern
		chunks := m.generateChunks(lowerPattern)
		// Add chunks to index
		for _, chunk := range chunks {
			m.chunkIndex[chunk] = append(m.chunkIndex[chunk], i)
		}
	}

	// Sort indices for each chunk to enable binary search
	for _, indices := range m.chunkIndex {
		sort.Ints(indices)
	}
}

// generateChunks generates chunks from a pattern
func (m *HybridChunkedAC) generateChunks(pattern string) []string {
	var chunks []string
	length := len(pattern)

	// Generate overlapping chunks
	for i := 0; i <= length-m.chunkSize; i++ {
		chunk := pattern[i : i+m.chunkSize]
		chunks = append(chunks, chunk)
	}

	// If pattern is shorter than chunk size, use the whole pattern
	if length < m.chunkSize {
		chunks = append(chunks, pattern)
	}

	return chunks
}

// Match finds all patterns that match in the given text
func (m *HybridChunkedAC) Match(text []byte) []int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	textStr := strings.ToLower(string(text))
	var matches []int
	matchedPatterns := make(map[int]struct{})

	// Generate chunks from the text
	textChunks := m.generateChunks(textStr)

	// For each text chunk, find candidate patterns
	for _, chunk := range textChunks {
		if candidateIndices, ok := m.chunkIndex[chunk]; ok {
			// Check each candidate pattern
			for _, patternIdx := range candidateIndices {
				if _, alreadyMatched := matchedPatterns[patternIdx]; alreadyMatched {
					continue
				}

				pattern := m.patterns[patternIdx]
				if strings.Contains(textStr, pattern) {
					matches = append(matches, patternIdx)
					matchedPatterns[patternIdx] = struct{}{}
				}
			}
		}
	}

	return matches
}

// MatchString finds all patterns that match in the given text string
func (m *HybridChunkedAC) MatchString(text string) []int {
	return m.Match([]byte(text))
}

// FindAll returns all matching pattern strings in the text
func (m *HybridChunkedAC) FindAll(text string) []string {
	indices := m.MatchString(text)
	var results []string
	for _, idx := range indices {
		results = append(results, m.patterns[idx])
	}
	return results
}

// Contains checks if any pattern matches in the text
func (m *HybridChunkedAC) Contains(text string) bool {
	indices := m.MatchString(text)
	return len(indices) > 0
}

// GetPatterns returns all patterns
func (m *HybridChunkedAC) GetPatterns() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.patterns
}

// PatternCount returns the number of patterns
func (m *HybridChunkedAC) PatternCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.patterns)
}
