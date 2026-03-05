package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const openAIRealtimePath = "/realtime"

// OpenAIWebSocketClient implements Client using OpenAI's realtime websocket endpoint.
type OpenAIWebSocketClient struct {
	apiKey  string
	model   string
	baseURL string
	dialer  *websocket.Dialer
}

func NewOpenAIWebSocketClient(apiKey, modelName string) (Client, error) {
	return NewOpenAIWebSocketClientWithBaseURL(apiKey, modelName, openAIDefaultBaseURL)
}

func NewOpenAIWebSocketClientWithBaseURL(apiKey, modelName, baseURL string) (*OpenAIWebSocketClient, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("openai websocket client requires an API key")
	}

	model := strings.TrimSpace(modelName)
	if model == "" {
		return nil, fmt.Errorf("openai websocket client requires a model")
	}

	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = openAIDefaultBaseURL
	}

	return &OpenAIWebSocketClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: trimmedBaseURL,
		dialer: &websocket.Dialer{
			HandshakeTimeout: 30 * time.Second,
		},
	}, nil
}

func (c *OpenAIWebSocketClient) GetModelName() string {
	return c.model
}

func (c *OpenAIWebSocketClient) GetLastResponseID() string {
	return ""
}

func (c *OpenAIWebSocketClient) SetPreviousResponseID(responseID string) {
}

func (c *OpenAIWebSocketClient) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := c.CompleteWithRequest(ctx, &CompletionRequest{
		Messages: []*Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 1.0,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (c *OpenAIWebSocketClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("openai websocket completion request cannot be nil")
	}
	return c.executeRealtimeRequest(ctx, req, nil)
}

func (c *OpenAIWebSocketClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("openai websocket completion request cannot be nil")
	}

	_, err := c.executeRealtimeRequest(ctx, req, callback)
	return err
}

func (c *OpenAIWebSocketClient) executeRealtimeRequest(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) (*CompletionResponse, error) {
	realtimeURL, err := buildOpenAIRealtimeURL(c.baseURL, c.model)
	if err != nil {
		return nil, err
	}

	headers := map[string][]string{
		"Authorization": {"Bearer " + c.apiKey},
		"OpenAI-Beta":   {"realtime=v1"},
	}

	conn, resp, err := c.dialer.DialContext(ctx, realtimeURL, headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("openai websocket connection failed: %s", resp.Status)
		}
		return nil, fmt.Errorf("openai websocket connection failed: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	requestPayload, err := c.buildRealtimeRequestPayload(req)
	if err != nil {
		return nil, err
	}

	if err := conn.WriteJSON(requestPayload); err != nil {
		return nil, fmt.Errorf("openai websocket write failed: %w", err)
	}

	return readOpenAIRealtimeEvents(ctx, conn, callback)
}

func (c *OpenAIWebSocketClient) buildRealtimeRequestPayload(req *CompletionRequest) (map[string]interface{}, error) {
	inputItems, err := buildResponsesInput(req.Messages)
	if err != nil {
		return nil, err
	}

	if len(inputItems) == 0 {
		return nil, fmt.Errorf("openai websocket completion requires at least one message")
	}

	response := map[string]interface{}{
		"modalities": []string{"text"},
		"input":      toJSONCompatible(inputItems),
	}

	if req.SystemPrompt != "" {
		response["instructions"] = req.SystemPrompt
	}
	if req.Temperature != 0 && !isOpenAITemperatureUnsupported(c.model) {
		response["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		response["max_output_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		response["tools"] = toJSONCompatible(convertResponsesTools(req.Tools))
	}

	return map[string]interface{}{
		"type":     "response.create",
		"response": response,
	}, nil
}

func toJSONCompatible(value interface{}) interface{} {
	b, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func buildOpenAIRealtimeURL(baseURL, model string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("openai websocket invalid base URL: %w", err)
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
		// keep as-is
	default:
		return "", fmt.Errorf("openai websocket unsupported URL scheme: %q", u.Scheme)
	}

	basePath := strings.TrimRight(u.Path, "/")
	if basePath == "" {
		basePath = "/v1"
	}
	if !strings.HasSuffix(basePath, openAIRealtimePath) {
		basePath += openAIRealtimePath
	}
	u.Path = basePath

	query := u.Query()
	query.Set("model", model)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func readOpenAIRealtimeEvents(ctx context.Context, conn *websocket.Conn, callback func(chunk string) error) (*CompletionResponse, error) {
	response := &CompletionResponse{StopReason: "stop"}
	var contentBuilder strings.Builder
	sawDelta := false

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("openai websocket read failed: %w", err)
		}

		var event map[string]interface{}
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("openai websocket decode failed: %w", err)
		}

		eventType := stringField(event, "type")
		switch eventType {
		case "error":
			errMsg := nestedStringField(event, "error", "message")
			if errMsg == "" {
				errMsg = "unknown realtime error"
			}
			return nil, fmt.Errorf("openai websocket error: %s", errMsg)
		case "response.output_text.delta":
			delta := stringField(event, "delta")
			if delta == "" {
				continue
			}
			sawDelta = true
			contentBuilder.WriteString(delta)
			if callback != nil {
				if err := callback(delta); err != nil {
					return nil, err
				}
			}
		case "response.output_text.done":
			if sawDelta {
				continue
			}
			text := stringField(event, "text")
			if text == "" {
				continue
			}
			contentBuilder.WriteString(text)
			if callback != nil {
				if err := callback(text); err != nil {
					return nil, err
				}
			}
		case "response.function_call_arguments.done":
			call := map[string]interface{}{
				"id":   stringField(event, "call_id"),
				"type": "function",
				"function": map[string]interface{}{
					"name":      stringField(event, "name"),
					"arguments": stringField(event, "arguments"),
				},
			}
			if call["id"] == "" {
				call["id"] = "call_unknown"
			}
			response.ToolCalls = append(response.ToolCalls, call)
		case "response.done":
			final := mapField(event, "response")
			response.Content = contentBuilder.String()
			if status := stringField(final, "status"); status != "" {
				response.StopReason = status
			}

			if usage := mapField(final, "usage"); usage != nil {
				response.Usage = usage
			}

			if len(response.ToolCalls) == 0 {
				response.ToolCalls = append(response.ToolCalls, extractRealtimeToolCalls(final)...)
			}

			if response.Content == "" {
				response.Content = extractRealtimeOutputText(final)
			}

			return response, nil
		}
	}
}

func extractRealtimeToolCalls(response map[string]interface{}) []map[string]interface{} {
	output, ok := response["output"].([]interface{})
	if !ok {
		return nil
	}

	result := make([]map[string]interface{}, 0)
	for _, item := range output {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if stringField(itemMap, "type") != "function_call" {
			continue
		}

		toolCall := map[string]interface{}{
			"id":   stringField(itemMap, "call_id"),
			"type": "function",
			"function": map[string]interface{}{
				"name":      stringField(itemMap, "name"),
				"arguments": stringField(itemMap, "arguments"),
			},
		}
		if toolCall["id"] == "" {
			toolCall["id"] = stringField(itemMap, "id")
		}
		result = append(result, toolCall)
	}

	return result
}

func extractRealtimeOutputText(response map[string]interface{}) string {
	output, ok := response["output"].([]interface{})
	if !ok {
		return ""
	}

	var builder strings.Builder
	for _, item := range output {
		itemMap, ok := item.(map[string]interface{})
		if !ok || stringField(itemMap, "type") != "message" {
			continue
		}

		content, ok := itemMap["content"].([]interface{})
		if !ok {
			continue
		}
		for _, part := range content {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if txt := stringField(partMap, "text"); txt != "" {
				builder.WriteString(txt)
			}
		}
	}

	return builder.String()
}

func mapField(data map[string]interface{}, key string) map[string]interface{} {
	if data == nil {
		return nil
	}
	raw, ok := data[key]
	if !ok {
		return nil
	}
	out, _ := raw.(map[string]interface{})
	return out
}

func stringField(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	raw, ok := data[key]
	if !ok {
		return ""
	}
	value, _ := raw.(string)
	return value
}

func nestedStringField(data map[string]interface{}, parent, child string) string {
	return stringField(mapField(data, parent), child)
}
