# Unix Socket Protocol Design

## Overview

This document defines the communication protocol, message format, and overall architecture for the Unix socket server in scriptschnell. The socket server enables multiple frontends to connect to a single running instance, supporting concurrent sessions and workspaces.

## Design Goals

1. **Multiple Concurrent Sessions**: Support multiple clients with independent sessions
2. **Workspace Isolation**: Each session operates in its own workspace context
3. **Shared Resources**: Reuse actor system, session storage, and domain blocker across clients
4. **Frontend Agnostic**: Protocol should work with TUI, CLI, and potentially new frontends
5. **Security**: Authentication and authorization for socket clients
6. **Compatibility**: Align with existing web WebSocket protocol where possible

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Unix Socket Server                             │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │              SocketListener Actor                         │  │
│  │  - Accepts new socket connections                        │  │
│  │  - Validates authentication                              │  │
│  │  - Spawns SocketConnection actors                        │  │
│  └──────────────────────────────────────────────────────────┘  │
│                            │                                    │
│                            ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │            Connection Manager                             │  │
│  │  - Tracks active connections                              │  │
│  │  - Maps ConnectionID → SocketConnection                    │  │
│  │  - Handles cleanup on disconnect                          │  │
│  └──────────────────────────────────────────────────────────┘  │
│                            │                                    │
│         ┌──────────────────┼──────────────────┐                │
│         ▼                  ▼                  ▼                │
│  ┌───────────┐      ┌───────────┐      ┌───────────┐           │
│  │  Client 1 │      │  Client 2 │      │  Client N │           │
│  │Connection│      │Connection│      │Connection│           │
│  └─────┬─────┘      └─────┬─────┘      └─────┬─────┘           │
│        │                  │                  │                  │
│        ▼                  ▼                  ▼                  │
│  ┌───────────┐      ┌───────────┐      ┌───────────┐           │
│  │  Session  │      │  Session  │      │  Session  │           │
│  │    A      │      │    B      │      │    N      │           │
│  └─────┬─────┘      └─────┬─────┘      └─────┬─────┘           │
│        │                  │                  │                  │
│        └──────────────────┼──────────────────┘                  │
│                           ▼                                     │
│              ┌─────────────────────┐                            │
│              │  Shared Resources   │                            │
│              │  - Actor System     │                            │
│              │  - Session Storage  │                            │
│              │  - Domain Blocker   │                            │
│              │  - Provider Manager │                            │
│              │  - Config           │                            │
│              └─────────────────────┘                            │
│                           │                                     │
│                           ▼                                     │
│              ┌─────────────────────┐                            │
│              │     Orchestrators   │                            │
│              │  (one per session)  │                            │
│              └─────────────────────┘                            │
└─────────────────────────────────────────────────────────────────┘
```

## Protocol Format

### Wire Format

All messages use JSON with newline delimiters (JSON-RPC style):

```
{"type":"message_type","data":{...},"request_id":"uuid"}\n
```

### Base Message Structure

```go
type SocketMessage struct {
    Type      string                 `json:"type"`       // Message type identifier
    RequestID string                 `json:"request_id,omitempty"` // Correlation ID for request/response
    Data      map[string]interface{} `json:"data"`       // Message-specific data
    Timestamp string                 `json:"timestamp,omitempty"` // ISO 8601 timestamp
    Error     *ErrorInfo             `json:"error,omitempty"` // Error information (for responses)
}

type ErrorInfo struct {
    Code    string `json:"code"`    // Error code
    Message string `json:"message"` // Human-readable error message
    Details string `json:"details,omitempty"` // Additional error details
}
```

## Message Types

### Handshake & Authentication

#### `auth_request`
Client sends authentication challenge or token.

```json
{
  "type": "auth_request",
  "data": {
    "client_type": "tui|cli|custom",
    "version": "1.0.0",
    "capabilities": ["progress", "authorization", "questions"],
    "token": "optional_pre_shared_token"
  },
  "request_id": "uuid"
}
```

#### `auth_response`
Server responds with authentication result.

```json
{
  "type": "auth_response",
  "request_id": "uuid",
  "data": {
    "success": true,
    "connection_id": "conn_abc123",
    "server_version": "1.0.0",
    "server_capabilities": ["progress", "authorization", "questions", "sessions"]
  },
  "error": null
}
```

**Authentication Methods:**

1. **Token-based**: Pre-shared token (insecure but simple)
2. **File permissions**: Socket file permissions control access (recommended)
3. **Challenge-response**: Client proves knowledge of secret
4. **Peer credentials**: Unix peer credentials (UID/GID based)

### Session Management

#### `session_create`
Create a new session in the specified workspace.

```json
{
  "type": "session_create",
  "data": {
    "workspace": "/path/to/workspace",
    "session_id": "optional_specific_id",
    "options": {
      "auto_save": true,
      "save_interval_seconds": 300
    }
  },
  "request_id": "uuid"
}
```

#### `session_create_response`

```json
{
  "type": "session_create_response",
  "request_id": "uuid",
  "data": {
    "session_id": "bright-silver-falcon",
    "status": "created|attached",
    "workspace": "/path/to/workspace",
    "working_dir": "/path/to/workspace"
  }
}
```

#### `session_attach`
Attach to an existing session (if supported for collaborative editing).

```json
{
  "type": "session_attach",
  "data": {
    "session_id": "bright-silver-falcon"
  },
  "request_id": "uuid"
}
```

#### `session_detach`
Detach from current session without destroying it.

```json
{
  "type": "session_detach",
  "request_id": "uuid"
}
```

#### `session_list`
List all active sessions.

```json
{
  "type": "session_list",
  "data": {
    "workspace": "/path/to/workspace" // optional, filter by workspace
  },
  "request_id": "uuid"
}
```

#### `session_list_response`

```json
{
  "type": "session_list_response",
  "request_id": "uuid",
  "data": {
    "sessions": [
      {
        "session_id": "bright-silver-falcon",
        "workspace": "/path/to/workspace",
        "created_at": "2024-01-15T10:30:00Z",
        "message_count": 42,
        "status": "active|idle"
      }
    ]
  }
}
```

#### `session_delete`
Delete a session.

```json
{
  "type": "session_delete",
  "data": {
    "session_id": "bright-silver-falcon",
    "workspace": "/path/to/workspace"
  },
  "request_id": "uuid"
}
```

### Chat & Generation

#### `chat_send`
Send a user message to the current session.

```json
{
  "type": "chat_send",
  "data": {
    "content": "Write a Go function to parse JSON",
    "options": {
      "temperature": 0.7,
      "max_tokens": 2000
    }
  },
  "request_id": "uuid"
}
```

#### `chat_stop`
Stop current generation.

```json
{
  "type": "chat_stop",
  "request_id": "uuid"
}
```

#### `chat_clear`
Clear the current session (reset conversation).

```json
{
  "type": "chat_clear",
  "request_id": "uuid"
}
```

#### `chat_message` (Server → Client)
Streaming chat message from assistant.

```json
{
  "type": "chat_message",
  "data": {
    "role": "assistant",
    "content": "Here's a Go function to parse JSON:",
    "stream_id": "stream_abc",
    "chunk_index": 0,
    "is_final": false
  }
}
```

### Tool Interactions

#### `tool_call` (Server → Client)
Notification that a tool is being called.

```json
{
  "type": "tool_call",
  "data": {
    "tool_name": "read_file",
    "tool_id": "call_abc123",
    "parameters": {
      "path": "main.go"
    },
    "description": "Reading main.go"
  }
}
```

#### `tool_result` (Server → Client)
Result of tool execution.

```json
{
  "type": "tool_result",
  "data": {
    "tool_id": "call_abc123",
    "result": "package main\n...",
    "error": null,
    "status": "completed|failed"
  }
}
```

#### `tool_compact` (Server → Client)
Compact format combining call and result.

```json
{
  "type": "tool_compact",
  "data": {
    "tool_id": "call_abc123",
    "tool_name": "read_file",
    "status": "calling|completed|error",
    "result": "package main\n...",
    "error": null,
    "description": "Reading main.go"
  }
}
```

### Authorization

#### `authorization_request` (Server → Client)
Request user authorization for a tool operation.

```json
{
  "type": "authorization_request",
  "data": {
    "auth_id": "auth_123",
    "tool_name": "execute_command",
    "parameters": {
      "command": "rm -rf /"
    },
    "reason": "This command will delete files"
  }
}
```

#### `authorization_ack` (Client → Server)
Acknowledge that authorization dialog was displayed.

```json
{
  "type": "authorization_ack",
  "data": {
    "auth_id": "auth_123"
  }
}
```

#### `authorization_response` (Client → Server)
User's approval/denial.

```json
{
  "type": "authorization_response",
  "data": {
    "auth_id": "auth_123",
    "approved": true
  }
}
```

### Question Dialogs (Planning Agent)

#### `question_request` (Server → Client)
Ask the user a question during planning.

```json
{
  "type": "question_request",
  "data": {
    "question_id": "q_123",
    "question": "Which approach should I use?",
    "multi_mode": false,
    "questions": [ // Only present in multi_mode
      {
        "question": "Question 1?",
        "options": ["A", "B", "C"]
      }
    ]
  }
}
```

#### `question_response` (Client → Server)

Single question:
```json
{
  "type": "question_response",
  "data": {
    "question_id": "q_123",
    "answer": "Option A"
  }
}
```

Multiple questions:
```json
{
  "type": "question_response",
  "data": {
    "question_id": "q_123",
    "answers_map": {
      "q_1": "Option A",
      "q_2": "Option B"
    }
  }
}
```

### Progress Updates

#### `progress` (Server → Client)
Progress update during generation.

```json
{
  "type": "progress",
  "data": {
    "message": "Calling tool: read_file",
    "context_usage": 75,
    "ephemeral": false,
    "verification_agent": false
  }
}
```

### Configuration

#### `config_get`
Get configuration information.

```json
{
  "type": "config_get",
  "data": {
    "keys": ["model", "working_dir", "temperature"]
  },
  "request_id": "uuid"
}
```

#### `config_set`
Set configuration options (scoped to session/workspace).

```json
{
  "type": "config_set",
  "data": {
    "model": "claude-3-opus-20240229",
    "temperature": 0.7
  },
  "request_id": "uuid"
}
```

### Workspace Management

#### `workspace_list`
List available workspaces (from config).

```json
{
  "type": "workspace_list",
  "request_id": "uuid"
}
```

#### `workspace_list_response`

```json
{
  "type": "workspace_list_response",
  "request_id": "uuid",
  "data": {
    "workspaces": [
      {
        "path": "/home/user/project1",
        "hash": "a1b2c3d4",
        "context_directories": ["/usr/share/doc"]
      }
    ]
  }
}
```

#### `workspace_set`
Set the current workspace for the connection.

```json
{
  "type": "workspace_set",
  "data": {
    "workspace": "/path/to/workspace"
  },
  "request_id": "uuid"
}
```

### Session Persistence

#### `session_save`
Save the current session.

```json
{
  "type": "session_save",
  "data": {
    "name": "Implement JSON parser"
  },
  "request_id": "uuid"
}
```

#### `session_load`
Load a saved session.

```json
{
  "type": "session_load",
  "data": {
    "session_id": "bright-silver-falcon",
    "workspace": "/path/to/workspace"
  },
  "request_id": "uuid"
}
```

### Connection Lifecycle

#### `ping` / `pong`
Keepalive messages.

```json
{"type":"ping","timestamp":"2024-01-15T10:30:00Z"}
{"type":"pong","timestamp":"2024-01-15T10:30:00.123Z"}
```

#### `close` (Client → Server)
Graceful connection close.

```json
{
  "type": "close",
  "data": {
    "reason": "client_shutdown",
    "preserve_session": true
  }
}
```

#### `closed` (Server → Client)
Server closing the connection.

```json
{
  "type": "closed",
  "data": {
    "reason": "server_shutdown",
    "reconnect": false
  }
}
```

## Session Lifecycle Management

### States

```
┌──────────┐    auth    ┌───────────────┐    create    ┌───────────┐
│ Connected├───────────►│ Authenticated├─────────────►│ Created  │
└──────────┘            └───────────────┘              └─────┬─────┘
                                                          │
                                                          │ chat_send
                                                          ▼
                                                   ┌────────────┐
                                                   │   Active   │
                                                   └─────┬──────┘
                                                         │
                    ┌─────────────────────┬──────────────┴────────┐
                    │                     │                       │
                    ▼                     ▼                       ▼
               ┌─────────┐          ┌──────────┐            ┌──────────┐
               │   Idle  │          │Stopping  │            │  Detach  │
               └────┬────┘          └─────┬────┘            └────┬─────┘
                    │                     │                       │
                    │ chat_send           │                       │
                    │                     │                       ▼
                    └─────────────────────┘                  ┌────────┐
                                                           │ Deleted│
                                                           └────────┘
```

### State Transitions

| From State | To State | Trigger | Action |
|------------|----------|---------|--------|
| Connected | Authenticated | Successful auth | Allocate connection ID |
| Authenticated | Created | `session_create` | Initialize session & orchestrator |
| Created | Active | `chat_send` | Start generation |
| Active | Idle | Generation complete | Wait for next input |
| Idle | Active | `chat_send` | Resume generation |
| Active | Stopping | `chat_stop` | Cancel in-flight operations |
| Stopping | Idle | Stop complete | Ready for next input |
| Active/Detached | Deleted | `session_delete` | Cleanup session |
| Any | Closed | Connection lost | Cleanup resources |

### Multi-Session Support

Each connection maintains:
- `connectionID`: Unique identifier for the connection
- `sessionID`: ID of the attached session (if any)
- `workspace`: Current workspace path
- `clientType`: Type of client (tui, cli, custom)

Session ownership model:
- **Exclusive ownership**: Only one connection can send commands to a session
- **Observer mode**: Multiple connections can observe the same session (read-only)
- **Handover**: Connection can transfer ownership to another connection

## Workspace Attachment/Detachment

### Workspace Context

Each session is associated with a workspace:
- `workspace`: Root directory of the workspace
- `working_dir`: Current working directory within workspace
- `context_directories`: Additional documentation directories

### Workspace Management Flow

```
1. Connection established
   └─> No workspace set

2. Client sends: workspace_set
   └─> Server validates workspace exists
   └─> Server loads workspace config (context dirs, permissions)
   └─> Connection now has workspace context

3. Client sends: session_create
   └─> Session inherits workspace from connection
   └─> Session can also override workspace in the request

4. Client sends: workspace_set (while session active)
   └─> Error: Cannot change workspace while session is attached
   └─> Or: Detach session first, then change workspace

5. Connection closes
   └─> Session persists if preserve_session=true
   └─> Workspace context lost (must re-attach to reconnect)
```

### Workspace Isolation

- Each session has its own `workingDir`
- Shared resources (actor system, session storage) are workspace-aware
- File permissions and sandbox rules are workspace-scoped
- Authorized domains/commands are workspace-scoped in config

## Authentication & Authorization

### Authentication Methods

#### 1. File Permission-Based (Recommended)

Socket file permissions control who can connect:

```bash
# Create socket with restricted permissions
chmod 600 ~/.scriptschnell.sock  # Only owner
chmod 660 ~/.scriptschnell.sock  # Owner and group
```

**Advantages:**
- Leverages Unix security model
- No additional token management
- Simple to audit

**Implementation:**
- Server creates socket with `0600` by default
- Configurable via `socket_permissions` in config
- Server validates peer UID/GID on accept (optional)

#### 2. Token-Based

Pre-shared token sent in `auth_request`:

```go
type AuthRequest struct {
    Token string `json:"token"`
}
```

**Token Storage:**
- In config file: `socket_token` field
- In environment variable: `SCRIPTSCHNELL_SOCKET_TOKEN`
- Or generated on server start and stored in a file

**Advantages:**
- Works across users (if permissions allow)
- Easy to revoke by changing token

#### 3. Challenge-Response

```go
// Server sends challenge
type AuthChallenge struct {
    Nonce string `json:"nonce"` // Random bytes
    Timestamp int64 `json:"timestamp"`
}

// Client responds
type AuthResponse struct {
    Nonce     string `json:"nonce"`
    Signature string `json:"signature"` // HMAC-SHA256(nonce, shared_secret)
}
```

**Advantages:**
- Prevents replay attacks
- Shared secret never transmitted

#### 4. Peer Credentials

Use `SO_PEERCRED` to validate UID/GID:

```go
// Get Unix domain socket credentials
func getPeerCredentials(conn *net.UnixConn) (*unix.Ucred, error) {
    file, err := conn.File()
    if err != nil {
        return nil, err
    }
    defer file.Close()

    ucred, err := unix.GetsockoptUcred(int(file.Fd()), unix.SOL_SOCKET, unix.SO_PEERCRED)
    if err != nil {
        return nil, err
    }

    return ucred, nil
}
```

**Advantages:**
- Strong OS-level security
- No shared secrets required

### Authorization

Once authenticated, authorization is session-scoped:

- **Tool execution**: Handled by existing authorization mechanism (via callbacks)
- **File access**: Handled by sandbox and filesystem permissions
- **Session access**: Clients can only access sessions they created or have been granted access to

### Recommended Configuration

Default: File permission-based + optional peer credential validation

```json
{
  "socket": {
    "path": "~/.scriptschnell.sock",
    "permissions": "0600",
    "require_peer_credentials": true,
    "allowed_uids": [1000, 1001],
    "allowed_gids": [1000]
  }
}
```

## Error Handling

### Error Codes

| Code | Description |
|------|-------------|
| `AUTH_FAILED` | Authentication failed |
| `AUTH_REQUIRED` | Authentication required |
| `INVALID_REQUEST` | Malformed or invalid request |
| `SESSION_NOT_FOUND` | Session does not exist |
| `SESSION_EXISTS` | Session with given ID already exists |
| `WORKSPACE_INVALID` | Workspace path does not exist |
| `WORKSPACE_ACCESS_DENIED` | No permission to access workspace |
| `OPERATION_NOT_ALLOWED` | Operation not allowed in current state |
| `INTERNAL_ERROR` | Server-side error |
| `TIMEOUT` | Operation timed out |
| `NOT_IMPLEMENTED` | Feature not implemented |

### Error Response Format

```json
{
  "type": "error",
  "request_id": "uuid",
  "error": {
    "code": "SESSION_NOT_FOUND",
    "message": "Session 'bright-silver-falcon' not found",
    "details": "Available sessions: ..."
  }
}
```

## Concurrency Model

### Connection Isolation

Each connection is handled independently:
- Each connection gets its own goroutine for reading/writing
- Locks protect shared state (connection manager, session registry)
- Channels for inter-connection communication (if needed)

### Session Isolation

Each session has its own:
- `Orchestrator` instance
- Message channels
- Authorization state
- File operation tracking

### Shared Resources

The following are shared across all connections:
- `actor.System`: Manages all actors
- `sessionStorageRef`: Session persistence
- `domainBlockerRef`: Domain blocking
- `providerMgr`: LLM provider management
- `cfg`: Global configuration

**Synchronization:**
- Actors are thread-safe (use message passing)
- Config has built-in mutex
- Session storage actor serializes access

## Performance Considerations

### Message Batching

For rapid message sequences (e.g., streaming), batch messages:

```json
{
  "type": "batch",
  "data": {
    "messages": [
      {"type": "tool_call", ...},
      {"type": "progress", ...},
      {"type": "tool_result", ...}
    ]
  }
}
```

### Backpressure

Client can signal backpressure:

```json
{
  "type": "flow_control",
  "data": {
    "pause": true
  }
}
```

Server respects pause and stops sending non-critical messages.

### Connection Limits

Configurable limits:
- `max_connections`: Maximum concurrent connections (default: 10)
- `max_sessions_per_connection`: Max sessions per connection (default: 1)
- `connection_timeout`: Idle timeout (default: 5 minutes)

## Security Considerations

### Socket File Location

Default: `~/.scriptschnell.sock`

Alternative locations:
- `/tmp/scriptschnell-$UID.sock` (multi-user system)
- `$XDG_RUNTIME_DIR/scriptschnell.sock` (systemd)
- Project-specific: `.git/scriptschnell.sock` (per-repo)

### File Permissions

- Default: `0600` (owner only)
- Configurable via `socket.permissions`
- Validate on startup

### Peer Credential Validation

Optionally validate UID/GID:
- Check against allowed list in config
- Useful for multi-user systems

### Rate Limiting

Per-connection rate limits:
- Messages per second
- Concurrent generations

### Logging

- Log all connection events (connect, auth, disconnect)
- Log session lifecycle events
- Log errors with connection ID

## Configuration

### Config Schema

```go
type SocketConfig struct {
    Enabled               bool              `json:"enabled"`
    AutoConnect           bool              `json:"auto_connect"` // auto-detect and connect to socket server
    Path                  string            `json:"path"`
    Permissions           string            `json:"permissions"` // octal string "0600"
    RequireAuth           bool              `json:"require_auth"`
    AuthMethod            string            `json:"auth_method"` // "file", "token", "challenge", "peercred"
    Token                 string            `json:"token,omitempty"`
    AllowedUIDs           []int             `json:"allowed_uids,omitempty"`
    AllowedGIDs           []int             `json:"allowed_gids,omitempty"`
    MaxConnections        int               `json:"max_connections"`
    MaxSessionsPerConn    int               `json:"max_sessions_per_connection"`
    ConnectionTimeoutSecs int               `json:"connection_timeout_seconds"`
    EnableBatching        bool              `json:"enable_batching"`
    BatchSize             int               `json:"batch_size"`
    LogLevel              string            `json:"log_level"`
}
```

### Default Config

```json
{
  "socket": {
    "enabled": true,
    "auto_connect": true,
    "path": "~/.scriptschnell.sock",
    "permissions": "0600",
    "require_auth": false,
    "auth_method": "file",
    "max_connections": 10,
    "max_sessions_per_connection": 1,
    "connection_timeout_seconds": 300,
    "enable_batching": true,
    "batch_size": 10,
    "log_level": "info"
  }
}
```

## Protocol Versioning

### Version Negotiation

Client includes version in `auth_request`:
```json
{
  "version": "1.0.0"
}
```

Server responds with supported version range:
```json
{
  "version": "1.0.0",
  "min_compatible_version": "1.0.0"
}
```

### Version Changes

| Version | Changes |
|---------|---------|
| 1.0.0 | Initial protocol |
| 1.1.0 | Add batching support |
| 1.2.0 | Add observer mode |

## Implementation Plan

See separate task for implementation details. Key components:

1. `internal/socket/server.go` - Socket server implementation
2. `internal/socket/connection.go` - Per-connection handler
3. `internal/socket/protocol.go` - Protocol message types
4. `internal/socket/auth.go` - Authentication logic
5. Integration with `cmd/scriptschnell/main.go` for socket mode

## Comparison with Web WebSocket Protocol

| Aspect | Unix Socket | WebSocket |
|--------|-------------|-----------|
| Transport | Unix domain socket | TCP + WebSocket |
| Authentication | File perms, peercred, token | Query token |
| Message format | JSON + newline | JSON frame |
| Concurrency | Multiple connections | Multiple connections |
| Streaming | Yes | Yes |
| Browser support | No | Yes |
| Native apps | Yes | Yes |
| Security | Unix permissions | TLS + token |
| Performance | Higher (no TCP overhead) | Good |

The Unix socket protocol is designed to be compatible with the web WebSocket protocol where possible, with message types and data structures aligned for consistency.