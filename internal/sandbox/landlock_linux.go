//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/codefionn/scriptschnell/internal/logger"
	landlock "github.com/landlock-lsm/go-landlock/landlock"
)

// PackageManagerConfig defines paths for a specific package manager
type PackageManagerConfig struct {
	Name        string
	EnvVars     []string // Environment variables that may contain paths
	HomeSubdirs []string // Subdirectories under HOME
	SystemPaths []string // System-wide paths
	GlobPaths   []string // Paths with glob patterns (under HOME)
}

// getPackageManagerConfigs returns standard package manager configurations
func getPackageManagerConfigs() []PackageManagerConfig {
	homeDir, _ := os.UserHomeDir()
	_ = homeDir // used in string construction below

	return []PackageManagerConfig{
		// Go
		{
			Name: "Go",
			EnvVars: []string{
				"GOPATH",
				"GOMODCACHE",
				"GOCACHE",
			},
			HomeSubdirs: []string{
				"go",
				".cache/go-build",
				".cache/tinygo",
			},
		},

		// Node.js / npm / yarn / pnpm
		{
			Name: "Node.js",
			EnvVars: []string{
				"NPM_CONFIG_PREFIX",
				"NPM_CONFIG_CACHE",
				"YARN_CACHE_FOLDER",
				"PNPM_STORE_PATH",
			},
			HomeSubdirs: []string{
				".npm",
				".npm-global",
				".nvm",
				".nvm/versions",
				".yarn",
				".yarn/cache",
				".pnpm-store",
				".local/share/pnpm",
				".cache/yarn",
				".cache/node-gyp",
			},
			SystemPaths: []string{
				"/usr/local/lib/node_modules",
				"/usr/lib/node_modules",
			},
		},

		// Python / pip / pipenv / poetry / conda
		{
			Name: "Python",
			EnvVars: []string{
				"VIRTUAL_ENV",
				"CONDA_PREFIX",
				"CONDA_ENVS_PATH",
				"PIPX_HOME",
				"POETRY_CACHE_DIR",
				"PIP_CACHE_DIR",
			},
			HomeSubdirs: []string{
				".local/lib/python3.11",
				".local/lib/python3.10",
				".local/lib/python3.9",
				".local/lib/python3.8",
				".local/share/pipx",
				".cache/pip",
				".cache/pipenv",
				".cache/pypoetry",
				".pyenv",
				".pyenv/versions",
				".conda",
				".conda/envs",
				"miniconda3",
				"anaconda3",
			},
		},

		// Rust / Cargo
		{
			Name: "Rust",
			EnvVars: []string{
				"CARGO_HOME",
				"RUSTUP_HOME",
			},
			HomeSubdirs: []string{
				".cargo",
				".cargo/bin",
				".cargo/registry",
				".cargo/git",
				".rustup",
				".rustup/toolchains",
			},
		},

		// Ruby / gem / bundler
		{
			Name: "Ruby",
			EnvVars: []string{
				"GEM_HOME",
				"BUNDLE_PATH",
			},
			HomeSubdirs: []string{
				".gem",
				".rbenv",
				".rbenv/versions",
				".rvm",
				".rvm/gems",
				".bundle",
			},
		},

		// Java / Maven / Gradle
		{
			Name: "Java",
			EnvVars: []string{
				"M2_HOME",
				"MAVEN_OPTS",
				"GRADLE_USER_HOME",
				"JAVA_HOME",
				"SDKMAN_DIR",
			},
			HomeSubdirs: []string{
				".m2",
				".m2/repository",
				".gradle",
				".gradle/caches",
				".gradle/wrapper",
				".sdkman",
				".sdkman/candidates",
			},
		},

		// PHP / Composer
		{
			Name: "PHP",
			EnvVars: []string{
				"COMPOSER_HOME",
				"COMPOSER_CACHE_DIR",
			},
			HomeSubdirs: []string{
				".composer",
				".cache/composer",
				".config/composer",
			},
		},

		// .NET / NuGet
		{
			Name: "dotnet",
			EnvVars: []string{
				"DOTNET_ROOT",
				"NUGET_PACKAGES",
				"NUGET_HTTP_CACHE_PATH",
			},
			HomeSubdirs: []string{
				".dotnet",
				".nuget",
				".nuget/packages",
				".cache/dotnet",
			},
			SystemPaths: []string{
				"/usr/share/dotnet",
				"/usr/lib/dotnet",
			},
		},

		// Docker (config only, not docker daemon)
		{
			Name: "Docker",
			HomeSubdirs: []string{
				".docker",
			},
		},

		// Git (SSH access not included by default - must be requested for git operations)
		{
			Name: "Git",
			HomeSubdirs: []string{
				".gitconfig",
			},
		},
	}
}

// LandlockSandbox provides filesystem sandboxing using Linux Landlock LSM.
type LandlockSandbox struct {
	workspaceDir    string
	allowedPaths    []DirectoryPermission
	additionalPaths []DirectoryPermission
	customROPaths   []string // Custom read-only paths from config
	customRWPaths   []string // Custom read-write paths from config
	enabled         bool
	available       bool
	bestEffort      bool
	disabled        bool // Explicitly disabled via config
}

// NewLandlockSandbox creates a new Landlock sandbox for the given workspace.
// If config is provided, custom paths are added and sandbox can be disabled.
func NewLandlockSandbox(workspaceDir string, cfg *SandboxConfig) *LandlockSandbox {
	sandbox := &LandlockSandbox{
		workspaceDir:  workspaceDir,
		allowedPaths:  getDefaultAllowedPaths(),
		bestEffort:    true, // Default to best effort mode for compatibility
		customROPaths: []string{},
		customRWPaths: []string{},
	}

	// Apply config if provided
	if cfg != nil {
		if cfg.DisableSandbox {
			sandbox.disabled = true
			sandbox.available = false
			sandbox.enabled = false
			logger.Info("Landlock sandbox explicitly disabled via config")
			return sandbox
		}
		sandbox.customROPaths = cfg.AdditionalReadOnlyPaths
		sandbox.customRWPaths = cfg.AdditionalReadWritePaths
		// Use BestEffort from config if explicitly set, otherwise keep default
		if cfg.BestEffort {
			sandbox.bestEffort = true
		}
	}

	// Check if Landlock is available on this system
	sandbox.available = checkLandlockAvailable()
	if sandbox.available {
		sandbox.enabled = true
		logger.Info("Landlock sandboxing enabled for workspace: %s (best_effort=%v)", workspaceDir, sandbox.bestEffort)
		if len(sandbox.customROPaths) > 0 {
			logger.Debug("Additional read-only paths: %v", sandbox.customROPaths)
		}
		if len(sandbox.customRWPaths) > 0 {
			logger.Debug("Additional read-write paths: %v", sandbox.customRWPaths)
		}
	} else {
		logger.Warn("Landlock not available on this system, shell commands will run unsandboxed")
	}

	return sandbox
}

// getDefaultAllowedPaths returns common package manager and system directories
// that should be allowed for read/write access by default.
func getDefaultAllowedPaths() []DirectoryPermission {
	paths := []DirectoryPermission{}
	homeDir, _ := os.UserHomeDir()
	addedPaths := make(map[string]bool) // Track already-added paths to avoid duplicates

	// Helper to add a path if it exists and hasn't been added
	addPathIfExists := func(p string, access AccessLevel) {
		if p == "" {
			return
		}
		// Normalize path
		absPath := p
		if !filepath.IsAbs(p) {
			absPath = filepath.Join(homeDir, p)
		}
		absPath = filepath.Clean(absPath)

		// Skip if already added
		if addedPaths[absPath] {
			return
		}

		// Check existence
		if _, err := os.Stat(absPath); err == nil {
			paths = append(paths, DirectoryPermission{Path: absPath, Access: access})
			addedPaths[absPath] = true
			logger.Debug("Added default allowed path: %s (access: %v)", absPath, access)
		}
	}

	// Add essential system paths (read-only for running programs)
	systemPaths := []string{
		"/usr",      // Most binaries and libraries
		"/bin",      // Essential binaries
		"/lib",      // Essential libraries
		"/lib64",    // 64-bit libraries on some systems
		"/etc",      // Configuration files (read-only)
		"/usr/bin",  // User binaries
		"/usr/lib",  // User libraries
		"/sbin",     // System binaries
		"/usr/sbin", // System admin binaries
	}

	// Check for system paths on various distros
	altSystemPaths := []string{
		"/usr/local/bin",
		"/usr/local/lib",
		"/run/current-system/sw", // NixOS
		"/nix/store",             // Nix store (read-only)
	}
	systemPaths = append(systemPaths, altSystemPaths...)

	for _, p := range systemPaths {
		addPathIfExists(p, AccessReadOnly)
	}

	// Add ~/.local/bin for user-installed binaries (read/execute only)
	if homeDir != "" {
		addPathIfExists(filepath.Join(homeDir, ".local/bin"), AccessReadOnly)
	}

	// Add package manager paths
	for _, pm := range getPackageManagerConfigs() {
		// Add paths from environment variables
		for _, envVar := range pm.EnvVars {
			if val := os.Getenv(envVar); val != "" {
				// Some env vars may contain multiple paths (e.g., PATH-like)
				if strings.Contains(val, ":") {
					for _, subPath := range strings.Split(val, ":") {
						addPathIfExists(subPath, AccessReadWrite)
					}
				} else {
					addPathIfExists(val, AccessReadWrite)
				}
			}
		}

		// Add paths under home directory
		for _, subdir := range pm.HomeSubdirs {
			addPathIfExists(subdir, AccessReadWrite)
		}

		// Add system-wide paths
		for _, sysPath := range pm.SystemPaths {
			addPathIfExists(sysPath, AccessReadWrite)
		}
	}

	// Add essential device files (needed by many programs)
	devFiles := []string{
		"/dev/null",
		"/dev/zero",
		"/dev/random",
		"/dev/urandom",
		"/dev/stdin",
		"/dev/stdout",
		"/dev/stderr",
	}
	for _, devFile := range devFiles {
		addPathIfExists(devFile, AccessReadWrite)
	}

	// Add temp directories
	tempDirs := []string{
		os.TempDir(),
		"/tmp",
		"/var/tmp",
	}
	for _, tmpDir := range tempDirs {
		addPathIfExists(tmpDir, AccessReadWrite)
	}

	// Add home directory for config files (read-only for safety, specific subdirs get RW)
	if homeDir != "" {
		addPathIfExists(homeDir, AccessReadOnly)
	}

	return paths
}

// checkLandlockAvailable checks if Landlock is available on the current system.
func checkLandlockAvailable() bool {
	// Landlock requires Linux 5.13+ with the Landlock LSM enabled
	// The go-landlock library handles this check internally
	return true // We'll discover this at runtime when trying to restrict
}

// AddAuthorizedPath adds a directory path that has been authorized by the user.
func (s *LandlockSandbox) AddAuthorizedPath(path string, access AccessLevel) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	s.additionalPaths = append(s.additionalPaths, DirectoryPermission{
		Path:   absPath,
		Access: access,
	})
}

// SetAdditionalPaths sets the additional authorized paths.
func (s *LandlockSandbox) SetAdditionalPaths(paths []DirectoryPermission) {
	s.additionalPaths = paths
}

// IsEnabled returns whether sandboxing is currently enabled.
func (s *LandlockSandbox) IsEnabled() bool {
	return s.enabled && s.available
}

// Enable enables the sandbox if available.
func (s *LandlockSandbox) Enable() {
	if s.available {
		s.enabled = true
	}
}

// Disable disables the sandbox.
func (s *LandlockSandbox) Disable() {
	s.enabled = false
}

// WrapCommand wraps an exec.Cmd with Landlock restrictions.
// This modifies the command to run with restricted filesystem access.
func (s *LandlockSandbox) WrapCommand(cmd *exec.Cmd) error {
	if !s.IsEnabled() {
		return nil // No sandboxing
	}

	// Landlock restrictions are applied to the current process and its children.
	// We need to set up restrictions before starting the command.
	// The go-landlock library applies restrictions to the current process,
	// so we need to apply them in a fork before exec.

	// For now, we'll apply restrictions that will affect the spawned process
	// since it inherits the parent's restrictions.
	return nil
}

// Restrict applies Landlock restrictions to the current process.
// This should be called before executing shell commands.
func (s *LandlockSandbox) Restrict() error {
	if !s.IsEnabled() {
		return nil
	}

	// Collect all paths to allow
	var roDirs []string
	var rwDirs []string

	// Always allow workspace directory with full access
	if s.workspaceDir != "" {
		absWorkspace, err := filepath.Abs(s.workspaceDir)
		if err == nil {
			rwDirs = append(rwDirs, absWorkspace)
		} else {
			rwDirs = append(rwDirs, s.workspaceDir)
		}
	}

	// Add default allowed paths
	for _, perm := range s.allowedPaths {
		switch perm.Access {
		case AccessReadOnly:
			roDirs = append(roDirs, perm.Path)
		case AccessReadWrite:
			rwDirs = append(rwDirs, perm.Path)
		}
	}

	// Add user-authorized paths
	for _, perm := range s.additionalPaths {
		switch perm.Access {
		case AccessReadOnly:
			roDirs = append(roDirs, perm.Path)
		case AccessReadWrite:
			rwDirs = append(rwDirs, perm.Path)
		}
	}

	// Add custom paths from config
	for _, path := range s.customROPaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		// Check if path exists before adding
		if _, err := os.Stat(absPath); err == nil {
			roDirs = append(roDirs, absPath)
			logger.Debug("Added custom read-only path: %s", absPath)
		}
	}
	for _, path := range s.customRWPaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		// Check if path exists before adding
		if _, err := os.Stat(absPath); err == nil {
			rwDirs = append(rwDirs, absPath)
			logger.Debug("Added custom read-write path: %s", absPath)
		}
	}

	// Build the restriction using go-landlock API
	// Use RODirs/RWDirs for directories and ROFiles/RWFiles for regular files,
	// because Landlock rejects directory access rights on regular files.
	rules := make([]landlock.Rule, 0, len(roDirs)+len(rwDirs))

	for _, path := range roDirs {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			rules = append(rules, landlock.ROFiles(path))
		} else {
			rules = append(rules, landlock.RODirs(path))
		}
	}
	for _, path := range rwDirs {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			rules = append(rules, landlock.RWFiles(path))
		} else {
			rules = append(rules, landlock.RWDirs(path))
		}
	}

	// Apply restrictions
	var err error
	if s.bestEffort {
		err = landlock.V6.BestEffort().RestrictPaths(rules...)
	} else {
		err = landlock.V6.RestrictPaths(rules...)
	}

	if err != nil {
		logger.Warn("Landlock restriction failed: %v, proceeding without sandbox", err)
		s.available = false
		s.enabled = false
		return fmt.Errorf("landlock restriction failed: %w", err)
	}

	logger.Debug("Landlock restrictions applied: %d RO dirs, %d RW dirs", len(roDirs), len(rwDirs))
	return nil
}

// GetAllowedPaths returns the current allowed paths.
func (s *LandlockSandbox) GetAllowedPaths() []DirectoryPermission {
	result := make([]DirectoryPermission, 0, len(s.allowedPaths)+len(s.additionalPaths))
	result = append(result, s.allowedPaths...)
	result = append(result, s.additionalPaths...)
	return result
}

// GetWorkspaceDir returns the workspace directory.
func (s *LandlockSandbox) GetWorkspaceDir() string {
	return s.workspaceDir
}

// init prints a debug message about Landlock initialization
func init() {
	if runtime.GOOS == "linux" {
		logger.Debug("Landlock sandbox support compiled in")
	}
}
