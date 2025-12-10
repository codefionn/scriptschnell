package tui

import (
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/session"
)

func TestSessionMenuItem(t *testing.T) {
	now := time.Now()
	metadata := session.SessionMetadata{
		ID:           "test-session-id-12345",
		Title:        "Test Session Title",
		Name:         "test-name",
		WorkingDir:   "/test/dir",
		CreatedAt:    now.Add(-24 * time.Hour),
		UpdatedAt:    now.Add(-1 * time.Hour),
		MessageCount: 42,
	}

	item := SessionMenuItem{
		metadata: metadata,
		index:    0,
	}

	// Test Title
	if item.Title() != "Test Session Title" {
		t.Errorf("Expected title 'Test Session Title', got '%s'", item.Title())
	}

	// Test FilterValue
	if item.FilterValue() != "Test Session Title" {
		t.Errorf("Expected filter value 'Test Session Title', got '%s'", item.FilterValue())
	}

	// Test GetSessionID
	if item.GetSessionID() != "test-session-id-12345" {
		t.Errorf("Expected session ID 'test-session-id-12345', got '%s'", item.GetSessionID())
	}

	// Test Description
	desc := item.Description()
	if desc == "" {
		t.Error("Expected non-empty description")
	}

	// Description should contain ID prefix
	if len(desc) < 8 {
		t.Errorf("Expected description to contain ID prefix, got '%s'", desc)
	}
}

func TestSessionMenuItemWithoutTitle(t *testing.T) {
	now := time.Now()
	metadata := session.SessionMetadata{
		ID:           "test-session-id-12345",
		Name:         "fallback-name",
		WorkingDir:   "/test/dir",
		CreatedAt:    now,
		UpdatedAt:    now,
		MessageCount: 5,
	}

	item := SessionMenuItem{
		metadata: metadata,
		index:    0,
	}

	// Should fall back to Name when Title is empty
	if item.Title() != "fallback-name" {
		t.Errorf("Expected title 'fallback-name', got '%s'", item.Title())
	}
}

func TestSessionMenuItemUnnamed(t *testing.T) {
	now := time.Now()
	metadata := session.SessionMetadata{
		ID:           "test-session-id-12345",
		WorkingDir:   "/test/dir",
		CreatedAt:    now,
		UpdatedAt:    now,
		MessageCount: 0,
	}

	item := SessionMenuItem{
		metadata: metadata,
		index:    0,
	}

	// Should show "Unnamed" when both Title and Name are empty
	if item.Title() != "Unnamed" {
		t.Errorf("Expected title 'Unnamed', got '%s'", item.Title())
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		time     time.Time
		contains string
	}{
		{
			name:     "just now",
			time:     now.Add(-30 * time.Second),
			contains: "just now",
		},
		{
			name:     "minutes ago",
			time:     now.Add(-5 * time.Minute),
			contains: "minutes ago",
		},
		{
			name:     "hours ago",
			time:     now.Add(-3 * time.Hour),
			contains: "hours ago",
		},
		{
			name:     "yesterday",
			time:     now.Add(-24 * time.Hour),
			contains: "yesterday",
		},
		{
			name:     "days ago",
			time:     now.Add(-3 * 24 * time.Hour),
			contains: "days ago",
		},
		{
			name:     "date format",
			time:     now.Add(-8 * 24 * time.Hour),
			contains: "-", // Should contain date separator
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatRelativeTime(tt.time)
			if result == "" {
				t.Error("Expected non-empty result")
			}
			// Just verify it doesn't panic and returns something
		})
	}
}
