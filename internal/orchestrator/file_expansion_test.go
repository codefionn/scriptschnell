package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/provider"
)

// TestExpandFileReferences_Unit tests the expandFileReferences method directly
func TestExpandFileReferences_Unit(t *testing.T) {
	ctx := context.Background()

	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("failed to create provider manager: %v", err)
	}

	cfg := &config.Config{
		WorkingDir:      ".",
		CacheTTL:        1,
		MaxCacheEntries: 10,
		Temperature:     0.7,
		MaxTokens:       512, // Small context window for testing
	}

	t.Run("no file references", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, mockFS)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		defer orch.Close()

		prompt := "hello world"
		result := orch.expandFileReferences(ctx, prompt)

		if result != prompt {
			t.Fatalf("expected unchanged prompt, got %q", result)
		}
	})

	t.Run("expand small file", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, mockFS)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		defer orch.Close()

		const filePath = "small.go"
		const fileContent = "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"
		if err := mockFS.WriteFile(ctx, filePath, []byte(fileContent)); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
		defer func() {
			_ = mockFS.Delete(ctx, filePath)
		}()

		prompt := "please review @small.go"
		result := orch.expandFileReferences(ctx, prompt)

		if !strings.Contains(result, fileContent) {
			t.Fatalf("expected file content to be included, got %q", result)
		}
		if strings.Contains(result, "@small.go\n") && !strings.Contains(result, "---") {
			t.Fatalf("expected @small.go to be replaced with content")
		}
	})

	t.Run("expand multiple files", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, mockFS)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		defer orch.Close()

		const file1 = "file1.go"
		const content1 = "package main"
		if err := mockFS.WriteFile(ctx, file1, []byte(content1)); err != nil {
			t.Fatalf("failed to write file1: %v", err)
		}

		const file2 = "file2.go"
		const content2 = "func main() {}"
		if err := mockFS.WriteFile(ctx, file2, []byte(content2)); err != nil {
			t.Fatalf("failed to write file2: %v", err)
		}

		defer func() {
			_ = mockFS.Delete(ctx, file1)
			_ = mockFS.Delete(ctx, file2)
		}()

		prompt := "review @file1.go and @file2.go"
		result := orch.expandFileReferences(ctx, prompt)

		if !strings.Contains(result, content1) {
			t.Fatalf("expected file1 content to be included")
		}
		if !strings.Contains(result, content2) {
			t.Fatalf("expected file2 content to be included")
		}
	})

	t.Run("file not found - keep reference", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, mockFS)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		defer orch.Close()

		prompt := "please review @nonexistent.go"
		result := orch.expandFileReferences(ctx, prompt)

		if !strings.Contains(result, "@nonexistent.go") {
			t.Fatalf("expected @nonexistent.go to be kept as-is")
		}
		if strings.Contains(result, "---") {
			t.Fatalf("expected no content markers for non-existent file")
		}
	})

	t.Run("duplicate file reference - expand only once", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, mockFS)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		defer orch.Close()

		const filePath = "dup.go"
		const fileContent = "package main"
		if err := mockFS.WriteFile(ctx, filePath, []byte(fileContent)); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
		defer func() {
			_ = mockFS.Delete(ctx, filePath)
		}()

		prompt := "check @dup.go and then @dup.go again"
		result := orch.expandFileReferences(ctx, prompt)

		contentCount := strings.Count(result, fileContent)
		if contentCount != 1 {
			t.Fatalf("expected file content to appear exactly once, appeared %d times", contentCount)
		}
	})

	t.Run("file with path", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, mockFS)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		defer orch.Close()

		const filePath = "internal/helper/utils.go"
		const fileContent = "package utils"
		if err := mockFS.WriteFile(ctx, filePath, []byte(fileContent)); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
		defer func() {
			_ = mockFS.Delete(ctx, filePath)
		}()

		prompt := "review @internal/helper/utils.go"
		result := orch.expandFileReferences(ctx, prompt)

		if !strings.Contains(result, fileContent) {
			t.Fatalf("expected file content to be included")
		}
	})

	t.Run("large file - keep reference", func(t *testing.T) {
		mockFS := fs.NewMockFS()
		orch, err := NewOrchestratorWithFS(cfg, providerMgr, true, mockFS)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		defer orch.Close()

		const filePath = "large.go"
		// Create content larger than 10% of default context window (8192 tokens -> ~3277 bytes)
		// Use a file size of ~4000 bytes to exceed the threshold
		largeContent := strings.Repeat("This is a large file that exceeds the threshold. ", 100)
		if err := mockFS.WriteFile(ctx, filePath, []byte(largeContent)); err != nil {
			t.Fatalf("failed to write large file: %v", err)
		}
		defer func() {
			_ = mockFS.Delete(ctx, filePath)
		}()

		prompt := "review @large.go"
		result := orch.expandFileReferences(ctx, prompt)

		if strings.Contains(result, largeContent) {
			t.Fatalf("expected large file content to NOT be included")
		}
		if !strings.Contains(result, "@large.go") {
			t.Fatalf("expected @large.go to be kept as-is")
		}
	})
}
