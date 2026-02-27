# Project Overview

A TUI/CLI application that generates code from prompts using LLMs, featuring multiple frontends (TUI, CLI, Web, ACP) and an actor-based architecture for concurrent tool execution.

# Project Context

- **Language**: Go
- **Language Version**: 1.25.1
- **Framework**: Bubbletea (TUI), wazero (WebAssembly runtime)

- 

## Dependencies

- bubbletea v1.3.10 (TUI framework)
- wazero v1.11.0 (WebAssembly runtime with WASI P2 support)
- anthropic-sdk-go v1.22.1 (Anthropic API)
- openai-go v1.12.0 (OpenAI API)
- tree-sitter-go v0.25.0, tree-sitter-python v0.25.0, tree-sitter-typescript v0.23.2 (syntax parsing)
- templ v0.3.977 (templating)
- httprouter v1.3.0 (HTTP routing)
- fsnotify v1.9.0 (filesystem watching)

## Tooling

- **Build**: `go build ./...`
- **Test**: `go test ./... -short`
- **Format**: `gofmt -s -w .`
- **Lint**: `golangci-lint run`
- **Install**: `go install ./cmd/scriptschnell`
