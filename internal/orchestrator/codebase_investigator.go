package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/llm"
	"github.com/statcode-ai/statcode-ai/internal/session"
	"github.com/statcode-ai/statcode-ai/internal/tools"
)

type CodebaseInvestigatorAgent struct {
	orch *Orchestrator
}

func NewCodebaseInvestigatorAgent(orch *Orchestrator) *CodebaseInvestigatorAgent {
	return &CodebaseInvestigatorAgent{
		orch: orch,
	}
}

func (a *CodebaseInvestigatorAgent) Investigate(ctx context.Context, objective string) (string, error) {
	// Create a new session for the investigation
	investigationSession := session.NewSession("investigation", a.orch.workingDir)
	investigationSession.AddMessage(&session.Message{
		Role:    "user",
		Content: fmt.Sprintf("Investigation Objective: %s", objective),
	})

	// Create limited registry
	registry := tools.NewRegistry(nil) // No authorizer needed for internal safe tools

	// Register tools
	modelFamily := llm.DetectModelFamily(a.orch.getSummarizeModelID())

	// Read File
	registry.Register(a.orch.getReadFileTool(modelFamily, investigationSession))

	// Search tools
	registry.Register(tools.NewSearchFilesTool(a.orch.fs))
	registry.Register(tools.NewSearchFileContentTool(a.orch.fs))

	// System prompt for investigator
	systemPrompt := `You are a Codebase Investigator agent.
Your goal is to explore the codebase to answer the user's objective.
You have access to tools to search and read files.
You should systematically explore relevant files.
When you have found the answer or gathered enough information, provide a comprehensive summary wrapped in <answer> tags.
If you cannot find the answer, explain what you checked and why you failed, also wrapped in <answer> tags.
Example:
<answer>
The requested logic is found in internal/module/file.go function DoWork().
</answer>`

	client := a.orch.summarizeClient
	if client == nil {
		return "", fmt.Errorf("summarization client not available")
	}

	maxTurns := 64

	for i := 0; i < maxTurns; i++ {
		// Prepare messages
		messages := investigationSession.GetMessages()
		llmMessages := make([]*llm.Message, len(messages))
		for j, msg := range messages {
			llmMessages[j] = &llm.Message{
				Role:      msg.Role,
				Content:   msg.Content,
				ToolCalls: msg.ToolCalls,
				ToolID:    msg.ToolID,
				ToolName:  msg.ToolName,
			}
		}

		req := &llm.CompletionRequest{
			Messages:     llmMessages,
			Tools:        registry.ToJSONSchema(),
			Temperature:  0,
			MaxTokens:    4096, // Reasonable limit for investigation steps
			SystemPrompt: systemPrompt,
		}

		resp, err := client.CompleteWithRequest(ctx, req)
		if err != nil {
			return "", fmt.Errorf("investigator LLM error: %w", err)
		}

		// Add assistant response
		investigationSession.AddMessage(&session.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			// No tools called, this is the final answer
			return extractAnswer(resp.Content), nil
		}

		// Execute tools using extracted logic
		// We use a simple adapter for the registry
		executor := &registryExecutor{registry: registry}

		// Use the extracted processToolCalls method
		// We pass nil for callbacks as we don't want to spam the main UI with sub-agent tool calls,
		// or maybe we do? For now, keep it silent or log only.
		err = a.orch.processToolCalls(ctx, resp.ToolCalls, executor, investigationSession, nil, nil, nil, nil)
		if err != nil {
			return "", fmt.Errorf("tool execution failed: %w", err)
		}
	}

	return "Investigation timed out after maximum turns.", nil
}

type registryExecutor struct {
	registry *tools.Registry
}

func (e *registryExecutor) Execute(ctx context.Context, call *tools.ToolCall, toolName string, statusCb StatusCallback) (*tools.ToolResult, error) {
	return e.registry.Execute(ctx, call), nil
}

func (e *registryExecutor) ExecuteWithApproval(ctx context.Context, call *tools.ToolCall, toolName string, statusCb StatusCallback) (*tools.ToolResult, error) {
	return e.registry.ExecuteWithApproval(ctx, call), nil
}

func extractAnswer(content string) string {
	startTag := "<answer>"
	endTag := "</answer>"
	start := strings.Index(content, startTag)
	if start == -1 {
		return content
	}
	content = content[start+len(startTag):]
	end := strings.LastIndex(content, endTag)
	if end == -1 {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(content[:end])
}
