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
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

const openAIDefaultBaseURL = "https://api.openai.com/v1"

// OpenAIClient implements the Client interface using OpenAI's native APIs.
type OpenAIClient struct {
	apiKey          string
	model           string
	baseURL         string
	httpClient      *http.Client
	useResponses    bool
	responsesClient *openai.Client
}

// NewOpenAIClient constructs a client that talks directly to the OpenAI API.
func NewOpenAIClient(apiKey, modelName string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("openai client requires an API key")
	}

	model := strings.TrimSpace(modelName)
	if model == "" {
		model = "gpt-5.1-codex"
	}

	client := &OpenAIClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: openAIDefaultBaseURL,
		httpClient: &http.Client{
			Timeout: consts.Timeout2Minutes,
		},
	}

	if requiresResponsesAPI(model) {
		apiClient := openai.NewClient(option.WithAPIKey(apiKey))
		client.useResponses = true
		client.responsesClient = &apiClient
	}

	return client, nil
}

func (c *OpenAIClient) GetModelName() string {
	return c.model
}

func (c *OpenAIClient) GetLastResponseID() string {
	return "" // Not applicable for OpenAI
}

func (c *OpenAIClient) SetPreviousResponseID(responseID string) {
	// Not applicable for OpenAI
}

func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *OpenAIClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("openai completion request cannot be nil")
	}

	if c.useResponses {
		return c.completeWithResponses(ctx, req)
	}
	return c.completeWithChat(ctx, req)
}

func (c *OpenAIClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("openai completion request cannot be nil")
	}

	if c.useResponses {
		return c.streamWithResponses(ctx, req, callback)
	}
	return c.streamWithChat(ctx, req, callback)
}

func (c *OpenAIClient) completeWithChat(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
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
		return nil, fmt.Errorf("openai completion failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("openai completion failed: %w", err)
	}

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message == nil {
		return &CompletionResponse{StopReason: "stop"}, nil
	}

	first := chatResp.Choices[0]
	content := extractOpenAIText(first.Message.Content)
	stopReason := first.FinishReason
	if strings.TrimSpace(stopReason) == "" {
		stopReason = "stop"
	}

	// Extract reasoning from content if it's structured (for models that embed reasoning)
	// Standard OpenAI chat completion doesn't separate reasoning, but we can check
	reasoning := extractOpenAIMessageReasoning(first.Message)

	return &CompletionResponse{
		Content:    content,
		Reasoning:  reasoning,
		ToolCalls:  convertOpenAIToolCalls(first.Message.ToolCalls),
		StopReason: stopReason,
		Usage:      chatResp.Usage,
	}, nil
}

func (c *OpenAIClient) streamWithChat(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
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
		return fmt.Errorf("openai stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("openai stream failed to decode chunk: %w", err)
		}

		for _, choice := range chunk.Choices {
			if choice.Delta == nil {
				continue
			}

			text := extractOpenAIText(choice.Delta.Content)
			if strings.TrimSpace(text) == "" {
				continue
			}
			if err := callback(text); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openai stream failed: %w", err)
	}

	return nil
}

func (c *OpenAIClient) buildChatRequest(req *CompletionRequest, stream bool) (*openAIChatRequest, error) {
	payload, err := convertRequestToOpenAI(req, c.model, stream, true)
	if err != nil {
		return nil, err
	}

	// override temperature handling for models that don't support it
	if req.Temperature != 0 && !isOpenAITemperatureUnsupported(c.model) {
		temp := req.Temperature
		payload.Temperature = &temp
	} else if isOpenAITemperatureUnsupported(c.model) {
		one := 1.0
		payload.Temperature = &one
	}

	return payload, nil
}

func (c *OpenAIClient) newChatRequest(ctx context.Context, payload *openAIChatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai failed to encode payload: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func (c *OpenAIClient) completeWithResponses(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if c.responsesClient == nil {
		return nil, fmt.Errorf("openai responses client not configured")
	}

	params, err := c.buildResponsesParams(req)
	if err != nil {
		return nil, err
	}

	resp, err := performResponsesCompletion(ctx, c.responsesClient, params)
	if err != nil {
		return nil, fmt.Errorf("openai completion failed: %w", err)
	}

	return resp, nil
}

func (c *OpenAIClient) streamWithResponses(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if c.responsesClient == nil {
		return fmt.Errorf("openai responses client not configured")
	}

	params, err := c.buildResponsesParams(req)
	if err != nil {
		return err
	}

	if err := performResponsesStream(ctx, c.responsesClient, params, callback); err != nil {
		return fmt.Errorf("openai completion failed: %w", err)
	}
	return nil
}

func (c *OpenAIClient) buildResponsesParams(req *CompletionRequest) (responses.ResponseNewParams, error) {
	inputItems, err := buildResponsesInput(req.Messages)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}

	if len(inputItems) == 0 {
		return responses.ResponseNewParams{}, fmt.Errorf("no messages provided")
	}

	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(c.model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: inputItems,
		},
	}

	if req.SystemPrompt != "" {
		params.Instructions = openai.String(req.SystemPrompt)
	}

	if req.Temperature != 0 && !isOpenAITemperatureUnsupported(c.model) {
		params.Temperature = openai.Float(req.Temperature)
	}

	if req.MaxTokens > 0 {
		params.MaxOutputTokens = openai.Int(int64(req.MaxTokens))
	}

	if len(req.Tools) > 0 {
		params.Tools = convertResponsesTools(req.Tools)
	}

	return params, nil
}

func convertMessagesToOpenAI(req *CompletionRequest, model string) ([]openAIChatMessage, error) {
	if req == nil {
		return nil, fmt.Errorf("openai completion request cannot be nil")
	}

	includeReasoning := shouldIncludeOpenAIReasoningMessages(model)

	// Check if messages have native OpenAI format - if so, use directly for caching
	hasNativeFormat := !includeReasoning && len(req.Messages) > 0 && req.Messages[0].NativeFormat != nil && req.Messages[0].NativeProvider == "openai"

	if hasNativeFormat {
		// Use native format directly to preserve caching metadata
		messages, err := extractNativeOpenAIMessages(req.Messages, req.SystemPrompt, req.EnableCaching)
		if err != nil {
			// Fall back to conversion
			return convertMessagesToOpenAIFromUnified(req, includeReasoning)
		}
		return messages, nil
	}

	// Convert from unified format
	return convertMessagesToOpenAIFromUnified(req, includeReasoning)
}

func convertMessagesToOpenAIFromUnified(req *CompletionRequest, includeReasoning bool) ([]openAIChatMessage, error) {
	messages := make([]openAIChatMessage, 0, len(req.Messages)+1)

	if system := strings.TrimSpace(req.SystemPrompt); system != "" {
		sysMsg := openAIChatMessage{
			Role:    "system",
			Content: system,
		}
		// Enable caching for system prompt if requested
		if req.EnableCaching {
			sysMsg.CacheControl = map[string]interface{}{"type": "ephemeral"}
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

		oMsg := openAIChatMessage{
			Role:    role,
			Content: msg.Content,
		}

		if includeReasoning && msg.Reasoning != "" {
			oMsg.Reasoning = msg.Reasoning
		}

		if includeReasoning && role == "assistant" && msg.Reasoning != "" {
			reasoningContent := msg.Reasoning
			oMsg.ReasoningContent = &reasoningContent
		}

		if msg.ToolName != "" {
			oMsg.Name = msg.ToolName
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			oMsg.ToolCalls = msg.ToolCalls
		}

		if role == "tool" && msg.ToolID != "" {
			oMsg.ToolCallID = msg.ToolID
		}

		messages = append(messages, oMsg)
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("openai completion requires at least one message")
	}

	return messages, nil
}

// extractNativeOpenAIMessages extracts native OpenAI messages from Message objects
func extractNativeOpenAIMessages(messages []*Message, systemPrompt string, enableCaching bool) ([]openAIChatMessage, error) {
	result := make([]openAIChatMessage, 0, len(messages)+1)

	// Add system message if provided and not already in native messages
	hasSystemMessage := false
	for _, msg := range messages {
		if msg != nil && msg.NativeFormat != nil {
			if m, ok := msg.NativeFormat.(map[string]interface{}); ok {
				if role, ok := m["role"].(string); ok && role == "system" {
					hasSystemMessage = true
					break
				}
			}
		}
	}

	if !hasSystemMessage && strings.TrimSpace(systemPrompt) != "" {
		sysMsg := openAIChatMessage{
			Role:    "system",
			Content: systemPrompt,
		}
		if enableCaching {
			sysMsg.CacheControl = map[string]interface{}{"type": "ephemeral"}
		}
		result = append(result, sysMsg)
	}

	// Extract native messages
	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			continue
		}

		data, err := json.Marshal(msg.NativeFormat)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal native message: %w", err)
		}

		var oaiMsg openAIChatMessage
		if err := json.Unmarshal(data, &oaiMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to openAIChatMessage: %w", err)
		}

		result = append(result, oaiMsg)
	}

	return result, nil
}

func convertOpenAIToolCalls(toolCalls []map[string]interface{}) []map[string]interface{} {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if tc == nil {
			continue
		}

		copyMap := make(map[string]interface{}, len(tc))
		for k, v := range tc {
			if k == "function" {
				fnMap, _ := v.(map[string]interface{})
				if fnMap == nil {
					continue
				}

				fnCopy := make(map[string]interface{}, len(fnMap))
				for fk, fv := range fnMap {
					if fk == "arguments" {
						fnCopy[fk] = stringifyArguments(fv)
					} else {
						fnCopy[fk] = fv
					}
				}
				copyMap[k] = fnCopy
				continue
			}

			copyMap[k] = v
		}

		result = append(result, copyMap)
	}

	return result
}

func extractOpenAIText(content interface{}) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []interface{}:
		var sb strings.Builder
		for _, part := range value {
			sb.WriteString(extractOpenAIText(part))
		}
		return sb.String()
	case map[string]interface{}:
		if text, ok := value["text"].(string); ok {
			return text
		}
		if inner, ok := value["content"]; ok {
			return extractOpenAIText(inner)
		}
	case json.RawMessage:
		var decoded interface{}
		if err := json.Unmarshal(value, &decoded); err == nil {
			return extractOpenAIText(decoded)
		}
	}
	return ""
}

func extractOpenAIReasoning(content interface{}) string {
	// For standard OpenAI chat completion, reasoning is typically embedded in content
	// Look for structured reasoning blocks if present
	switch value := content.(type) {
	case []interface{}:
		var reasoning strings.Builder
		for _, part := range value {
			if partMap, ok := part.(map[string]interface{}); ok {
				// Check for reasoning field in structured content
				if r, ok := partMap["reasoning"].(string); ok {
					reasoning.WriteString(r)
				}
				// Also check for thinking field
				if t, ok := partMap["thinking"].(string); ok {
					reasoning.WriteString(t)
				}
				if partType, ok := partMap["type"].(string); ok && (partType == "reasoning" || partType == "thinking") {
					if text, ok := partMap["text"].(string); ok {
						reasoning.WriteString(text)
					} else if inner, ok := partMap["content"]; ok {
						reasoning.WriteString(extractOpenAIText(inner))
					}
				}
			}
		}
		return reasoning.String()
	}
	return ""
}

func extractOpenAIMessageReasoning(msg *openAIChatMessage) string {
	if msg == nil {
		return ""
	}
	if msg.Reasoning != "" {
		return msg.Reasoning
	}
	if msg.Thinking != "" {
		return msg.Thinking
	}
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		return *msg.ReasoningContent
	}
	if msg.ThinkingContent != nil && *msg.ThinkingContent != "" {
		return *msg.ThinkingContent
	}
	return extractOpenAIReasoning(msg.Content)
}

type openAIChatRequest struct {
	Model       string                   `json:"model"`
	Messages    []openAIChatMessage      `json:"messages"`
	Tools       []map[string]interface{} `json:"tools,omitempty"`
	Temperature *float64                 `json:"temperature,omitempty"`
	MaxTokens   int                      `json:"max_tokens,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
}

type openAIChatMessage struct {
	Role             string                   `json:"role"`
	Content          interface{}              `json:"content"`
	Reasoning        string                   `json:"reasoning,omitempty"`
	Thinking         string                   `json:"thinking,omitempty"`
	ReasoningContent *string                  `json:"reasoning_content,omitempty"`
	ThinkingContent  *string                  `json:"thinking_content,omitempty"`
	Name             string                   `json:"name,omitempty"`
	ToolCalls        []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolCallID       string                   `json:"tool_call_id,omitempty"`
	CacheControl     map[string]interface{}   `json:"cache_control,omitempty"` // For prompt caching
}

type openAIChatResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Model   string                 `json:"model"`
	Created int64                  `json:"created"`
	Choices []openAIChatChoice     `json:"choices"`
	Usage   map[string]interface{} `json:"usage,omitempty"`
}

type openAIChatChoice struct {
	Index        int                `json:"index"`
	FinishReason string             `json:"finish_reason"`
	Message      *openAIChatMessage `json:"message"`
}

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Model   string               `json:"model"`
	Created int64                `json:"created"`
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Index        int              `json:"index"`
	FinishReason string           `json:"finish_reason"`
	Delta        *openAIChatDelta `json:"delta"`
}

type openAIChatDelta struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	Reasoning        string      `json:"reasoning,omitempty"`
	Thinking         string      `json:"thinking,omitempty"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ThinkingContent  string      `json:"thinking_content,omitempty"`
}
