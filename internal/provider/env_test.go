package provider

import "testing"

func TestResolveAPIKeyPrefersExplicit(t *testing.T) {
	const (
		explicit = "explicit-key"
		envKey   = "env-key"
	)

	t.Setenv("OPENAI_API_KEY", envKey)
	if got := resolveAPIKey("openai", explicit); got != explicit {
		t.Fatalf("expected explicit key %q, got %q", explicit, got)
	}
}

func TestResolveAPIKeyFallsBackToEnv(t *testing.T) {
	const value = "env-key"
	t.Setenv("MISTRAL_API_KEY", value)

	if got := resolveAPIKey("mistral", ""); got != value {
		t.Fatalf("expected env key %q, got %q", value, got)
	}

	// Aliases should resolve to the same env var list.
	t.Setenv("GEMINI_API_KEY", value)
	if got := resolveAPIKey("gemini", ""); got != value {
		t.Fatalf("expected alias gemini to resolve env key %q, got %q", value, got)
	}
}

func TestEnvVarHintsCopiesSlice(t *testing.T) {
	hints := EnvVarHints("openai")
	if len(hints) == 0 {
		t.Fatalf("expected hints for openai")
	}
	hints[0] = "mutated"

	again := EnvVarHints("openai")
	if again[0] == "mutated" {
		t.Fatalf("expected copy of hints, but slice was modified in place")
	}
}
