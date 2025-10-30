package provider

import (
	"os"
	"strings"
)

// providerEnvVars maps canonical provider names to the environment variables
// that can supply their API keys. Multiple variables allow backwards-compatible
// aliases (e.g., GEMINI_API_KEY and GOOGLE_API_KEY).
var providerEnvVars = map[string][]string{
	"openai":            {"OPENAI_API_KEY"},
	"anthropic":         {"ANTHROPIC_API_KEY"},
	"google":            {"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_API_KEY"},
	"openrouter":        {"OPENROUTER_API_KEY"},
	"mistral":           {"MISTRAL_API_KEY"},
	"cerebras":          {"CEREBRAS_API_KEY"},
	"groq":              {"GROQ_API_KEY"},
	"exa":               {"EXA_API_KEY"},
	"perplexity":        {"PERPLEXITY_API_KEY"},
	"openai-compatible": {"OPENAI_COMPATIBLE_API_KEY", "OPENAI_API_KEY"},
}

// canonicalProviderName normalizes provider aliases so they share the same
// environment-variable mapping.
func canonicalProviderName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "google", "googleai", "gemini":
		return "google"
	case "mistral", "mistralai":
		return "mistral"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

// resolveAPIKey returns the API key to use for a provider. If an explicit key
// is provided it takes precedence, otherwise the function falls back to known
// environment variables. Returned value is trimmed; empty string signals that
// no key is available.
func resolveAPIKey(providerName, explicit string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}

	canonical := canonicalProviderName(providerName)
	envVars := providerEnvVars[canonical]
	for _, envVar := range envVars {
		if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
			return value
		}
	}
	return ""
}

// ResolveAPIKey exposes the environment-variable lookup for external packages
// (e.g., CLI) that need to fetch credentials without depending on the manager.
func ResolveAPIKey(providerName string) string {
	return resolveAPIKey(providerName, "")
}

// EnvVarHints returns the known environment variables for a provider. This is
// useful for displaying contextual help in the UI.
func EnvVarHints(providerName string) []string {
	canonical := canonicalProviderName(providerName)
	hints := providerEnvVars[canonical]
	// Return a copy to avoid accidental external modification.
	out := make([]string, len(hints))
	copy(out, hints)
	return out
}
