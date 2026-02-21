package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/securemem"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = consts.Timeout10Seconds

	// Time allowed to read the next pong message from the peer.
	pongWait = consts.Timeout60Seconds

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 8192
)

// Client represents a WebSocket client
type Client struct {
	ID                 string
	hub                *Hub
	conn               *websocket.Conn
	send               chan *WebMessage
	broker             *MessageBroker
	cfg                *config.Config
	providerMgr        *provider.Manager
	secretsPassword    *securemem.String
	debug              bool
	requireSandboxAuth bool
	cancelFunc         context.CancelFunc
}

// NewClient creates a new WebSocket client
func NewClient(hub *Hub, conn *websocket.Conn, broker *MessageBroker, cfg *config.Config, providerMgr *provider.Manager, secretsPassword *securemem.String, debug bool, requireSandboxAuth bool) *Client {
	id, _ := generateClientID()

	client := &Client{
		ID:                 id,
		hub:                hub,
		conn:               conn,
		send:               make(chan *WebMessage, 256),
		broker:             broker,
		cfg:                cfg,
		providerMgr:        providerMgr,
		secretsPassword:    secretsPassword,
		debug:              debug,
		requireSandboxAuth: requireSandboxAuth,
	}

	return client
}

// ReadPump pumps messages from the WebSocket connection to the hub
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("WebSocket read error: %v", err)
			}
			break
		}

		// Parse incoming message
		var msg WebMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Error("Failed to unmarshal message: %v", err)
			continue
		}

		// Log message if debug is enabled
		if c.debug {
			logger.Debug("WebSocket received: %s", string(message))
		}

		// Handle message
		if err := c.handleMessage(&msg); err != nil {
			logger.Error("Failed to handle message: %v", err)
		}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Marshal message
			data, err := json.Marshal(message)
			if err != nil {
				logger.Error("Failed to marshal message: %v", err)
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				logger.Error("Failed to write message: %v", err)
				return
			}

			// Log message if debug is enabled
			if c.debug {
				logger.Debug("WebSocket sent: %s", string(data))
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming messages from the client
func (c *Client) handleMessage(msg *WebMessage) error {
	switch msg.Type {
	case MessageTypeChat:
		if msg.Role == "user" {
			// Process user message through broker
			if err := c.broker.InitializeSession(c.cfg, c.providerMgr, c.secretsPassword, c.requireSandboxAuth); err != nil {
				return fmt.Errorf("failed to initialize session: %w", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			c.cancelFunc = cancel
			// Run in a goroutine so ReadPump stays free to receive stop messages
			go func() {
				defer func() {
					cancel()
					c.cancelFunc = nil
				}()
				if err := c.broker.ProcessUserMessage(ctx, msg.Content, c.sendResponse); err != nil {
					c.sendResponse(&WebMessage{
						Type:    MessageTypeError,
						Content: err.Error(),
					})
				}
			}()
		}

	case MessageTypeStop:
		// Stop current generation
		if c.cancelFunc != nil {
			c.cancelFunc()
			c.cancelFunc = nil
		}
		if c.broker != nil {
			if err := c.broker.Stop(); err != nil {
				logger.Error("Failed to stop broker: %v", err)
			}
		}
		c.sendResponse(&WebMessage{
			Type:    MessageTypeSystem,
			Content: "Generation stopped",
		})

	case MessageTypeClear:
		// Stop current operations and reset session
		if c.broker != nil {
			// Stop any ongoing operations
			if err := c.broker.Stop(); err != nil {
				logger.Error("Failed to stop broker: %v", err)
			}

			// Reset the session so next message creates a fresh one
			c.broker.ResetSession()
			c.sendResponse(&WebMessage{
				Type:    MessageTypeSystem,
				Content: "Session cleared",
			})
		}

	case MessageTypeGetConfig:
		// Send config info
		c.sendResponse(&WebMessage{
			Type: MessageTypeConfig,
			Data: map[string]interface{}{
				"working_dir": c.cfg.WorkingDir,
				"model":       c.providerMgr.GetOrchestrationModel(),
			},
		})

	case MessageTypeGetProviders:
		// Send providers list
		providers := c.getProviders()
		c.sendResponse(&WebMessage{
			Type: MessageTypeProviders,
			Data: map[string]interface{}{
				"providers": providers,
			},
		})

	case MessageTypeAddProvider:
		// Add a new provider
		if err := c.addProvider(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeUpdateProvider:
		// Update an existing provider
		if err := c.updateProvider(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeGetModels:
		// Send models list and current selections
		models := c.getModels()
		currentModels := c.getCurrentModels()
		c.sendResponse(&WebMessage{
			Type: MessageTypeModels,
			Data: map[string]interface{}{
				"models":         models,
				"current_models": currentModels,
			},
		})

	case MessageTypeSetModel:
		// Set the model for a specific role
		if err := c.setModel(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeGetSearchConfig:
		// Send search config
		var apiKey string
		switch c.cfg.Search.Provider {
		case "exa":
			apiKey = c.cfg.Search.Exa.APIKey
		case "google_pse":
			apiKey = c.cfg.Search.GooglePSE.APIKey
		case "perplexity":
			apiKey = c.cfg.Search.Perplexity.APIKey
		}
		c.sendResponse(&WebMessage{
			Type: MessageTypeSearchConfig,
			Data: map[string]interface{}{
				"provider": c.cfg.Search.Provider,
				"api_key":  apiKey,
			},
		})

	case MessageTypeSetSearchConfig:
		// Set search config
		if err := c.setSearchConfig(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeSetPassword:
		// Set encryption password
		if err := c.setPassword(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeGetMCPServers:
		// Send MCP servers list
		servers := c.getMCPServers()
		c.sendResponse(&WebMessage{
			Type: MessageTypeMCPServers,
			Data: map[string]interface{}{
				"servers": servers,
			},
		})

	case MessageTypeAddMCPServer:
		// Add a new MCP server
		if err := c.addMCPServer(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeToggleMCPServer:
		// Toggle MCP server enabled/disabled
		if err := c.toggleMCPServer(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeDeleteMCPServer:
		// Delete an MCP server
		if err := c.deleteMCPServer(msg); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeAuthorizationAck:
		// Handle authorization ack from web client (confirms dialog was displayed)
		if msg.AuthID == "" {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: "auth_id is required for authorization ack",
			})
			return nil
		}
		if err := c.broker.HandleAuthorizationAck(msg.AuthID); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeAuthorizationResponse:
		// Handle authorization response from web client
		if msg.AuthID == "" {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: "auth_id is required for authorization response",
			})
			return nil
		}
		if msg.Approved == nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: "approved field is required for authorization response",
			})
			return nil
		}
		if err := c.broker.HandleAuthorizationResponse(msg.AuthID, *msg.Approved); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeQuestionResponse:
		// Handle question response from web client
		if msg.QuestionID == "" {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: "question_id is required for question response",
			})
			return nil
		}
		// For single question mode, answer is required
		// For multi question mode, answers map is required
		if msg.Answer == "" && len(msg.AnswersMap) == 0 {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: "answer or answers field is required for question response",
			})
			return nil
		}
		if err := c.broker.HandleQuestionResponse(msg.QuestionID, msg.Answer, msg.AnswersMap); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		}

	case MessageTypeGetSessions:
		sessions, err := c.broker.ListSessions(c.cfg.WorkingDir)
		if err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		} else {
			c.sendResponse(&WebMessage{
				Type: MessageTypeSessions,
				Data: map[string]interface{}{
					"sessions": sessions,
				},
			})
		}

	case MessageTypeSaveSession:
		name, _ := msg.Data["name"].(string)
		if err := c.broker.SaveCurrentSession(name); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		} else {
			c.sendResponse(&WebMessage{
				Type:    MessageTypeSystem,
				Content: "Session saved",
			})
		}

	case MessageTypeLoadSession:
		sessionID, ok := msg.Data["session_id"].(string)
		if !ok || sessionID == "" {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: "session_id is required",
			})
			return nil
		}
		msgs, err := c.broker.LoadSessionByID(c.cfg, c.providerMgr, c.secretsPassword, c.requireSandboxAuth, sessionID)
		if err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		} else {
			// Convert session messages to a serialisable slice
			history := make([]map[string]interface{}, len(msgs))
			for i, m := range msgs {
				history[i] = map[string]interface{}{
					"role":    m.Role,
					"content": m.Content,
				}
				if m.ToolName != "" {
					history[i]["tool_name"] = m.ToolName
				}
				if m.ToolID != "" {
					history[i]["tool_id"] = m.ToolID
				}
			}
			c.sendResponse(&WebMessage{
				Type: MessageTypeSessionLoaded,
				Data: map[string]interface{}{
					"messages": history,
				},
			})
		}

	case MessageTypeDeleteSession:
		sessionID, ok := msg.Data["session_id"].(string)
		if !ok || sessionID == "" {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: "session_id is required",
			})
			return nil
		}
		if err := c.broker.DeleteSessionByID(c.cfg.WorkingDir, sessionID); err != nil {
			c.sendResponse(&WebMessage{
				Type:  MessageTypeError,
				Error: err.Error(),
			})
		} else {
			c.sendResponse(&WebMessage{
				Type:    MessageTypeSystem,
				Content: "Session deleted",
			})
		}

	default:
		logger.Warn("Unknown message type: %s", msg.Type)
	}

	return nil
}

// sendResponse sends a response message to the client
func (c *Client) sendResponse(msg *WebMessage) {
	select {
	case c.send <- msg:
	default:
		logger.Warn("Client send channel full, dropping message")
	}
}

// generateClientID generates a random client ID
func generateClientID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GetProviderManager returns the provider manager
func (c *Client) GetProviderManager() *provider.Manager {
	return c.providerMgr
}

// GetConfig returns the config
func (c *Client) GetConfig() *config.Config {
	return c.cfg
}

// GetBroker returns the message broker
func (c *Client) GetBroker() *MessageBroker {
	return c.broker
}

// getProviders returns a list of all configured providers
func (c *Client) getProviders() []ProviderInfo {
	providers := c.providerMgr.ListProviders()
	result := make([]ProviderInfo, len(providers))

	for i, p := range providers {
		apiKeyStatus := "not configured"
		if p.APIKey != "" {
			apiKeyStatus = "configured"
		}

		var rateLimit *RateLimitInfo
		if p.RateLimit != nil {
			rateLimit = &RateLimitInfo{
				RequestsPerMinute: p.RateLimit.RequestsPerMinute,
				MinIntervalMillis: p.RateLimit.MinIntervalMillis,
				TokensPerMinute:   p.RateLimit.TokensPerMinute,
			}
		}

		result[i] = ProviderInfo{
			Name:        p.Name,
			DisplayName: friendlyProviderName(p.Name),
			APIKey:      apiKeyStatus,
			BaseURL:     p.BaseURL,
			ModelCount:  len(p.Models),
			RateLimit:   rateLimit,
		}
	}

	return result
}

// addProvider adds a new provider
func (c *Client) addProvider(msg *WebMessage) error {
	providerType, ok := msg.Data["provider_type"].(string)
	if !ok || providerType == "" {
		return fmt.Errorf("provider_type is required")
	}

	apiKey, _ := msg.Data["api_key"].(string)

	ctx := context.Background()

	if providerType == "openai-compatible" {
		baseURL, _ := msg.Data["base_url"].(string)
		if baseURL == "" {
			return fmt.Errorf("base_url is required for openai-compatible providers")
		}

		if err := c.providerMgr.AddProviderWithAPIListingAndBaseURL(ctx, providerType, apiKey, baseURL); err != nil {
			return fmt.Errorf("failed to add provider: %w", err)
		}
	} else {
		if err := c.providerMgr.AddProviderWithAPIListing(ctx, providerType, apiKey); err != nil {
			return fmt.Errorf("failed to add provider: %w", err)
		}
	}

	// Auto-configure default model if this is the first provider
	if err := c.providerMgr.ConfigureDefaultModelForProvider(providerType); err != nil {
		logger.Warn("Failed to configure default model: %v", err)
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: fmt.Sprintf("Successfully added %s provider", friendlyProviderName(providerType)),
	})

	return nil
}

// updateProvider updates an existing provider
func (c *Client) updateProvider(msg *WebMessage) error {
	providerName, ok := msg.Data["provider_name"].(string)
	if !ok || providerName == "" {
		return fmt.Errorf("provider_name is required")
	}

	apiKey, _ := msg.Data["api_key"].(string)
	baseURL, _ := msg.Data["base_url"].(string)

	// Update credentials if provided
	if apiKey != "" || baseURL != "" {
		if err := c.providerMgr.UpdateProviderConnection(providerName, apiKey, baseURL); err != nil {
			return fmt.Errorf("failed to update provider connection: %w", err)
		}
	}

	// Update rate limit if provided
	rpm, _ := msg.Data["requests_per_minute"].(float64)
	interval, _ := msg.Data["min_interval_ms"].(float64)
	tokens, _ := msg.Data["tokens_per_minute"].(float64)

	if rpm > 0 || interval > 0 || tokens > 0 {
		cfg := &provider.RateLimitConfig{
			RequestsPerMinute: int(rpm),
			MinIntervalMillis: int(interval),
			TokensPerMinute:   int(tokens),
		}
		if err := c.providerMgr.UpdateProviderRateLimit(providerName, cfg); err != nil {
			return fmt.Errorf("failed to update rate limit: %w", err)
		}
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: fmt.Sprintf("Successfully updated %s provider", friendlyProviderName(providerName)),
	})

	return nil
}

// getModels returns a list of all available models
func (c *Client) getModels() []ModelInfo {
	models := c.providerMgr.ListAllModels()
	result := make([]ModelInfo, len(models))

	for i, m := range models {
		result[i] = ModelInfo{
			ID:          m.ID,
			Name:        m.Name,
			Provider:    m.Provider,
			Description: m.Description,
		}
	}

	return result
}

// getCurrentModels returns the currently selected models for each role
func (c *Client) getCurrentModels() map[string]string {
	return map[string]string{
		"orchestration": c.providerMgr.GetOrchestrationModel(),
		"summarization": c.providerMgr.GetSummarizeModel(),
		"planning":      c.providerMgr.GetPlanningModel(),
		"safety":        c.providerMgr.GetSafetyModel(),
	}
}

// setModel sets the model for a specific role
func (c *Client) setModel(msg *WebMessage) error {
	modelRole, ok := msg.Data["role"].(string)
	if !ok || modelRole == "" {
		return fmt.Errorf("role is required (orchestration or summarize)")
	}

	modelID, ok := msg.Data["model_id"].(string)
	if !ok || modelID == "" {
		return fmt.Errorf("model_id is required")
	}

	switch modelRole {
	case "orchestration":
		if err := c.providerMgr.SetOrchestrationModel(modelID); err != nil {
			return fmt.Errorf("failed to set orchestration model: %w", err)
		}
	case "summarize":
		if err := c.providerMgr.SetSummarizeModel(modelID); err != nil {
			return fmt.Errorf("failed to set summarize model: %w", err)
		}
	case "safety":
		if err := c.providerMgr.SetSafetyModel(modelID); err != nil {
			return fmt.Errorf("failed to set safety model: %w", err)
		}
	default:
		return fmt.Errorf("invalid role: %s", modelRole)
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: fmt.Sprintf("Successfully set %s model to %s", modelRole, modelID),
	})

	return nil
}

// setSearchConfig sets the search configuration
func (c *Client) setSearchConfig(msg *WebMessage) error {
	searchProvider, ok := msg.Data["provider"].(string)
	if !ok {
		return fmt.Errorf("provider is required")
	}

	apiKey, _ := msg.Data["api_key"].(string)

	c.cfg.Search.Provider = searchProvider

	// Set API key based on provider type
	switch searchProvider {
	case "exa":
		c.cfg.Search.Exa.APIKey = apiKey
	case "google_pse":
		c.cfg.Search.GooglePSE.APIKey = apiKey
	case "perplexity":
		c.cfg.Search.Perplexity.APIKey = apiKey
	}

	// Save config
	if err := c.cfg.Save(config.GetConfigPath()); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: "Successfully updated search configuration",
	})

	return nil
}

// setPassword sets the encryption password
func (c *Client) setPassword(msg *WebMessage) error {
	password, ok := msg.Data["password"].(string)
	if !ok {
		return fmt.Errorf("password is required")
	}

	// Update secrets password - destroy old and create new
	if c.secretsPassword != nil {
		c.secretsPassword.Destroy()
	}
	c.secretsPassword = securemem.NewString(password)

	// Update verifier in config
	c.cfg.Secrets.Verifier = c.generatePasswordVerifier(password)
	c.cfg.Secrets.PasswordSet = password != ""

	// Save config
	if err := c.cfg.Save(config.GetConfigPath()); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: "Successfully updated encryption password",
	})

	return nil
}

// generatePasswordVerifier generates a simple verifier for the password
// This is a simplified version - in production, you might want to use proper hash
func (c *Client) generatePasswordVerifier(password string) string {
	if password == "" {
		return ""
	}
	// For now, just use a simple hash (TODO: use proper password hashing)
	return password // This should be replaced with proper hashing
}

// getMCPServers returns a list of all MCP servers
func (c *Client) getMCPServers() []MCPServerInfo {
	if c.cfg.MCP.Servers == nil {
		return []MCPServerInfo{}
	}

	result := make([]MCPServerInfo, 0, len(c.cfg.MCP.Servers))

	for name, server := range c.cfg.MCP.Servers {
		configMap := make(map[string]interface{})

		switch server.Type {
		case "openapi":
			if server.OpenAPI != nil {
				configMap["spec_path"] = server.OpenAPI.SpecPath
				configMap["url"] = server.OpenAPI.URL
			}
		case "command":
			if server.Command != nil {
				configMap["exec"] = server.Command.Exec
				configMap["working_dir"] = server.Command.WorkingDir
				configMap["timeout_seconds"] = server.Command.TimeoutSeconds
			}
		case "openai":
			if server.OpenAI != nil {
				configMap["model"] = server.OpenAI.Model
				configMap["base_url"] = server.OpenAI.BaseURL
			}
		}

		result = append(result, MCPServerInfo{
			Name:        name,
			Type:        server.Type,
			Description: server.Description,
			Disabled:    server.Disabled,
			Config:      configMap,
		})
	}

	return result
}

// addMCPServer adds a new MCP server
func (c *Client) addMCPServer(msg *WebMessage) error {
	serverName, ok := msg.Data["name"].(string)
	if !ok || serverName == "" {
		return fmt.Errorf("name is required")
	}

	serverType, ok := msg.Data["type"].(string)
	if !ok || serverType == "" {
		return fmt.Errorf("type is required")
	}

	if c.cfg.MCP.Servers == nil {
		c.cfg.MCP.Servers = make(map[string]*config.MCPServerConfig)
	}

	// Check if server already exists
	if _, exists := c.cfg.MCP.Servers[serverName]; exists {
		return fmt.Errorf("server with name '%s' already exists", serverName)
	}

	serverConfig := &config.MCPServerConfig{
		Type:        serverType,
		Description: msg.Data["description"].(string),
	}

	switch serverType {
	case "openapi":
		specPath, _ := msg.Data["spec_path"].(string)
		url, _ := msg.Data["url"].(string)
		serverConfig.OpenAPI = &config.MCPOpenAPIConfig{
			SpecPath: specPath,
			URL:      url,
		}
	case "command":
		execStr, _ := msg.Data["exec"].(string)
		execParts := strings.Fields(execStr)
		workingDir, _ := msg.Data["working_dir"].(string)
		timeout, _ := msg.Data["timeout_seconds"].(float64)
		serverConfig.Command = &config.MCPCommandConfig{
			Exec:           execParts,
			WorkingDir:     workingDir,
			TimeoutSeconds: int(timeout),
		}
	case "openai":
		model, _ := msg.Data["model"].(string)
		baseURL, _ := msg.Data["base_url"].(string)
		serverConfig.OpenAI = &config.MCPOpenAIConfig{
			Model:   model,
			BaseURL: baseURL,
		}
	}

	c.cfg.MCP.Servers[serverName] = serverConfig

	// Save config
	if err := c.cfg.Save(config.GetConfigPath()); err != nil {
		delete(c.cfg.MCP.Servers, serverName)
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: fmt.Sprintf("Successfully added MCP server '%s'", serverName),
	})

	return nil
}

// toggleMCPServer toggles an MCP server enabled/disabled
func (c *Client) toggleMCPServer(msg *WebMessage) error {
	serverName, ok := msg.Data["name"].(string)
	if !ok || serverName == "" {
		return fmt.Errorf("name is required")
	}

	if c.cfg.MCP.Servers == nil {
		return fmt.Errorf("no MCP servers configured")
	}

	server, exists := c.cfg.MCP.Servers[serverName]
	if !exists {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	server.Disabled = !server.Disabled

	// Save config
	if err := c.cfg.Save(config.GetConfigPath()); err != nil {
		server.Disabled = !server.Disabled // revert
		return fmt.Errorf("failed to save config: %w", err)
	}

	status := "enabled"
	if server.Disabled {
		status = "disabled"
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: fmt.Sprintf("Successfully %s MCP server '%s'", status, serverName),
	})

	return nil
}

// deleteMCPServer deletes an MCP server
func (c *Client) deleteMCPServer(msg *WebMessage) error {
	serverName, ok := msg.Data["name"].(string)
	if !ok || serverName == "" {
		return fmt.Errorf("name is required")
	}

	if c.cfg.MCP.Servers == nil {
		return fmt.Errorf("no MCP servers configured")
	}

	if _, exists := c.cfg.MCP.Servers[serverName]; !exists {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	delete(c.cfg.MCP.Servers, serverName)

	// Save config
	if err := c.cfg.Save(config.GetConfigPath()); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Send success response
	c.sendResponse(&WebMessage{
		Type:    MessageTypeSystem,
		Content: fmt.Sprintf("Successfully deleted MCP server '%s'", serverName),
	})

	return nil
}

// friendlyProviderName returns a human-readable provider name
func friendlyProviderName(name string) string {
	switch name {
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "google":
		return "Google"
	case "openrouter":
		return "OpenRouter"
	case "mistral":
		return "Mistral"
	case "cerebras":
		return "Cerebras"
	case "groq":
		return "Groq"
	case "kimi":
		return "Kimi"
	case "ollama":
		return "Ollama"
	case "openai-compatible":
		return "OpenAI-Compatible"
	default:
		if name == "" {
			return "Provider"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}
