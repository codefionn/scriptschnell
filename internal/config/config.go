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

// MCPConfig stores user-defined MCP servers
type MCPConfig struct {
	Servers map[string]*MCPServerConfig `json:"servers"`
}

// MCPServerConfig describes a custom MCP server
type MCPServerConfig struct {
	Type        string            `json:"type"`
	Description string            `json:"description,omitempty"`
	Command     *MCPCommandConfig `json:"command,omitempty"`
	OpenAPI     *MCPOpenAPIConfig `json:"openapi,omitempty"`
	OpenAI      *MCPOpenAIConfig  `json:"openai,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
}

// MCPCommandConfig describes a command-based MCP server
type MCPCommandConfig struct {
	Exec           []string          `json:"exec"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

// MCPOpenAPIConfig describes an OpenAPI-powered MCP server
type MCPOpenAPIConfig struct {
	SpecPath        string            `json:"spec_path"`
	URL             string            `json:"url"`
	DefaultHeaders  map[string]string `json:"default_headers,omitempty"`
	DefaultQuery    map[string]string `json:"default_query,omitempty"`
	AuthBearerToken string            `json:"auth_bearer_token,omitempty"`
	AuthBearerEnv   string            `json:"auth_bearer_env,omitempty"`
}

// MCPOpenAIConfig describes an OpenAI-powered MCP server
type MCPOpenAIConfig struct {
	Model        string  `json:"model"`
	APIKey       string  `json:"api_key,omitempty"`
	APIKeyEnvVar string  `json:"api_key_env,omitempty"`
	BaseURL      string  `json:"base_url,omitempty"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
	MaxOutput    int     `json:"max_output,omitempty"`
	ResponseJSON bool    `json:"response_json,omitempty"`
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
	MCP                MCPConfig       `json:"mcp,omitempty"`                 // Custom MCP server configuration
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
		MCP: MCPConfig{
			Servers: make(map[string]*MCPServerConfig),
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
	if config.MCP.Servers == nil {
		config.MCP.Servers = make(map[string]*MCPServerConfig)
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
