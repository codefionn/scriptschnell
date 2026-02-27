package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/acp"
	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/cli"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/lockfile"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/pidfile"
	"github.com/codefionn/scriptschnell/internal/pprof"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/secrets"
	"github.com/codefionn/scriptschnell/internal/securemem"
	"github.com/codefionn/scriptschnell/internal/socketserver"
	"github.com/codefionn/scriptschnell/internal/tui"
	"github.com/codefionn/scriptschnell/internal/web"
	"golang.org/x/term"
)

var (
	ErrQuitRequested = errors.New("quit requested")
	errACPMode       = errors.New("ACP mode requested")
	errWebMode       = errors.New("web mode requested")
	errSocketMode    = errors.New("socket server mode requested")
)

type stringSlice []string

const maxPasswordAttempts = 3

func (s *stringSlice) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *stringSlice) Set(value string) error {
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*s = append(*s, value)
	return nil
}

func (s stringSlice) toStrings() []string {
	if len(s) == 0 {
		return nil
	}
	return append([]string(nil), s...)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	prompt, cliOptions, cliMode, pprofCfg, webMode, webDebug, socketServerMode, socketPath, requireSandboxAuth, parseErr := parseCLIArgs(os.Args[1:])
	if parseErr != nil {
		if errors.Is(parseErr, flag.ErrHelp) {
			return nil
		}
		if errors.Is(parseErr, errACPMode) {
			// Handle ACP mode separately
			return runACPMode()
		}
		if errors.Is(parseErr, errSocketMode) {
			// Handle socket server mode separately (after config is loaded)
			// Fall through to config loading
		} else if errors.Is(parseErr, errWebMode) {
			// Handle web mode separately (after config is loaded)
			// Fall through to config loading
		} else if parseErr != nil {
			return parseErr
		}
	}

	// Start pprof handler if configured
	var pprofHandler *pprof.Handler
	if pprofCfg != nil && (pprofCfg.HTTPAddr != "" || pprofCfg.CPUProfile != "" || pprofCfg.HeapProfile != "" ||
		pprofCfg.GoroutineProfile != "" || pprofCfg.BlockProfile != "" || pprofCfg.MutexProfile != "" || pprofCfg.TraceProfile != "") {
		pprofHandler = pprof.NewHandler(*pprofCfg)
		if err := pprofHandler.Start(); err != nil {
			return fmt.Errorf("failed to start pprof: %w", err)
		}
		defer func() {
			if stopErr := pprofHandler.Stop(); stopErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to stop pprof: %v\n", stopErr)
			}
		}()
		fmt.Fprintf(os.Stderr, "Profiling enabled (HTTP: %s, CPU: %s, Heap: %s, Goroutine: %s, Block: %s, Mutex: %s, Trace: %s)\n",
			pprofCfg.HTTPAddr, pprofCfg.CPUProfile, pprofCfg.HeapProfile, pprofCfg.GoroutineProfile,
			pprofCfg.BlockProfile, pprofCfg.MutexProfile, pprofCfg.TraceProfile)
	}

	var loggerInitialized bool
	defer func() {
		if !loggerInitialized {
			return
		}
		if err != nil {
			logger.Error("Fatal error: %v", err)
		}
		if closeErr := logger.Global().Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close logger: %v\n", closeErr)
		}
		// Securely clean up all sensitive memory before exit
		securemem.Cleanup()
	}()

	configPath := config.GetConfigPath()
	envLogLevel := strings.TrimSpace(os.Getenv("SCRIPTSCHNELL_LOG_LEVEL"))
	envLogPath := strings.TrimSpace(os.Getenv("SCRIPTSCHNELL_LOG_PATH"))

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Allow environment variables to override config file values for logging.
	if envLogLevel != "" {
		cfg.LogLevel = envLogLevel
	}
	if envLogPath != "" {
		cfg.LogPath = envLogPath
	}

	// Get secrets password in secure memory
	secretsPassword, err := ensureSecretsPasswordSecure(cfg)
	if err != nil {
		return fmt.Errorf("failed to unlock API keys: %w", err)
	}
	defer secretsPassword.Destroy()

	// Initialize logger
	logLevel := logger.ParseLevel(cfg.LogLevel)

	// Enable console logging for socket server mode to allow users to see logs
	// when running the server interactively (can be disabled via config)
	enableConsole := cfg.LogToConsole || socketServerMode
	if initErr := logger.InitWithConsole(logLevel, cfg.LogPath, enableConsole); initErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", initErr)
	}
	loggerInitialized = true

	logger.Info("scriptschnell starting")
	logger.Debug("Configuration loaded: path=%s working_dir=%s log_level=%s log_path=%s env_log_level=%q env_log_path=%q",
		configPath, cfg.WorkingDir, cfg.LogLevel, cfg.LogPath, envLogLevel, envLogPath)

	// Ensure temp directory exists
	if err := os.MkdirAll(cfg.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Load provider manager
	providerMgr, err := provider.NewManagerSecure(cfg.ProviderConfigPath, secretsPassword)
	if err != nil {
		return fmt.Errorf("failed to initialize provider manager: %w", err)
	}

	// Refresh models from APIs - use synchronous version in CLI mode to ensure models are available
	// before orchestrator creation, use async in TUI mode for better startup performance
	ctx := context.Background()
	logger.Debug("Refreshing models from provider APIs")
	if cliMode {
		// Synchronous refresh for CLI mode to ensure models are loaded before use
		if err := providerMgr.RefreshAllModelsSync(ctx); err != nil {
			// Non-fatal, log warning and continue
			logger.Warn("Failed to refresh models: %v", err)
		}
	} else if webMode {
		// Async refresh for web mode
		providerMgr.RefreshAllModels(ctx)
	} else if socketServerMode {
		// Async refresh for socket server mode
		providerMgr.RefreshAllModels(ctx)
	} else {
		// Async refresh for TUI mode
		providerMgr.RefreshAllModels(ctx)
	}

	// Check for socket server mode after config is loaded
	if socketServerMode {
		return runSocketServer(cfg, providerMgr, secretsPassword, socketPath)
	}

	// Check for web mode flag after config is loaded
	if webMode {
		return runWeb(cfg, providerMgr, secretsPassword, webDebug, requireSandboxAuth)
	}

	if cliMode {
		return runCLI(cfg, providerMgr, prompt, cliOptions)
	}

	// Run TUI mode
	return runTUI(cfg, providerMgr, cliOptions)
}

func runCLI(cfg *config.Config, providerMgr *provider.Manager, prompt string, options *cli.Options) error {
	logger.Info("Running in CLI mode with prompt: %s", prompt)
	if options != nil {
		logger.Debug("CLI pre-authorization: danger=%v allow_network=%v dirs=%v files=%v domains=%v",
			options.DangerouslyAllowAll,
			options.AllowAllNetwork,
			options.AllowedDirs,
			options.AllowedFiles,
			options.AllowedDomains,
		)
	}

	// Check if socket client mode is explicitly enabled or auto-detected
	useSocketMode := false
	if options != nil && options.SocketClientMode {
		// Explicitly enabled via --connect-to-socket flag
		useSocketMode = true
		logger.Info("Socket mode explicitly enabled via --connect-to-socket flag")
		fmt.Fprintln(os.Stderr, "Connecting to socket server...")
	} else if cli.ShouldUseSocketMode(cfg, options) {
		// Auto-detected socket server
		useSocketMode = true
		// Enable socket client mode for the options
		if options != nil {
			options.SocketClientMode = true
		}
		logger.Info("Socket server auto-detected, using socket mode")
		fmt.Fprintln(os.Stderr, "Socket server detected, connecting...")
	} else {
		// Log why socket mode is not being used
		if options != nil && options.NoSocket {
			logger.Info("Socket mode disabled via --no-socket flag, using local mode")
			fmt.Fprintln(os.Stderr, "Socket auto-detection disabled, using local mode")
		} else if !cfg.Socket.Enabled {
			logger.Info("Socket mode disabled in config, using local mode")
		} else if !cfg.Socket.AutoConnect {
			logger.Info("Socket auto-connect disabled in config, using local mode")
		} else {
			logger.Info("No socket server detected, using local mode")
			fmt.Fprintln(os.Stderr, "No socket server detected, using local mode")
		}
	}

	if useSocketMode {
		return runCLISocket(cfg, options, prompt)
	}

	cliRunner, err := cli.New(cfg, providerMgr, options)
	if err != nil {
		logger.Error("Failed to create CLI runner: %v", err)
		return fmt.Errorf("failed to create CLI runner: %w", err)
	}
	defer func() {
		if closeErr := cliRunner.Close(); closeErr != nil {
			logger.Warn("Failed to close CLI runner cleanly: %v", closeErr)
			fmt.Fprintf(os.Stderr, "Warning: failed to close CLI runner cleanly: %v\n", closeErr)
		}
	}()

	return cliRunner.Run(context.Background(), prompt)
}

// runCLISocket runs CLI mode using the socket server
func runCLISocket(cfg *config.Config, options *cli.Options, prompt string) error {
	logger.Info("Running CLI mode with socket server at: %s", options.SocketClientPath)

	socketRunner, err := cli.NewSocket(cfg, options)
	if err != nil {
		logger.Error("Failed to create socket CLI runner: %v", err)
		return fmt.Errorf("failed to create socket CLI runner: %w", err)
	}
	defer func() {
		if closeErr := socketRunner.Close(); closeErr != nil {
			logger.Warn("Failed to close socket CLI runner cleanly: %v", closeErr)
			fmt.Fprintf(os.Stderr, "Warning: failed to close socket CLI runner cleanly: %v\n", closeErr)
		}
	}()

	// Connect to socket server
	ctx := context.Background()
	if err := socketRunner.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to socket server: %w", err)
	}

	// Run the prompt
	return socketRunner.Run(ctx, prompt)
}

func ensureSecretsPassword(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", errors.New("config is nil")
	}

	if cfg.Secrets.PasswordSet {
		for attempt := 0; attempt < maxPasswordAttempts; attempt++ {
			pw, err := promptForPassword("Enter encryption password: ")
			if err != nil {
				return "", err
			}
			if err := cfg.ApplySecretsPassword(pw); err != nil {
				if errors.Is(err, secrets.ErrInvalidPassword) {
					fmt.Fprintln(os.Stderr, "Invalid password, try again.")
					continue
				}
				return "", err
			}
			return cfg.SecretsPassword(), nil
		}
		return "", errors.New("too many invalid password attempts")
	}

	if err := cfg.ApplySecretsPassword(""); err != nil {
		return "", err
	}
	return cfg.SecretsPassword(), nil
}

// ensureSecretsPasswordSecure returns the secrets password stored in secure memory
func ensureSecretsPasswordSecure(cfg *config.Config) (*securemem.String, error) {
	password, err := ensureSecretsPassword(cfg)
	if err != nil {
		return nil, err
	}
	// Store password in secure memory
	secPassword := securemem.NewString(password)
	// Wipe the plaintext password
	securemem.SecureWipeString(&password)
	return secPassword, nil
}

func promptForPassword(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	fmt.Fprint(os.Stderr, prompt)

	if term.IsTerminal(fd) {
		bytes, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(bytes)), nil
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func runACPMode() error {
	fmt.Fprintf(os.Stderr, "Starting scriptschnell in Agent Client Protocol (ACP) mode...\n")

	// Load configuration
	cfg, err := config.Load(config.GetConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override logging from environment if set
	if envLevel := strings.TrimSpace(os.Getenv("SCRIPTSCHNELL_LOG_LEVEL")); envLevel != "" {
		cfg.LogLevel = envLevel
	}
	if envPath := strings.TrimSpace(os.Getenv("SCRIPTSCHNELL_LOG_PATH")); envPath != "" {
		cfg.LogPath = envPath
	}

	// Initialize logger
	logLevel := logger.ParseLevel(cfg.LogLevel)
	if initErr := logger.Init(logLevel, cfg.LogPath); initErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", initErr)
	}
	defer func() {
		if closeErr := logger.Global().Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close logger: %v\n", closeErr)
		}
		// Securely clean up all sensitive memory before exit
		securemem.Cleanup()
	}()

	logger.Info("scriptschnell starting in ACP mode")

	// Ensure temp directory exists
	if err := os.MkdirAll(cfg.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Load provider manager (without password for ACP mode)
	secretsPassword, err := ensureSecretsPasswordSecure(cfg)
	if err != nil {
		return fmt.Errorf("failed to unlock API keys: %w", err)
	}
	defer secretsPassword.Destroy()

	providerMgr, err := provider.NewManagerSecure(cfg.ProviderConfigPath, secretsPassword)
	if err != nil {
		return fmt.Errorf("failed to initialize provider manager: %w", err)
	}

	// Refresh models from APIs
	ctx := context.Background()
	providerMgr.RefreshAllModels(ctx)

	// Run the ACP agent
	return acp.RunACPAgent(ctx, cfg, providerMgr)
}

func runWeb(cfg *config.Config, providerMgr *provider.Manager, secretsPassword *securemem.String, webDebug bool, requireSandboxAuth bool) error {
	fmt.Fprintf(os.Stderr, "Starting scriptschnell in web mode...\n")

	// Initialize logger if not already initialized
	if logger.Global() == nil {
		logLevel := logger.ParseLevel(cfg.LogLevel)
		if initErr := logger.Init(logLevel, cfg.LogPath); initErr != nil {
			return fmt.Errorf("failed to initialize logger: %w", initErr)
		}
		defer func() {
			if closeErr := logger.Global().Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to close logger: %v\n", closeErr)
			}
		}()
	}

	logger.Info("scriptschnell starting in web mode")

	// Ensure temp directory exists
	if err := os.MkdirAll(cfg.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Refresh models from APIs
	ctx := context.Background()
	providerMgr.RefreshAllModels(ctx)

	// Create and start web server
	srv, err := web.NewServer(ctx, cfg, providerMgr, secretsPassword, webDebug, requireSandboxAuth)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}

	// Start server in background
	if err := srv.Start(); err != nil {
		return fmt.Errorf("failed to start web server: %w", err)
	}

	// Get URL with auth token
	url := srv.GetURL()

	// Print URL to console
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintf(os.Stderr, "  Web UI available at:\n")
	fmt.Fprintf(os.Stderr, "  %s\n", url)
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr)

	// Open browser automatically
	if err := srv.OpenBrowser(); err != nil {
		logger.Warn("Failed to open browser: %v", err)
		fmt.Fprintf(os.Stderr, "Could not open browser automatically. Please visit the URL above.\n")
	}

	// Wait for shutdown signal
	logger.Info("Web server started, waiting for shutdown signal")

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("Shutdown signal received")

	// Stop server
	if err := srv.Stop(); err != nil {
		logger.Error("Error stopping server: %v", err)
		return err
	}

	logger.Info("Web server stopped")
	return nil
}

func runSocketServer(cfg *config.Config, providerMgr *provider.Manager, secretsPassword *securemem.String, socketPath string) error {
	fmt.Fprintf(os.Stderr, "Starting scriptschnell in socket server mode...\n")

	// Enable console logging by default for socket server mode
	// This allows users to see logs when running the server interactively
	if !cfg.LogToConsole {
		cfg.LogToConsole = true
	}

	// Initialize logger if not already initialized
	if logger.Global() == nil {
		logLevel := logger.ParseLevel(cfg.LogLevel)
		// Enable console logging in socket server mode
		if initErr := logger.InitWithConsole(logLevel, cfg.LogPath, cfg.LogToConsole); initErr != nil {
			return fmt.Errorf("failed to initialize logger: %w", initErr)
		}
		defer func() {
			if closeErr := logger.Global().Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to close logger: %v\n", closeErr)
			}
		}()
	}

	logger.Info("scriptschnell starting in socket server mode")

	// Ensure temp directory exists
	if err := os.MkdirAll(cfg.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Refresh models from APIs
	ctx := context.Background()
	providerMgr.RefreshAllModels(ctx)

	// Override socket path if specified via CLI
	if socketPath != "" {
		// Expand ~ to home directory
		expandedPath := os.ExpandEnv(socketPath)
		if strings.HasPrefix(expandedPath, "~/") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			expandedPath = filepath.Join(homeDir, expandedPath[2:])
		}
		cfg.Socket.Path = expandedPath
		logger.Info("Socket path overridden to: %s", expandedPath)
	}

	// Create lockfile for single instance enforcement
	lockfilePath := filepath.Join(filepath.Dir(cfg.Socket.GetSocketPath()), ".scriptschnell-server.lock")
	lf := lockfile.New(lockfilePath)
	if err := lf.TryAcquire(); err != nil {
		if errors.Is(err, lockfile.ErrLocked) {
			return fmt.Errorf("another socket server instance is already running: %w", err)
		}
		return fmt.Errorf("failed to acquire lockfile: %w", err)
	}
	logger.Info("Lockfile acquired: %s", lockfilePath)

	// Create PID file
	pidfilePath := filepath.Join(filepath.Dir(cfg.Socket.GetSocketPath()), ".scriptschnell-server.pid")
	pf := pidfile.New(pidfilePath)
	if err := pf.Write(); err != nil {
		// Clean up lockfile before returning error
		lf.Release()
		return fmt.Errorf("failed to write pidfile: %w", err)
	}
	logger.Info("PID file written: %s", pidfilePath)

	// Track if cleanup was done to avoid double cleanup in defers
	cleanupDone := false
	defer func() {
		if !cleanupDone {
			if releaseErr := lf.Release(); releaseErr != nil {
				logger.Warn("Failed to release lockfile: %v", releaseErr)
			}
			if removeErr := pf.Remove(); removeErr != nil {
				logger.Warn("Failed to remove pidfile: %v", removeErr)
			}
		}
	}()

	// Create socket server
	srv, err := socketserver.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("failed to create socket server: %w", err)
	}

	// Set broker dependencies (provider manager, secrets password, config)
	// This is needed for the broker to create orchestrators
	srv.SetDependencies(providerMgr, secretsPassword)

	// Start server
	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("failed to start socket server: %w", err)
	}

	// Get socket path
	socketPath = cfg.Socket.GetSocketPath()

	// Print socket path to console
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintf(os.Stderr, "  Unix socket server listening at:\n")
	fmt.Fprintf(os.Stderr, "  %s\n", socketPath)
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "PID: %d\n", os.Getpid())
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Use 'scriptschnell --socket-path', or any frontend")
	fmt.Fprintln(os.Stderr, "to connect to this server.")
	fmt.Fprintln(os.Stderr)

	// Wait for shutdown signal
	logger.Info("Socket server started, waiting for shutdown signal")

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("Shutdown signal received")

	// Stop server (this also removes the socket file)
	if err := srv.Stop(); err != nil {
		logger.Error("Error stopping socket server: %v", err)
	}

	// Explicitly clean up lockfile and pidfile before returning
	// (defers may not run if process exits too quickly)
	cleanupDone = true

	if err := lf.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to release lockfile: %v\n", err)
	} else {
		logger.Info("Lockfile released: %s", lockfilePath)
	}

	if err := pf.Remove(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove pidfile: %v\n", err)
	} else {
		logger.Info("PID file removed: %s", pidfilePath)
	}

	logger.Info("Socket server stopped")
	return nil
}

func parseCLIArgs(args []string) (string, *cli.Options, bool, *pprof.Config, bool, bool, bool, string, bool, error) {
	fs := flag.NewFlagSet("scriptschnell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		dangerous          bool
		allowNetwork       bool
		allowDirs          stringSlice
		allowFiles         stringSlice
		allowDomains       stringSlice
		requireSandboxAuth bool
		showHelp           bool
		model              string
		provider           string
		acpMode            bool
		webMode            bool
		webDebug           bool
		socketServerMode   bool
		socketPath         string
		socketClientMode   bool
		socketClientPath   string
		noSocket           bool
		jsonOutput         bool
		jsonExtended       bool
		jsonFull           bool

		// pprof flags
		pprofAddr                 string
		pprofCPU                  string
		pprofHeap                 string
		pprofGoroutine            string
		pprofBlock                string
		pprofMutex                string
		pprofTrace                string
		pprofBlockProfileRate     int
		pprofMutexProfileFraction int
	)

	fs.BoolVar(&dangerous, "dangerous-allow-all", false, "Bypass all authorization checks (dangerous)")
	fs.BoolVar(&allowNetwork, "allow-network", false, "Allow network access to all domains")
	fs.Var(&allowDirs, "allow-dir", "Pre-authorize a directory for write operations (repeatable)")
	fs.Var(&allowFiles, "allow-file", "Pre-authorize a specific file for write operations (repeatable)")
	fs.Var(&allowDomains, "allow-domain", "Pre-authorize network access to a domain (repeatable)")
	fs.BoolVar(&requireSandboxAuth, "require-sandbox-auth", false, "Require authorization for every go_sandbox and shell call")
	fs.StringVar(&model, "model", "", "Model to use (e.g., gpt-5, claude-sonnet-4.5, gemini-2.5-pro)")
	fs.StringVar(&provider, "provider", "", "Provider name (e.g., openai, anthropic, google)")
	fs.BoolVar(&acpMode, "acp", false, "Run in Agent Client Protocol (ACP) mode for integration with code editors")
	fs.BoolVar(&webMode, "web", false, "Run in web mode with browser-based UI")
	fs.BoolVar(&webDebug, "web-debug", false, "Enable debug logging for web server and WebSocket connections")
	fs.BoolVar(&socketServerMode, "socket-server", false, "Run in Unix socket server mode for daemon operation")
	fs.StringVar(&socketPath, "socket-path", "", "Path to Unix socket file (default: ~/.scriptschnell.sock)")
	fs.BoolVar(&socketClientMode, "connect-to-socket", false, "Force connection to socket server (fails if not running)")
	fs.StringVar(&socketClientPath, "socket-client-path", "", "Path to Unix socket file for CLI mode (default: ~/.scriptschnell.sock)")
	fs.BoolVar(&noSocket, "no-socket", false, "Disable socket auto-detection, always use local mode")
	fs.BoolVar(&jsonOutput, "json", false, "Output final assistant message and usage as JSON")
	fs.BoolVar(&jsonExtended, "json-extended", false, "Output all messages as JSON one-liners plus usage statistics")
	fs.BoolVar(&jsonFull, "json-full", false, "Output all messages with full tool call outputs as single JSON object")
	fs.BoolVar(&showHelp, "help", false, "Show CLI usage information")

	// pprof flags
	fs.StringVar(&pprofAddr, "pprof.addr", "", "Enable pprof HTTP server on specified address (e.g., :6060)")
	fs.StringVar(&pprofCPU, "pprof.cpu", "", "Path to write CPU profile file (e.g., cpu.prof)")
	fs.StringVar(&pprofHeap, "pprof.heap", "", "Path to write heap profile file (e.g., heap.prof)")
	fs.StringVar(&pprofGoroutine, "pprof.goroutine", "", "Path to write goroutine profile file (e.g., goroutine.prof)")
	fs.StringVar(&pprofBlock, "pprof.block", "", "Path to write blocking profile file (e.g., block.prof)")
	fs.StringVar(&pprofMutex, "pprof.mutex", "", "Path to write mutex profile file (e.g., mutex.prof)")
	fs.StringVar(&pprofTrace, "pprof.trace", "", "Path to write execution trace file (e.g., trace.out)")
	fs.IntVar(&pprofBlockProfileRate, "pprof.block-rate", 1, "Blocking profile sampling rate (1/n events, default: 1)")
	fs.IntVar(&pprofMutexProfileFraction, "pprof.mutex-fraction", 1, "Mutex profile sampling fraction (1/n events, default: 1)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [options] \"your prompt here\"\n\n", os.Args[0])
		fmt.Fprintln(fs.Output(), "CLI mode automatically connects to a running socket server if one is detected.")
		fmt.Fprintln(fs.Output(), "Use --no-socket to disable auto-detection and run locally.")
		fmt.Fprintln(fs.Output(), "Use --connect-to-socket to force socket mode (fails if server is not running).")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", nil, false, nil, false, false, false, "", false, flag.ErrHelp
		}
		return "", nil, false, nil, false, false, false, "", false, err
	}

	if showHelp {
		fs.Usage()
		return "", nil, false, nil, false, false, false, "", false, flag.ErrHelp
	}

	remaining := fs.Args()
	optionsUsed := dangerous || allowNetwork || len(allowDirs) > 0 || len(allowFiles) > 0 || len(allowDomains) > 0

	// Build pprof config (used across modes)
	pprofCfg := &pprof.Config{
		HTTPAddr:             pprofAddr,
		CPUProfile:           pprofCPU,
		HeapProfile:          pprofHeap,
		GoroutineProfile:     pprofGoroutine,
		BlockProfile:         pprofBlock,
		MutexProfile:         pprofMutex,
		TraceProfile:         pprofTrace,
		BlockProfileRate:     pprofBlockProfileRate,
		MutexProfileFraction: pprofMutexProfileFraction,
	}

	// Handle ACP mode
	if acpMode {
		if len(remaining) > 0 {
			return "", nil, false, nil, false, false, false, "", false, nil
		}
		if optionsUsed {
			return "", nil, false, nil, false, false, false, "", false, nil
		}
		// Return special values to indicate ACP mode
		return "", nil, false, nil, false, false, false, "", false, errACPMode
	}

	// Handle socket server mode
	if socketServerMode {
		if len(remaining) > 0 {
			return "", nil, false, nil, false, false, false, "", false, flag.ErrHelp
		}
		if optionsUsed {
			return "", nil, false, nil, false, false, false, "", false, flag.ErrHelp
		}
		// Return special values to indicate socket server mode
		return "", nil, false, pprofCfg, false, false, true, socketPath, false, errSocketMode
	}

	// Handle web mode
	if webMode {
		if len(remaining) > 0 {
			return "", nil, false, nil, false, false, false, "", false, flag.ErrHelp
		}
		if optionsUsed {
			return "", nil, false, nil, false, false, false, "", false, flag.ErrHelp
		}
		// Return special values to indicate web mode (pass requireSandboxAuth)
		return "", nil, false, pprofCfg, true, webDebug, false, "", requireSandboxAuth, nil
	}

	if len(remaining) == 0 {
		// TUI mode - return options with RequireSandboxAuth and socket flags
		opts := &cli.Options{
			RequireSandboxAuth: requireSandboxAuth,
			SocketClientMode:   socketClientMode,
			SocketClientPath:   socketClientPath,
			NoSocket:           noSocket,
		}
		return "", opts, false, pprofCfg, false, false, false, "", false, nil
	}

	prompt := strings.TrimSpace(strings.Join(remaining, " "))
	if prompt == "" {
		return "", nil, false, nil, false, false, false, "", false, fmt.Errorf("prompt must not be empty")
	}

	opts := &cli.Options{
		DangerouslyAllowAll: dangerous,
		AllowAllNetwork:     allowNetwork,
		AllowedDirs:         allowDirs.toStrings(),
		AllowedFiles:        allowFiles.toStrings(),
		AllowedDomains:      allowDomains.toStrings(),
		RequireSandboxAuth:  requireSandboxAuth,
		Model:               model,
		Provider:            provider,
		JSONOutput:          jsonOutput,
		JSONExtended:        jsonExtended,
		JSONFull:            jsonFull,
	}
	if dangerous {
		opts.AllowAllNetwork = true
	}

	opts.SocketClientMode = socketClientMode
	opts.SocketClientPath = socketClientPath
	opts.NoSocket = noSocket

	return prompt, opts, true, pprofCfg, false, false, false, "", false, nil
}

func runTUI(cfg *config.Config, providerMgr *provider.Manager, cliOptions *cli.Options) error {
	logger.Info("Running in TUI mode")

	// Create cancellable context for application lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Check if socket server is available
	useSocketMode := tui.ShouldUseSocketMode(cfg)

	// Check if socket mode is explicitly enabled via --connect-to-socket flag
	if cliOptions != nil && cliOptions.SocketClientMode {
		logger.Info("Socket mode explicitly enabled via --connect-to-socket flag")
		useSocketMode = true
	}

	var factory *tui.RuntimeFactory
	var socketFactory *tui.SocketRuntimeFactory
	var model *tui.Model

	if useSocketMode {
		// Socket mode: connect to existing socket server
		logger.Info("Using socket mode - connecting to server")
		logger.Info("%s", tui.GetSocketDetectionInfo(cfg))

		// Apply socket path override from CLI if provided
		var socketPathOverride string
		if cliOptions != nil && cliOptions.SocketClientPath != "" {
			// Expand ~ to home directory and environment variables
			expandedPath := os.ExpandEnv(cliOptions.SocketClientPath)
			if strings.HasPrefix(expandedPath, "~/") {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to get home directory: %w", err)
				}
				expandedPath = filepath.Join(homeDir, expandedPath[2:])
			}
			socketPathOverride = expandedPath
			logger.Info("Socket path override from CLI: %s", socketPathOverride)
		}

		var err error
		socketFactory, err = tui.NewSocketRuntimeFactory(cfg, socketPathOverride)
		if err != nil {
			if cliOptions != nil && cliOptions.SocketClientMode {
				logger.Error("Failed to create socket factory (socket mode explicitly enabled): %v", err)
				return fmt.Errorf("failed to create socket factory: %w", err)
			}
			logger.Warn("Failed to create socket factory, falling back to local mode: %v", err)
			useSocketMode = false
		}
	}

	if useSocketMode {
		// Connect to socket server
		connectCtx, connectCancel := context.WithTimeout(ctx, 5*time.Second)
		defer connectCancel()

		// Log the socket path we're attempting to connect to
		socketPath := cfg.Socket.GetSocketPath()
		if cliOptions != nil && cliOptions.SocketClientPath != "" {
			logger.Info("Attempting to connect to socket server at: %s (from CLI override)", socketPath)
		} else {
			logger.Info("Attempting to connect to socket server at: %s (from config)", socketPath)
		}

		if err := socketFactory.Connect(connectCtx); err != nil {
			if cliOptions != nil && cliOptions.SocketClientMode {
				logger.Error("Failed to connect to socket server (socket mode explicitly enabled): %v", err)
				return fmt.Errorf("failed to connect to socket server: %w", err)
			}
			logger.Warn("Failed to connect to socket server, falling back to local mode: %v", err)
			useSocketMode = false
		}
	}

	if !useSocketMode {
		// Local mode: create RuntimeFactory for multi-tab concurrent generation
		requireSandboxAuth := false
		if cliOptions != nil {
			requireSandboxAuth = cliOptions.RequireSandboxAuth
		}
		var err error
		factory, err = tui.NewRuntimeFactoryWithRequireSandboxAuth(cfg, providerMgr, cfg.WorkingDir, false, requireSandboxAuth)
		if err != nil {
			logger.Error("Failed to create RuntimeFactory: %v", err)
			return fmt.Errorf("failed to create RuntimeFactory: %w", err)
		}
		defer factory.Close()
	}

	// Create TUI model with appropriate factory
	if useSocketMode && socketFactory != nil {
		model = tui.NewWithSocketFactory(socketFactory, cfg, providerMgr)
		model.AddSystemMessage("Connected to scriptschnell socket server")
		defer socketFactory.Close()
		// In socket mode, we don't have a local filesystem
		// Filesystem operations are handled by the server
	} else {
		model = tui.NewWithFactory(factory, cfg, providerMgr)
		logger.Info("Using local mode")
		// Set filesystem for filepath autocomplete
		model.SetFilesystem(factory.GetSharedFilesystem(), factory.GetWorkingDir())
	}

	// MCP provider callback (will be set per-tab via orchestrator in local mode)
	if !useSocketMode {
		model.SetActiveMCPProvider(func() []string {
			// Get active tab's runtime
			tab := model.GetActiveTab()
			if tab != nil && tab.Runtime != nil {
				return tab.Runtime.Orchestrator.GetActiveMCPServers()
			}
			return []string{}
		})
	}

	// Declare program variable first (will be assigned later)
	var program *tea.Program

	// Helper to get active tab's orchestrator
	getActiveOrchestrator := func() *tui.Orchestrator {
		tab := model.GetActiveTab()
		if tab != nil && tab.Runtime != nil {
			return tab.Runtime.Orchestrator
		}
		return nil
	}

	// Helper to update models for all tab runtimes
	updateAllTabModels := func() error {
		for _, tab := range model.GetAllTabs() {
			if tab.Runtime != nil && tab.Runtime.Orchestrator != nil {
				if err := tab.Runtime.Orchestrator.UpdateModels(); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Create command handler (note: orchestrator will be nil, per-tab runtimes used instead)
	cmdHandler := tui.NewCommandHandler(ctx, cfg, providerMgr, nil)

	// Set up multi-tab support
	cmdHandler.SetFactory(factory)
	cmdHandler.SetGetActiveTab(model.GetActiveTab)

	// Set up progress callback getter for multi-tab support
	cmdHandler.SetGetProgressCallback(func() progress.Callback {
		if activeTab := model.GetActiveTab(); activeTab != nil {
			return model.CreateProgressCallbackForTab(activeTab.ID)
		}
		return nil
	})

	// Helper to run overlay menus while managing terminal state
	runOverlayMenu := func(run func() error) (retErr error) {
		model.SetOverlayActive(true)
		released := false
		defer func() {
			if released {
				if rErr := program.RestoreTerminal(); rErr != nil {
					if retErr == nil {
						retErr = fmt.Errorf("failed to restore terminal: %w", rErr)
					} else {
						retErr = fmt.Errorf("%w; additionally failed to restore terminal: %v", retErr, rErr)
					}
				}
			}
			model.SetOverlayActive(false)
		}()

		if program != nil {
			if relErr := program.ReleaseTerminal(); relErr != nil {
				return fmt.Errorf("failed to release terminal: %w", relErr)
			}
			released = true
		}

		if run != nil {
			retErr = run()
		}

		return retErr
	}

	// Note: SetOnSubmit is no longer needed - prompt handling is done via per-tab runtimes
	// in startPromptForTab() method. All callbacks (authorization, progress, tool) are created
	// per-tab in createProgressCallbackForTab() and startPromptForTab().

	model.SetOnCommand(func(input string) (err error) {
		menuResult, err := cmdHandler.HandleCommand(input)
		if err != nil {
			if errors.Is(err, ErrQuitRequested) || err.Error() == "quit requested" {
				return ErrQuitRequested
			}
			return err
		}

		// Check if result contains a menu command
		if menuResult.Type != tui.MenuTypeNone {
			for {
				switch menuResult.Type {
				case tui.MenuTypeModels:
					modelType := string(menuResult.ModelRole)

					err := runOverlayMenu(func() error {
						for {
							menu := tui.NewModelsMenuV2(providerMgr, modelType, 0, 0)
							subProgram := tea.NewProgram(menu, tea.WithAltScreen())
							finalModel, err := subProgram.Run()
							if err != nil {
								return fmt.Errorf("menu error: %w", err)
							}

							menuModel, ok := finalModel.(*tui.ModelsMenuV2)
							if !ok || menuModel.GetSelectedModel() == nil {
								return nil
							}

							selectedModel := menuModel.GetSelectedModel()

							roleDialog := tui.NewModelRoleDialog(selectedModel.Name, modelType)
							roleProgram := tea.NewProgram(roleDialog, tea.WithAltScreen())
							roleResult, err := roleProgram.Run()
							if err != nil {
								return fmt.Errorf("role dialog error: %w", err)
							}

							roleChoice := modelType
							if rd, ok := roleResult.(tui.ModelRoleDialog); ok {
								if rd.GetChoice() == "" {
									model.AddSystemMessage("Model assignment cancelled")
									continue
								}
								roleChoice = rd.GetChoice()
							}

							switch roleChoice {
							case "orchestration":
								if err := providerMgr.SetOrchestrationModel(selectedModel.ID); err != nil {
									return fmt.Errorf("failed to set orchestration model: %w", err)
								}
								model.AddSystemMessage(fmt.Sprintf("✓ Orchestration model set to: %s", selectedModel.Name))
								if modelType == "orchestration" {
									modelType = "summarize"
								}

							case "summarize":
								if err := providerMgr.SetSummarizeModel(selectedModel.ID); err != nil {
									return fmt.Errorf("failed to set summarization model: %w", err)
								}
								model.AddSystemMessage(fmt.Sprintf("✓ Summarization model set to: %s", selectedModel.Name))
								if modelType == "summarize" {
									modelType = "orchestration"
								}

							case "planning":
								if err := providerMgr.SetPlanningModel(selectedModel.ID); err != nil {
									return fmt.Errorf("failed to set planning model: %w", err)
								}
								model.AddSystemMessage(fmt.Sprintf("✓ Planning model set to: %s", selectedModel.Name))
								if modelType == "planning" {
									modelType = "orchestration"
								}

							case "safety":
								if err := providerMgr.SetSafetyModel(selectedModel.ID); err != nil {
									return fmt.Errorf("failed to set safety model: %w", err)
								}
								model.AddSystemMessage(fmt.Sprintf("✓ Safety model set to: %s", selectedModel.Name))
								if modelType == "safety" {
									modelType = "orchestration"
								}

							default:
								model.AddSystemMessage("Model assignment cancelled")
								continue
							}

							if err := updateAllTabModels(); err != nil {
								return fmt.Errorf("failed to refresh orchestrator models: %w", err)
							}
							if activeOrch := getActiveOrchestrator(); activeOrch != nil {
								model.UpdateModel(activeOrch.GetCurrentModel())
							}

							// Continue the loop to optionally configure the other model type
						}
					})
					if err != nil {
						return err
					}
					if activeOrch := getActiveOrchestrator(); activeOrch != nil {
						model.SetContextFile(activeOrch.GetExtendedContextFile())
					}
					return nil

				case tui.MenuTypeProvider:
					err := runOverlayMenu(func() error {
						menu := tui.NewProviderMenu(providerMgr, 0, 0)
						subProgram := tea.NewProgram(menu, tea.WithAltScreen())
						_, err := subProgram.Run()
						if err != nil {
							return fmt.Errorf("menu error: %w", err)
						}
						return nil
					})
					if err != nil {
						return err
					}
					model.AddSystemMessage("Provider menu closed")
					if activeOrch := getActiveOrchestrator(); activeOrch != nil {
						model.SetContextFile(activeOrch.GetExtendedContextFile())
					}
					return nil

				case tui.MenuTypeSearch:
					var searchResult string
					err := runOverlayMenu(func() error {
						menu := tui.NewSettingsMenu(cfg, 0, 0)
						subProgram := tea.NewProgram(menu, tea.WithAltScreen())
						finalModel, err := subProgram.Run()
						if err != nil {
							return fmt.Errorf("menu error: %w", err)
						}
						if m, ok := finalModel.(tui.SettingsMenuModel); ok {
							searchResult = m.GetResult()
						} else if m, ok := finalModel.(*tui.SettingsMenuModel); ok {
							searchResult = m.GetResult()
						}
						return nil
					})
					if err != nil {
						return err
					}
					if searchResult != "" {
						model.AddSystemMessage(searchResult)
					} else {
						model.AddSystemMessage("Search settings closed")
					}
					if activeOrch := getActiveOrchestrator(); activeOrch != nil {
						model.SetContextFile(activeOrch.GetExtendedContextFile())
					}
					return nil
				case tui.MenuTypeSecrets:
					var (
						newPassword string
						confirmed   bool
					)
					err := runOverlayMenu(func() error {
						menu := tui.NewSecretsMenu(0, 0)
						subProgram := tea.NewProgram(menu, tea.WithAltScreen())
						finalModel, err := subProgram.Run()
						if err != nil {
							return fmt.Errorf("menu error: %w", err)
						}
						if m, ok := finalModel.(*tui.SecretsMenuModel); ok {
							newPassword, confirmed = m.Result()
						}
						return nil
					})
					if err != nil {
						return err
					}
					if !confirmed {
						model.AddSystemMessage("Password update cancelled")
						return nil
					}
					if err := cfg.UpdateSecretsPassword(newPassword); err != nil {
						return fmt.Errorf("failed to update secrets password: %w", err)
					}
					if err := cfg.Save(config.GetConfigPath()); err != nil {
						return fmt.Errorf("failed to save config: %w", err)
					}
					if err := providerMgr.SetPassword(newPassword); err != nil {
						return fmt.Errorf("failed to re-encrypt provider config: %w", err)
					}
					if newPassword == "" {
						model.AddSystemMessage("Encryption password cleared. API keys now use the default protection (empty password).")
					} else {
						model.AddSystemMessage("Encryption password updated. Keep this password safe—it's required at startup.")
					}
					return nil

				case tui.MenuTypeMCP:
					var mcpResult string
					err := runOverlayMenu(func() error {
						persist := func(serverName string, validate bool) (string, error) {
							if err := cfg.Save(config.GetConfigPath()); err != nil {
								return "", err
							}
							// Refresh MCP tools for all tab runtimes
							var errList []error
							for _, tab := range model.GetAllTabs() {
								if tab.Runtime != nil && tab.Runtime.Orchestrator != nil {
									tabErrs := tab.Runtime.Orchestrator.RefreshMCPTools()
									errList = append(errList, tabErrs...)
								}
							}
							if len(errList) > 0 {
								messages := make([]string, 0, len(errList))
								for _, e := range errList {
									if e != nil {
										messages = append(messages, e.Error())
									}
								}
								if len(messages) > 0 {
									return "", fmt.Errorf("some MCP tools failed to initialize: %s", strings.Join(messages, "; "))
								}
							}
							if validate && serverName != "" {
								// Test MCP server using active tab's orchestrator
								if activeOrch := getActiveOrchestrator(); activeOrch != nil {
									if err := activeOrch.TestMCPServer(serverName); err != nil {
										return "", fmt.Errorf("validation failed for '%s': %w", serverName, err)
									}
									return fmt.Sprintf("MCP server '%s' validated successfully", serverName), nil
								}
								return "", fmt.Errorf("no active orchestrator available for validation")
							}
							return "", nil
						}

						menu := tui.NewMCPMenu(cfg, 0, 0, persist)
						subProgram := tea.NewProgram(menu, tea.WithAltScreen())
						finalModel, err := subProgram.Run()
						if err != nil {
							return fmt.Errorf("menu error: %w", err)
						}
						if m, ok := finalModel.(*tui.MCPMenuModel); ok {
							mcpResult = m.GetResult()
						}
						return nil
					})
					if err != nil {
						return err
					}
					if mcpResult != "" {
						model.AddSystemMessage(mcpResult)
					} else {
						model.AddSystemMessage("MCP configuration closed")
					}
					if activeOrch := getActiveOrchestrator(); activeOrch != nil {
						model.SetContextFile(activeOrch.GetExtendedContextFile())
					}
					return nil

				case tui.MenuTypeSettings:
					var nextMenuResult tui.MenuResult
					err := runOverlayMenu(func() error {
						menu := tui.NewSettingsMainMenu(cfg, providerMgr, 0, 0)
						subProgram := tea.NewProgram(menu, tea.WithAltScreen())
						finalModel, err := subProgram.Run()
						if err != nil {
							return fmt.Errorf("menu error: %w", err)
						}
						if m, ok := finalModel.(tui.SettingsMainMenuModel); ok {
							nextMenuResult = m.GetMenuResult()
						} else if m, ok := finalModel.(*tui.SettingsMainMenuModel); ok {
							nextMenuResult = m.GetMenuResult()
						}
						return nil
					})
					if err != nil {
						return err
					}
					if nextMenuResult.Type == tui.MenuTypeNone {
						model.AddSystemMessage("Settings menu closed")
						return nil
					}
					menuResult = nextMenuResult
					continue

				case tui.MenuTypeClearSession:
					// Clear the TUI messages display
					model.ClearMessages()
					if menuResult.Message != "" {
						model.AddSystemMessage(menuResult.Message)
					} else {
						model.AddSystemMessage("Session cleared. Starting fresh conversation.")
					}
					return nil

				case tui.MenuTypeNewTab:
					// Create a new tab with the specified name
					if program != nil {
						program.Send(tui.NewTabMsg{Name: menuResult.TabName})
					}
					return nil

				case tui.MenuTypeSession:
					// Open session management menu
					var loadedSessionInfo *tui.LoadedSessionInfo
					err := runOverlayMenu(func() error {
						// Get session storage actor from factory
						storageRef := factory.GetSessionStorageRef()
						if storageRef == nil {
							return fmt.Errorf("session storage not initialized")
						}

						menu := tui.NewSessionMenu(ctx, storageRef, cfg.WorkingDir, 0, 0)
						subProgram := tea.NewProgram(menu, tea.WithAltScreen())
						finalModel, err := subProgram.Run()
						if err != nil {
							return fmt.Errorf("menu error: %w", err)
						}

						// Handle menu result
						if sessionMenu, ok := finalModel.(*tui.SessionMenuModel); ok {
							action, selectedItem := sessionMenu.GetAction()

							// Only process if an action was taken
							if action == "" {
								return nil
							}

							switch action {
							case "save":
								// Save current session
								activeOrch := getActiveOrchestrator()
								if activeOrch == nil {
									return fmt.Errorf("no active session to save")
								}

								saveName := sessionMenu.GetSaveName()
								if saveName == "" {
									saveName = actor.GenerateSessionName("")
								} else {
									saveName = actor.GenerateSessionName(saveName)
								}

								// Generate session title if not already present
								if err := activeOrch.GenerateSessionTitle(ctx); err != nil {
									logger.Warn("session save: failed to generate title: %v", err)
								}

								currentSession := activeOrch.GetSession()
								if err := actor.SaveSessionViaActor(ctx, storageRef, currentSession, saveName); err != nil {
									return fmt.Errorf("failed to save session: %w", err)
								}

								model.AddSystemMessage(fmt.Sprintf("Session saved as '%s'", saveName))

							case "load":
								sessionID := selectedItem.GetSessionID()
								if sessionID == "" {
									return fmt.Errorf("invalid session selected")
								}

								// Load the session using actor
								loadedSession, err := actor.LoadSessionViaActor(ctx, storageRef, cfg.WorkingDir, sessionID)
								if err != nil {
									return fmt.Errorf("failed to load session: %w", err)
								}

								// Get session name for display
								sessions, _ := actor.ListSessionsViaActor(ctx, storageRef, cfg.WorkingDir)
								var sessionName string
								for _, sess := range sessions {
									if sess.ID == sessionID {
										sessionName = sess.Name
										if sessionName == "" {
											sessionName = sess.Title
										}
										break
									}
								}

								// Replace current session in active tab's orchestrator
								if activeOrch := getActiveOrchestrator(); activeOrch != nil {
									activeOrch.SetSession(loadedSession)
								}

								// Store loaded session info to be restored after menu closes
								loadedSessionInfo = &tui.LoadedSessionInfo{
									Session: loadedSession,
									Name:    sessionName,
								}
								model.AddSystemMessage(fmt.Sprintf("Loaded session: %s", selectedItem.Title()))

							case "delete":
								sessionID := selectedItem.GetSessionID()
								if sessionID == "" {
									return fmt.Errorf("invalid session selected")
								}

								// Delete the session using actor
								deleteMsg := actor.SessionStorageDeleteMsg{
									WorkingDir:   cfg.WorkingDir,
									SessionID:    sessionID,
									ResponseChan: make(chan actor.SessionStorageDeleteResponse, 1),
								}
								if err := storageRef.Send(deleteMsg); err != nil {
									return fmt.Errorf("failed to send delete message: %w", err)
								}
								response := <-deleteMsg.ResponseChan
								if response.Err != nil {
									return fmt.Errorf("failed to delete session: %w", response.Err)
								}
								model.AddSystemMessage(fmt.Sprintf("Deleted session: %s", selectedItem.Title()))
							}
						}

						return nil
					})
					if err != nil {
						return err
					}

					// Restore UI state if a saved session was loaded
					if loadedSessionInfo != nil {
						model.RestoreLoadedSession(loadedSessionInfo)
					}
					return nil

				default:
					return nil
				}
			}
		}

		// Restore UI state if a saved session was loaded
		if menuResult.LoadedSession != nil {
			model.RestoreLoadedSession(menuResult.LoadedSession)
		}

		// Display any message from the result
		if menuResult.Message != "" {
			model.AddSystemMessage(menuResult.Message)
		}

		// Refresh orchestrator models for all tabs if needed (must happen before UpdateModel)
		if err := updateAllTabModels(); err != nil {
			return fmt.Errorf("failed to refresh orchestrator models: %w", err)
		}

		// Update model name and context file after orchestrators are refreshed
		if activeOrch := getActiveOrchestrator(); activeOrch != nil {
			model.UpdateModel(activeOrch.GetCurrentModel())
			model.SetContextFile(activeOrch.GetExtendedContextFile())
		}

		return nil
	})

	model.SetOnStop(func() error {
		// Stop all tab runtimes
		for _, tab := range model.GetAllTabs() {
			if tab.Runtime != nil && tab.Runtime.Orchestrator != nil {
				tab.Runtime.Orchestrator.Stop()
			}
		}
		return nil
	})

	model.SetOnBackground(func() error {
		// Background current shell job on active tab
		if activeOrch := getActiveOrchestrator(); activeOrch != nil {
			return activeOrch.BackgroundCurrentShellJob()
		}
		return nil
	})

	// Planning questions are handled via the per-tab user input callbacks configured in the TUI.

	// Attempt to resume the last session if auto-resume is enabled
	if cfg.AutoResume {
		logger.Info("Auto-resume enabled, attempting to resume last session")
		storageRef := factory.GetSessionStorageRef()
		if storageRef != nil {
			if err := model.ResumeLastSession(ctx, storageRef); err != nil {
				logger.Warn("Failed to resume last session: %v", err)
			}
		} else {
			logger.Warn("Session storage not available for auto-resume")
		}
	}

	// Run TUI
	program = tea.NewProgram(model, tea.WithAltScreen())

	// Set program reference for self-messaging (critical for per-tab message routing)
	model.SetProgram(program)

	// Run program in goroutine to catch and log panics
	errChan := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("TUI panic: %v", r)
				errChan <- fmt.Errorf("TUI panic: %v", r)
			}
		}()

		_, err := program.Run()
		errChan <- err
	}()

	if err := <-errChan; err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}
