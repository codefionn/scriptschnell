package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/secrets"
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

// AutoSaveConfig holds configuration for automatic session saving
type AutoSaveConfig struct {
	Enabled             bool `json:"enabled"`
	SaveIntervalSeconds int  `json:"save_interval_seconds"`
	MaxConcurrentSaves  int  `json:"max_concurrent_saves"`
}

// SandboxOutputCompactionConfig holds configuration for sandbox output compaction
type SandboxOutputCompactionConfig struct {
	Enabled              bool    `json:"enabled"`
	ContextWindowPercent float64 `json:"context_window_percent"` // Compaction threshold as percentage of context window (e.g., 0.1 for 10%)
	ChunkSize            int     `json:"chunk_size"`             // Size of each chunk in characters
}

// LoopConfig holds configuration for the orchestrator loop abstraction
type LoopConfig struct {
	Strategy                       string `json:"strategy"`                                // Loop strategy: "default", "conservative", "aggressive", "llm-judge"
	MaxIterations                  int    `json:"max_iterations"`                          // Maximum number of iterations (0 = use default)
	MaxAutoContinueAttempts        int    `json:"max_auto_continue_attempts"`              // Maximum auto-continue attempts (0 = use default)
	EnableLoopDetection            bool   `json:"enable_loop_detection"`                   // Enable repetitive pattern detection
	EnableAutoContinue             bool   `json:"enable_auto_continue"`                    // Enable automatic continuation on incomplete responses
	EnableLLMAutoContinueJudge     bool   `json:"enable_llm_auto_continue_judge"`          // Enable LLM-based auto-continue decisions
	LLMAutoContinueJudgeTimeout    int    `json:"llm_auto_continue_judge_timeout_seconds"` // LLM judge timeout in seconds (0 = use default 15s)
	LLMAutoContinueJudgeTokenLimit int    `json:"llm_auto_continue_judge_token_limit"`     // LLM judge token limit (0 = use default 1000)
}

// SandboxConfig holds configuration for shell command sandboxing
// This allows custom paths to be added to the landlock sandbox
// Default package manager paths are handled automatically
type SandboxConfig struct {
	// AdditionalReadOnlyPaths are extra directories to allow read-only access
	AdditionalReadOnlyPaths []string `json:"additional_read_only_paths,omitempty"`

	// AdditionalReadWritePaths are extra directories to allow full access
	AdditionalReadWritePaths []string `json:"additional_read_write_paths,omitempty"`

	// DisableSandbox disables landlock sandboxing entirely (not recommended)
	DisableSandbox bool `json:"disable_sandbox,omitempty"`

	// BestEffort enables best-effort mode for landlock restrictions.
	// When true (default), landlock will apply restrictions even if some
	// rules cannot be enforced (e.g., due to insufficient kernel support).
	// When false, landlock will fail if it cannot fully enforce all restrictions.
	BestEffort bool `json:"best_effort,omitempty"`
}

// SocketConfig holds configuration for the Unix socket server
type SocketConfig struct {
	Enabled               bool   `json:"enabled"`                     // Enable/disable socket server
	AutoConnect           bool   `json:"auto_connect"`                // Auto-detect and connect to socket server in clients
	Path                  string `json:"path"`                        // Socket file path (~/.scriptschnell.sock)
	Permissions           string `json:"permissions,omitempty"`       // Octal permissions (e.g., "0600")
	RequireAuth           bool   `json:"require_auth"`                // Whether auth is required
	AuthMethod            string `json:"auth_method,omitempty"`       // "file", "token", "challenge", "peercred"
	Token                 string `json:"token,omitempty"`             // Pre-shared token (empty string = not encrypted)
	AllowedUIDs           []int  `json:"allowed_uids,omitempty"`      // Allowed user IDs for peercred
	AllowedGIDs           []int  `json:"allowed_gids,omitempty"`      // Allowed group IDs for peercred
	MaxConnections        int    `json:"max_connections"`             // Max concurrent connections
	MaxSessionsPerConn    int    `json:"max_sessions_per_connection"` // Max sessions per connection
	ConnectionTimeoutSecs int    `json:"connection_timeout_seconds"`  // Idle timeout in seconds
	EnableBatching        bool   `json:"enable_batching"`             // Enable message batching
	BatchSize             int    `json:"batch_size"`                  // Messages per batch
}

// DefaultSocketPath is the default socket path
const DefaultSocketPath = "~/.scriptschnell.sock"

// testSocketPath is used during tests to avoid conflicts with production socket
var testSocketPath = ""

// SetTestSocketPath sets the socket path for testing (should be called in test init)
func SetTestSocketPath(path string) {
	testSocketPath = path
}

// GetSocketPath returns the expanded socket path with ~ expansion
func (s *SocketConfig) GetSocketPath() string {
	path := s.Path
	if path == "" {
		// Use test socket path if set (for tests)
		if testSocketPath != "" {
			path = testSocketPath
		} else {
			path = DefaultSocketPath
		}
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(homeDir, path[2:])
		}
	}

	return path
}

// Config represents application configuration
type Config struct {
	WorkingDir              string                                 `json:"working_dir"`
	CacheTTL                int                                    `json:"cache_ttl_seconds"`
	MaxCacheEntries         int                                    `json:"max_cache_entries"`
	DefaultTimeout          int                                    `json:"default_timeout_seconds"`
	TempDir                 string                                 `json:"-"`
	Temperature             float64                                `json:"temperature"`
	MaxTokens               int                                    `json:"max_tokens,omitempty"` // DEPRECATED: Only used as fallback when model doesn't specify context window
	ProviderConfigPath      string                                 `json:"-"`
	DisableAnimations       bool                                   `json:"disable_animations"`
	LogLevel                string                                 `json:"log_level"` // debug, info, warn, error, none
	LogPath                 string                                 `json:"-"`
	LogToConsole            bool                                   `json:"log_to_console"`                // Enable console logging in addition to file logging
	AuthorizedDomains       map[string]bool                        `json:"authorized_domains,omitempty"`  // Permanently authorized domains for network access
	AuthorizedCommands      map[string]bool                        `json:"authorized_commands,omitempty"` // Permanently authorized command prefixes for this project
	Search                  SearchConfig                           `json:"search"`                        // Web search provider configuration
	MCP                     MCPConfig                              `json:"mcp,omitempty"`                 // Custom MCP server configuration
	Secrets                 SecretsSettings                        `json:"secrets,omitempty"`             // Encryption settings
	EnablePromptCache       bool                                   `json:"enable_prompt_cache"`           // Enable prompt caching for compatible providers (Anthropic, OpenAI). Disabled by default as some providers like Mistral don't support cache_control ephemeral
	PromptCacheTTL          string                                 `json:"prompt_cache_ttl,omitempty"`    // Cache TTL: "5m" or "1h" (default: "1h", Anthropic only)
	ContextDirectories      map[string][]string                    `json:"context_directories,omitempty"` // Workspace-specific context directories (map of workspace path -> directories)
	OpenTabs                map[string]*WorkspaceTabState          `json:"open_tabs,omitempty"`           // Workspace-specific open tabs state (map of workspace path -> tab state)
	LandlockApprovals       map[string]*LandlockWorkspaceApprovals `json:"landlock_approvals,omitempty"`  // Workspace-specific landlock approvals (map of workspace hash -> approvals)
	Sandbox                 SandboxConfig                          `json:"sandbox,omitempty"`             // Sandbox configuration for shell commands
	AutoSave                AutoSaveConfig                         `json:"auto_save,omitempty"`           // Session auto-save configuration
	AutoResume              bool                                   `json:"auto_resume"`                   // Automatically resume last session on startup
	SandboxOutputCompaction SandboxOutputCompactionConfig          `json:"sandbox_output_compaction"`     // Sandbox output compaction configuration
	Socket                  SocketConfig                           `json:"socket,omitempty"`              // Unix socket server configuration
	Loop                    LoopConfig                             `json:"loop,omitempty"`                // Loop abstraction configuration

	authMu          sync.RWMutex `json:"-"` // Protects AuthorizedDomains and AuthorizedCommands for concurrent access
	secretsPassword string       `json:"-"` // Kept for backward compatibility
	mu              sync.RWMutex `json:"-"` // Protects the entire config during save/load operations
}

// SecretsSettings keeps track of password-protection state.
type SecretsSettings struct {
	PasswordSet bool   `json:"password_set,omitempty"`
	Verifier    string `json:"verifier,omitempty"`
}

// WorkspaceTabState tracks open tabs for a specific workspace
type WorkspaceTabState struct {
	ActiveTabID   int            `json:"active_tab_id"`            // ID of currently active tab
	TabIDs        []int          `json:"tab_ids"`                  // Ordered list of tab IDs
	TabNames      map[int]string `json:"tab_names,omitempty"`      // Tab ID -> name mapping
	WorktreePaths map[int]string `json:"worktree_paths,omitempty"` // Tab ID -> worktree path
}

// LandlockApproval represents an approved directory path for sandboxed shell execution
type LandlockApproval struct {
	Path        string `json:"path"`
	AccessLevel string `json:"access_level"` // "read" or "readwrite"
}

// LandlockWorkspaceApprovals stores landlock approvals for a specific workspace
type LandlockWorkspaceApprovals struct {
	Directories []LandlockApproval `json:"directories,omitempty"`
}

func defaultConfigDir() string {
	switch runtime.GOOS {
	case "linux":
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".config", "scriptschnell")
	case "windows":
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, "scriptschnell")
		}
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "AppData", "Roaming", "scriptschnell")
	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".config", "scriptschnell")
	}
}

func defaultStateDir() string {
	switch runtime.GOOS {
	case "linux":
		if stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); stateHome != "" {
			return filepath.Join(stateHome, "scriptschnell")
		}
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".local", "state", "scriptschnell")
	case "windows":
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "scriptschnell")
		}
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "AppData", "Local", "scriptschnell")
	default:
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".config", "scriptschnell")
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
		TempDir:            filepath.Join(os.TempDir(), "scriptschnell"),
		Temperature:        1.0,
		MaxTokens:          16384,
		ProviderConfigPath: filepath.Join(configDir, "providers.json"),
		LogLevel:           "info",
		LogPath:            filepath.Join(stateDir, "scriptschnell.log"),
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
		Secrets:            SecretsSettings{},
		EnablePromptCache:  false,                     // Disabled by default - some providers like Mistral don't support cache_control ephemeral
		PromptCacheTTL:     "5m",                      // Default to 5m
		ContextDirectories: make(map[string][]string), // No context directories by default
		AutoSave: AutoSaveConfig{
			Enabled:             true, // Enable by default
			SaveIntervalSeconds: 5,    // Save every 5 seconds
			MaxConcurrentSaves:  1,    // Only one save operation at a time
		},
		AutoResume: false, // Disable auto-resume by default for now
		SandboxOutputCompaction: SandboxOutputCompactionConfig{
			Enabled:              true,
			ContextWindowPercent: 0.1, // 10% of context window
			ChunkSize:            50000,
		},
		LandlockApprovals: make(map[string]*LandlockWorkspaceApprovals),
		Sandbox: SandboxConfig{
			AdditionalReadOnlyPaths:  []string{},
			AdditionalReadWritePaths: []string{},
			DisableSandbox:           false,
			BestEffort:               true, // Best-effort mode for better compatibility
		},
		Socket: SocketConfig{
			Enabled:               true,
			AutoConnect:           true,
			Path:                  "~/.scriptschnell.sock",
			Permissions:           "0600",
			RequireAuth:           false,
			AuthMethod:            "file",
			MaxConnections:        10,
			MaxSessionsPerConn:    1,
			ConnectionTimeoutSecs: 300,
			EnableBatching:        true,
			BatchSize:             10,
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
		config.TempDir = filepath.Join(os.TempDir(), "scriptschnell")
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
		config.LogPath = filepath.Join(stateDir, "scriptschnell.log")
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
	if config.ContextDirectories == nil {
		config.ContextDirectories = make(map[string][]string)
	}
	if config.LandlockApprovals == nil {
		config.LandlockApprovals = make(map[string]*LandlockWorkspaceApprovals)
	}

	return config, nil
}

// AuthorizeDomain adds a domain to the permanently authorized list
func (c *Config) AuthorizeDomain(domain string) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.AuthorizedDomains == nil {
		c.AuthorizedDomains = make(map[string]bool)
	}
	c.AuthorizedDomains[domain] = true
}

// IsDomainAuthorized checks if a domain is permanently authorized
func (c *Config) IsDomainAuthorized(domain string) bool {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.AuthorizedDomains == nil {
		return false
	}
	return c.AuthorizedDomains[domain]
}

// AuthorizeCommand adds a command prefix to the permanently authorized list
func (c *Config) AuthorizeCommand(commandPrefix string) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.AuthorizedCommands == nil {
		c.AuthorizedCommands = make(map[string]bool)
	}
	c.AuthorizedCommands[commandPrefix] = true
}

// IsCommandAuthorized checks if a command prefix is permanently authorized
func (c *Config) IsCommandAuthorized(commandPrefix string) bool {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.AuthorizedCommands == nil {
		return false
	}
	return c.AuthorizedCommands[commandPrefix]
}

// AddContextDirectory adds a directory to the context directories list for a specific workspace
// The workspace parameter should be an absolute path to the workspace directory
func (c *Config) AddContextDirectory(workspace, dir string) {
	if c.ContextDirectories == nil {
		c.ContextDirectories = make(map[string][]string)
	}

	// Resolve workspace to absolute path if it's relative
	absWorkspace := workspace
	if !filepath.IsAbs(workspace) {
		if abs, err := filepath.Abs(workspace); err == nil {
			absWorkspace = abs
		}
	}
	absWorkspace = filepath.Clean(absWorkspace)

	// Get or create the list for this workspace
	dirs := c.ContextDirectories[absWorkspace]
	// Check if already exists
	for _, existing := range dirs {
		if existing == dir {
			return
		}
	}
	c.ContextDirectories[absWorkspace] = append(dirs, dir)
}

// RemoveContextDirectory removes a directory from the context directories list for a specific workspace
// The workspace parameter should be an absolute path to the workspace directory
func (c *Config) RemoveContextDirectory(workspace, dir string) bool {
	if c.ContextDirectories == nil {
		return false
	}

	// Resolve workspace to absolute path if it's relative
	absWorkspace := workspace
	if !filepath.IsAbs(workspace) {
		if abs, err := filepath.Abs(workspace); err == nil {
			absWorkspace = abs
		}
	}
	absWorkspace = filepath.Clean(absWorkspace)

	dirs := c.ContextDirectories[absWorkspace]
	for i, existing := range dirs {
		if existing == dir {
			c.ContextDirectories[absWorkspace] = append(dirs[:i], dirs[i+1:]...)
			// If workspace has no more directories, remove the workspace key
			if len(c.ContextDirectories[absWorkspace]) == 0 {
				delete(c.ContextDirectories, absWorkspace)
			}
			return true
		}
	}
	return false
}

// GetContextDirectories returns a copy of the context directories list for a specific workspace
// The workspace parameter should be an absolute path to the workspace directory
func (c *Config) GetContextDirectories(workspace string) []string {
	if c.ContextDirectories == nil {
		return []string{}
	}

	// Resolve workspace to absolute path if it's relative
	absWorkspace := workspace
	if !filepath.IsAbs(workspace) {
		if abs, err := filepath.Abs(workspace); err == nil {
			absWorkspace = abs
		}
	}
	absWorkspace = filepath.Clean(absWorkspace)

	dirs := c.ContextDirectories[absWorkspace]
	if dirs == nil {
		return []string{}
	}
	result := make([]string, len(dirs))
	copy(result, dirs)
	return result
}

// SetOpenTabState sets the tab state for a workspace in a thread-safe manner.
// This method acquires a write lock to ensure safe concurrent access.
func (c *Config) SetOpenTabState(workspace string, tabState *WorkspaceTabState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.OpenTabs == nil {
		c.OpenTabs = make(map[string]*WorkspaceTabState)
	}
	c.OpenTabs[workspace] = tabState
}

// GetOpenTabState returns the tab state for a workspace in a thread-safe manner.
// This method acquires a read lock to ensure safe concurrent access.
func (c *Config) GetOpenTabState(workspace string) (*WorkspaceTabState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.OpenTabs == nil {
		return nil, false
	}
	state, ok := c.OpenTabs[workspace]
	return state, ok
}

// GetWorkspaceHash generates a SHA256 hash for a workspace path to use as a unique identifier.
func GetWorkspaceHash(workspace string) string {
	absWorkspace := workspace
	if !filepath.IsAbs(workspace) {
		if abs, err := filepath.Abs(workspace); err == nil {
			absWorkspace = abs
		}
	}
	absWorkspace = filepath.Clean(absWorkspace)

	hash := sha256.Sum256([]byte(absWorkspace))
	return hex.EncodeToString(hash[:])[:16] // Use first 16 chars for shorter filenames
}

// AddLandlockApproval adds an approved directory for a specific workspace.
func (c *Config) AddLandlockApproval(workspace, path, accessLevel string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.LandlockApprovals == nil {
		c.LandlockApprovals = make(map[string]*LandlockWorkspaceApprovals)
	}

	workspaceHash := GetWorkspaceHash(workspace)
	approvals := c.LandlockApprovals[workspaceHash]
	if approvals == nil {
		approvals = &LandlockWorkspaceApprovals{Directories: []LandlockApproval{}}
		c.LandlockApprovals[workspaceHash] = approvals
	}

	// Check if path already exists
	for i, dir := range approvals.Directories {
		if dir.Path == path {
			// Update access level if path exists
			approvals.Directories[i].AccessLevel = accessLevel
			return
		}
	}

	// Add new approval
	approvals.Directories = append(approvals.Directories, LandlockApproval{
		Path:        path,
		AccessLevel: accessLevel,
	})
}

// RemoveLandlockApproval removes an approved directory for a specific workspace.
func (c *Config) RemoveLandlockApproval(workspace, path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.LandlockApprovals == nil {
		return false
	}

	workspaceHash := GetWorkspaceHash(workspace)
	approvals := c.LandlockApprovals[workspaceHash]
	if approvals == nil {
		return false
	}

	for i, dir := range approvals.Directories {
		if dir.Path == path {
			approvals.Directories = append(approvals.Directories[:i], approvals.Directories[i+1:]...)
			return true
		}
	}
	return false
}

// GetLandlockApprovals returns the landlock approvals for a specific workspace.
func (c *Config) GetLandlockApprovals(workspace string) []LandlockApproval {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.LandlockApprovals == nil {
		return []LandlockApproval{}
	}

	workspaceHash := GetWorkspaceHash(workspace)
	approvals := c.LandlockApprovals[workspaceHash]
	if approvals == nil {
		return []LandlockApproval{}
	}

	result := make([]LandlockApproval, len(approvals.Directories))
	copy(result, approvals.Directories)
	return result
}

// IsLandlockApproved checks if a path is approved for a specific workspace.
func (c *Config) IsLandlockApproved(workspace, path string, accessLevel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.LandlockApprovals == nil {
		return false
	}

	workspaceHash := GetWorkspaceHash(workspace)
	approvals := c.LandlockApprovals[workspaceHash]
	if approvals == nil {
		return false
	}

	for _, dir := range approvals.Directories {
		if dir.Path == path {
			// If we need readwrite but only have read, it's not approved
			if accessLevel == "readwrite" && dir.AccessLevel == "read" {
				return false
			}
			return true
		}
	}
	return false
}

// Save saves configuration to file using atomic writes, but only if something has changed.
// This method is thread-safe and can be called concurrently from multiple goroutines.
func (c *Config) Save(path string) error {
	// Acquire read lock while marshaling to ensure consistent snapshot of config state
	c.mu.RLock()
	// Check if the file already exists and if content is unchanged
	data, err := c.marshalWithEncryptedSecrets()
	c.mu.RUnlock()
	if err != nil {
		return err
	}

	// Read existing file content for comparison
	existingData, readErr := os.ReadFile(path)
	if readErr == nil && bytes.Equal(existingData, data) {
		// Content is identical, no need to save
		return nil
	}

	// Defensive check: warn if we're about to clear search settings that exist on disk
	if readErr == nil {
		var existingConfig Config
		if jsonErr := json.Unmarshal(existingData, &existingConfig); jsonErr == nil {
			existingHasSearch := existingConfig.Search.Provider != "" ||
				existingConfig.Search.Exa.APIKey != "" ||
				existingConfig.Search.GooglePSE.APIKey != "" ||
				existingConfig.Search.Perplexity.APIKey != ""
			newHasSearch := c.Search.Provider != "" ||
				c.Search.Exa.APIKey != "" ||
				c.Search.GooglePSE.APIKey != "" ||
				c.Search.Perplexity.APIKey != ""

			if existingHasSearch && !newHasSearch {
				// Log warning about clearing search settings (this helps diagnose the issue)
				logger.Warn("Config save would clear existing search settings! Existing provider=%q, new provider=%q",
					existingConfig.Search.Provider, c.Search.Provider)
			}
		}
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Atomic write: write to temporary file then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
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
		&c.Socket.Token,
	}

	for _, srv := range c.mcpServersInOrder() {
		if srv.OpenAI != nil {
			fields = append(fields, &srv.OpenAI.APIKey)
		}
		if srv.OpenAPI != nil {
			fields = append(fields, &srv.OpenAPI.AuthBearerToken)
		}
	}

	// Track which fields were successfully decrypted
	decryptedFields := make([]*string, 0, len(fields))
	decryptedValues := make([]string, 0, len(fields))

	for _, field := range fields {
		if field == nil {
			continue
		}
		// Save original value in case decryption fails
		originalValue := *field
		if err := decryptField(field, password); err != nil {
			// Restore any fields that were already decrypted
			for i, df := range decryptedFields {
				*df = decryptedValues[i]
			}
			return err
		}
		// Track successfully decrypted field
		decryptedFields = append(decryptedFields, field)
		decryptedValues = append(decryptedValues, originalValue)
	}
	return nil
}

func (c *Config) marshalWithEncryptedSecrets() ([]byte, error) {
	copyCfg := Config{
		WorkingDir:              c.WorkingDir,
		CacheTTL:                c.CacheTTL,
		MaxCacheEntries:         c.MaxCacheEntries,
		DefaultTimeout:          c.DefaultTimeout,
		TempDir:                 c.TempDir,
		Temperature:             c.Temperature,
		MaxTokens:               c.MaxTokens,
		ProviderConfigPath:      c.ProviderConfigPath,
		DisableAnimations:       c.DisableAnimations,
		LogLevel:                c.LogLevel,
		LogPath:                 c.LogPath,
		AuthorizedDomains:       c.AuthorizedDomains,
		AuthorizedCommands:      c.AuthorizedCommands,
		Search:                  c.Search,
		MCP:                     c.MCP,
		Secrets:                 c.Secrets,
		EnablePromptCache:       c.EnablePromptCache,
		AutoSave:                c.AutoSave,
		PromptCacheTTL:          c.PromptCacheTTL,
		ContextDirectories:      c.ContextDirectories,
		OpenTabs:                c.OpenTabs,
		AutoResume:              c.AutoResume,
		SandboxOutputCompaction: c.SandboxOutputCompaction,
		LandlockApprovals:       c.LandlockApprovals,
		Socket:                  c.Socket,
		secretsPassword:         c.secretsPassword,
	}

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
	copyCfg.Socket.Token, err = encryptField(copyCfg.Socket.Token, c.secretsPassword)
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
		copyCfg.Secrets.Verifier, err = encryptField("scriptschnell", c.secretsPassword)
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
	// If value is already encrypted and password is empty, keep it as-is
	// This prevents double-encryption when password hasn't been set
	if strings.HasPrefix(value, "enc:") && password == "" {
		return value, nil
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
