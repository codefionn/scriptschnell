package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
)

func TestOutputCompactor_ShouldCompact(t *testing.T) {
	tests := []struct {
		name          string
		cfg           config.SandboxOutputCompactionConfig
		contextWindow int
		output        string
		expected      bool
	}{
		{
			"disabled",
			config.SandboxOutputCompactionConfig{Enabled: false, ContextWindowPercent: 0.1, ChunkSize: 50000},
			100000,
			strings.Repeat("a", 10000),
			false,
		},
		{
			"zero_context_window",
			config.SandboxOutputCompactionConfig{Enabled: true, ContextWindowPercent: 0.1, ChunkSize: 50000},
			0,
			"some output",
			false,
		},
		{
			"below_threshold",
			config.SandboxOutputCompactionConfig{Enabled: true, ContextWindowPercent: 0.1, ChunkSize: 50000},
			100000,
			"short",
			false,
		},
		{
			"above_threshold",
			config.SandboxOutputCompactionConfig{Enabled: true, ContextWindowPercent: 0.1, ChunkSize: 50000},
			1000,
			strings.Repeat("a", 5000),
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewOutputCompactor(tt.cfg, tt.contextWindow)
			got := c.ShouldCompact(tt.output)
			if got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestOutputCompactor_SplitIntoChunks(t *testing.T) {
	tests := []struct {
		name           string
		chunkSize      int
		output         string
		expectedChunks int
		checkFirst     string
	}{
		{
			"empty",
			100,
			"",
			0,
			"",
		},
		{
			"shorter_than_chunk",
			100,
			"hello world",
			1,
			"hello world",
		},
		{
			"exact_chunk_size",
			5,
			"12345",
			1,
			"12345",
		},
		{
			"two_chunks_no_newline",
			5,
			"1234567890",
			2,
			"12345",
		},
		{
			"breaks_at_newline",
			10,
			"abc\ndef\nghi\njkl",
			2,
			"abc\ndef\n",
		},
		{
			"zero_chunk_size_uses_default",
			0,
			strings.Repeat("x", 60000),
			2,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.SandboxOutputCompactionConfig{
				Enabled:   true,
				ChunkSize: tt.chunkSize,
			}
			c := NewOutputCompactor(cfg, 10000)
			chunks := c.splitIntoChunks(tt.output)
			if len(chunks) != tt.expectedChunks {
				t.Fatalf("expected %d chunks, got %d", tt.expectedChunks, len(chunks))
			}
			if tt.checkFirst != "" && len(chunks) > 0 && chunks[0] != tt.checkFirst {
				t.Fatalf("expected first chunk %q, got %q", tt.checkFirst, chunks[0])
			}
		})
	}
}

func TestOutputCompactor_ChunksToKeep(t *testing.T) {
	tests := []struct {
		totalChunks int
		expected    int
	}{
		{1, 1},
		{5, 1},
		{10, 1},
		{11, 2},
		{50, 2},
		{51, 3},
		{100, 3},
	}

	cfg := config.SandboxOutputCompactionConfig{Enabled: true}
	c := NewOutputCompactor(cfg, 10000)

	for _, tt := range tests {
		got := c.chunksToKeep(tt.totalChunks)
		if got != tt.expected {
			t.Fatalf("chunksToKeep(%d): expected %d, got %d", tt.totalChunks, tt.expected, got)
		}
	}
}

func TestOutputCompactor_GroupChunksForSummarization(t *testing.T) {
	tests := []struct {
		name           string
		chunkSize      int
		chunks         []string
		expectedGroups int
	}{
		{
			"single_chunk",
			100,
			[]string{"abc"},
			1,
		},
		{
			"multiple_small",
			100,
			[]string{"a", "b", "c"},
			1,
		},
		{
			"chunks_exceeding_group",
			10,
			[]string{strings.Repeat("x", 15), strings.Repeat("y", 15)},
			2,
		},
		{
			"empty",
			100,
			[]string{},
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.SandboxOutputCompactionConfig{
				Enabled:   true,
				ChunkSize: tt.chunkSize,
			}
			c := NewOutputCompactor(cfg, 10000)
			groups := c.groupChunksForSummarization(tt.chunks)
			if len(groups) != tt.expectedGroups {
				t.Fatalf("expected %d groups, got %d", tt.expectedGroups, len(groups))
			}
		})
	}
}

func TestOutputCompactor_TruncateGroup(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{Enabled: true}
	c := NewOutputCompactor(cfg, 10000)

	t.Run("within_limit", func(t *testing.T) {
		result := c.truncateGroup([]string{"short"}, 1000)
		if result != "short" {
			t.Fatalf("expected %q, got %q", "short", result)
		}
	})

	t.Run("exact_limit", func(t *testing.T) {
		result := c.truncateGroup([]string{"12345"}, 5)
		if result != "12345" {
			t.Fatalf("expected %q, got %q", "12345", result)
		}
	})

	t.Run("over_limit_no_newline", func(t *testing.T) {
		result := c.truncateGroup([]string{"1234567890"}, 5)
		if !strings.Contains(result, "[... output truncated") {
			t.Fatalf("expected truncation indicator, got %q", result)
		}
	})

	t.Run("over_limit_newline_past_midpoint", func(t *testing.T) {
		result := c.truncateGroup([]string{"abcde\nfghij"}, 8)
		if !strings.HasPrefix(result, "abcde") {
			t.Fatalf("expected break at newline, got %q", result)
		}
		if !strings.Contains(result, "[... output truncated") {
			t.Fatalf("expected truncation indicator, got %q", result)
		}
	})

	t.Run("multiple_groups_combined", func(t *testing.T) {
		result := c.truncateGroup([]string{"aaa", "bbb"}, 5)
		if !strings.Contains(result, "[... output truncated") {
			t.Fatalf("expected truncation indicator, got %q", result)
		}
	})
}

func TestOutputCompactor_Compact_Disabled(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{Enabled: false}
	c := NewOutputCompactor(cfg, 10000)

	result, err := c.Compact(context.Background(), "some output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WasCompacted {
		t.Fatal("expected WasCompacted=false")
	}
	if result.Output != "some output" {
		t.Fatalf("expected original output, got %q", result.Output)
	}
	if result.OriginalSize != len("some output") {
		t.Fatalf("expected OriginalSize=%d, got %d", len("some output"), result.OriginalSize)
	}
}

func TestOutputCompactor_Compact_NoSummarizeClient(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{Enabled: true, ChunkSize: 100}
	c := NewOutputCompactor(cfg, 10000)

	result, err := c.Compact(context.Background(), "output data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WasCompacted {
		t.Fatal("expected WasCompacted=false without summarize client")
	}
	if result.Output != "output data" {
		t.Fatalf("expected original output, got %q", result.Output)
	}
}

func TestOutputCompactor_Compact_EmptyOutput(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{Enabled: true, ChunkSize: 100}
	c := NewOutputCompactor(cfg, 10000)
	c.SetSummarizeClient(&MockSummarizeClient{response: "summary"})

	result, err := c.Compact(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WasCompacted {
		t.Fatal("expected WasCompacted=false for empty output")
	}
	if result.Output != "" {
		t.Fatalf("expected empty output, got %q", result.Output)
	}
}

func TestOutputCompactor_Compact_WithSummarization(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{
		Enabled:              true,
		ChunkSize:            10,
		ContextWindowPercent: 0.1,
	}
	c := NewOutputCompactor(cfg, 10000)
	c.SetSummarizeClient(&MockSummarizeClient{response: "[summarized]"})

	// Create output large enough to produce multiple chunks
	output := strings.Repeat("abcdefghij", 5) // 50 chars -> 5 chunks of 10

	result, err := c.Compact(context.Background(), output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.WasCompacted {
		t.Fatal("expected WasCompacted=true")
	}
	if !strings.Contains(result.Output, "=== COMPACTED OUTPUT ===") {
		t.Fatalf("expected compaction marker, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "[summarized]") {
		t.Fatalf("expected summary content, got %q", result.Output)
	}
	if result.OriginalSize != len(output) {
		t.Fatalf("expected OriginalSize=%d, got %d", len(output), result.OriginalSize)
	}
	if result.ChunksKept < 1 {
		t.Fatalf("expected at least 1 chunk kept, got %d", result.ChunksKept)
	}
}
