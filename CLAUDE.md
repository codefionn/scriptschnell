# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build the binary
go build -o scriptschnell ./cmd/scriptschnell

# Run simple tests
go test ./... -short

# Run in TUI mode
./scriptschnell

# Run in CLI mode (single-shot)
./scriptschnell "your prompt here"

# Run in Agent Client Protocol (ACP) mode for code editor integration
./scriptschnell --acp

# Install globally
go install ./cmd/scriptschnell

# Format files
gofmt -s -w .
```

## Architecture Overview

scriptschnell is an AI-assisted programming TUI built with Go, using the **actor model** for concurrent, isolated component communication. The application supports both TUI and CLI modes.

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
- **Session Storage Actor**: Handles persistent storage and retrieval of conversation sessions

#### Dual LLM System

The application uses two separate LLM models ([internal/llm/client.go](internal/llm/client.go:1-49)):

1. **Orchestration Model**: Main conversation, tool calls, reasoning (e.g., Claude 3.5 Sonnet, GPT-4o, o3-mini)
2. **Summarization Model**: Fast summarization of large files (e.g., Claude Haiku 4.5, Gemini 2.0 Flash)

This separation optimizes cost and performance - expensive models only where needed.

#### Planning Agent

The planning agent ([internal/planning/agent.go](internal/planning/agent.go)) performs a pre-loop analysis for complex tasks:

- **Planning Decision**: Before the main loop, the **Summarization Model** analyzes the prompt to decide:
  1. If planning is required (complex/multi-step vs simple/single-step)
  2. Which external MCP tools are relevant and should be allowed
- **Planning Execution**: If triggered, the Planning Agent:
  - Uses the **Planning Model** (or Orchestrator Model fallback)
  - Can investigate the codebase using read-only tools
  - Can ask clarifying questions (in TUI mode)
  - Generates a step-by-step plan that is fed into the main session context
- **MCP Access**: The planning agent has restricted access to MCP tools, filtered by the summarization model's decision to ensure relevance and safety.

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

#### Session Persistence

Sessions can be saved and loaded using gob format with workspace isolation ([internal/session/storage.go](internal/session/storage.go) and [internal/actor/session_storage_actor.go](internal/actor/session_storage_actor.go)):

- **Workspace Isolation**: Sessions are stored per workspace using SHA256 hash of workspace path
- **Storage Location**: Platform-specific state directories:
  - Linux: `$XDG_STATE_HOME/scriptschnell/sessions/` or `~/.local/state/scriptschnell/sessions/`
  - macOS: `~/Library/Application Support/scriptschnell/sessions/`
  - Windows: `%LOCALAPPDATA%\\scriptschnell\\sessions\\`
- **Storage Format**: gob-encoded binary with versioning for forward compatibility
- **Atomic Writes**: Sessions are saved to `.tmp` files then renamed for data safety
- **Session Metadata**: Includes ID, name, workspace, timestamps, and message count
- **Slash Commands**: Available in both TUI and ACP modes:
  - `/session save [name]` - Save current session with optional name
  - `/session load [id]` - Load session by ID (or list without ID)
  - `/session list` - Show all sessions for current workspace
  - `/session delete [id]` - Delete a saved session
- **Actor Integration**: Session storage handled by dedicated SessionStorageActor for concurrent access

**Important**: Only sessions from the current workspace are visible/accessible for security and organization.

#### Syntax Validation

Automatic syntax validation using tree-sitter parsers ([internal/syntax/validator.go](internal/syntax/validator.go)):

- **Automatic Validation**: Syntax is automatically validated after file writes (`write_file_diff`, `create_file`)
- **Non-Blocking**: Validation warnings don't prevent file writes - LLM receives detailed error info but writes always succeed
- **Supported Languages**: Go, Python, TypeScript, JavaScript, TSX, JSX, Bash
  - Additional languages can be added by including tree-sitter grammar packages
- **Error Detection**: Uses tree-sitter's ERROR and MISSING nodes to identify syntax issues
  - **Note**: Tree-sitter has excellent error recovery - it may not flag all recoverable syntax errors
  - Unrecoverable errors (incomplete tokens, missing critical syntax) are always detected
- **Error Reporting**:
  - **LLM receives** JSON with line/column numbers and error descriptions
  - **UI displays** markdown-formatted warnings with error details
  - Limited to first 5 errors for readability

**Implementation**:
- Shared language detection ([internal/syntax/language.go](internal/syntax/language.go))
- Tree-sitter integration for parsing
- Validation results included in tool responses:
  ```json
  {
    "path": "main.go",
    "bytes_written": 150,
    "updated": true,
    "validation_warning": "Found 2 syntax error(s)..."
  }
  ```

**Disabling**: Validation is automatic for supported languages. To skip validation for specific files, use file extensions that don't match supported languages.

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
      - Linux: `$XDG_CACHE_HOME/scriptschnell/tinygo/` (or `~/.cache/scriptschnell/tinygo/`)
      - macOS: `~/Library/Caches/scriptschnell/tinygo/`
      - Windows: `%LOCALAPPDATA%\scriptschnell\tinygo\`
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

7. **Secret Detection**: The `secretdetect` package provides utilities to prevent secret leakage.
   - Use `secretdetect.Scan()` to check content before sending to LLMs
   - Use `secretdetect.Redact()` to mask secrets in UI output
   - Integrate into file reading and user input processing flows

### Configuration

Configuration files stored in platform-specific locations:

- Linux: `~/.config/scriptschnell/`
- Windows: `%APPDATA%\\scriptschnell\\`

- **config.json**: Application settings (working_dir, cache_ttl_seconds, temperature, log_level)
  - **Note**: `max_tokens` is deprecated and automatically retrieved from model metadata via `GetModelMaxOutputTokens()`
  - **authorized_domains**: Permanently authorized domains for network access (map of domain -> bool)
  - **authorized_commands**: Permanently authorized shell command prefixes for this project (map of prefix -> bool)
- **providers.json**: Provider API keys and model selections
  - Each model includes `context_window` (input) and `max_output_tokens` fetched from provider APIs or estimated based on model ID
  - For OpenRouter and cross-provider support, max output tokens are intelligently estimated based on model patterns (e.g., gpt-5: 128K, Claude 3.5: 8K)

### Logging System

The application includes a comprehensive logging system ([internal/logger/logger.go](internal/logger/logger.go)) with configurable log levels:

- **Log Levels**: debug, info, warn, error, none
- **Configuration**: Set `log_level` in config.json; log path is derived from the state directory and can be overridden via `STATCODE_LOG_PATH`
- **Default**: INFO level; on Linux logs to `$XDG_STATE_HOME/scriptschnell/scriptschnell.log` (or `~/.local/state/scriptschnell/scriptschnell.log`), on Windows to `%LOCALAPPDATA%\\scriptschnell\\scriptschnell.log` (or `~/AppData/Local/scriptschnell/scriptschnell.log`), and on other platforms to `~/.config/scriptschnell/scriptschnell.log`
- **Features**:
  - Timestamp and level for each log entry
  - Component-specific prefixes via `WithPrefix()`
  - Thread-safe logging
  - File-based output with append mode

See [LOGGING.md](LOGGING.md) for detailed logging documentation.

Usage in code:
```go
import "github.com/scriptschnell/scriptschnell/internal/logger"

logger.Debug("detailed info: %s", value)
logger.Info("normal operation: %s", value)
logger.Warn("warning: %s", value)
logger.Error("error: %v", err)

// Component-specific logger
componentLogger := logger.Global().WithPrefix("mycomponent")
```

### Entry Points

- **TUI Mode** ([cmd/scriptschnell/main.go](cmd/scriptschnell/main.go:61-116)): Interactive Bubbletea interface
- **CLI Mode** ([cmd/scriptschnell/main.go](cmd/scriptschnell/main.go:53-59)): Single-shot prompt execution

### Provider System

Multi-provider support

- Supports OpenAI, Anthropic and other
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
   - Test with multiple providers (OpenAI, Anthropic)

4. **Actor system changes**:
   - Actors must be thread-safe (use mutexes)
   - Messages should be immutable
   - Always handle context cancellation in Receive()

- Format Go code with `gofmt -s -w .` after making changes

## Project Structure

```
scriptschnell/
├── cmd/scriptschnell/          # Main entry point
├── internal/
│   ├── actor/                # Actor model implementation
│   ├── cli/                  # CLI mode
│   ├── config/               # Configuration management
│   ├── fs/                   # Filesystem abstraction (CachedFS, MockFS)
│   ├── llm/                  # LLM client interface
│   ├── provider/             # Provider management (API keys, models)
│   ├── secretdetect/         # Secret detection and redaction
│   ├── session/              # Session state management
│   ├── tools/                # LLM tools (read_file, create_file, write_file_diff, shell, etc.)
│   └── tui/                  # TUI implementation (Bubbletea)
```

## Dependencies

- **bubbletea**: TUI framework
- **wasmer-go**: WebAssembly runtime with WASI support and custom host function hooks for sandboxed code execution
- **fsnotify**: Filesystem watcher for cache invalidation
- **ahocorasick**: Efficient model name search

## Testing

No test files currently exist in the repository. When writing tests:

- Use `MockFS` for filesystem operations
- Test actor message handling with controlled contexts
- Mock LLM responses for tool execution tests
