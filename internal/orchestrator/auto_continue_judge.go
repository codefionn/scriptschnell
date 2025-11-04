package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/session"
)

const (
	autoContinueTokenLimit   = 1000
	autoContinueJudgeTimeout = 15 * time.Second
)

func (o *Orchestrator) shouldAutoContinue(ctx context.Context, systemPrompt string) (bool, string) {
	if o.summarizeClient == nil {
		logger.Debug("Auto-continue skipped: no summarize client")
		return false, ""
	}

	modelID := o.getSummarizeModelID()
	if modelID == "" {
		logger.Debug("Auto-continue skipped: no summarize-capable model configured")
		return false, ""
	}

	messages := o.session.GetMessages()
	if len(messages) == 0 {
		logger.Debug("Auto-continue skipped: no messages in session")
		return false, ""
	}

	// Check for text loops in recent assistant messages
	if hasLoopInRecentMessages(messages) {
		reason := "STOP - detected repetitive text pattern in recent messages, continuing would likely produce more loops"
		logger.Info("Auto-continue blocked: %s", reason)
		return false, reason
	}

	userPrompts := collectRecentUserPrompts(messages, 10)
	if len(userPrompts) == 0 {
		logger.Debug("Auto-continue skipped: no user prompts found")
		return false, ""
	}

	recentMessages := selectRecentMessagesByTokens(modelID, messages, autoContinueTokenLimit)
	if len(recentMessages) == 0 {
		recentMessages = messages
	}
	logger.Debug("Auto-continue judge analyzing %d recent messages (from %d total)", len(recentMessages), len(messages))

	prompt := buildAutoContinueJudgePrompt(userPrompts, recentMessages, systemPrompt)

	judgeCtx, cancel := context.WithTimeout(ctx, autoContinueJudgeTimeout)
	defer cancel()

	logger.Debug("Calling auto-continue judge with timeout %v", autoContinueJudgeTimeout)
	result, err := o.summarizeClient.Complete(judgeCtx, prompt)
	if err != nil {
		logger.Warn("Auto-continue judge failed: %v", err)
		return false, ""
	}

	decision := strings.TrimSpace(result)
	if decision == "" {
		logger.Warn("Auto-continue judge returned empty decision")
		return false, ""
	}

	upper := strings.ToUpper(decision)
	fields := strings.Fields(upper)
	head := upper
	if len(fields) > 0 {
		head = fields[0]
	}

	switch head {
	case "CONTINUE":
		logger.Debug("Auto-continue judge decided: CONTINUE (full response: %q)", decision)
		return true, decision
	case "STOP":
		logger.Debug("Auto-continue judge decided: STOP (full response: %q)", decision)
		return false, decision
	default:
		if strings.Contains(upper, "CONTINUE") && !strings.Contains(upper, "DO NOT CONTINUE") {
			logger.Debug("Auto-continue judge decided: CONTINUE (heuristic match, full response: %q)", decision)
			return true, decision
		}
		logger.Debug("Auto-continue judge decided: STOP (no match, full response: %q)", decision)
	}

	return false, decision
}

func collectRecentUserPrompts(messages []*session.Message, limit int) []string {
	if limit <= 0 {
		return nil
	}

	prompts := make([]string, 0, limit)
	for i := len(messages) - 1; i >= 0 && len(prompts) < limit; i-- {
		if !strings.EqualFold(messages[i].Role, "user") {
			continue
		}

		text := strings.TrimSpace(messages[i].Content)
		if text == "" {
			text = "(empty)"
		}

		prompts = append(prompts, text)
	}

	for i, j := 0, len(prompts)-1; i < j; i, j = i+1, j-1 {
		prompts[i], prompts[j] = prompts[j], prompts[i]
	}

	return prompts
}

func selectRecentMessagesByTokens(modelID string, messages []*session.Message, tokenLimit int) []*session.Message {
	if tokenLimit <= 0 || len(messages) == 0 {
		return nil
	}

	_, perMessageTokens, _ := estimateContextTokens(modelID, "", messages)

	total := 0
	start := len(messages)

	for i := len(messages) - 1; i >= 0; i-- {
		tokens := perMessageTokens[i]
		if tokens <= 0 {
			tokens = 1
		}

		if total+tokens > tokenLimit && start < len(messages) {
			break
		}

		total += tokens
		start = i

		if total >= tokenLimit {
			break
		}
	}

	return messages[start:]
}

func buildAutoContinueJudgePrompt(userPrompts []string, messages []*session.Message, systemPrompt string) string {
	var sb strings.Builder
	sb.WriteString("You are an auto-continue judge. Decide whether the assistant should keep generating its reply.\n")
	sb.WriteString("Respond with exactly one word: CONTINUE or STOP.\n")
	sb.WriteString("Choose CONTINUE when the assistant response appears incomplete, truncated, or when unresolved tasks remain.\n")
	sb.WriteString("Choose STOP when:\n")
	sb.WriteString("- The response is complete or further continuation is unnecessary\n")
	sb.WriteString("- The assistant is repeating the same text or patterns\n")
	sb.WriteString("- The conversation appears to be stuck in a loop\n")
	sb.WriteString("- The assistant is generating repetitive tool calls without making progress\n\n")

	trimmedSystemPrompt := strings.TrimSpace(systemPrompt)
	if trimmedSystemPrompt == "" {
		sb.WriteString("System prompt: (unavailable)\n")
	} else {
		sb.WriteString("System prompt (includes project context):\n")
		sb.WriteString(trimmedSystemPrompt)
		if !strings.HasSuffix(trimmedSystemPrompt, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	if len(userPrompts) > 0 {
		sb.WriteString("Recent user prompts (oldest to newest):\n")
		for i, prompt := range userPrompts {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, prompt))
		}
	} else {
		sb.WriteString("Recent user prompts: (none)\n")
	}

	sb.WriteString("\nConversation excerpt (most recent context, approx last ")
	sb.WriteString(fmt.Sprintf("%d tokens):\n", autoContinueTokenLimit))

	if len(messages) == 0 {
		sb.WriteString("(no messages)\n")
	} else {
		for _, msg := range messages {
			role := formatRoleLabel(msg)
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				content = "(no content)"
			}

			sb.WriteString(role)
			sb.WriteString(": ")
			sb.WriteString(content)
			sb.WriteString("\n---\n")
		}
	}

	sb.WriteString("\nReply with exactly one word: CONTINUE or STOP.")
	return sb.String()
}

// hasLoopInRecentMessages checks if recent assistant messages contain repetitive text patterns
func hasLoopInRecentMessages(messages []*session.Message) bool {
	if len(messages) == 0 {
		return false
	}

	// Create a temporary loop detector for this check
	tempDetector := NewLoopDetector()

	// Collect recent assistant messages (last 10)
	assistantMessages := make([]string, 0, 10)
	for i := len(messages) - 1; i >= 0 && len(assistantMessages) < 10; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			content := strings.TrimSpace(messages[i].Content)
			if content != "" {
				assistantMessages = append(assistantMessages, content)
			}
		}
	}

	if len(assistantMessages) == 0 {
		return false
	}

	// Reverse to process in chronological order
	for i, j := 0, len(assistantMessages)-1; i < j; i, j = i+1, j-1 {
		assistantMessages[i], assistantMessages[j] = assistantMessages[j], assistantMessages[i]
	}

	// Analyze each message for loops
	for _, content := range assistantMessages {
		isLoop, _, _ := tempDetector.AddText(content)
		if isLoop {
			logger.Debug("Loop detected in recent assistant messages")
			return true
		}
	}

	return false
}
