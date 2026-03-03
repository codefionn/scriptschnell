package actor

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventBus_BasicPublishSubscribe(t *testing.T) {
	bus := NewEventBus(100)
	defer bus.Close()

	var received Event
	done := make(chan bool, 1)

	bus.Subscribe(EventTypeProgress, func(e Event) {
		received = e
		done <- true
	})

	event := Event{
		Type:      EventTypeProgress,
		Source:    "test-actor",
		SessionID: "test-session",
		Data: map[string]interface{}{
			"message": "test progress",
		},
	}

	bus.Publish(event)

	select {
	case <-done:
		if received.Type != EventTypeProgress {
			t.Errorf("Expected event type %s, got %s", EventTypeProgress, received.Type)
		}
		if received.Source != "test-actor" {
			t.Errorf("Expected source 'test-actor', got %s", received.Source)
		}
		if received.Data["message"] != "test progress" {
			t.Errorf("Expected message 'test progress', got %v", received.Data["message"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestEventBus_SubscribeAll(t *testing.T) {
	bus := NewEventBus(100)
	defer bus.Close()

	var count int32
	done := make(chan bool, 1)

	bus.SubscribeAll(func(e Event) {
		atomic.AddInt32(&count, 1)
		if atomic.LoadInt32(&count) >= 2 {
			done <- true
		}
	})

	bus.Publish(Event{
		Type:   EventTypeProgress,
		Source: "test",
		Data:   map[string]interface{}{},
	})

	bus.Publish(Event{
		Type:   EventTypeMessage,
		Source: "test",
		Data:   map[string]interface{}{},
	})

	select {
	case <-done:
		if count != 2 {
			t.Errorf("Expected 2 events, got %d", count)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for events")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus(100)
	defer bus.Close()

	var count1, count2 int32

	bus.Subscribe(EventTypeProgress, func(e Event) {
		atomic.AddInt32(&count1, 1)
	})

	bus.Subscribe(EventTypeProgress, func(e Event) {
		atomic.AddInt32(&count2, 1)
	})

	bus.Publish(Event{
		Type:   EventTypeProgress,
		Source: "test",
		Data:   map[string]interface{}{},
	})

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&count1) != 1 {
		t.Errorf("Expected subscriber 1 to receive 1 event, got %d", count1)
	}
	if atomic.LoadInt32(&count2) != 1 {
		t.Errorf("Expected subscriber 2 to receive 1 event, got %d", count2)
	}
}

func TestEventBus_BufferFull(t *testing.T) {
	// Create a small buffer
	bus := NewEventBus(1)
	defer bus.Close()

	// Block the dispatcher
	block := make(chan bool)
	bus.SubscribeAll(func(e Event) {
		<-block
	})

	// Fill the buffer and one more (should not block or panic)
	for i := 0; i < 10; i++ {
		bus.Publish(Event{
			Type:   EventTypeProgress,
			Source: "test",
			Data:   map[string]interface{}{"index": i},
		})
	}

	// Unblock to allow cleanup
	close(block)
	time.Sleep(50 * time.Millisecond)
}

func TestEventBus_ConcurrentPublishSubscribe(t *testing.T) {
	bus := NewEventBus(1000)
	defer bus.Close()

	var count int32
	var wg sync.WaitGroup

	// Multiple subscribers
	for i := 0; i < 5; i++ {
		bus.Subscribe(EventTypeProgress, func(e Event) {
			atomic.AddInt32(&count, 1)
		})
	}

	// Multiple publishers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				bus.Publish(Event{
					Type:   EventTypeProgress,
					Source: "test",
					Data:   map[string]interface{}{"publisher": index, "msg": j},
				})
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // Allow processing

	// 10 publishers * 10 messages * 5 subscribers = 500 total
	expected := int32(10 * 10 * 5)
	actual := atomic.LoadInt32(&count)
	if actual != expected {
		t.Errorf("Expected %d events, got %d", expected, actual)
	}
}

func TestPublishEvent(t *testing.T) {
	// Use a fresh event bus for this test
	originalBus := SystemEventBus
	SystemEventBus = NewEventBus(100)
	defer func() {
		SystemEventBus.Close()
		SystemEventBus = originalBus
	}()

	var received Event
	done := make(chan bool, 1)

	SystemEventBus.Subscribe(EventTypeMessage, func(e Event) {
		received = e
		done <- true
	})

	PublishEvent(EventTypeMessage, "actor-1", "session-1", map[string]interface{}{
		"content": "hello",
	})

	select {
	case <-done:
		if received.Source != "actor-1" {
			t.Errorf("Expected source 'actor-1', got %s", received.Source)
		}
		if received.SessionID != "session-1" {
			t.Errorf("Expected session 'session-1', got %s", received.SessionID)
		}
		if received.Data["content"] != "hello" {
			t.Errorf("Expected content 'hello', got %v", received.Data["content"])
		}
		if received.Timestamp.IsZero() {
			t.Error("Expected timestamp to be set")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestActorEventPublisher(t *testing.T) {
	// Use a fresh event bus
	originalBus := SystemEventBus
	SystemEventBus = NewEventBus(100)
	defer func() {
		SystemEventBus.Close()
		SystemEventBus = originalBus
	}()

	publisher := NewActorEventPublisher("my-actor", "my-session")

	var received Event
	done := make(chan bool, 1)

	SystemEventBus.Subscribe(EventTypeProgress, func(e Event) {
		received = e
		done <- true
	})

	publisher.PublishProgress("test message", true)

	select {
	case <-done:
		if received.Source != "my-actor" {
			t.Errorf("Expected source 'my-actor', got %s", received.Source)
		}
		if received.SessionID != "my-session" {
			t.Errorf("Expected session 'my-session', got %s", received.SessionID)
		}
		if received.Data["message"] != "test message" {
			t.Errorf("Expected message 'test message', got %v", received.Data["message"])
		}
		if received.Data["ephemeral"] != true {
			t.Errorf("Expected ephemeral true, got %v", received.Data["ephemeral"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestActorEventPublisher_PublishMessage(t *testing.T) {
	originalBus := SystemEventBus
	SystemEventBus = NewEventBus(100)
	defer func() {
		SystemEventBus.Close()
		SystemEventBus = originalBus
	}()

	publisher := NewActorEventPublisher("my-actor", "my-session")

	var received Event
	done := make(chan bool, 1)

	SystemEventBus.Subscribe(EventTypeMessage, func(e Event) {
		received = e
		done <- true
	})

	publisher.PublishMessage("user", "hello world")

	select {
	case <-done:
		if received.Data["role"] != "user" {
			t.Errorf("Expected role 'user', got %v", received.Data["role"])
		}
		if received.Data["content"] != "hello world" {
			t.Errorf("Expected content 'hello world', got %v", received.Data["content"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestActorEventPublisher_PublishToolCall(t *testing.T) {
	originalBus := SystemEventBus
	SystemEventBus = NewEventBus(100)
	defer func() {
		SystemEventBus.Close()
		SystemEventBus = originalBus
	}()

	publisher := NewActorEventPublisher("my-actor", "my-session")

	var received Event
	done := make(chan bool, 1)

	SystemEventBus.Subscribe(EventTypeToolCall, func(e Event) {
		received = e
		done <- true
	})

	params := map[string]interface{}{"arg1": "value1"}
	publisher.PublishToolCall("tool-123", "my-tool", params)

	select {
	case <-done:
		if received.Data["tool_id"] != "tool-123" {
			t.Errorf("Expected tool_id 'tool-123', got %v", received.Data["tool_id"])
		}
		if received.Data["tool_name"] != "my-tool" {
			t.Errorf("Expected tool_name 'my-tool', got %v", received.Data["tool_name"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestActorEventPublisher_WithSession(t *testing.T) {
	originalBus := SystemEventBus
	SystemEventBus = NewEventBus(100)
	defer func() {
		SystemEventBus.Close()
		SystemEventBus = originalBus
	}()

	publisher := NewActorEventPublisher("my-actor", "original-session")
	newPublisher := publisher.WithSession("new-session")

	var received Event
	done := make(chan bool, 1)

	SystemEventBus.Subscribe(EventTypeProgress, func(e Event) {
		received = e
		done <- true
	})

	newPublisher.PublishProgress("test", false)

	select {
	case <-done:
		if received.SessionID != "new-session" {
			t.Errorf("Expected session 'new-session', got %s", received.SessionID)
		}
		// Original publisher should still send events with original session
		var originalSessionEvent Event
		done2 := make(chan bool, 1)
		SystemEventBus.Subscribe(EventTypeStatus, func(e Event) {
			originalSessionEvent = e
			done2 <- true
		})
		publisher.PublishStatus("test", nil)

		select {
		case <-done2:
			if originalSessionEvent.SessionID != "original-session" {
				t.Errorf("Original publisher session should be 'original-session', got %s", originalSessionEvent.SessionID)
			}
		case <-time.After(time.Second):
			t.Error("Timeout waiting for original session event")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestActorRef_EventPublisher(t *testing.T) {
	actor := &TestActor{id: "test-actor"}
	ref := NewActorRef("test-actor", actor, 10)

	if ref.EventPublisher() == nil {
		t.Error("Expected EventPublisher to not be nil")
	}

	if ref.EventPublisher().actorID != "test-actor" {
		t.Errorf("Expected actorID 'test-actor', got %s", ref.EventPublisher().actorID)
	}
}

func TestActorRef_SetSessionID(t *testing.T) {
	actor := &TestActor{id: "test-actor"}
	ref := NewActorRef("test-actor", actor, 10)

	ref.SetSessionID("session-123")

	if ref.EventPublisher().sessionID != "session-123" {
		t.Errorf("Expected sessionID 'session-123', got %s", ref.EventPublisher().sessionID)
	}
}
