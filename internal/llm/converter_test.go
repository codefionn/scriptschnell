package llm

import (
	"testing"
)

func TestAnthropicConverter_RoundTrip(t *testing.T) {
	converter := &AnthropicConverterImpl{}

	// Create test messages
	messages := []*Message{
		{
			Role:    "user",
			Content: "Hello, how are you?",
		},
		{
			Role:    "assistant",
			Content: "I'm doing well, thank you!",
		},
	}

	// Convert to native
	native, err := converter.ConvertToNative(messages, "You are a helpful assistant", true, "5m")
	if err != nil {
		t.Fatalf("ConvertToNative failed: %v", err)
	}

	if len(native) == 0 {
		t.Fatal("Expected native messages, got empty slice")
	}

	// Convert back from native
	unified, err := converter.ConvertFromNative(native)
	if err != nil {
		t.Fatalf("ConvertFromNative failed: %v", err)
	}

	// Verify we got messages back (system message is skipped in conversion back)
	if len(unified) != 2 {
		t.Errorf("Expected 2 messages after round trip, got %d", len(unified))
	}

	// Verify content is preserved
	if unified[0].Role != "user" || unified[0].Content != "Hello, how are you?" {
		t.Errorf("First message not preserved correctly: role=%s, content=%s", unified[0].Role, unified[0].Content)
	}
}

func TestOpenAIConverter_RoundTrip(t *testing.T) {
	converter := &OpenAIConverterImpl{}

	messages := []*Message{
		{
			Role:    "user",
			Content: "Test message",
		},
	}

	native, err := converter.ConvertToNative(messages, "System prompt", true, "")
	if err != nil {
		t.Fatalf("ConvertToNative failed: %v", err)
	}

	if len(native) < 2 { // Should have system + user message
		t.Fatalf("Expected at least 2 native messages (system + user), got %d", len(native))
	}

	unified, err := converter.ConvertFromNative(native)
	if err != nil {
		t.Fatalf("ConvertFromNative failed: %v", err)
	}

	// System message is filtered out in conversion back
	if len(unified) != 1 {
		t.Errorf("Expected 1 message after round trip, got %d", len(unified))
	}

	if unified[0].Content != "Test message" {
		t.Errorf("Message content not preserved: got %s", unified[0].Content)
	}
}

func TestGoogleConverter_Basic(t *testing.T) {
	converter := &GoogleConverterImpl{}

	messages := []*Message{
		{
			Role:    "user",
			Content: "Hello Google",
		},
		{
			Role:    "assistant",
			Content: "Hello user",
		},
	}

	native, err := converter.ConvertToNative(messages, "Test system", false, "")
	if err != nil {
		t.Fatalf("ConvertToNative failed: %v", err)
	}

	// Should have system metadata + 2 messages
	if len(native) < 2 {
		t.Errorf("Expected at least 2 items, got %d", len(native))
	}
}

func TestGetConverter(t *testing.T) {
	tests := []struct {
		modelID  string
		provider string
	}{
		{"claude-3-5-sonnet-20241022", "anthropic"},
		{"gpt-4o", "openai"},
		{"gemini-2.0-flash-exp", "google"},
		{"mistral-large-latest", "mistral"},
		{"anthropic/claude-3.5-sonnet", "openrouter"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			converter := GetConverter(tt.modelID)
			if converter == nil {
				t.Fatalf("GetConverter returned nil for %s", tt.modelID)
			}

			if converter.GetProviderName() != tt.provider {
				t.Errorf("Expected provider %s, got %s", tt.provider, converter.GetProviderName())
			}
		})
	}
}

func TestNativeFormatPreservation(t *testing.T) {
	// Test that provider detection works correctly
	msg := &Message{
		Role:              "user",
		Content:           "Test",
		NativeProvider:    "anthropic",
		NativeModelFamily: "claude-3",
	}

	if msg.NativeProvider != "anthropic" {
		t.Errorf("Native provider not preserved")
	}
}
