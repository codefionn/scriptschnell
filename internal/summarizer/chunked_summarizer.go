package summarizer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// ChunkedSummarizer handles summarization of large texts by splitting into chunks
// and combining partial summaries into a coherent final result.
type ChunkedSummarizer struct {
	client llm.Client

	// Configuration
	ChunkThresholdPercent float64 // Percentage of context window (default: 0.7)
	MaxSummaryBytes       int     // Max bytes for summary input (default: 16384)
	TokenEstimator        TokenEstimator
}

// TokenEstimator estimates token count for text
type TokenEstimator func(text string) int

// NewChunkedSummarizer creates a new ChunkedSummarizer
func NewChunkedSummarizer(client llm.Client) *ChunkedSummarizer {
	return &ChunkedSummarizer{
		client:                client,
		ChunkThresholdPercent: 0.7,
		MaxSummaryBytes:       16_384,
		TokenEstimator:        DefaultTokenEstimator,
	}
}

// SummaryResult contains the result of a summarization operation
type SummaryResult struct {
	Summary        string
	ChunksUsed     int
	TotalTokens    int
	ChunkSummaries []string // Partial summaries before final combination
}

// SummarizeOptions configures the summarization process
type SummarizeOptions struct {
	Context          string // Additional context (e.g., URL, file path)
	BasePrompt       string // The main summarization goal
	MaxBytes         int    // Override default MaxSummaryBytes
	Timeout          time.Duration
	ProgressCallback func(status string)
}

// Summarize summarizes content with automatic chunking if needed
func (cs *ChunkedSummarizer) Summarize(ctx context.Context, content string, opts SummarizeOptions) (*SummaryResult, error) {
	if cs.client == nil {
		return nil, fmt.Errorf("summarization client not configured")
	}

	// Set defaults
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	maxBytes := cs.MaxSummaryBytes
	if opts.MaxBytes > 0 {
		maxBytes = opts.MaxBytes
	}

	// Get context window size
	contextWindow := cs.getContextWindowSize()
	if contextWindow == 0 {
		contextWindow = 128_000 // Fallback default
	}

	// Calculate chunk threshold
	chunkThreshold := int(float64(contextWindow) * cs.ChunkThresholdPercent)

	// Estimate token count
	estimatedTokens := cs.TokenEstimator(content)
	logger.Debug("summarizer: content size %d bytes, estimated %d tokens, threshold %d tokens", len(content), estimatedTokens, chunkThreshold)

	// If content fits, summarize directly
	if estimatedTokens <= chunkThreshold {
		if opts.ProgressCallback != nil {
			opts.ProgressCallback("Summarizing content directly")
		}
		return cs.summarizeDirectly(ctx, content, opts, maxBytes)
	}

	// Otherwise, use chunked summarization
	if opts.ProgressCallback != nil {
		opts.ProgressCallback(fmt.Sprintf("Content too large (%d tokens > %d), using chunked summarization", estimatedTokens, chunkThreshold))
	}
	return cs.summarizeWithChunking(ctx, content, opts, chunkThreshold, maxBytes)
}

// summarizeDirectly summarizes content without chunking
func (cs *ChunkedSummarizer) summarizeDirectly(ctx context.Context, content string, opts SummarizeOptions, maxBytes int) (*SummaryResult, error) {
	// Truncate if needed
	trimmedContent, wasTruncated := truncateStringToBytes(content, maxBytes)
	if wasTruncated {
		trimmedContent += fmt.Sprintf("\n\n[Content truncated to %d bytes for summarization]", maxBytes)
	}

	// Build prompt
	prompt := cs.buildSummaryPrompt(trimmedContent, opts)

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Summarize
	summary, err := cs.client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("summarization failed: %w", err)
	}

	return &SummaryResult{
		Summary:     strings.TrimSpace(summary),
		ChunksUsed:  1,
		TotalTokens: cs.TokenEstimator(content),
	}, nil
}

// summarizeWithChunking splits content and summarizes with gluing
func (cs *ChunkedSummarizer) summarizeWithChunking(ctx context.Context, content string, opts SummarizeOptions, chunkThreshold, maxBytes int) (*SummaryResult, error) {
	chunks := cs.splitContentIntoChunks(content, chunkThreshold)
	logger.Debug("summarizer: split into %d chunks", len(chunks))

	if opts.ProgressCallback != nil {
		opts.ProgressCallback(fmt.Sprintf("Split into %d chunks", len(chunks)))
	}

	var partialSummaries []string
	totalTokens := 0

	// Summarize each chunk
	for i, chunk := range chunks {
		if opts.ProgressCallback != nil {
			opts.ProgressCallback(fmt.Sprintf("Summarizing chunk %d of %d", i+1, len(chunks)))
		}

		chunkOpts := opts
		chunkOpts.BasePrompt = fmt.Sprintf("%s (Part %d of %d)", opts.BasePrompt, i+1, len(chunks))

		result, err := cs.summarizeDirectly(ctx, chunk, chunkOpts, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to summarize chunk %d: %w", i+1, err)
		}

		partialSummaries = append(partialSummaries, result.Summary)
		totalTokens += result.TotalTokens
	}

	// If single chunk, return as-is
	if len(partialSummaries) == 1 {
		return &SummaryResult{
			Summary:        partialSummaries[0],
			ChunksUsed:     1,
			TotalTokens:    totalTokens,
			ChunkSummaries: partialSummaries,
		}, nil
	}

	// Combine partial summaries
	if opts.ProgressCallback != nil {
		opts.ProgressCallback("Combining partial summaries into final result")
	}

	combinedSummaries := strings.Join(partialSummaries, "\n\n---\n\n")
	finalPrompt := cs.buildCombinationPrompt(combinedSummaries, opts)

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	finalSummary, err := cs.client.Complete(ctx, finalPrompt)
	if err != nil {
		logger.Debug("summarizer: final combination failed, returning combined partials: %v", err)
		// Fallback: return combined partial summaries
		return &SummaryResult{
			Summary:        combinedSummaries,
			ChunksUsed:     len(partialSummaries),
			TotalTokens:    totalTokens,
			ChunkSummaries: partialSummaries,
		}, nil
	}

	return &SummaryResult{
		Summary:        strings.TrimSpace(finalSummary),
		ChunksUsed:     len(partialSummaries),
		TotalTokens:    totalTokens,
		ChunkSummaries: partialSummaries,
	}, nil
}

// splitContentIntoChunks splits content based on estimated token count
func (cs *ChunkedSummarizer) splitContentIntoChunks(content string, maxTokensPerChunk int) []string {
	if cs.TokenEstimator(content) <= maxTokensPerChunk {
		return []string{content}
	}

	var chunks []string
	runes := []rune(content)
	chunkSize := maxTokensPerChunk * 4 // Convert to approximate character count

	for len(runes) > 0 {
		if len(runes) <= chunkSize {
			chunks = append(chunks, string(runes))
			break
		}

		// Try to split at paragraph boundary
		splitPoint := chunkSize
		for i := chunkSize; i > chunkSize-200 && i > 0; i-- {
			if runes[i] == '\n' && (i+1 >= len(runes) || runes[i+1] == '\n') {
				splitPoint = i + 1
				break
			}
		}

		chunks = append(chunks, string(runes[:splitPoint]))
		runes = runes[splitPoint:]
	}

	return chunks
}

// getContextWindowSize determines the context window size of the summarization model
func (cs *ChunkedSummarizer) getContextWindowSize() int {
	if cs.client == nil {
		return 0
	}

	modelID := cs.client.GetModelName()
	if modelID == "" {
		return 0
	}

	family := llm.DetectModelFamily(modelID)
	return llm.DetectContextWindow(modelID, family)
}

// buildSummaryPrompt builds the prompt for direct summarization
func (cs *ChunkedSummarizer) buildSummaryPrompt(content string, opts SummarizeOptions) string {
	var sb strings.Builder

	if opts.Context != "" {
		sb.WriteString(fmt.Sprintf("Context: %s\n\n", opts.Context))
	}

	sb.WriteString(fmt.Sprintf("Summarize the following content for this goal: %s\n\n", opts.BasePrompt))
	sb.WriteString("Content:\n")
	sb.WriteString(content)

	return sb.String()
}

// buildCombinationPrompt builds the prompt for combining partial summaries
func (cs *ChunkedSummarizer) buildCombinationPrompt(combinedSummaries string, opts SummarizeOptions) string {
	var sb strings.Builder

	if opts.Context != "" {
		sb.WriteString(fmt.Sprintf("Context: %s\n\n", opts.Context))
	}

	sb.WriteString(fmt.Sprintf("Combine these partial summaries into a coherent final summary for this goal: \"%s\"\n\n", opts.BasePrompt))
	sb.WriteString("Partial summaries:\n")
	sb.WriteString(combinedSummaries)

	return sb.String()
}

// DefaultTokenEstimator provides a rough token count estimate (4 chars per token)
func DefaultTokenEstimator(text string) int {
	return len(text) / 4
}

// truncateStringToBytes trims a string to the specified byte limit without breaking characters
func truncateStringToBytes(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	var (
		builder strings.Builder
		used    int
	)

	for _, r := range s {
		rb := []byte(string(r))
		if used+len(rb) > limit {
			break
		}
		builder.Write(rb)
		used += len(rb)
	}

	return builder.String(), true
}
