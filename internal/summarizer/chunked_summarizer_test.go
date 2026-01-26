package summarizer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// MockClient is a mock implementation of llm.Client for testing
type MockClient struct {
	CompleteFunc func(ctx context.Context, prompt string) (string, error)
	modelName    string
}

func (m *MockClient) CompleteWithRequest(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	resp, err := m.CompleteFunc(ctx, req.SystemPrompt+"\n"+formatMessages(req.Messages))
	return &llm.CompletionResponse{Content: resp}, err
}

func (m *MockClient) Complete(ctx context.Context, prompt string) (string, error) {
	if m.CompleteFunc != nil {
		return m.CompleteFunc(ctx, prompt)
	}
	return "mock summary", nil
}

func (m *MockClient) Stream(ctx context.Context, req *llm.CompletionRequest, callback func(chunk string) error) error {
	return nil
}

func (m *MockClient) GetLastResponseID() string {
	return ""
}

func (m *MockClient) SetPreviousResponseID(responseID string) {
}

func (m *MockClient) GetModelName() string {
	if m.modelName != "" {
		return m.modelName
	}
	return "claude-3-haiku-20240307"
}

func formatMessages(messages []*llm.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(msg.Role)
		sb.WriteString(": ")
		sb.WriteString(msg.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestChunkedSummarizer_SummarizeDirectly(t *testing.T) {
	mockClient := &MockClient{
		CompleteFunc: func(ctx context.Context, prompt string) (string, error) {
			return "Direct summary of the content", nil
		},
	}

	summarizer := NewChunkedSummarizer(mockClient)

	result, err := summarizer.Summarize(context.Background(), "Short content that fits", SummarizeOptions{
		BasePrompt: "Extract key points",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Summary != "Direct summary of the content" {
		t.Fatalf("unexpected summary: %s", result.Summary)
	}

	if result.ChunksUsed != 1 {
		t.Fatalf("expected 1 chunk, got %d", result.ChunksUsed)
	}
}

func TestChunkedSummarizer_SummarizeWithChunking(t *testing.T) {
	callCount := 0
	mockClient := &MockClient{
		CompleteFunc: func(ctx context.Context, prompt string) (string, error) {
			callCount++
			truncated, _ := truncateStringToBytes(prompt, 200)
			t.Logf("LLM call #%d: %s", callCount, truncated)
			if strings.Contains(prompt, "Part 1 of") {
				return "Summary of part 1", nil
			} else if strings.Contains(prompt, "Part 2 of") {
				return "Summary of part 2", nil
			} else if strings.Contains(prompt, "Part 3 of") {
				return "Summary of part 3", nil
			} else if strings.Contains(prompt, "Combine these partial summaries") {
				return "Final combined summary", nil
			}
			return fmt.Sprintf("Default summary for call #%d", callCount), nil
		},
	}

	summarizer := NewChunkedSummarizer(mockClient)
	summarizer.ChunkThresholdPercent = 0.05 // Very low threshold to force chunking

	// Create large content (enough to trigger chunking with low threshold)
	largeContent := strings.Repeat("This is a paragraph with enough content to force chunking. ", 2000)

	result, err := summarizer.Summarize(context.Background(), largeContent, SummarizeOptions{
		BasePrompt: "Extract key information",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summaryTruncated, _ := truncateStringToBytes(result.Summary, 100)
	t.Logf("Result: ChunksUsed=%d, Summary=%s", result.ChunksUsed, summaryTruncated)

	// Accept either final combined summary or combined partials
	if result.Summary != "Final combined summary" && !strings.Contains(result.Summary, "Summary of part") {
		t.Fatalf("unexpected final summary: %s", result.Summary)
	}

	if result.ChunksUsed < 1 {
		t.Fatalf("expected at least 1 chunk, got %d", result.ChunksUsed)
	}

	// Verify we got multiple calls (chunks + combination)
	if callCount < 2 {
		t.Fatalf("expected multiple LLM calls, got %d", callCount)
	}
}

func TestChunkedSummarizer_SummarizeWithFallback(t *testing.T) {
	chunkSummaries := []string{}
	mockClient := &MockClient{
		CompleteFunc: func(ctx context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "Combine these partial summaries") {
				return "", fmt.Errorf("combination failed")
			}
			summary := "Partial summary"
			chunkSummaries = append(chunkSummaries, summary)
			return summary, nil
		},
	}

	summarizer := NewChunkedSummarizer(mockClient)
	summarizer.ChunkThresholdPercent = 0.1

	largeContent := strings.Repeat("Paragraph. ", 1000)

	result, err := summarizer.Summarize(context.Background(), largeContent, SummarizeOptions{
		BasePrompt: "Summarize",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return combined partial summaries when final combination fails
	if !strings.Contains(result.Summary, "Partial summary") {
		t.Fatalf("expected fallback to combined partials, got: %s", result.Summary)
	}
}

func TestChunkedSummarizer_ProgressCallback(t *testing.T) {
	progressMessages := []string{}
	mockClient := &MockClient{
		CompleteFunc: func(ctx context.Context, prompt string) (string, error) {
			return "Summary", nil
		},
	}

	summarizer := NewChunkedSummarizer(mockClient)
	summarizer.ChunkThresholdPercent = 0.05 // Very low to force chunking

	// Create very large content to ensure chunking
	largeContent := strings.Repeat("This is more content. ", 5000)

	_, err := summarizer.Summarize(context.Background(), largeContent, SummarizeOptions{
		BasePrompt: "Summarize",
		ProgressCallback: func(status string) {
			t.Logf("Progress: %s", status)
			progressMessages = append(progressMessages, status)
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(progressMessages) == 0 {
		t.Fatal("expected progress callback to be called")
	}

	// Verify some expected progress messages
	foundChunkMsg := false
	foundCombineMsg := false
	for _, msg := range progressMessages {
		if strings.Contains(msg, "chunk") || strings.Contains(msg, "Chunk") {
			foundChunkMsg = true
		}
		if strings.Contains(msg, "Combine") || strings.Contains(msg, "combining") || strings.Contains(msg, "Combining") {
			foundCombineMsg = true
		}
	}

	t.Logf("Progress messages: %v", progressMessages)

	if !foundChunkMsg {
		t.Error("expected chunking progress message")
	}
	if !foundCombineMsg {
		t.Error("expected combination progress message")
	}
}

func TestDefaultTokenEstimator(t *testing.T) {
	// Test basic estimation
	text := "This is a test string with twenty characters"
	tokens := DefaultTokenEstimator(text)
	if tokens <= 0 {
		t.Fatal("expected positive token estimate")
	}

	// Verify roughly 4 chars per token
	expectedTokens := len(text) / 4
	if tokens != expectedTokens {
		t.Fatalf("expected %d tokens, got %d", expectedTokens, tokens)
	}
}

func TestTruncateStringToBytes(t *testing.T) {
	// Test no truncation needed
	result, truncated := truncateStringToBytes("short", 10)
	if truncated {
		t.Fatal("unexpected truncation")
	}
	if result != "short" {
		t.Fatalf("unexpected result: %s", result)
	}

	// Test truncation
	result, truncated = truncateStringToBytes("longer text", 8)
	if !truncated {
		t.Fatal("expected truncation")
	}
	// Should not cut a multi-byte character
	if len(result) > 8 {
		t.Fatalf("result too long: %d bytes", len(result))
	}
}

func TestChunkedSummarizer_NoClient(t *testing.T) {
	summarizer := NewChunkedSummarizer(nil)

	_, err := summarizer.Summarize(context.Background(), "content", SummarizeOptions{})

	if err == nil {
		t.Fatal("expected error when client is nil")
	}

	if !strings.Contains(err.Error(), "summarization client not configured") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestChunkedSummarizer_ContextInPrompt(t *testing.T) {
	lastPrompt := ""
	mockClient := &MockClient{
		CompleteFunc: func(ctx context.Context, prompt string) (string, error) {
			lastPrompt = prompt
			return "Summary", nil
		},
	}

	summarizer := NewChunkedSummarizer(mockClient)

	_, err := summarizer.Summarize(context.Background(), "content", SummarizeOptions{
		BasePrompt: "Extract info",
		Context:    "https://example.com",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(lastPrompt, "https://example.com") {
		t.Error("expected context to be included in prompt")
	}

	if !strings.Contains(lastPrompt, "Extract info") {
		t.Error("expected base prompt to be included")
	}
}
