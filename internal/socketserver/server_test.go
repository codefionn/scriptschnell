package socketserver

import (
	"testing"
)

// TestNewServer verifies that a new server can be created
func TestNewServer(t *testing.T) {
	// This is a placeholder test - full tests will be added in later tasks
	// For now, just verify the package compiles correctly
}

// TestHubCreation verifies that a hub can be created
func TestHubCreation(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("Hub should not be nil")
	}
}

// TestMessageCreation verifies that messages can be created
func TestMessageCreation(t *testing.T) {
	msg := NewMessage("test_type", nil)
	if msg.Type != "test_type" {
		t.Errorf("Expected message type 'test_type', got '%s'", msg.Type)
	}

	msgWithID := NewRequest("test_type", "req_123", nil)
	if msgWithID.RequestID != "req_123" {
		t.Errorf("Expected request ID 'req_123', got '%s'", msgWithID.RequestID)
	}

	msgError := NewError("req_123", "TEST_ERROR", "Test message", "Test details")
	if msgError.Error == nil {
		t.Fatal("Error info should not be nil")
	}
	if msgError.Error.Code != "TEST_ERROR" {
		t.Errorf("Expected error code 'TEST_ERROR', got '%s'", msgError.Error.Code)
	}
}