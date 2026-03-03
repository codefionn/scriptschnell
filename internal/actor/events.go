package actor

import (
	"context"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// EventType represents the type of event being published
type EventType string

const (
	// EventTypeProgress indicates a progress update event
	EventTypeProgress EventType = "progress"
	// EventTypeMessage indicates a chat message event
	EventTypeMessage EventType = "message"
	// EventTypeToolCall indicates a tool execution event
	EventTypeToolCall EventType = "tool_call"
	// EventTypeToolResult indicates a tool execution result event
	EventTypeToolResult EventType = "tool_result"
	// EventTypeAuthorization indicates an authorization request event
	EventTypeAuthorization EventType = "authorization"
	// EventTypeQuestion indicates a question request event
	EventTypeQuestion EventType = "question"
	// EventTypeStatus indicates a status update event
	EventTypeStatus EventType = "status"
	// EventTypeError indicates an error event
	EventTypeError EventType = "error"
	// EventTypeSession indicates a session-related event
	EventTypeSession EventType = "session"
)

// Event represents an event that can be published by actors and consumed by frontends
type Event struct {
	// Type is the event type
	Type EventType `json:"type"`
	// Source is the actor ID that published the event
	Source string `json:"source"`
	// SessionID is the session this event belongs to (if applicable)
	SessionID string `json:"session_id,omitempty"`
	// Data contains the event payload (type-specific)
	Data map[string]interface{} `json:"data"`
	// Timestamp is when the event was created
	Timestamp time.Time `json:"timestamp"`
}

// EventHandler is a function that handles events
type EventHandler func(event Event)

// EventBus is a publish-subscribe system for actor events
// It allows actors to publish events without knowing about subscribers,
// and frontends to subscribe to events without knowing about publishers.
type EventBus struct {
	subscribers map[EventType][]EventHandler
	allHandlers []EventHandler // handlers that receive all events
	mu          sync.RWMutex
	bufferSize  int
	eventChan   chan Event
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewEventBus creates a new event bus with the specified buffer size
func NewEventBus(bufferSize int) *EventBus {
	ctx, cancel := context.WithCancel(context.Background())
	eb := &EventBus{
		subscribers: make(map[EventType][]EventHandler),
		allHandlers: make([]EventHandler, 0),
		bufferSize:  bufferSize,
		eventChan:   make(chan Event, bufferSize),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start the event dispatcher
	go eb.dispatcher()

	return eb
}

// Subscribe registers a handler for a specific event type
func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.subscribers[eventType] = append(eb.subscribers[eventType], handler)
	logger.Debug("EventBus: subscribed handler for event type %s", eventType)
}

// SubscribeAll registers a handler that receives all events
func (eb *EventBus) SubscribeAll(handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.allHandlers = append(eb.allHandlers, handler)
	logger.Debug("EventBus: subscribed handler for all events")
}

// Unsubscribe removes a handler for a specific event type
func (eb *EventBus) Unsubscribe(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	handlers := eb.subscribers[eventType]
	for i, h := range handlers {
		if &h == &handler {
			eb.subscribers[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
}

// Publish sends an event to all subscribers
func (eb *EventBus) Publish(event Event) {
	// Ensure timestamp is set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case eb.eventChan <- event:
		// Event queued successfully
		logger.Debug("EventBus: published event type=%s session=%s source=%s", event.Type, event.SessionID, event.Source)
	default:
		// Channel full, log warning
		logger.Warn("EventBus: event channel full, dropping event of type %s", event.Type)
	}
}

// PublishSync sends an event synchronously (blocks until handlers complete)
func (eb *EventBus) PublishSync(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	eb.dispatch(event)
}

// dispatcher is the background goroutine that dispatches events to handlers
func (eb *EventBus) dispatcher() {
	for {
		select {
		case <-eb.ctx.Done():
			return
		case event := <-eb.eventChan:
			eb.dispatch(event)
		}
	}
}

// dispatch sends an event to all relevant handlers
func (eb *EventBus) dispatch(event Event) {
	eb.mu.RLock()
	handlers := eb.subscribers[event.Type]
	allHandlers := make([]EventHandler, len(eb.allHandlers))
	copy(allHandlers, eb.allHandlers)
	numTypeHandlers := len(handlers)
	numAllHandlers := len(allHandlers)
	eb.mu.RUnlock()

	logger.Debug("EventBus: dispatching event type=%s to %d type handlers and %d all handlers", event.Type, numTypeHandlers, numAllHandlers)

	// Call type-specific handlers
	for _, handler := range handlers {
		func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("EventBus: handler panicked for event type %s: %v", event.Type, r)
				}
			}()
			h(event)
		}(handler)
	}

	// Call handlers that subscribe to all events
	for _, handler := range allHandlers {
		func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("EventBus: all-handler panicked for event type %s: %v", event.Type, r)
				}
			}()
			h(event)
		}(handler)
	}
}

// Close shuts down the event bus
func (eb *EventBus) Close() {
	eb.cancel()
	close(eb.eventChan)
}

// SystemEventBus is the global event bus instance for the actor system
var SystemEventBus = NewEventBus(1000)

// PublishEvent is a convenience function to publish an event to the system event bus
func PublishEvent(eventType EventType, source, sessionID string, data map[string]interface{}) {
	SystemEventBus.Publish(Event{
		Type:      eventType,
		Source:    source,
		SessionID: sessionID,
		Data:      data,
	})
}

// ActorEventPublisher provides actors with an easy way to publish events
type ActorEventPublisher struct {
	actorID   string
	sessionID string
}

// NewActorEventPublisher creates a new event publisher for an actor
func NewActorEventPublisher(actorID, sessionID string) *ActorEventPublisher {
	return &ActorEventPublisher{
		actorID:   actorID,
		sessionID: sessionID,
	}
}

// Publish publishes an event to the system event bus
func (p *ActorEventPublisher) Publish(eventType EventType, data map[string]interface{}) {
	PublishEvent(eventType, p.actorID, p.sessionID, data)
}

// PublishProgress publishes a progress update event
func (p *ActorEventPublisher) PublishProgress(message string, ephemeral bool) {
	p.Publish(EventTypeProgress, map[string]interface{}{
		"message":   message,
		"ephemeral": ephemeral,
	})
}

// PublishMessage publishes a chat message event
func (p *ActorEventPublisher) PublishMessage(role, content string) {
	p.Publish(EventTypeMessage, map[string]interface{}{
		"role":    role,
		"content": content,
	})
}

// PublishToolCall publishes a tool call event
func (p *ActorEventPublisher) PublishToolCall(toolID, toolName string, parameters map[string]interface{}) {
	p.Publish(EventTypeToolCall, map[string]interface{}{
		"tool_id":    toolID,
		"tool_name":  toolName,
		"parameters": parameters,
	})
}

// PublishToolResult publishes a tool result event
func (p *ActorEventPublisher) PublishToolResult(toolID, result, errorMsg string) {
	data := map[string]interface{}{
		"tool_id": toolID,
		"result":  result,
	}
	if errorMsg != "" {
		data["error"] = errorMsg
	}
	p.Publish(EventTypeToolResult, data)
}

// PublishAuthorization publishes an authorization request event
func (p *ActorEventPublisher) PublishAuthorization(authID, toolName, reason string, parameters map[string]interface{}) {
	p.Publish(EventTypeAuthorization, map[string]interface{}{
		"auth_id":    authID,
		"tool_name":  toolName,
		"reason":     reason,
		"parameters": parameters,
	})
}

// PublishQuestion publishes a question request event
func (p *ActorEventPublisher) PublishQuestion(questionID, question string, multiMode bool) {
	p.Publish(EventTypeQuestion, map[string]interface{}{
		"question_id": questionID,
		"question":    question,
		"multi_mode":  multiMode,
	})
}

// PublishError publishes an error event
func (p *ActorEventPublisher) PublishError(code, message string) {
	p.Publish(EventTypeError, map[string]interface{}{
		"code":    code,
		"message": message,
	})
}

// PublishStatus publishes a status update event
func (p *ActorEventPublisher) PublishStatus(status string, details map[string]interface{}) {
	data := map[string]interface{}{
		"status": status,
	}
	for k, v := range details {
		data[k] = v
	}
	p.Publish(EventTypeStatus, data)
}

// WithSession returns a new publisher with a different session ID
func (p *ActorEventPublisher) WithSession(sessionID string) *ActorEventPublisher {
	return NewActorEventPublisher(p.actorID, sessionID)
}
