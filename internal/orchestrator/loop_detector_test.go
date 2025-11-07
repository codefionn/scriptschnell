package orchestrator

import (
	"strings"
	"testing"
)

func TestLoopDetector_BasicSentenceSplitting(t *testing.T) {
	ld := NewLoopDetector()

	text := "This is sentence one. This is sentence two! This is sentence three?"
	isLoop, _, _ := ld.AddText(text)

	if isLoop {
		t.Errorf("Expected no loop for initial text")
	}

	sentenceCount, _, _ := ld.GetStats()
	if sentenceCount != 3 {
		t.Errorf("Expected 3 sentences, got %d", sentenceCount)
	}
}

func TestLoopDetector_SimpleLoop(t *testing.T) {
	ld := NewLoopDetector()

	// Add the same sentence 11 times (threshold is 10)
	for i := 0; i < 11; i++ {
		isLoop, pattern, count := ld.AddText("This is a repeated sentence.")

		if i < 9 {
			if isLoop {
				t.Errorf("Iteration %d: Expected no loop yet", i)
			}
		} else {
			if !isLoop {
				t.Errorf("Iteration %d: Expected loop to be detected", i)
			}
			if count < 10 {
				t.Errorf("Expected count>=10, got %d", count)
			}
			if !strings.Contains(pattern, "This is a repeated sentence") {
				t.Errorf("Expected pattern to contain the repeated sentence, got: %s", pattern)
			}
		}
	}
}

func TestLoopDetector_NGramLoop(t *testing.T) {
	ld := NewLoopDetector()

	// Create a 2-sentence pattern and repeat it
	pattern1 := "First sentence. Second sentence."

	// Add it 11 times (threshold is 10)
	for i := 0; i < 11; i++ {
		isLoop, detectedPattern, count := ld.AddText(pattern1)

		if i < 9 {
			if isLoop {
				t.Errorf("Iteration %d: Expected no loop yet", i)
			}
		} else {
			if !isLoop {
				t.Errorf("Iteration %d: Expected loop to be detected", i)
			}
			if count < 10 {
				t.Errorf("Expected count>=10, got %d", count)
			}
			t.Logf("Detected pattern: %s (count=%d)", detectedPattern, count)
		}
	}
}

func TestLoopDetector_MaxSentencesLimit(t *testing.T) {
	ld := NewLoopDetector()

	// Add more than maxSentences (100) unique sentences
	for i := 0; i < 150; i++ {
		text := strings.Repeat("Sentence ", i+1) + "."
		ld.AddText(text)
	}

	sentenceCount, _, _ := ld.GetStats()
	if sentenceCount > maxSentences {
		t.Errorf("Expected sentence count <= %d, got %d", maxSentences, sentenceCount)
	}
}

func TestLoopDetector_MaxCharsLimit(t *testing.T) {
	ld := NewLoopDetector()

	// Add sentences until we exceed maxTotalChars
	longSentence := strings.Repeat("word ", 1000) + "."
	for i := 0; i < 20; i++ {
		ld.AddText(longSentence)
	}

	_, totalChars, _ := ld.GetStats()
	if totalChars > maxTotalChars {
		t.Errorf("Expected total chars <= %d, got %d", maxTotalChars, totalChars)
	}
}

func TestLoopDetector_Reset(t *testing.T) {
	ld := NewLoopDetector()

	// Add some sentences
	ld.AddText("Sentence one. Sentence two. Sentence three.")

	sentenceCount, _, _ := ld.GetStats()
	if sentenceCount != 3 {
		t.Errorf("Expected 3 sentences before reset, got %d", sentenceCount)
	}

	// Reset
	ld.Reset()

	sentenceCount, totalChars, patternCount := ld.GetStats()
	if sentenceCount != 0 {
		t.Errorf("Expected 0 sentences after reset, got %d", sentenceCount)
	}
	if totalChars != 0 {
		t.Errorf("Expected 0 chars after reset, got %d", totalChars)
	}
	if patternCount != 0 {
		t.Errorf("Expected 0 patterns after reset, got %d", patternCount)
	}
}

func TestLoopDetector_NoLoopWithVariation(t *testing.T) {
	ld := NewLoopDetector()

	// Add similar but not identical sentences
	for i := 0; i < 15; i++ {
		text := "This is sentence number " + string(rune('0'+i)) + "."
		isLoop, _, _ := ld.AddText(text)
		if isLoop {
			t.Errorf("Iteration %d: Expected no loop with varying sentences", i)
		}
	}
}

func TestLoopDetector_EmptyText(t *testing.T) {
	ld := NewLoopDetector()

	isLoop, _, _ := ld.AddText("")
	if isLoop {
		t.Errorf("Expected no loop for empty text")
	}

	sentenceCount, _, _ := ld.GetStats()
	if sentenceCount != 0 {
		t.Errorf("Expected 0 sentences for empty text, got %d", sentenceCount)
	}
}

func TestLoopDetector_WhitespaceNormalization(t *testing.T) {
	ld := NewLoopDetector()

	// These should be treated as the same sentence after normalization
	ld.AddText("This   is   a   sentence.")
	ld.AddText("This is a sentence.")

	sentenceCount, _, _ := ld.GetStats()
	// Should have 2 sentences in buffer
	if sentenceCount != 2 {
		t.Errorf("Expected 2 sentences, got %d", sentenceCount)
	}
}
