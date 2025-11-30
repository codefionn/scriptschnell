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

Test the codebase with `-short` flag to skip slow integration tests.

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
- **create_file**: Create new files
- **write_file_diff**: Update existing files with unified diffs (headers + `@@` hunks by default; GPT models get a simplified parser that tolerates missing hunks)
- **shell**: Execute shell commands (supports background jobs with `&`)
- **go_sandbox**: Execute Go code in sandboxed environment
- **parallel_tools**: Execute several registered tools concurrently and merge responses
- **todo**: Manage todo items
- **status**: Check status of background jobs

### Critical Safety Rules

1. **Read-Before-Write**: Files must be read before modification ([internal/tools/write_file_diff.go](internal/tools/write_file_diff.go#L73) or the simplified parser variant in [internal/tools/write_file_simple_diff.go](internal/tools/write_file_simple_diff.go#L60))
   - Session tracks which files were read via `TrackFileRead()`
   - Write operations check `WasFileRead()` before allowing modifications
   - New files can be written without reading

2. **Line Limits**: Maximum 2000 lines per read operation ([internal/tools/read_file.go](internal/tools/read_file.go:96-104))
   - Files exceeding limit are automatically truncated with notification
   - Use `from_line` and `to_line` parameters to read specific ranges

3. **Timeouts**:
   - Sandbox execution: 30s default, 600s max

4. **Sandbox Shell Helper**: When writing Go sandbox programs, call `Shell` with a command slice (e.g., `Shell([]string{"ls", "-la"}, "")`). The earlier `Shell("ls -la")` form is deprecated and will be rejected.

### Configuration

Configuration files stored in `~/.config/statcode-ai/`:

- **config.json**: Application settings (working_dir, cache_ttl_seconds, temperature, max_tokens)
- **providers.json**: Provider API keys and model selections

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
- **fsnotify**: Filesystem watcher for cache invalidation
- **ahocorasick**: Efficient model name search

## Testing

No test files currently exist in the repository. When writing tests:

- Use `MockFS` for filesystem operations
- Test actor message handling with controlled contexts
- Mock LLM responses for tool execution tests
