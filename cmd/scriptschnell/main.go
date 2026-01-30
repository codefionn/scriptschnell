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
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/acp"
	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/cli"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/pprof"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/secrets"
	"github.com/codefionn/scriptschnell/internal/securemem"
	"github.com/codefionn/scriptschnell/internal/tui"
	"github.com/codefionn/scriptschnell/internal/web"
	"golang.org/x/term"
)

var (
	ErrQuitRequested = errors.New("quit requested")
	errACPMode       = errors.New("ACP mode requested")
	errWebMode       = errors.New("web mode requested")
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
	prompt, cliOptions, cliMode, pprofCfg, webMode, webDebug, parseErr := parseCLIArgs(os.Args[1:])
	if parseErr != nil {
		if errors.Is(parseErr, flag.ErrHelp) {
			return nil
		}
		if errors.Is(parseErr, errACPMode) {
			// Handle ACP mode separately
			return runACPMode()
		}
		if errors.Is(parseErr, errWebMode) {
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

	if initErr := logger.Init(logLevel, cfg.LogPath); initErr != nil {
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
	} else {
		// Async refresh for TUI mode
		providerMgr.RefreshAllModels(ctx)
	}

	// Check for web mode flag after config is loaded
	if webMode {
		return runWeb(cfg, providerMgr, secretsPassword, webDebug)
	}

	if cliMode {
		return runCLI(cfg, providerMgr, prompt, cliOptions)
	}

	// Run TUI mode
	return runTUI(cfg, providerMgr)
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

func runWeb(cfg *config.Config, providerMgr *provider.Manager, secretsPassword *securemem.String, webDebug bool) error {
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
	srv, err := web.NewServer(ctx, cfg, providerMgr, secretsPassword, webDebug)
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

func parseCLIArgs(args []string) (string, *cli.Options, bool, *pprof.Config, bool, bool, error) {
	fs := flag.NewFlagSet("scriptschnell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		dangerous    bool
		allowNetwork bool
		allowDirs    stringSlice
		allowFiles   stringSlice
		allowDomains stringSlice
		showHelp     bool
		model        string
		provider     string
		acpMode      bool
		webMode      bool
		webDebug     bool
		jsonOutput   bool
		jsonExtended bool
		jsonFull     bool

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
	fs.StringVar(&model, "model", "", "Model to use (e.g., gpt-5, claude-sonnet-4.5, gemini-2.5-pro)")
	fs.StringVar(&provider, "provider", "", "Provider name (e.g., openai, anthropic, google)")
	fs.BoolVar(&acpMode, "acp", false, "Run in Agent Client Protocol (ACP) mode for integration with code editors")
	fs.BoolVar(&webMode, "web", false, "Run in web mode with browser-based UI")
	fs.BoolVar(&webDebug, "web-debug", false, "Enable debug logging for web server and WebSocket connections")
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
		fmt.Fprintln(fs.Output(), "Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", nil, false, nil, false, false, flag.ErrHelp
		}
		return "", nil, false, nil, false, false, err
	}

	if showHelp {
		fs.Usage()
		return "", nil, false, nil, false, false, flag.ErrHelp
	}

	remaining := fs.Args()
	optionsUsed := dangerous || allowNetwork || len(allowDirs) > 0 || len(allowFiles) > 0 || len(allowDomains) > 0

	// Handle ACP mode
	if acpMode {
		if len(remaining) > 0 {
			return "", nil, false, nil, false, false, nil
		}
		if optionsUsed {
			return "", nil, false, nil, false, false, nil
		}
		// Return special values to indicate ACP mode
		return "", nil, false, nil, false, false, errACPMode
	}

	// Handle web mode
	if webMode {
		if len(remaining) > 0 {
			return "", nil, false, nil, false, false, flag.ErrHelp
		}
		if optionsUsed {
			return "", nil, false, nil, false, false, flag.ErrHelp
		}
		// Return special values to indicate web mode
		return "", nil, false, nil, true, webDebug, nil
	}

	if len(remaining) == 0 {
		if optionsUsed {
			return "", nil, false, nil, false, false, nil
		}
		return "", nil, false, nil, false, false, nil
	}

	prompt := strings.TrimSpace(strings.Join(remaining, " "))
	if prompt == "" {
		return "", nil, false, nil, false, false, fmt.Errorf("prompt must not be empty")
	}

	opts := &cli.Options{
		DangerouslyAllowAll: dangerous,
		AllowAllNetwork:     allowNetwork,
		AllowedDirs:         allowDirs.toStrings(),
		AllowedFiles:        allowFiles.toStrings(),
		AllowedDomains:      allowDomains.toStrings(),
		Model:               model,
		Provider:            provider,
		JSONOutput:          jsonOutput,
		JSONExtended:        jsonExtended,
		JSONFull:            jsonFull,
	}
	if dangerous {
		opts.AllowAllNetwork = true
	}

	// Build pprof config
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

	return prompt, opts, true, pprofCfg, false, false, nil
}

func runTUI(cfg *config.Config, providerMgr *provider.Manager) error {
	logger.Info("Running in TUI mode")

	// Create cancellable context for application lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create RuntimeFactory for multi-tab concurrent generation
	factory, err := tui.NewRuntimeFactory(cfg, providerMgr, cfg.WorkingDir, false)
	if err != nil {
		logger.Error("Failed to create RuntimeFactory: %v", err)
		return fmt.Errorf("failed to create RuntimeFactory: %w", err)
	}
	defer factory.Close()

	// Create TUI model with factory
	model := tui.NewWithFactory(factory, cfg, providerMgr)

	// Set filesystem for filepath autocomplete
	model.SetFilesystem(factory.GetSharedFilesystem(), factory.GetWorkingDir())

	// MCP provider callback (will be set per-tab via orchestrator)
	model.SetActiveMCPProvider(func() []string {
		// Get active tab's runtime
		tab := model.GetActiveTab()
		if tab != nil && tab.Runtime != nil {
			return tab.Runtime.Orchestrator.GetActiveMCPServers()
		}
		return []string{}
	})

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

							sessionID := selectedItem.GetSessionID()
							if sessionID == "" {
								return fmt.Errorf("invalid session selected")
							}

							switch action {
							case "load":
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

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}
