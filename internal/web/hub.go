package web

import (
	"sync"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan *WebMessage
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	quit       chan struct{}
}

// NewHub creates a new hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan *WebMessage, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		quit:       make(chan struct{}),
	}
}

// Run starts the hub
func (h *Hub) Run() {
	logger.Info("WebSocket hub started")
	defer logger.Info("WebSocket hub stopped")

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			logger.Debug("Client registered: %s", client.ID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			logger.Debug("Client unregistered: %s", client.ID)

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Failed to send, remove client
					delete(h.clients, client)
					close(client.send)
				}
			}
			h.mu.RUnlock()

		case <-h.quit:
			return
		}
	}
}

// Stop stops the hub
func (h *Hub) Stop() {
	close(h.quit)
}

// Register registers a new client
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister unregisters a client
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// Broadcast broadcasts a message to all clients
func (h *Hub) Broadcast(message *WebMessage) {
	select {
	case h.broadcast <- message:
	default:
		logger.Warn("Broadcast channel full, dropping message")
	}
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
