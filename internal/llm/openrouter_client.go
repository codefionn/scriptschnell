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

	"github.com/codefionn/scriptschnell/internal/logger"
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
		model = "openai/o3-mini"
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

	logger.Debug("OpenRouter: sending completion request for model %s", c.model)

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

	logger.Debug("OpenRouter: received response with %d choices, usage: %v", len(chatResp.Choices), chatResp.Usage)

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message == nil {
		logger.Debug("OpenRouter: no valid choices in response, returning stop reason")
		return &CompletionResponse{StopReason: "stop"}, nil
	}

	first := chatResp.Choices[0]
	content := extractOpenRouterText(first.Message.Content)
	stopReason := first.FinishReason
	if strings.TrimSpace(stopReason) == "" {
		stopReason = "stop"
	}

	logger.Debug("OpenRouter: extracted content length=%d, tool_calls=%d, usage=%v", len(content), len(first.Message.ToolCalls), chatResp.Usage)

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  convertOpenRouterToolCalls(first.Message.ToolCalls),
		StopReason: stopReason,
		Usage:      chatResp.Usage,
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

	logger.Debug("OpenRouter: starting stream request for model %s", c.model)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openrouter stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openrouter stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	logger.Debug("OpenRouter: stream connection established, processing chunks")

	scanner := bufio.NewScanner(resp.Body)
	buffer := make([]byte, 0, 256*1024)
	scanner.Buffer(buffer, 1024*1024)

	chunkCount := 0
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
			logger.Debug("OpenRouter: received [DONE] signal after %d chunks", chunkCount)
			break
		}

		var chunk openRouterStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("openrouter stream failed to decode chunk: %w", err)
		}

		logger.Debug("OpenRouter: received chunk %d with %d choices", chunkCount+1, len(chunk.Choices))
		chunkCount++

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

	logger.Debug("OpenRouter: stream completed with %d chunks processed", chunkCount)

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openrouter stream failed: %w", err)
	}

	return nil
}

func (c *OpenRouterClient) buildChatRequest(req *CompletionRequest, stream bool) (*openRouterChatRequest, error) {
	logger.Debug("OpenRouter: building chat request for model %s, stream=%v", c.model, stream)

	messages, err := c.convertMessagesToOpenRouter(req)
	if err != nil {
		return nil, err
	}

	logger.Debug("OpenRouter: converted %d messages for request", len(messages))

	payload := &openRouterChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   stream,
	}

	if req.Temperature != 0 {
		temp := req.Temperature
		payload.Temperature = &temp
		logger.Debug("OpenRouter: set temperature to %f", temp)
	}
	if req.MaxTokens > 0 {
		payload.MaxTokens = req.MaxTokens
		logger.Debug("OpenRouter: set max_tokens to %d", req.MaxTokens)
	}
	if len(req.Tools) > 0 {
		payload.Tools = req.Tools
		logger.Debug("OpenRouter: set %d tools", len(req.Tools))
	}

	return payload, nil
}

// getUnderlyingProvider extracts the underlying provider from OpenRouter model ID
// e.g., "mistralai/codestral-2508" -> "mistralai"
func (c *OpenRouterClient) getUnderlyingProvider() string {
	parts := strings.Split(c.model, "/")
	if len(parts) > 1 {
		return strings.ToLower(parts[0])
	}
	return ""
}

// providerSupportsMultipartCache returns true if the provider supports multipart content format with cache_control
func (c *OpenRouterClient) providerSupportsMultipartCache() bool {
	provider := c.getUnderlyingProvider()
	switch provider {
	case "openai", "anthropic", "google":
		return true
	case "":
		// If we can't determine the provider (e.g., model doesn't have /), be conservative
		return false
	default:
		// Mistral, Cerebras, Cohere, DeepSeek, etc. don't support multipart format
		return false
	}
}

func (c *OpenRouterClient) newChatRequest(ctx context.Context, payload *openRouterChatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openrouter failed to encode payload: %w", err)
	}

	logger.Debug("OpenRouter: creating HTTP request with payload size=%d bytes", len(body))

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", openRouterReferer)
	req.Header.Set("X-Title", openRouterAppTitle)

	logger.Debug("OpenRouter: created HTTP request for %s with %d bytes", url, len(body))

	return req, nil
}

func (c *OpenRouterClient) convertMessagesToOpenRouter(req *CompletionRequest) ([]openRouterChatMessage, error) {
	if req == nil {
		return nil, fmt.Errorf("openrouter completion request cannot be nil")
	}

	// Check if we can use native OpenRouter format
	hasNativeFormat := len(req.Messages) > 0 && req.Messages[0].NativeFormat != nil && req.Messages[0].NativeProvider == "openrouter"

	if hasNativeFormat {
		logger.Debug("Using native OpenRouter message format (%d messages)", len(req.Messages))
		messages, err := extractNativeOpenRouterMessages(req.Messages, req.SystemPrompt)
		if err != nil {
			logger.Warn("Failed to extract native OpenRouter messages, falling back to conversion: %v", err)
			return c.convertMessagesToOpenRouterFromUnified(req)
		}
		return messages, nil
	}

	return c.convertMessagesToOpenRouterFromUnified(req)
}

func (c *OpenRouterClient) convertMessagesToOpenRouterFromUnified(req *CompletionRequest) ([]openRouterChatMessage, error) {
	logger.Debug("convertMessagesToOpenRouterFromUnified: converting %d messages for model %s, system_prompt=%d chars, caching=%v", len(req.Messages), c.model, len(req.SystemPrompt), req.EnableCaching)

	messages := make([]openRouterChatMessage, 0, len(req.Messages)+1)

	if system := strings.TrimSpace(req.SystemPrompt); system != "" {
		logger.Debug("convertMessagesToOpenRouterFromUnified: adding system prompt with %d chars", len(system))
		sysMsg := openRouterChatMessage{
			Role: "system",
		}

		// Check if the underlying provider supports multipart content format with cache_control
		// Some providers (like Mistral) expect simple string content and fail with 422 on multipart format
		if req.EnableCaching && c.providerSupportsMultipartCache() {
			sysMsg.Content = []openRouterContentBlock{
				{
					Type:         "text",
					Text:         system,
					CacheControl: map[string]interface{}{"type": "ephemeral"},
				},
			}
			logger.Debug("convertMessagesToOpenRouterFromUnified: added system prompt with caching enabled (multipart format)")
		} else {
			sysMsg.Content = system
			if req.EnableCaching {
				logger.Debug("convertMessagesToOpenRouterFromUnified: added system prompt with caching disabled (provider %s doesn't support multipart format)", c.getUnderlyingProvider())
			} else {
				logger.Debug("convertMessagesToOpenRouterFromUnified: added system prompt without caching")
			}
		}

		messages = append(messages, sysMsg)
	}

	logger.Debug("convertMessagesToOpenRouterFromUnified: converting %d user messages", len(req.Messages))

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
			// Mistral (via OpenRouter) rejects tool calls with call_id field - only id is allowed
			if c.getUnderlyingProvider() == "mistralai" {
				oMsg.ToolCalls = removeCallIDFromToolCalls(msg.ToolCalls)
			} else {
				oMsg.ToolCalls = msg.ToolCalls
			}
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

	logger.Debug("convertMessagesToOpenRouterFromUnified: successfully converted %d messages", len(messages))
	return messages, nil
}

// extractNativeOpenRouterMessages extracts native OpenRouter messages from unified messages
// Note: System prompt should NOT be added here as it's already included in the native format
func extractNativeOpenRouterMessages(messages []*Message, systemPrompt string) ([]openRouterChatMessage, error) {
	result := make([]openRouterChatMessage, 0, len(messages))

	// Extract native messages (system prompt is already included in native format)
	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			return nil, fmt.Errorf("message missing native format")
		}

		// Type assert to map[string]interface{} (OpenRouter uses OpenAI-compatible format)
		nativeMap, ok := msg.NativeFormat.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("native format is not a map")
		}

		// Marshal and unmarshal to convert to openRouterChatMessage
		data, err := json.Marshal(nativeMap)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var openRouterMsg openRouterChatMessage
		if err := json.Unmarshal(data, &openRouterMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to OpenRouter message: %w", err)
		}

		result = append(result, openRouterMsg)
	}

	return result, nil
}

func convertOpenRouterToolCalls(toolCalls []openRouterToolCall) []map[string]interface{} {
	if len(toolCalls) == 0 {
		logger.Debug("convertOpenRouterToolCalls: no tool calls to convert")
		return nil
	}

	logger.Debug("convertOpenRouterToolCalls: converting %d tool calls", len(toolCalls))

	result := make([]map[string]interface{}, 0, len(toolCalls))
	for i, tc := range toolCalls {
		if tc.Function == nil {
			logger.Debug("convertOpenRouterToolCalls: tool call %d has no function, skipping", i)
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
		logger.Debug("convertOpenRouterToolCalls: converted tool call %d with name=%s", i, tc.Function.Name)
	}

	logger.Debug("convertOpenRouterToolCalls: successfully converted %d tool calls", len(result))
	return result
}

// removeCallIDFromToolCalls removes the call_id field from tool calls
// This is needed for Mistral which rejects call_id and only accepts id
func removeCallIDFromToolCalls(toolCalls []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, len(toolCalls))
	for i, tc := range toolCalls {
		// Create a new map without call_id
		cleanCall := make(map[string]interface{})
		for k, v := range tc {
			if k != "call_id" {
				cleanCall[k] = v
			}
		}
		result[i] = cleanCall
	}
	return result
}

func extractOpenRouterText(content interface{}) string {
	logger.Debug("extractOpenRouterText: processing content of type %T", content)

	switch value := content.(type) {
	case nil:
		logger.Debug("extractOpenRouterText: content is nil")
		return ""
	case string:
		logger.Debug("extractOpenRouterText: extracted string content length=%d", len(value))
		return value
	case []interface{}:
		logger.Debug("extractOpenRouterText: processing array of %d parts", len(value))
		var sb strings.Builder
		for i, part := range value {
			partText := extractOpenRouterText(part)
			sb.WriteString(partText)
			logger.Debug("extractOpenRouterText: processed part %d, extracted %d chars", i, len(partText))
		}
		result := sb.String()
		logger.Debug("extractOpenRouterText: array processing complete, total length=%d", len(result))
		return result
	case map[string]interface{}:
		logger.Debug("extractOpenRouterText: processing map with %d keys", len(value))
		if text, ok := value["text"].(string); ok {
			logger.Debug("extractOpenRouterText: found text field, length=%d", len(text))
			return text
		}
		if inner, ok := value["content"]; ok {
			logger.Debug("extractOpenRouterText: found content field, recursing")
			return extractOpenRouterText(inner)
		}
		logger.Debug("extractOpenRouterText: map processing complete, no text found")
	case json.RawMessage:
		logger.Debug("extractOpenRouterText: processing json.RawMessage")
		var decoded interface{}
		if err := json.Unmarshal(value, &decoded); err == nil {
			logger.Debug("extractOpenRouterText: successfully decoded json.RawMessage")
			return extractOpenRouterText(decoded)
		} else {
			logger.Debug("extractOpenRouterText: failed to decode json.RawMessage: %v", err)
		}
	}
	logger.Debug("extractOpenRouterText: no content extracted, returning empty")
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
