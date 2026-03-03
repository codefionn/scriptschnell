package socketserver

import (
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
)

func TestNewEventBridge(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	if bridge == nil {
		t.Fatal("Expected bridge to not be nil")
	}
	if bridge.hub != hub {
		t.Error("Expected bridge hub to match provided hub")
	}
	if bridge.sessionClients == nil {
		t.Error("Expected sessionClients map to be initialized")
	}
	if bridge.started {
		t.Error("Expected bridge to not be started initially")
	}
}

func TestEventBridge_StartStop(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	// Start the bridge
	bridge.Start()
	if !bridge.started {
		t.Error("Expected bridge to be started")
	}

	// Starting again should be a no-op
	bridge.Start()
	if !bridge.started {
		t.Error("Expected bridge to still be started")
	}

	// Stop the bridge
	bridge.Stop()
	if bridge.started {
		t.Error("Expected bridge to be stopped")
	}

	// Stopping again should be a no-op
	bridge.Stop()
	if bridge.started {
		t.Error("Expected bridge to still be stopped")
	}
}

func TestEventBridge_RegisterUnregisterSessionClient(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	client := &Client{ID: "test-client"}
	sessionID := "test-session"

	// Register client
	bridge.RegisterSessionClient(sessionID, client)

	bridge.mu.RLock()
	clients := bridge.sessionClients[sessionID]
	bridge.mu.RUnlock()

	if len(clients) != 1 {
		t.Errorf("Expected 1 client, got %d", len(clients))
	}
	if clients[0].ID != client.ID {
		t.Errorf("Expected client ID %s, got %s", client.ID, clients[0].ID)
	}

	// Register same client again (should be no-op)
	bridge.RegisterSessionClient(sessionID, client)
	bridge.mu.RLock()
	clients = bridge.sessionClients[sessionID]
	bridge.mu.RUnlock()

	if len(clients) != 1 {
		t.Errorf("Expected still 1 client after duplicate registration, got %d", len(clients))
	}

	// Unregister client
	bridge.UnregisterSessionClient(sessionID, client)
	bridge.mu.RLock()
	clients = bridge.sessionClients[sessionID]
	bridge.mu.RUnlock()

	if len(clients) != 0 {
		t.Errorf("Expected 0 clients after unregister, got %d", len(clients))
	}
}

func TestEventBridge_UnregisterClient(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	client1 := &Client{ID: "client-1"}
	client2 := &Client{ID: "client-2"}

	// Register clients to multiple sessions
	bridge.RegisterSessionClient("session-1", client1)
	bridge.RegisterSessionClient("session-2", client1)
	bridge.RegisterSessionClient("session-1", client2)

	// Unregister client1 from all sessions
	bridge.UnregisterClient(client1)

	bridge.mu.RLock()
	session1Clients := bridge.sessionClients["session-1"]
	session2Clients := bridge.sessionClients["session-2"]
	bridge.mu.RUnlock()

	if len(session1Clients) != 1 || session1Clients[0].ID != "client-2" {
		t.Errorf("Expected session-1 to have only client-2, got %v", session1Clients)
	}
	if len(session2Clients) != 0 {
		t.Errorf("Expected session-2 to have no clients, got %d", len(session2Clients))
	}
}

func TestEventBridge_ConvertEventToMessage(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	tests := []struct {
		name     string
		event    actor.Event
		wantType string
	}{
		{
			name: "progress event",
			event: actor.Event{
				Type:      actor.EventTypeProgress,
				Source:    "test",
				SessionID: "session-1",
				Data:      map[string]interface{}{"message": "test"},
			},
			wantType: MessageTypeProgress,
		},
		{
			name: "message event",
			event: actor.Event{
				Type:      actor.EventTypeMessage,
				Source:    "test",
				SessionID: "session-1",
				Data:      map[string]interface{}{"role": "user", "content": "hello"},
			},
			wantType: MessageTypeChatMessage,
		},
		{
			name: "tool call event",
			event: actor.Event{
				Type:      actor.EventTypeToolCall,
				Source:    "test",
				SessionID: "session-1",
				Data:      map[string]interface{}{"tool_id": "123", "tool_name": "test"},
			},
			wantType: MessageTypeToolCall,
		},
		{
			name: "tool result event",
			event: actor.Event{
				Type:      actor.EventTypeToolResult,
				Source:    "test",
				SessionID: "session-1",
				Data:      map[string]interface{}{"tool_id": "123", "result": "done"},
			},
			wantType: MessageTypeToolResult,
		},
		{
			name: "authorization event",
			event: actor.Event{
				Type:      actor.EventTypeAuthorization,
				Source:    "test",
				SessionID: "session-1",
				Data:      map[string]interface{}{"auth_id": "auth-1"},
			},
			wantType: MessageTypeAuthorizationRequest,
		},
		{
			name: "question event",
			event: actor.Event{
				Type:      actor.EventTypeQuestion,
				Source:    "test",
				SessionID: "session-1",
				Data:      map[string]interface{}{"question_id": "q-1"},
			},
			wantType: MessageTypeQuestionRequest,
		},
		{
			name: "error event",
			event: actor.Event{
				Type:   actor.EventTypeError,
				Source: "test",
				Data:   map[string]interface{}{"code": "ERROR", "message": "test error"},
			},
			wantType: MessageTypeError,
		},
		{
			name: "status event",
			event: actor.Event{
				Type:      actor.EventTypeStatus,
				Source:    "test",
				SessionID: "session-1",
				Data:      map[string]interface{}{"status": "ready"},
			},
			wantType: MessageTypeProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := bridge.convertEventToMessage(tt.event)
			if msg == nil {
				t.Fatal("Expected message to not be nil")
			}
			if msg.Type != tt.wantType {
				t.Errorf("Expected message type %s, got %s", tt.wantType, msg.Type)
			}
		})
	}
}

func TestEventBridge_ConvertEventToMessage_UnknownType(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	event := actor.Event{
		Type:      actor.EventType("unknown_type"),
		Source:    "test",
		SessionID: "session-1",
		Data:      map[string]interface{}{},
	}

	msg := bridge.convertEventToMessage(event)
	if msg != nil {
		t.Error("Expected nil message for unknown event type")
	}
}

func TestEventBridge_HandleEvent_GlobalEvent(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	// Start the bridge
	bridge.Start()
	defer bridge.Stop()

	// Global events (no session ID) should be broadcast to all
	event := actor.Event{
		Type:   actor.EventTypeProgress,
		Source: "test",
		Data:   map[string]interface{}{"message": "global event"},
	}

	// This should not panic
	bridge.handleEvent(event)
}

func TestEventBridge_HandleEvent_NotStarted(t *testing.T) {
	hub := NewHub()
	bridge := NewEventBridge(hub)

	// Don't start the bridge
	event := actor.Event{
		Type:      actor.EventTypeProgress,
		Source:    "test",
		SessionID: "session-1",
		Data:      map[string]interface{}{"message": "test"},
	}

	// This should not panic or process the event
	bridge.handleEvent(event)
}

func TestEventBridge_SessionEventRouting(t *testing.T) {
	// Use a fresh event bus
	originalBus := actor.SystemEventBus
	actor.SystemEventBus = actor.NewEventBus(100)
	defer func() {
		actor.SystemEventBus.Close()
		actor.SystemEventBus = originalBus
	}()

	hub := NewHub()
	bridge := NewEventBridge(hub)

	// Create mock clients
	client1 := &Client{
		ID:            "client-1",
		send:          make(chan *BaseMessage, 10),
		authenticated: true,
	}
	client2 := &Client{
		ID:            "client-2",
		send:          make(chan *BaseMessage, 10),
		authenticated: true,
	}

	// Start the bridge
	bridge.Start()
	defer bridge.Stop()

	// Register clients
	bridge.RegisterSessionClient("session-1", client1)
	bridge.RegisterSessionClient("session-1", client2)

	// Publish an event to the actor event bus
	actor.PublishEvent(actor.EventTypeProgress, "test-actor", "session-1", map[string]interface{}{
		"message": "test progress",
	})

	// Wait for event to be processed
	time.Sleep(100 * time.Millisecond)

	// Both clients should receive the event
	select {
	case msg := <-client1.send:
		if msg.Type != MessageTypeProgress {
			t.Errorf("Expected progress message, got %s", msg.Type)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for client1 to receive message")
	}

	select {
	case msg := <-client2.send:
		if msg.Type != MessageTypeProgress {
			t.Errorf("Expected progress message, got %s", msg.Type)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for client2 to receive message")
	}
}

func TestEventBridge_SessionIsolation(t *testing.T) {
	// Use a fresh event bus
	originalBus := actor.SystemEventBus
	actor.SystemEventBus = actor.NewEventBus(100)
	defer func() {
		actor.SystemEventBus.Close()
		actor.SystemEventBus = originalBus
	}()

	hub := NewHub()
	bridge := NewEventBridge(hub)

	// Create mock clients for different sessions
	client1 := &Client{
		ID:            "client-1",
		send:          make(chan *BaseMessage, 10),
		authenticated: true,
	}
	client2 := &Client{
		ID:            "client-2",
		send:          make(chan *BaseMessage, 10),
		authenticated: true,
	}

	// Start the bridge
	bridge.Start()
	defer bridge.Stop()

	// Register clients to different sessions
	bridge.RegisterSessionClient("session-1", client1)
	bridge.RegisterSessionClient("session-2", client2)

	// Publish an event for session-1
	actor.PublishEvent(actor.EventTypeProgress, "test-actor", "session-1", map[string]interface{}{
		"message": "session-1 message",
	})

	// Wait for event to be processed
	time.Sleep(100 * time.Millisecond)

	// client1 should receive the event
	select {
	case <-client1.send:
		// Expected
	case <-time.After(200 * time.Millisecond):
		t.Error("client1 should have received the message")
	}

	// client2 should NOT receive the event
	select {
	case <-client2.send:
		t.Error("client2 should NOT have received the message for session-1")
	case <-time.After(200 * time.Millisecond):
		// Expected - no message received
	}
}

func TestEventBridge_NonAuthenticatedClient(t *testing.T) {
	// Use a fresh event bus
	originalBus := actor.SystemEventBus
	actor.SystemEventBus = actor.NewEventBus(100)
	defer func() {
		actor.SystemEventBus.Close()
		actor.SystemEventBus = originalBus
	}()

	hub := NewHub()
	bridge := NewEventBridge(hub)

	// Create non-authenticated client
	client := &Client{
		ID:            "client-1",
		send:          make(chan *BaseMessage, 10),
		authenticated: false, // Not authenticated
	}

	// Start the bridge
	bridge.Start()
	defer bridge.Stop()

	// Register client
	bridge.RegisterSessionClient("session-1", client)

	// Publish an event
	actor.PublishEvent(actor.EventTypeProgress, "test-actor", "session-1", map[string]interface{}{
		"message": "test",
	})

	// Wait for event to be processed
	time.Sleep(100 * time.Millisecond)

	// Client should not receive the event (not authenticated)
	select {
	case <-client.send:
		t.Error("Non-authenticated client should NOT receive messages")
	case <-time.After(200 * time.Millisecond):
		// Expected
	}
}
