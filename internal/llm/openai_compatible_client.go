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

// OpenAICompatibleClient implements the Client interface for generic OpenAI-compatible APIs.
// It uses the same JSON payloads as OpenAI's chat completions endpoint and supports optional
// API keys plus custom base URLs.
//
// Features:
// - Chat completions (single-shot + structured requests)
// - Streaming responses via SSE-style data chunks
// - Tool call serialization / deserialization
// - Optional system prompts
// - Customizable temperature and max_tokens if supported
// - Uses provider-specific message normalization (Mistral) reused from helper functions
//
// This client intentionally mirrors OpenAIClient's behavior but delegates HTTP calls to
// arbitrary OpenAI-compatible servers (LocalAI, LM Studio, Groq base, etc.).
type OpenAICompatibleClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewOpenAICompatibleClient constructs a client for an OpenAI-compatible API.
// baseURL must point to the API root (e.g. http://localhost:11434/v1). If apiKey is empty,
// requests are sent without Authorization headers (useful for unsecured local servers).
func NewOpenAICompatibleClient(apiKey, baseURL, modelName string) (*OpenAICompatibleClient, error) {
	model := strings.TrimSpace(modelName)
	if model == "" {
		return nil, fmt.Errorf("model name is required for OpenAI-compatible provider")
	}

	trimmedBase := strings.TrimSpace(baseURL)
	if trimmedBase == "" {
		return nil, fmt.Errorf("base URL is required for OpenAI-compatible provider")
	}

	trimmedBase = strings.TrimRight(trimmedBase, "/")

	return &OpenAICompatibleClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: trimmedBase,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

func (c *OpenAICompatibleClient) GetModelName() string {
	return c.model
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *OpenAICompatibleClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("openai-compatible completion request cannot be nil")
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
		return nil, fmt.Errorf("openai-compatible completion failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai-compatible completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var chatResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("openai-compatible completion failed: %w", err)
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

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  convertOpenAIToolCalls(first.Message.ToolCalls),
		StopReason: stopReason,
	}, nil
}

func (c *OpenAICompatibleClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("openai-compatible completion request cannot be nil")
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
		return fmt.Errorf("openai-compatible stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai-compatible stream failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("openai-compatible stream failed to decode chunk: %w", err)
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
		return fmt.Errorf("openai-compatible stream failed: %w", err)
	}

	return nil
}

func (c *OpenAICompatibleClient) buildChatRequest(req *CompletionRequest, stream bool) (*openAIChatRequest, error) {
	payload, err := convertRequestToOpenAI(req, c.model, stream, false)
	if err != nil {
		return nil, err
	}

	if req.Temperature != 0 {
		temp := req.Temperature
		payload.Temperature = &temp
	}

	return payload, nil
}

func (c *OpenAICompatibleClient) newChatRequest(ctx context.Context, payload *openAIChatRequest) (*http.Request, error) {
	if payload == nil {
		return nil, fmt.Errorf("openai-compatible payload cannot be nil")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai-compatible failed to encode payload: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai-compatible failed to create request: %w", err)
	}

	if strings.TrimSpace(c.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}
