package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestOpenAIWebSocketClient_CompleteWithRequest(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("expected Authorization header to be set")
		}
		if r.URL.Path != "/v1/realtime" {
			t.Fatalf("expected path /v1/realtime, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("model") != "gpt-realtime-mini" {
			t.Fatalf("expected model query to be set")
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("failed to upgrade websocket: %v", err)
		}
		defer func() {
			_ = conn.Close()
		}()

		var createEvent map[string]interface{}
		if err := conn.ReadJSON(&createEvent); err != nil {
			t.Fatalf("failed to read request payload: %v", err)
		}
		if got := stringField(createEvent, "type"); got != "response.create" {
			t.Fatalf("expected response.create event, got %q", got)
		}

		_ = conn.WriteJSON(map[string]interface{}{
			"type":  "response.output_text.delta",
			"delta": "Hello ",
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"type":  "response.output_text.delta",
			"delta": "ws",
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"type": "response.done",
			"response": map[string]interface{}{
				"status": "completed",
				"usage": map[string]interface{}{
					"output_tokens": float64(2),
				},
			},
		})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewOpenAIWebSocketClientWithBaseURL("test-key", "gpt-realtime-mini", wsURL+"/v1")
	if err != nil {
		t.Fatalf("failed to create websocket client: %v", err)
	}

	resp, err := client.CompleteWithRequest(context.Background(), &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: "Say hello"},
		},
	})
	if err != nil {
		t.Fatalf("CompleteWithRequest returned error: %v", err)
	}

	if resp.Content != "Hello ws" {
		t.Fatalf("expected concatenated stream content, got %q", resp.Content)
	}
	if resp.StopReason != "completed" {
		t.Fatalf("expected stop reason 'completed', got %q", resp.StopReason)
	}
	if resp.Usage == nil || resp.Usage["output_tokens"] != float64(2) {
		t.Fatalf("expected usage data in response")
	}
}

func TestOpenAIWebSocketClient_Stream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("failed to upgrade websocket: %v", err)
		}
		defer func() {
			_ = conn.Close()
		}()

		var createEvent map[string]interface{}
		if err := conn.ReadJSON(&createEvent); err != nil {
			t.Fatalf("failed to read request payload: %v", err)
		}

		_ = conn.WriteJSON(map[string]interface{}{
			"type":  "response.output_text.delta",
			"delta": "A",
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"type":  "response.output_text.delta",
			"delta": "B",
		})
		_ = conn.WriteJSON(map[string]interface{}{
			"type": "response.done",
			"response": map[string]interface{}{
				"status": "completed",
			},
		})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewOpenAIWebSocketClientWithBaseURL("test-key", "gpt-realtime-mini", wsURL+"/v1")
	if err != nil {
		t.Fatalf("failed to create websocket client: %v", err)
	}

	var got strings.Builder
	err = client.Stream(context.Background(), &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: "stream"},
		},
	}, func(chunk string) error {
		got.WriteString(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	if got.String() != "AB" {
		t.Fatalf("expected stream callback to receive AB, got %q", got.String())
	}
}

func TestShouldUseOpenAIWebSocket(t *testing.T) {
	t.Run("realtime model defaults to websocket", func(t *testing.T) {
		t.Setenv("SCRIPTSCHNELL_OPENAI_TRANSPORT", "")
		if !shouldUseOpenAIWebSocket("gpt-realtime") {
			t.Fatalf("expected realtime model to use websocket transport")
		}
	})

	t.Run("non realtime model defaults to http", func(t *testing.T) {
		t.Setenv("SCRIPTSCHNELL_OPENAI_TRANSPORT", "")
		if shouldUseOpenAIWebSocket("gpt-5") {
			t.Fatalf("expected non-realtime model to use HTTP transport by default")
		}
	})

	t.Run("env var forces websocket", func(t *testing.T) {
		t.Setenv("SCRIPTSCHNELL_OPENAI_TRANSPORT", "websocket")
		if !shouldUseOpenAIWebSocket("gpt-5") {
			t.Fatalf("expected env var to force websocket")
		}
	})

	t.Run("env var forces http", func(t *testing.T) {
		t.Setenv("SCRIPTSCHNELL_OPENAI_TRANSPORT", "http")
		if shouldUseOpenAIWebSocket("gpt-realtime") {
			t.Fatalf("expected env var to force HTTP")
		}
	})
}
