package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/codefionn/scriptschnell/internal/llm"
)

// OpenAIToolConfig contains the configuration required to create an OpenAITool.
type OpenAIToolConfig struct {
	Name         string
	Description  string
	Model        string
	APIKey       string
	APIKeyEnv    string
	BaseURL      string
	SystemPrompt string
	Temperature  float64
	MaxOutput    int
	ResponseJSON bool
}

// OpenAITool invokes an OpenAI or OpenAI-compatible model as a tool action.
type OpenAITool struct {
	name         string
	description  string
	model        string
	systemPrompt string
	temperature  float64
	maxOutput    int
	responseJSON bool
	apiKey       string
	apiKeyEnv    string
	baseURL      string
	client       llm.Client
}

// NewOpenAITool constructs an OpenAITool. Client creation errors are deferred to Execute
// so that the tool can report user-facing configuration issues at runtime.
func NewOpenAITool(cfg *OpenAIToolConfig) *OpenAITool {
	tool := &OpenAITool{
		name:         cfg.Name,
		description:  cfg.Description,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
		temperature:  cfg.Temperature,
		maxOutput:    cfg.MaxOutput,
		responseJSON: cfg.ResponseJSON,
		apiKey:       strings.TrimSpace(cfg.APIKey),
		apiKeyEnv:    strings.TrimSpace(cfg.APIKeyEnv),
		baseURL:      strings.TrimSpace(cfg.BaseURL),
	}

	if tool.temperature == 0 {
		tool.temperature = 1.0
	}

	tool.tryInitializeClient()

	return tool
}

func buildOpenAIClient(apiKey, baseURL, model string) (llm.Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return llm.NewOpenAIClient(apiKey, model)
	}
	return llm.NewOpenAICompatibleClient(apiKey, baseURL, model)
}

// Name implements Tool.
func (o *OpenAITool) Name() string {
	return o.name
}

// Description implements Tool.
func (o *OpenAITool) Description() string {
	if o.description != "" {
		return o.description
	}
	return fmt.Sprintf("Invoke OpenAI model %s as a tool", o.model)
}

// Parameters implements Tool.
func (o *OpenAITool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Primary prompt to send to the OpenAI model.",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Optional additional context appended to the model conversation.",
			},
			"instructions": map[string]interface{}{
				"type":        "string",
				"description": "Optional override instructions prepended as a system message.",
			},
		},
		"required": []string{"prompt"},
	}
}

// Execute runs the configured OpenAI model and returns the response text.
func (o *OpenAITool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	if o.client == nil {
		o.tryInitializeClient()
	}

	if o.client == nil {
		return &ToolResult{Error: "OpenAI client not configured; ensure API key and model are set"}
	}

	prompt := GetStringParam(params, "prompt", "")
	if strings.TrimSpace(prompt) == "" {
		return &ToolResult{Error: "prompt parameter is required"}
	}

	contextText := GetStringParam(params, "context", "")
	instructions := GetStringParam(params, "instructions", "")

	messages := []*llm.Message{
		{Role: "user", Content: prompt},
	}

	if strings.TrimSpace(contextText) != "" {
		messages = append(messages, &llm.Message{Role: "user", Content: contextText})
	}

	systemPrompt := o.systemPrompt
	if strings.TrimSpace(instructions) != "" {
		if systemPrompt != "" {
			systemPrompt = systemPrompt + "\n\n" + instructions
		} else {
			systemPrompt = instructions
		}
	}

	req := &llm.CompletionRequest{
		Messages:     messages,
		Temperature:  o.temperature,
		MaxTokens:    o.maxOutput,
		SystemPrompt: systemPrompt,
	}

	resp, err := o.client.CompleteWithRequest(ctx, req)
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("OpenAI request failed: %v", err)}
	}

	result := map[string]interface{}{
		"model":    o.model,
		"response": resp.Content,
	}
	if len(resp.ToolCalls) > 0 {
		result["tool_calls"] = resp.ToolCalls
	}
	if o.responseJSON && resp.Content != "" {
		result["note"] = "JSON response requested; ensure the model is prompted accordingly."
	}

	return &ToolResult{Result: result}
}

func (o *OpenAITool) tryInitializeClient() {
	apiKey := o.apiKey
	if apiKey == "" && o.apiKeyEnv != "" {
		apiKey = os.Getenv(o.apiKeyEnv)
	}

	client, err := buildOpenAIClient(apiKey, o.baseURL, o.model)
	if err != nil {
		return
	}
	o.client = client
}
