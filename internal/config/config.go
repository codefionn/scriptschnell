package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SearchConfig holds configuration for web search providers
type SearchConfig struct {
	Provider   string           `json:"provider"` // "exa", "google_pse", "perplexity", or ""
	Exa        ExaConfig        `json:"exa"`
	GooglePSE  GooglePSEConfig  `json:"google_pse"`
	Perplexity PerplexityConfig `json:"perplexity"`
}

// ExaConfig holds Exa AI Search API configuration
type ExaConfig struct {
	APIKey string `json:"api_key"`
}

// GooglePSEConfig holds Google Programmable Search Engine configuration
type GooglePSEConfig struct {
	APIKey string `json:"api_key"`
	CX     string `json:"cx"` // Search Engine ID
}

// PerplexityConfig holds Perplexity Search API configuration
type PerplexityConfig struct {
	APIKey string `json:"api_key"`
}

// Config represents application configuration
type Config struct {
	WorkingDir         string          `json:"working_dir"`
	CacheTTL           int             `json:"cache_ttl_seconds"`
	MaxCacheEntries    int             `json:"max_cache_entries"`
	DefaultTimeout     int             `json:"default_timeout_seconds"`
	TempDir            string          `json:"temp_dir"`
	Temperature        float64         `json:"temperature"`
	MaxTokens          int             `json:"max_tokens,omitempty"` // DEPRECATED: Only used as fallback when model doesn't specify context window
	ProviderConfigPath string          `json:"provider_config_path"`
	DisableAnimations  bool            `json:"disable_animations"`
	LogLevel           string          `json:"log_level"`                     // debug, info, warn, error, none
	LogPath            string          `json:"log_path"`                      // path to log file
	AuthorizedDomains  map[string]bool `json:"authorized_domains,omitempty"`  // Permanently authorized domains for network access
	AuthorizedCommands map[string]bool `json:"authorized_commands,omitempty"` // Permanently authorized command prefixes for this project
	Search             SearchConfig    `json:"search"`                        // Web search provider configuration
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".config", "statcode-ai")

	return &Config{
		WorkingDir:         ".",
		CacheTTL:           300,
		MaxCacheEntries:    100,
		DefaultTimeout:     30,
		TempDir:            filepath.Join(os.TempDir(), "statcode-ai"),
		Temperature:        0.7,
		MaxTokens:          4096,
		ProviderConfigPath: filepath.Join(configDir, "providers.json"),
		LogLevel:           "info",
		LogPath:            filepath.Join(configDir, "statcode-ai.log"),
		AuthorizedDomains:  make(map[string]bool),
		AuthorizedCommands: make(map[string]bool),
		Search: SearchConfig{
			Provider:   "",
			Exa:        ExaConfig{APIKey: ""},
			GooglePSE:  GooglePSEConfig{APIKey: "", CX: ""},
			Perplexity: PerplexityConfig{APIKey: ""},
		},
	}
}

// Load loads configuration from file
func Load(path string) (*Config, error) {
	// Start with default config
	config := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config if file doesn't exist
			return config, nil
		}
		return nil, err
	}

	// Unmarshal into default config (overrides only provided fields)
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	// Ensure critical fields have defaults if still empty
	if config.TempDir == "" {
		config.TempDir = filepath.Join(os.TempDir(), "statcode-ai")
	}
	if config.WorkingDir == "" {
		config.WorkingDir = "."
	}
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".config", "statcode-ai")
	if config.ProviderConfigPath == "" {
		config.ProviderConfigPath = filepath.Join(configDir, "providers.json")
	}
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	if config.LogPath == "" {
		config.LogPath = filepath.Join(configDir, "statcode-ai.log")
	}
	if config.AuthorizedDomains == nil {
		config.AuthorizedDomains = make(map[string]bool)
	}
	if config.AuthorizedCommands == nil {
		config.AuthorizedCommands = make(map[string]bool)
	}

	return config, nil
}

// AuthorizeDomain adds a domain to the permanently authorized list
func (c *Config) AuthorizeDomain(domain string) {
	if c.AuthorizedDomains == nil {
		c.AuthorizedDomains = make(map[string]bool)
	}
	c.AuthorizedDomains[domain] = true
}

// IsDomainAuthorized checks if a domain is permanently authorized
func (c *Config) IsDomainAuthorized(domain string) bool {
	if c.AuthorizedDomains == nil {
		return false
	}
	return c.AuthorizedDomains[domain]
}

// AuthorizeCommand adds a command prefix to the permanently authorized list
func (c *Config) AuthorizeCommand(commandPrefix string) {
	if c.AuthorizedCommands == nil {
		c.AuthorizedCommands = make(map[string]bool)
	}
	c.AuthorizedCommands[commandPrefix] = true
}

// IsCommandAuthorized checks if a command prefix is permanently authorized
func (c *Config) IsCommandAuthorized(commandPrefix string) bool {
	if c.AuthorizedCommands == nil {
		return false
	}
	return c.AuthorizedCommands[commandPrefix]
}

// Save saves configuration to file
func (c *Config) Save(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetConfigPath returns the default config path
func GetConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "statcode-ai", "config.json")
}
