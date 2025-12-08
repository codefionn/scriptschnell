package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectorDetectsGoProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", []byte("module example.com/test\n"))

	types, err := NewDetector(root).Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	found := false
	for _, pt := range types {
		if pt.ID == "go" {
			found = true
			if pt.Confidence < 0.9 {
				t.Fatalf("expected high confidence for Go project, got %.2f", pt.Confidence)
			}
			break
		}
	}

	if !found {
		t.Fatalf("expected Go project type in detection results: %+v", types)
	}
}

func TestDetectorDetectsMultipleProjectTypes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", []byte(`{"name":"app"}`))
	writeFile(t, root, "Cargo.toml", []byte(`[package]\nname = "example"`))
	writeFile(t, root, filepath.Join("solution", "App.csproj"), []byte(`<Project></Project>`))

	types, err := NewDetector(root).Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	foundIDs := map[string]bool{}
	for _, pt := range types {
		foundIDs[pt.ID] = true
	}

	for _, expect := range []string{"nodejs", "rust", "csharp"} {
		if !foundIDs[expect] {
			t.Fatalf("expected project type %q to be detected, got %v", expect, types)
		}
	}
}

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("failed to create directory for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", rel, err)
	}
}
