package orchestrator

import (
	"context"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/provider"
)

func TestSystemPromptCaching(t *testing.T) {
	// Create a mock filesystem
	mockFS := fs.NewMockFS()

	// Create minimal config
	cfg := &config.Config{
		WorkingDir:  "/test",
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	// Create provider manager
	providerMgr, err := provider.NewManager("", "")
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}

	// Create orchestrator with mock filesystem
	orch, err := NewOrchestratorWithFS(cfg, providerMgr, false, mockFS)
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}
	defer orch.Stop()

	ctx := context.Background()
	modelID := "test-model"

	// First call should build the system prompt
	prompt1, err := orch.getOrBuildSystemPrompt(ctx, modelID)
	if err != nil {
		t.Fatalf("Failed to build system prompt: %v", err)
	}

	if prompt1 == "" {
		t.Fatal("System prompt should not be empty")
	}

	// Second call should return the cached version
	prompt2, err := orch.getOrBuildSystemPrompt(ctx, modelID)
	if err != nil {
		t.Fatalf("Failed to get cached system prompt: %v", err)
	}

	if prompt1 != prompt2 {
		t.Error("Cached system prompt should be identical to the first one")
	}

	// Verify the prompt is actually cached
	if orch.cachedSystemPrompt == "" {
		t.Error("System prompt should be cached in orchestrator")
	}

	// Clear session should reset the cache
	err = orch.ClearSession()
	if err != nil {
		t.Fatalf("Failed to clear session: %v", err)
	}

	if orch.cachedSystemPrompt != "" {
		t.Error("Cached system prompt should be cleared after ClearSession")
	}

	// Next call should rebuild the system prompt
	prompt3, err := orch.getOrBuildSystemPrompt(ctx, modelID)
	if err != nil {
		t.Fatalf("Failed to rebuild system prompt after clear: %v", err)
	}

	if prompt3 == "" {
		t.Fatal("Rebuilt system prompt should not be empty")
	}

	// The content should be the same since filesystem hasn't changed
	if prompt1 != prompt3 {
		t.Error("Rebuilt system prompt should have the same content")
	}
}
