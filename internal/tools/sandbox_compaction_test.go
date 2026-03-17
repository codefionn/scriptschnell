package tools

import (
	"context"
	"os"
	"path/filepath"
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
		{
			"above_hard_limit_128kib",
			config.SandboxOutputCompactionConfig{Enabled: true, ContextWindowPercent: 0.1, ChunkSize: 50000},
			0, // zero context window, but hard limit should still trigger
			strings.Repeat("a", 128*1024),
			true,
		},
		{
			"below_hard_limit_128kib",
			config.SandboxOutputCompactionConfig{Enabled: true, ContextWindowPercent: 0.1, ChunkSize: 50000},
			0, // zero context window and below hard limit
			strings.Repeat("a", 128*1024-1),
			false,
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

func TestOutputCompactor_Compact_WritesToFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.SandboxOutputCompactionConfig{
		Enabled:              true,
		ChunkSize:            10,
		ContextWindowPercent: 0.1,
	}
	c := NewOutputCompactor(cfg, 10000)
	c.SetTempDir(tmpDir)

	// Create output large enough to trigger compaction
	output := strings.Repeat("line of output\n", 100)

	result, err := c.Compact(context.Background(), output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.WasCompacted {
		t.Fatal("expected WasCompacted=true")
	}
	if !strings.Contains(result.Output, "OUTPUT TOO LARGE FOR CONTEXT WINDOW") {
		t.Fatalf("expected file-based instructions, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "read_file") {
		t.Fatalf("expected read_file instructions, got %q", result.Output)
	}
	if !strings.Contains(result.Output, tmpDir) {
		t.Fatalf("expected temp dir path in output, got %q", result.Output)
	}

	// Verify the file was actually written
	matches, err := filepath.Glob(filepath.Join(tmpDir, "sandbox-output-*.txt"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 output file, got %d", len(matches))
	}

	// Verify file contents match original output
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(data) != output {
		t.Fatalf("output file contents don't match: got %d bytes, expected %d bytes", len(data), len(output))
	}
}

func TestOutputCompactor_Compact_FallbackTruncate(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{
		Enabled:              true,
		ChunkSize:            10,
		ContextWindowPercent: 0.1,
	}
	c := NewOutputCompactor(cfg, 10000)
	// Use a non-existent directory to trigger fallback
	c.SetTempDir("/nonexistent/path/that/should/not/exist")

	output := strings.Repeat("x", 10000)

	result, err := c.Compact(context.Background(), output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to truncation
	if !result.WasCompacted {
		t.Fatal("expected WasCompacted=true")
	}
	if result.CompactedSize >= result.OriginalSize {
		t.Fatalf("expected compacted size < original, got %d >= %d", result.CompactedSize, result.OriginalSize)
	}
	if !strings.Contains(result.Output, "bytes truncated") {
		t.Fatalf("expected truncation indicator, got %q", result.Output[:200])
	}
}

func TestOutputCompactor_SetTempDir(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{Enabled: true}
	c := NewOutputCompactor(cfg, 10000)

	tmpDir := t.TempDir()
	c.SetTempDir(tmpDir)

	if c.tempDir != tmpDir {
		t.Fatalf("expected tempDir=%s, got %s", tmpDir, c.tempDir)
	}
}

func TestOutputCompactor_FallbackTruncate_ShortOutput(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{Enabled: true}
	c := NewOutputCompactor(cfg, 10000)

	// Output shorter than 2*maxKeep (4000) should not be truncated
	result := c.fallbackTruncate("short output", 12)
	if result.WasCompacted {
		t.Fatal("expected WasCompacted=false for short output")
	}
	if result.Output != "short output" {
		t.Fatalf("expected original output, got %q", result.Output)
	}
}

func TestOutputCompactor_FallbackTruncate_LongOutput(t *testing.T) {
	cfg := config.SandboxOutputCompactionConfig{Enabled: true}
	c := NewOutputCompactor(cfg, 10000)

	output := strings.Repeat("abcdefghij\n", 1000) // ~11000 chars
	result := c.fallbackTruncate(output, len(output))

	if !result.WasCompacted {
		t.Fatal("expected WasCompacted=true")
	}
	if !strings.Contains(result.Output, "bytes truncated") {
		t.Fatalf("expected truncation indicator, got %q", result.Output[:200])
	}
	if result.CompactedSize >= result.OriginalSize {
		t.Fatalf("expected compacted < original")
	}
}
