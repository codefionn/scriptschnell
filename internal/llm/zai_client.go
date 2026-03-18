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

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/logger"
)

const zaiAPIBaseURL = "https://api.z.ai/api/paas/v4"

// ZaiClient implements the Client interface using the native Z.AI Chat Completions API.
// It supports the thinking parameter for chain-of-thought reasoning and streaming tool calls.
type ZaiClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewZaiClient constructs a Z.AI client for the specified model.
func NewZaiClient(apiKey, baseURL, modelID string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("zai client requires an API key")
	}

	model := strings.TrimSpace(modelID)
	if model == "" {
		model = "glm-5"
	}

	if strings.TrimSpace(baseURL) == "" {
		baseURL = zaiAPIBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &ZaiClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: consts.TimeoutClient,
		},
	}, nil
}

func (c *ZaiClient) GetModelName() string {
	return c.model
}

func (c *ZaiClient) GetLastResponseID() string {
	return ""
}

func (c *ZaiClient) SetPreviousResponseID(responseID string) {
}

func (c *ZaiClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *ZaiClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("zai completion request cannot be nil")
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
		return nil, fmt.Errorf("zai completion failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("zai completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp zaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("zai completion failed: %w", err)
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
		Reasoning:  first.Message.ReasoningContent,
		ToolCalls:  convertZaiToolCalls(first.Message.ToolCalls),
		StopReason: stopReason,
		Usage:      chatResp.Usage,
	}, nil
}

func (c *ZaiClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("zai completion request cannot be nil")
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
		return fmt.Errorf("zai stream failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zai stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buffer := make([]byte, 0, consts.BufferSize256KB)
	scanner.Buffer(buffer, consts.BufferSize1MB)

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

		var chunk zaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("zai stream failed to decode chunk: %w", err)
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
		return fmt.Errorf("zai stream failed: %w", err)
	}

	return nil
}

func (c *ZaiClient) buildChatRequest(req *CompletionRequest, stream bool) (*zaiChatRequest, error) {
	messages, err := convertMessagesToZai(req)
	if err != nil {
		return nil, err
	}

	payload := &zaiChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   stream,
		Thinking: &zaiThinking{
			Type:          "enabled",
			ClearThinking: false,
		},
	}

	if stream {
		payload.ToolStream = true
	}

	if req.ClearThinking != nil && *req.ClearThinking {
		payload.Thinking.ClearThinking = true
	}

	if req.Temperature != 0 {
		temp := req.Temperature
		payload.Temperature = &temp
	}

	if req.MaxTokens > 0 {
		payload.MaxTokens = req.MaxTokens
	}

	if req.TopP > 0 {
		payload.TopP = req.TopP
	}

	if len(req.Tools) > 0 {
		payload.Tools = req.Tools
		payload.ToolChoice = "auto"
	}

	return payload, nil
}

func (c *ZaiClient) newChatRequest(ctx context.Context, payload *zaiChatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("zai failed to encode payload: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("zai failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func convertMessagesToZai(req *CompletionRequest) ([]zaiChatMessage, error) {
	if req == nil {
		return nil, fmt.Errorf("zai completion request cannot be nil")
	}

	hasNativeFormat := len(req.Messages) > 0 && req.Messages[0].NativeFormat != nil && req.Messages[0].NativeProvider == "zai"

	if hasNativeFormat {
		logger.Debug("Using native Z.AI message format (%d messages)", len(req.Messages))
		messages, err := extractNativeZaiMessages(req.Messages, req.SystemPrompt)
		if err != nil {
			logger.Warn("Failed to extract native Z.AI messages, falling back to conversion: %v", err)
			return convertMessagesToZaiFromUnified(req)
		}
		return messages, nil
	}

	return convertMessagesToZaiFromUnified(req)
}

func convertMessagesToZaiFromUnified(req *CompletionRequest) ([]zaiChatMessage, error) {
	// Sanitize messages to fix structural issues from compaction races
	sanitized, repaired := SanitizeMessages(req.Messages)
	if repaired {
		logger.Info("zai: sanitized %d messages before conversion", len(sanitized))
		req.Messages = sanitized
	}

	messages := make([]zaiChatMessage, 0, len(req.Messages)+1)

	if system := strings.TrimSpace(req.SystemPrompt); system != "" {
		messages = append(messages, zaiChatMessage{
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

		zMsg := zaiChatMessage{
			Role:    role,
			Content: msg.Content,
		}

		if role == "assistant" && msg.Reasoning != "" {
			zMsg.ReasoningContent = msg.Reasoning
		}

		if len(msg.ToolCalls) > 0 {
			zMsg.ToolCalls = msg.ToolCalls
		}
		if msg.ToolID != "" {
			zMsg.ToolCallID = msg.ToolID
		}
		if msg.ToolName != "" {
			zMsg.Name = msg.ToolName
		}

		messages = append(messages, zMsg)
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("zai completion requires at least one message")
	}

	return messages, nil
}

func extractNativeZaiMessages(messages []*Message, _ string) ([]zaiChatMessage, error) {
	result := make([]zaiChatMessage, 0, len(messages))

	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			return nil, fmt.Errorf("message missing native format")
		}

		nativeMap, ok := msg.NativeFormat.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("native format is not a map")
		}

		data, err := json.Marshal(nativeMap)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var zaiMsg zaiChatMessage
		if err := json.Unmarshal(data, &zaiMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to Z.AI message: %w", err)
		}

		result = append(result, zaiMsg)
	}

	return result, nil
}

func convertZaiToolCalls(toolCalls []zaiToolCall) []map[string]any {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(toolCalls))
	for _, tc := range toolCalls {
		call := map[string]any{
			"type": tc.Type,
			"function": map[string]any{
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

// Request types

type zaiThinking struct {
	Type          string `json:"type"`
	ClearThinking bool   `json:"clear_thinking"`
}

type zaiChatRequest struct {
	Model       string                   `json:"model"`
	Messages    []zaiChatMessage         `json:"messages"`
	Temperature *float64                 `json:"temperature,omitempty"`
	TopP        float64                  `json:"top_p,omitempty"`
	MaxTokens   int                      `json:"max_tokens,omitempty"`
	Stream      bool                     `json:"stream"`
	Tools       []map[string]any `json:"tools,omitempty"`
	ToolChoice  any              `json:"tool_choice,omitempty"`
	ToolStream  bool                     `json:"tool_stream,omitempty"`
	Thinking    *zaiThinking             `json:"thinking,omitempty"`
}

type zaiChatMessage struct {
	Role             string                   `json:"role"`
	Content          string                   `json:"content"`
	ReasoningContent string                   `json:"reasoning_content,omitempty"`
	Name             string                   `json:"name,omitempty"`
	ToolCalls        []map[string]any `json:"tool_calls,omitempty"`
	ToolCallID       string                   `json:"tool_call_id,omitempty"`
}

// Response types

type zaiChatResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Model   string                 `json:"model"`
	Created int64                  `json:"created"`
	Choices []zaiChatChoice        `json:"choices"`
	Usage   map[string]any `json:"usage,omitempty"`
}

type zaiChatChoice struct {
	Index        int                    `json:"index"`
	FinishReason string                 `json:"finish_reason"`
	Message      *zaiChatChoiceMessage  `json:"message"`
}

type zaiChatChoiceMessage struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []zaiToolCall `json:"tool_calls,omitempty"`
}

type zaiToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function zaiToolCallFunction `json:"function"`
}

type zaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Streaming types

type zaiStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Model   string             `json:"model"`
	Created int64              `json:"created"`
	Choices []zaiStreamChoice  `json:"choices"`
}

type zaiStreamChoice struct {
	Index        int            `json:"index"`
	Delta        zaiStreamDelta `json:"delta"`
	FinishReason string         `json:"finish_reason"`
}

type zaiStreamDelta struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []zaiToolCall `json:"tool_calls,omitempty"`
}
