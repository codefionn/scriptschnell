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

const cerebrasAPIBaseURL = "https://api.cerebras.ai/v1"

// CerebrasClient implements the Client interface using the native Cerebras Chat Completions API.
type CerebrasClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewCerebrasClient constructs a Cerebras client for the specified model.
func NewCerebrasClient(apiKey, modelID string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("cerebras client requires an API key")
	}

	model := strings.TrimSpace(modelID)
	if model == "" {
		model = "llama-3.3-70b"
	}

	return &CerebrasClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: cerebrasAPIBaseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

func (c *CerebrasClient) GetModelName() string {
	return c.model
}

func (c *CerebrasClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *CerebrasClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("cerebras completion request cannot be nil")
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
		return nil, fmt.Errorf("cerebras completion failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cerebras completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp cerebrasChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("cerebras completion failed: %w", err)
	}

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message == nil {
		return &CompletionResponse{StopReason: "stop"}, nil
	}

	first := chatResp.Choices[0]
	stopReason := first.FinishReason
	if strings.TrimSpace(stopReason) == "" {
		stopReason = "stop"
	}

	return &CompletionResponse{
		Content:    first.Message.Content,
		ToolCalls:  convertCerebrasToolCalls(first.Message.ToolCalls),
		StopReason: stopReason,
	}, nil
}

func (c *CerebrasClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("cerebras completion request cannot be nil")
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
		return fmt.Errorf("cerebras stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cerebras stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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

		var chunk cerebrasStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("cerebras stream failed to decode chunk: %w", err)
		}

		for _, choice := range chunk.Choices {
			text := choice.Delta.Content
			if strings.TrimSpace(text) == "" {
				continue
			}
			if err := callback(text); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("cerebras stream failed: %w", err)
	}

	return nil
}

func (c *CerebrasClient) buildChatRequest(req *CompletionRequest, stream bool) (*cerebrasChatRequest, error) {
	messages, err := convertMessagesToCerebras(req)
	if err != nil {
		return nil, err
	}

	payload := &cerebrasChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   stream,
	}

	if req.Temperature != 0 {
		temp := req.Temperature
		payload.Temperature = &temp
	}

	if req.MaxTokens > 0 {
		payload.MaxCompletionTokens = req.MaxTokens
	}

	if len(req.Tools) > 0 {
		payload.Tools = req.Tools
		payload.ToolChoice = "auto"
	}

	return payload, nil
}

func (c *CerebrasClient) newChatRequest(ctx context.Context, payload *cerebrasChatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cerebras completion failed to encode payload: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cerebras completion failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func convertMessagesToCerebras(req *CompletionRequest) ([]cerebrasChatMessage, error) {
	if req == nil {
		return nil, fmt.Errorf("cerebras completion request cannot be nil")
	}

	// Check if we can use native Cerebras format
	hasNativeFormat := len(req.Messages) > 0 && req.Messages[0].NativeFormat != nil && req.Messages[0].NativeProvider == "cerebras"

	if hasNativeFormat {
		logger.Debug("Using native Cerebras message format (%d messages)", len(req.Messages))
		messages, err := extractNativeCerebrasMessages(req.Messages, req.SystemPrompt)
		if err != nil {
			logger.Warn("Failed to extract native Cerebras messages, falling back to conversion: %v", err)
			return convertMessagesToCerebrasFromUnified(req)
		}
		return messages, nil
	}

	return convertMessagesToCerebrasFromUnified(req)
}

func convertMessagesToCerebrasFromUnified(req *CompletionRequest) ([]cerebrasChatMessage, error) {
	messages := make([]cerebrasChatMessage, 0, len(req.Messages)+1)

	if system := strings.TrimSpace(req.SystemPrompt); system != "" {
		messages = append(messages, cerebrasChatMessage{
			Role:    "system",
			Content: system,
		})
	}

	for _, msg := range req.Messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}

		cMsg := cerebrasChatMessage{
			Role:    role,
			Content: msg.Content,
		}

		if len(msg.ToolCalls) > 0 {
			cMsg.ToolCalls = msg.ToolCalls
		}
		if msg.ToolID != "" {
			cMsg.ToolCallID = msg.ToolID
		}
		if msg.ToolName != "" {
			cMsg.Name = msg.ToolName
		}

		messages = append(messages, cMsg)
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("cerebras completion requires at least one message")
	}

	return messages, nil
}

// extractNativeCerebrasMessages extracts native Cerebras messages from unified messages
// Note: System prompt should NOT be added here as it's already included in the native format
func extractNativeCerebrasMessages(messages []*Message, systemPrompt string) ([]cerebrasChatMessage, error) {
	result := make([]cerebrasChatMessage, 0, len(messages))

	// Extract native messages (system prompt is already included in native format)
	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			return nil, fmt.Errorf("message missing native format")
		}

		// Type assert to map[string]interface{} (Cerebras uses OpenAI-compatible format)
		nativeMap, ok := msg.NativeFormat.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("native format is not a map")
		}

		// Marshal and unmarshal to convert to cerebrasChatMessage
		data, err := json.Marshal(nativeMap)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var cerebrasMsg cerebrasChatMessage
		if err := json.Unmarshal(data, &cerebrasMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to Cerebras message: %w", err)
		}

		result = append(result, cerebrasMsg)
	}

	return result, nil
}

func convertCerebrasToolCalls(toolCalls []cerebrasToolCall) []map[string]interface{} {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
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

type cerebrasChatRequest struct {
	Model               string                   `json:"model"`
	Messages            []cerebrasChatMessage    `json:"messages"`
	Temperature         *float64                 `json:"temperature,omitempty"`
	MaxCompletionTokens int                      `json:"max_completion_tokens,omitempty"`
	Stream              bool                     `json:"stream"`
	Tools               []map[string]interface{} `json:"tools,omitempty"`
	ToolChoice          interface{}              `json:"tool_choice,omitempty"`
	ResponseFormat      map[string]interface{}   `json:"response_format,omitempty"`
	TopP                float64                  `json:"top_p,omitempty"`
	Stop                []string                 `json:"stop,omitempty"`
	User                string                   `json:"user,omitempty"`
	LogProbs            *bool                    `json:"logprobs,omitempty"`
	TopLogProbs         *int                     `json:"top_logprobs,omitempty"`
}

type cerebrasChatMessage struct {
	Role       string                   `json:"role"`
	Content    string                   `json:"content"`
	Name       string                   `json:"name,omitempty"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
}

type cerebrasChatResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Choices []cerebrasChatChoice   `json:"choices"`
	Usage   map[string]interface{} `json:"usage,omitempty"`
}

type cerebrasChatChoice struct {
	Index        int                        `json:"index"`
	FinishReason string                     `json:"finish_reason"`
	Message      *cerebrasChatChoiceMessage `json:"message"`
}

type cerebrasChatChoiceMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	Reasoning string             `json:"reasoning,omitempty"`
	ToolCalls []cerebrasToolCall `json:"tool_calls,omitempty"`
}

type cerebrasToolCall struct {
	ID       string                   `json:"id"`
	Type     string                   `json:"type"`
	Function cerebrasToolCallFunction `json:"function"`
}

type cerebrasToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type cerebrasStreamChunk struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Choices []cerebrasStreamChoice `json:"choices"`
}

type cerebrasStreamChoice struct {
	Index        int                 `json:"index"`
	Delta        cerebrasStreamDelta `json:"delta"`
	FinishReason string              `json:"finish_reason"`
}

type cerebrasStreamDelta struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []cerebrasToolCall `json:"tool_calls,omitempty"`
}
