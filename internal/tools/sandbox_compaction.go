package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	contextWindow    int    // The model's context window in tokens
	tempDir          string // Directory for writing large output files

	mu sync.RWMutex
}

// NewOutputCompactor creates a new output compactor
func NewOutputCompactor(compactionConfig config.SandboxOutputCompactionConfig, contextWindow int) *OutputCompactor {
	return &OutputCompactor{
		compactionConfig: compactionConfig,
		contextWindow:    contextWindow,
	}
}

// SetTempDir sets the directory for writing large output files
func (c *OutputCompactor) SetTempDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tempDir = dir
}

const compactionHardLimitBytes = 128 * 1024 // 128 KiB

// ShouldCompact determines if output should be compacted based on size.
// Triggers when output exceeds 10% of the context window OR is >= 128 KiB.
func (c *OutputCompactor) ShouldCompact(output string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.compactionConfig.Enabled {
		return false
	}

	// Hard size limit: always compact if output is >= 128 KiB
	if len(output) >= compactionHardLimitBytes {
		return true
	}

	if c.contextWindow == 0 {
		return false
	}

	// Soft limit: compact if output exceeds configured percentage of context window
	tokens := llm.EstimateTokenCount(output)
	threshold := int(float64(c.contextWindow) * c.compactionConfig.ContextWindowPercent)

	return tokens >= threshold
}

// Compact compacts the output by writing it to a file and returning instructions
// for the LLM to read it piece by piece, avoiding context window bloat.
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

	originalSize := len(output)

	// Write the full output to a temporary file
	outputFile, err := c.writeOutputToFile(output)
	if err != nil {
		logger.Debug("sandbox compaction: failed to write output to file: %v", err)
		// Fall back to truncation
		return c.fallbackTruncate(output, originalSize), nil
	}

	// Count total lines for the instructions
	lineCount := strings.Count(output, "\n") + 1

	// Build instructions for the LLM to read the output piece by piece
	linesPerChunk := 200
	var instructions strings.Builder
	instructions.WriteString("=== OUTPUT TOO LARGE FOR CONTEXT WINDOW ===\n")
	fmt.Fprintf(&instructions, "The sandbox output was %d bytes (%d lines), exceeding 10%% of the context window.\n", originalSize, lineCount)
	fmt.Fprintf(&instructions, "Full output has been saved to: %s\n\n", outputFile)
	instructions.WriteString("To read the output, use the read_file tool with offset and limit parameters:\n")
	fmt.Fprintf(&instructions, "  - File: %s\n", outputFile)
	fmt.Fprintf(&instructions, "  - Total lines: %d\n", lineCount)
	fmt.Fprintf(&instructions, "  - Recommended chunk size: %d lines\n", linesPerChunk)
	fmt.Fprintf(&instructions, "  - Example: read_file(path=\"%s\", offset=1, limit=%d) for the first chunk\n", outputFile, linesPerChunk)
	fmt.Fprintf(&instructions, "  - Then: read_file(path=\"%s\", offset=%d, limit=%d) for the next chunk, etc.\n\n", outputFile, linesPerChunk+1, linesPerChunk)
	instructions.WriteString("Start by reading the last chunk (the end of the output) as it often contains the most relevant information (errors, final results):\n")
	lastOffset := lineCount - linesPerChunk
	if lastOffset < 1 {
		lastOffset = 1
	}
	fmt.Fprintf(&instructions, "  read_file(path=\"%s\", offset=%d, limit=%d)\n", outputFile, lastOffset, linesPerChunk)

	compactedOutput := instructions.String()

	result := &CompactionResult{
		Output:        compactedOutput,
		WasCompacted:  true,
		OriginalSize:  originalSize,
		CompactedSize: len(compactedOutput),
		ChunksKept:    0,
	}

	logger.Debug("sandbox compaction: output saved to %s (%d bytes, %d lines), returning read instructions",
		outputFile, originalSize, lineCount)

	return result, nil
}

// writeOutputToFile writes the output to a temporary file and returns the file path
func (c *OutputCompactor) writeOutputToFile(output string) (string, error) {
	dir := c.tempDir
	if dir == "" {
		dir = os.TempDir()
	}

	// Ensure the directory exists
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	f, err := os.CreateTemp(dir, "sandbox-output-*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(output); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("failed to write output: %w", err)
	}

	return filepath.Abs(f.Name())
}

// fallbackTruncate truncates the output when file writing fails
func (c *OutputCompactor) fallbackTruncate(output string, originalSize int) *CompactionResult {
	// Keep first and last 2000 chars
	maxKeep := 2000
	if len(output) <= maxKeep*2 {
		return &CompactionResult{
			Output:        output,
			WasCompacted:  false,
			OriginalSize:  originalSize,
			CompactedSize: len(output),
		}
	}

	head := output[:maxKeep]
	if idx := strings.LastIndex(head, "\n"); idx > maxKeep/2 {
		head = head[:idx]
	}
	tail := output[len(output)-maxKeep:]
	if idx := strings.Index(tail, "\n"); idx > 0 && idx < maxKeep/2 {
		tail = tail[idx+1:]
	}

	truncated := head + "\n\n[... " + fmt.Sprintf("%d", originalSize-len(head)-len(tail)) + " bytes truncated ...]\n\n" + tail

	return &CompactionResult{
		Output:        truncated,
		WasCompacted:  true,
		OriginalSize:  originalSize,
		CompactedSize: len(truncated),
	}
}

