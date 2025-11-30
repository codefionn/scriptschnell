package llm

// convertRequestToOpenAI is a helper shared between native OpenAI and OpenAI-compatible
// clients. It converts a CompletionRequest into an openAIChatRequest, injecting the
// system prompt as the first message and normalizing roles/tool calls.
func convertRequestToOpenAI(req *CompletionRequest, model string, stream bool, enforceOpenAITemperature bool) (*openAIChatRequest, error) {
	messages, err := convertMessagesToOpenAI(req)
	if err != nil {
		return nil, err
	}

	payload := &openAIChatRequest{
		Model:    model,
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
		payload.Tools = req.Tools
	}

	return payload, nil
}
