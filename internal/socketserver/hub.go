package socketserver

import (
	"sync"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// Hub maintains the set of active clients and handles broadcasting
type Hub struct {
	clients map[*Client]bool
	mu      sync.RWMutex

	// Inbound messages from clients (not used in current design but kept for future broadcast needs)
	broadcast chan *BaseMessage

	// Register/unregister requests from clients
	register   chan *Client
	unregister chan *Client

	// Session registry for tracking which client owns which session
	sessions map[string]*Client // sessionID -> client
}

// NewHub creates a new hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan *BaseMessage, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		sessions:   make(map[string]*Client),
	}
}

// Run starts the hub's event loop
func (h *Hub) Run() {
	logger.Info("Socket hub started")
	for {
		select {
		case client := <-h.register:
			h.registerClient(client)

		case client := <-h.unregister:
			h.unregisterClient(client)

		case message := <-h.broadcast:
			h.broadcastMessage(message)
		}
	}
}

// registerClient adds a new client to the hub
func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clients[client] = true
	logger.Info("Socket client registered: %s (total: %d)", client.ID, len(h.clients))
}

// unregisterClient removes a client from the hub
func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		// Remove any session associations
		if client.SessionID != "" {
			delete(h.sessions, client.SessionID)
		}
		logger.Info("Socket client unregistered: %s (total: %d)", client.ID, len(h.clients))
	}
}

// broadcastMessage sends a message to all connected clients (for future use)
func (h *Hub) broadcastMessage(message *BaseMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			// Client's send buffer is full, likely dead connection
			logger.Warn("Failed to send message to client %s, closing connection", client.ID)
			client.Close()
		}
	}
}

// RegisterClient adds a client (called from client goroutine)
func (h *Hub) RegisterClient(client *Client) {
	h.register <- client
}

// UnregisterClient removes a client (called from client goroutine)
func (h *Hub) UnregisterClient(client *Client) {
	h.unregister <- client
}

// RegisterSession associates a session with a client
func (h *Hub) RegisterSession(sessionID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if session is already owned by another client
	if existingClient, ok := h.sessions[sessionID]; ok && existingClient != client {
		logger.Warn("Session %s already owned by client %s, reassigning to %s", sessionID, existingClient.ID, client.ID)
	}

	h.sessions[sessionID] = client
	logger.Info("Session %s registered to client %s", sessionID, client.ID)
}

// UnregisterSession removes a session association
func (h *Hub) UnregisterSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.sessions[sessionID]; ok {
		delete(h.sessions, sessionID)
		logger.Info("Session %s unregistered", sessionID)
	}
}

// GetSessionOwner returns the client that owns a session
func (h *Hub) GetSessionOwner(sessionID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	client, ok := h.sessions[sessionID]
	return client, ok
}

// GetClientCount returns the number of connected clients
func (h *Hub) GetClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients)
}

// GetSessionCount returns the number of active sessions
func (h *Hub) GetSessionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.sessions)
}

// ListSessions returns information about all active sessions
func (h *Hub) ListSessions() []SessionInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sessions := make([]SessionInfo, 0, len(h.sessions))
	for sessionID, client := range h.sessions {
		sessions = append(sessions, SessionInfo{
			SessionID:    sessionID,
			Workspace:    client.Workspace,
			MessageCount: len(client.messages),
			Status:       "active",
		})
	}

	return sessions
}

// Shutdown closes all client connections
func (h *Hub) Shutdown() {
	logger.Info("Shutting down hub, closing all connections")

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	// Close all clients
	for _, client := range clients {
		client.Close()
	}
}
