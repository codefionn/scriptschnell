# Socket Client Library

The `internal/socketclient` package provides a reusable client library for connecting to the scriptschnell Unix socket server.

## Features

- Full protocol implementation for all message types
- Automatic reconnection with exponential backoff
- Callback-based API for streaming messages
- Session and workspace management
- Authorization and question dialog support
- Thread-safe concurrent operations
- Context support for cancellation and timeouts

## Usage Example

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/your-org/scriptschnell/internal/socketclient"
)

func main() {
    // Create client
    client, err := socketclient.NewClient("~/.scriptschnell.sock")
    if err != nil {
        panic(err)
    }

    // Set up callbacks
    client.SetChatMessageCallback(func(msg socketclient.ChatMessage) {
        fmt.Printf("[%s]: %s\n", msg.Role, msg.Content)
    })

    client.SetProgressCallback(func(progress socketclient.ProgressData) {
        if progress.IsCompact {
            fmt.Printf("\033[2K\r%s", progress.Message)
        } else {
            fmt.Println(progress.Message)
        }
    })

    client.SetStateChangedCallback(func(state socketclient.ConnectionState, err error) {
        fmt.Printf("State: %v\n", state)
    })

    // Connect
    ctx := context.Background()
    if err := client.Connect(ctx); err != nil {
        panic(err)
    }
    defer client.Disconnect()

    // Create session
    session, err := client.CreateSession(ctx, "/path/to/workspace", "", "")
    if err != nil {
        panic(err)
    }
    fmt.Printf("Created session: %s\n", session)

    // Send chat message
    err = client.SendChat(ctx, "Hello, how can you help me?", nil)
    if err != nil {
        panic(err)
    }

    // Wait a bit for responses
    time.Sleep(10 * time.Second)
}
```

## Configuration

The client supports configuration via `Config`:

```go
config := &socketclient.Config{
    SocketPath:           "~/.scriptschnell.sock",
    ClientType:           "cli",
    ClientVersion:        "1.0.0",
    ConnectTimeout:       10 * time.Second,
    ReconnectEnabled:     true,
    MaxReconnectAttempts: 10,
    ReconnectDelay:       2 * time.Second,
    RequestTimeout:       30 * time.Second,
}

client, err := socketclient.NewClientWithConfig(config)
```

## Callbacks

### Chat Messages
```go
client.SetChatMessageCallback(func(msg socketclient.ChatMessage) {
    if msg.Reasoning {
        // Handle extended thinking content
        fmt.Printf("Thinking: %s\n", msg.Content)
    } else {
        fmt.Printf("[%s]: %s\n", msg.Role, msg.Content)
    }
})
```

### Progress Updates
```go
client.SetProgressCallback(func(progress socketclient.ProgressData) {
    fmt.Println(progress.Message)
})
```

### Authorization Requests
```go
client.SetAuthorizationCallback(func(req socketclient.AuthorizationRequest) (bool, error) {
    fmt.Printf("Authorize tool '%s'?\n", req.ToolName)
    fmt.Printf("Reason: %s\n", req.Reason)
    // Ask user and return approval
    return true, nil
})
```

### Question Dialogs
```go
client.SetQuestionCallback(func(req socketclient.QuestionRequest) (map[string]string, error) {
    answers := make(map[string]string)
    for id, question := range req.Questions {
        fmt.Printf("%s: ", question)
        var answer string
        fmt.Scanln(&answer)
        answers[id] = answer
    }
    return answers, nil
})
```

### Connection State Changes
```go
client.SetStateChangedCallback(func(state socketclient.ConnectionState, err error) {
    fmt.Printf("Connection state: %v\n", state)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
})
```

### Reconnection Events
```go
client.SetReconnectingCallback(func(attempt, maxAttempts int) {
    fmt.Printf("Reconnecting... (%d/%d)\n", attempt, maxAttempts)
})
```

## Session Management

```go
// Create new session
sessionID, err := client.CreateSession(ctx, workspace, "", workingDir)

// List sessions
sessions, err := client.ListSessions(ctx, workspace)
for _, sess := range sessions {
    fmt.Printf("%s: %s (%d messages)\n", sess.ID, sess.Title, sess.MessageCount)
}

// Attach to existing session
err = client.AttachSession(ctx, sessionID)

// Detach from session
err = client.DetachSession(ctx)

// Save session
err = client.SaveSession(ctx, "My Session")

// Load session
err = client.LoadSession(ctx, sessionID, workspace)

// Delete session
err = client.DeleteSession(ctx, sessionID, workspace)
```

## Workspace Management

```go
// List workspaces
workspaces, err := client.ListWorkspaces(ctx)
for _, ws := range workspaces {
    fmt.Printf("%s: %s (%d sessions)\n", ws.ID, ws.Path, ws.SessionCount)
}

// Set active workspace
err = client.SetWorkspace(ctx, "/path/to/workspace")

// Create workspace (git worktree)
workspaceID, path, err := client.CreateWorkspace(ctx, baseWorkspace, "feature-branch")
```

## Chat Operations

```go
// Send chat message
err := client.SendChat(ctx, "Hello, world!", nil)

// Stop current operation
err := client.StopChat(ctx)

// Clear chat history
err := client.ClearChat(ctx)

// Stream with custom callback
err := client.StreamChat(ctx, "Hello", nil, func(msg socketclient.ChatMessage) {
    fmt.Printf("Stream: %s\n", msg.Content)
})

// Wait for completion
err := client.WaitForCompletion(ctx, 30*time.Second)
```

## Error Handling

```go
err := client.SendChat(ctx, "test", nil)
if err != nil {
    var socketErr *socketclient.SocketError
    if errors.As(err, &socketErr) {
        fmt.Printf("Socket error: [%s] %s\n", socketErr.Code, socketErr.Message)
        if socketErr.Details != "" {
            fmt.Printf("Details: %s\n", socketErr.Details)
        }
    }
}
```

## Reconnection

The client supports automatic reconnection:

```go
// Enable/disable reconnection
client.SetReconnectEnabled(true)

// Set max attempts
client.SetMaxReconnectAttempts(10)

// Set delay parameters
client.SetReconnectDelay(2 * time.Second)
client.SetReconnectMaxDelay(30 * time.Second)

// Monitor attempts
fmt.Printf("Current attempts: %d\n", client.GetReconnectAttempts())
```

## Thread Safety

All client methods are thread-safe. Callbacks may be invoked concurrently, so implementations should be thread-safe if they access shared state.

## License

This is part of the scriptschnell project. See the main LICENSE file for details.