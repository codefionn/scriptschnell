// Package socketclient provides a client library for connecting to the scriptschnell Unix socket server.
//
// The client library enables frontends (TUI, CLI, Web, and custom clients) to communicate with
// a running scriptschnell instance over Unix domain sockets, supporting concurrent sessions,
// workspace isolation, and full protocol features.
//
// # Architecture
//
// The client follows a straightforward architecture:
//
//   - SocketClient: Main client struct managing connection, message I/O, and protocol handling
//   - Callback-based API: Receive messages via registered callbacks for different message types
//   - Reconnection: Automatic reconnection with configurable retry logic
//   - Request-Response Pattern: Send requests with request IDs and wait for responses
//
// Basic Usage
//
//	// Create new client
//	client, err := socketclient.NewClient("~/.scriptschnell.sock")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Set up message callbacks
//	client.SetChatMessageCallback(func(msg socketclient.ChatMessage) {
//	    fmt.Printf("[%s]: %s\n", msg.Role, msg.Content)
//	})
//
//	// Connect to server
//	ctx := context.Background()
//	if err := client.Connect(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Disconnect()
//
//	// Create session
//	session, err := client.CreateSession(ctx, "my-workspace", "")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Send chat message
//	err = client.SendChat(ctx, "Hello, how can you help me?")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Connection Management
//
// The client provides automatic reconnection with configurable parameters:
//
//	// Configure reconnection
//	client.SetReconnectEnabled(true)
//	client.SetMaxReconnectAttempts(10)
//	client.SetReconnectDelay(2 * time.Second)
//
//	// Monitor connection state
//	client.SetStateChangedCallback(func(state socketclient.ConnectionState, err error) {
//	    fmt.Printf("State changed: %v\n", state)
//	    if err != nil {
//	        fmt.Printf("Error: %v\n", err)
//	    }
//	})
//
// # Session Management
//
// The client provides convenient methods for session operations:
//
//	// List available sessions
//	sessions, err := client.ListSessions(ctx, "")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, sess := range sessions {
//	    fmt.Printf("%s: %s (%d messages)\n", sess.ID, sess.Title, sess.MessageCount)
//	}
//
//	// Attach to existing session
//	err = client.AttachSession(ctx, "existing-session-id")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Save current session
//	err = client.SaveSession(ctx, "My Session")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Workspace Management
//
// Workspaces can be listed and switched:
//
//	// List workspaces
//	workspaces, err := client.ListWorkspaces(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Set active workspace
//	err = client.SetWorkspace(ctx, "/path/to/workspace")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Message Protocol
//
// The client implements the full message protocol defined in docs/unix-socket-protocol.md:
//   - Authentication (auth_request, auth_response)
//   - Session management (session_create, session_attach, session_list, etc.)
//   - Chat and generation (chat_send, chat_stop, chat_message)
//   - Tool interactions (tool_call, tool_result, tool_compact)
//   - Authorization (authorization_request, authorization_response)
//   - Question dialogs (question_request, question_response)
//   - Progress updates (progress)
//   - Configuration (config_get, config_set)
//   - Workspace management (workspace_list, workspace_set)
//   - Session persistence (session_save, session_load)
//   - Connection lifecycle (ping, pong, close, closed)
//
// # Authorization Flow
//
// Authorization requests are handled via callbacks:
//
//	// Set authorization callback
//	client.SetAuthorizationCallback(func(req socketclient.AuthorizationRequest) (approved bool, err error) {
//	    fmt.Printf("Authorization required for tool: %s\n", req.ToolName)
//	    fmt.Printf("Reason: %s\n", req.Reason)
//	    // Ask user for approval (in TUI/CLI/Web)
//	    return true, nil // or false, nil
//	})
//
// # Question Dialogs
//
// Planning agent questions are handled via callbacks:
//
//	// Set question callback
//	client.SetQuestionCallback(func(req socketclient.QuestionRequest) (answers map[string]string, err error) {
//	    if req.MultiMode {
//	        answers = make(map[string]string)
//	        for id, question := range req.Questions {
//	            fmt.Printf("Q: %s\n", question)
//	            answers[id] = getUserInput() // Get user input
//	        }
//	        return answers, nil
//	    }
//	    return map[string]string{"answer": getUserInput()}, nil
//	})
//
// # Progress Updates
//
// Progress updates are streamed via callbacks:
//
//	// Set progress callback
//	client.SetProgressCallback(func(progress socketclient.ProgressData) {
//	    fmt.Printf("Progress: %s\n", progress.Message)
//	    if progress.IsCompact {
//	        fmt.Printf("\033[2K\r%s", progress.Message) // Update in place
//	    }
//	})
//
// # Error Handling
//
// The client provides detailed error information via the error interface:
//
//	err := client.SendChat(ctx, "test")
//	if err != nil {
//	    var socketErr *socketclient.SocketError
//	    if errors.As(err, &socketErr) {
//	        fmt.Printf("Socket error: %s (code: %s)\n", socketErr.Message, socketErr.Code)
//	        if socketErr.Details != "" {
//	            fmt.Printf("Details: %s\n", socketErr.Details)
//	        }
//	    }
//	}
//
// # Thread Safety
//
// SocketClient is thread-safe for concurrent use from multiple goroutines. All operations
// are protected by mutexes. Callbacks may be invoked concurrently from different goroutines,
// so implementations must be thread-safe if they access shared state.
//
// # Context Support
//
// Most methods accept a context.Context for cancellation and timeout:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	session, err := client.CreateSession(ctx, "workspace", "")
//	if err != nil {
//	    if errors.Is(err, context.DeadlineExceeded) {
//	        fmt.Println("Request timed out")
//	    }
//	    return
//	}
//
// # Reconnection Behavior
//
// When enabled, automatic reconnection attempts to reconnect on connection loss:
//   - Waits with exponential backoff (configurable)
//   - Attempts up to max retry attempts (default: 10)
//   - Notifies state changes via callback
//   - Restores session attachment if previously attached
//   - Re-subscribes to message callbacks
//
// # Graceful Shutdown
//
// Always disconnect the client when done:
//
//	client.Disconnect()
//
// Or use Close for immediate shutdown without sending close message:
//
//	client.Close()
package socketclient
