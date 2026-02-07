package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/logger"
)

const (
	mistralDefaultBaseURL = "https://api.mistral.ai/v1"
)

// MistralClient implements the Client interface using the native Mistral API.
type MistralClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewMistralClient creates a new client for the Mistral chat completion API.
func NewMistralClient(apiKey, modelName string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("mistral client requires an API key")
	}

	model := strings.TrimSpace(modelName)
	if model == "" {
		model = "mistral-large-latest"
	}

	return &MistralClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: mistralDefaultBaseURL,
		httpClient: &http.Client{
			Timeout: consts.Timeout2Minutes,
		},
	}, nil
}

func (c *MistralClient) GetModelName() string {
	return c.model
}

func (c *MistralClient) GetLastResponseID() string {
	return "" // Not applicable for Mistral
}

func (c *MistralClient) SetPreviousResponseID(responseID string) {
	// Not applicable for Mistral
}

func (c *MistralClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *MistralClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("mistral completion request cannot be nil")
	}

	normalized := normalizeMistralConversation(req.Messages)
	local := *req
	local.Messages = normalized

	payload, err := c.buildChatRequest(&local, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := c.newChatRequest(ctx, payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mistral completion failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mistral completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp mistralChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("mistral completion failed: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return &CompletionResponse{StopReason: "stop"}, nil
	}

	first := chatResp.Choices[0]
	var content string
	if first.Message != nil {
		content = mistralExtractContent(first.Message.Content)
	}

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  mistralConvertToolCallsToGeneric(first.Message),
		StopReason: first.FinishReason,
	}, nil
}

func (c *MistralClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("mistral completion request cannot be nil")
	}

	normalized := normalizeMistralConversation(req.Messages)
	local := *req
	local.Messages = normalized

	payload, err := c.buildChatRequest(&local, true)
	if err != nil {
		return err
	}

	httpReq, err := c.newChatRequest(ctx, payload)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("mistral stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mistral stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buffer := make([]byte, 0, consts.BufferSize256KB)
	scanner.Buffer(buffer, consts.BufferSize1MB)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if chunk == "" || chunk == "[DONE]" {
			if chunk == "[DONE]" {
				break
			}
			continue
		}

		var event mistralStreamEvent
		if err := json.Unmarshal([]byte(chunk), &event); err != nil {
			return fmt.Errorf("mistral stream failed to decode chunk: %w", err)
		}

		for _, choice := range event.Choices {
			text := mistralExtractContent(choice.Delta.Content)
			if strings.TrimSpace(text) == "" {
				continue
			}

			if err := callback(text); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("mistral stream failed: %w", err)
	}

	return nil
}

func (c *MistralClient) buildChatRequest(req *CompletionRequest, stream bool) (*mistralChatRequest, error) {
	// Check if we can use native Mistral format
	hasNativeFormat := len(req.Messages) > 0 && req.Messages[0].NativeFormat != nil && req.Messages[0].NativeProvider == "mistral"

	var messages []mistralChatMessage
	var err error

	if hasNativeFormat {
		logger.Debug("Using native Mistral message format (%d messages)", len(req.Messages))
		messages, err = extractNativeMistralMessages(req.Messages, req.SystemPrompt)
		if err != nil {
			logger.Warn("Failed to extract native Mistral messages, falling back to conversion: %v", err)
			messages = c.convertMessages(req)
		}
	} else {
		messages = c.convertMessages(req)
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("mistral completion requires at least one message")
	}

	payload := &mistralChatRequest{
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
		payload.Tools = convertMistralTools(req.Tools)
		payload.ParallelToolCall = true
	}

	return payload, nil
}

func (c *MistralClient) newChatRequest(ctx context.Context, payload *mistralChatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode mistral payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create mistral request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	return req, nil
}

func (c *MistralClient) convertMessages(req *CompletionRequest) []mistralChatMessage {
	messages := make([]mistralChatMessage, 0, len(req.Messages)+1)

	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, mistralChatMessage{
			Role:    "system",
			Content: strings.TrimSpace(req.SystemPrompt),
		})
	}

	for _, msg := range req.Messages {
		if msg == nil {
			continue
		}

		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "user"
		}

		apiMsg := mistralChatMessage{
			Role: role,
		}

		switch role {
		case "assistant":
			apiMsg.Content = msg.Content
			if len(msg.ToolCalls) > 0 {
				apiMsg.ToolCalls = convertMistralToolCalls(msg.ToolCalls)
			}
			// Mistral API requires assistant messages to have either content or tool_calls
			// Skip assistant messages that have neither
			if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
				continue
			}
		case "tool":
			apiMsg.ToolCallID = msg.ToolID
			apiMsg.Name = sanitizeMistralToolName(msg.ToolName)
			apiMsg.Content = msg.Content
		default:
			apiMsg.Content = msg.Content
		}

		messages = append(messages, apiMsg)
	}

	return messages
}

// extractNativeMistralMessages extracts native Mistral messages from unified messages
// Note: System prompt should NOT be added here as it's already included in the native format
func extractNativeMistralMessages(messages []*Message, systemPrompt string) ([]mistralChatMessage, error) {
	result := make([]mistralChatMessage, 0, len(messages))

	// Extract native messages (system prompt is already included in native format)
	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			return nil, fmt.Errorf("message missing native format")
		}

		// Type assert to map[string]interface{} (Mistral uses OpenAI-compatible format)
		nativeMap, ok := msg.NativeFormat.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("native format is not a map")
		}

		// Marshal and unmarshal to convert to mistralChatMessage
		data, err := json.Marshal(nativeMap)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var mistralMsg mistralChatMessage
		if err := json.Unmarshal(data, &mistralMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to Mistral message: %w", err)
		}

		result = append(result, mistralMsg)
	}

	return result, nil
}

type mistralChatRequest struct {
	Model            string               `json:"model"`
	Messages         []mistralChatMessage `json:"messages"`
	Temperature      *float64             `json:"temperature,omitempty"`
	MaxTokens        int                  `json:"max_tokens,omitempty"`
	Tools            []mistralTool        `json:"tools,omitempty"`
	ParallelToolCall bool                 `json:"parallel_tool_calls,omitempty"`
	Stream           bool                 `json:"stream,omitempty"`
}

type mistralChatMessage struct {
	Role       string            `json:"role"`
	Content    interface{}       `json:"content,omitempty"`
	Name       string            `json:"name,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []mistralToolCall `json:"tool_calls,omitempty"`
}

type mistralTool struct {
	Type     string                    `json:"type"`
	Function *mistralFunctionSignature `json:"function,omitempty"`
}

type mistralFunctionSignature struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type mistralToolCall struct {
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function *mistralFunctionCall `json:"function,omitempty"`
}

type mistralFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type mistralChatResponse struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []mistralChatChoice  `json:"choices"`
	Usage   *mistralUsageMetrics `json:"usage,omitempty"`
}

type mistralChatChoice struct {
	Index        int                         `json:"index"`
	FinishReason string                      `json:"finish_reason"`
	Message      *mistralChatResponseMessage `json:"message"`
}

type mistralChatResponseMessage struct {
	Role      string            `json:"role"`
	Content   interface{}       `json:"content,omitempty"`
	ToolCalls []mistralToolCall `json:"tool_calls,omitempty"`
}

type mistralUsageMetrics struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type mistralStreamEvent struct {
	Choices []mistralStreamChoice `json:"choices"`
}

type mistralStreamChoice struct {
	Delta struct {
		Content   interface{}       `json:"content,omitempty"`
		ToolCalls []mistralToolCall `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

func convertMistralTools(tools []map[string]interface{}) []mistralTool {
	result := make([]mistralTool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}

		toolType, _ := t["type"].(string)
		if toolType == "" {
			toolType = "function"
		}

		tool := mistralTool{Type: toolType}
		if fn, ok := t["function"].(map[string]interface{}); ok {
			mFn := &mistralFunctionSignature{}
			if name, _ := fn["name"].(string); name != "" {
				mFn.Name = name
			}
			if desc, _ := fn["description"].(string); desc != "" {
				mFn.Description = desc
			}
			if params, _ := fn["parameters"].(map[string]interface{}); params != nil {
				mFn.Parameters = params
			}
			tool.Function = mFn
		}
		result = append(result, tool)
	}
	return result
}

func convertMistralToolCalls(toolCalls []map[string]interface{}) []mistralToolCall {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]mistralToolCall, 0, len(toolCalls))
	for _, call := range toolCalls {
		if call == nil {
			continue
		}

		fnData, _ := call["function"].(map[string]interface{})
		name, _ := fnData["name"].(string)
		name = sanitizeMistralToolName(name)
		args := stringifyArguments(fnData["arguments"])

		callType := toString(call["type"])
		if callType == "" {
			callType = "function"
		}

		// Normalize the tool call ID to meet Mistral's requirements
		originalID := toString(call["id"])
		normalizedID := normalizeMistralToolCallID(originalID)

		result = append(result, mistralToolCall{
			ID:   normalizedID,
			Type: callType,
			Function: &mistralFunctionCall{
				Name:      name,
				Arguments: args,
			},
		})
	}

	return result
}

// normalizeMistralToolCallID ensures tool call IDs meet Mistral's requirements:
// - Alphanumeric only
// - Maximum 9 characters
// Uses xxhash for fast hashing of long IDs
func normalizeMistralToolCallID(id string) string {
	if id == "" {
		// Generate a random-looking ID from current timestamp hash
		id = "call_null"
	}

	// Remove non-alphanumeric characters
	var alphanumeric strings.Builder
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			alphanumeric.WriteRune(ch)
		}
	}
	cleaned := alphanumeric.String()

	// If the cleaned ID is empty or too long, hash it
	if cleaned == "" || len(cleaned) > 9 {
		// Use xxhash for fast hashing
		hash := xxhash.Sum64String(id)
		// Convert to base36 (0-9, a-z) for alphanumeric output
		hashStr := strconv.FormatUint(hash, 36)
		// Take first 9 characters
		if len(hashStr) > 9 {
			return hashStr[:9]
		}
		return hashStr
	}

	return cleaned
}

func mistralConvertToolCallsToGeneric(message *mistralChatResponseMessage) []map[string]interface{} {
	if message == nil || len(message.ToolCalls) == 0 {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		if call.Function == nil {
			continue
		}
		callType := call.Type
		if strings.TrimSpace(callType) == "" {
			callType = "function"
		}
		result = append(result, map[string]interface{}{
			"id":   call.ID,
			"type": callType,
			"function": map[string]interface{}{
				"name":      call.Function.Name,
				"arguments": call.Function.Arguments,
			},
		})
	}
	return result
}

func mistralExtractContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var builder strings.Builder
		for _, part := range v {
			switch pv := part.(type) {
			case string:
				if builder.Len() > 0 {
					builder.WriteString("")
				}
				builder.WriteString(pv)
			case map[string]interface{}:
				if text, ok := pv["text"].(string); ok {
					builder.WriteString(text)
				} else if data, ok := pv["content"].(string); ok {
					builder.WriteString(data)
				}
			}
		}
		return builder.String()
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if data, ok := v["content"].(string); ok {
			return data
		}
	}
	return ""
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(bytes)
	}
}

func sanitizeMistralToolName(name string) string {
	// Mistral requires tool names to match ^[a-zA-Z0-9_-]+$
	// Sometimes the model hallucinates and puts JSON args in the name (e.g. {"action":...}todo)
	if idx := strings.LastIndex(name, "}"); idx != -1 && idx < len(name)-1 {
		suffix := strings.TrimSpace(name[idx+1:])
		if suffix != "" {
			name = suffix
		}
	}

	// Remove invalid characters
	var builder strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			builder.WriteRune(r)
		}
	}
	cleaned := builder.String()
	if cleaned == "" {
		return "unknown_tool"
	}
	return cleaned
}
