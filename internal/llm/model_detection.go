package llm

import (
	"strings"
)

// ModelFamily represents a family of models from a specific provider
type ModelFamily int

const (
	FamilyUnknown ModelFamily = iota
	// OpenAI families
	FamilyGPT5
	FamilyO3
	FamilyGPT4o
	FamilyGPT4
	FamilyGPT35
	// Anthropic families
	FamilyClaude45
	FamilyClaude41
	FamilyClaude4
	FamilyClaude35
	FamilyClaude3
	FamilyClaude2
	// Google families
	FamilyGemini2
	FamilyGemini15
	FamilyGemini1
	// Meta families
	FamilyLlama33
	FamilyLlama32
	FamilyLlama31
	FamilyLlama3
	FamilyLlama2
	// Mistral families
	FamilyMistralLarge
	FamilyMistralMedium
	FamilyMistralSmall
	FamilyCodestral
	FamilyPixtral
	FamilyMixtral
	FamilyDevstral
	// Other families
	FamilyQwen
	FamilyGemma
	FamilyPhi
	FamilyDeepSeek
	FamilyCommand
	FamilyZaiGLM
	FamilyKimi
	FamilyMiniMax
)

// Model identifier constants for pattern matching
const (
	// OpenAI model identifiers
	ModelIDGPT5          = "gpt-5"
	ModelIDChatGPT5      = "chatgpt-5"
	ModelIDO3Mini        = "o3-mini"
	ModelIDO3            = "o3"
	ModelIDGPT4o         = "gpt-4o"
	ModelIDChatGPT4o     = "chatgpt-4o"
	ModelIDGPT4          = "gpt-4"
	ModelIDChatGPT4      = "chatgpt-4"
	ModelIDGPT35         = "gpt-3.5"
	ModelIDGPT35Alt      = "gpt-35"
	ModelIDMini          = "mini"
	ModelIDRealtime      = "realtime"
	ModelIDTurbo         = "turbo"
	ModelIDVision        = "vision"
	ModelID0125          = "0125"
	ModelID1106          = "1106"
	ModelID0301          = "0301"
	ModelID0314          = "0314"
	ModelIDTurbo20240409 = "turbo-2024-04-09"
	ModelIDPreview       = "preview"
	ModelIDAudioPreview  = "audio-preview"

	// Anthropic model identifiers
	ModelIDClaude45       = "claude-4-5"
	ModelIDClaude45Alt    = "claude-4.5"
	ModelIDClaude45Sonnet = "claude-sonnet-4.5"
	ModelIDClaude45Opus   = "claude-opus-4.5"
	ModelIDClaude41       = "claude-4-1"
	ModelIDClaude41Alt    = "claude-4.1"
	ModelIDClaude4        = "claude-4"
	ModelIDClaude35       = "claude-3-5"
	ModelIDClaude35Alt    = "claude-3.5"
	ModelIDClaude3        = "claude-3"
	ModelIDClaude2        = "claude-2"
	ModelIDSonnet         = "sonnet"
	ModelIDOpus           = "opus"
	ModelIDHaiku          = "haiku"

	// Google model identifiers
	ModelIDGemini2     = "gemini-2"
	ModelIDGemini2Alt  = "gemini2"
	ModelIDGemini15    = "gemini-1.5"
	ModelIDGemini15Alt = "gemini-15"
	ModelIDGemini1     = "gemini-1"
	ModelIDGemini1Alt  = "gemini1"
	ModelIDFlash       = "flash"

	// Meta Llama model identifiers
	ModelIDLlama33    = "llama-3.3"
	ModelIDLlama33Alt = "llama3.3"
	ModelIDLlama32    = "llama-3.2"
	ModelIDLlama32Alt = "llama3.2"
	ModelIDLlama31    = "llama-3.1"
	ModelIDLlama31Alt = "llama3.1"
	ModelIDLlama3     = "llama-3"
	ModelIDLlama3Alt  = "llama3"
	ModelIDLlama2     = "llama-2"
	ModelIDLlama2Alt  = "llama2"
	ModelID70B        = "70b"

	// Mistral model identifiers
	ModelIDCodestral     = "codestral"
	ModelIDPixtral       = "pixtral"
	ModelIDMixtral       = "mixtral"
	ModelIDMistralLarge  = "mistral-large"
	ModelIDMistralMedium = "mistral-medium"
	ModelIDMistralSmall  = "mistral-small"
	ModelIDDevstral      = "devstral"
	ModelIDOpenMistral   = "open-mistral"
	ModelIDMedium        = "medium"
	ModelIDSmall         = "small"

	// Other model identifiers
	ModelIDQwen     = "qwen"
	ModelIDGemma    = "gemma"
	ModelIDPhi      = "phi"
	ModelIDDeepSeek = "deepseek"
	ModelIDCommand  = "command"
	ModelIDZaiGLM   = "zai-glm"
	ModelIDKimi     = "kimi"
	ModelIDK2       = "k2"
	ModelIDMoonshot = "moonshot"
	ModelIDMiniMax  = "minimax"

	// Size indicators
	ModelID128K   = "128k"
	ModelID100K   = "100k"
	ModelID64K    = "64k"
	ModelID32K    = "32k"
	ModelID16K    = "16k"
	ModelID8K     = "8k"
	ModelID4K     = "4k"
	ModelID200K   = "200k"
	ModelID131072 = "131072"
	ModelID32768  = "32768"
	ModelID16384  = "16384"
	ModelID8192   = "8192"
)

// PrefixPattern represents a pattern with a prefix and associated value
type PrefixPattern struct {
	Prefix string
	Value  int
}

// normalizeModelID normalizes a model ID for consistent matching
func normalizeModelID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// DetectModelFamily detects the model family from a model ID
func DetectModelFamily(modelID string) ModelFamily {
	id := normalizeModelID(modelID)

	// OpenAI families (most specific first)
	if containsAny(id, ModelIDGPT5, ModelIDChatGPT5) {
		return FamilyGPT5
	}
	if containsAny(id, ModelIDO3Mini, ModelIDO3) {
		return FamilyO3
	}
	if containsAny(id, ModelIDGPT4o, ModelIDChatGPT4o) {
		return FamilyGPT4o
	}
	if containsAny(id, ModelIDGPT4, ModelIDChatGPT4) {
		return FamilyGPT4
	}
	if containsAny(id, ModelIDGPT35, ModelIDGPT35Alt) {
		return FamilyGPT35
	}

	// Anthropic families
	if containsAny(id, ModelIDClaude45, ModelIDClaude45Alt, ModelIDClaude45Sonnet, ModelIDClaude45Opus) || (strings.Contains(id, "claude") && containsAny(id, "4-5", "4.5")) {
		return FamilyClaude45
	}
	if containsAny(id, ModelIDClaude41, ModelIDClaude41Alt) || (strings.Contains(id, "claude") && containsAny(id, "4-1", "4.1")) {
		return FamilyClaude41
	}
	if strings.Contains(id, ModelIDClaude4) {
		return FamilyClaude4
	}
	if containsAny(id, ModelIDClaude35, ModelIDClaude35Alt) {
		return FamilyClaude35
	}
	if strings.Contains(id, ModelIDClaude3) {
		return FamilyClaude3
	}
	if strings.Contains(id, ModelIDClaude2) {
		return FamilyClaude2
	}

	// Google families
	if containsAny(id, ModelIDGemini2, ModelIDGemini2Alt) {
		return FamilyGemini2
	}
	if containsAny(id, ModelIDGemini15, ModelIDGemini15Alt) {
		return FamilyGemini15
	}
	if containsAny(id, ModelIDGemini1, ModelIDGemini1Alt) {
		return FamilyGemini1
	}

	// Meta Llama families
	if containsAny(id, ModelIDLlama33, ModelIDLlama33Alt) {
		return FamilyLlama33
	}
	if containsAny(id, ModelIDLlama32, ModelIDLlama32Alt) {
		return FamilyLlama32
	}
	if containsAny(id, ModelIDLlama31, ModelIDLlama31Alt) {
		return FamilyLlama31
	}
	if containsAny(id, ModelIDLlama3, ModelIDLlama3Alt) {
		return FamilyLlama3
	}
	if containsAny(id, ModelIDLlama2, ModelIDLlama2Alt) {
		return FamilyLlama2
	}

	// Mistral families
	if strings.Contains(id, ModelIDDevstral) {
		return FamilyDevstral
	}
	if strings.Contains(id, ModelIDCodestral) {
		return FamilyCodestral
	}
	if strings.Contains(id, ModelIDPixtral) {
		return FamilyPixtral
	}
	if strings.Contains(id, ModelIDMixtral) {
		return FamilyMixtral
	}
	if strings.Contains(id, ModelIDMistralLarge) {
		return FamilyMistralLarge
	}
	if containsAny(id, ModelIDMistralMedium, ModelIDMedium) {
		return FamilyMistralMedium
	}
	if containsAny(id, ModelIDMistralSmall, ModelIDSmall, ModelIDOpenMistral) {
		return FamilyMistralSmall
	}

	// Other families
	if strings.Contains(id, ModelIDQwen) {
		return FamilyQwen
	}
	if strings.Contains(id, ModelIDGemma) {
		return FamilyGemma
	}
	if strings.Contains(id, ModelIDPhi) {
		return FamilyPhi
	}
	if strings.Contains(id, ModelIDDeepSeek) {
		return FamilyDeepSeek
	}
	if strings.Contains(id, ModelIDCommand) {
		return FamilyCommand
	}
	if strings.Contains(id, ModelIDZaiGLM) {
		return FamilyZaiGLM
	}
	if strings.Contains(id, ModelIDKimi) || strings.Contains(id, ModelIDK2) || strings.Contains(id, ModelIDMoonshot) {
		return FamilyKimi
	}
	if strings.Contains(id, ModelIDMiniMax) {
		return FamilyMiniMax
	}

	return FamilyUnknown
}

// IsMistralModel checks if the given model ID belongs to the Mistral family
func IsMistralModel(modelID string) bool {
	family := DetectModelFamily(modelID)
	switch family {
	case FamilyMistralLarge, FamilyMistralMedium, FamilyMistralSmall, FamilyCodestral, FamilyPixtral, FamilyDevstral:
		return true
	default:
		return false
	}
}

// containsAny checks if the string contains any of the given substrings
func containsAny(s string, substrings ...string) bool {
	for _, substr := range substrings {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// MatchExactOrPrefix tries exact match first, then prefix patterns (longest first)
func MatchExactOrPrefix(modelID string, exactMatches map[string]int, prefixPatterns []PrefixPattern) (int, bool) {
	id := normalizeModelID(modelID)

	// Try exact match first
	if value, ok := exactMatches[id]; ok {
		return value, true
	}

	// Try prefix patterns (should be ordered from longest/most specific to shortest)
	for _, pattern := range prefixPatterns {
		if strings.HasPrefix(id, normalizeModelID(pattern.Prefix)) {
			return pattern.Value, true
		}
	}

	return 0, false
}

// ParseExplicitSize extracts explicit size indicators from model ID (128k, 32768, etc.)
func ParseExplicitSize(modelID string) (int, bool) {
	id := normalizeModelID(modelID)

	// Check for "k" suffix formats (128k, 64k, etc.)
	sizes := []struct {
		pattern string
		value   int
	}{
		{"200k", 204800},
		{"128k", 131072},
		{"100k", 102400},
		{"64k", 65536},
		{"32k", 32768},
		{"16k", 16384},
		{"8k", 8192},
		{"4k", 4096},
	}

	for _, size := range sizes {
		if strings.Contains(id, size.pattern) {
			return size.value, true
		}
	}

	// Check for exact number formats (32768, 8192, etc.)
	exactSizes := []struct {
		pattern string
		value   int
	}{
		{"131072", 131072},
		{"32768", 32768},
		{"16384", 16384},
		{"8192", 8192},
	}

	for _, size := range exactSizes {
		if strings.Contains(id, size.pattern) {
			return size.value, true
		}
	}

	return 0, false
}

// DetectContextWindow detects context window size for a model
func DetectContextWindow(modelID string, family ModelFamily) int {
	// First try to parse explicit size indicators
	if size, ok := ParseExplicitSize(modelID); ok {
		return size
	}

	// Fall back to family-based detection
	switch family {
	// OpenAI families
	case FamilyGPT5:
		return 128000
	case FamilyO3:
		return 200000
	case FamilyGPT4o:
		return 128000
	case FamilyGPT4:
		id := normalizeModelID(modelID)
		if containsAny(id, "turbo-2024-04-09", "0125", "1106") {
			return 128000
		}
		if containsAny(id, "32k", "vision") {
			return 32768
		}
		return 8192
	case FamilyGPT35:
		if strings.Contains(normalizeModelID(modelID), "16k") {
			return 16384
		}
		return 4096

	// Anthropic families
	case FamilyClaude45:
		if strings.Contains(normalizeModelID(modelID), "sonnet") {
			return 1000000
		}
		return 200000
	case FamilyClaude41:
		return 200000
	case FamilyClaude4:
		return 200000
	case FamilyClaude35:
		return 200000
	case FamilyClaude3:
		return 200000
	case FamilyClaude2:
		return 100000

	// Google families
	case FamilyGemini2:
		return 1000000
	case FamilyGemini15:
		if strings.Contains(normalizeModelID(modelID), "flash") {
			return 1000000
		}
		return 2000000
	case FamilyGemini1:
		return 32768

	// Meta Llama families
	case FamilyLlama33, FamilyLlama32, FamilyLlama31:
		return 131072
	case FamilyLlama3:
		return 8192
	case FamilyLlama2:
		if strings.Contains(normalizeModelID(modelID), "70b") {
			return 4096
		}
		return 4096

	// Mistral families
	case FamilyMistralLarge, FamilyCodestral:
		return 128000
	case FamilyMistralMedium:
		return 32000
	case FamilyMistralSmall:
		return 32000
	case FamilyPixtral:
		return 128000
	case FamilyMixtral:
		return 32000
	case FamilyDevstral:
		return 128000

	// Other families
	case FamilyQwen:
		if strings.Contains(normalizeModelID(modelID), "70b") {
			return 32768
		}
		return 8192
	case FamilyGemma:
		return 8192
	case FamilyPhi:
		return 128000
	case FamilyDeepSeek:
		return 64000
	case FamilyCommand:
		return 128000
	case FamilyZaiGLM:
		return 131072
	case FamilyKimi:
		return 200000
	case FamilyMiniMax:
		return 245760
	}

	// Default fallback
	return 8192
}

// DetectMaxOutputTokens detects maximum output tokens for a model
func DetectMaxOutputTokens(modelID string, family ModelFamily, contextWindow int) int {
	id := normalizeModelID(modelID)

	// Family-based detection
	switch family {
	// OpenAI families
	case FamilyGPT5:
		return 128000
	case FamilyO3:
		return 100000
	case FamilyGPT4o:
		if containsAny(id, "mini", "realtime") {
			return 16384
		}
		return 16384
	case FamilyGPT4:
		if containsAny(id, "turbo-2024-04-09", "0125", "1106") {
			return 4096
		}
		return 8192
	case FamilyGPT35:
		return 4096

	// Anthropic families
	case FamilyClaude45:
		return 64000
	case FamilyClaude41:
		return 32000
	case FamilyClaude4:
		return 8192
	case FamilyClaude35:
		return 8192
	case FamilyClaude3:
		return 4096
	case FamilyClaude2:
		return 4096

	// Google families
	case FamilyGemini2, FamilyGemini15:
		return 8192
	case FamilyGemini1:
		return 2048

	// Meta Llama families
	case FamilyLlama33, FamilyLlama32, FamilyLlama31, FamilyLlama3:
		return 8192
	case FamilyLlama2:
		return 4096

	// Mistral families
	case FamilyMistralLarge, FamilyMistralMedium, FamilyMistralSmall:
		return 8192
	case FamilyCodestral:
		return 8192
	case FamilyPixtral:
		return 8192
	case FamilyMixtral:
		return 8192
	case FamilyDevstral:
		return 32000

	// Other families
	case FamilyQwen:
		return 8192
	case FamilyGemma:
		return 8192
	case FamilyPhi:
		return 4096
	case FamilyDeepSeek:
		return 8192
	case FamilyCommand:
		return 4096
	case FamilyZaiGLM:
		return 40000
	case FamilyKimi:
		return 8192
	case FamilyMiniMax:
		return 8192
	}

	// Conservative fallback based on context window
	if contextWindow >= 100000 {
		return 8192
	} else if contextWindow >= 32000 {
		return 4096
	}
	return 2048
}

// SupportsToolCalling detects if a model supports tool/function calling
func SupportsToolCalling(modelID string, family ModelFamily) bool {
	id := normalizeModelID(modelID)

	// Exclusions for models that don't support tools
	exclusions := []string{
		"gpt-3.5-turbo-0301",
		"gpt-4-0314",
		"gpt-4-32k-0314",
		"gpt-4-vision",
		"audio-preview",
		"realtime",
	}

	for _, excl := range exclusions {
		if strings.Contains(id, excl) {
			return false
		}
	}

	// Family-based support
	switch family {
	case FamilyGPT5, FamilyO3, FamilyGPT4o, FamilyGPT4, FamilyGPT35:
		return true
	case FamilyClaude45, FamilyClaude41, FamilyClaude4, FamilyClaude35, FamilyClaude3:
		return true
	case FamilyGemini2, FamilyGemini15, FamilyGemini1:
		return true
	case FamilyMistralLarge, FamilyMistralMedium, FamilyMistralSmall, FamilyDevstral:
		return true
	case FamilyCommand:
		return true
	case FamilyZaiGLM:
		return true
	case FamilyKimi:
		return true
	case FamilyMiniMax:
		return true
	}

	// Default to true for unknown models (API will validate)
	return true
}

// FormatModelDisplayName formats a model ID into a human-readable display name
func FormatModelDisplayName(modelID string, family ModelFamily) string {
	id := strings.TrimSpace(modelID)

	// Family-specific formatting
	switch family {
	case FamilyClaude45:
		if strings.Contains(normalizeModelID(id), "sonnet") {
			return "Claude 4.5 Sonnet"
		}
		if strings.Contains(normalizeModelID(id), "haiku") {
			return "Claude 4.5 Haiku"
		}
		if strings.Contains(normalizeModelID(id), "opus") {
			return "Claude 4.5 Opus"
		}
		return "Claude 4.5"
	case FamilyClaude41:
		if strings.Contains(normalizeModelID(id), "opus") {
			return "Claude 4.1 Opus"
		}
		return "Claude 4.1"
	case FamilyClaude4:
		return "Claude 4"
	case FamilyClaude35:
		if strings.Contains(normalizeModelID(id), "sonnet") {
			return "Claude 3.5 Sonnet"
		}
		if strings.Contains(normalizeModelID(id), "haiku") {
			return "Claude 3.5 Haiku"
		}
		return "Claude 3.5"
	case FamilyClaude3:
		if strings.Contains(normalizeModelID(id), "opus") {
			return "Claude 3 Opus"
		}
		if strings.Contains(normalizeModelID(id), "sonnet") {
			return "Claude 3 Sonnet"
		}
		if strings.Contains(normalizeModelID(id), "haiku") {
			return "Claude 3 Haiku"
		}
		return "Claude 3"
	case FamilyClaude2:
		return "Claude 2"
	case FamilyDevstral:
		if strings.Contains(normalizeModelID(id), "small") {
			return "Devstral Small"
		}
		return "Devstral"
	case FamilyMiniMax:
		// Extract variant from model ID (e.g., "minimax-01" -> "MiniMax 01")
		parts := strings.Split(normalizeModelID(id), "-")
		if len(parts) > 1 {
			return "MiniMax " + strings.ToUpper(parts[1])
		}
		return "MiniMax"
	}

	// Generic formatting: capitalize, replace hyphens/underscores with spaces
	name := id
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, ":", " ")

	// Capitalize words
	words := strings.Fields(name)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// GetModelDescription generates a description for a model
func GetModelDescription(modelID string, family ModelFamily) string {
	id := normalizeModelID(modelID)

	// Family-specific descriptions
	switch family {
	case FamilyGPT5:
		return "Most advanced GPT-5 model with enhanced reasoning"
	case FamilyO3:
		return "Advanced reasoning model optimized for complex tasks"
	case FamilyGPT4o:
		if strings.Contains(id, "mini") {
			return "Affordable and intelligent small model for fast tasks"
		}
		return "High-intelligence flagship model for complex tasks"
	case FamilyGPT4:
		if strings.Contains(id, "turbo") {
			return "GPT-4 Turbo with improved performance"
		}
		return "Large multimodal model for complex tasks"
	case FamilyGPT35:
		return "Fast, inexpensive model for simple tasks"

	case FamilyClaude45:
		if strings.Contains(id, "sonnet") {
			return "Our most intelligent model for complex agents and coding"
		}
		if strings.Contains(id, "haiku") {
			return "Our fastest model with near frontier intelligence"
		}
		if strings.Contains(id, "opus") {
			return "Premium model combining max intelligence with practical performance"
		}
		return "Advanced Claude 4.5 model"
	case FamilyClaude41:
		if strings.Contains(id, "opus") {
			return "Exceptional model for specialized reasoning tasks"
		}
		return "Advanced Claude 4.1 model"
	case FamilyClaude4:
		return "High-performance Claude 4 model"
	case FamilyClaude35:
		if strings.Contains(id, "sonnet") {
			return "Balanced model for speed and intelligence"
		}
		if strings.Contains(id, "haiku") {
			return "Fastest and most compact model"
		}
		return "Advanced Claude 3.5 model"
	case FamilyClaude3:
		if strings.Contains(id, "opus") {
			return "Most capable Claude 3 model"
		}
		if strings.Contains(id, "sonnet") {
			return "Balanced Claude 3 model"
		}
		if strings.Contains(id, "haiku") {
			return "Fastest Claude 3 model"
		}
		return "Claude 3 model"

	case FamilyGemini2:
		return "Next generation Gemini model with multimodal capabilities"
	case FamilyGemini15:
		if strings.Contains(id, "flash") {
			return "Fast and efficient multimodal model"
		}
		return "Powerful multimodal model with extended context"

	case FamilyLlama33, FamilyLlama32, FamilyLlama31:
		if strings.Contains(id, "70b") {
			return "Large open-source language model (70B parameters)"
		}
		return "Open-source language model from Meta"

	case FamilyMistralLarge:
		return "Flagship model for complex tasks"
	case FamilyCodestral:
		return "Specialized for code generation"
	case FamilyPixtral:
		return "Multimodal vision model"
	case FamilyMixtral:
		return "Mixture of experts model"
	case FamilyDevstral:
		return "Agentic model for software engineering tasks"

	case FamilyCommand:
		return "Cohere's command model for text generation"
	case FamilyZaiGLM:
		return "Zhipu AI's GLM model for text generation"
	case FamilyKimi:
		return "Moonshot AI's Kimi model with advanced reasoning capabilities"
	case FamilyMiniMax:
		return "MiniMax AI's multimodal model with long-context support"
	}

	return "Language model"
}
