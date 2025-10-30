package tui

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/provider"
)

// CommandHandler handles TUI commands
type CommandHandler struct {
	providerMgr     *provider.Manager
	orchestrator    *Orchestrator
	ctx             context.Context
	streamCallback  func(string) error
	statusCallback  func(string) error
	contextCallback ContextUsageCallback
}

func NewCommandHandler(ctx context.Context, providerMgr *provider.Manager, orchestrator *Orchestrator) *CommandHandler {
	return &CommandHandler{
		providerMgr:  providerMgr,
		orchestrator: orchestrator,
		ctx:          ctx,
	}
}

// SetStreamCallback sets the callback for streaming responses
func (ch *CommandHandler) SetStreamCallback(callback func(string) error) {
	ch.streamCallback = callback
}

// SetStatusCallback sets the callback for status updates
func (ch *CommandHandler) SetStatusCallback(callback func(string) error) {
	ch.statusCallback = callback
}

// SetContextCallback sets the callback for context usage updates
func (ch *CommandHandler) SetContextCallback(callback ContextUsageCallback) {
	ch.contextCallback = callback
}

// HandleCommand processes a command
func (ch *CommandHandler) HandleCommand(command string) (MenuResult, error) {
	command = strings.TrimSpace(command)
	if !strings.HasPrefix(command, "/") {
		return MenuResult{}, fmt.Errorf("commands must start with /")
	}

	parts := strings.Fields(command)
	cmd := parts[0]

	switch cmd {
	case "/help":
		return NewMenuResult(ch.handleHelp()), nil

	case "/settings":
		return ch.handleSettings()

	case "/models":
		return ch.handleModels(parts[1:])

	case "/provider":
		return ch.handleProvider(parts[1:])

	case "/init":
		return ch.handleInit()

	case "/quit":
		return MenuResult{}, ErrQuitRequested

	case "/clear":
		return ch.handleClear()

	default:
		return MenuResult{}, fmt.Errorf("unknown command: %s. Type /help for available commands", cmd)
	}
}

func (ch *CommandHandler) handleHelp() string {
	help := `Available Commands:

/help             - Show this help message
/settings         - Open main settings menu (providers, models, search)
/models           - Interactive model selector (orchestration)
/models refresh   - Fetch latest models from provider APIs
/provider         - Interactive provider management
/init             - Initialize/update AGENTS.md with codebase summary
/clear            - Clear conversation and start a new session
/quit             - Quit the application

Keyboard Shortcuts:

Ctrl+X            - Enter command mode
Ctrl+C (×2)       - Quit application
ESC               - Stop current generation
Shift+Enter       - Insert newline in prompt
Alt+Enter         - Alternate newline shortcut
Enter             - Submit prompt or command

Quick Start:
1. Configure settings: /settings
2. Add a provider and select models
3. Optionally configure web search
4. Start chatting!

Model Information:
- Orchestration: Used for main conversation and tool calls
- Summarize: Used for file summarization tasks
`
	return help
}

func (ch *CommandHandler) handleSettings() (MenuResult, error) {
	return NewSettingsMenuResult(), nil
}

func (ch *CommandHandler) handleModels(args []string) (MenuResult, error) {
	if len(args) == 0 {
		// No arguments - open interactive menu for orchestration model by default
		return NewModelsMenuResult(ModelRoleOrchestration), nil
	}

	subCmd := args[0]
	switch subCmd {
	case "refresh":
		// Refresh models from API for all providers
		providers := ch.providerMgr.ListProviders()
		if len(providers) == 0 {
			return NewMenuResult("No providers configured. Use /provider to add a provider first."), nil
		}

		var sb strings.Builder
		sb.WriteString("Refreshing models from provider APIs...\n\n")

		totalModels := 0
		for _, p := range providers {
			if err := ch.providerMgr.RefreshModels(ch.ctx, p.Name); err != nil {
				sb.WriteString(fmt.Sprintf("✗ %s: %v\n", p.Name, err))
			} else {
				// Get updated provider
				updatedProvider, _ := ch.providerMgr.GetProvider(p.Name)
				modelCount := len(updatedProvider.Models)
				totalModels += modelCount
				sb.WriteString(fmt.Sprintf("✓ %s: fetched %d models\n", p.Name, modelCount))
			}
		}

		sb.WriteString(fmt.Sprintf("\nTotal: %d models available\n", totalModels))
		return NewMenuResult(sb.String()), nil

	case "menu":
		if len(args) < 2 {
			return MenuResult{}, fmt.Errorf("usage: /models menu <orchestration|summarize>")
		}
		modelType := args[1]
		if modelType != "orchestration" && modelType != "summarize" {
			return MenuResult{}, fmt.Errorf("unknown model type: %s (use 'orchestration' or 'summarize')", modelType)
		}

		return NewModelsMenuResult(ModelRole(modelType)), nil

	default:
		return MenuResult{}, fmt.Errorf("unknown subcommand: %s", subCmd)
	}
}

func (ch *CommandHandler) handleProvider(args []string) (MenuResult, error) {
	if len(args) == 0 {
		// No arguments - open interactive menu by default
		return NewProviderMenuResult(), nil
	}

	subCmd := args[0]
	switch subCmd {
	case "menu":
		return NewProviderMenuResult(), nil

	default:
		return MenuResult{}, fmt.Errorf("unknown provider subcommand: %s", subCmd)
	}
}

func (ch *CommandHandler) handleInit() (MenuResult, error) {
	// Check if orchestration model is configured
	orchModelID := ch.providerMgr.GetOrchestrationModel()
	if orchModelID == "" {
		return MenuResult{}, fmt.Errorf("no orchestration model configured. Use /models to set one")
	}

	// Check if we have a stream callback
	if ch.streamCallback == nil {
		return MenuResult{}, fmt.Errorf("streaming not available in this context")
	}

	// Get init prompt
	initPrompt := ch.orchestrator.GetInitPrompt()

	// Set initial status
	if ch.statusCallback != nil {
		if err := ch.statusCallback("Analyzing codebase to generate AGENTS.md"); err != nil {
			return MenuResult{}, fmt.Errorf("failed to update status: %w", err)
		}
	}

	// Process through orchestrator with streaming (in background)
	go func() {
		if err := ch.orchestrator.ProcessPrompt(ch.ctx, initPrompt, ch.streamCallback, ch.statusCallback, ch.contextCallback, nil); err != nil {
			// Error will be handled by orchestrator's error handling
			// Clear status on error
			if ch.statusCallback != nil {
				if err := ch.statusCallback(""); err != nil {
					log.Printf("Failed to clear status after init error: %v", err)
				}
			}
			return
		}
		// Clear status on success
		if ch.statusCallback != nil {
			if err := ch.statusCallback(""); err != nil {
				log.Printf("Failed to clear status after init success: %v", err)
			}
		}
	}()

	return NewMenuResult("Analyzing codebase to generate AGENTS.md..."), nil
}

func (ch *CommandHandler) handleClear() (MenuResult, error) {
	// Clear the session in the orchestrator
	if err := ch.orchestrator.ClearSession(); err != nil {
		return MenuResult{}, fmt.Errorf("failed to clear session: %w", err)
	}

	return NewClearSessionResult(), nil
}

// GetKeyMap returns keyboard shortcut help
func GetKeyMap() string {
	return `Keyboard Shortcuts:

Basic:
  Enter         - Submit prompt/command
  Ctrl+C (×2)   - Quit application
  ESC           - Stop current generation

Commands:
  Ctrl+X + M    - Open models menu
  Ctrl+X + P    - Open provider menu
  Ctrl+X + I    - Initialize AGENTS.md
  Ctrl+X + H    - Show help
  Ctrl+X + Q    - Quit
`
}
