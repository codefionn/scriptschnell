package tui

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/tools"
)

type commandHelpEntry struct {
	Usage       string
	Description string
}

type commandDefinition struct {
	Name               string
	Description        string
	Suggestions        []string
	PlaceholderExample string
	HelpEntries        []commandHelpEntry
	Handler            func(*CommandHandler, []string) (MenuResult, error)
}

func getDefaultCommandDefinitions() []commandDefinition {
	return []commandDefinition{
		{
			Name:               "/help",
			Description:        "Show this help message",
			Suggestions:        []string{"/help"},
			PlaceholderExample: "/help",
			Handler:            (*CommandHandler).handleHelpCommand,
		},
		{
			Name:        "/settings",
			Description: "Open main settings menu (providers, models, search)",
			Suggestions: []string{"/settings"},
			Handler:     (*CommandHandler).handleSettings,
		},
		{
			Name:               "/models",
			Description:        "Interactive model selector (orchestration) and refresh utilities",
			Suggestions:        []string{"/models", "/models refresh"},
			PlaceholderExample: "/models",
			HelpEntries: []commandHelpEntry{
				{
					Usage:       "/models",
					Description: "Interactive model selector (orchestration)",
				},
				{
					Usage:       "/models refresh",
					Description: "Fetch latest models from provider APIs",
				},
			},
			Handler: (*CommandHandler).handleModels,
		},
		{
			Name:               "/provider",
			Description:        "Interactive provider management",
			Suggestions:        []string{"/provider"},
			PlaceholderExample: "/provider",
			Handler:            (*CommandHandler).handleProvider,
		},
		{
			Name:               "/init",
			Description:        "Initialize/update AGENTS.md with codebase summary",
			Suggestions:        []string{"/init"},
			PlaceholderExample: "/init",
			Handler:            (*CommandHandler).handleInit,
		},
		{
			Name:        "/clear",
			Description: "Clear conversation, todos, and start a new session",
			Suggestions: []string{"/clear"},
			Handler:     (*CommandHandler).handleClear,
		},
		{
			Name:               "/quit",
			Description:        "Quit the application",
			Suggestions:        []string{"/quit"},
			PlaceholderExample: "/quit",
			Handler:            (*CommandHandler).handleQuitCommand,
		},
		{
			Name:        "/mcp",
			Description: "Manage custom MCP servers (/mcp help for subcommands)",
			Suggestions: []string{"/mcp"},
			Handler:     (*CommandHandler).handleMCP,
		},
		{
			Name:        "/context",
			Description: "Manage context directories (/context help for subcommands)",
			Suggestions: []string{"/context"},
			Handler:     (*CommandHandler).handleContext,
		},
		{
			Name:        "/session",
			Description: "Open session management menu",
			Suggestions: []string{"/session"},
			Handler:     (*CommandHandler).handleSession,
		},
		{
			Name:               "/new",
			Description:        "Create new session tab with optional name (creates git worktree if named)",
			Suggestions:        []string{"/new", "/new <name>"},
			PlaceholderExample: "/new feature-auth",
			Handler:            (*CommandHandler).handleNew,
		},
	}
}

// CommandHandler handles TUI commands
type CommandHandler struct {
	providerMgr      *provider.Manager
	orchestrator     *Orchestrator // Deprecated: Use factory + getActiveTab instead
	config           *config.Config
	ctx              context.Context
	progressCallback ProgressCallback
	contextCallback  ContextUsageCallback
	usageCallback    OpenRouterUsageCallback
	commands         map[string]commandDefinition

	// Multi-tab support
	factory             *RuntimeFactory
	getActiveTab        func() *TabSession
	getProgressCallback func() progress.Callback
}

func NewCommandHandler(ctx context.Context, cfg *config.Config, providerMgr *provider.Manager, orchestrator *Orchestrator) *CommandHandler {
	handler := &CommandHandler{
		config:       cfg,
		providerMgr:  providerMgr,
		orchestrator: orchestrator,
		ctx:          ctx,
	}
	handler.initCommands()
	return handler
}

func (ch *CommandHandler) initCommands() {
	definitions := getDefaultCommandDefinitions()
	ch.commands = make(map[string]commandDefinition, len(definitions))
	for _, def := range definitions {
		ch.commands[def.Name] = def
	}
}

// SetFactory sets the RuntimeFactory for multi-tab support
func (ch *CommandHandler) SetFactory(factory *RuntimeFactory) {
	ch.factory = factory
}

// SetGetActiveTab sets the function to get the active tab
func (ch *CommandHandler) SetGetActiveTab(fn func() *TabSession) {
	ch.getActiveTab = fn
}

// SetGetProgressCallback sets the function to get the progress callback for the active tab
func (ch *CommandHandler) SetGetProgressCallback(fn func() progress.Callback) {
	ch.getProgressCallback = fn
}

// SetProgressCallback sets the callback for progress/status updates
func (ch *CommandHandler) SetProgressCallback(callback ProgressCallback) {
	ch.progressCallback = callback
}

// SetContextCallback sets the callback for context usage updates
func (ch *CommandHandler) SetContextCallback(callback ContextUsageCallback) {
	ch.contextCallback = callback
}

func (ch *CommandHandler) SetUsageCallback(callback OpenRouterUsageCallback) {
	ch.usageCallback = callback
}

// HandleCommand processes a command
func (ch *CommandHandler) HandleCommand(command string) (MenuResult, error) {
	command = strings.TrimSpace(command)
	if !strings.HasPrefix(command, "/") {
		return MenuResult{}, fmt.Errorf("commands must start with /")
	}

	parts := strings.Fields(command)
	cmd := parts[0]

	definition, ok := ch.commands[cmd]
	if !ok {
		return MenuResult{}, fmt.Errorf("unknown command: %s. Type /help for available commands", cmd)
	}

	if definition.Handler == nil {
		return MenuResult{}, fmt.Errorf("command %s is not implemented", cmd)
	}

	return definition.Handler(ch, parts[1:])
}

func (ch *CommandHandler) handleHelpCommand(_ []string) (MenuResult, error) {
	return NewMenuResult(ch.buildHelpMessage()), nil
}

func (ch *CommandHandler) buildHelpMessage() string {
	entries := commandHelpEntries()
	maxWidth := 0
	for _, entry := range entries {
		if len(entry.Usage) > maxWidth {
			maxWidth = len(entry.Usage)
		}
	}

	sb := acquireBuilder()
	sb.WriteString("Available Commands:\n\n")
	for _, entry := range entries {
		sb.WriteString(fmt.Sprintf("%-*s - %s\n", maxWidth, entry.Usage, entry.Description))
	}
	sb.WriteString(helpFooter)
	return builderString(sb)
}

func (ch *CommandHandler) handleQuitCommand(_ []string) (MenuResult, error) {
	return MenuResult{}, ErrQuitRequested
}

const helpFooter = `
Keyboard Shortcuts:

Ctrl+X            - Enter command mode
Ctrl+B            - Background current shell job
Ctrl+C (×2)       - Quit application
Ctrl+E            - Toggle tool mode (navigate tool calls)
ESC               - Stop current generation
Shift+Enter       - Insert newline in prompt
Alt+Enter         - Alternate newline shortcut
Enter             - Submit prompt or command

Tool Mode Shortcuts (Ctrl+E to enable):
  j/k             - Next/previous tool message
  g/G             - First/last tool message
  e               - Expand/collapse tool result
  E/C             - Expand/collapse all results
  y               - Copy output to clipboard
  Y               - Copy full result to clipboard
  ESC             - Exit tool mode

Quick Start:
1. Configure settings: /settings
2. Add a provider and select models
3. Optionally configure web search
4. Start chatting!

Model Information:
- Orchestration: Used for main conversation and tool calls
- Summarize: Used for file summarization tasks
`

func commandHelpEntries() []commandHelpEntry {
	definitions := getDefaultCommandDefinitions()
	entries := make([]commandHelpEntry, 0, len(definitions))
	for _, def := range definitions {
		if len(def.HelpEntries) > 0 {
			entries = append(entries, def.HelpEntries...)
			continue
		}
		entries = append(entries, commandHelpEntry{
			Usage:       def.Name,
			Description: def.Description,
		})
	}
	return entries
}

func availableCommandSuggestions() []string {
	definitions := getDefaultCommandDefinitions()
	suggestions := make([]string, 0, len(definitions))
	seen := make(map[string]struct{})
	for _, def := range definitions {
		entries := def.Suggestions
		if len(entries) == 0 {
			entries = []string{def.Name}
		}
		for _, entry := range entries {
			if _, ok := seen[entry]; ok {
				continue
			}
			suggestions = append(suggestions, entry)
			seen[entry] = struct{}{}
		}
	}
	return suggestions
}

func commandPlaceholderExamples() []string {
	definitions := getDefaultCommandDefinitions()
	examples := make([]string, 0, len(definitions))
	seen := make(map[string]struct{})
	for _, def := range definitions {
		example := def.PlaceholderExample
		if example == "" {
			continue
		}
		if _, ok := seen[example]; ok {
			continue
		}
		examples = append(examples, example)
		seen[example] = struct{}{}
	}
	return examples
}

func commandModePlaceholder() string {
	examples := commandPlaceholderExamples()
	if len(examples) == 0 {
		return "Enter command:"
	}
	return fmt.Sprintf("Enter command: %s", strings.Join(examples, ", "))
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

	sb := acquireBuilder()
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

	return NewMenuResult(strings.TrimRight(builderString(sb), "\n")), nil
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

func (ch *CommandHandler) handleSettings(_ []string) (MenuResult, error) {
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

		sb := acquireBuilder()
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
		return NewMenuResult(builderString(sb)), nil

	case "menu":
		if len(args) < 2 {
			return MenuResult{}, fmt.Errorf("usage: /models menu <orchestration|summarize|safety>")
		}
		modelType := args[1]
		if modelType != "orchestration" && modelType != "summarize" && modelType != "safety" {
			return MenuResult{}, fmt.Errorf("unknown model type: %s (use 'orchestration', 'summarize', or 'safety')", modelType)
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

func (ch *CommandHandler) handleInit(_ []string) (MenuResult, error) {
	// Get active tab
	var tab *TabSession
	var orch *Orchestrator
	var progressCallback progress.Callback

	if ch.getActiveTab != nil {
		tab = ch.getActiveTab()
		if tab != nil {
			// Get or create runtime for this tab (same logic as startPromptForTab)
			if ch.factory != nil && tab.Runtime == nil {
				var ok bool
				tab.Runtime, ok = ch.factory.GetTabRuntime(tab.ID)
				if !ok {
					var err error
					tab.Runtime, err = ch.factory.CreateTabRuntime(tab.ID, tab.Session)
					if err != nil {
						return MenuResult{}, fmt.Errorf("failed to create runtime for tab %d: %w", tab.ID, err)
					}
					logger.Info("Created runtime for tab %d in handleInit", tab.ID)
				}
			}
			if tab.Runtime != nil {
				orch = tab.Runtime.Orchestrator
			}
		}
	} else if ch.orchestrator != nil {
		// Fallback for non-multi-tab mode
		orch = ch.orchestrator
	}

	// Get progress callback
	if ch.getProgressCallback != nil {
		progressCallback = ch.getProgressCallback()
	} else {
		// Fallback for non-multi-tab mode
		progressCallback = ch.progressCallback
	}

	// Check if orchestration model is configured
	orchModelID := ch.providerMgr.GetOrchestrationModel()
	if orchModelID == "" {
		return MenuResult{}, fmt.Errorf("no orchestration model configured. Use /models to set one")
	}

	// Check if we have an orchestrator
	if orch == nil {
		return MenuResult{}, fmt.Errorf("no orchestrator available in this context")
	}

	// Check if we have a progress callback
	if progressCallback == nil {
		return MenuResult{}, fmt.Errorf("streaming not available in this context")
	}

	dispatch := func(update progress.Update) {
		if err := progress.Dispatch(progressCallback, update); err != nil {
			log.Printf("Failed to send progress update: %v", err)
		}
	}

	// Get init prompt
	initPrompt := orch.GetInitPrompt()

	// Set initial status
	dispatch(progress.Update{Message: "Analyzing codebase to generate AGENTS.md", Mode: progress.ReportJustStatus, Ephemeral: true})

	// Process through orchestrator with streaming (in background)
	go func() {
		if err := orch.ProcessPromptWithVerification(ch.ctx, initPrompt, progressCallback, ch.contextCallback, nil, nil, nil, ch.usageCallback); err != nil {
			// Error will be handled by orchestrator's error handling
			// Clear status on error
			dispatch(progress.Update{Message: "", Mode: progress.ReportJustStatus, Ephemeral: true})
			return
		}
		// Clear status on success
		dispatch(progress.Update{Message: "", Mode: progress.ReportJustStatus, Ephemeral: true})
	}()

	return NewMenuResult("Analyzing codebase to generate AGENTS.md..."), nil
}

func (ch *CommandHandler) handleClear(_ []string) (MenuResult, error) {
	// Get active tab
	var tab *TabSession
	var orch *Orchestrator

	if ch.getActiveTab != nil {
		tab = ch.getActiveTab()
		if tab != nil && tab.Runtime != nil {
			orch = tab.Runtime.Orchestrator
		}
	} else if ch.orchestrator != nil {
		// Fallback for non-multi-tab mode
		orch = ch.orchestrator
	}

	if orch == nil {
		return MenuResult{}, fmt.Errorf("no active session to clear")
	}

	// Auto-save the current session before clearing if it has messages
	saved := false
	currentSession := orch.GetSession()
	if currentSession != nil && len(currentSession.GetMessages()) > 0 {
		// Generate session title (best-effort)
		if err := orch.GenerateSessionTitle(ch.ctx); err != nil {
			logger.Warn("handleClear: failed to generate title: %v", err)
		}

		name := actor.GenerateSessionName("")

		// Try saving via actor first, fall back to direct storage
		if storageRef, exists := orch.GetActor("session_storage"); exists {
			if err := actor.SaveSessionViaActor(ch.ctx, storageRef, currentSession, name); err != nil {
				logger.Warn("handleClear: failed to auto-save session via actor: %v", err)
			} else {
				logger.Info("handleClear: auto-saved session %s as '%s'", currentSession.ID, name)
				saved = true
			}
		} else if storage, err := session.NewSessionStorage(); err == nil {
			if err := storage.SaveSession(currentSession, name); err != nil {
				logger.Warn("handleClear: failed to auto-save session directly: %v", err)
			} else {
				logger.Info("handleClear: auto-saved session %s as '%s' (direct)", currentSession.ID, name)
				saved = true
			}
		}
	}

	// Clear the session in the orchestrator
	if err := orch.ClearSession(); err != nil {
		return MenuResult{}, fmt.Errorf("failed to clear session: %w", err)
	}

	// If we have a tab and factory, create a fresh session
	if tab != nil && ch.factory != nil {
		// Destroy old runtime
		if tab.Runtime != nil {
			if err := ch.factory.DestroyTabRuntime(tab.ID); err != nil {
				logger.Warn("Failed to destroy runtime during clear: %v", err)
			}
			tab.Runtime = nil
		}

		// Create new session with fresh ID
		sessionID := session.GenerateID()
		workingDir := ch.factory.GetWorkingDir()
		if tab.WorktreePath != "" {
			workingDir = tab.WorktreePath
		}
		newSession := session.NewSession(sessionID, workingDir)
		tab.Session = newSession

		logger.Info("Created new session %s for tab %d after clear", sessionID, tab.ID)
	}

	if saved {
		return NewMenuResult("Session saved and cleared. Starting fresh conversation."), nil
	}
	return NewClearSessionResult(), nil
}

func (ch *CommandHandler) handleContext(args []string) (MenuResult, error) {
	if ch.config == nil {
		return MenuResult{}, fmt.Errorf("configuration unavailable")
	}

	if len(args) == 0 || args[0] == "help" {
		return NewMenuResult(ch.contextHelp()), nil
	}

	subCmd := strings.ToLower(args[0])
	switch subCmd {
	case "list":
		return ch.handleContextList()
	case "add":
		return ch.handleContextAdd(args[1:])
	case "remove":
		return ch.handleContextRemove(args[1:])
	default:
		return MenuResult{}, fmt.Errorf("unknown /context subcommand: %s", subCmd)
	}
}

func (ch *CommandHandler) contextHelp() string {
	return `Context Directory Commands:

/context list
    Show configured context directories.

/context add <directory>
    Add a directory to the context directories list. This makes external documentation
    or library sources available to the AI via search_context_files, grep_context_files,
    and read_context_file tools.

/context remove <directory>
    Remove a directory from the context directories list.

Context directories are stored per-project and persist across sessions.
Use absolute paths or paths relative to the working directory.

Examples:
  /context add /usr/share/doc/python3
  /context add ~/projects/my-library/docs
  /context remove /usr/share/doc/python3
`
}

func (ch *CommandHandler) handleContextList() (MenuResult, error) {
	contextDirs := ch.config.GetContextDirectories(ch.config.WorkingDir)
	if len(contextDirs) == 0 {
		return NewMenuResult("No context directories configured for this workspace."), nil
	}

	sb := acquireBuilder()
	sb.WriteString("Configured context directories:\n\n")

	for i, dir := range contextDirs {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, dir))
	}

	sb.WriteString(fmt.Sprintf("\nTotal: %d context director", len(contextDirs)))
	if len(contextDirs) == 1 {
		sb.WriteString("y")
	} else {
		sb.WriteString("ies")
	}

	return NewMenuResult(builderString(sb)), nil
}

func (ch *CommandHandler) handleContextAdd(args []string) (MenuResult, error) {
	if len(args) == 0 {
		return MenuResult{}, fmt.Errorf("usage: /context add <directory>")
	}

	// Join all args to support paths with spaces
	dir := strings.Join(args, " ")
	dir = strings.TrimSpace(dir)

	if dir == "" {
		return MenuResult{}, fmt.Errorf("directory path cannot be empty")
	}

	// Check if the directory is the user's home directory
	isHomeDir, err := tools.IsHomeDirectory(dir)
	if err != nil {
		return MenuResult{}, fmt.Errorf("failed to validate directory: %w", err)
	}
	if isHomeDir {
		return MenuResult{}, fmt.Errorf("cannot add home directory as context directory for security reasons; add a subdirectory instead (e.g., ~/Documents, ~/projects)")
	}

	// Add to config for current workspace
	ch.config.AddContextDirectory(ch.config.WorkingDir, dir)

	// Save config
	if err := ch.config.Save(config.GetConfigPath()); err != nil {
		return MenuResult{}, fmt.Errorf("failed to save config: %w", err)
	}

	return NewMenuResult(fmt.Sprintf("Added context directory: %s\n\nThe AI can now search and read files in this directory using:\n- search_context_files\n- grep_context_files\n- read_context_file", dir)), nil
}

func (ch *CommandHandler) handleContextRemove(args []string) (MenuResult, error) {
	if len(args) == 0 {
		return MenuResult{}, fmt.Errorf("usage: /context remove <directory>")
	}

	// Join all args to support paths with spaces
	dir := strings.Join(args, " ")
	dir = strings.TrimSpace(dir)

	if dir == "" {
		return MenuResult{}, fmt.Errorf("directory path cannot be empty")
	}

	// Remove from config for current workspace
	removed := ch.config.RemoveContextDirectory(ch.config.WorkingDir, dir)
	if !removed {
		return MenuResult{}, fmt.Errorf("context directory not found: %s", dir)
	}

	// Save config
	if err := ch.config.Save(config.GetConfigPath()); err != nil {
		return MenuResult{}, fmt.Errorf("failed to save config: %w", err)
	}

	return NewMenuResult(fmt.Sprintf("Removed context directory: %s", dir)), nil
}

func (ch *CommandHandler) handleSession(_ []string) (MenuResult, error) {
	return NewSessionMenuResult(), nil
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

// handleNew creates a new session tab
func (ch *CommandHandler) handleNew(args []string) (MenuResult, error) {
	var name string
	if len(args) > 0 {
		name = strings.Join(args, " ")
	}

	// Validation will be done in the TUI's handleNewTab method
	return NewNewTabResult(name), nil
}
