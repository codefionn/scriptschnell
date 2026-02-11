package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/project"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/tools"
)

// VerificationAgent handles post-execution verification of implementation tasks.
// It runs after the main orchestrator loop stops (LLM EOS + auto-continue didn't trigger)
// to verify that implementation tasks were completed successfully by building/formatting/linting/testing.
type VerificationAgent struct {
	orch *Orchestrator
}

// VerificationResult represents the outcome of verification checks.
type VerificationResult struct {
	Success      bool     `json:"success"`
	BuildPassed  bool     `json:"build_passed"`
	FormatPassed bool     `json:"format_passed"`
	LintPassed   bool     `json:"lint_passed"`
	TestsPassed  bool     `json:"tests_passed"`
	Errors       []string `json:"errors,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	Summary      string   `json:"summary"`
}

// NewVerificationAgent creates a new verification agent.
func NewVerificationAgent(orch *Orchestrator) *VerificationAgent {
	return &VerificationAgent{
		orch: orch,
	}
}

// decideVerificationNeeded determines if verification should run based on the original prompt.
// Returns true if this was an implementation request, false if it was just a question.
func (a *VerificationAgent) decideVerificationNeeded(ctx context.Context, userPrompts []string, filesModified []string) (bool, string, error) {
	// Quick heuristic: if no files were modified, it was likely a question
	if len(filesModified) == 0 {
		return false, "No files were modified - likely a question or information request", nil
	}

	client := a.orch.orchestrationClient
	if client == nil {
		// If no orchestration client, default to running verification when files were modified
		return true, "Files were modified, defaulting to verification", nil
	}

	// Prepare the user prompts summary
	promptsSummary := strings.Join(userPrompts, "\n---\n")
	if len(promptsSummary) > 2000 {
		promptsSummary = promptsSummary[:2000] + "..."
	}

	systemPrompt := `You are a task classifier. Determine if the user's request was:
1. A QUESTION - asking for information, explanation, documentation lookup, or general inquiry
2. An IMPLEMENTATION - requesting code changes, file modifications, bug fixes, or feature additions

Files were modified during this session, which strongly suggests implementation.
However, some queries (like "show me the code for X" followed by accidental modifications) may still be questions.

Respond with ONLY one word: "QUESTION" or "IMPLEMENTATION"`

	messages := []*llm.Message{
		{Role: "user", Content: fmt.Sprintf("User request(s):\n%s\n\nFiles modified: %d files", promptsSummary, len(filesModified))},
	}

	req := &llm.CompletionRequest{
		Messages:     messages,
		SystemPrompt: systemPrompt,
		Temperature:  0,
		MaxTokens:    4096,
	}

	resp, err := client.CompleteWithRequest(ctx, req)
	if err != nil {
		logger.Warn("Verification decision LLM error: %v, defaulting to verification", err)
		return true, "LLM error, defaulting to verification", nil
	}

	decision := strings.TrimSpace(strings.ToUpper(resp.Content))
	if strings.Contains(decision, "QUESTION") {
		return false, "Classified as a question/information request", nil
	}

	return true, "Classified as an implementation request", nil
}

// buildToolRegistry creates a tool registry with read tools and go_sandbox access.
func (a *VerificationAgent) buildToolRegistry(verificationSession *session.Session) *tools.Registry {
	// Use the orchestrator's authorizer (which respects session-level authorizations)
	registry := tools.NewRegistryWithSecrets(a.orch.authorizer, secretdetect.NewDetector())

	// Register tools
	modelFamily := llm.DetectModelFamily(a.orch.providerMgr.GetOrchestrationModel())

	// Read File - essential for checking modified files
	registry.Register(a.orch.getReadFileTool(modelFamily, verificationSession))

	// Search tools - for finding related files
	registry.Register(tools.NewSearchFilesTool(a.orch.fs))
	registry.Register(tools.NewSearchFileContentTool(a.orch.fs))

	// Go Sandbox tool - for running build/lint/test commands and code execution
	sandboxSpec, _ := tools.WrapLegacyTool(tools.NewSandboxToolWithFS(a.orch.workingDir, a.orch.config.TempDir, a.orch.fs, verificationSession, a.orch.shellActorClient))
	sandboxFactory := func(_ *tools.Registry) tools.ToolExecutor {
		instance := tools.NewSandboxToolWithFS(a.orch.workingDir, a.orch.config.TempDir, a.orch.fs, verificationSession, a.orch.shellActorClient)
		a.orch.configureSandboxTool(instance)
		return instance
	}
	registry.RegisterSpec(sandboxSpec, sandboxFactory)

	// Parallel execution tool
	registry.Register(tools.NewParallelTool(registry))

	return registry
}

// buildSystemPrompt creates the system prompt for the verification agent.
func (a *VerificationAgent) buildSystemPrompt(projectTypes []project.ProjectType, filesModified []string, questionsAnswered []session.QuestionAnswer) string {
	var projectInfo string
	if len(projectTypes) > 0 {
		bestMatch := projectTypes[0]
		projectInfo = fmt.Sprintf("- Language/Framework: %s", bestMatch.Name)
		if bestMatch.Description != "" {
			projectInfo += fmt.Sprintf(" (%s)", bestMatch.Description)
		}
	} else {
		projectInfo = "- Language/Framework: Unknown"
	}

	filesModifiedStr := strings.Join(filesModified, "\n  - ")
	if filesModifiedStr != "" {
		filesModifiedStr = "  - " + filesModifiedStr
	}

	// Build answered questions section
	var questionsStr string
	if len(questionsAnswered) > 0 {
		var sb strings.Builder
		for i, qa := range questionsAnswered {
			sb.WriteString(fmt.Sprintf("%d. Q: %s\n   A: %s\n", i+1, qa.Question, qa.Answer))
		}
		questionsStr = sb.String()
	}

	return fmt.Sprintf(`You are a Verification Agent responsible for ensuring implementation tasks were completed successfully.

## Your Goal
Verify that the code changes are correct by:
1. Reading the modified files to check for obvious errors or incomplete changes
2. Running the project build command to ensure it compiles
3. Running the linter (if available) to catch style/quality issues
4. Running relevant tests to ensure functionality works

## Modified Files
%s

## Project Information
%s

## Planning Questions & Answers
%s

## Available Tools
- **go_sandbox**: Execute Go code in a sandboxed environment and run shell commands
- **read_file**: Read files to check modifications
- **search_files/search_file_content**: Find relevant files
- **parallel_tools**: Run multiple checks concurrently

Build commands (via go_sandbox): go build, npm run build, cargo build, make, gradle build, mvn compile
Format commands (via go_sandbox): gofmt, npm run format, cargo fmt, black, prettier
Lint commands (via go_sandbox): golangci-lint, eslint, cargo clippy, ruff, flake8, mypy
Test commands (via go_sandbox): go test, npm test, pytest, cargo test, gradle test

Note: Commands will go through normal authorization checks. The system will determine if user approval is needed.

## Instructions
1. First, briefly review the modified files to understand what changed
2. Run the appropriate build command for this project type
3. Run code formatting to ensure code follows project style conventions
4. Run linting if a linter is configured
5. Run tests - if tests exist in the project, they MUST be run.
6. Report any failures with clear error messages

## Important Notes
- Be efficient: don't read files that weren't modified
- Use the parallel_tools to run independent checks concurrently
- For large test suites, run only relevant tests or use -short flags
- If a command fails, report the error but continue with other checks
- Keep command output concise (avoid verbose flags unless debugging)
- Use go_sandbox for all command execution (build, lint, test)

When complete, provide a summary wrapped in <verification_result> tags:
<verification_result>
{
  "success": true/false,
  "build_passed": true/false,
  "format_passed": true/false,
  "lint_passed": true/false,
  "tests_passed": true/false,
  "errors": ["error1", "error2"],
  "warnings": ["warning1"],
  "summary": "Brief summary of verification results"
}
</verification_result>

If ALL checks pass, you can provide a short success message. If any check fails, provide details about what failed and why.`,
		filesModifiedStr, projectInfo, questionsStr)
}

// extractVerificationResult parses the verification result from the LLM response.
func extractVerificationResult(content string) *VerificationResult {
	// Strip <think> tags from reasoning models
	content = stripThinkTags(content)

	// Look for verification_result tags
	startTag := "<verification_result>"
	endTag := "</verification_result>"

	startIdx := strings.Index(content, startTag)
	endIdx := strings.Index(content, endTag)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		// No tags found, try to infer from content
		result := &VerificationResult{
			Summary: content,
		}
		contentLower := strings.ToLower(content)
		result.Success = !strings.Contains(contentLower, "fail") && !strings.Contains(contentLower, "error")
		result.BuildPassed = result.Success
		result.FormatPassed = result.Success
		result.LintPassed = result.Success
		result.TestsPassed = result.Success
		return result
	}

	jsonContent := strings.TrimSpace(content[startIdx+len(startTag) : endIdx])

	// Fix multiline JSON strings from LLM output
	jsonContent = normalizeJSONStrings(jsonContent)

	var result VerificationResult
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		logger.Debug("Failed to parse verification result JSON: %v", err)
		// Return a basic result
		return &VerificationResult{
			Summary: content,
			Success: !strings.Contains(strings.ToLower(content), "fail"),
		}
	}

	return &result
}

// Verify runs the verification process.
func (a *VerificationAgent) Verify(ctx context.Context, userPrompts []string, filesModified []string, questionsAnswered []session.QuestionAnswer, progressCb progress.Callback) (*VerificationResult, error) {
	// Pre-check: decide if verification is needed
	shouldVerify, reason, err := a.decideVerificationNeeded(ctx, userPrompts, filesModified)
	if err != nil {
		logger.Warn("Error deciding verification need: %v", err)
	}

	if !shouldVerify {
		logger.Debug("Skipping verification: %s", reason)
		return nil, nil
	}

	logger.Info("Running verification: %s", reason)

	sendStatus := func(msg string) {
		dispatchProgress(progressCb, progress.VerificationAgentUpdate(msg, progress.ReportJustStatus))
	}

	sendStream := func(msg string) {
		dispatchProgress(progressCb, progress.VerificationAgentUpdate(msg, progress.ReportNoStatus))
	}

	// Send initial progress message
	sendStream("**Verification Phase**\n\n")
	sendStatus("Running verification checks...")

	// Create a new session for verification
	verificationSession := session.NewSession(session.GenerateID(), a.orch.workingDir)

	// Mark all modified files as read so the verification agent can access them
	// We pass empty content since we only need the "was read" check to pass
	for _, f := range filesModified {
		verificationSession.TrackFileRead(f, "")
	}

	// Create tool registry with go_sandbox and other verification tools
	registry := a.buildToolRegistry(verificationSession)

	// Detect project type
	detector := project.NewDetector(a.orch.workingDir)
	projectTypes, _ := detector.Detect(ctx)

	// Build system prompt
	systemPrompt := a.buildSystemPrompt(projectTypes, filesModified, questionsAnswered)

	// Add initial message
	verificationSession.AddMessage(&session.Message{
		Role:    "user",
		Content: "Please verify that the implementation was completed successfully. Check the modified files, build the project, run linting, and run tests.",
	})

	client := a.orch.orchestrationClient
	if client == nil {
		return nil, fmt.Errorf("orchestration client not available")
	}

	// Initialize loop detector
	loopDetector := NewLoopDetector(12, 3)

	maxTurns := 96

	for i := 0; i < maxTurns; i++ {
		// Prepare messages
		messages := verificationSession.GetMessages()
		llmMessages := make([]*llm.Message, len(messages))
		for j, msg := range messages {
			llmMessages[j] = &llm.Message{
				Role:      msg.Role,
				Content:   msg.Content,
				Reasoning: msg.Reasoning,
				ToolCalls: msg.ToolCalls,
				ToolID:    msg.ToolID,
				ToolName:  msg.ToolName,
			}
		}

		req := &llm.CompletionRequest{
			Messages:      llmMessages,
			Tools:         registry.ToJSONSchema(),
			Temperature:   0,
			MaxTokens:     consts.DefaultMaxTokens,
			SystemPrompt:  systemPrompt,
			EnableCaching: true,
			CacheTTL:      "5m",
		}

		// Execute LLM completion with retry logic for transient errors
		resp, err := a.completeWithRetry(ctx, client, req, sendStatus, sendStream)
		if err != nil {
			return nil, fmt.Errorf("verification LLM error: %w", err)
		}

		// Add assistant response
		verificationSession.AddMessage(&session.Message{
			Role:      "assistant",
			Content:   resp.Content,
			Reasoning: resp.Reasoning,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			// No tools called, this is the final answer
			sendStream("Verification complete.\n\n")
			sendStatus("")
			return extractVerificationResult(resp.Content), nil
		}

		// Check for loops
		for _, tc := range resp.ToolCalls {
			if loopDetector.RecordCall(tc) {
				sendStream("**Warning:** Loop detected in verification, stopping early.\n\n")
				return &VerificationResult{
					Success: false,
					Summary: "Verification stopped due to detected loop in tool calls",
					Errors:  []string{"Loop detected - verification incomplete"},
				}, nil
			}
		}

		// Send progress update for tool calls
		for _, tc := range resp.ToolCalls {
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				toolName, _ := fn["name"].(string)
				argsJSON, _ := fn["arguments"].(string)

				var args map[string]interface{}
				if argsJSON != "" {
					_ = json.Unmarshal([]byte(argsJSON), &args)
				}

				msg := a.formatVerificationToolCall(toolName, args)
				if msg != "" {
					sendStream(msg)
					sendStatus(strings.TrimSpace(msg))
				}
			}
		}

		// Execute tools
		enhancedStatusCb := func(update progress.Update) error {
			if update.Message == "" && !update.ShouldStatus() {
				return nil
			}
			if update.ShouldStatus() {
				update.Ephemeral = true
			}
			return progress.Dispatch(progressCb, progress.Normalize(update))
		}

		execFn := func(execCtx context.Context, call *tools.ToolCall, toolName string, progressCb progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback, approved bool) (*tools.ToolResult, error) {
			return registry.ExecuteWithCallbacks(execCtx, call, toolName, progressCb, toolCallCb, toolResultCb, approved), nil
		}

		err = a.orch.processToolCalls(ctx, resp.ToolCalls, verificationSession, enhancedStatusCb, nil, nil, nil, execFn)
		if err != nil {
			logger.Warn("Verification tool execution error: %v", err)
		}
	}

	return &VerificationResult{
		Success: false,
		Summary: "Verification timed out after maximum turns",
		Errors:  []string{"Verification incomplete - timed out"},
	}, nil
}

// completeWithRetry wraps LLM completion with retry logic for transient errors.
// Uses up to consts.VerificationMaxRetries attempts with exponential backoff.
// This is similar to PlanningAgent's completeWithRetry but adapted for verification context.
func (a *VerificationAgent) completeWithRetry(ctx context.Context, client llm.Client, req *llm.CompletionRequest, statusCb func(string), streamCb func(string)) (*llm.CompletionResponse, error) {
	maxAttempts := consts.VerificationMaxRetries

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		response, err := client.CompleteWithRequest(ctx, req)
		if err == nil {
			return response, nil
		}

		logger.Warn("Verification completion error (attempt %d/%d): %v", attempt, maxAttempts, err)

		// Last attempt - return the error
		if attempt >= maxAttempts {
			return nil, err
		}

		// Check for context cancellation (either via ctx.Err() or if the error itself is a context error)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		// Determine sleep duration based on error type
		errStr := strings.ToLower(err.Error())
		var sleepSeconds int
		switch {
		case strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "429"):
			sleepSeconds = 5 * (1 << uint(attempt-1)) // 5, 10, 20, 40...
			if sleepSeconds > 120 {
				sleepSeconds = 120
			}
		case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline"):
			sleepSeconds = attempt * 3
		case strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
			strings.Contains(errStr, "503") || strings.Contains(errStr, "overloaded"):
			sleepSeconds = attempt * 3
		default:
			sleepSeconds = attempt * 2
		}

		// Notify user about retry
		retryMsg := fmt.Sprintf("\n⏳ Verification: retrying in %d seconds... (attempt %d/%d)\n", sleepSeconds, attempt, maxAttempts)
		if streamCb != nil {
			streamCb(retryMsg)
		}
		if statusCb != nil {
			statusCb(fmt.Sprintf("Verification: retrying in %ds...", sleepSeconds))
		}

		// Sleep before retry
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(sleepSeconds) * time.Second):
			// Continue to retry
		}

		if statusCb != nil {
			statusCb(fmt.Sprintf("Verification: retrying (attempt %d/%d)...", attempt+1, maxAttempts))
		}
	}

	return nil, fmt.Errorf("verification: max retry attempts (%d) exceeded", maxAttempts)
}

// formatVerificationToolCall formats a tool call for display.
func (a *VerificationAgent) formatVerificationToolCall(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "go_sandbox":
		// Check for description parameter first
		if description, ok := args["description"].(string); ok && description != "" {
			// Truncate long descriptions
			if len(description) > 80 {
				description = description[:77] + "..."
			}
			return fmt.Sprintf("→ Running: %s\n", description)
		}
		// Fall back to showing code if no description
		if code, ok := args["code"].(string); ok {
			// Show first line of code or command
			lines := strings.Split(code, "\n")
			if len(lines) > 0 {
				firstLine := strings.TrimSpace(lines[0])
				if len(firstLine) > 80 {
					firstLine = firstLine[:77] + "..."
				}
				if strings.HasPrefix(firstLine, "ExecuteCommand") {
					// Extract shell command from ExecuteCommand
					if idx := strings.Index(firstLine, "\""); idx != -1 {
						if endIdx := strings.Index(firstLine[idx+1:], "\""); endIdx != -1 {
							cmd := firstLine[idx+1 : idx+1+endIdx]
							return fmt.Sprintf("→ Running: `%s`\n", cmd)
						}
					}
				}
				return fmt.Sprintf("→ Running: `%s`\n", firstLine)
			}
		}
		return "→ Running go_sandbox\n"

	case "read_file":
		if path, ok := args["path"].(string); ok {
			return fmt.Sprintf("→ Checking: %s\n", path)
		}
		return "→ Reading file\n"

	case "parallel_tools":
		if toolCalls, ok := args["tool_calls"].([]interface{}); ok {
			return fmt.Sprintf("→ Running %d checks in parallel\n", len(toolCalls))
		}
		return "→ Running parallel checks\n"

	default:
		return fmt.Sprintf("→ %s\n", toolName)
	}
}

// formatVerificationFailureFeedback converts a failed verification result into a user prompt for fixes.
func (a *VerificationAgent) formatVerificationFailureFeedback(result *VerificationResult, attempt int) string {
	if result == nil || result.Success {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("VERIFICATION FAILED (Attempt %d/%d)\n\n", attempt, maxVerificationRetries))

	hasDetails := false
	if result.Summary != "" {
		sb.WriteString("Summary:\n")
		sb.WriteString(result.Summary)
		sb.WriteString("\n\n")
		hasDetails = true
	}

	if len(result.Errors) > 0 {
		sb.WriteString("Errors:\n")
		for _, err := range result.Errors {
			sb.WriteString(fmt.Sprintf("- %s\n", err))
		}
		sb.WriteString("\n")
		hasDetails = true
	}

	if len(result.Warnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, warn := range result.Warnings {
			sb.WriteString(fmt.Sprintf("- %s\n", warn))
		}
		sb.WriteString("\n")
		hasDetails = true
	}

	if !hasDetails {
		sb.WriteString("No verification summary or errors were provided.\n\n")
	}

	sb.WriteString("Please fix the issues above.\n")

	return sb.String()
}

// reportResults formats and sends the verification results to the user.
func (a *VerificationAgent) reportResults(result *VerificationResult, progressCb progress.Callback) {
	if result == nil {
		return
	}

	var sb strings.Builder
	sb.WriteString("\n**Verification Results**\n\n")

	// Status emoji
	if result.Success {
		sb.WriteString("**Status:** All checks passed\n\n")
	} else {
		sb.WriteString("**Status:** Some checks failed\n\n")
	}

	// Individual check results
	sb.WriteString(fmt.Sprintf("- Build: %s\n", statusText(result.BuildPassed)))
	sb.WriteString(fmt.Sprintf("- Format: %s\n", statusText(result.FormatPassed)))
	sb.WriteString(fmt.Sprintf("- Lint: %s\n", statusText(result.LintPassed)))
	sb.WriteString(fmt.Sprintf("- Tests: %s\n", statusText(result.TestsPassed)))

	// Errors
	if len(result.Errors) > 0 {
		sb.WriteString("\n**Errors:**\n")
		for _, err := range result.Errors {
			sb.WriteString(fmt.Sprintf("- %s\n", err))
		}
	}

	// Warnings
	if len(result.Warnings) > 0 {
		sb.WriteString("\n**Warnings:**\n")
		for _, warn := range result.Warnings {
			sb.WriteString(fmt.Sprintf("- %s\n", warn))
		}
	}

	// Summary
	if result.Summary != "" {
		sb.WriteString(fmt.Sprintf("\n**Summary:** %s\n", result.Summary))
	}

	sb.WriteString("\n---\n")

	dispatchProgress(progressCb, progress.VerificationAgentUpdate(sb.String(), progress.ReportNoStatus))
}

func statusText(passed bool) string {
	if passed {
		return "Passed"
	}
	return "Failed"
}
