package acp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/orchestrator"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/project"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/tools"
	"github.com/coder/acp-go-sdk"
	godiff "github.com/sourcegraph/go-diff/diff"
)

const maxLogSnippetLen = 256

// truncateForLog truncates a string to a maximum length for logging purposes.
// If the string exceeds maxLogSnippetLen, it returns a truncated version with a suffix.
func truncateForLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLogSnippetLen {
		return s
	}
	return s[:maxLogSnippetLen] + "...(truncated)"
}

// truncateMapForLog converts a map to a string and truncates it for logging purposes.
// Returns an empty map representation string if the map is empty.
func truncateMapForLog(m map[string]interface{}) string {
	if len(m) == 0 {
		return "{}"
	}
	return truncateForLog(fmt.Sprintf("%v", m))
}

// SlashCommand represents a slash command that can be executed by the agent
type SlashCommand struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Input       *acp.AvailableCommandInput `json:"input,omitempty"`
	Handler     func(sessionID, args string) (string, error)
}

// GetAvailableCommands returns the list of available slash commands
func (a *ScriptschnellAIAgent) GetAvailableCommands() []acp.AvailableCommand {
	commands := []acp.AvailableCommand{
		{
			Name:        "init",
			Description: "Initialize a new project or workspace",
			Input: &acp.AvailableCommandInput{
				UnstructuredCommandInput: &acp.AvailableCommandUnstructuredCommandInput{
					Hint: "project type or description",
				},
			},
		},
		{
			Name:        "help",
			Description: "Show available commands and help information",
		},
		{
			Name:        "status",
			Description: "Show current session and project status",
		},
		{
			Name:        "clear",
			Description: "Clear the current conversation context",
		},
		{
			Name:        "context",
			Description: "Manage context directories for external documentation",
			Input: &acp.AvailableCommandInput{
				UnstructuredCommandInput: &acp.AvailableCommandUnstructuredCommandInput{
					Hint: "list | add <directory> | remove <directory>",
				},
			},
		},
		{
			Name:        "session",
			Description: "Manage LLM sessions (/session save | /session load | /session list | /session delete)",
			Input: &acp.AvailableCommandInput{
				UnstructuredCommandInput: &acp.AvailableCommandUnstructuredCommandInput{
					Hint: "save [name] | load [id] | list | delete [id]",
				},
			},
		},
	}

	return commands
}

// parseSlashCommand parses a prompt to detect and extract slash commands
func (a *ScriptschnellAIAgent) parseSlashCommand(promptText string) (command, args string, isCommand bool) {
	// Trim leading whitespace
	original := promptText
	promptText = strings.TrimSpace(promptText)

	// Check if it starts with /
	if !strings.HasPrefix(promptText, "/") {
		logger.Debug("parseSlashCommand: no leading slash (input=%q)", truncateForLog(original))
		return "", "", false
	}

	// Extract command and args
	parts := strings.Fields(promptText)
	if len(parts) == 0 {
		logger.Debug("parseSlashCommand: no command tokens found (input=%q)", truncateForLog(original))
		return "", "", false
	}

	command = strings.TrimPrefix(parts[0], "/")
	if len(parts) > 1 {
		// Join all remaining parts as args
		args = strings.Join(parts[1:], " ")
	}

	logger.Debug("parseSlashCommand: detected command=%q args=%q", command, truncateForLog(args))
	return command, args, true
}

// executeSlashCommand executes a slash command and returns the response
func (a *ScriptschnellAIAgent) executeSlashCommand(sessionID, command, args string) (string, error) {
	logger.Debug("executeSlashCommand[%s]: command=%q args=%q", sessionID, command, truncateForLog(args))

	// Get the session
	a.mu.Lock()
	session, exists := a.sessions[sessionID]
	if !exists {
		a.mu.Unlock()
		logger.Warn("executeSlashCommand[%s]: session not found", sessionID)
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	a.mu.Unlock()

	var (
		resp string
		err  error
	)

	switch command {
	case "init":
		resp, err = a.handleInitCommand(sessionID, args)
	case "help":
		resp, err = a.handleHelpCommand(), nil
	case "status":
		resp, err = a.handleStatusCommand(session), nil
	case "clear":
		resp, err = a.handleClearCommand(session), nil
	case "context":
		resp, err = a.handleContextCommand(args)
	case "session":
		resp, err = a.handleSessionCommand(args)
	default:
		err = fmt.Errorf("unknown command: /%s", command)
	}

	if err != nil {
		logger.Debug("executeSlashCommand[%s]: command %q failed: %v", sessionID, command, err)
	} else {
		logger.Debug("executeSlashCommand[%s]: command %q succeeded", sessionID, command)
	}

	return resp, err
}

// handleInitCommand handles the /init command
func (a *ScriptschnellAIAgent) handleInitCommand(sessionID, args string) (string, error) {
	logger.Debug("handleInitCommand[%s]: args=%q", sessionID, truncateForLog(args))

	response := "🚀 Initializing project...\n\n"

	if args != "" {
		response += fmt.Sprintf("Project type: %s\n\n", args)
	}

	response += "I'll help you set up a new project. Let me check what's in your current directory first.\n\n"

	// Use the orchestrator to execute some initialization logic
	response += "→ Reading current directory structure...\n"

	projectTypes, detectErr := project.NewDetector(a.config.WorkingDir).Detect(context.Background())
	if detectErr != nil {
		response += fmt.Sprintf("→ Project detection failed: %v\n\n", detectErr)
	} else if len(projectTypes) == 0 {
		response += "→ Unable to detect a supported project type from existing files.\n\n"
	} else {
		response += "→ Detected project type(s):\n"
		for _, pt := range projectTypes {
			response += fmt.Sprintf("   - %s (%.0f%% confidence): %s\n", pt.Name, pt.Confidence*100, pt.Description)
			if len(pt.Evidence) > 0 {
				response += fmt.Sprintf("     Evidence: %s\n", pt.Evidence[0])
			}
		}
		response += "\n"
	}

	// In a real implementation, this would:
	// 1. Examine the current directory
	// 2. Detect project type if not specified
	// 3. Create appropriate project structure
	// 4. Set up configuration files

	response += "✓ Project initialized successfully!\n\n"
	response += "Next steps:\n"
	response += "- Add your source code files\n"
	response += "- Configure your build settings\n"
	response += "- Start developing with AI assistance\n"

	logger.Debug("handleInitCommand[%s]: returning canned response", sessionID)
	return response, nil
}

// handleHelpCommand handles the /help command
func (a *ScriptschnellAIAgent) handleHelpCommand() string {
	logger.Debug("handleHelpCommand: generating command list")

	commands := a.GetAvailableCommands()

	response := "📋 Available Commands:\n\n"
	for _, cmd := range commands {
		response += fmt.Sprintf("/%s - %s\n", cmd.Name, cmd.Description)
		if cmd.Input != nil && cmd.Input.UnstructuredCommandInput != nil {
			response += fmt.Sprintf("  Usage: /%s <%s>\n", cmd.Name, cmd.Input.UnstructuredCommandInput.Hint)
		}
	}

	response += "\n💡 Tips:\n"
	response += "- Commands can be used at the start of any message\n"
	response += "- Some commands accept additional arguments\n"
	response += "- You can combine commands with regular conversation\n"

	logger.Debug("handleHelpCommand: listed %d commands", len(commands))
	return response
}

// handleStatusCommand handles the /status command
func (a *ScriptschnellAIAgent) handleStatusCommand(session *statcodeSession) string {
	logger.Debug("handleStatusCommand[%s]: rendering status", session.sessionID)

	response := "📊 Current Status:\n\n"
	response += fmt.Sprintf("Session ID: %s\n", session.sessionID)
	response += fmt.Sprintf("Active: %t\n", session.isActive)

	// Add working directory info
	response += fmt.Sprintf("Working Directory: %s\n", a.config.WorkingDir)

	// Add provider info if available
	if a.providerMgr != nil {
		response += "Providers: Configured\n"
	}

	// Add filesystem info
	if a.supportsFilesystemProtocol() {
		response += "Filesystem: ACP Protocol\n"
	} else {
		response += "Filesystem: Local\n"
	}

	response += "\n✅ System ready for assistance\n"

	logger.Debug("handleStatusCommand[%s]: done", session.sessionID)
	return response
}

// handleClearCommand handles the /clear command
func (a *ScriptschnellAIAgent) handleClearCommand(session *statcodeSession) string {
	if session == nil || session.orchestrator == nil {
		if session != nil {
			logger.Warn("handleClearCommand[%s]: session or orchestrator missing", session.sessionID)
		} else {
			logger.Warn("handleClearCommand: session is nil")
		}
		return "⚠️ Unable to clear session: orchestrator not available."
	}

	logger.Debug("handleClearCommand[%s]: clearing session", session.sessionID)

	// Auto-save the current session before clearing if it has messages
	saved := false
	currentSession := session.orchestrator.GetSession()
	if currentSession != nil && len(currentSession.GetMessages()) > 0 {
		if storageRef, exists := session.orchestrator.GetActor("session_storage"); exists {
			// Generate session title (best-effort)
			if err := session.orchestrator.GenerateSessionTitle(context.Background()); err != nil {
				logger.Warn("handleClearCommand[%s]: failed to generate title: %v", session.sessionID, err)
			}

			name := actor.GenerateSessionName("")
			if err := actor.SaveSessionViaActor(context.Background(), storageRef, currentSession, name); err != nil {
				logger.Warn("handleClearCommand[%s]: failed to auto-save session: %v", session.sessionID, err)
			} else {
				logger.Info("handleClearCommand[%s]: auto-saved session %s as '%s'", session.sessionID, currentSession.ID, name)
				saved = true
			}
		}
	}

	if err := session.orchestrator.ClearSession(); err != nil {
		logger.Warn("handleClearCommand[%s]: failed to clear session: %v", session.sessionID, err)
		return fmt.Sprintf("⚠️ Failed to clear session: %v", err)
	}

	if saved {
		return "🧹 Session saved and cleared.\n\nReady for a fresh start! What would you like to work on?\n"
	}
	return "🧹 Conversation context and todos cleared.\n\nReady for a fresh start! What would you like to work on?\n"
}

// handleContextCommand handles the /context command
func (a *ScriptschnellAIAgent) handleContextCommand(args string) (string, error) {
	logger.Debug("handleContextCommand: args=%q", truncateForLog(args))

	args = strings.TrimSpace(args)
	parts := strings.Fields(args)

	if len(parts) == 0 || parts[0] == "help" {
		return a.contextHelp(), nil
	}

	subCmd := strings.ToLower(parts[0])
	switch subCmd {
	case "list":
		return a.handleContextList()
	case "add":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /context add <directory>")
		}
		dir := strings.Join(parts[1:], " ")
		return a.handleContextAdd(dir)
	case "remove":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /context remove <directory>")
		}
		dir := strings.Join(parts[1:], " ")
		return a.handleContextRemove(dir)
	default:
		return "", fmt.Errorf("unknown /context subcommand: %s", subCmd)
	}
}

func (a *ScriptschnellAIAgent) contextHelp() string {
	return `📁 Context Directory Commands:

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

func (a *ScriptschnellAIAgent) handleContextList() (string, error) {
	contextDirs := a.config.GetContextDirectories(a.config.WorkingDir)
	if len(contextDirs) == 0 {
		return "No context directories configured for this workspace.\n\nUse /context add <directory> to add context directories.", nil
	}

	var sb strings.Builder
	sb.WriteString("📁 Configured context directories:\n\n")

	for i, dir := range contextDirs {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, dir))
	}

	sb.WriteString(fmt.Sprintf("\nTotal: %d context director", len(contextDirs)))
	if len(contextDirs) == 1 {
		sb.WriteString("y")
	} else {
		sb.WriteString("ies")
	}
	sb.WriteString("\n\nThe AI can search and read files in these directories using:\n")
	sb.WriteString("- search_context_files\n")
	sb.WriteString("- grep_context_files\n")
	sb.WriteString("- read_context_file\n")

	return sb.String(), nil
}

func (a *ScriptschnellAIAgent) handleContextAdd(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("directory path cannot be empty")
	}

	// Check if the directory is the user's home directory
	isHomeDir, err := tools.IsHomeDirectory(dir)
	if err != nil {
		return "", fmt.Errorf("failed to validate directory: %w", err)
	}
	if isHomeDir {
		return "", fmt.Errorf("cannot add home directory as context directory for security reasons; add a subdirectory instead (e.g., ~/Documents, ~/projects)")
	}

	// Add to config for current workspace
	a.config.AddContextDirectory(a.config.WorkingDir, dir)

	// Save config
	if err := a.config.Save(config.GetConfigPath()); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	logger.Debug("handleContextAdd: added context directory %s for workspace %s", dir, a.config.WorkingDir)

	return fmt.Sprintf("✓ Added context directory: %s\n\nThe AI can now search and read files in this directory using:\n- search_context_files\n- grep_context_files\n- read_context_file", dir), nil
}

func (a *ScriptschnellAIAgent) handleContextRemove(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("directory path cannot be empty")
	}

	// Remove from config for current workspace
	removed := a.config.RemoveContextDirectory(a.config.WorkingDir, dir)
	if !removed {
		return "", fmt.Errorf("context directory not found: %s", dir)
	}

	// Save config
	if err := a.config.Save(config.GetConfigPath()); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	logger.Debug("handleContextRemove: removed context directory %s", dir)

	return fmt.Sprintf("✓ Removed context directory: %s", dir), nil
}

// handleSessionCommand handles the /session command
func (a *ScriptschnellAIAgent) handleSessionCommand(args string) (string, error) {
	logger.Debug("handleSessionCommand: args=%q", truncateForLog(args))

	args = strings.TrimSpace(args)
	parts := strings.Fields(args)

	if len(parts) == 0 || parts[0] == "help" {
		return a.sessionHelp(), nil
	}

	subCmd := strings.ToLower(parts[0])
	switch subCmd {
	case "save":
		var name string
		if len(parts) > 1 {
			name = strings.Join(parts[1:], " ")
		}
		return a.handleSessionSave(name)
	case "load":
		var sessionID string
		if len(parts) > 1 {
			sessionID = parts[1]
		}
		return a.handleSessionLoad(sessionID)
	case "list":
		return a.handleSessionList()
	case "delete":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /session delete <session_id>")
		}
		return a.handleSessionDelete(parts[1])
	default:
		return "", fmt.Errorf("unknown /session subcommand: %s", subCmd)
	}
}

func (a *ScriptschnellAIAgent) sessionHelp() string {
	return `💾 Session Commands:

/session save [name]
    Save the current session with an optional name.
    Sessions are stored per workspace.

/session load [session_id]
    Load a session by ID. If no ID provided shows selection menu.
    Only shows sessions from the current workspace.

/session list
    List all saved sessions for the current workspace.

/session delete [session_id]
    Delete a saved session by ID.

Sessions are workspace-dependent and stored persistently.
Each session contains the conversation history, file tracking, and metadata.

Examples:
  /session save MyProjectSetup
  /session list
  /session load abc123def456
  /session delete abc123def456
`
}

func (a *ScriptschnellAIAgent) handleSessionSave(name string) (string, error) {
	// Get the orchestrator from the active session
	activeSession := a.getActiveSession()
	if activeSession == nil || activeSession.orchestrator == nil {
		return "", fmt.Errorf("no active orchestrator available")
	}

	// Get session storage actor
	storageRef, exists := activeSession.orchestrator.GetActor("session_storage")
	if !exists {
		return "", fmt.Errorf("session storage not initialized")
	}

	// Generate name if not provided
	if name == "" {
		name = actor.GenerateSessionName("")
	} else {
		name = actor.GenerateSessionName(name)
	}

	// Generate session title if not already present
	if err := activeSession.orchestrator.GenerateSessionTitle(context.Background()); err != nil {
		// Continue with save even if title generation fails
		logger.Warn("Failed to generate session title: %v", err)
	}

	// Save the session
	currentSession := activeSession.orchestrator.GetSession()
	if err := actor.SaveSessionViaActor(context.Background(), storageRef, currentSession, name); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return fmt.Sprintf("✓ Session saved as '%s' (ID: %s)", name, currentSession.ID), nil
}

func (a *ScriptschnellAIAgent) handleSessionList() (string, error) {
	// Get the orchestrator from the active session
	activeSession := a.getActiveSession()
	if activeSession == nil || activeSession.orchestrator == nil {
		return "", fmt.Errorf("no active orchestrator available")
	}

	// Get session storage actor
	storageRef, exists := activeSession.orchestrator.GetActor("session_storage")
	if !exists {
		return "", fmt.Errorf("session storage not initialized")
	}

	// List sessions for current workspace
	sessions, err := actor.ListSessionsViaActor(context.Background(), storageRef, a.config.WorkingDir)
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	// Sort sessions by creation time (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	// Limit to last 5 sessions
	if len(sessions) > 5 {
		sessions = sessions[:5]
	}

	if len(sessions) == 0 {
		return "No saved sessions found for this workspace.\n\nUse /session save [name] to save the current session.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("💾 Saved sessions for workspace: %s\n\n", a.config.WorkingDir))

	for i, sess := range sessions {
		// Display title if available, otherwise fall back to name
		displayTitle := sess.Title
		if displayTitle == "" {
			displayTitle = sess.Name
		}
		if displayTitle == "" {
			displayTitle = "Unnamed"
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, displayTitle))
		sb.WriteString(fmt.Sprintf("   ID: %s\n", sess.ID))
		sb.WriteString(fmt.Sprintf("   Created: %s\n", sess.CreatedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("   Updated: %s\n", sess.UpdatedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("   Messages: %d\n", sess.MessageCount))
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}

func (a *ScriptschnellAIAgent) handleSessionLoad(sessionID string) (string, error) {
	// Get the orchestrator from the active session
	activeSession := a.getActiveSession()
	if activeSession == nil || activeSession.orchestrator == nil {
		return "", fmt.Errorf("no active orchestrator available")
	}

	// Get session storage actor
	storageRef, exists := activeSession.orchestrator.GetActor("session_storage")
	if !exists {
		return "", fmt.Errorf("session storage not initialized")
	}

	// If no session ID provided, show list
	if sessionID == "" {
		return a.handleSessionLoadWithMenu(storageRef)
	}

	// Load specific session
	loadedSession, err := actor.LoadSessionViaActor(context.Background(), storageRef, a.config.WorkingDir, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to load session: %w", err)
	}

	// Replace current session in orchestrator
	activeSession.orchestrator.SetSession(loadedSession)

	// Stream the restored conversation back to the client so the UI reflects the loaded session
	if err := a.streamSessionHistory(sessionID, loadedSession); err != nil {
		logger.Warn("Failed to stream session history for %s: %v", sessionID, err)
	}

	// Get session name for display
	var name string
	if len(loadedSession.GetMessages()) == 0 {
		name = "Unnamed"
	} else {
		// Try to get the name from storage metadata
		sessions, _ := actor.ListSessionsViaActor(context.Background(), storageRef, a.config.WorkingDir)
		for _, sess := range sessions {
			if sess.ID == sessionID {
				name = sess.Name
				if name == "" {
					name = "Unnamed"
				}
				break
			}
		}
	}

	return fmt.Sprintf("✓ Loaded session '%s' (ID: %s) with %d messages", name, sessionID, len(loadedSession.GetMessages())), nil
}

func (a *ScriptschnellAIAgent) handleSessionLoadWithMenu(storageRef *actor.ActorRef) (string, error) {
	// Get available sessions
	sessions, err := actor.ListSessionsViaActor(context.Background(), storageRef, a.config.WorkingDir)
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		return "No saved sessions found for this workspace.\n\nUse /session save [name] to save the current session.", nil
	}

	// Build menu response
	var sb strings.Builder
	sb.WriteString("💾 Available sessions to load:\n\n")

	for i, sess := range sessions {
		name := sess.Name
		if name == "" {
			name = "Unnamed"
		}
		sb.WriteString(fmt.Sprintf("%d. %s (ID: %s) - %d messages\n", i+1, name, sess.ID, sess.MessageCount))
	}

	sb.WriteString("\nTo load a session, use: /session load <session_id>\n")
	sb.WriteString("Example: /session load abc123def456")

	return sb.String(), nil
}

// streamSessionHistory replays a loaded session's conversation to the ACP client
func (a *ScriptschnellAIAgent) streamSessionHistory(sessionID string, sess *session.Session) error {
	if sess == nil || a.conn == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	messages := sess.GetMessages()
	for idx, msg := range messages {
		update, ok := sessionMessageToUpdate(msg)
		if !ok {
			continue
		}

		if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: acp.SessionId(sessionID),
			Update:    update,
		}); err != nil {
			return fmt.Errorf("failed to stream history message %d: %w", idx, err)
		}
	}

	return nil
}

// sessionMessageToUpdate converts a stored session message into an ACP session update
func sessionMessageToUpdate(msg *session.Message) (acp.SessionUpdate, bool) {
	if msg == nil {
		return acp.SessionUpdate{}, false
	}

	switch strings.ToLower(msg.Role) {
	case "user":
		return acp.UpdateUserMessageText(msg.Content), true
	case "assistant":
		return acp.UpdateAgentMessageText(msg.Content), true
	case "tool":
		content := msg.Content
		if msg.ToolName != "" {
			content = fmt.Sprintf("🛠️ %s: %s", msg.ToolName, msg.Content)
		}
		return acp.UpdateAgentMessageText(content), true
	default:
		return acp.UpdateAgentMessageText(msg.Content), true
	}
}

func (a *ScriptschnellAIAgent) handleSessionDelete(sessionID string) (string, error) {
	// Get the orchestrator from the active session
	activeSession := a.getActiveSession()
	if activeSession == nil || activeSession.orchestrator == nil {
		return "", fmt.Errorf("no active orchestrator available")
	}

	// Get session storage actor
	storageRef, exists := activeSession.orchestrator.GetActor("session_storage")
	if !exists {
		return "", fmt.Errorf("session storage not initialized")
	}

	// First, show session info before deletion
	sessions, err := actor.ListSessionsViaActor(context.Background(), storageRef, a.config.WorkingDir)
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	var targetSession *session.SessionMetadata
	for _, sess := range sessions {
		if sess.ID == sessionID {
			targetSession = &sess
			break
		}
	}

	if targetSession == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	// Show session info before deletion
	name := targetSession.Name
	if name == "" {
		name = "Unnamed"
	}

	// Delete the session
	err = actor.DeleteSessionViaActor(context.Background(), storageRef, a.config.WorkingDir, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to delete session: %w", err)
	}

	return fmt.Sprintf("✓ Deleted session: %s\n\nName: %s\nID: %s\nCreated: %s\nMessages: %d",
		sessionID, name, targetSession.ID, targetSession.CreatedAt.Format("2006-01-02 15:04:05"), targetSession.MessageCount), nil
}

// getActiveSession returns the current active session
func (a *ScriptschnellAIAgent) getActiveSession() *statcodeSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Find the first active session (in a real implementation, we might track this differently)
	for _, session := range a.sessions {
		return session
	}
	return nil
}

// ScriptschnellAIAgent implements the acp.Agent interface to expose statcode-ai functionality via ACP
type ScriptschnellAIAgent struct {
	conn         *acp.AgentSideConnection
	config       *config.Config
	providerMgr  *provider.Manager
	orchestrator *orchestrator.Orchestrator
	sessions     map[string]*statcodeSession
	clientCaps   *acp.ClientCapabilities // Store client capabilities
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
}

type statcodeSession struct {
	sessionID     string
	orchestrator  *orchestrator.Orchestrator
	promptCtx     context.Context
	promptCancel  context.CancelFunc
	isActive      bool
	toolLocations map[string][]acp.ToolCallLocation
	toolParams    map[string]map[string]interface{}
	toolProgress  map[string]*strings.Builder // Accumulated progress output per tool ID
	mu            sync.Mutex
}

func (a *ScriptschnellAIAgent) rememberToolContext(session *statcodeSession, toolID string, params map[string]interface{}, locations []acp.ToolCallLocation) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.toolLocations == nil {
		session.toolLocations = make(map[string][]acp.ToolCallLocation)
	}
	if session.toolParams == nil {
		session.toolParams = make(map[string]map[string]interface{})
	}

	session.toolParams[toolID] = params
	if len(locations) > 0 {
		session.toolLocations[toolID] = locations
	}
}

func (a *ScriptschnellAIAgent) getToolLocations(session *statcodeSession, toolID string) []acp.ToolCallLocation {
	session.mu.Lock()
	defer session.mu.Unlock()

	locs := session.toolLocations[toolID]
	if len(locs) == 0 {
		return nil
	}

	out := make([]acp.ToolCallLocation, len(locs))
	copy(out, locs)
	return out
}

func (a *ScriptschnellAIAgent) accumulateToolProgress(session *statcodeSession, toolID, message string) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.toolProgress == nil {
		session.toolProgress = make(map[string]*strings.Builder)
	}

	if _, exists := session.toolProgress[toolID]; !exists {
		session.toolProgress[toolID] = &strings.Builder{}
	}

	session.toolProgress[toolID].WriteString(message)
}

func (a *ScriptschnellAIAgent) popToolContext(session *statcodeSession, toolID string) (map[string]interface{}, []acp.ToolCallLocation, string) {
	session.mu.Lock()
	defer session.mu.Unlock()

	params := session.toolParams[toolID]
	locations := session.toolLocations[toolID]
	var progressText string

	if builder, exists := session.toolProgress[toolID]; exists {
		progressText = builder.String()
		delete(session.toolProgress, toolID)
	}

	delete(session.toolParams, toolID)
	delete(session.toolLocations, toolID)

	return params, locations, progressText
}

var (
	_ acp.Agent             = (*ScriptschnellAIAgent)(nil)
	_ acp.AgentLoader       = (*ScriptschnellAIAgent)(nil)
	_ acp.AgentExperimental = (*ScriptschnellAIAgent)(nil)
)

// NewScriptschnellAIAgent creates a new ACP agent that wraps statcode-ai functionality
func NewScriptschnellAIAgent(ctx context.Context, cfg *config.Config, providerMgr *provider.Manager) (*ScriptschnellAIAgent, error) {
	logger.Info("Creating scriptschnell ACP Agent")
	logger.Debug("NewScriptschnellAIAgent: workingDir=%q providerConfigured=%t", cfg.WorkingDir, providerMgr != nil)

	// Create orchestrator for ACP mode (non-interactive)
	orch, err := orchestrator.NewOrchestrator(cfg, providerMgr, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}
	logger.Debug("NewScriptschnellAIAgent: base orchestrator created (%T)", orch)

	agentCtx, agentCancel := context.WithCancel(ctx)

	agent := &ScriptschnellAIAgent{
		config:       cfg,
		providerMgr:  providerMgr,
		orchestrator: orch,
		sessions:     make(map[string]*statcodeSession),
		clientCaps:   nil, // Will be set during Initialize
		ctx:          agentCtx,
		cancel:       agentCancel,
	}

	return agent, nil
}

// SetAgentConnection implements acp.AgentConnAware to receive the connection after construction
func (a *ScriptschnellAIAgent) SetAgentConnection(conn *acp.AgentSideConnection) {
	logger.Debug("SetAgentConnection: binding ACP connection %p", conn)
	a.conn = conn
}

// Initialize implements acp.Agent
func (a *ScriptschnellAIAgent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	logger.Info("Initializing ACP agent connection")
	logger.Debug("Initialize: client meta=%+v", params.ClientInfo)

	// Store client capabilities for filesystem protocol checking
	a.mu.Lock()
	a.clientCaps = &params.ClientCapabilities
	a.mu.Unlock()

	logger.Debug("Initialize: client capabilities=%+v", params.ClientCapabilities)

	// Check if client supports filesystem protocol
	supportsFilesystem := false
	if a.clientCaps != nil && a.clientCaps.Fs.ReadTextFile && a.clientCaps.Fs.WriteTextFile {
		supportsFilesystem = true
		logger.Info("Client supports filesystem protocol (readTextFile, writeTextFile)")
	}

	if !supportsFilesystem {
		logger.Info("Client does not support filesystem protocol, using local filesystem")
	}

	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: false, // We'll handle session creation fresh each time
		},
	}, nil
}

// createTodoPlanUpdateCallback creates a callback function that sends todo plan updates via ACP
func (a *ScriptschnellAIAgent) createTodoPlanUpdateCallback(sessionID string) tools.TodoChangeCallback {
	return func(todos *tools.TodoList) {
		// Format the plan as text
		planText := tools.FormatTodoPlanAsText(todos)

		// Send plan update via ACP
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		logger.Debug("Sending todo plan update for session %s with %d items", sessionID, len(todos.Items))
		err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: acp.SessionId(sessionID),
			Update:    acp.UpdateAgentMessageText(planText),
		})

		if err != nil {
			logger.Error("Failed to send todo plan update via ACP: %v", err)
		}
	}
}

// NewSession implements acp.Agent
func (a *ScriptschnellAIAgent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	sessionID := fmt.Sprintf("statcode_%d", time.Now().UnixNano())

	logger.Info("Creating new ACP session: %s", sessionID)
	logger.Debug("NewSession[%s]: parameters=%+v", sessionID, params)

	supportsFS := a.supportsFilesystemProtocol()
	logger.Debug("NewSession[%s]: preparing filesystem (clientFS=%t)", sessionID, supportsFS)
	// Create filesystem for this session
	sessionFS, err := a.createFilesystemForSession(sessionID, supportsFS)
	if err != nil {
		logger.Error("NewSession[%s]: failed to create filesystem: %v", sessionID, err)
		return acp.NewSessionResponse{}, fmt.Errorf("failed to create filesystem for session: %w", err)
	}
	logger.Debug("NewSession[%s]: filesystem created (%T)", sessionID, sessionFS)

	if afs, ok := sessionFS.(*ACPFileSystem); ok {
		logger.Debug("NewSession[%s]: ACP filesystem configured (session=%s)", sessionID, afs.sessionID)
	}

	// Create todo actor for this session
	todoActor := tools.NewTodoActor("todo")
	logger.Debug("NewSession[%s]: instantiated todo actor (%p)", sessionID, todoActor)

	// Create a new orchestrator instance for this session to maintain isolation
	// Use the todo actor and custom filesystem
	sessionOrch, err := orchestrator.NewOrchestratorWithFSAndTodoActor(a.config, a.providerMgr, false, sessionFS, todoActor)
	if err != nil {
		logger.Warn("NewSession[%s]: orchestrator with filesystem failed: %v", sessionID, err)
		// Fallback: try creating without custom filesystem but with todo actor
		sessionOrch, err = orchestrator.NewOrchestratorWithTodoActor(a.config, a.providerMgr, false, todoActor)
		if err != nil {
			logger.Warn("NewSession[%s]: orchestrator with todo actor fallback failed: %v", sessionID, err)
			// Final fallback: try creating without customizations
			sessionOrch, err = orchestrator.NewOrchestrator(a.config, a.providerMgr, false)
			if err != nil {
				logger.Error("NewSession[%s]: failed to create session orchestrator after fallbacks: %v", sessionID, err)
				return acp.NewSessionResponse{}, fmt.Errorf("failed to create session orchestrator: %w", err)
			}
			logger.Debug("NewSession[%s]: using base orchestrator without ACP customizations", sessionID)
		} else {
			logger.Debug("NewSession[%s]: using orchestrator with todo actor fallback", sessionID)
		}
	} else {
		logger.Debug("NewSession[%s]: using orchestrator with ACP filesystem + todo actor", sessionID)
	}

	promptCtx, promptCancel := context.WithCancel(a.ctx)

	session := &statcodeSession{
		sessionID:     sessionID,
		orchestrator:  sessionOrch,
		promptCtx:     promptCtx,
		promptCancel:  promptCancel,
		isActive:      true,
		toolLocations: make(map[string][]acp.ToolCallLocation),
		toolParams:    make(map[string]map[string]interface{}),
		toolProgress:  make(map[string]*strings.Builder),
	}

	a.mu.Lock()
	a.sessions[sessionID] = session
	logger.Debug("NewSession[%s]: session state initialized", sessionID)
	a.mu.Unlock()

	// Set up change callback on the todo actor to send plan updates via ACP
	// Get the todo actor from the orchestrator (might be the same one we passed in, or a fallback)
	sessionTodoActor := sessionOrch.GetTodoActor()
	logger.Debug("NewSession[%s]: configuring change callback on todo actor (actor=%T)", sessionID, sessionTodoActor)
	if sessionTodoActor != nil {
		// Create a callback that sends plan updates via ACP
		callback := a.createTodoPlanUpdateCallback(sessionID)
		sessionTodoActor.SetChangeCallback(callback)
		logger.Debug("Change callback set on todo actor for session %s", sessionID)
	} else {
		logger.Warn("NewSession[%s]: todo actor missing; ACP plan updates unavailable", sessionID)
	}

	// Advertise available slash commands to the client
	availableCommands := a.GetAvailableCommands()
	if len(availableCommands) > 0 {
		// Create the session update for available commands
		update := acp.SessionUpdate{
			AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
				AvailableCommands: availableCommands,
			},
		}

		if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: acp.SessionId(sessionID),
			Update:    update,
		}); err != nil {
			logger.Warn("Failed to advertise available commands: %v", err)
			// Don't fail the session creation, just log the error
		} else {
			logger.Debug("Advertised %d available commands for session %s", len(availableCommands), sessionID)
		}
	}

	return acp.NewSessionResponse{SessionId: acp.SessionId(sessionID)}, nil
}

// Authenticate implements acp.Agent
func (a *ScriptschnellAIAgent) Authenticate(ctx context.Context, params acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	// No authentication required for now
	return acp.AuthenticateResponse{}, nil
}

// LoadSession implements acp.Agent
func (a *ScriptschnellAIAgent) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	// Session loading not supported
	return acp.LoadSessionResponse{}, fmt.Errorf("session loading not supported")
}

// Cancel implements acp.Agent
func (a *ScriptschnellAIAgent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	sessionID := string(params.SessionId)
	logger.Info("Cancelling ACP session: %s", sessionID)
	logger.Debug("Cancel[%s]: request=%+v", sessionID, params)
	if metaMap, ok := params.Meta.(map[string]any); ok {
		if reason, ok := metaMap["reason"]; ok {
			logger.Debug("Cancel[%s]: reason=%v", sessionID, reason)
		} else {
			logger.Debug("Cancel[%s]: meta=%v", sessionID, metaMap)
		}
	} else if params.Meta != nil {
		logger.Debug("Cancel[%s]: meta=%v", sessionID, params.Meta)
	} else {
		logger.Debug("Cancel[%s]: no additional metadata", sessionID)
	}

	a.mu.Lock()
	session, exists := a.sessions[sessionID]
	if exists && session.promptCancel != nil {
		session.promptCancel()
		session.isActive = false
		logger.Debug("Cancel[%s]: prompt cancelled", sessionID)
	} else if !exists {
		logger.Warn("Cancel[%s]: session not found", sessionID)
	}
	a.mu.Unlock()

	return nil
}

// Prompt implements acp.Agent - this is the main method where we process user prompts
func (a *ScriptschnellAIAgent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sessionID := string(params.SessionId)
	logger.Info("Processing prompt for ACP session: %s", sessionID)
	logger.Debug("Prompt[%s]: received %d blocks", sessionID, len(params.Prompt))

	// Get the session
	a.mu.Lock()
	session, exists := a.sessions[sessionID]
	if !exists {
		a.mu.Unlock()
		logger.Warn("Prompt[%s]: session not found", sessionID)
		return acp.PromptResponse{}, fmt.Errorf("session %s not found", sessionID)
	}
	logger.Debug("Prompt[%s]: session located", sessionID)

	// Cancel any previous prompt for this session
	if session.promptCancel != nil {
		logger.Debug("Prompt[%s]: cancelling previous prompt context", sessionID)
		session.promptCancel()
	}

	// Create new context for this prompt
	promptCtx, promptCancel := context.WithCancel(a.ctx)
	session.promptCtx = promptCtx
	session.promptCancel = promptCancel
	session.isActive = true
	logger.Debug("Prompt[%s]: prompt context initialized", sessionID)
	a.mu.Unlock()

	// Extract the text content from the prompt
	var promptText string
	for _, block := range params.Prompt {
		if block.Text != nil {
			promptText += block.Text.Text
		}
	}

	if promptText == "" {
		logger.Warn("Prompt[%s]: no text content found", sessionID)
		return acp.PromptResponse{}, fmt.Errorf("no text content found in prompt")
	}
	logger.Debug("Prompt[%s]: prompt text=%q", sessionID, truncateForLog(promptText))

	// Check for slash commands at the beginning of the prompt
	command, args, isCommand := a.parseSlashCommand(promptText)
	if isCommand {
		logger.Info("Detected slash command: /%s", command)
		logger.Debug("Prompt[%s]: slash command args=%q", sessionID, truncateForLog(args))

		// Execute the slash command
		response, err := a.executeSlashCommand(sessionID, command, args)
		if err != nil {
			logger.Error("Error executing slash command: %v", err)
			// Send error response as a message
			if sendErr := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
				SessionId: acp.SessionId(session.sessionID),
				Update:    acp.UpdateAgentMessageText(fmt.Sprintf("❌ Error executing command: %v", err)),
			}); sendErr != nil {
				logger.Error("Failed to send command error response: %v", sendErr)
			}
			return acp.PromptResponse{}, err
		}

		// Send the command response as a message
		if err := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
			SessionId: acp.SessionId(session.sessionID),
			Update:    acp.UpdateAgentMessageText(response),
		}); err != nil {
			logger.Error("Failed to send command response: %v", err)
			return acp.PromptResponse{}, err
		}

		// Clean up the prompt context
		session.mu.Lock()
		session.promptCancel = nil
		session.isActive = false
		session.mu.Unlock()

		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	// Process the prompt using the orchestrator with ACP callbacks
	err := a.processPromptWithStreaming(session, promptText)
	if err != nil {
		if session.promptCtx.Err() != nil {
			// Prompt was cancelled
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		logger.Error("Error processing prompt: %v", err)
		return acp.PromptResponse{}, err
	}

	// Clean up the prompt context
	session.mu.Lock()
	session.promptCancel = nil
	session.isActive = false
	session.mu.Unlock()

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// SetSessionMode implements acp.Agent
func (a *ScriptschnellAIAgent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

// SetSessionModel implements acp.AgentExperimental
func (a *ScriptschnellAIAgent) SetSessionModel(ctx context.Context, params acp.SetSessionModelRequest) (acp.SetSessionModelResponse, error) {
	return acp.SetSessionModelResponse{}, nil
}

// processPromptWithStreaming processes a prompt using the orchestrator and streams updates via ACP
func (a *ScriptschnellAIAgent) processPromptWithStreaming(session *statcodeSession, promptText string) error {
	logger.Debug("processPromptWithStreaming[%s]: prompt=%q", session.sessionID, truncateForLog(promptText))

	// Buffer for handling line boundaries properly
	var textBuffer strings.Builder
	var lastSentLength int
	var mu sync.Mutex
	var toolProgressMu sync.Mutex
	activeToolCalls := make(map[string]string)

	// Create streaming callback for ACP
	streamChunk := func(chunk string) error {
		logger.Debug("streamCallback[%s]: chunk=%q", session.sessionID, truncateForLog(chunk))
		mu.Lock()
		defer mu.Unlock()

		// Check if this is a status message that should be sent immediately
		// Status messages typically start with →, ✓, 🔍, or contain tool call indicators
		isStatusMessage := strings.HasPrefix(chunk, "→ ") ||
			strings.HasPrefix(chunk, "✓ ") ||
			strings.HasPrefix(chunk, "🔍 ") ||
			strings.Contains(chunk, "**") &&
				(strings.Contains(chunk, "search_") || strings.Contains(chunk, "read_file") || strings.Contains(chunk, "parallel_tools"))

		if isStatusMessage {
			// Send status messages immediately without buffering
			if err := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
				SessionId: acp.SessionId(session.sessionID),
				Update:    acp.UpdateAgentMessageText(chunk),
			}); err != nil {
				logger.Error("Failed to stream status message: %v", err)
				return err
			}
			return nil
		}

		// Regular LLM content - apply buffering for proper newline handling
		// Add the new chunk to our buffer
		textBuffer.WriteString(chunk)
		bufferedText := textBuffer.String()

		// Only send text that ends with a newline or if this is likely the final chunk
		// This helps ensure proper line boundaries for editors like Zed
		shouldSend := false

		// If the buffer ends with a newline, send everything up to but not including the last newline
		if strings.HasSuffix(bufferedText, "\n") {
			// Send the complete buffer (including the newline)
			shouldSend = true
		} else if len(bufferedText)-lastSentLength > 100 {
			// If we've accumulated a substantial amount without a newline, send it anyway
			// to avoid too-long delays in streaming
			shouldSend = true
		}

		if shouldSend {
			// Send the accumulated text since last send
			textToSend := bufferedText[lastSentLength:]
			if textToSend != "" {
				logger.Debug("streamCallback[%s]: flushing %d bytes", session.sessionID, len(textToSend))
				if err := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
					SessionId: acp.SessionId(session.sessionID),
					Update:    acp.UpdateAgentMessageText(textToSend),
				}); err != nil {
					logger.Error("Failed to stream chunk: %v", err)
					return err
				}
				lastSentLength = len(bufferedText)
			}
		}

		return nil
	}

	// Create progress callback
	progressCallback := func(update progress.Update) error {
		normalized := progress.Normalize(update)

		// Log for debugging
		if normalized.ShouldStatus() {
			logger.Debug("Prompt[%s] status: %s", session.sessionID, normalized.Message)
		}

		// Stream messages that should be shown to the user in the main conversation
		if normalized.ShouldStream() && normalized.Message != "" {
			return streamChunk(normalized.Message)
		}

		return nil
	}

	// Create context usage callback
	contextCallback := func(percent int, contextWindow int) error {
		// Could be exposed as ACP updates if needed
		logger.Debug("Context usage[%s]: %d%% of %d", session.sessionID, percent, contextWindow)
		return nil
	}

	// Create authorization callback for ACP
	authCallback := func(toolName string, params map[string]interface{}, reason string) (bool, error) {
		logger.Debug("authCallback[%s]: tool=%s reason=%q params=%s", session.sessionID, toolName, reason, truncateMapForLog(params))
		allowed, err := a.handleACPAuthorization(session, toolName, params, reason)
		logger.Debug("authCallback[%s]: tool=%s allowed=%t err=%v", session.sessionID, toolName, allowed, err)
		return allowed, err
	}

	// Create tool call callbacks
	toolCallCallback := func(toolName, toolID string, parameters map[string]interface{}) error {
		logger.Debug("toolCallCallback[%s]: start tool=%s id=%s params=%s", session.sessionID, toolName, toolID, truncateMapForLog(parameters))
		toolProgressMu.Lock()
		activeToolCalls[toolID] = toolName
		toolProgressMu.Unlock()
		startErr := a.handleToolCallStart(session, toolName, toolID, parameters)
		if startErr != nil {
			logger.Warn("toolCallCallback[%s]: failed to notify start for tool=%s id=%s err=%v", session.sessionID, toolName, toolID, startErr)
		}
		return startErr
	}

	toolResultCallback := func(toolName, toolID, result, errorMsg string) error {
		logger.Debug("toolResultCallback[%s]: done tool=%s id=%s err=%q resultLen=%d", session.sessionID, toolName, toolID, errorMsg, len(result))
		toolProgressMu.Lock()
		delete(activeToolCalls, toolID)
		toolProgressMu.Unlock()
		resultErr := a.handleToolCallResult(session, toolName, toolID, result, errorMsg)
		if resultErr != nil {
			logger.Warn("toolResultCallback[%s]: failed to notify result for tool=%s id=%s err=%v", session.sessionID, toolName, toolID, resultErr)
		}
		return resultErr
	}

	// Create enhanced progress callback that can send tool progress
	enhancedProgressCallback := func(update progress.Update) error {
		normalized := progress.Normalize(update)

		// Send regular progress/status updates to the main conversation
		if err := progressCallback(normalized); err != nil {
			return err
		}

		// Forward status updates to active tool calls for ACP tool progress streaming
		// This ensures tools can show progress in their tool call UI
		if normalized.ShouldStatus() && normalized.Message != "" {
			toolProgressMu.Lock()
			activeIDs := make([]string, 0, len(activeToolCalls))
			for id := range activeToolCalls {
				activeIDs = append(activeIDs, id)
			}
			toolProgressMu.Unlock()

			for _, toolID := range activeIDs {
				if err := a.sendToolCallProgress(session, toolID, normalized.Message); err != nil {
					logger.Warn("Failed to send tool progress for %s: %v", toolID, err)
					// Don't fail the whole operation
				}
			}
		}

		return nil
	}

	// Set up the ACP interaction handler for authorization
	handler := NewACPInteractionHandler(a.conn, session.sessionID)
	if err := session.orchestrator.SetUserInteractionHandler(handler); err != nil {
		logger.Warn("processPromptWithStreaming[%s]: failed to set user interaction handler: %v", session.sessionID, err)
		// Continue with legacy callback as fallback
	}

	// Process the prompt with the orchestrator
	err := session.orchestrator.ProcessPrompt(
		session.promptCtx,
		promptText,
		enhancedProgressCallback,
		contextCallback,
		authCallback, // Kept as fallback
		toolCallCallback,
		toolResultCallback,
		nil, // OpenRouter usage callback - not used in ACP
	)

	// Send any remaining text in the buffer after processing completes
	mu.Lock()
	finalText := textBuffer.String()[lastSentLength:]
	if finalText != "" {
		logger.Debug("processPromptWithStreaming[%s]: sending final %d bytes", session.sessionID, len(finalText))
		if sendErr := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
			SessionId: acp.SessionId(session.sessionID),
			Update:    acp.UpdateAgentMessageText(finalText),
		}); sendErr != nil {
			logger.Error("Failed to send final text chunk: %v", sendErr)
			// Don't overwrite the original error if there was one
			if err == nil {
				err = sendErr
			}
		}
	}
	mu.Unlock()

	if err != nil {
		logger.Debug("processPromptWithStreaming[%s]: completed with error: %v", session.sessionID, err)
	} else {
		logger.Debug("processPromptWithStreaming[%s]: completed successfully", session.sessionID)
	}

	return err
}

// handleACPAuthorization handles permission requests via ACP
func (a *ScriptschnellAIAgent) handleACPAuthorization(session *statcodeSession, toolName string, params map[string]interface{}, reason string) (bool, error) {
	// Request permission from the client
	logger.Debug("handleACPAuthorization[%s]: requesting permission for tool=%s", session.sessionID, toolName)
	permResp, err := a.conn.RequestPermission(session.promptCtx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(session.sessionID),
		ToolCall: acp.RequestPermissionToolCall{
			ToolCallId: acp.ToolCallId(fmt.Sprintf("tool_%d", time.Now().UnixNano())),
			Title:      acp.Ptr(fmt.Sprintf("Execute %s", toolName)),
			Kind:       acp.Ptr(acp.ToolKindEdit), // Default to edit, could be more specific
			Status:     acp.Ptr(acp.ToolCallStatusPending),
			Locations:  []acp.ToolCallLocation{}, // Could extract file paths from params
			RawInput:   params,
		},
		Options: []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: acp.PermissionOptionId("allow")},
			{Kind: acp.PermissionOptionKindRejectOnce, Name: "Deny", OptionId: acp.PermissionOptionId("deny")},
		},
	})

	if err != nil {
		logger.Warn("handleACPAuthorization[%s]: permission request failed: %v", session.sessionID, err)
		return false, err
	}

	if permResp.Outcome.Cancelled != nil {
		logger.Debug("handleACPAuthorization[%s]: permission cancelled by client", session.sessionID)
		return false, fmt.Errorf("authorization cancelled")
	}

	if permResp.Outcome.Selected == nil {
		logger.Debug("handleACPAuthorization[%s]: no option selected", session.sessionID)
		return false, fmt.Errorf("no authorization option selected")
	}

	switch string(permResp.Outcome.Selected.OptionId) {
	case "allow":
		logger.Debug("handleACPAuthorization[%s]: tool=%s authorized", session.sessionID, toolName)
		return true, nil
	case "deny":
		logger.Debug("handleACPAuthorization[%s]: tool=%s denied", session.sessionID, toolName)
		return false, nil
	default:
		logger.Warn("handleACPAuthorization[%s]: unexpected option %s", session.sessionID, permResp.Outcome.Selected.OptionId)
		return false, fmt.Errorf("unexpected authorization option: %s", permResp.Outcome.Selected.OptionId)
	}
}

// getToolKind determines the appropriate tool kind based on tool name and parameters
func (a *ScriptschnellAIAgent) getToolKind(toolName string, parameters map[string]interface{}) acp.ToolKind {
	switch toolName {
	case "read_file", "read_file_summarized":
		return acp.ToolKindRead
	case "create_file", "edit_file", "write_file_replace":
		return acp.ToolKindEdit // Use Edit instead of Write
	case "shell", "go_sandbox":
		return acp.ToolKindExecute
	case "search_file_content", "search_files", "web_search":
		return acp.ToolKindSearch
	case "todo":
		return acp.ToolKindEdit // Use Edit instead of Plan
	case "parallel_tool_execution":
		return acp.ToolKindEdit // Use Edit instead of Orchestrate
	case "status_program", "stop_program":
		return acp.ToolKindEdit // Use Edit instead of Monitor
	case "codebase_investigator":
		return acp.ToolKindEdit // Use Edit instead of Analyze
	default:
		return acp.ToolKindEdit // Default fallback
	}
}

// extractLocations extracts file locations from tool parameters
func (a *ScriptschnellAIAgent) extractLocations(toolName string, parameters map[string]interface{}) []acp.ToolCallLocation {
	var locations []acp.ToolCallLocation

	switch toolName {
	case "read_file", "create_file", "edit_file", "write_file_replace":
		if path, ok := parameters["path"].(string); ok {
			// Convert relative paths to absolute for better client display
			if !strings.HasPrefix(path, "/") {
				// Get working directory from session context or config
				path = a.config.WorkingDir + "/" + path
			}
			locations = append(locations, acp.ToolCallLocation{
				Path: path,
			})
		}
	case "search_file_content", "search_files":
		if path, ok := parameters["path"].(string); ok {
			if !strings.HasPrefix(path, "/") {
				path = a.config.WorkingDir + "/" + path
			}
			locations = append(locations, acp.ToolCallLocation{
				Path: path,
			})
		}
	case "shell", "go_sandbox":
		// Shell commands might affect multiple files, but we can't easily predict them
		// Could potentially parse command to extract file paths
		// This is crude, could be improved with better parsing
	}

	return locations
}

// shouldCreateTerminal determines if a shell command should create a terminal
func (a *ScriptschnellAIAgent) shouldCreateTerminal(toolName string, parameters map[string]interface{}) bool {
	if toolName != "shell" && toolName != "go_sandbox" {
		return false
	}

	// Never attempt terminal methods if the client doesn't support them
	if !a.supportsTerminalProtocol() {
		return false
	}

	// Check if command is likely to be long-running or interactive
	if cmd, ok := parameters["command"].(string); ok {
		// Background commands (ending with &)
		if strings.Contains(cmd, " &") {
			return true
		}

		// Interactive commands
		interactiveCmds := []string{"vim", "nano", "emacs", "top", "htop", "less", "more", "man"}
		for _, interactive := range interactiveCmds {
			if strings.Contains(cmd, interactive) {
				return true
			}
		}

		// Long-running commands
		longRunningCmds := []string{"watch", "tail -f", "ping", "wget", "curl", "npm run", "yarn start", "go run"}
		for _, longRunning := range longRunningCmds {
			if strings.Contains(cmd, longRunning) {
				return true
			}
		}

		// Development servers
		if strings.Contains(cmd, "serve") || strings.Contains(cmd, "dev") || strings.Contains(cmd, "start") {
			return true
		}
	}

	return false
}

// createTerminalForToolCall creates a terminal for the given tool call if appropriate
func (a *ScriptschnellAIAgent) createTerminalForToolCall(session *statcodeSession, toolName, toolID string, parameters map[string]interface{}) (string, error) {
	if !a.shouldCreateTerminal(toolName, parameters) {
		return "", nil
	}

	// If terminal protocol isn't supported, don't attempt to create one
	if !a.supportsTerminalProtocol() {
		logger.Debug("Terminal protocol not supported by client; skipping terminal creation for tool %s", toolName)
		return "", nil
	}

	// Create a terminal
	termID := fmt.Sprintf("term_%s_%d", toolID, time.Now().UnixNano())

	// For now, we'll simulate terminal creation by returning a terminal ID
	// In a full implementation, we would use acp.TerminalCreate
	// This requires the client to support terminal protocol
	logger.Debug("Would create terminal %s for tool %s", termID, toolName)

	return termID, nil
}

// handleToolCallStart handles the start of a tool call
func (a *ScriptschnellAIAgent) handleToolCallStart(session *statcodeSession, toolName, toolID string, parameters map[string]interface{}) error {
	toolKind := a.getToolKind(toolName, parameters)
	locations := a.extractLocations(toolName, parameters)
	a.rememberToolContext(session, toolID, parameters, locations)

	// Check if we should create a terminal for this tool call
	terminalID, err := a.createTerminalForToolCall(session, toolName, toolID, parameters)
	if err != nil {
		logger.Warn("Failed to create terminal for tool %s: %v", toolName, err)
	}

	// Create a more descriptive title
	title := fmt.Sprintf("Executing %s", toolName)

	// Check for description parameter (used by go_sandbox and other tools)
	if description, ok := parameters["description"].(string); ok && description != "" {
		// Use description if provided
		title = description
	} else {
		// Fall back to tool kind-based titles
		switch toolKind {
		case acp.ToolKindRead:
			if path, ok := parameters["path"].(string); ok {
				title = fmt.Sprintf("Reading %s", path)
			}
		case acp.ToolKindEdit:
			if path, ok := parameters["path"].(string); ok {
				title = fmt.Sprintf("Writing %s", path)
			}
		case acp.ToolKindExecute:
			if cmd, ok := parameters["command"].(string); ok {
				title = fmt.Sprintf("Running: %s", cmd)
				if len(title) > 50 {
					title = "Running command"
				}
			}
		case acp.ToolKindSearch:
			if pattern, ok := parameters["pattern"].(string); ok {
				title = fmt.Sprintf("Searching for %s", pattern)
				if len(title) > 50 {
					title = "Searching files"
				}
			}
		}
	}

	// Notify the client about the tool call
	opts := []acp.ToolCallStartOpt{
		acp.WithStartKind(toolKind),
		acp.WithStartStatus(acp.ToolCallStatusPending),
		acp.WithStartRawInput(parameters),
	}
	if len(locations) > 0 {
		opts = append(opts, acp.WithStartLocations(locations))
	}

	update := acp.StartToolCall(
		acp.ToolCallId(toolID),
		title,
		opts...,
	)

	if err := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
		SessionId: acp.SessionId(session.sessionID),
		Update:    update,
	}); err != nil {
		logger.Warn("handleToolCallStart[%s]: failed to send start update for tool %s: %v", session.sessionID, toolName, err)
		return err
	}

	// Send an in_progress update to show the tool is starting execution
	progressUpdate := acp.UpdateToolCall(
		acp.ToolCallId(toolID),
		acp.WithUpdateStatus(acp.ToolCallStatusInProgress),
	)

	// Add terminal content if we created one
	if terminalID != "" {
		progressUpdate.ToolCallUpdate.Content = []acp.ToolCallContent{
			acp.ToolTerminalRef(terminalID),
		}
	}

	if err := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
		SessionId: acp.SessionId(session.sessionID),
		Update:    progressUpdate,
	}); err != nil {
		logger.Warn("handleToolCallStart[%s]: failed to send in-progress update for tool %s: %v", session.sessionID, toolName, err)
		// Don't fail the whole operation for this
	}

	return nil
}

// formatToolResultContent formats the tool result into appropriate ACP content types
func (a *ScriptschnellAIAgent) formatToolResultContent(toolName string, result string, params map[string]interface{}) []acp.ToolCallContent {
	if strings.TrimSpace(result) == "" {
		return nil
	}

	switch toolName {
	case "edit_file", "write_file_simple_diff", "write_file_replace", "create_file":
		if diffContent := a.parseDiffContent(result, params); len(diffContent) > 0 {
			return diffContent
		}
	}

	return []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(result))}
}

func (a *ScriptschnellAIAgent) parseDiffContent(diffText string, params map[string]interface{}) []acp.ToolCallContent {
	fileDiffs, err := godiff.ParseMultiFileDiff([]byte(diffText))
	if err != nil {
		return nil
	}

	var contents []acp.ToolCallContent

	for _, fd := range fileDiffs {
		if fd == nil {
			continue
		}

		path := a.resolveDiffPath(fd, params)
		if path == "" {
			continue
		}

		newContent, readErr := os.ReadFile(path)
		if readErr != nil {
			logger.Debug("formatToolResultContent: unable to read updated file %s: %v", path, readErr)
		}

		finalText := string(newContent)
		if fd.NewName == "/dev/null" {
			// File was deleted
			finalText = ""
		} else if finalText == "" && readErr != nil {
			// Fall back to the diff text so the client still shows context
			finalText = diffText
		}

		contents = append(contents, acp.ToolDiffContent(path, finalText))
	}

	return contents
}

func (a *ScriptschnellAIAgent) resolveDiffPath(fd *godiff.FileDiff, params map[string]interface{}) string {
	if fd == nil {
		return ""
	}

	candidate := strings.TrimSpace(fd.NewName)
	if candidate == "" || candidate == "/dev/null" {
		candidate = strings.TrimSpace(fd.OrigName)
	}

	candidate = strings.Trim(candidate, "\"")
	candidate = strings.TrimPrefix(candidate, "a/")
	candidate = strings.TrimPrefix(candidate, "b/")

	if candidate == "" && params != nil {
		if pathVal, ok := params["path"].(string); ok {
			candidate = pathVal
		}
	}

	if candidate == "" {
		return ""
	}

	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(a.config.WorkingDir, candidate)
	}

	return filepath.Clean(candidate)
}

// sendToolCallProgress accumulates intermediate progress updates for long-running tools
// Instead of sending individual ACP updates that replace content, we accumulate
// the progress and include it in the final result to avoid content replacement issues
func (a *ScriptschnellAIAgent) sendToolCallProgress(session *statcodeSession, toolID string, message string) error {
	// Accumulate the progress message locally instead of sending immediate ACP updates
	// This prevents the ACP client from replacing content with each progress update
	a.accumulateToolProgress(session, toolID, message)
	logger.Debug("sendToolCallProgress[%s]: accumulated progress for tool %s: %q", session.sessionID, toolID, truncateForLog(message))
	return nil
}

// handleToolCallResult handles the completion of a tool call
func (a *ScriptschnellAIAgent) handleToolCallResult(session *statcodeSession, toolName, toolID, result, errorMsg string) error {
	params, locations, progressText := a.popToolContext(session, toolID)

	status := acp.ToolCallStatusCompleted
	var content []acp.ToolCallContent

	if errorMsg != "" {
		status = acp.ToolCallStatusFailed
		content = append(content, acp.ToolContent(acp.TextBlock(fmt.Sprintf("Error: %s", errorMsg))))
	} else {
		// Include accumulated progress output along with the final result
		combinedResult := result
		if progressText != "" {
			// Combine progress and final result, with progress first to show the stream
			if combinedResult != "" {
				combinedResult = progressText + combinedResult
			} else {
				combinedResult = progressText
			}
		}
		content = a.formatToolResultContent(toolName, combinedResult, params)
	}

	// Update the client about the tool call result
	rawOutput := map[string]interface{}{"result": result}
	if progressText != "" {
		rawOutput["progress_output"] = progressText
	}
	if errorMsg != "" {
		rawOutput["error"] = errorMsg
	}

	updateOpts := []acp.ToolCallUpdateOpt{
		acp.WithUpdateStatus(status),
		acp.WithUpdateContent(content),
		acp.WithUpdateRawOutput(rawOutput),
	}
	if len(locations) > 0 {
		updateOpts = append(updateOpts, acp.WithUpdateLocations(locations))
	}

	if err := a.conn.SessionUpdate(session.promptCtx, acp.SessionNotification{
		SessionId: acp.SessionId(session.sessionID),
		Update: acp.UpdateToolCall(
			acp.ToolCallId(toolID),
			updateOpts...,
		),
	}); err != nil {
		logger.Warn("handleToolCallResult[%s]: failed to send result for tool %s (status=%s): %v", session.sessionID, toolName, status, err)
		return err
	}

	logger.Debug("handleToolCallResult[%s]: tool %s completed with status %s (progressLen=%d, resultLen=%d)", session.sessionID, toolName, status, len(progressText), len(result))
	return nil
}

// supportsFilesystemProtocol checks if the client supports the filesystem protocol
func (a *ScriptschnellAIAgent) supportsFilesystemProtocol() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.clientCaps == nil {
		return false
	}

	return a.clientCaps.Fs.ReadTextFile && a.clientCaps.Fs.WriteTextFile
}

// supportsTerminalProtocol checks if the client supports terminal methods
func (a *ScriptschnellAIAgent) supportsTerminalProtocol() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.clientCaps == nil {
		return false
	}

	return a.clientCaps.Terminal
}

// createFilesystemForSession creates a filesystem implementation for the session
func (a *ScriptschnellAIAgent) createFilesystemForSession(sessionID string, clientSupportsFS bool) (fs.FileSystem, error) {
	if clientSupportsFS {
		// Create a filesystem that uses the client's filesystem protocol
		return NewACPFileSystem(a.conn, sessionID, a.config.WorkingDir), nil
	}

	// Use the local filesystem
	return fs.NewCachedFS(
		a.config.WorkingDir,
		time.Duration(a.config.CacheTTL)*time.Second,
		a.config.MaxCacheEntries,
	), nil
}

// Close cleans up the agent and all its sessions
func (a *ScriptschnellAIAgent) Close() error {
	logger.Info("Closing ACP agent")

	a.cancel()

	a.mu.Lock()
	defer a.mu.Unlock()

	logger.Debug("Close: shutting down %d active sessions", len(a.sessions))

	// Cancel all active sessions and clean up orchestrators
	for sessionID, session := range a.sessions {
		if session.promptCancel != nil {
			session.promptCancel()
		}
		logger.Debug("Close: tearing down session %s", sessionID)
		session.orchestrator.Close()
		delete(a.sessions, sessionID)
	}

	// Close the main orchestrator
	if a.orchestrator != nil {
		logger.Debug("Close: closing base orchestrator")
		a.orchestrator.Close()
	}

	return nil
}

// RunACPAgent starts an ACP agent server using stdio
func RunACPAgent(ctx context.Context, cfg *config.Config, providerMgr *provider.Manager) error {
	logger.Info("Starting scriptschnell ACP Agent")

	agent, err := NewScriptschnellAIAgent(ctx, cfg, providerMgr)
	if err != nil {
		return fmt.Errorf("failed to create ACP agent: %w", err)
	}
	defer agent.Close()

	// Create the ACP connection for stdio communication
	conn := acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	// Route ACP SDK logs through our logger to avoid stdout writes
	if handler := logger.NewSlogHandler(logger.Global()); handler != nil {
		conn.SetLogger(slog.New(handler))
	}
	agent.SetAgentConnection(conn)

	// Block until the connection is done
	<-conn.Done()
	logger.Info("ACP Agent connection closed")

	return nil
}
