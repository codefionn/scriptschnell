package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/secrets"
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
	TempDir            string          `json:"-"`
	Temperature        float64         `json:"temperature"`
	MaxTokens          int             `json:"max_tokens,omitempty"` // DEPRECATED: Only used as fallback when model doesn't specify context window
	ProviderConfigPath string          `json:"-"`
	DisableAnimations  bool            `json:"disable_animations"`
	LogLevel           string          `json:"log_level"` // debug, info, warn, error, none
	LogPath            string          `json:"-"`
	AuthorizedDomains  map[string]bool `json:"authorized_domains,omitempty"`  // Permanently authorized domains for network access
	AuthorizedCommands map[string]bool `json:"authorized_commands,omitempty"` // Permanently authorized command prefixes for this project
	Search             SearchConfig    `json:"search"`                        // Web search provider configuration
	MCP                MCPConfig       `json:"mcp,omitempty"`                 // Custom MCP server configuration
	Secrets            SecretsSettings `json:"secrets,omitempty"`             // Encryption settings
	EnablePromptCache  bool            `json:"enable_prompt_cache"`           // Enable prompt caching for compatible providers (Anthropic, OpenAI, OpenRouter)
	PromptCacheTTL     string          `json:"prompt_cache_ttl,omitempty"`    // Cache TTL: "5m" or "1h" (default: "1h", Anthropic only)

	secretsPassword string `json:"-"`
}

// SecretsSettings keeps track of password-protection state.
type SecretsSettings struct {
	PasswordSet bool   `json:"password_set,omitempty"`
	Verifier    string `json:"verifier,omitempty"`
}

func defaultConfigDir() string {
	switch runtime.GOOS {
	case "linux":
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".config", "statcode-ai")
	case "windows":
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, "statcode-ai")
		}
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "AppData", "Roaming", "statcode-ai")
	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".config", "statcode-ai")
	}
}

func defaultStateDir() string {
	switch runtime.GOOS {
	case "linux":
		if stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); stateHome != "" {
			return filepath.Join(stateHome, "statcode-ai")
		}
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".local", "state", "statcode-ai")
	case "windows":
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "statcode-ai")
		}
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "AppData", "Local", "statcode-ai")
	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".config", "statcode-ai")
	}
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	configDir := defaultConfigDir()
	stateDir := defaultStateDir()

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
		LogPath:            filepath.Join(stateDir, "statcode-ai.log"),
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
		Secrets:           SecretsSettings{},
		EnablePromptCache: true,  // Enable by default for cost savings
		PromptCacheTTL:    "1h",  // Default to 1 hour for longer sessions
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
	configDir := defaultConfigDir()
	stateDir := defaultStateDir()
	if config.ProviderConfigPath == "" {
		config.ProviderConfigPath = filepath.Join(configDir, "providers.json")
	}
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	if config.LogPath == "" {
		config.LogPath = filepath.Join(stateDir, "statcode-ai.log")
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

	data, err := c.marshalWithEncryptedSecrets()
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetConfigPath returns the default config path
func GetConfigPath() string {
	return filepath.Join(defaultConfigDir(), "config.json")
}

// ApplySecretsPassword records the active password and decrypts any encrypted fields.
func (c *Config) ApplySecretsPassword(password string) error {
	if err := c.decryptSensitiveFields(password); err != nil {
		return err
	}
	c.secretsPassword = password
	return nil
}

// SecretsPassword returns the active secrets password (empty string by default).
func (c *Config) SecretsPassword() string {
	return c.secretsPassword
}

// UpdateSecretsPassword switches the runtime password and updates the persisted flags.
func (c *Config) UpdateSecretsPassword(password string) error {
	if c == nil {
		return nil
	}
	c.Secrets.PasswordSet = password != ""
	c.Secrets.Verifier = ""
	return c.ApplySecretsPassword(password)
}

func (c *Config) decryptSensitiveFields(password string) error {
	if err := c.verifyPassword(password); err != nil {
		return err
	}

	fields := []*string{
		&c.Search.Exa.APIKey,
		&c.Search.GooglePSE.APIKey,
		&c.Search.Perplexity.APIKey,
	}

	for _, srv := range c.mcpServersInOrder() {
		if srv.OpenAI != nil {
			fields = append(fields, &srv.OpenAI.APIKey)
		}
		if srv.OpenAPI != nil {
			fields = append(fields, &srv.OpenAPI.AuthBearerToken)
		}
	}

	for _, field := range fields {
		if field == nil {
			continue
		}
		if err := decryptField(field, password); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) marshalWithEncryptedSecrets() ([]byte, error) {
	copyCfg := *c
	copyCfg.Search = c.Search

	var err error
	copyCfg.Search.Exa.APIKey, err = encryptField(copyCfg.Search.Exa.APIKey, c.secretsPassword)
	if err != nil {
		return nil, err
	}
	copyCfg.Search.GooglePSE.APIKey, err = encryptField(copyCfg.Search.GooglePSE.APIKey, c.secretsPassword)
	if err != nil {
		return nil, err
	}
	copyCfg.Search.Perplexity.APIKey, err = encryptField(copyCfg.Search.Perplexity.APIKey, c.secretsPassword)
	if err != nil {
		return nil, err
	}

	copyCfg.MCP.Servers = c.cloneMCPServersForSave()

	for _, srv := range copyCfg.MCP.Servers {
		if srv == nil {
			continue
		}
		if srv.OpenAI != nil {
			srv.OpenAI.APIKey, err = encryptField(srv.OpenAI.APIKey, c.secretsPassword)
			if err != nil {
				return nil, err
			}
		}
		if srv.OpenAPI != nil {
			srv.OpenAPI.AuthBearerToken, err = encryptField(srv.OpenAPI.AuthBearerToken, c.secretsPassword)
			if err != nil {
				return nil, err
			}
		}
	}

	if copyCfg.Secrets.PasswordSet {
		copyCfg.Secrets.Verifier, err = encryptField("statcode-ai", c.secretsPassword)
		if err != nil {
			return nil, err
		}
	} else {
		copyCfg.Secrets.Verifier = ""
	}

	return json.MarshalIndent(&copyCfg, "", "  ")
}

func (c *Config) cloneMCPServersForSave() map[string]*MCPServerConfig {
	if c.MCP.Servers == nil {
		return nil
	}
	clone := make(map[string]*MCPServerConfig, len(c.MCP.Servers))
	for name, srv := range c.MCP.Servers {
		if srv == nil {
			clone[name] = nil
			continue
		}
		srvCopy := *srv
		if srv.OpenAI != nil {
			openAICopy := *srv.OpenAI
			srvCopy.OpenAI = &openAICopy
		}
		if srv.OpenAPI != nil {
			openAPICopy := *srv.OpenAPI
			srvCopy.OpenAPI = &openAPICopy
		}
		if srv.Command != nil {
			cmdCopy := *srv.Command
			if len(cmdCopy.Env) > 0 {
				envCopy := make(map[string]string, len(cmdCopy.Env))
				for k, v := range cmdCopy.Env {
					envCopy[k] = v
				}
				cmdCopy.Env = envCopy
			}
			if cmdCopy.Exec != nil {
				cmdCopy.Exec = append([]string{}, cmdCopy.Exec...)
			}
			srvCopy.Command = &cmdCopy
		}
		clone[name] = &srvCopy
	}
	return clone
}

func (c *Config) mcpServersInOrder() []*MCPServerConfig {
	if len(c.MCP.Servers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.MCP.Servers))
	for k := range c.MCP.Servers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	servers := make([]*MCPServerConfig, 0, len(keys))
	for _, k := range keys {
		servers = append(servers, c.MCP.Servers[k])
	}
	return servers
}

func encryptField(value, password string) (string, error) {
	if value == "" {
		return "", nil
	}
	return secrets.EncryptString(value, password)
}

func decryptField(value *string, password string) error {
	if value == nil || *value == "" {
		return nil
	}
	plain, encrypted, err := secrets.DecryptString(*value, password)
	if err != nil && encrypted {
		return err
	}
	if encrypted && err == nil {
		*value = plain
	}
	return nil
}

func (c *Config) verifyPassword(password string) error {
	if !c.Secrets.PasswordSet || c.Secrets.Verifier == "" {
		return nil
	}
	_, _, err := secrets.DecryptString(c.Secrets.Verifier, password)
	return err
}
