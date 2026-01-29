package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 8192
)

// Client represents a WebSocket client
type Client struct {
	ID              string
	hub             *Hub
	conn            *websocket.Conn
	send            chan *WebMessage
	broker          *MessageBroker
	cfg             *config.Config
	providerMgr     *provider.Manager
	secretsPassword string
	debug           bool
}

// NewClient creates a new WebSocket client
func NewClient(hub *Hub, conn *websocket.Conn, broker *MessageBroker, cfg *config.Config, providerMgr *provider.Manager, secretsPassword string, debug bool) *Client {
	id, _ := generateClientID()

	client := &Client{
		ID:              id,
		hub:             hub,
		conn:            conn,
		send:            make(chan *WebMessage, 256),
		broker:          broker,
		cfg:             cfg,
		providerMgr:     providerMgr,
		secretsPassword: secretsPassword,
		debug:           debug,
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
			if err := c.broker.InitializeSession(c.cfg, c.providerMgr, c.secretsPassword); err != nil {
				return fmt.Errorf("failed to initialize session: %w", err)
			}

			ctx := context.Background()
			if err := c.broker.ProcessUserMessage(ctx, msg.Content, c.sendResponse); err != nil {
				c.sendResponse(&WebMessage{
					Type:    MessageTypeError,
					Content: err.Error(),
				})
				return err
			}
		}

	case MessageTypeClear:
		// Stop current operations and clear session
		if c.broker != nil {
			// Stop any ongoing operations
			if err := c.broker.Stop(); err != nil {
				logger.Error("Failed to stop broker: %v", err)
			}
			
			// Clear the session
			if c.broker.GetSession() != nil {
				c.broker.GetSession().Clear()
				c.sendResponse(&WebMessage{
					Type:    MessageTypeSystem,
					Content: "Session cleared",
				})
			}
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
