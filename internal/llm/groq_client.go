package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GroqClient implements the Client interface for Groq's API, supporting both
// the standard OpenAI-compatible chat completions API and the new Responses API
// for specific models (e.g., models with "openai/" prefix).
//
// The client automatically detects which API to use based on the model name:
// - Responses API: models with "openai/" prefix (e.g., "openai/gpt-oss-120b")
// - Chat Completions API: all other models
type GroqClient struct {
	apiKey          string
	model           string
	baseURL         string
	httpClient      *http.Client
	useResponsesAPI bool
}

// NewGroqClient creates a Groq client that automatically chooses between
// the Responses API and standard chat completions API based on the model.
func NewGroqClient(apiKey, modelID string) (Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("API key is required")
	}

	model := strings.TrimSpace(modelID)
	if model == "" {
		model = "llama-3.1-8b-instant"
	}

	client := &GroqClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.groq.com/openai/v1",
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		useResponsesAPI: groqRequiresResponsesAPI(model),
	}

	return client, nil
}

// groqRequiresResponsesAPI determines if a Groq model should use the Responses API.
func groqRequiresResponsesAPI(modelName string) bool {
	model := strings.TrimSpace(strings.ToLower(modelName))
	if model == "" {
		return false
	}

	// Models with "openai/" prefix use the Responses API
	return strings.HasPrefix(model, "openai/")
}

func (c *GroqClient) GetModelName() string {
	return c.model
}

func (c *GroqClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *GroqClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("groq completion request cannot be nil")
	}

	if c.useResponsesAPI {
		return c.completeWithGroqResponses(ctx, req)
	}
	return c.completeWithChat(ctx, req)
}

func (c *GroqClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if req == nil {
		return fmt.Errorf("groq completion request cannot be nil")
	}

	if c.useResponsesAPI {
		return c.streamWithGroqResponses(ctx, req, callback)
	}
	return c.streamWithChat(ctx, req, callback)
}

// Groq Responses API structures

type groqResponsesRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
	// Additional fields can be added as needed
}

type groqResponsesResponse struct {
	ID                 string               `json:"id"`
	Object             string               `json:"object"`
	Status             string               `json:"status"`
	CreatedAt          int64                `json:"created_at"`
	Output             []groqResponseOutput `json:"output"`
	PreviousResponseID *string              `json:"previous_response_id"`
	Usage              *groqUsage           `json:"usage,omitempty"`
}

type groqResponseOutput struct {
	Type    string                `json:"type"`
	ID      string                `json:"id"`
	Status  string                `json:"status"`
	Role    string                `json:"role"`
	Content []groqResponseContent `json:"content"`
}

type groqResponseContent struct {
	Type        string   `json:"type"`
	Text        string   `json:"text"`
	Annotations []string `json:"annotations"`
}

type groqUsage struct {
	InputTokens         int                    `json:"input_tokens"`
	InputTokensDetails  map[string]interface{} `json:"input_tokens_details,omitempty"`
	OutputTokens        int                    `json:"output_tokens"`
	OutputTokensDetails map[string]interface{} `json:"output_tokens_details,omitempty"`
	TotalTokens         int                    `json:"total_tokens"`
}

// completeWithGroqResponses handles completions using Groq's Responses API
func (c *GroqClient) completeWithGroqResponses(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// Convert messages to a single input string for Responses API
	input, err := c.buildGroqResponsesInput(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("failed to build Responses API input: %w", err)
	}

	payload := groqResponsesRequest{
		Model: c.model,
		Input: input,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("groq responses completion failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("groq responses completion failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var responsesResp groqResponsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&responsesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.convertGroqResponsesResponse(&responsesResp), nil
}

// streamWithGroqResponses handles streaming using Groq's Responses API
// Note: This implementation assumes Responses API supports streaming similar to OpenAI
func (c *GroqClient) streamWithGroqResponses(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	// For now, fall back to non-streaming and send the complete response as one chunk
	// This can be enhanced later if Groq's Responses API supports streaming
	resp, err := c.completeWithGroqResponses(ctx, req)
	if err != nil {
		return err
	}

	if resp.Content != "" {
		if err := callback(resp.Content); err != nil {
			return err
		}
	}

	return nil
}

// completeWithChat handles completions using Groq's standard OpenAI-compatible chat API
func (c *GroqClient) completeWithChat(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// Use the existing OpenAI compatible client functionality
	oaClient, err := NewOpenAICompatibleClient(c.apiKey, c.baseURL, c.model)
	if err != nil {
		return nil, err
	}

	return oaClient.CompleteWithRequest(ctx, req)
}

// streamWithChat handles streaming using Groq's standard OpenAI-compatible chat API
func (c *GroqClient) streamWithChat(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	// Use the existing OpenAI compatible client functionality
	oaClient, err := NewOpenAICompatibleClient(c.apiKey, c.baseURL, c.model)
	if err != nil {
		return err
	}

	return oaClient.Stream(ctx, req, callback)
}

// buildGroqResponsesInput converts internal messages to a single input string for Responses API
func (c *GroqClient) buildGroqResponsesInput(messages []*Message) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	// For Responses API, we need to construct a single input string
	// This is a simplified implementation - can be enhanced based on specific needs
	var input strings.Builder

	for _, msg := range messages {
		if msg == nil || strings.TrimSpace(msg.Content) == "" {
			continue
		}

		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "system":
			input.WriteString("System: ")
			input.WriteString(msg.Content)
			input.WriteString("\n\n")
		case "assistant":
			input.WriteString("Assistant: ")
			input.WriteString(msg.Content)
			input.WriteString("\n\n")
		case "user", "tool":
			input.WriteString("User: ")
			input.WriteString(msg.Content)
			input.WriteString("\n\n")
		default:
			input.WriteString(msg.Content)
			input.WriteString("\n\n")
		}
	}

	result := strings.TrimSpace(input.String())
	if result == "" {
		return "", fmt.Errorf("no valid content in messages")
	}

	return result, nil
}

// convertGroqResponsesResponse converts Groq Responses API response to CompletionResponse
func (c *GroqClient) convertGroqResponsesResponse(resp *groqResponsesResponse) *CompletionResponse {
	if resp == nil {
		return &CompletionResponse{StopReason: "unknown"}
	}

	var content string
	var stopReason string = "stop"

	// Extract text from the response output
	for _, output := range resp.Output {
		if output.Type == "message" && strings.ToLower(output.Role) == "assistant" {
			for _, item := range output.Content {
				if item.Type == "output_text" && strings.TrimSpace(item.Text) != "" {
					content = item.Text
					break
				}
			}
			if content != "" {
				break
			}
		}
	}

	// Use status as stop reason if available
	if resp.Status != "" {
		stopReason = resp.Status
	}

	// Convert usage data to map[string]interface{}
	var usage map[string]interface{}
	if resp.Usage != nil {
		usage = make(map[string]interface{})
		usage["input_tokens"] = resp.Usage.InputTokens
		usage["output_tokens"] = resp.Usage.OutputTokens
		usage["total_tokens"] = resp.Usage.TotalTokens

		if resp.Usage.InputTokensDetails != nil {
			usage["input_tokens_details"] = resp.Usage.InputTokensDetails
		}
		if resp.Usage.OutputTokensDetails != nil {
			usage["output_tokens_details"] = resp.Usage.OutputTokensDetails
		}
	}

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  nil, // Responses API may not support tools in the same way
		StopReason: stopReason,
		Usage:      usage,
	}
}
