package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
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

	// Manual check: if last message ends with ':' followed by newlines, auto-continue
	if shouldContinue, reason := checkMessageEndsWithColonNewline(messages); shouldContinue {
		return true, reason
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

	prompt := buildAutoContinueJudgePrompt(userPrompts, recentMessages, systemPrompt, modelID)

	judgeCtx, cancel := context.WithTimeout(ctx, autoContinueJudgeTimeout)
	defer cancel()

	logger.Debug("Calling auto-continue judge with timeout %v", autoContinueJudgeTimeout)
	result, err := o.summarizeClient.Complete(judgeCtx, prompt)
	if err != nil {
		logger.Warn("Auto-continue judge failed: %v", err)
		return false, ""
	}

	decision := strings.TrimSpace(result)
	// Strip <think> tags from reasoning models (e.g., DeepSeek, Qwen 3)
	decision = stripThinkTags(decision)
	decision = strings.TrimSpace(decision)
	if decision == "" {
		logger.Warn("Auto-continue judge returned empty decision")
		return false, ""
	}

	if decision != "CONTINUE" && decision != "STOP" {
		logger.Warn("Summary model decision does not equal exactly what was asked for: %q", decision)
	}

	// For Qwen 3 models, be conservative - only continue on clear cases
	if llm.IsQwen3Model(modelID) {
		upper := strings.ToUpper(decision)
		normalized := strings.TrimSpace(upper)

		if normalized == "CONTINUE" && len(normalized) == 8 {
			logger.Debug("Auto-continue judge decided: CONTINUE (Qwen 3 model - pristine match only, full response: %q)", decision)
			return true, decision
		}

		logger.Debug("Auto-continue judge decided: STOP (Qwen 3 model - conservative approach, normalized: %q, full response: %q)", normalized, decision)
		return false, decision
	}

	// For Mistral models, be extremely conservative - only continue on crystal clear cases
	if llm.IsMistralModel(modelID) {
		upper := strings.ToUpper(decision)
		// Remove any whitespace and normalize for strict comparison
		normalized := strings.TrimSpace(upper)

		// Mistral models should ONLY continue on a pristine "CONTINUE" - no extra words, no ambiguity
		if normalized == "CONTINUE" && len(normalized) == 8 {
			logger.Debug("Auto-continue judge decided: CONTINUE (Mistral model - pristine match only, full response: %q)", decision)
			return true, decision
		}

		// For ANY deviation from pristine "CONTINUE" or any STOP indication, don't continue
		logger.Debug("Auto-continue judge decided: STOP (Mistral model - ultra-conservative approach, normalized: %q, full response: %q)", normalized, decision)
		return false, decision
	}

	// For other models, use the original more permissive logic
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

// checkMessageEndsWithColonNewline checks if the last message ends with a colon followed by one or more newlines.
// This is a manual check that happens before calling the summarization model for auto-continue decisions.
// Returns (true, reason) if the last message ends with ':[newline]*', indicating continuation is required.
func checkMessageEndsWithColonNewline(messages []*session.Message) (bool, string) {
	if len(messages) == 0 {
		return false, ""
	}

	lastMessage := messages[len(messages)-1]
	if lastMessage == nil || lastMessage.Content == "" {
		return false, ""
	}

	content := lastMessage.Content

	// Check if content ends with colon followed by one or more newlines
	// We trim trailing newlines and check if what remains ends with ':'
	trimmed := strings.TrimRight(content, "\r\n")

	// If trimming removed characters and the trimmed version ends with ':', we have a match
	if strings.HasSuffix(trimmed, ":") && len(trimmed) < len(content) {
		logger.Debug("Auto-continue triggered: last message (role=%s) ends with ':[newline]*'", lastMessage.Role)
		return true, "CONTINUE - message ends with colon and newlines"
	}

	return false, ""
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

func buildAutoContinueJudgePrompt(userPrompts []string, messages []*session.Message, systemPrompt, modelID string) string {
	var sb strings.Builder
	sb.WriteString("You are an auto-continue judge. Decide whether the assistant should keep generating its reply.\n")
	sb.WriteString("Respond with exactly one word: CONTINUE or STOP.\n")
	sb.WriteString("Choose CONTINUE when the assistant response appears incomplete, truncated, or when unresolved tasks remain.\n")
	sb.WriteString("Choose STOP when:\n")
	sb.WriteString("- The response is complete or further continuation is unnecessary\n")
	sb.WriteString("- The assistant is repeating the same text or patterns\n")
	sb.WriteString("- The conversation appears to be stuck in a loop\n")
	sb.WriteString("- The assistant is generating repetitive tool calls without making progress\n\n")

	if llm.IsQwen3Model(modelID) {
		sb.WriteString("IMPORTANT: Be conservative in your decision.\n")
		sb.WriteString("Prefer STOP over CONTINUE. Only choose CONTINUE if:\n")
		sb.WriteString("1. The response is visibly cut off mid-sentence or mid-code block\n")
		sb.WriteString("2. There is a clear task that was started but not completed\n")
		sb.WriteString("3. The response ends with obvious truncation indicators\n\n")
		sb.WriteString("Choose STOP for everything else, including:\n")
		sb.WriteString("- Complete responses\n")
		sb.WriteString("- Natural stopping points\n")
		sb.WriteString("- Responses that could continue but don't need to\n\n")
		sb.WriteString("When in doubt, choose STOP.\n\n")
	}

	if llm.IsMistralModel(modelID) {
		sb.WriteString("IMPORTANT: Be extremely conservative in your decision.\n")
		sb.WriteString("Strongly prefer STOP over CONTINUE. Only choose CONTINUE if ALL of these conditions are met:\n")
		sb.WriteString("1. The response is visibly cut off mid-sentence or mid-code block\n")
		sb.WriteString("2. There is an clear task that was started but not completed\n")
		sb.WriteString("3. The response ends with obvious truncation indicators (e.g., incomplete code, hanging parentheses, or 'I'll continue')\n")
		sb.WriteString("4. No ambiguity exists - continuation is absolutely necessary\n\n")
		sb.WriteString("Choose STOP for everything else, including:\n")
		sb.WriteString("- Perfectly complete responses\n")
		sb.WriteString("- Natural stopping points (end of a thought or completed explanation)\n")
		sb.WriteString("- Responses that could continue but don't need to\n")
		sb.WriteString("- Any uncertainty or doubt in your judgment\n\n")
		sb.WriteString("When in doubt, ALWAYS choose STOP.\n\n")
	}

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
	tempDetector := loopdetector.NewLoopDetector()

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
