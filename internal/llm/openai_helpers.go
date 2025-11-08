package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
)

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

func isOpenAITemperatureUnsupported(modelName string) bool {
	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	if modelLower == "" {
		return false
	}

	if strings.Contains(modelLower, "o1") ||
		strings.Contains(modelLower, "o3") ||
		strings.Contains(modelLower, "reasoning") {
		return true
	}

	if strings.HasPrefix(modelLower, "gpt-") {
		knownSupportedModels := []string{
			// Reserved for specific models confirmed to support temperature adjustments.
		}

		for _, supported := range knownSupportedModels {
			if modelLower == supported {
				return false
			}
		}

		return true
	}

	return false
}

func performResponsesCompletion(ctx context.Context, client *openai.Client, params responses.ResponseNewParams) (*CompletionResponse, error) {
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return convertResponsesCompletion(resp), nil
}

func performResponsesStream(ctx context.Context, client *openai.Client, params responses.ResponseNewParams, callback func(chunk string) error) error {
	stream := client.Responses.NewStreaming(ctx, params)
	for stream.Next() {
		event := stream.Current()
		if event.Type != "response.output_text.delta" {
			continue
		}

		delta := event.AsResponseOutputTextDelta()
		if delta.Delta == "" {
			continue
		}

		if err := callback(delta.Delta); err != nil {
			return err
		}
	}

	if err := stream.Err(); err != nil {
		return err
	}

	return nil
}
