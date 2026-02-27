// Package socketserver implements a Unix domain socket server for scriptschnell.
//
// The socket server enables multiple frontends (TUI, CLI, and custom clients) to connect
// to a single running scriptschnell instance, supporting concurrent sessions and workspace
// isolation.
//
// # Architecture
//
// The server follows an actor-based architecture with the following components:
//
//   - Server: Listens for incoming Unix socket connections and manages the overall server lifecycle
//   - Hub: Manages active client connections and session registry
//   - Client: Handles individual client connections with read/write pumps and message dispatching
//
// # Message Protocol
//
// Communication uses JSON messages delimited by newlines (JSON-RPC style):
//
//	{"type":"message_type","data":{...},"request_id":"uuid"}\n
//
// The protocol supports message types for:
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
// # Session Management
//
// Each client connection can:
//   - Create new sessions in a specified workspace
//   - Attach to existing sessions (exclusive ownership model)
//   - Detach from sessions without destroying them
//   - List and delete sessions
//
// # Authentication
//
// The server supports multiple authentication methods:
//   - File permission-based (recommended): Uses Unix socket file permissions
//   - Token-based: Pre-shared token sent in auth_request
//   - Challenge-response: HMAC-SHA256 with nonce
//   - Peer credentials: SO_PEERCRED to validate UID/GID
//
// Usage
//
//	// Create server with configuration
//	server, err := socketserver.NewServer(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Start the server
//	ctx := context.Background()
//	if err := server.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Wait for shutdown signal
//	<-ctx.Done()
//
//	// Stop the server
//	if err := server.Stop(); err != nil {
//	    log.Fatal(err)
//	}
//
// Client Connection
//
//	Clients connect to the Unix socket and send messages in JSON format:
//
//	// Connect to socket
//	conn, err := net.Dial("unix", socketPath)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer conn.Close()
//
//	// Send authentication request
//	authReq := `{"type":"auth_request","data":{"client_type":"cli","version":"1.0.0"},"request_id":"123"}`
//	fmt.Fprintf(conn, "%s\n", authReq)
//
//	// Read response
//	reader := bufio.NewReader(conn)
//	response, _ := reader.ReadString('\n')
package socketserver
