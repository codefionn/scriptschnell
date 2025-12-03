package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openRouterAPIBaseURL = "https://openrouter.ai/api/v1"
	openRouterReferer    = "https://github.com/codefionn/scriptschnell"
	openRouterAppTitle   = "scriptschnell"
)

// OpenRouterClient implements the Client interface using the native OpenRouter API.
type OpenRouterClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewOpenRouterClient creates a new OpenRouter client.
func NewOpenRouterClient(apiKey, modelID string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("openrouter client requires an API key")
	}

	model := strings.TrimSpace(modelID)
	if model == "" {
		model = "openai/gpt-4o-mini"
	}

	return &OpenRouterClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: openRouterAPIBaseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

func (c *OpenRouterClient) GetModelName() string {
	return c.model
}

func (c *OpenRouterClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *OpenRouterClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("openrouter completion request cannot be nil")
	}

	payload, err := c.buildChatRequest(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := c.newChatRequest(ctx, payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter completion failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp openRouterChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("openrouter completion failed: %w", err)
	}

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message == nil {
		return &CompletionResponse{StopReason: "stop"}, nil
	}

	first := chatResp.Choices[0]
	content := extractOpenRouterText(first.Message.Content)
	stopReason := first.FinishReason
	if strings.TrimSpace(stopReason) == "" {
		stopReason = "stop"
	}

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  convertOpenRouterToolCalls(first.Message.ToolCalls),
		StopReason: stopReason,
	}, nil
}

func (c *OpenRouterClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("openrouter completion request cannot be nil")
	}

	payload, err := c.buildChatRequest(req, true)
	if err != nil {
		return err
	}

	httpReq, err := c.newChatRequest(ctx, payload)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openrouter stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openrouter stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buffer := make([]byte, 0, 256*1024)
	scanner.Buffer(buffer, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var chunk openRouterStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("openrouter stream failed to decode chunk: %w", err)
		}

		for _, choice := range chunk.Choices {
			if choice.Delta == nil {
				continue
			}

			text := extractOpenRouterText(choice.Delta.Content)
			if strings.TrimSpace(text) == "" {
				continue
			}
			if err := callback(text); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openrouter stream failed: %w", err)
	}

	return nil
}

func (c *OpenRouterClient) buildChatRequest(req *CompletionRequest, stream bool) (*openRouterChatRequest, error) {
	messages, err := convertMessagesToOpenRouter(req)
	if err != nil {
		return nil, err
	}

	payload := &openRouterChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   stream,
	}

	if req.Temperature != 0 {
		temp := req.Temperature
		payload.Temperature = &temp
	}
	if req.MaxTokens > 0 {
		payload.MaxTokens = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		payload.Tools = req.Tools
	}

	return payload, nil
}

func (c *OpenRouterClient) newChatRequest(ctx context.Context, payload *openRouterChatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openrouter failed to encode payload: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", openRouterReferer)
	req.Header.Set("X-Title", openRouterAppTitle)

	return req, nil
}

func convertMessagesToOpenRouter(req *CompletionRequest) ([]openRouterChatMessage, error) {
	if req == nil {
		return nil, fmt.Errorf("openrouter completion request cannot be nil")
	}

	messages := make([]openRouterChatMessage, 0, len(req.Messages)+1)

	if system := strings.TrimSpace(req.SystemPrompt); system != "" {
		sysMsg := openRouterChatMessage{
			Role: "system",
		}

		// Use multipart content with cache_control for caching support
		if req.EnableCaching {
			sysMsg.Content = []openRouterContentBlock{
				{
					Type:         "text",
					Text:         system,
					CacheControl: map[string]interface{}{"type": "ephemeral"},
				},
			}
		} else {
			sysMsg.Content = system
		}

		messages = append(messages, sysMsg)
	}

	for _, msg := range req.Messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "user"
		}

		oMsg := openRouterChatMessage{
			Role:    role,
			Content: msg.Content,
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			oMsg.ToolCalls = msg.ToolCalls
		}

		if role == "tool" && msg.ToolID != "" {
			oMsg.ToolCallID = msg.ToolID
		}

		if msg.ToolName != "" {
			oMsg.Name = msg.ToolName
		}

		messages = append(messages, oMsg)
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("openrouter completion requires at least one message")
	}

	return messages, nil
}

func convertOpenRouterToolCalls(toolCalls []openRouterToolCall) []map[string]interface{} {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if tc.Function == nil {
			continue
		}

		call := map[string]interface{}{
			"type": tc.Type,
			"function": map[string]interface{}{
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			},
		}
		if tc.ID != "" {
			call["id"] = tc.ID
		}
		result = append(result, call)
	}
	return result
}

func extractOpenRouterText(content interface{}) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []interface{}:
		var sb strings.Builder
		for _, part := range value {
			sb.WriteString(extractOpenRouterText(part))
		}
		return sb.String()
	case map[string]interface{}:
		if text, ok := value["text"].(string); ok {
			return text
		}
		if inner, ok := value["content"]; ok {
			return extractOpenRouterText(inner)
		}
	case json.RawMessage:
		var decoded interface{}
		if err := json.Unmarshal(value, &decoded); err == nil {
			return extractOpenRouterText(decoded)
		}
	}
	return ""
}

type openRouterChatRequest struct {
	Model       string                   `json:"model"`
	Messages    []openRouterChatMessage  `json:"messages"`
	Tools       []map[string]interface{} `json:"tools,omitempty"`
	Temperature *float64                 `json:"temperature,omitempty"`
	MaxTokens   int                      `json:"max_tokens,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
}

type openRouterChatMessage struct {
	Role       string                   `json:"role"`
	Content    interface{}              `json:"content"` // Can be string or []contentBlock for caching
	Name       string                   `json:"name,omitempty"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
}

type openRouterContentBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl map[string]interface{} `json:"cache_control,omitempty"`
}

type openRouterChatResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Model   string                 `json:"model"`
	Created int64                  `json:"created"`
	Usage   map[string]interface{} `json:"usage,omitempty"`
	Choices []openRouterChatChoice `json:"choices"`
}

type openRouterChatChoice struct {
	Index        int                            `json:"index"`
	FinishReason string                         `json:"finish_reason"`
	Message      *openRouterChatResponseMessage `json:"message"`
}

type openRouterChatResponseMessage struct {
	Role      string               `json:"role"`
	Content   interface{}          `json:"content"`
	ToolCalls []openRouterToolCall `json:"tool_calls,omitempty"`
}

type openRouterToolCall struct {
	ID       string                  `json:"id,omitempty"`
	Type     string                  `json:"type,omitempty"`
	Function *openRouterToolFunction `json:"function,omitempty"`
}

type openRouterToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openRouterStreamChunk struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Choices []openRouterStreamChoice `json:"choices"`
}

type openRouterStreamChoice struct {
	Index        int                    `json:"index"`
	FinishReason string                 `json:"finish_reason"`
	Delta        *openRouterStreamDelta `json:"delta"`
}

type openRouterStreamDelta struct {
	Role      string               `json:"role"`
	Content   interface{}          `json:"content"`
	ToolCalls []openRouterToolCall `json:"tool_calls,omitempty"`
}
