# OpenAI-Compatible Provider

This document describes how to use the OpenAI-compatible provider in StatCode AI to connect to any API service that implements the OpenAI API specification.

## Overview

The OpenAI-compatible provider allows you to connect StatCode AI to:

- **Local LLMs**: LM Studio, LocalAI, text-generation-webui with OpenAI extension, Ollama with OpenAI compatibility layer
- **Self-hosted deployments**: vLLM, TGI (Text Generation Inference), FastChat, Aphrodite Engine
- **Custom API gateways**: Any service providing OpenAI-compatible endpoints
- **Alternative cloud services**: Services that mirror the OpenAI API structure

## Adding via TUI Menu

The easiest way to add an OpenAI-compatible provider is through the TUI:

1. Launch StatCode AI:
   ```bash
   ./statcode-ai
   ```

2. Navigate to the provider menu (usually accessible from settings)

3. Select "Add OpenAI-Compatible Provider"

4. Enter the required information:
   - **Base URL**: The API endpoint (e.g., `http://localhost:1234/v1` for LM Studio)
   - **API Key**: Optional - leave empty for local servers without authentication

5. Press Enter to connect and fetch available models

The provider will automatically:
- Connect to the API's `/v1/models` endpoint
- Fetch the list of available models
- Estimate context windows and capabilities based on model names
- Save the configuration to `~/.config/statcode-ai/providers.json`

## Manual Configuration

You can also manually edit the configuration file at `~/.config/statcode-ai/providers.json`:

```json
{
  "providers": {
    "openai-compatible": {
      "name": "openai-compatible",
      "api_key": "",
      "base_url": "http://localhost:1234/v1",
      "models": []
    }
  },
  "orchestration_model": "",
  "summarize_model": ""
}
```

### Configuration Fields

- `name`: Must be `"openai-compatible"`
- `api_key`: Your API key (leave empty `""` for local servers)
- `base_url`: The API endpoint URL (required)
- `models`: Leave as empty array `[]` - will be populated automatically

After editing the file, restart StatCode AI or use the refresh models option.

## Common Base URLs

### LM Studio
- Default: `http://localhost:1234/v1`
- Custom port: `http://localhost:PORT/v1`

### LocalAI
- Default: `http://localhost:8080/v1`
- Custom: `http://your-server:port/v1`

### text-generation-webui
- Default: `http://localhost:5000/v1`
- Requires the `openai` extension to be enabled

### vLLM
- Default: `http://localhost:8000/v1`
- Custom deployment: `http://your-server:port/v1`

### Ollama (with OpenAI compatibility)
- Default: `http://localhost:11434/v1`
- Note: For native Ollama support, use the dedicated Ollama provider instead

## Intelligent Model Detection

The provider automatically detects and configures models based on their names:

### Context Window Estimation

- **Llama 3.3/3.2**: 128k context
- **Llama 3.1**: 128k context
- **Llama 3.0**: 8k context
- **Mistral Large**: 128k context
- **Mistral Small/Medium**: 32k context
- **Mixtral 8x22B**: 64k context
- **Mixtral 8x7B**: 32k context
- **Qwen 2.5**: 128k context
- **DeepSeek**: 64k context
- **Explicit indicators**: Respects `32k`, `64k`, `128k` in model names

### Max Output Tokens

- **Modern models** (DeepSeek, Qwen 2.5, Mistral Large, Llama 3.x): 8k tokens
- **Default**: 4k tokens

## Features

✅ **Automatic model discovery** - Fetches models from the API endpoint
✅ **Optional authentication** - API key can be empty for local servers
✅ **Smart estimation** - Context windows estimated based on model families
✅ **Tool calling support** - Enabled by default for modern models
✅ **Streaming support** - Full streaming capabilities
✅ **Flexible URLs** - Supports any valid base URL with `/v1` path

## Troubleshooting

### Connection Failed

1. **Verify the base URL**: Ensure the server is running and accessible
2. **Check the port**: Make sure the port matches your server configuration
3. **Test manually**: Try `curl http://localhost:1234/v1/models` to verify the API responds

### No Models Found

1. **Check API endpoint**: Ensure `/v1/models` returns a valid response
2. **Verify compatibility**: The server must implement OpenAI's models list endpoint
3. **Check filters**: Some models may be filtered out (embeddings, TTS, etc.)

### Authentication Errors

1. **API key required**: If the server requires auth, provide an API key
2. **API key format**: Ensure the key is correct (no extra spaces or quotes)
3. **Server configuration**: Check if the server expects `Bearer` token format

### Models Not Working

1. **Tool calling**: Ensure the model supports function calling if using tools
2. **Context length**: Verify the estimated context window matches the actual model
3. **Temperature**: Some models may not support custom temperature settings

## Example Configurations

### LM Studio (Local, No Auth)
```json
{
  "name": "openai-compatible",
  "api_key": "",
  "base_url": "http://localhost:1234/v1",
  "models": []
}
```

### vLLM (Remote, With Auth)
```json
{
  "name": "openai-compatible",
  "api_key": "your-api-key-here",
  "base_url": "https://your-vllm-server.com/v1",
  "models": []
}
```

### text-generation-webui (Custom Port)
```json
{
  "name": "openai-compatible",
  "api_key": "",
  "base_url": "http://localhost:5000/v1",
  "models": []
}
```

## API Compatibility Requirements

For a service to work with this provider, it must implement:

1. **Models endpoint**: `GET /v1/models`
   - Returns a list of available models
   - Format: `{"data": [{"id": "model-name", ...}]}`

2. **Chat completions endpoint**: `POST /v1/chat/completions`
   - Accepts OpenAI-compatible chat completion requests
   - Supports streaming (optional)

3. **Authentication** (optional): `Authorization: Bearer <token>` header

## Notes

- The provider name in the menu will appear as "OpenAI-Compatible"
- Multiple instances are not supported - only one OpenAI-compatible provider can be configured
- For native Ollama support with better integration, use the dedicated Ollama provider
- The base URL should include the `/v1` path (e.g., `http://localhost:1234/v1`)
- Trailing slashes in the base URL are automatically removed

## Support

For issues or questions:
- Check the logs at `~/.config/statcode-ai/statcode-ai.log`
- Verify your server implements the OpenAI API specification
- Ensure the `/v1/models` endpoint is accessible and returns valid data
