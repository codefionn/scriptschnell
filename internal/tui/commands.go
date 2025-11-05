package tui

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/provider"
)

// CommandHandler handles TUI commands
type CommandHandler struct {
	providerMgr     *provider.Manager
	orchestrator    *Orchestrator
	config          *config.Config
	ctx             context.Context
	streamCallback  func(string) error
	statusCallback  func(string) error
	contextCallback ContextUsageCallback
}

func NewCommandHandler(ctx context.Context, cfg *config.Config, providerMgr *provider.Manager, orchestrator *Orchestrator) *CommandHandler {
	return &CommandHandler{
		config:       cfg,
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

	case "/mcp":
		return ch.handleMCP(parts[1:])

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
/mcp              - Manage custom MCP servers (/mcp help for subcommands)

Keyboard Shortcuts:

Ctrl+X            - Enter command mode
Ctrl+B            - Background current shell job
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

func (ch *CommandHandler) handleMCP(args []string) (MenuResult, error) {
	if ch.config == nil {
		return MenuResult{}, fmt.Errorf("configuration unavailable")
	}

	if len(args) == 0 || args[0] == "help" {
		return NewMenuResult(ch.mcpHelp()), nil
	}

	subCmd := strings.ToLower(args[0])
	switch subCmd {
	case "list":
		return ch.handleMCPList()
	case "add-openapi":
		return ch.handleMCPAddOpenAPI(args[1:])
	case "add-command":
		return ch.handleMCPAddCommand(args[1:])
	case "add-openai":
		return ch.handleMCPAddOpenAI(args[1:])
	case "remove":
		return ch.handleMCPRemove(args[1:])
	case "enable":
		return ch.handleMCPEnableDisable(args[1:], false)
	case "disable":
		return ch.handleMCPEnableDisable(args[1:], true)
	default:
		return MenuResult{}, fmt.Errorf("unknown /mcp subcommand: %s", subCmd)
	}
}

func (ch *CommandHandler) mcpHelp() string {
	return `MCP Commands:

/mcp list
    Show configured MCP servers.

/mcp add-openapi <name> <spec_path_or_url> [--base-url URL] [--header KEY:VALUE] [--query KEY=VALUE] [--description TEXT]
    Register an OpenAPI document as a set of tools. Headers/queries are optional and may be repeated.

/mcp add-command <name> <command ...> [--cwd PATH] [--env KEY=VALUE] [--timeout SECONDS] [--description TEXT]
    Expose a local command as an MCP tool. Environment variables and timeout are optional and may repeat.

/mcp add-openai <name> <model> [--api-key KEY] [--api-key-env ENV] [--base-url URL] [--temperature FLOAT] [--max-output TOKENS] [--system TEXT] [--json]
    Invoke an OpenAI or compatible model as a tool. Prefer --api-key-env to avoid storing secrets on disk.

/mcp remove <name>
    Delete a configured MCP server.

/mcp enable <name>
    Enable a previously disabled MCP server.

/mcp disable <name>
    Disable an MCP server without deleting it.
`
}

func (ch *CommandHandler) handleMCPList() (MenuResult, error) {
	servers := ch.config.MCP.Servers
	if len(servers) == 0 {
		return NewMenuResult("No MCP servers configured."), nil
	}

	var sb strings.Builder
	sb.WriteString("Configured MCP servers:\n\n")

	for name, server := range servers {
		status := "enabled"
		if server.Disabled {
			status = "disabled"
		}
		sb.WriteString(fmt.Sprintf("- %s (%s, %s)\n", name, server.Type, status))
		if desc := strings.TrimSpace(server.Description); desc != "" {
			sb.WriteString(fmt.Sprintf("  \u2514 %s\n", desc))
		}
		switch strings.ToLower(server.Type) {
		case "openapi":
			if server.OpenAPI != nil {
				sb.WriteString(fmt.Sprintf("  \u2514 spec: %s\n", server.OpenAPI.SpecPath))
			}
		case "command":
			if server.Command != nil {
				sb.WriteString(fmt.Sprintf("  \u2514 command: %s\n", strings.Join(server.Command.Exec, " ")))
			}
		case "openai":
			if server.OpenAI != nil {
				sb.WriteString(fmt.Sprintf("  \u2514 model: %s\n", server.OpenAI.Model))
			}
		}
		sb.WriteString("\n")
	}

	return NewMenuResult(strings.TrimRight(sb.String(), "\n")), nil
}

func (ch *CommandHandler) handleMCPAddOpenAPI(args []string) (MenuResult, error) {
	if len(args) < 2 {
		return MenuResult{}, fmt.Errorf("usage: /mcp add-openapi <name> <spec_path_or_url> [options]")
	}

	name := args[0]
	spec := args[1]
	baseURL := ""
	description := ""
	headers := make(map[string]string)
	queries := make(map[string]string)

	for i := 2; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "--base-url="):
			baseURL = strings.TrimPrefix(arg, "--base-url=")
		case arg == "--base-url" && i+1 < len(args):
			i++
			baseURL = args[i]
		case strings.HasPrefix(arg, "--header="):
			key, val, err := splitKeyValue(strings.TrimPrefix(arg, "--header="), ':')
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid header: %w", err)
			}
			headers[key] = val
		case arg == "--header" && i+1 < len(args):
			i++
			key, val, err := splitKeyValue(args[i], ':')
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid header: %w", err)
			}
			headers[key] = val
		case strings.HasPrefix(arg, "--query="):
			key, val, err := splitKeyValue(strings.TrimPrefix(arg, "--query="), '=')
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid query: %w", err)
			}
			queries[key] = val
		case arg == "--query" && i+1 < len(args):
			i++
			key, val, err := splitKeyValue(args[i], '=')
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid query: %w", err)
			}
			queries[key] = val
		case strings.HasPrefix(arg, "--description="):
			description = strings.TrimPrefix(arg, "--description=")
		case arg == "--description" && i+1 < len(args):
			i++
			description = args[i]
		default:
			return MenuResult{}, fmt.Errorf("unknown option for add-openapi: %s", arg)
		}
	}

	ch.ensureMCPServerMap()
	ch.config.MCP.Servers[name] = &config.MCPServerConfig{
		Type:        "openapi",
		Description: description,
		OpenAPI: &config.MCPOpenAPIConfig{
			SpecPath:       spec,
			URL:            baseURL,
			DefaultHeaders: headers,
			DefaultQuery:   queries,
		},
	}

	if err := ch.persistMCPChanges(); err != nil {
		return MenuResult{}, err
	}

	return NewMenuResult(fmt.Sprintf("Registered OpenAPI MCP server '%s'.", name)), nil
}

func (ch *CommandHandler) handleMCPAddCommand(args []string) (MenuResult, error) {
	if len(args) < 2 {
		return MenuResult{}, fmt.Errorf("usage: /mcp add-command <name> <command ...> [options]")
	}

	name := args[0]
	commandParts := make([]string, 0)
	var (
		cwd         string
		description string
		timeout     = 60
		envVars     = make(map[string]string)
	)

	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "--cwd="):
			cwd = strings.TrimPrefix(arg, "--cwd=")
		case arg == "--cwd" && i+1 < len(args):
			i++
			cwd = args[i]
		case strings.HasPrefix(arg, "--env="):
			key, val, err := splitKeyValue(strings.TrimPrefix(arg, "--env="), '=')
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid env: %w", err)
			}
			envVars[key] = val
		case arg == "--env" && i+1 < len(args):
			i++
			key, val, err := splitKeyValue(args[i], '=')
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid env: %w", err)
			}
			envVars[key] = val
		case strings.HasPrefix(arg, "--timeout="):
			valStr := strings.TrimPrefix(arg, "--timeout=")
			seconds, err := strconv.Atoi(valStr)
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid timeout: %w", err)
			}
			timeout = seconds
		case arg == "--timeout" && i+1 < len(args):
			i++
			seconds, err := strconv.Atoi(args[i])
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid timeout: %w", err)
			}
			timeout = seconds
		case strings.HasPrefix(arg, "--description="):
			description = strings.TrimPrefix(arg, "--description=")
		case arg == "--description" && i+1 < len(args):
			i++
			description = args[i]
		default:
			commandParts = append(commandParts, arg)
		}
	}

	if len(commandParts) == 0 {
		return MenuResult{}, fmt.Errorf("missing command payload for add-command")
	}

	ch.ensureMCPServerMap()
	ch.config.MCP.Servers[name] = &config.MCPServerConfig{
		Type:        "command",
		Description: description,
		Command: &config.MCPCommandConfig{
			Exec:           commandParts,
			WorkingDir:     cwd,
			Env:            envVars,
			TimeoutSeconds: timeout,
		},
	}

	if err := ch.persistMCPChanges(); err != nil {
		return MenuResult{}, err
	}

	return NewMenuResult(fmt.Sprintf("Registered command MCP server '%s'.", name)), nil
}

func (ch *CommandHandler) handleMCPAddOpenAI(args []string) (MenuResult, error) {
	if len(args) < 2 {
		return MenuResult{}, fmt.Errorf("usage: /mcp add-openai <name> <model> [options]")
	}

	name := args[0]
	model := args[1]
	var (
		description string
		apiKey      string
		apiKeyEnv   string
		baseURL     string
		system      string
		temp        = 1.0
		maxOutput   = 0
		jsonMode    = false
	)

	for i := 2; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "--api-key="):
			apiKey = strings.TrimPrefix(arg, "--api-key=")
		case arg == "--api-key" && i+1 < len(args):
			i++
			apiKey = args[i]
		case strings.HasPrefix(arg, "--api-key-env="):
			apiKeyEnv = strings.TrimPrefix(arg, "--api-key-env=")
		case arg == "--api-key-env" && i+1 < len(args):
			i++
			apiKeyEnv = args[i]
		case strings.HasPrefix(arg, "--base-url="):
			baseURL = strings.TrimPrefix(arg, "--base-url=")
		case arg == "--base-url" && i+1 < len(args):
			i++
			baseURL = args[i]
		case strings.HasPrefix(arg, "--temperature="):
			val := strings.TrimPrefix(arg, "--temperature=")
			parsed, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid temperature: %w", err)
			}
			temp = parsed
		case arg == "--temperature" && i+1 < len(args):
			i++
			parsed, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid temperature: %w", err)
			}
			temp = parsed
		case strings.HasPrefix(arg, "--max-output="):
			val := strings.TrimPrefix(arg, "--max-output=")
			parsed, err := strconv.Atoi(val)
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid max-output: %w", err)
			}
			maxOutput = parsed
		case arg == "--max-output" && i+1 < len(args):
			i++
			parsed, err := strconv.Atoi(args[i])
			if err != nil {
				return MenuResult{}, fmt.Errorf("invalid max-output: %w", err)
			}
			maxOutput = parsed
		case strings.HasPrefix(arg, "--system="):
			system = strings.TrimPrefix(arg, "--system=")
		case arg == "--system" && i+1 < len(args):
			i++
			system = args[i]
		case strings.HasPrefix(arg, "--description="):
			description = strings.TrimPrefix(arg, "--description=")
		case arg == "--description" && i+1 < len(args):
			i++
			description = args[i]
		case arg == "--json":
			jsonMode = true
		default:
			return MenuResult{}, fmt.Errorf("unknown option for add-openai: %s", arg)
		}
	}

	if apiKey == "" && apiKeyEnv == "" {
		log.Println("[MCP] no API key provided for openai tool; expecting environment variable at runtime")
	}

	ch.ensureMCPServerMap()
	ch.config.MCP.Servers[name] = &config.MCPServerConfig{
		Type:        "openai",
		Description: description,
		OpenAI: &config.MCPOpenAIConfig{
			Model:        model,
			APIKey:       apiKey,
			APIKeyEnvVar: apiKeyEnv,
			BaseURL:      baseURL,
			SystemPrompt: system,
			Temperature:  temp,
			MaxOutput:    maxOutput,
			ResponseJSON: jsonMode,
		},
	}

	if err := ch.persistMCPChanges(); err != nil {
		return MenuResult{}, err
	}

	return NewMenuResult(fmt.Sprintf("Registered OpenAI MCP server '%s'.", name)), nil
}

func (ch *CommandHandler) handleMCPRemove(args []string) (MenuResult, error) {
	if len(args) == 0 {
		return MenuResult{}, fmt.Errorf("usage: /mcp remove <name>")
	}

	name := args[0]
	if _, ok := ch.config.MCP.Servers[name]; !ok {
		return MenuResult{}, fmt.Errorf("unknown MCP server: %s", name)
	}
	delete(ch.config.MCP.Servers, name)

	if err := ch.persistMCPChanges(); err != nil {
		return MenuResult{}, err
	}

	return NewMenuResult(fmt.Sprintf("Removed MCP server '%s'.", name)), nil
}

func (ch *CommandHandler) handleMCPEnableDisable(args []string, disable bool) (MenuResult, error) {
	if len(args) == 0 {
		if disable {
			return MenuResult{}, fmt.Errorf("usage: /mcp disable <name>")
		}
		return MenuResult{}, fmt.Errorf("usage: /mcp enable <name>")
	}

	name := args[0]
	server, ok := ch.config.MCP.Servers[name]
	if !ok {
		return MenuResult{}, fmt.Errorf("unknown MCP server: %s", name)
	}
	server.Disabled = disable

	if err := ch.persistMCPChanges(); err != nil {
		return MenuResult{}, err
	}

	var statusText string
	if disable {
		statusText = "Disabled"
	} else {
		statusText = "Enabled"
	}

	return NewMenuResult(fmt.Sprintf("%s MCP server '%s'.", statusText, name)), nil
}

func (ch *CommandHandler) ensureMCPServerMap() {
	if ch.config.MCP.Servers == nil {
		ch.config.MCP.Servers = make(map[string]*config.MCPServerConfig)
	}
}

func (ch *CommandHandler) persistMCPChanges() error {
	if err := ch.config.Save(config.GetConfigPath()); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if ch.orchestrator != nil {
		errList := ch.orchestrator.RefreshMCPTools()
		if len(errList) > 0 {
			messages := make([]string, 0, len(errList))
			for _, err := range errList {
				if err != nil {
					messages = append(messages, err.Error())
				}
			}
			if len(messages) > 0 {
				return fmt.Errorf("some MCP tools failed to initialize: %s", strings.Join(messages, "; "))
			}
		}
	}
	return nil
}

func splitKeyValue(input string, sep rune) (string, string, error) {
	parts := strings.SplitN(input, string(sep), 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected KEY%[1]cVALUE format", sep)
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", fmt.Errorf("key cannot be empty")
	}
	return key, val, nil
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
  Ctrl+B        - Background shell job
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
