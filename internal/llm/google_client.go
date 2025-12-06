package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/logger"
	genai "google.golang.org/genai"
)

// GoogleGenAIClient implements the Client interface using the official Google GenAI SDK.
type GoogleGenAIClient struct {
	modelName string
	client    *genai.Client
}

// NewGoogleAIClient creates a Google GenAI client for the provided model.
func NewGoogleAIClient(apiKey, modelName string) (Client, error) {
	normalizedModel := normalizeGoogleModelName(modelName)

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Google GenAI client: %w", err)
	}

	return &GoogleGenAIClient{
		modelName: normalizedModel,
		client:    client,
	}, nil
}

func (c *GoogleGenAIClient) GetModelName() string {
	return c.modelName
}

func (c *GoogleGenAIClient) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := c.client.Models.GenerateContent(ctx, c.modelName, genai.Text(prompt), nil)
	if err != nil {
		return "", fmt.Errorf("google genai completion failed: %w", err)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", nil
	}
	return collectTextFromContent(resp.Candidates[0].Content), nil
}

func (c *GoogleGenAIClient) CompleteWithRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	contents, err := convertMessagesToGenAI(req.Messages)
	if err != nil {
		return nil, err
	}
	if len(contents) == 0 {
		return &CompletionResponse{}, nil
	}

	cfg := buildGenAIGenerationConfig(req)

	resp, err := c.client.Models.GenerateContent(ctx, c.modelName, contents, cfg)
	if err != nil {
		return nil, fmt.Errorf("google genai completion failed: %w", err)
	}

	return buildCompletionResponse(resp), nil
}

func (c *GoogleGenAIClient) Stream(ctx context.Context, req *CompletionRequest, callback func(chunk string) error) error {
	contents, err := convertMessagesToGenAI(req.Messages)
	if err != nil {
		return err
	}
	if len(contents) == 0 {
		return nil
	}

	cfg := buildGenAIGenerationConfig(req)

	stream := c.client.Models.GenerateContentStream(ctx, c.modelName, contents, cfg)
	for result, err := range stream {
		if err != nil {
			return fmt.Errorf("google genai stream failed: %w", err)
		}
		if len(result.Candidates) == 0 || result.Candidates[0].Content == nil {
			continue
		}
		chunk := collectTextFromContent(result.Candidates[0].Content)
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		if err := callback(chunk); err != nil {
			return err
		}
	}
	return nil
}

func buildCompletionResponse(resp *genai.GenerateContentResponse) *CompletionResponse {
	if resp == nil || len(resp.Candidates) == 0 {
		stop := ""
		if resp != nil && resp.PromptFeedback != nil {
			stop = string(resp.PromptFeedback.BlockReason)
		}
		return &CompletionResponse{StopReason: stop}
	}

	candidate := resp.Candidates[0]
	content := ""
	if candidate.Content != nil {
		content = collectTextFromContent(candidate.Content)
	}

	toolCalls := convertToolCallsFromContent(candidate.Content)

	stopReason := string(candidate.FinishReason)
	if stopReason == "" {
		stopReason = candidate.FinishMessage
	}

	return &CompletionResponse{
		Content:    content,
		ToolCalls:  toolCalls,
		StopReason: stopReason,
	}
}

func collectTextFromContent(content *genai.Content) string {
	if content == nil {
		return ""
	}

	var sb strings.Builder
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

func convertToolCallsFromContent(content *genai.Content) []map[string]interface{} {
	if content == nil {
		return nil
	}

	toolCalls := make([]map[string]interface{}, 0)
	for _, part := range content.Parts {
		if part == nil || part.FunctionCall == nil {
			continue
		}

		argsJSON, err := json.Marshal(part.FunctionCall.Args)
		if err != nil {
			argsJSON = []byte("{}")
		}

		toolCall := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":      part.FunctionCall.Name,
				"arguments": string(argsJSON),
			},
		}
		if part.FunctionCall.ID != "" {
			toolCall["id"] = part.FunctionCall.ID
		}
		if part.Thought {
			toolCall["thought"] = true
		}
		if len(part.ThoughtSignature) > 0 {
			toolCall["thought_signature"] = base64.StdEncoding.EncodeToString(part.ThoughtSignature)
		}

		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls
}

func convertMessagesToGenAI(messages []*Message) ([]*genai.Content, error) {
	// Check if we can use native Google format
	hasNativeFormat := len(messages) > 0 && messages[0].NativeFormat != nil && messages[0].NativeProvider == "google"

	if hasNativeFormat {
		logger.Debug("Using native Google message format (%d messages)", len(messages))
		contents, err := extractNativeGoogleMessages(messages)
		if err != nil {
			logger.Warn("Failed to extract native Google messages, falling back to conversion: %v", err)
			return convertMessagesToGenAIFromUnified(messages)
		}
		return contents, nil
	}

	return convertMessagesToGenAIFromUnified(messages)
}

func convertMessagesToGenAIFromUnified(messages []*Message) ([]*genai.Content, error) {
	contents := make([]*genai.Content, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		switch msg.Role {
		case "assistant":
			content, err := convertAssistantMessage(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, content)
		case "tool":
			content, err := convertToolResponseMessage(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, content)
		case "system":
			fallthrough
		default:
			if msg.Content == "" {
				continue
			}
			contents = append(contents, genai.NewContentFromText(msg.Content, genai.RoleUser))
		}
	}
	return contents, nil
}

func convertAssistantMessage(msg *Message) (*genai.Content, error) {
	parts := make([]*genai.Part, 0, len(msg.ToolCalls)+1)

	if msg.Content != "" {
		parts = append(parts, genai.NewPartFromText(msg.Content))
	}

	for _, tc := range msg.ToolCalls {
		function, _ := tc["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		if name == "" {
			continue
		}

		argsValue := function["arguments"]
		argsMap := make(map[string]any)
		switch v := argsValue.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				if err := json.Unmarshal([]byte(v), &argsMap); err != nil {
					return nil, fmt.Errorf("invalid function call arguments: %w", err)
				}
			}
		case map[string]interface{}:
			for key, value := range v {
				argsMap[key] = value
			}
		case nil:
			// leave empty
		default:
			// unsupported format, ignore
		}

		part := genai.NewPartFromFunctionCall(name, argsMap)
		if id, _ := tc["id"].(string); id != "" {
			part.FunctionCall.ID = id
		}
		if thought, _ := tc["thought"].(bool); thought {
			part.Thought = true
		}
		if sigStr, _ := tc["thought_signature"].(string); sigStr != "" {
			if sig, err := base64.StdEncoding.DecodeString(sigStr); err == nil {
				part.ThoughtSignature = sig
			}
		} else if sigBytes, ok := tc["thought_signature"].([]byte); ok && len(sigBytes) > 0 {
			part.ThoughtSignature = sigBytes
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		parts = append(parts, genai.NewPartFromText(""))
	}

	return genai.NewContentFromParts(parts, genai.RoleModel), nil
}

func convertToolResponseMessage(msg *Message) (*genai.Content, error) {
	responsePayload := make(map[string]any)
	if strings.TrimSpace(msg.Content) != "" {
		if err := json.Unmarshal([]byte(msg.Content), &responsePayload); err != nil {
			responsePayload["output"] = msg.Content
		}
	}

	part := genai.NewPartFromFunctionResponse(msg.ToolName, responsePayload)
	if msg.ToolID != "" {
		part.FunctionResponse.ID = msg.ToolID
	}

	return genai.NewContentFromParts([]*genai.Part{part}, genai.RoleUser), nil
}

func buildGenAIGenerationConfig(req *CompletionRequest) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{}

	if req.SystemPrompt != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.SystemPrompt, genai.RoleUser)
	}

	if req.Temperature > 0 {
		temp := float32(req.Temperature)
		cfg.Temperature = &temp
	}

	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = int32(req.MaxTokens)
	}

	if len(req.Tools) > 0 {
		cfg.Tools = convertToolsToGenAI(req.Tools)
		cfg.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto},
		}
	}

	return cfg
}

func convertToolsToGenAI(tools []map[string]interface{}) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}

	result := make([]*genai.Tool, 0, len(tools))
	for _, tool := range tools {
		function, ok := tool["function"].(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := function["name"].(string)
		if name == "" {
			continue
		}

		description, _ := function["description"].(string)

		decl := &genai.FunctionDeclaration{
			Name:        name,
			Description: description,
		}

		if params, ok := function["parameters"].(map[string]interface{}); ok {
			decl.ParametersJsonSchema = params
		}

		result = append(result, &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}})
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// extractNativeGoogleMessages extracts native Google messages from unified messages
func extractNativeGoogleMessages(messages []*Message) ([]*genai.Content, error) {
	contents := make([]*genai.Content, 0, len(messages))

	for _, msg := range messages {
		if msg == nil || msg.NativeFormat == nil {
			return nil, fmt.Errorf("message missing native format")
		}

		// Type assert to map[string]interface{} (Google native format)
		nativeMap, ok := msg.NativeFormat.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("native format is not a map")
		}

		// Skip system metadata
		if _, isSystem := nativeMap["_google_system"]; isSystem {
			continue
		}

		// Extract role
		roleStr, _ := nativeMap["role"].(string)

		// Extract parts
		partsInterface, _ := nativeMap["parts"].([]interface{})
		parts := make([]*genai.Part, 0, len(partsInterface))

		for _, partInterface := range partsInterface {
			partMap, ok := partInterface.(map[string]interface{})
			if !ok {
				continue
			}

			// Handle text parts
			if text, ok := partMap["text"].(string); ok {
				parts = append(parts, genai.NewPartFromText(text))
			}

			// Handle function call parts
			if functionCall, ok := partMap["functionCall"].(map[string]interface{}); ok {
				name, _ := functionCall["name"].(string)
				args, _ := functionCall["args"].(map[string]interface{})
				argsMap := make(map[string]any)
				for k, v := range args {
					argsMap[k] = v
				}
				parts = append(parts, genai.NewPartFromFunctionCall(name, argsMap))
			}

			// Handle function response parts
			if functionResponse, ok := partMap["functionResponse"].(map[string]interface{}); ok {
				name, _ := functionResponse["name"].(string)
				response, _ := functionResponse["response"].(map[string]interface{})
				responseMap := make(map[string]any)
				for k, v := range response {
					responseMap[k] = v
				}
				parts = append(parts, genai.NewPartFromFunctionResponse(name, responseMap))
			}
		}

		// Determine role based on native role string
		var content *genai.Content
		switch roleStr {
		case "model":
			content = genai.NewContentFromParts(parts, genai.RoleModel)
		default: // "user" or "function"
			content = genai.NewContentFromParts(parts, genai.RoleUser)
		}
		contents = append(contents, content)
	}

	return contents, nil
}

func normalizeGoogleModelName(modelName string) string {
	trimmed := strings.TrimSpace(modelName)
	if trimmed == "" {
		return "models/gemini-2.0-flash"
	}

	lowered := strings.ToLower(trimmed)
	if strings.HasPrefix(lowered, "models/") || strings.HasPrefix(lowered, "publishers/") {
		return trimmed
	}

	return "models/" + trimmed
}
