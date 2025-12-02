package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
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
	hasLoop, loopInfo := checkMessagesForLoops(messages, 10, "assistant")
	if hasLoop {
		reason := fmt.Sprintf("STOP - detected repetitive text pattern in recent messages: %s", loopInfo)
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

// checkMessagesForLoops checks if recent messages contain repetitive text patterns
// Parameters:
//   - messages: All session messages to analyze
//   - maxMessages: Maximum number of recent messages to check (0 = check all)
//   - roleFilter: Filter by role (e.g., "assistant", "user", "tool"), empty string = all roles
//
// Returns:
//   - hasLoop: true if a loop was detected
//   - loopInfo: description of the detected loop (pattern summary and count)
func checkMessagesForLoops(messages []*session.Message, maxMessages int, roleFilter string) (bool, string) {
	if len(messages) == 0 {
		return false, ""
	}

	// Create a temporary loop detector for this check
	tempDetector := NewLoopDetector()

	// Collect recent messages matching the role filter
	limit := maxMessages
	if limit <= 0 {
		limit = len(messages)
	}

	matchedMessages := make([]string, 0, limit)
	for i := len(messages) - 1; i >= 0 && len(matchedMessages) < limit; i-- {
		msg := messages[i]

		// Apply role filter if specified
		if roleFilter != "" && !strings.EqualFold(msg.Role, roleFilter) {
			continue
		}

		content := strings.TrimSpace(msg.Content)
		if content != "" {
			matchedMessages = append(matchedMessages, content)
		}
	}

	if len(matchedMessages) == 0 {
		return false, ""
	}

	// Reverse to process in chronological order
	for i, j := 0, len(matchedMessages)-1; i < j; i, j = i+1, j-1 {
		matchedMessages[i], matchedMessages[j] = matchedMessages[j], matchedMessages[i]
	}

	// Analyze each message for loops
	for _, content := range matchedMessages {
		isLoop, pattern, count := tempDetector.AddText(content)
		if isLoop {
			// Create a summary of the loop
			patternSummary := pattern
			if len(patternSummary) > 80 {
				patternSummary = patternSummary[:80] + "..."
			}
			loopInfo := fmt.Sprintf("pattern repeated %d times: %s", count, patternSummary)
			logger.Debug("Loop detected in recent %s messages: %s", roleFilter, loopInfo)
			return true, loopInfo
		}
	}

	return false, ""
}
