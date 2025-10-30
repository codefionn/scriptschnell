package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
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

// NewOpenAIClient creates an OpenAI client
func NewOpenAIClient(apiKey, modelName string) (*LangChainClient, error) {
	if modelName == "" {
		modelName = "gpt-4-turbo-preview"
	}

	if requiresResponsesAPI(modelName) {
		client := openai.NewClient(option.WithAPIKey(apiKey))
		return &LangChainClient{
			modelName:       modelName,
			provider:        "openai",
			useResponses:    true,
			responsesClient: &client,
		}, nil
	}

	llm, err := llmopenai.New(
		llmopenai.WithToken(apiKey),
		llmopenai.WithModel(modelName),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAI client: %w", err)
	}

	return &LangChainClient{
		llm:       llm,
		modelName: modelName,
		provider:  "openai",
	}, nil
}

// NewAnthropicClient creates an Anthropic client
func NewAnthropicClient(apiKey, modelName string) (*LangChainClient, error) {
	if modelName == "" {
		modelName = "claude-3-5-sonnet-20241022"
	}

	llm, err := anthropic.New(
		anthropic.WithToken(apiKey),
		anthropic.WithModel(modelName),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Anthropic client: %w", err)
	}

	return &LangChainClient{
		llm:       llm,
		modelName: modelName,
		provider:  "anthropic",
	}, nil
}

// NewOpenRouterClient creates an OpenRouter client using the OpenAI-compatible API
func NewOpenRouterClient(apiKey, modelName string) (*LangChainClient, error) {
	trimmedModel := strings.TrimSpace(modelName)
	if trimmedModel == "" {
		trimmedModel = "openai/gpt-4o-mini"
	}

	llm, err := llmopenai.New(
		llmopenai.WithToken(apiKey),
		llmopenai.WithModel(trimmedModel),
		llmopenai.WithBaseURL("https://openrouter.ai/api/v1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenRouter client: %w", err)
	}

	return &LangChainClient{
		llm:       llm,
		modelName: trimmedModel,
		provider:  "openrouter",
	}, nil
}

// NewCerebrasLangChainClient creates a Cerebras client using the OpenAI-compatible API
func NewCerebrasLangChainClient(apiKey, modelName string) (*LangChainClient, error) {
	if modelName == "" {
		modelName = "llama3.1-8b"
	}

	llm, err := llmopenai.New(
		llmopenai.WithToken(apiKey),
		llmopenai.WithModel(modelName),
		llmopenai.WithBaseURL("https://api.cerebras.ai/v1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cerebras client: %w", err)
	}

	return &LangChainClient{
		llm:       llm,
		modelName: modelName,
		provider:  "cerebras",
	}, nil
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
	// OpenAI has recently changed their API - many models no longer support custom temperature
	if c.provider == "openai" {
		modelLower := strings.ToLower(c.modelName)

		// o1 series, o3 series, and reasoning models don't support temperature
		if strings.Contains(modelLower, "o1") ||
			strings.Contains(modelLower, "o3") ||
			strings.Contains(modelLower, "reasoning") {
			return true
		}

		// Check if model starts with "gpt-" prefix (OpenAI's standard naming)
		// Many recent GPT models also don't support custom temperature
		// As a safe default, we'll skip temperature for all newer GPT models
		// unless they're known to support it
		if strings.HasPrefix(modelLower, "gpt-") {
			// Older models that are known to support temperature
			// Add specific model versions here if needed
			knownSupportedModels := []string{
				// Add any models confirmed to support temperature
			}

			for _, supported := range knownSupportedModels {
				if modelLower == supported {
					return false
				}
			}

			// For safety, assume newer GPT models don't support custom temperature
			// OpenAI seems to be moving towards temperature=1 as default
			return true
		}
	}
	return false
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

func requiresResponsesAPI(modelName string) bool {
	model := strings.TrimSpace(strings.ToLower(modelName))
	if model == "" {
		return false
	}

	if strings.HasPrefix(model, "gpt-5") {
		return true
	}

	if strings.Contains(model, "codex") {
		return true
	}

	if strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
		return true
	}

	if strings.HasPrefix(model, "gpt-4.1") {
		return true
	}

	return false
}

func (c *LangChainClient) completeWithResponses(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if c.responsesClient == nil {
		return nil, fmt.Errorf("completion failed: responses client not configured")
	}

	params, err := c.buildResponsesParams(req)
	if err != nil {
		return nil, fmt.Errorf("completion failed: %w", err)
	}

	resp, err := c.responsesClient.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("completion failed: %w", err)
	}

	return convertResponsesCompletion(resp), nil
}

func (c *LangChainClient) streamWithResponses(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	if c.responsesClient == nil {
		return fmt.Errorf("completion failed: responses client not configured")
	}

	params, err := c.buildResponsesParams(req)
	if err != nil {
		return fmt.Errorf("completion failed: %w", err)
	}

	stream := c.responsesClient.Responses.NewStreaming(ctx, params)
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			delta := event.AsResponseOutputTextDelta()
			if delta.Delta != "" {
				if err := callback(delta.Delta); err != nil {
					return err
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
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

func buildResponsesInput(messages []*Message) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(messages))

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "tool":
			if msg.ToolID == "" {
				continue
			}
			input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(msg.ToolID, msg.Content))
		case "assistant":
			if strings.TrimSpace(msg.Content) != "" {
				input = append(input, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleAssistant))
			}
			for _, tc := range msg.ToolCalls {
				callID, name, args, ok := parseToolCall(tc)
				if !ok {
					continue
				}
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(args, callID, name))
			}
		case "system":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			input = append(input, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleSystem))
		case "developer":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			input = append(input, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleDeveloper))
		default:
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			input = append(input, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleUser))
		}
	}

	return input, nil
}

func convertResponsesTools(tools []map[string]interface{}) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		if toolType, _ := tool["type"].(string); toolType != "function" {
			continue
		}

		function, ok := tool["function"].(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := function["name"].(string)
		if name == "" {
			continue
		}

		parameters, _ := function["parameters"].(map[string]interface{})
		description, _ := function["description"].(string)
		strict, _ := function["strict"].(bool)

		variant := responses.ToolParamOfFunction(name, parameters, strict)
		if description != "" && variant.OfFunction != nil {
			variant.OfFunction.Description = openai.String(description)
		}

		result = append(result, variant)
	}
	return result
}

func parseToolCall(raw map[string]interface{}) (string, string, string, bool) {
	if raw == nil {
		return "", "", "", false
	}

	callID, _ := raw["id"].(string)
	if callID == "" {
		callID, _ = raw["call_id"].(string)
	}

	function, ok := raw["function"].(map[string]interface{})
	if !ok {
		return "", "", "", false
	}

	name, _ := function["name"].(string)
	if name == "" {
		return "", "", "", false
	}

	args := stringifyArguments(function["arguments"])
	if callID == "" {
		callID = fmt.Sprintf("call_%s", name)
	}

	return callID, name, args, true
}

func stringifyArguments(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
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

func convertResponsesCompletion(resp *responses.Response) *CompletionResponse {
	if resp == nil {
		return &CompletionResponse{StopReason: ""}
	}

	return &CompletionResponse{
		Content:    resp.OutputText(),
		ToolCalls:  extractResponsesToolCalls(resp.Output),
		StopReason: string(resp.Status),
	}
}

func extractResponsesToolCalls(items []responses.ResponseOutputItemUnion) []map[string]interface{} {
	toolCalls := make([]map[string]interface{}, 0)
	for _, item := range items {
		if item.Type != "function_call" {
			continue
		}

		call := item.AsFunctionCall()
		identifier := call.CallID
		if identifier == "" {
			identifier = call.ID
		}

		toolCalls = append(toolCalls, map[string]interface{}{
			"id":   identifier,
			"type": "function",
			"function": map[string]interface{}{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return toolCalls
}
