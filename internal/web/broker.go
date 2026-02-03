package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/orchestrator"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/securemem"
	"github.com/codefionn/scriptschnell/internal/session"
)

// MessageBroker handles the orchestration of LLM interactions for web clients
type MessageBroker struct {
	session      *session.Session
	orchestrator *orchestrator.Orchestrator
	cfg          *config.Config
	providerMgr  *provider.Manager
	initialized  bool
}

// NewMessageBroker creates a new message broker
func NewMessageBroker() *MessageBroker {
	return &MessageBroker{}
}

// InitializeSession initializes a new session
func (mb *MessageBroker) InitializeSession(cfg *config.Config, providerMgr *provider.Manager, secretsPassword *securemem.String) error {
	mb.cfg = cfg
	mb.providerMgr = providerMgr

	// Create new session
	sess := session.NewSession(session.GenerateID(), cfg.WorkingDir)
	mb.session = sess

	// Create filesystem
	filesystem := fs.NewCachedFS(
		cfg.WorkingDir,
		time.Duration(cfg.CacheTTL)*time.Second,
		cfg.MaxCacheEntries,
	)

	// Create orchestrator
	orch, err := orchestrator.NewOrchestratorWithSharedResources(
		cfg,
		providerMgr,
		false, // cliMode
		filesystem,
		sess,
		nil,   // sessionStorageRef
		nil,   // domainBlockerRef
		false, // requireSandboxAuth (web mode doesn't use this flag)
	)
	if err != nil {
		filesystem.Close()
		return fmt.Errorf("failed to create orchestrator: %w", err)
	}

	mb.orchestrator = orch
	mb.initialized = true

	return nil
}

// ProcessUserMessage processes a user message through the orchestrator
func (mb *MessageBroker) ProcessUserMessage(ctx context.Context, message string, callback func(*WebMessage)) error {
	if !mb.initialized {
		return fmt.Errorf("session not initialized")
	}

	// Send user message to callback
	callback(&WebMessage{
		Type:    MessageTypeChat,
		Role:    "user",
		Content: message,
	})

	// Track messages for callback
	pendingMessages := make(chan *session.Message, 100)
	pendingToolCalls := make(chan ToolCallMsg, 100)
	pendingToolResults := make(chan ToolResultMsg, 100)
	pendingErrors := make(chan error, 10)

	// Create auth callback - for web mode, auto-approve for now
	authCallback := func(toolName string, params map[string]interface{}, reason string) (bool, error) {
		logger.Debug("Auth callback for tool %s: %s", toolName, reason)
		return true, nil // Auto-approve for web mode
	}

	// Create tool call callback - now sends compact interactions
	toolCallCallback := func(toolName, toolID string, parameters map[string]interface{}) error {
		// Send compact tool interaction message
		callback(
			&WebMessage{
				Type:       MessageTypeToolInteraction,
				ToolName:   toolName,
				ToolID:     toolID,
				Parameters: parameters,
				Status:     "calling",
				Compact:    true,
			})
		return nil
	}

	// Create tool result callback - updates existing compact interaction
	toolResultCallback := func(toolName, toolID, result, errorMsg string) error {
		callback(
			&WebMessage{
				Type:    MessageTypeToolInteraction,
				ToolID:  toolID,
				Result:  result,
				Error:   errorMsg,
				Status:  "completed",
				Compact: true,
			})
		return nil
	}

	// Create progress callback - filter out tool call status messages
	progressCallback := func(msg progress.Update) error {
		// Skip tool calling status messages for web UI
		if strings.Contains(msg.Message, "Calling tool:") {
			return nil
		}
		// Skip "Thinking..." messages
		if msg.Message == "Thinking..." {
			return nil
		}
		// Only send non-ephemeral important messages
		if !msg.Ephemeral && msg.Message != "" {
			logger.Debug("Progress: %s", msg.Message)
			callback(&WebMessage{
				Type:    MessageTypeSystem,
				Content: msg.Message,
			})
		}
		return nil
	}

	// Start goroutine to send responses to callback
	// Note: Tool calls and results are now handled directly via callbacks
	// This goroutine only handles assistant messages and errors
	done := make(chan struct{})
	go func() {
		for {
			select {
			case msg := <-pendingMessages:
				if msg.Role == "assistant" {
					callback(&WebMessage{
						Type:    MessageTypeChat,
						Role:    "assistant",
						Content: msg.Content,
					})
				}
			case tc := <-pendingToolCalls:
				// Tool calls are now handled directly via callback
				_ = tc // Avoid unused variable error
				continue
			case tr := <-pendingToolResults:
				// Tool results are now handled directly via callback
				_ = tr // Avoid unused variable error
				continue
			case err := <-pendingErrors:
				callback(&WebMessage{
					Type:    MessageTypeError,
					Content: err.Error(),
				})
			case <-done:
				return
			}
		}
	}()

	// Process through orchestrator
	err := mb.orchestrator.ProcessPromptWithVerification(
		ctx,
		message,
		progressCallback,
		nil, // contextCallback
		authCallback,
		toolCallCallback,
		toolResultCallback,
		nil, // openRouterUsageCallback
	)

	// Signal completion
	close(done)

	// Wait for the response goroutine to finish
	// This prevents sending to closed channels
	select {
	case <-done:
	case <-time.After(consts.Timeout5Seconds):
		logger.Warn("Timeout waiting for response goroutine to finish")
	}

	if err != nil {
		pendingErrors <- err
		return err
	}

	return nil
}

// GetSession returns the current session
func (mb *MessageBroker) GetSession() *session.Session {
	return mb.session
}

// GetOrchestrator returns the current orchestrator
func (mb *MessageBroker) GetOrchestrator() *orchestrator.Orchestrator {
	return mb.orchestrator
}

// GetProviderManager returns the provider manager
func (mb *MessageBroker) GetProviderManager() *provider.Manager {
	return mb.providerMgr
}

// Stop stops the current session operations
func (mb *MessageBroker) Stop() error {
	if mb.orchestrator != nil {
		mb.orchestrator.Stop()
	}
	return nil
}

// GetConfig returns the config
func (mb *MessageBroker) GetConfig() *config.Config {
	return mb.cfg
}

// Close cleans up resources
func (mb *MessageBroker) Close() error {
	if mb.orchestrator != nil {
		return mb.orchestrator.Close()
	}
	return nil
}

// ToolCallMsg represents a tool call message
type ToolCallMsg struct {
	Name   string
	ID     string
	Params map[string]interface{}
}

// ToolResultMsg represents a tool result message
type ToolResultMsg struct {
	ID     string
	Result interface{}
	Error  string
}
