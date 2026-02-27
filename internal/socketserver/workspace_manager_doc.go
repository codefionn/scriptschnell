// Package socketserver provides Unix socket server functionality with workspace management.
//
// Workspace Management Overview:
//
// The WorkspaceManager manages workspace lifecycle and state for the Unix socket server.
// Workspaces represent working directories that sessions operate in, with support for:
// - Automatic workspace registration when sessions are created
// - Git repository detection and tracking (repository root, current branch)
// - Git worktree support for isolated session environments
// - Workspace-scoped configuration (context directories, landlock permissions, approvals)
// - Session count tracking per workspace
// - Automatic cleanup of unused worktrees
//
// Workspace Lifecycle:
//
// 1. Registration:
//    - Workspaces are automatically registered when a session is created or explicitly set
//    - Each workspace is assigned a unique ID (hash of working directory path)
//    - Git metadata is detected and stored (repository root, current branch)
//
// 2. Session Tracking:
//    - WorkspaceManager tracks the number of active sessions per workspace
//    - Session count is incremented when a session is created in a workspace
//    - Session count is decremented when a client detaches or disconnects
//
// 3. Configuration:
//    - Context directories can be set per workspace (for documentation lookup)
//    - Landlock permissions (read/write paths) can be configured per workspace
//    - Network domains and command prefixes can be approved per workspace
//
// 4. Cleanup:
//    - Git worktrees are automatically cleaned up when they have no active sessions
//    - Regular working directories are never removed by the manager
//
// Git Worktree Support:
//
// The WorkspaceManager can create git worktrees for isolated session environments:
// - Worktrees are created as siblings to the base repository
// - Naming pattern: {repo-name}-{session-name}
// - Each worktree gets its own branch: session/{session-name}
// - Worktrees are automatically tracked and cleaned up when unused
//
// Protocol Messages:
//
// The socket protocol supports the following workspace-related messages:
//
// workspace_list:
//   - Lists all registered workspaces
//   - Returns metadata: ID, path, name, git info, session count, etc.
//
// workspace_set:
//   - Sets the active workspace for the client
//   - Resolves the working directory and registers if needed
//   - Updates last accessed time
//
// Example Usage:
//
//   // Create workspace manager
//   wm, err := socketserver.NewWorkspaceManager()
//
//   // Resolve a workspace (registers if not exists)
//   ws, err := wm.ResolveWorkspace(ctx, "/path/to/project")
//
//   // Create a git worktree for a session
//   worktree, err := wm.CreateWorktree(ctx, "/path/to/project", "session-name")
//
//   // Update session count
//   wm.UpdateWorkspaceSessionCount(ws.ID, 1)
//
//   // Set workspace configuration
//   wm.SetWorkspaceContextDirs(ws.ID, []string{"/docs", "/include"})
//   wm.ApproveDomainForWorkspace(ws.ID, "api.example.com")
//   wm.ApproveCommandForWorkspace(ws.ID, "docker")
//
// Thread Safety:
//
// All WorkspaceManager operations are thread-safe and use internal mutexes
// for concurrent access. The manager is designed to handle multiple clients
// accessing and modifying workspace state simultaneously.
//
// Integration with SessionManager:
//
// The WorkspaceManager works closely with SessionManager:
// - SessionManager uses WorkingDir for each session
// - WorkspaceManager tracks all workspaces and their session counts
// - When a session is created, WorkspaceManager resolves and increments count
// - When a session is detached/deleted, WorkspaceManager decrements count
// - On shutdown, WorkspaceManager cleans up unused worktrees
//
package socketserver