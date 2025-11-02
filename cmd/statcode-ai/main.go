package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/statcode-ai/statcode-ai/internal/cli"
	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/provider"
	"github.com/statcode-ai/statcode-ai/internal/tui"
)

var ErrQuitRequested = errors.New("quit requested")

type stringSlice []string

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
	prompt, cliOptions, cliMode, parseErr := parseCLIArgs(os.Args[1:])
	if parseErr != nil {
		if errors.Is(parseErr, flag.ErrHelp) {
			return nil
		}
		return parseErr
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
	}()

	// Load configuration
	fmt.Fprintf(os.Stderr, "[Main] About to load config from: %s\n", config.GetConfigPath())
	fmt.Fprintf(os.Stderr, "[Main] Environment vars: STATCODE_LOG_LEVEL=%q STATCODE_LOG_PATH=%q\n",
		os.Getenv("STATCODE_LOG_LEVEL"), os.Getenv("STATCODE_LOG_PATH"))

	cfg, err := config.Load(config.GetConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[Main] Config loaded successfully\n")

	// Allow environment variables to override config file values for logging.
	if envLevel := strings.TrimSpace(os.Getenv("STATCODE_LOG_LEVEL")); envLevel != "" {
		fmt.Fprintf(os.Stderr, "[Main] Overriding log level from environment: %s\n", envLevel)
		cfg.LogLevel = envLevel
	}
	if envPath := strings.TrimSpace(os.Getenv("STATCODE_LOG_PATH")); envPath != "" {
		fmt.Fprintf(os.Stderr, "[Main] Overriding log path from environment: %s\n", envPath)
		cfg.LogPath = envPath
	}

	// Initialize logger
	logLevel := logger.ParseLevel(cfg.LogLevel)

	// Print log configuration to stderr for debugging (especially in CLI mode)
	if len(os.Args) > 1 {
		fmt.Fprintf(os.Stderr, "[Debug] Log level: %s, Log path: %s\n", cfg.LogLevel, cfg.LogPath)
	}

	if initErr := logger.Init(logLevel, cfg.LogPath); initErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", initErr)
	}
	loggerInitialized = true

	logger.Info("StatCode AI starting")
	logger.Debug("Configuration loaded: working_dir=%s, log_level=%s, log_path=%s", cfg.WorkingDir, cfg.LogLevel, cfg.LogPath)

	// Ensure temp directory exists
	if err := os.MkdirAll(cfg.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Load provider manager
	providerMgr, err := provider.NewManager(cfg.ProviderConfigPath)
	if err != nil {
		return fmt.Errorf("failed to initialize provider manager: %w", err)
	}

	// Refresh models from APIs in background on startup
	ctx := context.Background()
	logger.Debug("Refreshing models from provider APIs")
	providerMgr.RefreshAllModels(ctx)

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

func parseCLIArgs(args []string) (string, *cli.Options, bool, error) {
	fs := flag.NewFlagSet("statcode-ai", flag.ContinueOnError)
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
	)

	fs.BoolVar(&dangerous, "dangerous-allow-all", false, "Bypass all authorization checks (dangerous)")
	fs.BoolVar(&allowNetwork, "allow-network", false, "Allow network access to all domains")
	fs.Var(&allowDirs, "allow-dir", "Pre-authorize a directory for write operations (repeatable)")
	fs.Var(&allowFiles, "allow-file", "Pre-authorize a specific file for write operations (repeatable)")
	fs.Var(&allowDomains, "allow-domain", "Pre-authorize network access to a domain (repeatable)")
	fs.StringVar(&model, "model", "", "Model to use (e.g., gpt-5, claude-sonnet-4.5, gemini-2.5-pro)")
	fs.StringVar(&provider, "provider", "", "Provider name (e.g., openai, anthropic, google)")
	fs.BoolVar(&showHelp, "help", false, "Show CLI usage information")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [options] \"your prompt here\"\n\n", os.Args[0])
		fmt.Fprintln(fs.Output(), "Options:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", nil, false, flag.ErrHelp
		}
		return "", nil, false, err
	}

	if showHelp {
		fs.Usage()
		return "", nil, false, flag.ErrHelp
	}

	remaining := fs.Args()
	optionsUsed := dangerous || allowNetwork || len(allowDirs) > 0 || len(allowFiles) > 0 || len(allowDomains) > 0
	if len(remaining) == 0 {
		if optionsUsed {
			return "", nil, false, fmt.Errorf("authorization flags require a prompt in CLI mode")
		}
		return "", nil, false, nil
	}

	prompt := strings.TrimSpace(strings.Join(remaining, " "))
	if prompt == "" {
		return "", nil, false, fmt.Errorf("prompt must not be empty")
	}

	opts := &cli.Options{
		DangerouslyAllowAll: dangerous,
		AllowAllNetwork:     allowNetwork,
		AllowedDirs:         allowDirs.toStrings(),
		AllowedFiles:        allowFiles.toStrings(),
		AllowedDomains:      allowDomains.toStrings(),
		Model:               model,
		Provider:            provider,
	}
	if dangerous {
		opts.AllowAllNetwork = true
	}

	return prompt, opts, true, nil
}

func runTUI(cfg *config.Config, providerMgr *provider.Manager) error {
	logger.Info("Running in TUI mode")
	ctx := context.Background()

	// Create orchestrator
	// TUI mode is interactive, so pass cliMode=false
	orch, err := tui.NewOrchestrator(cfg, providerMgr, false)
	if err != nil {
		logger.Error("Failed to create orchestrator: %v", err)
		return fmt.Errorf("failed to create orchestrator: %w", err)
	}
	defer orch.Close()

	// Create TUI model
	model := tui.New(orch.GetCurrentModel(), orch.GetContextFile(), cfg.DisableAnimations)

	// Set filesystem for filepath autocomplete
	model.SetFilesystem(orch.GetFilesystem(), orch.GetWorkingDir())
	model.SetTodoClient(orch.GetTodoClient())

	// Declare program variable first (will be assigned later)
	var program *tea.Program

	// Create streaming callback
	streamCallback := func(chunk string) error {
		if program != nil {
			program.Send(tui.GeneratingMsg{Content: chunk})
		}
		return nil
	}

	// Create command handler with streaming support
	cmdHandler := tui.NewCommandHandler(ctx, providerMgr, orch)
	cmdHandler.SetStreamCallback(streamCallback)
	cmdHandler.SetStatusCallback(func(status string) error {
		// Send processing status updates
		if program != nil {
			program.Send(tui.ProcessingStatusMsg{Status: status})
		}
		return nil
	})
	cmdHandler.SetContextCallback(func(percent int, contextWindow int) error {
		if program != nil {
			program.Send(tui.ContextUsageMsg{FreePercent: percent, ContextWindow: contextWindow})
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

	// Set up callbacks
	model.SetOnSubmit(func(input string) error {
		model.SetContextFile(orch.GetContextFile())
		// Process prompt in a goroutine and send chunks via tea.Cmd
		go func() {
			err := orch.ProcessPrompt(ctx, input, func(chunk string) error {
				// Send each chunk as a message to the program
				if program != nil {
					program.Send(tui.GeneratingMsg{Content: chunk})
				}
				return nil
			}, func(status string) error {
				// Send processing status updates
				if program != nil {
					program.Send(tui.ProcessingStatusMsg{Status: status})
				}
				return nil
			}, func(percent int, contextWindow int) error {
				if program != nil {
					program.Send(tui.ContextUsageMsg{FreePercent: percent, ContextWindow: contextWindow})
				}
				return nil
			}, func(toolName string, params map[string]interface{}, reason string) (bool, error) {
				// Authorization callback - show dialog and wait for user response
				responseChan := make(chan bool, 1)
				errorChan := make(chan error, 1)

				// Release terminal and show authorization dialog
				go func() {
					model.SetOverlayActive(true)
					defer model.SetOverlayActive(false)

					if program != nil {
						if err := program.ReleaseTerminal(); err != nil {
							errorChan <- fmt.Errorf("failed to release terminal: %w", err)
							return
						}
						defer func() {
							if err := program.RestoreTerminal(); err != nil {
								// Log error but don't fail
								fmt.Fprintf(os.Stderr, "Warning: failed to restore terminal: %v\n", err)
							}
						}()
					}

					// Show authorization dialog
					authDialog := tui.NewAuthorizationDialog(tui.AuthorizationRequest{
						ToolName:   toolName,
						Parameters: params,
						Reason:     reason,
					})
					authProgram := tea.NewProgram(authDialog, tea.WithAltScreen())
					finalModel, err := authProgram.Run()
					if err != nil {
						errorChan <- fmt.Errorf("authorization dialog error: %w", err)
						return
					}

					// Get user's decision
					if authModel, ok := finalModel.(tui.AuthorizationDialog); ok {
						responseChan <- authModel.GetApproved()
					} else {
						responseChan <- false
					}
				}()

				// Wait for response
				select {
				case err := <-errorChan:
					return false, err
				case approved := <-responseChan:
					return approved, nil
				case <-ctx.Done():
					return false, ctx.Err()
				}
			})

			// Send complete message or error
			if program != nil {
				if err != nil {
					program.Send(tui.ErrMsg(err))
				} else {
					program.Send(tui.CompleteMsg{})
				}
			}
		}()
		return nil
	})

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

							default:
								model.AddSystemMessage("Model assignment cancelled")
								continue
							}

							if err := orch.UpdateModels(); err != nil {
								return fmt.Errorf("failed to refresh orchestrator models: %w", err)
							}
							model.UpdateModel(orch.GetCurrentModel())

							// Continue the loop to optionally configure the other model type
						}
					})
					if err != nil {
						return err
					}
					model.SetContextFile(orch.GetContextFile())
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
					model.SetContextFile(orch.GetContextFile())
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
					model.SetContextFile(orch.GetContextFile())
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

				default:
					return nil
				}
			}
		}

		// Display any message from the result
		if menuResult.Message != "" {
			model.AddSystemMessage(menuResult.Message)
		}

		// Update model name if changed
		model.UpdateModel(orch.GetCurrentModel())

		// Refresh orchestrator if needed
		if err := orch.UpdateModels(); err != nil {
			return fmt.Errorf("failed to refresh orchestrator models: %w", err)
		}
		model.SetContextFile(orch.GetContextFile())

		return nil
	})

	model.SetOnStop(func() error {
		orch.Stop()
		return nil
	})

	model.SetOnBackground(func() error {
		return orch.BackgroundCurrentShellJob()
	})

	// Run TUI
	program = tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}
