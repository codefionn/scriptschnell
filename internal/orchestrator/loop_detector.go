package orchestrator

import (
	"regexp"
	"strings"
	"sync"

	"github.com/statcode-ai/statcode-ai/internal/logger"
)

const (
	maxSentences       = 100   // Maximum number of sentences to track
	maxTotalChars      = 16384 // Maximum total characters to track
	loopThreshold      = 10    // Number of repetitions to trigger loop detection
	maxNGramSize       = 10    // Maximum n-gram size to check (1-10 sentences)
)

// LoopDetector detects repetitive text patterns in LLM responses
type LoopDetector struct {
	mu            sync.Mutex
	sentences     []string          // Rolling buffer of sentences
	totalChars    int               // Total characters in buffer
	patternCounts map[string]int    // Pattern -> occurrence count
	sentenceRegex *regexp.Regexp    // Regex for sentence splitting
}

// NewLoopDetector creates a new loop detector
func NewLoopDetector() *LoopDetector {
	// Regex to split on sentence boundaries (., !, ?, with optional quotes/parens)
	// Handles common abbreviations by requiring space or end of string after punctuation
	sentenceRegex := regexp.MustCompile(`[.!?]+(?:\s+|["'\)]*\s+|["'\)]*$)`)

	return &LoopDetector{
		sentences:     make([]string, 0, maxSentences),
		totalChars:    0,
		patternCounts: make(map[string]int),
		sentenceRegex: sentenceRegex,
	}
}

// splitSentences splits text into sentences and returns cleaned sentences
func (ld *LoopDetector) splitSentences(text string) []string {
	if text == "" {
		return nil
	}

	// Split by sentence boundaries
	parts := ld.sentenceRegex.Split(text, -1)

	sentences := make([]string, 0, len(parts))
	for _, part := range parts {
		// Clean and normalize
		sentence := strings.TrimSpace(part)
		if sentence == "" {
			continue
		}

		// Normalize whitespace
		sentence = strings.Join(strings.Fields(sentence), " ")
		sentences = append(sentences, sentence)
	}

	return sentences
}

// addSentence adds a sentence to the buffer, maintaining size and char limits
func (ld *LoopDetector) addSentence(sentence string) {
	sentenceLen := len(sentence)

	// Add to buffer
	ld.sentences = append(ld.sentences, sentence)
	ld.totalChars += sentenceLen

	// Trim buffer if exceeds limits
	for (len(ld.sentences) > maxSentences || ld.totalChars > maxTotalChars) && len(ld.sentences) > 0 {
		removed := ld.sentences[0]
		ld.sentences = ld.sentences[1:]
		ld.totalChars -= len(removed)
	}
}

// generateNGram creates a normalized pattern string from n consecutive sentences
func (ld *LoopDetector) generateNGram(startIdx, n int) string {
	if startIdx+n > len(ld.sentences) {
		return ""
	}

	// Join sentences with a delimiter
	pattern := strings.Join(ld.sentences[startIdx:startIdx+n], " | ")
	return pattern
}

// checkForLoops checks if any n-gram pattern has repeated more than the threshold
// Returns (isLoop bool, pattern string, count int)
func (ld *LoopDetector) checkForLoops() (bool, string, int) {
	numSentences := len(ld.sentences)
	if numSentences < 2 {
		return false, "", 0
	}

	// Clear pattern counts for fresh analysis
	ld.patternCounts = make(map[string]int)

	// Check n-grams from size 1 to maxNGramSize (or numSentences, whichever is smaller)
	maxN := maxNGramSize
	if numSentences < maxN {
		maxN = numSentences
	}

	for n := 1; n <= maxN; n++ {
		// Count occurrences of each n-gram pattern
		for i := 0; i <= numSentences-n; i++ {
			pattern := ld.generateNGram(i, n)
			if pattern == "" {
				continue
			}

			ld.patternCounts[pattern]++

			// Check if this pattern exceeded threshold
			if ld.patternCounts[pattern] > loopThreshold {
				logger.Warn("Loop detected: %d-gram pattern repeated %d times", n, ld.patternCounts[pattern])
				return true, pattern, ld.patternCounts[pattern]
			}
		}
	}

	return false, "", 0
}

// AddText adds new text to the detector and checks for loops
// Returns (isLoop bool, pattern string, count int)
func (ld *LoopDetector) AddText(text string) (bool, string, int) {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	// Split into sentences
	newSentences := ld.splitSentences(text)
	if len(newSentences) == 0 {
		return false, "", 0
	}

	logger.Debug("LoopDetector: Adding %d new sentences", len(newSentences))

	// Add each sentence to buffer
	for _, sentence := range newSentences {
		ld.addSentence(sentence)
	}

	logger.Debug("LoopDetector: Buffer now has %d sentences (%d chars)", len(ld.sentences), ld.totalChars)

	// Check for loops
	return ld.checkForLoops()
}

// Reset clears the detector state
func (ld *LoopDetector) Reset() {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	ld.sentences = make([]string, 0, maxSentences)
	ld.totalChars = 0
	ld.patternCounts = make(map[string]int)

	logger.Debug("LoopDetector: Reset")
}

// GetStats returns current statistics for debugging
func (ld *LoopDetector) GetStats() (sentenceCount int, totalChars int, patternCount int) {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	return len(ld.sentences), ld.totalChars, len(ld.patternCounts)
}
