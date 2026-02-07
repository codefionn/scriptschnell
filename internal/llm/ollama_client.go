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

// OllamaClient implements the Client interface for the Ollama REST API.
type OllamaClient struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaClient creates a new Ollama client for the provided model.
func NewOllamaClient(baseURL, model string) (Client, error) {
	normalized := normalizeOllamaBaseURL(baseURL)
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("ollama client requires a model identifier")
	}

	return &OllamaClient{
		baseURL: normalized,
		model:   model,
		client: &http.Client{
			Timeout: consts.Timeout2Minutes,
		},
	}, nil
}

func (c *OllamaClient) GetModelName() string {
	return c.model
}

func (c *OllamaClient) GetLastResponseID() string {
	return "" // Not applicable for Ollama
}

func (c *OllamaClient) SetPreviousResponseID(responseID string) {
	// Not applicable for Ollama
}

func (c *OllamaClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *OllamaClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("completion request cannot be nil")
	}

	payload, err := c.buildChatRequest(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := c.newChatRequest(ctx, payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama completion failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("ollama completion failed: %w", err)
	}

	content := ""
	var toolCalls []map[string]interface{}
	if chatResp.Message != nil {
		content = chatResp.Message.Content
		if len(chatResp.Message.ToolCalls) > 0 {
			toolCalls = chatResp.Message.ToolCalls
		}
	}

	stopReason := strings.TrimSpace(chatResp.DoneReason)
	if stopReason == "" && chatResp.Done {
		stopReason = "stop"
	}

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  toolCalls,
		StopReason: stopReason,
	}, nil
}

func (c *OllamaClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("completion request cannot be nil")
	}

	payload, err := c.buildChatRequest(req, true)
	if err != nil {
		return err
	}

	httpReq, err := c.newChatRequest(ctx, payload)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buffer := make([]byte, 0, consts.BufferSize256KB)
	scanner.Buffer(buffer, consts.BufferSize1MB)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event ollamaChatStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("ollama stream failed to decode chunk: %w", err)
		}

		if event.Message != nil && strings.TrimSpace(event.Message.Content) != "" {
			if err := callback(event.Message.Content); err != nil {
				return err
			}
		}

		if event.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ollama stream failed: %w", err)
	}

	return nil
}

func (c *OllamaClient) buildChatRequest(req *CompletionRequest, stream bool) (*ollamaChatRequest, error) {
	// Check if we can use native Ollama format
	hasNativeFormat := len(req.Messages) > 0 && req.Messages[0].NativeFormat != nil && req.Messages[0].NativeProvider == "ollama"

	var messages []ollamaChatMessage
	var systemPrompt string
	var err error

	if hasNativeFormat {
		logger.Debug("Using native Ollama message format (%d messages)", len(req.Messages))
		messages, systemPrompt, err = extractNativeOllamaMessages(req.Messages, req.SystemPrompt)
		if err != nil {
			logger.Warn("Failed to extract native Ollama messages, falling back to conversion: %v", err)
			messages, systemPrompt = convertMessagesToOllamaFromUnified(req)
		}
	} else {
		messages, systemPrompt = convertMessagesToOllamaFromUnified(req)
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("ollama completion requires at least one message")
	}

	options := make(map[string]interface{})
	if req.Temperature != 0 {
		options["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		options["num_predict"] = req.MaxTokens
	}
	if len(options) == 0 {
		options = nil
	}

	return &ollamaChatRequest{
		Model:    c.model,
		Stream:   stream,
		System:   systemPrompt,
		Messages: messages,
		Tools:    req.Tools,
		Options:  options,
	}, nil
}

func convertMessagesToOllamaFromUnified(req *CompletionRequest) ([]ollamaChatMessage, string) {
	messages := make([]ollamaChatMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}

		oMsg := ollamaChatMessage{
			Role:    role,
			Content: msg.Content,
		}

		if len(msg.ToolCalls) > 0 {
			oMsg.ToolCalls = msg.ToolCalls
		}
		if msg.ToolID != "" {
			oMsg.ToolCallID = msg.ToolID
		}
		if msg.ToolName != "" {
			oMsg.Name = msg.ToolName
		}

		messages = append(messages, oMsg)
	}

	return messages, req.SystemPrompt
}

// extractNativeOllamaMessages extracts native Ollama messages and system prompt
// Note: Ollama stores system prompt separately as metadata, not as a message
func extractNativeOllamaMessages(messages []*Message, fallbackSystemPrompt string) ([]ollamaChatMessage, string, error) {
	result := make([]ollamaChatMessage, 0, len(messages))
	systemPrompt := ""

	// Extract native messages and system prompt from metadata
	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			return nil, "", fmt.Errorf("message missing native format")
		}

		// Type assert to map[string]interface{}
		nativeMap, ok := msg.NativeFormat.(map[string]interface{})
		if !ok {
			return nil, "", fmt.Errorf("native format is not a map")
		}

		// Check for system metadata
		if sys, isSystem := nativeMap["_ollama_system"]; isSystem {
			if sysStr, ok := sys.(string); ok {
				systemPrompt = sysStr
			}
			continue
		}

		// Marshal and unmarshal to convert to ollamaChatMessage
		data, err := json.Marshal(nativeMap)
		if err != nil {
			return nil, "", fmt.Errorf("failed to marshal native message: %w", err)
		}

		var ollamaMsg ollamaChatMessage
		if err := json.Unmarshal(data, &ollamaMsg); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal to Ollama message: %w", err)
		}

		result = append(result, ollamaMsg)
	}

	// Use fallback if no system prompt in native format
	if systemPrompt == "" {
		systemPrompt = fallbackSystemPrompt
	}

	return result, systemPrompt, nil
}

func (c *OllamaClient) newChatRequest(ctx context.Context, payload *ollamaChatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ollama request: %w", err)
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

type ollamaChatRequest struct {
	Model    string                   `json:"model"`
	Messages []ollamaChatMessage      `json:"messages"`
	Tools    []map[string]interface{} `json:"tools,omitempty"`
	Stream   bool                     `json:"stream"`
	System   string                   `json:"system,omitempty"`
	Options  map[string]interface{}   `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role       string                   `json:"role"`
	Content    string                   `json:"content"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
	Name       string                   `json:"name,omitempty"`
}

type ollamaChatResponse struct {
	Model      string             `json:"model"`
	CreatedAt  string             `json:"created_at"`
	Message    *ollamaChatMessage `json:"message"`
	Done       bool               `json:"done"`
	DoneReason string             `json:"done_reason"`
}

type ollamaChatStreamEvent struct {
	Model      string             `json:"model"`
	CreatedAt  string             `json:"created_at"`
	Message    *ollamaChatMessage `json:"message"`
	Done       bool               `json:"done"`
	DoneReason string             `json:"done_reason"`
}

func normalizeOllamaBaseURL(baseURL string) string {
	url := strings.TrimSpace(baseURL)
	if url == "" {
		return "http://localhost:11434"
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	return strings.TrimRight(url, "/")
}
