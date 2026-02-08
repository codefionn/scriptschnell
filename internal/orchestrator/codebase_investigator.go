package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/consts"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/project"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/summarizer"
	"github.com/codefionn/scriptschnell/internal/tools"
)

type CodebaseInvestigatorAgent struct {
	orch *Orchestrator
}

const codebaseInvestigatorOutputPercent = 0.2

func NewCodebaseInvestigatorAgent(orch *Orchestrator) *CodebaseInvestigatorAgent {
	return &CodebaseInvestigatorAgent{
		orch: orch,
	}
}

// ToolCallRecord tracks a tool call for loop detection
type ToolCallRecord struct {
	Timestamp time.Time
	ToolName  string
	Arguments map[string]interface{}
}

// LoopDetector detects repeated tool calls to prevent infinite loops
type LoopDetector struct {
	callHistory []ToolCallRecord
	maxHistory  int
	threshold   int // Number of repetitions to consider a loop
}

// NewLoopDetector creates a new loop detector
func NewLoopDetector(maxHistory, threshold int) *LoopDetector {
	return &LoopDetector{
		callHistory: make([]ToolCallRecord, 0, maxHistory),
		maxHistory:  maxHistory,
		threshold:   threshold,
	}
}

// RecordCall records a tool call and checks for loops
func (ld *LoopDetector) RecordCall(toolCall map[string]interface{}) bool {
	if fn, ok := toolCall["function"].(map[string]interface{}); ok {
		toolName := ""
		arguments := make(map[string]interface{})

		if name, found := fn["name"].(string); found {
			toolName = name
		}
		if args, found := fn["arguments"].(string); found {
			// Parse JSON arguments
			parsed := make(map[string]interface{})
			if err := json.Unmarshal([]byte(args), &parsed); err == nil {
				arguments = parsed
			}
		}

		record := ToolCallRecord{
			Timestamp: time.Now(),
			ToolName:  toolName,
			Arguments: arguments,
		}

		// Add to history
		ld.callHistory = append(ld.callHistory, record)

		// Keep history size under control
		if len(ld.callHistory) > ld.maxHistory {
			ld.callHistory = ld.callHistory[1:]
		}

		// Check for loops by counting recent identical calls
		return ld.checkForLoops(&record)
	}
	return false
}

// checkForLoops checks if the current call repeats too frequently
func (ld *LoopDetector) checkForLoops(current *ToolCallRecord) bool {
	// Count identical calls in recent history
	count := 0
	for i := len(ld.callHistory) - 1; i >= 0; i-- {
		record := &ld.callHistory[i]
		// Compare tool name and arguments
		if record.ToolName == current.ToolName && ld.argsEqual(record.Arguments, current.Arguments) {
			count++
		} else {
			break // Stop counting when we hit a different call type
		}
	}

	// Consider it a loop if we've seen this exact call too many times
	return count >= ld.threshold
}

// argsEqual checks if two argument maps are equal
func (ld *LoopDetector) argsEqual(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for key, valA := range a {
		valB, exists := b[key]
		if !exists {
			return false
		}
		// Simple equality check - might need refinement for complex types
		if fmt.Sprintf("%v", valA) != fmt.Sprintf("%v", valB) {
			return false
		}
	}
	return true
}

func (a *CodebaseInvestigatorAgent) Investigate(ctx context.Context, objectives []string) ([]string, error) {
	if len(objectives) == 0 {
		return nil, fmt.Errorf("at least one objective is required")
	}

	// For a single objective, run directly
	if len(objectives) == 1 {
		result, err := a.investigateInternal(ctx, objectives[0], nil)
		if err != nil {
			return nil, err
		}
		return []string{result}, nil
	}

	// For multiple objectives, run concurrently
	type investigationResult struct {
		index  int
		result string
		err    error
	}

	resultChan := make(chan investigationResult, len(objectives))

	for i, objective := range objectives {
		go func(idx int, obj string) {
			result, err := a.investigateInternal(ctx, obj, nil)
			resultChan <- investigationResult{index: idx, result: result, err: err}
		}(i, objective)
	}

	// Collect results
	results := make([]string, len(objectives))
	var firstErr error
	for range objectives {
		res := <-resultChan
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		results[res.index] = res.result
	}

	if firstErr != nil {
		return results, firstErr
	}

	return results, nil
}

func (a *CodebaseInvestigatorAgent) investigateInternal(ctx context.Context, objective string, progressCb progress.Callback) (string, error) {
	// If no callback provided, try to get it from the orchestrator's current context
	if progressCb == nil {
		progressCb = a.orch.GetCurrentProgressCallback()
	}

	// Initialize loop detector for this investigation
	loopDetector := NewLoopDetector(12, 3) // Keep last 12 calls, trigger on 3 identical consecutive calls

	sendStatus := func(msg string) {
		dispatchProgress(progressCb, progress.Update{
			Message:   msg,
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		})
	}

	sendStream := func(msg string) {
		dispatchProgress(progressCb, progress.Update{
			Message: msg,
			Mode:    progress.ReportNoStatus,
		})
	}

	// Send initial progress message to chat
	sendStream(fmt.Sprintf("\n\n🔍 **Investigating codebase**: %s\n\n", objective))
	sendStatus(fmt.Sprintf("→ Starting investigation: %s", objective))

	// Create enhanced status callback that also sends ACP progress updates
	enhancedStatusCb := func(update progress.Update) error {
		if update.Message == "" && !update.ShouldStatus() {
			return nil
		}
		if update.ShouldStatus() {
			update.Ephemeral = true
		}
		return progress.Dispatch(progressCb, progress.Normalize(update))
	}

	// Create a new session for the investigation
	investigationSession := session.NewSession(session.GenerateID(), a.orch.workingDir)

	// Add initial objective message - this must remain immutable for prompt caching
	investigationSession.AddMessage(&session.Message{
		Role:    "user",
		Content: fmt.Sprintf("Investigation Objective: %s", objective),
	})

	// Create limited registry (with authorizer to enforce network/domain rules)
	registry := tools.NewRegistryWithSecrets(a.orch.authorizer, secretdetect.NewDetector())

	// Register tools
	modelFamily := llm.DetectModelFamily(a.orch.getSummarizeModelID())

	// Read File
	registry.Register(a.orch.getReadFileTool(modelFamily, investigationSession))

	// Search tools
	registry.Register(tools.NewSearchFilesTool(a.orch.fs))
	registry.Register(tools.NewSearchFileContentTool(a.orch.fs))

	// Web fetch (domain-authorized)
	registry.RegisterSpec(
		&tools.WebFetchToolSpec{},
		tools.NewWebFetchToolFactory(nil, a.orch.summarizeClient, a.orch.authorizer, secretdetect.NewDetector(), a.orch.featureFlags),
	)

	// Parallel execution tool (allows the investigator to speed up by running multiple tools concurrently)
	registry.Register(tools.NewParallelTool(registry))

	// Context tools (if context directories are configured)
	if len(a.orch.config.ContextDirectories) > 0 {
		registry.RegisterSpec(
			&tools.SearchContextFilesToolSpec{},
			tools.NewSearchContextFilesToolFactory(a.orch.fs, a.orch.config, a.orch.session),
		)
		registry.RegisterSpec(
			&tools.GrepContextFilesToolSpec{},
			tools.NewGrepContextFilesToolFactory(a.orch.fs, a.orch.config, a.orch.session),
		)
		registry.RegisterSpec(
			&tools.ReadContextFileToolSpec{},
			tools.NewReadContextFileToolFactory(a.orch.fs, a.orch.config, a.orch.session),
		)
	}

	// Build file tree for context
	fileTree := a.buildFileTree(ctx, 3, 200) // Max depth 3, max 200 files

	// Detect project language/framework
	detector := project.NewDetector(a.orch.workingDir)
	projectTypes, err := detector.Detect(ctx)
	var projectLanguage, projectFramework string
	if err == nil && len(projectTypes) > 0 {
		bestMatch := projectTypes[0]
		projectLanguage = bestMatch.Name
		projectFramework = bestMatch.Description
	} else {
		projectLanguage = "Unknown"
		projectFramework = ""
	}

	// System prompt for investigator
	systemPrompt := fmt.Sprintf(`You are a Codebase Investigator agent.:
Your goal is to explore the codebase to answer the user's objective.
You have access to tools to search and read files.
You should systematically explore relevant files.

%s

Project Information:
- Language/Framework: %s%s

Use the parallel_tool to execute multiple tools (e.g. multiple search_files, search_file_content, read_file) concurrently.

If context directories are configured, you also have access to context tools (search_context_files, grep_context_files, read_context_file) for searching external documentation or library sources.

Exit early (e.g. 5 tool calls) if you have not sufficient information to answer the objective.

When you have found the answer or gathered enough information, provide a comprehensive summary wrapped in <answer> tags.
If you cannot find the answer, explain what you checked and why you failed, also wrapped in <answer> tags.

Also for really relevant files, provide the file path and code location (e.g. function name and line numbers) where the information was found.
Example:
<answer>
The requested logic is found in internal/module/file.go function DoWork().
</answer>`, fileTree, projectLanguage, func() string {
		if projectFramework != "" {
			return " (" + projectFramework + ")"
		}
		return ""
	}())

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
			MaxTokens:     consts.DefaultMaxTokens, // Reasonable limit for investigation steps
			SystemPrompt:  systemPrompt,
			EnableCaching: true, // Enable caching for investigation to speed up repeated queries
			CacheTTL:      "5m",
		}

		resp, err := client.CompleteWithRequest(ctx, req)
		if err != nil {
			return "", fmt.Errorf("investigator LLM error: %w", err)
		}

		// Add assistant response
		investigationSession.AddMessage(&session.Message{
			Role:      "assistant",
			Content:   resp.Content,
			Reasoning: resp.Reasoning,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			// No tools called, this is the final answer
			sendStream("✓ Investigation complete\n\n")
			sendStatus("✓ Investigation complete")
			return a.finalizeInvestigationAnswer(ctx, resp.Content), nil
		}

		// Check for loops in tool calls and handle them
		if len(resp.ToolCalls) > 0 {
			for _, tc := range resp.ToolCalls {
				// Check if this call creates a loop
				if loopDetector.RecordCall(tc) {
					sendStream("⚠️ **Loop detected!** Multiple identical tool calls detected. Stopping investigation to prevent infinite loops.\n")
					sendStatus("⚠️ Loop detected - stopping investigation")

					// Try to extract partial results before terminating
					partialObjective := fmt.Sprintf("Based on the investigation attempted for: %s\n\nPlease provide a summary of what (if anything) was discovered so far about this objective, even if incomplete.", objective)

					// Create summary with loop warning
					completionReq := &llm.CompletionRequest{
						Messages: []*llm.Message{{
							Role:    "user",
							Content: partialObjective,
						}},
						Tools:        []map[string]interface{}{},
						Temperature:  0,
						MaxTokens:    consts.DefaultMaxTokens,
						SystemPrompt: "You are a helpful assistant. Help provide available insights about a codebase investigation that was terminated early due to detected loops.",
					}

					summaryResp, err := client.CompleteWithRequest(ctx, completionReq)
					if err == nil && summaryResp.Content != "" {
						sendStream("📝 Providing partial results due to loop detection\n\n")
						return a.finalizeInvestigationAnswer(ctx, summaryResp.Content), nil
					}

					// Fallback if summary fails
					return a.finalizeInvestigationAnswer(ctx, "Investigation stopped due to detected loop - no significant findings were discovered before termination."), nil
				}
			}
		}

		// Send progress update for tool calls with details
		if len(resp.ToolCalls) > 0 {
			for _, tc := range resp.ToolCalls {
				if fn, ok := tc["function"].(map[string]interface{}); ok {
					toolName, _ := fn["name"].(string)
					argsJSON, _ := fn["arguments"].(string)

					// Parse arguments to extract relevant details
					var args map[string]interface{}
					if argsJSON != "" {
						_ = json.Unmarshal([]byte(argsJSON), &args)
					}

					// Format tool call message based on tool type
					msg := formatToolCallMessage(toolName, args)
					if msg != "" {
						// Stream to chat if available
						sendStream(msg)
						// Also send status updates so ACP clients receive progress as tool_call_update
						sendStatus(strings.TrimSpace(msg))
					}
				}
			}
		}

		// Execute tools using extracted logic.
		// Use a unified execution function so ACP-aware callbacks share the same path.
		execFn := func(execCtx context.Context, call *tools.ToolCall, toolName string, progressCb progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback, approved bool) (*tools.ToolResult, error) {
			return registry.ExecuteWithCallbacks(execCtx, call, toolName, progressCb, toolCallCb, toolResultCb, approved), nil
		}

		// Use the extracted processToolCalls method.
		// Pass the enhanced status callback to show live progress in the UI and ACP.
		err = a.orch.processToolCalls(ctx, resp.ToolCalls, investigationSession, enhancedStatusCb, nil, nil, nil, execFn)
		if err != nil {
			return "", fmt.Errorf("tool execution failed: %w", err)
		}
	}

	return "Investigation timed out after maximum turns.", nil
}

func formatToolCallMessage(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "read_file":
		if path, ok := args["path"].(string); ok {
			// Show just the filename if it's a long path
			filename := filepath.Base(path)
			if filename != path && len(path) > 40 {
				return fmt.Sprintf("→ **read_file**: %s\n", filename)
			}
			return fmt.Sprintf("→ **read_file**: %s\n", path)
		}
		return "→ **read_file**\n"

	case "search_files":
		if pattern, ok := args["pattern"].(string); ok {
			return fmt.Sprintf("→ **search_files**: `%s`\n", pattern)
		}
		return "→ **search_files**\n"

	case "search_file_content":
		if pattern, ok := args["pattern"].(string); ok {
			// Show directory context if provided
			if dir, ok := args["directory"].(string); ok && dir != "" && dir != "." {
				return fmt.Sprintf("→ **search_file_content**: `%s` in %s\n", pattern, dir)
			}
			return fmt.Sprintf("→ **search_file_content**: `%s`\n", pattern)
		}
		return "→ **search_file_content**\n"

	case "parallel_tools":
		// Extract the list of tool calls being executed in parallel
		if toolCalls, ok := args["tool_calls"].([]interface{}); ok {
			var toolNames []string
			for _, tc := range toolCalls {
				if callMap, ok := tc.(map[string]interface{}); ok {
					if name, ok := callMap["name"].(string); ok {
						// Try to get additional details for each parallel tool
						var details string
						if params, ok := callMap["parameters"].(map[string]interface{}); ok {
							details = extractToolDetails(name, params)
						}
						if details != "" {
							toolNames = append(toolNames, fmt.Sprintf("%s(%s)", name, details))
						} else {
							toolNames = append(toolNames, name)
						}
					}
				}
			}
			if len(toolNames) > 0 {
				return fmt.Sprintf("→ **parallel_tools** [%d]: %s\n", len(toolNames), strings.Join(toolNames, ", "))
			}
		}
		return "→ **parallel_tools**\n"

	default:
		// Generic fallback for unknown tools
		return fmt.Sprintf("→ **%s**\n", toolName)
	}
}

// extractToolDetails extracts concise details from tool parameters
func extractToolDetails(toolName string, params map[string]interface{}) string {
	switch toolName {
	case "read_file":
		if path, ok := params["path"].(string); ok {
			return filepath.Base(path)
		}
	case "search_files":
		if pattern, ok := params["pattern"].(string); ok {
			return pattern
		}
	case "search_file_content":
		if pattern, ok := params["pattern"].(string); ok {
			return pattern
		}
	}
	return ""
}

// buildFileTree creates a tree representation of files in the workspace
func (a *CodebaseInvestigatorAgent) buildFileTree(ctx context.Context, maxDepth int, maxFiles int) string {
	var result strings.Builder
	result.WriteString("## Workspace File Structure\n\n")

	fileCount := 0
	var walk func(dir string, prefix string, depth int) error
	walk = func(dir string, prefix string, depth int) error {
		if depth > maxDepth || fileCount >= maxFiles {
			return nil
		}

		entries, err := a.orch.fs.ListDir(ctx, dir)
		if err != nil {
			return err
		}

		// Sort entries: directories first, then files alphabetically
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir != entries[j].IsDir {
				return entries[i].IsDir
			}
			return filepath.Base(entries[i].Path) < filepath.Base(entries[j].Path)
		})

		for i, entry := range entries {
			if fileCount >= maxFiles {
				result.WriteString(fmt.Sprintf("%s... (truncated, %d files shown)\n", prefix, maxFiles))
				return nil
			}

			// Skip common directories to ignore
			name := filepath.Base(entry.Path)
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == ".next" || name == "dist" || name == "build" ||
				name == "__pycache__" || name == ".cache" {
				continue
			}

			isLast := i == len(entries)-1
			connector := "├── "
			if isLast {
				connector = "└── "
			}

			if entry.IsDir {
				result.WriteString(fmt.Sprintf("%s%s%s/\n", prefix, connector, name))
				fileCount++

				// Recurse into subdirectory
				newPrefix := prefix
				if isLast {
					newPrefix += "    "
				} else {
					newPrefix += "│   "
				}
				_ = walk(entry.Path, newPrefix, depth+1)
			} else {
				result.WriteString(fmt.Sprintf("%s%s%s\n", prefix, connector, name))
				fileCount++
			}
		}

		return nil
	}

	_ = walk(a.orch.workingDir, "", 0)

	if fileCount >= maxFiles {
		result.WriteString(fmt.Sprintf("\n(Showing first %d files/directories)\n", maxFiles))
	}

	return result.String()
}

func extractAnswer(content string) string {
	// Strip <think> tags from reasoning models (e.g., DeepSeek, Qwen 3)
	content = stripThinkTags(content)
	startTag := "<answer>"
	endTag := "</answer>"
	start := strings.Index(content, startTag)
	if start == -1 {
		if strings.TrimSpace(content) != "" {
			logger.Warn("Summary model decision does not equal exactly what was asked for (missing <answer> tags): %s", content)
		}
		return content
	}
	content = content[start+len(startTag):]
	end := strings.LastIndex(content, endTag)
	if end == -1 {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(content[:end])
}

func (a *CodebaseInvestigatorAgent) finalizeInvestigationAnswer(ctx context.Context, content string) string {
	answer := strings.TrimSpace(extractAnswer(content))
	if answer == "" {
		return answer
	}

	contextWindow := a.getInvestigationContextWindow()
	if contextWindow == 0 {
		return answer
	}

	threshold := int(float64(contextWindow) * codebaseInvestigatorOutputPercent)
	if llm.EstimateTokenCount(answer) <= threshold {
		return answer
	}

	return a.compactInvestigationAnswer(ctx, answer, threshold)
}

func (a *CodebaseInvestigatorAgent) compactInvestigationAnswer(ctx context.Context, answer string, threshold int) string {
	client := a.orch.summarizeClient
	if client == nil {
		return answer
	}

	chunkedSummarizer := summarizer.NewChunkedSummarizer(client)
	chunkedSummarizer.ChunkThresholdPercent = codebaseInvestigatorOutputPercent

	maxBytes := threshold * 4
	if maxBytes <= 0 {
		return answer
	}

	prompt := fmt.Sprintf("Condense this codebase investigation result so it fits within %d tokens while preserving key findings, file paths, and line references:", threshold)
	result, err := chunkedSummarizer.Summarize(ctx, answer, summarizer.SummarizeOptions{
		BasePrompt: prompt,
		MaxBytes:   maxBytes,
	})
	if err != nil || result == nil || strings.TrimSpace(result.Summary) == "" {
		logger.Warn("codebase investigator: output compaction failed: %v", err)
		return answer
	}

	return strings.TrimSpace(result.Summary)
}

func (a *CodebaseInvestigatorAgent) getInvestigationContextWindow() int {
	contextWindow := a.getSummarizeContextWindow()
	if contextWindow > 0 {
		return contextWindow
	}

	if a.orch == nil || a.orch.providerMgr == nil {
		return 0
	}

	modelID := a.orch.providerMgr.GetOrchestrationModel()
	if modelID == "" {
		return 0
	}

	return a.orch.getContextWindow(modelID)
}

func (a *CodebaseInvestigatorAgent) getSummarizeContextWindow() int {
	if a.orch == nil || a.orch.summarizeClient == nil {
		return 0
	}

	modelID := a.orch.summarizeClient.GetModelName()
	if modelID == "" {
		return 0
	}

	family := llm.DetectModelFamily(modelID)
	return llm.DetectContextWindow(modelID, family)
}
