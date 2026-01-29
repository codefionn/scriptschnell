package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/secrets"
	"github.com/codefionn/scriptschnell/internal/stringsearch"
)

// Provider represents an LLM provider
type Provider struct {
	Name      string           `json:"name"`
	APIKey    string           `json:"api_key"`
	BaseURL   string           `json:"base_url,omitempty"` // Optional custom base URL for OpenAI-compatible providers
	Models    []*Model         `json:"models"`
	RateLimit *RateLimitConfig `json:"rate_limit,omitempty"`
}

// RateLimitConfig controls how quickly requests are sent to a provider.
type RateLimitConfig struct {
	// RequestsPerMinute enforces a ceiling on request throughput.
	// If both fields are set, the slower effective interval wins.
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`
	// MinIntervalMillis enforces a fixed delay between the start of each request.
	MinIntervalMillis int `json:"min_interval_ms,omitempty"`
	// TokensPerMinute limits how many prompt tokens (including tool output) are sent per minute.
	TokensPerMinute int `json:"tokens_per_minute,omitempty"`
}

func (p *Provider) rateLimitInterval() time.Duration {
	if p == nil || p.RateLimit == nil {
		return 0
	}

	var interval time.Duration
	if p.RateLimit.MinIntervalMillis > 0 {
		interval = time.Duration(p.RateLimit.MinIntervalMillis) * time.Millisecond
	}
	if p.RateLimit.RequestsPerMinute > 0 {
		rpmInterval := time.Minute / time.Duration(p.RateLimit.RequestsPerMinute)
		if interval == 0 || rpmInterval > interval {
			interval = rpmInterval
		}
	}
	return interval
}

func (p *Provider) tokensPerMinute() int {
	if p == nil || p.RateLimit == nil {
		return 0
	}
	return p.RateLimit.TokensPerMinute
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
	PlanningModel      string               `json:"planning_model"`
	SafetyModel        string               `json:"safety_model,omitempty"`
}

// Manager manages LLM providers
type Manager struct {
	config        *Config
	configPath    string
	matcher       stringsearch.StringMatcher
	cacheActorRef *actor.ActorRef
	mu            sync.RWMutex
	password      string
}

// NewManager creates a new provider manager
func NewManager(configPath, password string) (*Manager, error) {
	cacheDir, err := providerModelsCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine provider cache directory: %w", err)
	}

	cacheActor := newProviderModelsCacheActor("provider-model-cache", cacheDir)
	cacheRef := actor.NewActorRef(cacheActor.ID(), cacheActor, 32)
	if err := cacheRef.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start provider cache actor: %w", err)
	}

	m := &Manager{
		configPath:    configPath,
		cacheActorRef: cacheRef,
		config: &Config{
			Providers: make(map[string]*Provider),
		},
		password: password,
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

	plaintext, err := m.maybeDecrypt(data)
	if err != nil {
		return err
	}

	var config Config
	if err := json.Unmarshal(plaintext, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	for name, provider := range config.Providers {
		if provider == nil {
			continue
		}

		if len(provider.Models) > 0 {
			// Persist embedded models to cache, but don't keep in memory during startup
			if err := m.saveProviderModels(name, provider.Models); err != nil {
				logger.Warn("provider: failed to persist embedded models for %s: %v", name, err)
			}
			provider.Models = nil // Clear from memory - will be lazy-loaded
		}
	}

	m.mu.Lock()
	m.config = &config
	m.mu.Unlock()

	// Don't rebuild matcher at startup - will be done on first use
	return nil
}

// ensureModelsLoaded lazily loads models for a provider if not already loaded
func (m *Manager) ensureModelsLoaded(providerName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	provider, ok := m.config.Providers[providerName]
	if !ok || provider == nil {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	// Already loaded
	if len(provider.Models) > 0 {
		return nil
	}

	// Load from cache
	models, err := m.loadProviderModels(providerName)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("provider: failed to load cached models for %s: %v", providerName, err)
		}
		return err
	}

	provider.Models = models
	return nil
}

// ensureMatcherBuilt ensures the search matcher is built
func (m *Manager) ensureMatcherBuilt() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.matcher == nil {
		m.rebuildMatcher()
	}
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

	configCopy := &Config{
		Providers:          make(map[string]*Provider, len(m.config.Providers)),
		OrchestrationModel: m.config.OrchestrationModel,
		SummarizeModel:     m.config.SummarizeModel,
		PlanningModel:      m.config.PlanningModel,
	}
	for name, provider := range m.config.Providers {
		if provider == nil {
			continue
		}
		clone := *provider
		clone.Models = nil
		configCopy.Providers[name] = &clone
	}

	data, err := json.MarshalIndent(configCopy, "", "  ")
	if err != nil {
		return err
	}

	encrypted, err := m.encrypt(data)
	if err != nil {
		return err
	}

	// Atomic write: write to temporary file then rename
	tmpPath := m.configPath + ".tmp"
	if err := os.WriteFile(tmpPath, encrypted, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, m.configPath)
}

func (m *Manager) maybeDecrypt(data []byte) ([]byte, error) {
	payload, err := secrets.DecodePayload(data)
	if err != nil {
		// Legacy plaintext file; return as-is.
		return data, nil
	}

	plaintext, err := secrets.DecryptBytes(payload, m.password)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func (m *Manager) encrypt(plaintext []byte) ([]byte, error) {
	payload, err := secrets.EncryptBytes(plaintext, m.password)
	if err != nil {
		return nil, err
	}
	return secrets.EncodePayload(payload)
}

// SetPassword updates the password used to encrypt provider config and forces a re-save.
func (m *Manager) SetPassword(password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.password = password
	if m.config == nil {
		return nil
	}
	return m.save()
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

	if err := m.saveProviderModels(name, models); err != nil {
		return fmt.Errorf("failed to cache models for provider %s: %w", name, err)
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
	cacheErr := m.saveProviderModels(providerName, models)
	saveErr := m.save()
	m.mu.Unlock()

	if cacheErr != nil {
		return fmt.Errorf("failed to cache models for provider %s: %w", providerName, cacheErr)
	}
	return saveErr
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

// RefreshAllModelsSync refreshes models for all configured providers synchronously
// Returns the first error encountered, if any
func (m *Manager) RefreshAllModelsSync(ctx context.Context) error {
	providers := m.ListProviders()
	if len(providers) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(providers))

	for _, p := range providers {
		// Skip if context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Refresh each provider in parallel
		wg.Add(1)
		go func(providerName string) {
			defer wg.Done()
			// Refresh models and send any errors to channel
			if err := m.RefreshModels(ctx, providerName); err != nil {
				errChan <- fmt.Errorf("failed to refresh models for provider %s: %w", providerName, err)
			}
		}(p.Name)
	}

	// Wait for all refreshes to complete
	wg.Wait()
	close(errChan)

	// Return first error if any
	for err := range errChan {
		return err
	}

	return nil
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
	case "kimi":
		return llm.NewKimiProvider(resolvedKey), nil
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

// GetProviderWithModels gets a provider by name and ensures models are loaded
func (m *Manager) GetProviderWithModels(name string) (*Provider, bool) {
	_ = m.ensureModelsLoaded(name)
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

// UpdateProviderRateLimit updates or clears a provider's rate limit settings.
func (m *Manager) UpdateProviderRateLimit(providerName string, cfg *RateLimitConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.config.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	var normalized *RateLimitConfig
	if cfg != nil {
		if cfg.RequestsPerMinute <= 0 && cfg.MinIntervalMillis <= 0 && cfg.TokensPerMinute <= 0 {
			normalized = nil
		} else {
			copyCfg := *cfg
			if copyCfg.RequestsPerMinute < 0 {
				copyCfg.RequestsPerMinute = 0
			}
			if copyCfg.MinIntervalMillis < 0 {
				copyCfg.MinIntervalMillis = 0
			}
			if copyCfg.TokensPerMinute < 0 {
				copyCfg.TokensPerMinute = 0
			}
			normalized = &copyCfg
		}
	}

	p.RateLimit = normalized
	return m.save()
}

// UpdateProviderConnection updates stored credentials/base URL for a provider.
// Blank values leave the existing configuration untouched.
func (m *Manager) UpdateProviderConnection(providerName, apiKey, baseURL string) error {
	apiKey = strings.TrimSpace(apiKey)
	baseURL = strings.TrimSpace(baseURL)
	if apiKey == "" && baseURL == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.config.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	updated := false
	if apiKey != "" && apiKey != p.APIKey {
		p.APIKey = apiKey
		updated = true
	}
	if baseURL != "" && baseURL != p.BaseURL {
		p.BaseURL = baseURL
		updated = true
	}

	if !updated {
		return nil
	}

	return m.save()
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

// SetSafetyModel sets the safety model
func (m *Manager) SetSafetyModel(modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.SafetyModel = modelID
	return m.save()
}

// SetPlanningModel sets the planning model
func (m *Manager) SetPlanningModel(modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.PlanningModel = modelID
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

// GetSafetyModel gets the safety model ID
func (m *Manager) GetSafetyModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.SafetyModel
}

// GetPlanningModel gets the planning model ID
func (m *Manager) GetPlanningModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.PlanningModel
}

// SearchModels searches for models using Aho-Corasick algorithm
func (m *Manager) SearchModels(query string) []*Model {
	m.ensureMatcherBuilt()

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.matcher == nil {
		return nil
	}

	query = strings.ToLower(query)
	results := make([]*Model, 0)

	// Collect all models and search
	for _, provider := range m.config.Providers {
		// Ensure models are loaded for this provider
		if len(provider.Models) == 0 {
			// Release lock temporarily to load models
			m.mu.RUnlock()
			_ = m.ensureModelsLoaded(provider.Name)
			m.mu.RLock()
			// Get provider again since it may have been reloaded
			p := m.config.Providers[provider.Name]
			if p != nil {
				provider = p
			}
		}
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
	m.ensureMatcherBuilt()

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

	// Get the canonical provider name
	provName := canonicalProviderName(model.Provider)

	// Create client based on provider
	apiKey := resolveAPIKey(provider.Name, provider.APIKey)
	var (
		client llm.Client
		err    error
	)
	switch provName {
	case "openai":
		client, err = llm.NewOpenAIClient(apiKey, model.ID)
	case "anthropic":
		client, err = llm.NewAnthropicClient(apiKey, model.ID)
	case "google":
		client, err = llm.NewGoogleAIClient(apiKey, model.ID)
	case "openrouter":
		client, err = llm.NewOpenRouterClient(apiKey, model.ID)
	case "mistral":
		client, err = llm.NewMistralClient(apiKey, model.ID)
	case "cerebras":
		client, err = llm.NewCerebrasClient(apiKey, model.ID)
	case "groq":
		client, err = llm.NewGroqClient(apiKey, model.ID)
	case "ollama":
		client, err = llm.NewOllamaClient(apiKey, model.ID)
	case "kimi":
		client, err = llm.NewKimiClient(apiKey, model.ID)
	case "openai-compatible":
		if provider.BaseURL == "" {
			return nil, fmt.Errorf("base URL is required for openai-compatible provider")
		}
		client, err = llm.NewOpenAICompatibleClient(apiKey, provider.BaseURL, model.ID)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", model.Provider)
	}

	if err != nil {
		return nil, err
	}

	// Wrap with caching-aware client to disable caching for OpenAI-compatible providers
	client = llm.NewCachingAwareClient(client, provName)

	interval := provider.rateLimitInterval()
	tokensPerMinute := provider.tokensPerMinute()
	if interval > 0 || tokensPerMinute > 0 {
		return llm.NewRateLimitedClient(client, interval, tokensPerMinute), nil
	}

	return client, nil
}

type warmupSpec struct {
	providerName string
	apiKey       string
	baseURL      string
}

// collectWarmupSpecs gathers provider connection info for the provided model IDs (deduplicated by provider).
func (m *Manager) collectWarmupSpecs(modelIDs []string) []warmupSpec {
	if len(modelIDs) == 0 {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	specs := make([]warmupSpec, 0, len(modelIDs))
	seen := make(map[string]struct{})

	for _, modelID := range modelIDs {
		if strings.TrimSpace(modelID) == "" {
			continue
		}

		for _, provider := range m.config.Providers {
			if _, exists := seen[provider.Name]; exists {
				continue
			}

			for _, model := range provider.Models {
				if model.ID == modelID {
					specs = append(specs, warmupSpec{
						providerName: provider.Name,
						apiKey:       provider.APIKey,
						baseURL:      provider.BaseURL,
					})
					seen[provider.Name] = struct{}{}
					break
				}
			}
		}
	}

	return specs
}

// WarmConnections performs lightweight authenticated requests for the given models to establish TLS connections upfront.
// It returns two booleans: attempted indicates whether at least one provider was targeted, and success indicates whether
// any warmup call succeeded.
func (m *Manager) WarmConnections(ctx context.Context, modelIDs ...string) (attempted bool, success bool) {
	specs := m.collectWarmupSpecs(modelIDs)
	if len(specs) == 0 {
		return false, false
	}

	if ctx == nil {
		ctx = context.Background()
	}

	for _, spec := range specs {
		attempted = true

		llmProvider, err := m.createLLMProviderWithBaseURL(spec.providerName, spec.apiKey, spec.baseURL)
		if err != nil {
			logger.Debug("WarmConnections: unable to build provider %s: %v", spec.providerName, err)
			continue
		}

		warmCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = llmProvider.ValidateAPIKey(warmCtx)
		cancel()
		if err != nil {
			logger.Debug("WarmConnections: preconnect to %s failed: %v", spec.providerName, err)
			continue
		}

		success = true
	}

	return attempted, success
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

	m.matcher = stringsearch.NewStringMatcher(terms)
}

func (m *Manager) saveProviderModels(providerName string, models []*Model) error {
	if m.cacheActorRef == nil {
		return fmt.Errorf("provider cache actor is not initialized")
	}

	response := make(chan error, 1)
	msg := providerModelsSaveMsg{
		ProviderName: providerName,
		Models:       models,
		ResponseChan: response,
	}
	if err := m.cacheActorRef.Send(msg); err != nil {
		return err
	}
	return <-response
}

func (m *Manager) loadProviderModels(providerName string) ([]*Model, error) {
	if m.cacheActorRef == nil {
		return nil, fmt.Errorf("provider cache actor is not initialized")
	}

	response := make(chan providerModelsLoadResponse, 1)
	msg := providerModelsLoadMsg{
		ProviderName: providerName,
		ResponseChan: response,
	}
	if err := m.cacheActorRef.Send(msg); err != nil {
		return nil, err
	}
	res := <-response
	return res.Models, res.Err
}

// PreferredModels contains the list of preferred models for each provider, ordered by preference
var PreferredModels = map[string][]string{
	"anthropic": {"claude-haiku-4-5", "claude-sonnet-4-5", "claude-3-5-sonnet-20241022"},
	"openai":    {"gpt-5.1-codex", "gpt-5.1-codex-max", "o3-mini", "gpt-5", "gpt-4o"},
	"google":    {"models/gemini-2.5-flash", "models/gemini-2.0-pro-exp"},
	"mistral":   {"codestral-latest", "mistral-large-latest"},
	"openrouter": {
		"deepseek/deepseek-v3.2",
		"anthropic/claude-haiku-4.5",
		"google/gemini-2.0-flash-001",
		"meta-llama/llama-3.3-70b-instruct",
		"openrouter/auto",
	},
	"cerebras": {"zai-glm-4.6", "qwen-3-235b-a22b-instruct-2507"},
	"groq":     {"llama-3.3-70b-versatile", "mixtral-8x7b-32768"},
	"kimi":     {"kimi-k2.5", "kimi-k2-turbo-preview", "kimi-k2-thinking", "moonshot-v1-128k"},
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
