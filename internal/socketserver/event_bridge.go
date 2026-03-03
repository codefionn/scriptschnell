package socketserver

import (
	"sync"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// EventBridge connects the actor event bus to the socket server hub,
// allowing actor events to flow to connected frontend clients.
type EventBridge struct {
	hub            *Hub
	mu             sync.RWMutex
	sessionClients map[string][]*Client // sessionID -> clients subscribed to that session
	started        bool
}

// NewEventBridge creates a new event bridge for the given hub
func NewEventBridge(hub *Hub) *EventBridge {
	return &EventBridge{
		hub:            hub,
		sessionClients: make(map[string][]*Client),
	}
}

// Start begins listening for actor events and forwarding them to socket clients
func (eb *EventBridge) Start() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.started {
		return
	}
	eb.started = true

	// Subscribe to all actor events
	actor.SystemEventBus.SubscribeAll(eb.handleEvent)

	logger.Info("EventBridge: started, subscribed to actor events")
}

// Stop stops the event bridge
func (eb *EventBridge) Stop() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.started {
		return
	}
	eb.started = false

	logger.Info("EventBridge: stopped")
}

// RegisterSessionClient registers a client as interested in events for a specific session
func (eb *EventBridge) RegisterSessionClient(sessionID string, client *Client) {
	if sessionID == "" {
		return
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Check if client is already registered for this session
	clients := eb.sessionClients[sessionID]
	for _, c := range clients {
		if c.ID == client.ID {
			return // Already registered
		}
	}

	eb.sessionClients[sessionID] = append(clients, client)
	logger.Debug("EventBridge: registered client %s for session %s", client.ID, sessionID)
}

// UnregisterSessionClient removes a client's interest in a session
func (eb *EventBridge) UnregisterSessionClient(sessionID string, client *Client) {
	if sessionID == "" {
		return
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	clients := eb.sessionClients[sessionID]
	for i, c := range clients {
		if c.ID == client.ID {
			eb.sessionClients[sessionID] = append(clients[:i], clients[i+1:]...)
			logger.Debug("EventBridge: unregistered client %s from session %s", client.ID, sessionID)
			break
		}
	}
}

// UnregisterClient removes a client from all session subscriptions
func (eb *EventBridge) UnregisterClient(client *Client) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for sessionID, clients := range eb.sessionClients {
		for i, c := range clients {
			if c.ID == client.ID {
				eb.sessionClients[sessionID] = append(clients[:i], clients[i+1:]...)
				break
			}
		}
	}
}

// handleEvent processes actor events and forwards them to appropriate clients
func (eb *EventBridge) handleEvent(event actor.Event) {
	eb.mu.RLock()
	started := eb.started
	eb.mu.RUnlock()

	if !started {
		logger.Debug("EventBridge: not started, dropping event type=%s", event.Type)
		return
	}

	logger.Debug("EventBridge: received event type=%s session=%s source=%s", event.Type, event.SessionID, event.Source)

	// Convert actor event to BaseMessage
	msg := eb.convertEventToMessage(event)
	if msg == nil {
		return
	}

	// Determine which clients should receive this event
	var targetClients []*Client

	if event.SessionID != "" {
		// Event is for a specific session - send to clients subscribed to that session
		eb.mu.RLock()
		sessionClients := make([]*Client, len(eb.sessionClients[event.SessionID]))
		copy(sessionClients, eb.sessionClients[event.SessionID])
		registeredCount := len(eb.sessionClients[event.SessionID])
		eb.mu.RUnlock()

		logger.Debug("EventBridge: session=%s has %d registered clients", event.SessionID, registeredCount)
		targetClients = sessionClients
	} else {
		// Event is global - broadcast to all connected clients
		logger.Debug("EventBridge: broadcasting global event")
		eb.hub.broadcast <- msg
		return
	}

	// Send to targeted clients
	sentCount := 0
	for _, client := range targetClients {
		// Check if client is still connected and authenticated
		if client.Authenticated() {
			select {
			case client.send <- msg:
				sentCount++
				logger.Debug("EventBridge: sent event to client %s", client.ID)
			default:
				// Client's send buffer is full, log warning
				logger.Warn("EventBridge: client %s send buffer full, dropping event", client.ID)
			}
		} else {
			logger.Debug("EventBridge: client %s not authenticated, skipping", client.ID)
		}
	}
	logger.Debug("EventBridge: sent event to %d/%d clients", sentCount, len(targetClients))
}

// convertEventToMessage converts an actor event to a BaseMessage for socket transmission
func (eb *EventBridge) convertEventToMessage(event actor.Event) *BaseMessage {
	switch event.Type {
	case actor.EventTypeProgress:
		return eb.convertProgressEvent(event)
	case actor.EventTypeMessage:
		return eb.convertMessageEvent(event)
	case actor.EventTypeToolCall:
		return eb.convertToolCallEvent(event)
	case actor.EventTypeToolResult:
		return eb.convertToolResultEvent(event)
	case actor.EventTypeAuthorization:
		return eb.convertAuthorizationEvent(event)
	case actor.EventTypeQuestion:
		return eb.convertQuestionEvent(event)
	case actor.EventTypeError:
		return eb.convertErrorEvent(event)
	case actor.EventTypeStatus:
		return eb.convertStatusEvent(event)
	default:
		logger.Debug("EventBridge: unknown event type %s", event.Type)
		return nil
	}
}

func (eb *EventBridge) convertProgressEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Ensure session_id is set
	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}

	return NewMessage(MessageTypeProgress, data)
}

func (eb *EventBridge) convertMessageEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Ensure session_id is set
	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}

	return NewMessage(MessageTypeChatMessage, data)
}

func (eb *EventBridge) convertToolCallEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Ensure session_id is set
	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}

	return NewMessage(MessageTypeToolCall, data)
}

func (eb *EventBridge) convertToolResultEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Ensure session_id is set
	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}

	return NewMessage(MessageTypeToolResult, data)
}

func (eb *EventBridge) convertAuthorizationEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Ensure session_id is set
	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}

	return NewMessage(MessageTypeAuthorizationRequest, data)
}

func (eb *EventBridge) convertQuestionEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Ensure session_id is set
	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}

	return NewMessage(MessageTypeQuestionRequest, data)
}

func (eb *EventBridge) convertErrorEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	code, _ := data["code"].(string)
	message, _ := data["message"].(string)
	details, _ := data["details"].(string)

	return NewError("", code, message, details)
}

func (eb *EventBridge) convertStatusEvent(event actor.Event) *BaseMessage {
	data := event.Data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Status events use progress message type with specific flag
	data["is_status"] = true

	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}

	return NewMessage(MessageTypeProgress, data)
}
