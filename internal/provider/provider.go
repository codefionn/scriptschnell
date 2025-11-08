package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudflare/ahocorasick"
	"github.com/statcode-ai/statcode-ai/internal/llm"
)

// Provider represents an LLM provider
type Provider struct {
	Name    string   `json:"name"`
	APIKey  string   `json:"api_key"`
	BaseURL string   `json:"base_url,omitempty"` // Optional custom base URL for OpenAI-compatible providers
	Models  []*Model `json:"models"`
}

// Model represents an LLM model
type Model struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Description     string `json:"description,omitempty"`
	ContextWindow   int    `json:"context_window,omitempty"`    // Input context window size
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"` // Maximum output tokens
}

// Config stores provider configuration
type Config struct {
	Providers          map[string]*Provider `json:"providers"`
	OrchestrationModel string               `json:"orchestration_model"`
	SummarizeModel     string               `json:"summarize_model"`
}

// Manager manages LLM providers
type Manager struct {
	config     *Config
	configPath string
	matcher    *ahocorasick.Matcher
	mu         sync.RWMutex
}

// NewManager creates a new provider manager
func NewManager(configPath string) (*Manager, error) {
	m := &Manager{
		configPath: configPath,
		config: &Config{
			Providers: make(map[string]*Provider),
		},
	}

	// Load config if exists
	if err := m.Load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Build search matcher
	m.rebuildMatcher()

	return m, nil
}

// Load loads configuration from disk
func (m *Manager) Load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	m.mu.Lock()
	m.config = &config
	m.mu.Unlock()

	m.rebuildMatcher()
	return nil
}

// Save saves configuration to disk
func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.save()
}

// save is the internal save method that doesn't acquire locks
func (m *Manager) save() error {
	// Ensure directory exists
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.configPath, data, 0600) // Secure permissions for API keys
}

// AddProvider adds or updates a provider
func (m *Manager) AddProvider(name, apiKey string, models []*Model) error {
	return m.AddProviderWithBaseURL(name, apiKey, "", models)
}

// AddProviderWithBaseURL adds or updates a provider with a custom base URL
func (m *Manager) AddProviderWithBaseURL(name, apiKey, baseURL string, models []*Model) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.Providers[name] = &Provider{
		Name:    name,
		APIKey:  apiKey,
		BaseURL: baseURL,
		Models:  models,
	}

	m.rebuildMatcher()
	return m.save()
}

// AddProviderWithAPIListing adds a provider and fetches models from API
func (m *Manager) AddProviderWithAPIListing(ctx context.Context, name, apiKey string) error {
	return m.AddProviderWithAPIListingAndBaseURL(ctx, name, apiKey, "")
}

// AddProviderWithAPIListingAndBaseURL adds a provider with custom base URL and fetches models from API
func (m *Manager) AddProviderWithAPIListingAndBaseURL(ctx context.Context, name, apiKey, baseURL string) error {
	// Create provider instance
	llmProvider, err := m.createLLMProviderWithBaseURL(name, apiKey, baseURL)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Fetch models from API
	modelInfos, err := llmProvider.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	// Convert to internal Model format
	models := make([]*Model, len(modelInfos))
	for i, info := range modelInfos {
		models[i] = &Model{
			ID:              info.ID,
			Name:            info.Name,
			Provider:        info.Provider,
			Description:     info.Description,
			ContextWindow:   info.ContextWindow,
			MaxOutputTokens: info.MaxOutputTokens,
		}
	}

	// Add provider with fetched models
	return m.AddProviderWithBaseURL(name, apiKey, baseURL, models)
}

// RefreshModels refreshes the model list from the API for a given provider
func (m *Manager) RefreshModels(ctx context.Context, providerName string) error {
	m.mu.RLock()
	provider, ok := m.config.Providers[providerName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	// Create provider instance with base URL if present
	llmProvider, err := m.createLLMProviderWithBaseURL(providerName, provider.APIKey, provider.BaseURL)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Fetch models from API
	modelInfos, err := llmProvider.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	// Convert to internal Model format
	models := make([]*Model, len(modelInfos))
	for i, info := range modelInfos {
		models[i] = &Model{
			ID:              info.ID,
			Name:            info.Name,
			Provider:        info.Provider,
			Description:     info.Description,
			ContextWindow:   info.ContextWindow,
			MaxOutputTokens: info.MaxOutputTokens,
		}
	}

	// Update provider
	m.mu.Lock()
	provider.Models = models
	m.rebuildMatcher()
	err = m.save()
	m.mu.Unlock()

	return err
}

// RefreshAllModels refreshes models for all configured providers in the background
func (m *Manager) RefreshAllModels(ctx context.Context) {
	providers := m.ListProviders()
	if len(providers) == 0 {
		return
	}

	// Refresh in background goroutine
	go func() {
		var wg sync.WaitGroup
		for _, p := range providers {
			// Skip if context is cancelled
			if ctx.Err() != nil {
				return
			}

			// Refresh each provider in parallel
			wg.Add(1)
			go func(providerName string) {
				defer wg.Done()
				// Refresh models (errors are silently ignored in background refresh)
				_ = m.RefreshModels(ctx, providerName)
			}(p.Name)
		}
		wg.Wait()
	}()
}

// createLLMProvider creates an LLM provider instance
func (m *Manager) createLLMProvider(name, apiKey string) (llm.Provider, error) {
	return m.createLLMProviderWithBaseURL(name, apiKey, "")
}

// createLLMProviderWithBaseURL creates an LLM provider instance with optional base URL
func (m *Manager) createLLMProviderWithBaseURL(name, apiKey, baseURL string) (llm.Provider, error) {
	normalized := canonicalProviderName(name)
	resolvedKey := resolveAPIKey(normalized, apiKey)

	switch normalized {
	case "openai":
		return llm.NewOpenAIProvider(resolvedKey), nil
	case "anthropic":
		return llm.NewAnthropicProvider(resolvedKey), nil
	case "google":
		return llm.NewGoogleProvider(resolvedKey), nil
	case "openrouter":
		return llm.NewOpenRouterProvider(resolvedKey), nil
	case "mistral":
		return llm.NewMistralProvider(resolvedKey), nil
	case "cerebras":
		return llm.NewCerebrasProvider(resolvedKey), nil
	case "groq":
		return llm.NewGroqProvider(resolvedKey), nil
	case "ollama":
		return llm.NewOllamaProvider(resolvedKey), nil
	case "openai-compatible":
		if baseURL == "" {
			return nil, fmt.Errorf("base URL is required for openai-compatible provider")
		}
		return llm.NewOpenAICompatibleProvider(resolvedKey, baseURL), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}

// GetProvider gets a provider by name
func (m *Manager) GetProvider(name string) (*Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.config.Providers[name]
	return p, ok
}

// ListProviders lists all providers
func (m *Manager) ListProviders() []*Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := make([]*Provider, 0, len(m.config.Providers))
	for _, p := range m.config.Providers {
		providers = append(providers, p)
	}
	return providers
}

// SetOrchestrationModel sets the orchestration model
func (m *Manager) SetOrchestrationModel(modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.OrchestrationModel = modelID
	return m.save()
}

// SetSummarizeModel sets the summarize model
func (m *Manager) SetSummarizeModel(modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.SummarizeModel = modelID
	return m.save()
}

// GetOrchestrationModel gets the orchestration model ID
func (m *Manager) GetOrchestrationModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.OrchestrationModel
}

// GetSummarizeModel gets the summarize model ID
func (m *Manager) GetSummarizeModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.SummarizeModel
}

// SearchModels searches for models using Aho-Corasick algorithm
func (m *Manager) SearchModels(query string) []*Model {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.matcher == nil {
		return nil
	}

	query = strings.ToLower(query)
	results := make([]*Model, 0)

	// Collect all models and search
	for _, provider := range m.config.Providers {
		for _, model := range provider.Models {
			// Check if query matches model ID or name
			modelText := strings.ToLower(model.ID + " " + model.Name + " " + model.Description)
			if strings.Contains(modelText, query) {
				results = append(results, model)
			}
		}
	}

	return results
}

// ListAllModels returns all available models
func (m *Manager) ListAllModels() []*Model {
	m.mu.RLock()
	defer m.mu.RUnlock()

	models := make([]*Model, 0)
	for _, provider := range m.config.Providers {
		models = append(models, provider.Models...)
	}
	return models
}

// GetModel returns a copy of the model configuration for the given ID
func (m *Manager) GetModel(modelID string) (*Model, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, provider := range m.config.Providers {
		for _, model := range provider.Models {
			if model.ID == modelID {
				clone := *model
				return &clone, true
			}
		}
	}

	return nil, false
}

// GetModelContextWindow returns the configured context window for the model if known
func (m *Manager) GetModelContextWindow(modelID string) int {
	model, ok := m.GetModel(modelID)
	if !ok {
		return 0
	}
	return model.ContextWindow
}

// GetModelMaxOutputTokens returns the maximum output tokens for the model if known
func (m *Manager) GetModelMaxOutputTokens(modelID string) int {
	model, ok := m.GetModel(modelID)
	if !ok {
		return 0
	}
	return model.MaxOutputTokens
}

// CreateClient creates an LLM client for a model
func (m *Manager) CreateClient(modelID string) (llm.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find model and provider
	var model *Model
	var provider *Provider

	for _, p := range m.config.Providers {
		for _, mod := range p.Models {
			if mod.ID == modelID {
				model = mod
				provider = p
				break
			}
		}
		if model != nil {
			break
		}
	}

	if model == nil {
		return nil, fmt.Errorf("model not found: %s", modelID)
	}

	// Create client based on provider
	apiKey := resolveAPIKey(provider.Name, provider.APIKey)
	switch canonicalProviderName(model.Provider) {
	case "openai":
		return llm.NewOpenAIClient(apiKey, model.ID)
	case "anthropic":
		return llm.NewAnthropicClient(apiKey, model.ID)
	case "google":
		return llm.NewGoogleAIClient(apiKey, model.ID)
	case "openrouter":
		return llm.NewOpenRouterClient(apiKey, model.ID)
	case "mistral":
		return llm.NewMistralClient(apiKey, model.ID)
	case "cerebras":
		return llm.NewCerebrasClient(apiKey, model.ID)
	case "groq":
		return llm.NewGroqClient(apiKey, model.ID)
	case "ollama":
		return llm.NewOllamaClient(apiKey, model.ID)
	case "openai-compatible":
		if provider.BaseURL == "" {
			return nil, fmt.Errorf("base URL is required for openai-compatible provider")
		}
		return llm.NewOpenAICompatibleClient(apiKey, provider.BaseURL, model.ID)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", model.Provider)
	}
}

// TestConnection tests a provider's API connection
func (m *Manager) TestConnection(ctx context.Context, providerName string) error {
	provider, ok := m.GetProvider(providerName)
	if !ok {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	if len(provider.Models) == 0 {
		return fmt.Errorf("no models configured for provider: %s", providerName)
	}

	// Try to create a client with the first model
	client, err := m.CreateClient(provider.Models[0].ID)
	if err != nil {
		return err
	}

	// Test with a simple prompt
	_, err = client.Complete(ctx, "Hello")
	return err
}

// rebuildMatcher rebuilds the Aho-Corasick matcher
func (m *Manager) rebuildMatcher() {
	// Collect all search terms
	terms := make([]string, 0)
	for _, provider := range m.config.Providers {
		for _, model := range provider.Models {
			terms = append(terms, strings.ToLower(model.ID))
			terms = append(terms, strings.ToLower(model.Name))
		}
	}

	if len(terms) == 0 {
		return
	}

	m.matcher = ahocorasick.NewStringMatcher(terms)
}

// PreferredModels contains the list of preferred models for each provider, ordered by preference
var PreferredModels = map[string][]string{
	"anthropic": {"claude-4-5-sonnet-20250514", "claude-sonnet-4.5", "claude-3-5-sonnet-20241022"},
	"openai":    {"gpt-5-codex", "gpt-5", "gpt-5-chat-latest", "gpt-4o"},
	"google":    {"models/gemini-2.5-pro", "models/gemini-2.0-pro-exp"},
	"mistral":   {"codestral-latest"},
	"openrouter": {
		"minimax/minimax-m2",
		"anthropic/claude-3.5-sonnet",
		"meta-llama/llama-3.1-70b-instruct",
		"openrouter/auto",
	},
	"cerebras": {"zai-glm-4.6"},
	"groq":     {"llama-3.3-70b-versatile", "mixtral-8x7b-32768"},
}

// ChooseDefaultModel chooses the best default model for a provider based on preferred models list
func (m *Manager) ChooseDefaultModel(providerName string, preferred []string) (string, error) {
	p, ok := m.GetProvider(providerName)
	if !ok || p == nil || len(p.Models) == 0 {
		return "", fmt.Errorf("no models available for provider %s", providerName)
	}

	if len(preferred) > 0 {
		for _, candidate := range preferred {
			for _, model := range p.Models {
				if strings.EqualFold(model.ID, candidate) {
					return model.ID, nil
				}
			}
		}
	}

	return p.Models[0].ID, nil
}

// ConfigureDefaultModelForProvider automatically selects and configures default models
// for a provider if no orchestration model is currently configured
func (m *Manager) ConfigureDefaultModelForProvider(providerName string) error {
	// Only auto-configure if no orchestration model is set
	if m.GetOrchestrationModel() != "" {
		return nil // Already configured, nothing to do
	}

	// Get preferred models for this provider
	preferred := PreferredModels[providerName]

	// Choose the best model
	modelName, err := m.ChooseDefaultModel(providerName, preferred)
	if err != nil {
		return err
	}

	// Set both orchestration and summarization models
	if err := m.SetOrchestrationModel(modelName); err != nil {
		return fmt.Errorf("failed to set orchestration model: %w", err)
	}
	if err := m.SetSummarizeModel(modelName); err != nil {
		return fmt.Errorf("failed to set summarization model: %w", err)
	}

	return nil
}
