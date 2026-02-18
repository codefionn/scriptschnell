package loop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
)

// LLMJudgeStrategy uses an LLM to make intelligent auto-continue decisions.
// It falls back to pattern matching from DefaultStrategy when the LLM judge is unavailable or fails.
type LLMJudgeStrategy struct {
	DefaultStrategy // Embedded for fallback to pattern matching
	llmClient       llm.Client
	modelID         string
	timeout         time.Duration
	tokenLimit      int
	session         Session // Session to access conversation messages
}

// NewLLMJudgeStrategy creates a new LLMJudgeStrategy with the specified configuration and LLM client.
// The strategy will use the LLM to make intelligent auto-continue decisions,
// falling back to pattern matching if the LLM is unavailable or returns an error.
func NewLLMJudgeStrategy(config *Config, llmClient llm.Client, modelID string, session Session) *LLMJudgeStrategy {
	if config == nil {
		config = DefaultConfig()
	}

	timeout := config.LLMAutoContinueJudgeTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	tokenLimit := config.LLMAutoContinueJudgeTokenLimit
	if tokenLimit == 0 {
		tokenLimit = 1000
	}

	return &LLMJudgeStrategy{
		DefaultStrategy: *NewDefaultStrategy(config),
		llmClient:       llmClient,
		modelID:         modelID,
		timeout:         timeout,
		tokenLimit:      tokenLimit,
		session:         session,
	}
}

// ShouldAutoContinue determines if auto-continue should be triggered.
// It first checks the standard limits, then queries the LLM judge if available.
// Falls back to pattern matching on any LLM failure.
func (s *LLMJudgeStrategy) ShouldAutoContinue(state State, content string) bool {
	// First check standard limits (via embedded DefaultStrategy)
	if !s.DefaultStrategy.config.EnableAutoContinue {
		return false
	}
	if state.HasReachedAutoContinueLimit() {
		return false
	}

	// If LLM judge is disabled or not available, use pattern matching fallback
	if !s.DefaultStrategy.config.EnableLLMAutoContinueJudge || s.llmClient == nil || s.modelID == "" {
		logger.Debug("LLM judge disabled or unavailable, using pattern matching fallback")
		return s.DefaultStrategy.ShouldAutoContinue(state, content)
	}

	// Get messages from session for loop detection
	var messages []Message
	if s.session != nil {
		messages = s.session.GetMessages()
	}

	// Check for text loops in recent assistant messages BEFORE any LLM call
	// This is a hard stop - if loop detected, don't allow auto-continue
	if len(messages) > 0 {
		hasLoop, loopInfo := checkMessagesForLoops(messages, 10, "assistant")
		if hasLoop {
			reason := fmt.Sprintf("STOP - detected repetitive text pattern in recent messages: %s", loopInfo)
			logger.Info("Auto-continue blocked by loop detection: %s", reason)
			return false
		}
	}

	// Try LLM judge with fallback
	shouldContinue := s.queryLLMJudge(state, content)

	// If LLM judge returns false, still try pattern matching as a safety net
	// This ensures we don't miss obvious incomplete patterns
	if !shouldContinue {
		return s.DefaultStrategy.ShouldAutoContinue(state, content)
	}

	return true
}

// queryLLMJudge queries the LLM to determine if auto-continue should happen.
// Returns false on any error, causing fallback to pattern matching.
func (s *LLMJudgeStrategy) queryLLMJudge(state State, content string) bool {
	logger.Debug("LLM judge evaluating auto-continue decision")

	// Get messages from session
	var messages []Message
	if s.session != nil {
		messages = s.session.GetMessages()
	}

	if len(messages) == 0 {
		logger.Debug("LLM judge skipped: no messages in session")
		return false
	}

	// Manual check: if last message ends with ':' followed by newlines, auto-continue
	if shouldContinue, reason := checkMessageEndsWithColonNewline(messages); shouldContinue {
		logger.Debug("Auto-continue via manual check: %s", reason)
		return true
	}

	userPrompts := collectRecentUserPrompts(messages, 10)
	if len(userPrompts) == 0 {
		logger.Debug("LLM judge skipped: no user prompts found")
		return false
	}

	recentMessages := selectRecentMessagesByTokenLimit(messages, s.tokenLimit)
	if len(recentMessages) == 0 {
		recentMessages = messages
	}
	logger.Debug("LLM judge analyzing %d recent messages (from %d total)", len(recentMessages), len(messages))

	// Build the judge prompt
	prompt := buildAutoContinueJudgePrompt(userPrompts, recentMessages, s.modelID)

	// Query LLM with timeout
	judgeCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	logger.Debug("Calling LLM judge with timeout %v", s.timeout)
	result, err := s.llmClient.Complete(judgeCtx, prompt)
	if err != nil {
		logger.Warn("LLM judge failed: %v, falling back to pattern matching", err)
		return false
	}

	decision := strings.TrimSpace(result)
	// Strip <think> tags from reasoning models (e.g., DeepSeek, Qwen 3)
	decision = stripThinkTags(decision)
	decision = strings.TrimSpace(decision)
	if decision == "" {
		logger.Warn("LLM judge returned empty decision, falling back to pattern matching")
		return false
	}

	if decision != "CONTINUE" && decision != "STOP" {
		logger.Warn("LLM judge returned unexpected response: %q, treating as STOP", decision)
	}

	// Model-specific decision logic
	if llm.IsQwen3Model(s.modelID) {
		return s.evaluateQwen3Decision(decision)
	}

	if llm.IsMistralModel(s.modelID) {
		return s.evaluateMistralDecision(decision)
	}

	// For other models, use more permissive logic
	return s.evaluateGeneralDecision(decision)
}

// evaluateQwen3Decision evaluates the decision for Qwen 3 models.
// Uses conservative approach - only continue on pristine "CONTINUE".
func (s *LLMJudgeStrategy) evaluateQwen3Decision(decision string) bool {
	upper := strings.ToUpper(decision)
	normalized := strings.TrimSpace(upper)

	if normalized == "CONTINUE" && len(normalized) == 8 {
		logger.Debug("LLM judge decided: CONTINUE (Qwen 3 model - pristine match only)")
		return true
	}

	logger.Debug("LLM judge decided: STOP (Qwen 3 model - conservative approach)")
	return false
}

// evaluateMistralDecision evaluates the decision for Mistral models.
// Uses ultra-conservative approach - only continue on pristine "CONTINUE".
func (s *LLMJudgeStrategy) evaluateMistralDecision(decision string) bool {
	upper := strings.ToUpper(decision)
	normalized := strings.TrimSpace(upper)

	if normalized == "CONTINUE" && len(normalized) == 8 {
		logger.Debug("LLM judge decided: CONTINUE (Mistral model - pristine match only)")
		return true
	}

	logger.Debug("LLM judge decided: STOP (Mistral model - ultra-conservative approach)")
	return false
}

// evaluateGeneralDecision evaluates the decision for general models.
// Uses more permissive logic - allows "CONTINUE" to appear anywhere.
func (s *LLMJudgeStrategy) evaluateGeneralDecision(decision string) bool {
	upper := strings.ToUpper(decision)
	fields := strings.Fields(upper)
	head := upper
	if len(fields) > 0 {
		head = fields[0]
	}

	switch head {
	case "CONTINUE":
		logger.Debug("LLM judge decided: CONTINUE (general model)")
		return true
	case "STOP":
		logger.Debug("LLM judge decided: STOP (general model)")
		return false
	default:
		if strings.Contains(upper, "CONTINUE") && !strings.Contains(upper, "DO NOT CONTINUE") {
			logger.Debug("LLM judge decided: CONTINUE (general model - heuristic match)")
			return true
		}
		logger.Debug("LLM judge decided: STOP (general model - no match)")
		return false
	}
}

// checkMessageEndsWithColonNewline checks if the last message ends with a colon followed by one or more newlines.
// This is a manual check that happens before calling the LLM judge.
func checkMessageEndsWithColonNewline(messages []Message) (bool, string) {
	if len(messages) == 0 {
		return false, ""
	}

	lastMessage := messages[len(messages)-1]
	if lastMessage == nil {
		return false, ""
	}

	content := lastMessage.GetContent()
	if content == "" {
		return false, ""
	}

	// Check if content ends with colon followed by one or more newlines
	trimmed := strings.TrimRight(content, "\r\n")

	// If trimming removed characters and the trimmed version ends with ':', we have a match
	if strings.HasSuffix(trimmed, ":") && len(trimmed) < len(content) {
		logger.Debug("Auto-continue triggered: last message (role=%s) ends with ':[newline]*'", lastMessage.GetRole())
		return true, "CONTINUE - message ends with colon and newlines"
	}

	return false, ""
}

// collectRecentUserPrompts collects recent user prompts from messages.
func collectRecentUserPrompts(messages []Message, limit int) []string {
	if limit <= 0 {
		return nil
	}

	prompts := make([]string, 0, limit)
	for i := len(messages) - 1; i >= 0 && len(prompts) < limit; i-- {
		if !strings.EqualFold(messages[i].GetRole(), "user") {
			continue
		}

		text := strings.TrimSpace(messages[i].GetContent())
		if text == "" {
			text = "(empty)"
		}

		prompts = append(prompts, text)
	}

	// Reverse to get chronological order
	for i, j := 0, len(prompts)-1; i < j; i, j = i+1, j-1 {
		prompts[i], prompts[j] = prompts[j], prompts[i]
	}

	return prompts
}

// selectRecentMessagesByTokenLimit selects recent messages up to a token limit.
// This is a simplified version that estimates tokens based on message length.
func selectRecentMessagesByTokenLimit(messages []Message, tokenLimit int) []Message {
	if tokenLimit <= 0 || len(messages) == 0 {
		return nil
	}

	// Simple token estimation: 4 characters ~= 1 token
	estimatedTokens := func(content string) int {
		return len(content) / 4
	}

	total := 0
	start := len(messages)

	for i := len(messages) - 1; i >= 0; i-- {
		tokens := estimatedTokens(messages[i].GetContent())
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

// buildAutoContinueJudgePrompt creates the prompt for the auto-continue judge.
func buildAutoContinueJudgePrompt(userPrompts []string, messages []Message, modelID string) string {
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

	if len(userPrompts) > 0 {
		sb.WriteString("Recent user prompts (oldest to newest):\n")
		for i, prompt := range userPrompts {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, prompt))
		}
	} else {
		sb.WriteString("Recent user prompts: (none)\n")
	}

	sb.WriteString("\nConversation excerpt (most recent context):\n")

	if len(messages) == 0 {
		sb.WriteString("(no messages)\n")
	} else {
		for _, msg := range messages {
			role := msg.GetRole()
			content := strings.TrimSpace(msg.GetContent())
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

// stripThinkTags removes <think>...</think> tags from reasoning model outputs.
func stripThinkTags(text string) string {
	// Remove opening tag
	text = strings.ReplaceAll(text, "<think>", "")
	// Remove closing tag
	text = strings.ReplaceAll(text, "</think>", "")
	// Remove opening tag with variant
	text = strings.ReplaceAll(text, "<think >", "")
	return text
}

// checkMessagesForLoops checks if recent messages contain repetitive text patterns.
// Parameters:
//   - messages: All session messages to analyze
//   - maxMessages: Maximum number of recent messages to check (0 = check all)
//   - roleFilter: Filter by role (e.g., "assistant", "user", "tool"), empty string = all roles
//
// Returns:
//   - hasLoop: true if a loop was detected
//   - loopInfo: description of the detected loop (pattern summary and count)
func checkMessagesForLoops(messages []Message, maxMessages int, roleFilter string) (bool, string) {
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
		if roleFilter != "" && !strings.EqualFold(msg.GetRole(), roleFilter) {
			continue
		}

		content := strings.TrimSpace(msg.GetContent())
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
