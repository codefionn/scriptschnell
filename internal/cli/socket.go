package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/socketclient"
	"github.com/codefionn/scriptschnell/internal/socketutil"
)

// DetectSocketServer checks if a socket server is running at the configured path.
// Deprecated: Use socketutil.DetectSocketServer instead.
func DetectSocketServer(cfg *config.Config) bool {
	return socketutil.DetectSocketServer(cfg)
}

// ShouldUseSocketMode determines if CLI should use socket mode based on config and detection.
// Deprecated: Use socketutil.ShouldUseSocketMode instead.
func ShouldUseSocketMode(cfg *config.Config, opts *Options) bool {
	// Check if socket mode is explicitly disabled via flag
	noSocket := opts != nil && opts.NoSocket
	return socketutil.ShouldUseSocketMode(cfg, noSocket)
}

// SocketCLI handles command-line interface using the socket server
type SocketCLI struct {
	config     *config.Config
	client     *socketclient.Client
	options    *Options
	sessionID  string
	socketPath string
	completion atomic.Bool
}

// NewSocket creates a new socket-based CLI runner
func NewSocket(cfg *config.Config, opts *Options) (*SocketCLI, error) {
	// Determine socket path
	socketPath := cfg.Socket.GetSocketPath()
	if opts != nil && opts.SocketClientPath != "" {
		socketPath = opts.SocketClientPath
	}

	// Expand ~ if present
	if strings.HasPrefix(socketPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to expand home directory: %w", err)
		}
		socketPath = home + socketPath[1:]
	}

	// Create socket client config
	clientConfig := socketclient.DefaultConfig()
	clientConfig.SocketPath = socketPath
	clientConfig.ReconnectEnabled = false // CLI mode doesn't need reconnection
	clientConfig.ConnectTimeout = 5 * time.Second
	clientConfig.ReadTimeout = 60 * time.Second
	clientConfig.WriteTimeout = 10 * time.Second

	// Create socket client
	client, err := socketclient.NewClientWithConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket client: %w", err)
	}

	return &SocketCLI{
		config:     cfg,
		client:     client,
		options:    opts,
		socketPath: socketPath,
	}, nil
}

// Connect establishes connection to the socket server
func (c *SocketCLI) Connect(ctx context.Context) error {
	if err := c.client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to socket server: %w", err)
	}
	logger.Info("Connected to scriptschnell socket server at %s", c.socketPath)
	return nil
}

// Close releases resources
func (c *SocketCLI) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Run executes a single prompt using the socket server
func (c *SocketCLI) Run(ctx context.Context, prompt string) error {
	// Set up progress callback for streaming output
	progressCallback := func(update progress.Update) error {
		if c.options != nil && (c.options.JSONOutput || c.options.JSONExtended || c.options.JSONFull) {
			return nil
		}
		normalized := progress.Normalize(update)
		if normalized.ShouldStatus() {
			if normalized.Message == "" {
				return nil
			}
			msg := normalized.Message
			if !strings.HasSuffix(msg, "\n") {
				msg += "\n"
			}
			fmt.Fprint(os.Stderr, msg)
			return nil
		}
		if normalized.Message == "" || !normalized.ShouldStream() {
			return nil
		}
		fmt.Print(normalized.Message)
		return nil
	}

	// Register progress callback
	c.client.SetProgressCallback(func(msg socketclient.ProgressData) {
		if msg.Message != "" || msg.Reasoning != "" {
			// Convert string mode to ReportMode
			var mode progress.ReportMode
			if msg.Mode != "" {
				modeVal, err := strconv.Atoi(msg.Mode)
				if err == nil {
					mode = progress.ReportMode(modeVal)
				}
			}
			progressCallback(progress.Update{
				Message:   msg.Message,
				Reasoning: msg.Reasoning,
				Mode:      mode,
				Ephemeral: msg.Ephemeral,
			})
		}
	})

	// Register authorization callback
	c.client.SetAuthorizationCallback(func(req socketclient.AuthorizationRequest) (bool, error) {
		if c.options != nil && c.options.DangerouslyAllowAll {
			// Auto-approve everything
			fmt.Fprintf(os.Stderr, "[Auto-approved: %s]\n", req.ToolName)
			return true, nil
		}

		// In CLI mode without auto-approval, deny by default
		// (Interactive approval would require more complex TTY handling)
		return false, fmt.Errorf("authorization required but not granted via CLI flags")
	})

	// Register question callback
	c.client.SetQuestionCallback(func(req socketclient.QuestionRequest) (map[string]string, error) {
		// CLI mode doesn't support interactive questions
		return nil, fmt.Errorf("interactive questions not supported in CLI mode")
	})

	// Register completion callback
	c.completion.Store(false)
	c.client.SetCompletionCallback(func(requestID string, success bool, errorMsg string) {
		c.completion.Store(true)
		if !success && errorMsg != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", errorMsg)
		}
	})

	// Determine working directory
	workingDir := c.config.WorkingDir
	if workingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		workingDir = wd
	}

	// Create or attach to session
	if err := c.setupSession(ctx, workingDir); err != nil {
		return fmt.Errorf("failed to setup session: %w", err)
	}

	// Send chat message
	if err := c.client.SendChat(ctx, prompt, nil); err != nil {
		return fmt.Errorf("failed to send chat message: %w", err)
	}

	// Wait for completion
	if err := c.waitForCompletion(ctx); err != nil {
		return err
	}

	// Print final newline if not JSON mode
	if c.options == nil || (!c.options.JSONOutput && !c.options.JSONExtended && !c.options.JSONFull) {
		fmt.Println()
	}

	// Print usage statistics
	if err := c.printUsageStats(ctx); err != nil {
		return err
	}

	// Output JSON if requested
	if c.options != nil {
		if c.options.JSONFull {
			return c.outputJSONFull(ctx)
		}
		if c.options.JSONOutput {
			return c.outputJSON(ctx)
		}
		if c.options.JSONExtended {
			return c.outputJSONExtended(ctx)
		}
	}

	return nil
}

// setupSession creates a new session or attaches to an existing one
func (c *SocketCLI) setupSession(ctx context.Context, workingDir string) error {
	// List existing sessions for this workspace
	sessions, err := c.client.ListSessions(ctx, workingDir)
	if err != nil {
		logger.Warn("Failed to list sessions: %v", err)
	}

	// Strategy: Look for a recent session (less than 1 hour old) to reuse
	var recentSession *socketclient.SessionInfo
	now := time.Now()
	for _, sess := range sessions {
		// Parse UpdatedAt as time
		updatedAt, err := time.Parse(time.RFC3339, sess.UpdatedAt)
		if err != nil {
			// If parsing fails, skip this session
			continue
		}
		if now.Sub(updatedAt) < 1*time.Hour {
			recentSession = &sess
			break
		}
	}

	// If there's a recent session, try to attach to it
	if recentSession != nil {
		logger.Info("Attaching to recent session: %s", recentSession.SessionID)
		if err := c.client.AttachSession(ctx, recentSession.SessionID); err != nil {
			logger.Warn("Failed to attach to session %s: %v, creating new session", recentSession.SessionID, err)
			// Fall through to create new session
		} else {
			c.sessionID = recentSession.SessionID
			logger.Info("Attached to session: %s", recentSession.SessionID)
			return nil
		}
	}

	// Create a new session
	logger.Info("Creating new session in workspace: %s", workingDir)
	sessionID, err := c.client.CreateSession(ctx, "", "", workingDir)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	c.sessionID = sessionID
	logger.Info("Created new session: %s", sessionID)
	return nil
}

// waitForCompletion waits for the chat operation to complete
func (c *SocketCLI) waitForCompletion(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("operation timed out after 30 minutes")
		case <-ticker.C:
			if c.completion.Load() {
				return nil
			}
		}
	}
}

// printUsageStats prints usage statistics from the session
func (c *SocketCLI) printUsageStats(ctx context.Context) error {
	// Get session info
	sessionInfo, err := c.client.GetSessionInfo(ctx)
	if err != nil {
		logger.Debug("Failed to get session info: %v", err)
		return nil
	}

	if c.options != nil && (c.options.JSONOutput || c.options.JSONExtended || c.options.JSONFull) {
		return nil
	}

	// Print usage statistics
	fmt.Fprintf(os.Stderr, "\n--- Usage Statistics ---\n")

	if sessionInfo.TotalTokens > 0 {
		fmt.Fprintf(os.Stderr, "Total tokens: %d\n", sessionInfo.TotalTokens)
	}

	if sessionInfo.CachedTokens > 0 {
		percentage := float64(sessionInfo.CachedTokens) / float64(sessionInfo.TotalTokens) * 100
		fmt.Fprintf(os.Stderr, "Cached tokens: %d (%.1f%%)\n", sessionInfo.CachedTokens, percentage)
	}

	if sessionInfo.TotalCost > 0 {
		fmt.Fprintf(os.Stderr, "Total cost: $%.6f\n", sessionInfo.TotalCost)
	}

	fmt.Fprintf(os.Stderr, "------------------------\n")
	return nil
}

// outputJSON outputs the last assistant message as JSON
func (c *SocketCLI) outputJSON(ctx context.Context) error {
	result := map[string]interface{}{
		"message": c.lastAssistantMessage(ctx),
	}

	if usage := c.buildUsageSummary(ctx); len(usage) > 0 {
		result["usage"] = usage
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON output: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// outputJSONExtended outputs all messages as JSON one-liners
func (c *SocketCLI) outputJSONExtended(ctx context.Context) error {
	// Get session info with message history
	sessionInfo, err := c.client.GetSessionInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}

	if sessionInfo.MessageHistory == nil {
		return fmt.Errorf("no message history available")
	}

	// Output each message as a JSON one-liner
	for _, msg := range sessionInfo.MessageHistory {
		msgObj := map[string]interface{}{
			"role":      msg.Role,
			"timestamp": msg.Timestamp.Format(time.RFC3339),
		}

		if msg.Content != "" {
			msgObj["content"] = msg.Content
		}

		if msg.Reasoning != "" {
			msgObj["reasoning"] = msg.Reasoning
		}

		data, err := json.Marshal(msgObj)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	}

	// Output usage summary
	if usage := c.buildUsageSummary(ctx); len(usage) > 0 {
		usageData, err := json.MarshalIndent(map[string]interface{}{
			"usage": usage,
		}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(usageData))
	}

	return nil
}

// outputJSONFull outputs all messages with full tool call outputs
func (c *SocketCLI) outputJSONFull(ctx context.Context) error {
	// Get session info with message history
	sessionInfo, err := c.client.GetSessionInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}

	if sessionInfo.MessageHistory == nil {
		return fmt.Errorf("no message history available")
	}

	// Build full output object
	output := map[string]interface{}{
		"messages": sessionInfo.MessageHistory,
	}

	// Add tool calls if available
	// Note: Tool call details would need to be tracked separately
	// For now, we'll include what's available in the session info

	// Add usage statistics
	if usage := c.buildUsageSummary(ctx); len(usage) > 0 {
		output["usage"] = usage
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON output: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// lastAssistantMessage extracts the last assistant message from session history
func (c *SocketCLI) lastAssistantMessage(ctx context.Context) string {
	sessionInfo, err := c.client.GetSessionInfo(ctx)
	if err != nil {
		return ""
	}

	if sessionInfo.MessageHistory == nil {
		return ""
	}

	// Find last assistant message
	for i := len(sessionInfo.MessageHistory) - 1; i >= 0; i-- {
		msg := sessionInfo.MessageHistory[i]
		if msg.Role == "assistant" && msg.Content != "" {
			return msg.Content
		}
	}

	return ""
}

// buildUsageSummary builds a usage summary map
func (c *SocketCLI) buildUsageSummary(ctx context.Context) map[string]interface{} {
	sessionInfo, err := c.client.GetSessionInfo(ctx)
	if err != nil {
		return nil
	}

	usage := make(map[string]interface{})

	if sessionInfo.TotalTokens > 0 {
		usage["total_tokens"] = sessionInfo.TotalTokens
	}

	if sessionInfo.CachedTokens > 0 {
		usage["cached_tokens"] = sessionInfo.CachedTokens
	}

	if sessionInfo.TotalCost > 0 {
		usage["total_cost"] = sessionInfo.TotalCost
	}

	if sessionInfo.MessageCount > 0 {
		usage["message_count"] = sessionInfo.MessageCount
	}

	return usage
}
