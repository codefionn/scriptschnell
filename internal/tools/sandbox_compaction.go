package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// CompactionResult contains the compacted output and metadata
type CompactionResult struct {
	Output        string `json:"output"`
	WasCompacted  bool   `json:"was_compacted"`
	OriginalSize  int    `json:"original_size"`
	CompactedSize int    `json:"compacted_size"`
	SummaryCount  int    `json:"summary_count"`
	ChunksKept    int    `json:"chunks_kept"`
}

// OutputCompactor handles compaction of large sandbox outputs
type OutputCompactor struct {
	compactionConfig config.SandboxOutputCompactionConfig
	summarizeClient  llm.Client
	contextWindow    int // The model's context window in tokens

	mu sync.RWMutex
}

// NewOutputCompactor creates a new output compactor
func NewOutputCompactor(compactionConfig config.SandboxOutputCompactionConfig, contextWindow int) *OutputCompactor {
	return &OutputCompactor{
		compactionConfig: compactionConfig,
		contextWindow:    contextWindow,
	}
}

// SetSummarizeClient sets the summarization LLM client
func (c *OutputCompactor) SetSummarizeClient(client llm.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.summarizeClient = client
}

// ShouldCompact determines if output should be compacted based on size
func (c *OutputCompactor) ShouldCompact(output string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.compactionConfig.Enabled {
		return false
	}

	if c.contextWindow == 0 {
		return false
	}

	// Estimate token count
	tokens := llm.EstimateTokenCount(output)
	threshold := int(float64(c.contextWindow) * c.compactionConfig.ContextWindowPercent)

	return tokens >= threshold
}

// Compact compacts the output using chunking and summarization
func (c *OutputCompactor) Compact(ctx context.Context, output string) (*CompactionResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.compactionConfig.Enabled {
		return &CompactionResult{
			Output:        output,
			WasCompacted:  false,
			OriginalSize:  len(output),
			CompactedSize: len(output),
		}, nil
	}

	if c.summarizeClient == nil {
		// No summarization client, just truncate
		result := &CompactionResult{
			Output:        output,
			WasCompacted:  false,
			OriginalSize:  len(output),
			CompactedSize: len(output),
		}
		logger.Debug("sandbox compaction: no summarization client available, skipping compaction")
		return result, nil
	}

	originalSize := len(output)

	// Split output into chunks
	chunks := c.splitIntoChunks(output)
	if len(chunks) == 0 {
		return &CompactionResult{
			Output:        output,
			WasCompacted:  false,
			OriginalSize:  originalSize,
			CompactedSize: originalSize,
		}, nil
	}

	// Determine how many chunks to keep at the beginning and end
	chunksToKeep := c.chunksToKeep(len(chunks))

	var resultParts []string
	var summaryParts []string
	summariesCount := 0

	// Keep first N chunks
	if chunksToKeep > 0 {
		resultParts = append(resultParts, chunks[0:chunksToKeep]...)
	}

	// Summarize middle chunks
	if len(chunks) > chunksToKeep*2 {
		middleChunks := chunks[chunksToKeep : len(chunks)-chunksToKeep]

		if len(middleChunks) > 0 {
			// Combine middle chunks into groups for summarization
			groups := c.groupChunksForSummarization(middleChunks)

			for i, group := range groups {
				groupText := strings.Join(group, "\n")
				summary, err := c.summarizeChunk(ctx, groupText, i+1, len(groups))
				if err != nil {
					logger.Debug("sandbox compaction: failed to summarize chunk group %d: %v", i+1, err)
					// If summarization fails, keep a truncated portion instead
					truncated := c.truncateGroup(group, 2000)
					summaryParts = append(summaryParts, truncated)
				} else {
					summaryParts = append(summaryParts, summary)
					summariesCount++
				}
			}
		}
	}

	// Keep last N chunks
	if chunksToKeep > 0 && len(chunks) > chunksToKeep {
		resultParts = append(resultParts, chunks[len(chunks)-chunksToKeep:]...)
	}

	// Combine all parts
	var compactedOutput strings.Builder

	// Add beginning chunks
	if len(resultParts) > 0 {
		compactedOutput.WriteString(strings.Join(resultParts, "\n"))
	}

	// Add summaries
	if len(summaryParts) > 0 {
		if compactedOutput.Len() > 0 {
			compactedOutput.WriteString("\n\n")
		}
		compactedOutput.WriteString("=== COMPACTED OUTPUT ===\n")
		compactedOutput.WriteString("The following output has been summarized to reduce size:\n\n")
		for i, summary := range summaryParts {
			if i > 0 {
				compactedOutput.WriteString("\n\n")
			}
			compactedOutput.WriteString(summary)
		}
	}

	result := &CompactionResult{
		Output:        compactedOutput.String(),
		WasCompacted:  true,
		OriginalSize:  originalSize,
		CompactedSize: compactedOutput.Len(),
		SummaryCount:  summariesCount,
		ChunksKept:    len(resultParts),
	}

	logger.Debug("sandbox compaction: compacted from %d to %d chars (ratio: %.2f), %d summaries, %d chunks kept",
		result.OriginalSize, result.CompactedSize,
		float64(result.CompactedSize)/float64(result.OriginalSize),
		result.SummaryCount, result.ChunksKept)

	return result, nil
}

// splitIntoChunks splits output into chunks based on configured chunk size
func (c *OutputCompactor) splitIntoChunks(output string) []string {
	chunkSize := c.compactionConfig.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 50000 // Default fallback
	}

	var chunks []string
	start := 0
	outputLen := len(output)

	for start < outputLen {
		end := start + chunkSize
		if end > outputLen {
			end = outputLen
		}

		// Try to break at a line boundary for better readability
		if end < outputLen {
			// Look for the last newline before the chunk size
			lastNewline := strings.LastIndex(output[start:end], "\n")
			if lastNewline > 0 && lastNewline > chunkSize/2 {
				end = start + lastNewline + 1
			}
		}

		chunks = append(chunks, output[start:end])
		start = end
	}

	return chunks
}

// chunksToKeep calculates how many chunks to keep at the beginning and end
func (c *OutputCompactor) chunksToKeep(totalChunks int) int {
	// Keep a small percentage of chunks at the start and end
	// Typically 1-2 chunks to preserve context
	keep := 1
	if totalChunks > 10 {
		keep = 2
	}
	if totalChunks > 50 {
		keep = 3
	}
	return keep
}

// groupChunksForSummarization groups chunks for summarization
func (c *OutputCompactor) groupChunksForSummarization(chunks []string) [][]string {
	// Group chunks to reduce API calls - aim for groups of ~100K chars
	targetGroupSize := c.compactionConfig.ChunkSize * 2

	var groups [][]string
	var currentGroup []string
	currentSize := 0

	for _, chunk := range chunks {
		if currentSize+len(chunk) > targetGroupSize && len(currentGroup) > 0 {
			groups = append(groups, currentGroup)
			currentGroup = []string{chunk}
			currentSize = len(chunk)
		} else {
			currentGroup = append(currentGroup, chunk)
			currentSize += len(chunk)
		}
	}

	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	return groups
}

// summarizeChunk summarizes a chunk of output using the LLM
func (c *OutputCompactor) summarizeChunk(ctx context.Context, chunk string, index, total int) (string, error) {
	prompt := fmt.Sprintf(
		"Summarize the following output from a Go program execution (part %d of %d). "+
			"Focus on key information: errors, warnings, important results, and any unexpected behavior. "+
			"Be concise but preserve important technical details.\n\nOutput:\n%s",
		index, total, chunk,
	)

	response, err := c.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	return response, nil
}

// truncateGroup truncates a group of chunks when summarization fails
func (c *OutputCompactor) truncateGroup(group []string, maxChars int) string {
	combined := strings.Join(group, "\n")
	if len(combined) <= maxChars {
		return combined
	}

	// Truncate and add indicator
	truncated := combined[:maxChars]
	// Find last newline to avoid cutting mid-line
	if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > maxChars/2 {
		truncated = truncated[:lastNewline]
	}

	return truncated + "\n\n[... output truncated due to summarization failure ...]"
}
