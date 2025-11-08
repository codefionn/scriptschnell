package llm

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/tmc/langchaingo/llms"
	llmopenai "github.com/tmc/langchaingo/llms/openai"
)

// LangChainClient wraps langchaingo LLM
type LangChainClient struct {
	llm             llms.Model
	modelName       string
	provider        string
	useResponses    bool
	responsesClient *openai.Client
}

// NewOpenAICompatibleClient creates a client for any OpenAI-compatible API
// This supports local LLMs (LM Studio, LocalAI, etc.) and custom deployments
func NewOpenAICompatibleClient(apiKey, baseURL, modelName string) (*LangChainClient, error) {
	if modelName == "" {
		return nil, fmt.Errorf("model name is required for OpenAI-compatible provider")
	}

	// Build options
	opts := []llmopenai.Option{
		llmopenai.WithModel(modelName),
		llmopenai.WithBaseURL(baseURL),
	}

	// Only add API key if provided (some local servers don't require auth)
	if apiKey != "" {
		opts = append(opts, llmopenai.WithToken(apiKey))
	}

	llm, err := llmopenai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAI-compatible client: %w", err)
	}

	return &LangChainClient{
		llm:       llm,
		modelName: modelName,
		provider:  "openai-compatible",
	}, nil
}

func (c *LangChainClient) GetModelName() string {
	return c.modelName
}

func (c *LangChainClient) Complete(ctx context.Context, prompt string) (string, error) {
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

func (c *LangChainClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if c.useResponses {
		return c.completeWithResponses(ctx, req)
	}

	// Normalize messages for provider-specific requirements before conversion.
	normalized := c.transformMessagesForProvider(req.Messages)

	// Convert messages to langchaingo format
	messages := c.convertMessages(normalized)

	// Prepend system message if present
	if req.SystemPrompt != "" {
		systemMsg := llms.MessageContent{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				llms.TextPart(req.SystemPrompt),
			},
		}
		messages = append([]llms.MessageContent{systemMsg}, messages...)
	}

	// Set options
	opts := []llms.CallOption{}

	// OpenAI has changed their API - many models now only support temperature=1
	if c.isTemperatureUnsupported() {
		opts = append(opts, llms.WithTemperature(1.0))
	} else {
		opts = append(opts, llms.WithTemperature(req.Temperature))
	}

	if req.MaxTokens > 0 {
		opts = append(opts, llms.WithMaxTokens(req.MaxTokens))
	}

	// Add tools if present
	if len(req.Tools) > 0 {
		tools := c.convertTools(req.Tools)
		opts = append(opts, llms.WithTools(tools))
	}

	// Generate completion using GenerateContent for tool support
	response, err := c.llm.GenerateContent(ctx, messages, opts...)
	if err != nil {
		return nil, fmt.Errorf("completion failed: %w", err)
	}

	// Extract the first choice
	if len(response.Choices) == 0 {
		return &CompletionResponse{
			Content:    "",
			StopReason: "stop",
		}, nil
	}

	choice := response.Choices[0]

	// Convert tool calls if present
	var toolCalls []map[string]interface{}
	for _, tc := range choice.ToolCalls {
		toolCalls = append(toolCalls, map[string]interface{}{
			"id":   tc.ID,
			"type": tc.Type,
			"function": map[string]interface{}{
				"name":      tc.FunctionCall.Name,
				"arguments": tc.FunctionCall.Arguments,
			},
		})
	}

	return &CompletionResponse{
		Content:    choice.Content,
		ToolCalls:  toolCalls,
		StopReason: choice.StopReason,
	}, nil
}

func (c *LangChainClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if c.useResponses {
		return c.streamWithResponses(ctx, req, callback)
	}
	normalized := c.transformMessagesForProvider(req.Messages)

	// Build a single prompt from messages
	var promptBuilder strings.Builder

	if req.SystemPrompt != "" {
		promptBuilder.WriteString("System: ")
		promptBuilder.WriteString(req.SystemPrompt)
		promptBuilder.WriteString("\n\n")
	}

	for _, msg := range normalized {
		promptBuilder.WriteString(msg.Role)
		promptBuilder.WriteString(": ")
		promptBuilder.WriteString(msg.Content)
		promptBuilder.WriteString("\n\n")
	}

	opts := []llms.CallOption{
		llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			return callback(string(chunk))
		}),
	}

	// OpenAI has changed their API - many models now only support temperature=1
	if c.isTemperatureUnsupported() {
		// Must explicitly set temperature to 1 for these models
		opts = append(opts, llms.WithTemperature(1.0))
	} else {
		// Use the requested temperature for models that support it
		opts = append(opts, llms.WithTemperature(req.Temperature))
	}

	if req.MaxTokens > 0 {
		opts = append(opts, llms.WithMaxTokens(req.MaxTokens))
	}

	_, err := llms.GenerateFromSinglePrompt(ctx, c.llm, promptBuilder.String(), opts...)
	return err
}

// isTemperatureUnsupported checks if the model doesn't support custom temperature
func (c *LangChainClient) isTemperatureUnsupported() bool {
	if c.provider != "openai" {
		return false
	}
	return isOpenAITemperatureUnsupported(c.modelName)
}

// convertMessages converts our Message format to langchaingo MessageContent format
func (c *LangChainClient) convertMessages(messages []*Message) []llms.MessageContent {
	result := make([]llms.MessageContent, 0, len(messages))

	for _, msg := range messages {
		// Convert role to langchaingo format
		var role llms.ChatMessageType
		switch msg.Role {
		case "user":
			role = llms.ChatMessageTypeHuman
		case "assistant":
			role = llms.ChatMessageTypeAI
		case "system":
			role = llms.ChatMessageTypeSystem
		case "tool":
			role = llms.ChatMessageTypeTool
		default:
			role = llms.ChatMessageTypeHuman
		}

		// Handle assistant messages with tool calls
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			parts := []llms.ContentPart{}

			// Mistral's SDK splits mixed assistant content (text + tool calls) into
			// separate assistant messages, which triggers its ordering validation.
			// To avoid consecutive assistant turns, omit textual content when
			// targeting the native Mistral API.
			if msg.Content != "" && c.provider != "mistral" {
				parts = append(parts, llms.TextPart(msg.Content))
			}

			// Convert tool calls
			for _, tc := range msg.ToolCalls {
				toolID, _ := tc["id"].(string)
				function, _ := tc["function"].(map[string]interface{})
				name, _ := function["name"].(string)
				arguments, _ := function["arguments"].(string)

				parts = append(parts, llms.ToolCall{
					ID:   toolID,
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      name,
						Arguments: arguments,
					},
				})
			}

			result = append(result, llms.MessageContent{
				Role:  role,
				Parts: parts,
			})
			continue
		}

		// Handle tool responses
		if msg.Role == "tool" && msg.ToolID != "" {
			result = append(result, llms.MessageContent{
				Role: role,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: msg.ToolID,
						Name:       msg.ToolName,
						Content:    msg.Content,
					},
				},
			})
			continue
		}

		// Regular text message
		result = append(result, llms.MessageContent{
			Role: role,
			Parts: []llms.ContentPart{
				llms.TextPart(msg.Content),
			},
		})
	}
	return result
}

// convertTools converts our tool format to langchaingo Tool format
func (c *LangChainClient) convertTools(tools []map[string]interface{}) []llms.Tool {
	result := make([]llms.Tool, 0, len(tools))
	for _, tool := range tools {
		// Extract function definition
		function, ok := tool["function"].(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := function["name"].(string)
		description, _ := function["description"].(string)
		parameters := function["parameters"]

		result = append(result, llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  parameters,
			},
		})
	}
	return result
}

// transformMessagesForProvider applies provider-specific normalization rules before sending
// a completion request. Most providers don't require adjustments, but some (e.g. Mistral)
// expect conversations to avoid consecutive assistant messages.
func (c *LangChainClient) transformMessagesForProvider(messages []*Message) []*Message {
	if c.provider != "mistral" {
		return messages
	}
	return normalizeMistralConversation(messages)
}

func cloneMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}

	cloned := &Message{
		Role:     msg.Role,
		Content:  msg.Content,
		ToolID:   msg.ToolID,
		ToolName: msg.ToolName,
	}

	if len(msg.ToolCalls) > 0 {
		cloned.ToolCalls = cloneToolCalls(msg.ToolCalls)
	}

	return cloned
}

func cloneToolCalls(toolCalls []map[string]interface{}) []map[string]interface{} {
	if len(toolCalls) == 0 {
		return nil
	}

	cloned := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if tc == nil {
			continue
		}
		copyMap := make(map[string]interface{}, len(tc))
		for k, v := range tc {
			copyMap[k] = v
		}
		cloned = append(cloned, copyMap)
	}

	return cloned
}

func normalizeMistralConversation(messages []*Message) []*Message {
	if len(messages) == 0 {
		return messages
	}

	cloned := make([]*Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		cloned = append(cloned, cloneMessage(msg))
	}

	if len(cloned) == 0 {
		return cloned
	}

	trimmed := trimTrailingAssistantMessages(cloned)
	if len(trimmed) == 0 {
		return trimmed
	}

	merged := make([]*Message, 0, len(trimmed))
	for _, msg := range trimmed {
		if len(merged) == 0 {
			merged = append(merged, msg)
			continue
		}

		prev := merged[len(merged)-1]
		if strings.EqualFold(prev.Role, "assistant") && strings.EqualFold(msg.Role, "assistant") {
			switch {
			case prev.Content != "" && msg.Content != "":
				prev.Content = prev.Content + "\n" + msg.Content
			case prev.Content == "":
				prev.Content = msg.Content
			case msg.Content != "":
				prev.Content += msg.Content
			}

			if len(msg.ToolCalls) > 0 {
				prev.ToolCalls = append(prev.ToolCalls, cloneToolCalls(msg.ToolCalls)...)
			}
			continue
		}

		merged = append(merged, msg)
	}

	return merged
}

func trimTrailingAssistantMessages(messages []*Message) []*Message {
	if len(messages) == 0 {
		return messages
	}

	end := len(messages)
	for end > 0 {
		role := strings.ToLower(strings.TrimSpace(messages[end-1].Role))
		if role == "user" || role == "tool" {
			break
		}
		end--
	}

	if end == 0 || end == len(messages) {
		return messages
	}

	return messages[:end]
}

func (c *LangChainClient) completeWithResponses(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if c.responsesClient == nil {
		return nil, fmt.Errorf("completion failed: responses client not configured")
	}

	params, err := c.buildResponsesParams(req)
	if err != nil {
		return nil, fmt.Errorf("completion failed: %w", err)
	}

	resp, err := performResponsesCompletion(ctx, c.responsesClient, params)
	if err != nil {
		return nil, fmt.Errorf("completion failed: %w", err)
	}

	return resp, nil
}

func (c *LangChainClient) streamWithResponses(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if c.responsesClient == nil {
		return fmt.Errorf("completion failed: responses client not configured")
	}

	params, err := c.buildResponsesParams(req)
	if err != nil {
		return fmt.Errorf("completion failed: %w", err)
	}

	if err := performResponsesStream(ctx, c.responsesClient, params, callback); err != nil {
		return fmt.Errorf("completion failed: %w", err)
	}
	return nil
}

func (c *LangChainClient) buildResponsesParams(req *CompletionRequest) (responses.ResponseNewParams, error) {
	transformed := c.transformMessagesForProvider(req.Messages)
	inputItems, err := buildResponsesInput(transformed)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}

	if len(inputItems) == 0 {
		return responses.ResponseNewParams{}, fmt.Errorf("no messages provided")
	}

	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(c.modelName),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: inputItems,
		},
	}

	if req.SystemPrompt != "" {
		params.Instructions = openai.String(req.SystemPrompt)
	}

	if req.Temperature != 0 && !c.isTemperatureUnsupported() {
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
