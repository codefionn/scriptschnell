# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build the binary
go build -o statcode-ai ./cmd/statcode-ai

# Run simple tests
go test ./... -short

# Run in TUI mode
./statcode-ai

# Run in CLI mode (single-shot)
./statcode-ai "your prompt here"

# Install globally
go install ./cmd/statcode-ai
```

## Architecture Overview

StatCode AI is an AI-assisted programming TUI built with Go, using the **actor model** for concurrent, isolated component communication. The application supports both TUI and CLI modes.

### Core Architectural Patterns

#### Actor Model Implementation

The codebase uses a custom actor model ([internal/actor/actor.go](internal/actor/actor.go:1-200)) for managing concurrent components:

- **ActorRef**: Wraps actors with mailboxes and message processing loops
- **Actor System**: Manages actor lifecycle (spawn, stop, message routing)
- **Message passing**: Non-blocking Send() with buffered mailboxes
- Each actor runs in its own goroutine with isolated state

Key actors in the system:
- **Orchestrator Actor**: Manages LLM interactions and tool execution
- **Tool Actors**: Execute individual tools (read_file, create_file, write_file_diff, shell, etc.)
- **Session Actor**: Manages conversation state and file tracking
- **FS Actor**: Handles filesystem operations with caching

#### Dual LLM System

The application uses two separate LLM models ([internal/llm/client.go](internal/llm/client.go:1-49)):

1. **Orchestration Model**: Main conversation, tool calls, reasoning (e.g., Claude 3.5 Sonnet, GPT-4)
2. **Summarization Model**: Fast summarization of large files (e.g., Claude 3 Haiku, GPT-3.5)

This separation optimizes cost and performance - expensive models only where needed.

#### Provider Interface System

The provider interface ([internal/llm/provider.go](internal/llm/provider.go)) enables dynamic model discovery:

- **OpenAI Provider** ([internal/llm/openai_provider.go](internal/llm/openai_provider.go)): Fetches models from OpenAI API
- **Anthropic Provider** ([internal/llm/anthropic_provider.go](internal/llm/anthropic_provider.go)): Curated Claude models list
- **Google Provider** ([internal/llm/google_provider.go](internal/llm/google_provider.go)): Lists Gemini models from the Google Generative Language API (v1beta)
- **OpenRouter Provider** ([internal/llm/openrouter_provider.go](internal/llm/openrouter_provider.go)): Integrates the OpenRouter catalog (OpenAI-compatible API gateway)
- **Mistral Provider** ([internal/llm/mistral_provider.go](internal/llm/mistral_provider.go)): Fetches Mistral AI models from their API (OpenAI-compatible)
- **Provider Manager** ([internal/provider/provider.go](internal/provider/provider.go)): Manages providers and model listings

Key features:
- `ListModels()`: Fetches available models from provider APIs
- `CreateClient()`: Creates LLM clients for specific models
- `ValidateAPIKey()`: Tests API key validity
- Automatically refreshes model lists when adding providers

See [PROVIDER_INTERFACE.md](PROVIDER_INTERFACE.md) for detailed documentation.

#### Filesystem Abstraction Layer

All filesystem operations go through the `FileSystem` interface ([internal/fs/fs.go](internal/fs/fs.go:1-430)):

- **CachedFS**: Production implementation with smart caching
  - Only **directory listings** are cached (with TTL)
  - **File reads** always fetch from disk (real-time data)
  - Uses `fsnotify` to auto-invalidate cache on filesystem changes
- **MockFS**: In-memory implementation for testing
- Cache management: LRU eviction when hitting maxEntries

**Important**: File reads are never cached - only directory listings are cached for performance.

#### Session Management

Sessions track conversation state and enforce safety rules ([internal/session/session.go](internal/session/session.go:1-147)):

- **FilesRead**: Map of files read in session (required for write operations)
- **FilesModified**: Tracks which files were changed
- **BackgroundJobs**: Manages long-running shell commands
- **Thread-safe**: All operations use read/write locks

#### Tool Registry System

Tools are registered in a central registry ([internal/tools/tools.go](internal/tools/tools.go:1-135)):

- Each tool implements the `Tool` interface (Name, Description, Parameters, Execute)
- Registry converts tools to JSON schema for LLM consumption
- Tools receive parameters as `map[string]interface{}` from LLM
- Helper functions: `GetStringParam()`, `GetIntParam()`, `GetBoolParam()`

Available tools:
- **read_file**: Read files with line range support (max 2000 lines)
- **read_file_summarized**: AI-powered summarization of large files
- **create_file**: Create new files from scratch
- **write_file_diff**: Update existing files using unified diffs (always include file headers and hunk markers; GPT models may omit `@@` hunks thanks to a simplified parser). Example:
  ```
  --- a/internal/example.txt
  +++ b/internal/example.txt
  @@ -1,3 +1,4 @@
   line one
  -line two
  +updated line two
   line three
  +new line four
  ```
- **shell**: Execute shell commands (supports background jobs with `&`)
- **go_sandbox**: Execute Go code in sandboxed WebAssembly environment
  - **Builder Pattern** ([internal/tools/sandbox_builder.go](internal/tools/sandbox_builder.go)): Fluent API for programmatic use
    - Type-safe builder with method chaining: `NewSandboxBuilder().SetCode(code).SetTimeout(30).Execute(ctx)`
    - Methods: `SetCode()`, `AddLibrary()`, `SetTimeout()`, `SetAuthorization()`, `AllowDomain()`, etc.
    - Validation, cloning, and batch execution support
    - See [SANDBOX_BUILDER.md](SANDBOX_BUILDER.md) for complete API documentation
  - **TinyGo Compiler Integration** ([internal/tools/tinygo_manager.go](internal/tools/tinygo_manager.go)): Automatic download and caching
    - Downloads TinyGo compiler on first use (no system Go installation required)
    - **TinyGo is REQUIRED** - standard Go only supports WASI P1, we need WASI P2
    - Platform-specific cache directories:
      - Linux: `~/.cache/statcode-ai/tinygo/`
      - macOS: `~/Library/Caches/statcode-ai/tinygo/`
      - Windows: `%LOCALAPPDATA%\statcode-ai\tinygo\`
    - Cache persists across sessions for faster compilation
    - Download progress shown in TUI status bar
  - Compiles to WASI P2 target using TinyGo (Component Model support)
  - Uses wazero runtime for WASM execution
  - **HTTP Authorization System** - Multi-layered network interception:
    1. **Code Injection Layer** ([internal/wasi/code_wrapper.go](internal/wasi/code_wrapper.go)): Wraps user code with authorization checks before compilation
       - Injects custom `http.RoundTripper` that intercepts all HTTP requests
       - Calls host function `authorize_domain` for each network request
       - Provides `http.DefaultClient` replacement with authorization
    2. **Host Function Layer** ([internal/wasi/host_state.go](internal/wasi/host_state.go)): Provides `authorize_domain` import to WASM
       - Reads domain string from WASM memory
       - Calls authorization actor for LLM-based safety analysis
       - Returns 1 (authorized) or 0 (denied) to WASM code
    3. **Host Sandbox Layer**: WASI Preview 1 execution provides no socket APIs, so any attempt
       - TinyGo builds target `wasip1`, which lacks outbound network primitives
       - Additional host checks ensure HTTP calls go through authorization layer
  - Domain authorization enforced before any HTTP request
  - Controlled filesystem access only to files previously read via read_file tool
  - **Note**: Go net/http in WASM doesn't use WASI sockets directly - authorization happens at HTTP layer
- **parallel_tools**: Execute several registered tools concurrently and merge responses
- **todo**: Manage todo items
- **status**: Check status of background jobs

### Critical Safety Rules

1. **Read-Before-Write**: Files must be read before modification ([internal/tools/write_file_diff.go](internal/tools/write_file_diff.go#L73))
   - Session tracks which files were read via `TrackFileRead()`
   - Write operations check `WasFileRead()` before allowing modifications
   - New files can be written without reading

2. **Shell Command Authorization**: Shell commands are analyzed for safety before execution ([internal/tools/authorization_actor.go](internal/tools/authorization_actor.go:510-608))
   - Uses the **summarization model** to judge if commands are harmless or require user approval
   - Potentially harmful commands (git commit, git push, rm, curl, etc.) require user authorization
   - Harmless commands (ls, cat, grep, git status, go build, go test, etc.) are auto-approved
   - Command prefixes can be permanently authorized at session-level or project-level:
     - **Session-level**: `session.AuthorizeCommand("git commit")` - lasts for current session
     - **Project-level**: Stored in `config.json` under `authorized_commands` - permanent for this project
   - LLM generates suggested prefixes for permanent authorization (e.g., "git commit" for all git commits)
   - Authorization checks can be bypassed with `AuthorizationOptions.AllowedCommands` for specific command prefixes

3. **Network Domain Authorization**: Domains accessed by sandboxed Go code are analyzed for safety ([internal/tools/authorization_actor.go](internal/tools/authorization_actor.go:404-508))
   - Uses the **summarization model** to judge if domains are safe or require user approval
   - Safe domains (github.com, googleapis.com, npmjs.org, pkg.go.dev, etc.) are auto-approved
   - Unknown or suspicious domains require user authorization
   - Domain patterns can be permanently authorized at session-level or project-level:
     - **Session-level**: `session.AuthorizeDomain("example.com")` - lasts for current session
     - **Project-level**: Stored in `config.json` under `authorized_domains` - permanent for this project
   - LLM generates wildcard patterns for permanent authorization (e.g., "*.github.com" for all GitHub subdomains)
   - Authorization checks can be bypassed with `AuthorizationOptions.AllowedDomains` for specific domains/patterns
   - **Implementation Architecture**:
     - **Code Wrapping**: User Go code is automatically wrapped with HTTP authorization layer before compilation
     - **HTTP Interception**: Custom `http.RoundTripper` intercepts all `http.DefaultClient` requests
     - **Host Communication**: WASM code calls `authorize_domain` host function via `//go:wasmimport`
     - **Memory Bridge**: Host reads domain string from WASM linear memory for authorization check
     - **Runtime Decision**: Authorization actor makes real-time safety decision for each domain
     - **Defensive Layer**: WASI P1 socket functions (`sock_open`, `sock_connect`) are blocked as fallback
   - **How it works**:
     1. User writes Go code with `http.Get("https://example.com")`
     2. Code wrapper injects authorization layer before compilation
     3. At runtime, HTTP client calls `authorize_domain` host function
     4. Host function queries authorization actor with LLM analysis
     5. If denied, HTTP request returns 403 Forbidden without network access
     6. If approved, request proceeds through normal Go HTTP stack

4. **Line Limits**: Maximum 2000 lines per read operation ([internal/tools/read_file.go](internal/tools/read_file.go:96-104))
   - Files exceeding limit are automatically truncated with notification
   - Use `from_line` and `to_line` parameters to read specific ranges

5. **Timeouts**:
   - Sandbox execution: 30s default, 600s max

6. **Sandbox Shell Helper**: When writing Go sandbox programs, call `Shell` with a command slice (e.g., `Shell([]string{"ls", "-la"}, "")`). The earlier `Shell("ls -la")` form is deprecated and will be rejected.

### Configuration

Configuration files stored in `~/.config/statcode-ai/`:

- **config.json**: Application settings (working_dir, cache_ttl_seconds, temperature, log_level, log_path)
  - **Note**: `max_tokens` is deprecated and automatically retrieved from model metadata via `GetModelMaxOutputTokens()`
  - **authorized_domains**: Permanently authorized domains for network access (map of domain -> bool)
  - **authorized_commands**: Permanently authorized shell command prefixes for this project (map of prefix -> bool)
- **providers.json**: Provider API keys and model selections
  - Each model includes `context_window` (input) and `max_output_tokens` fetched from provider APIs or estimated based on model ID
  - For OpenRouter and cross-provider support, max output tokens are intelligently estimated based on model patterns (e.g., gpt-5: 128K, Claude 3.5: 8K)

### Logging System

The application includes a comprehensive logging system ([internal/logger/logger.go](internal/logger/logger.go)) with configurable log levels:

- **Log Levels**: debug, info, warn, error, none
- **Configuration**: Set `log_level` and `log_path` in config.json
- **Default**: INFO level, logs to `~/.config/statcode-ai/statcode-ai.log`
- **Features**:
  - Timestamp and level for each log entry
  - Component-specific prefixes via `WithPrefix()`
  - Thread-safe logging
  - File-based output with append mode

See [LOGGING.md](LOGGING.md) for detailed logging documentation.

Usage in code:
```go
import "github.com/statcode-ai/statcode-ai/internal/logger"

logger.Debug("detailed info: %s", value)
logger.Info("normal operation: %s", value)
logger.Warn("warning: %s", value)
logger.Error("error: %v", err)

// Component-specific logger
componentLogger := logger.Global().WithPrefix("mycomponent")
```

### Entry Points

- **TUI Mode** ([cmd/statcode-ai/main.go](cmd/statcode-ai/main.go:61-116)): Interactive Bubbletea interface
- **CLI Mode** ([cmd/statcode-ai/main.go](cmd/statcode-ai/main.go:53-59)): Single-shot prompt execution

### Provider System

Multi-provider support via langchaingo ([internal/provider/provider.go](internal/provider/provider.go)):

- Supports OpenAI, Anthropic, and other langchaingo-compatible providers
- Provider manager handles API key storage and model selection
- Model search uses Aho-Corasick algorithm for efficient matching

## Development Workflow

When modifying the codebase:

1. **Adding a new tool**:
   - Create struct implementing `Tool` interface in `internal/tools/`
   - Implement Name(), Description(), Parameters(), Execute()
   - Register in orchestrator's tool registry
   - Update README.md with tool documentation

2. **Modifying filesystem behavior**:
   - Changes go in `internal/fs/fs.go`
   - Ensure both CachedFS and MockFS implementations updated
   - Consider cache invalidation implications

3. **Changing LLM integration**:
   - Update `internal/llm/client.go` interface if needed
   - Modify langchaingo wrapper in `internal/llm/langchain.go`
   - Test with multiple providers (OpenAI, Anthropic)

4. **Actor system changes**:
   - Actors must be thread-safe (use mutexes)
   - Messages should be immutable
   - Always handle context cancellation in Receive()

## Project Structure

```
statcode-ai/
├── cmd/statcode-ai/          # Main entry point
├── internal/
│   ├── actor/                # Actor model implementation
│   ├── cli/                  # CLI mode
│   ├── config/               # Configuration management
│   ├── fs/                   # Filesystem abstraction (CachedFS, MockFS)
│   ├── llm/                  # LLM client interface + langchaingo wrapper
│   ├── provider/             # Provider management (API keys, models)
│   ├── session/              # Session state management
│   ├── tools/                # LLM tools (read_file, create_file, write_file_diff, shell, etc.)
│   └── tui/                  # TUI implementation (Bubbletea)
```

## Dependencies

- **bubbletea**: TUI framework
- **langchaingo**: LLM integration (OpenAI, Anthropic, etc.)
- **wasmer-go**: WebAssembly runtime with WASI support and custom host function hooks for sandboxed code execution
- **fsnotify**: Filesystem watcher for cache invalidation
- **ahocorasick**: Efficient model name search

## Testing

No test files currently exist in the repository. When writing tests:

- Use `MockFS` for filesystem operations
- Test actor message handling with controlled contexts
- Mock LLM responses for tool execution tests
