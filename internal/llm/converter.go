package llm

import (
	"strings"
	"time"
)

// NativeMessage wraps a provider-specific message with metadata
type NativeMessage struct {
	Native      interface{}
	Provider    string
	ModelFamily string
	Timestamp   time.Time
}

// NativeConverter handles bidirectional message conversion for a provider
type NativeConverter interface {
	// GetProviderName returns the provider identifier (e.g., "anthropic", "openai")
	GetProviderName() string

	// GetModelFamily returns the model family for a given model ID (e.g., "claude-3", "gpt-4")
	GetModelFamily(modelID string) string

	// ConvertToNative converts unified messages to provider-specific format
	// Returns slice of provider-specific message objects
	ConvertToNative(messages []*Message, systemPrompt string, enableCaching bool, cacheTTL string) ([]interface{}, error)

	// ConvertFromNative converts provider-specific messages back to unified format
	ConvertFromNative(native []interface{}) ([]*Message, error)

	// SupportsNativeStorage indicates if this provider supports native message storage
	SupportsNativeStorage() bool
}

// GetConverter returns the appropriate converter for a model ID
func GetConverter(modelID string) NativeConverter {
	modelLower := strings.ToLower(modelID)

	// OpenRouter models (check provider prefix first - has "/" format)
	if strings.Contains(modelLower, "/") {
		return &OpenRouterConverterImpl{}
	}

	// Anthropic/Claude models
	if strings.Contains(modelLower, "claude") {
		return &AnthropicConverterImpl{}
	}

	// OpenAI models
	if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "o1") || strings.Contains(modelLower, "o3") {
		return &OpenAIConverterImpl{}
	}

	// Google/Gemini models
	if strings.Contains(modelLower, "gemini") {
		return &GoogleConverterImpl{}
	}

	// Mistral models
	if strings.Contains(modelLower, "mistral") || strings.Contains(modelLower, "codestral") || strings.Contains(modelLower, "mixtral") {
		return &MistralConverterImpl{}
	}

	// Cerebras models
	if strings.Contains(modelLower, "llama") {
		return &CerebrasConverterImpl{}
	}

	// Default: no native storage support
	return nil
}

// GetProviderAndFamily is a helper function to extract provider and family from model ID
func GetProviderAndFamily(modelID string) (provider, modelFamily string) {
	converter := GetConverter(modelID)
	if converter == nil {
		return "unknown", "unknown"
	}
	return converter.GetProviderName(), converter.GetModelFamily(modelID)
}

// extractModelFamily is a helper function to extract model family from model ID
func extractModelFamily(modelID string) string {
	modelLower := strings.ToLower(modelID)

	// Claude models
	if strings.Contains(modelLower, "claude-3.5") || strings.Contains(modelLower, "claude-3-5") {
		return "claude-3.5"
	}
	if strings.Contains(modelLower, "claude-3") {
		return "claude-3"
	}
	if strings.Contains(modelLower, "claude-2") {
		return "claude-2"
	}

	// GPT models
	if strings.Contains(modelLower, "gpt-4o") {
		return "gpt-4o"
	}
	if strings.Contains(modelLower, "gpt-4-turbo") || strings.Contains(modelLower, "gpt-4-1106") || strings.Contains(modelLower, "gpt-4-0125") {
		return "gpt-4-turbo"
	}
	if strings.Contains(modelLower, "gpt-4") {
		return "gpt-4"
	}
	if strings.Contains(modelLower, "gpt-3.5") || strings.Contains(modelLower, "gpt-35") {
		return "gpt-3.5"
	}
	if strings.Contains(modelLower, "o1-preview") || strings.Contains(modelLower, "o1-mini") {
		return "o1"
	}
	if strings.Contains(modelLower, "o3") {
		return "o3"
	}

	// Gemini models
	if strings.Contains(modelLower, "gemini-2") {
		return "gemini-2"
	}
	if strings.Contains(modelLower, "gemini-1.5") {
		return "gemini-1.5"
	}
	if strings.Contains(modelLower, "gemini-1") {
		return "gemini-1"
	}

	// Mistral models
	if strings.Contains(modelLower, "mistral-large") {
		return "mistral-large"
	}
	if strings.Contains(modelLower, "mistral-medium") {
		return "mistral-medium"
	}
	if strings.Contains(modelLower, "mistral-small") {
		return "mistral-small"
	}
	if strings.Contains(modelLower, "mixtral") {
		return "mixtral"
	}
	if strings.Contains(modelLower, "codestral") {
		return "codestral"
	}

	// Default: use model ID as family
	return modelID
}

// All converters are implemented:
// - AnthropicConverterImpl in anthropic_converter.go
// - OpenAIConverterImpl in openai_converter.go
// - GoogleConverterImpl in google_converter.go
// - MistralConverterImpl, OpenRouterConverterImpl, CerebrasConverterImpl in simple_converters.go
