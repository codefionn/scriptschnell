package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/session"
	realtools "github.com/codefionn/scriptschnell/internal/tools"
)

// PlanningTool represents a tool that can be executed by the planning agent
type PlanningTool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, params map[string]interface{}) *PlanningToolResult
}

// PlanningToolResult represents the result of a planning tool execution
type PlanningToolResult struct {
	Result interface{} `json:"result"`
	Error  string      `json:"error,omitempty"`
}

// PlanningToolRegistry manages tools available to the planning agent
type PlanningToolRegistry struct {
	tools map[string]PlanningTool
	mu    sync.RWMutex
}

func NewPlanningToolRegistry() *PlanningToolRegistry {
	return &PlanningToolRegistry{
		tools: make(map[string]PlanningTool),
	}
}

func (r *PlanningToolRegistry) Register(tool PlanningTool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

func (r *PlanningToolRegistry) Execute(ctx context.Context, toolName string, params map[string]interface{}) *PlanningToolResult {
	r.mu.RLock()
	tool, exists := r.tools[toolName]
	r.mu.RUnlock()

	if exists {
		return tool.Execute(ctx, params)
	}
	return &PlanningToolResult{Error: fmt.Sprintf("tool not found: %s", toolName)}
}

func (r *PlanningToolRegistry) ToJSONSchema() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var schemas []map[string]interface{}
	for _, tool := range r.tools {
		schemas = append(schemas, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters":  tool.Parameters(),
			},
		})
	}
	return schemas
}

// PlanningAgent handles planning tasks using a dedicated LLM model
type PlanningAgent struct {
	id           string
	fs           fs.FileSystem
	session      *session.Session
	client       llm.Client
	toolRegistry *PlanningToolRegistry
	actorSystem  *actor.System
	investigator realtools.Investigator
	mu           sync.RWMutex
}

// PlanningRequest represents a planning request
type PlanningRequest struct {
	Objective      string   `json:"objective"`
	Context        string   `json:"context,omitempty"`
	ContextFiles   []string `json:"context_files,omitempty"`
	AllowQuestions bool     `json:"allow_questions"`
	MaxQuestions   int      `json:"max_questions,omitempty"`
}

// PlanningResponse represents a planning response
type PlanningResponse struct {
	Plan       []string `json:"plan"`
	Questions  []string `json:"questions,omitempty"`
	NeedsInput bool     `json:"needs_input"`
	Complete   bool     `json:"complete"`
}

// UserInputCallback is called when the planning agent needs user input
type UserInputCallback func(question string) (string, error)

// NewPlanningAgent creates a new planning agent
func NewPlanningAgent(id string, filesystem fs.FileSystem, sess *session.Session, client llm.Client, investigator realtools.Investigator) *PlanningAgent {
	agent := &PlanningAgent{
		id:           id,
		fs:           filesystem,
		session:      sess,
		client:       client,
		investigator: investigator,
	}

	// Initialize actor system and tools
	agent.actorSystem = actor.NewSystem()
	agent.initializeTools()

	return agent
}

// initializeTools sets up the planning-specific tools
func (p *PlanningAgent) initializeTools() {
	p.resetToolsLocked(nil)
}

// resetToolsLocked rebuilds the tool registry with the default tools and any extra tools provided.
// Caller must hold p.mu when invoking this method.
func (p *PlanningAgent) resetToolsLocked(extraTools []PlanningTool) {
	// Create tool registry
	p.toolRegistry = NewPlanningToolRegistry()

	// Register planning-specific tools
	p.toolRegistry.Register(NewAskUserTool())

	// Register real tools via adapter
	p.toolRegistry.Register(NewRealToolAdapter(
		&realtools.ReadFileToolSpec{},
		realtools.NewReadFileTool(p.fs, p.session),
	))

	p.toolRegistry.Register(NewRealToolAdapter(
		&realtools.SearchFilesToolSpec{},
		realtools.NewSearchFilesTool(p.fs),
	))

	p.toolRegistry.Register(NewRealToolAdapter(
		&realtools.SearchFileContentToolSpec{},
		realtools.NewSearchFileContentTool(p.fs),
	))

	if p.investigator != nil {
		p.toolRegistry.Register(NewRealToolAdapter(
			&realtools.CodebaseInvestigatorToolSpec{},
			realtools.NewCodebaseInvestigatorTool(p.investigator),
		))
	}

	// Register any extra tools provided by the caller (e.g., read-only MCP tools)
	for _, tool := range extraTools {
		if tool == nil {
			continue
		}
		p.toolRegistry.Register(tool)
	}

	logger.Debug("Planning agent initialized with %d tools", len(p.toolRegistry.tools))
}

// SetExternalTools replaces the tool registry with the default planning tools plus any extra tools provided.
func (p *PlanningAgent) SetExternalTools(tools []PlanningTool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetToolsLocked(tools)
}

const maxContextFileBytes = 50_000

// collectContextFiles reads provided context files and returns a formatted string with their contents.
func (p *PlanningAgent) collectContextFiles(ctx context.Context, paths []string) string {
	if len(paths) == 0 || p.fs == nil {
		return ""
	}

	var sb strings.Builder
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		data, err := p.fs.ReadFile(ctx, path)
		if err != nil {
			sb.WriteString(fmt.Sprintf("Context file: %s\n<error: %v>\n\n", path, err))
			continue
		}

		// Enforce a size limit to avoid overwhelming the model.
		if len(data) > maxContextFileBytes {
			data = data[:maxContextFileBytes]
			sb.WriteString(fmt.Sprintf("Context file: %s (truncated to %d bytes)\n", path, maxContextFileBytes))
		} else {
			sb.WriteString(fmt.Sprintf("Context file: %s\n", path))
		}

		content := string(data)
		sb.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")

		if p.session != nil {
			p.session.TrackFileRead(path, content)
		}
	}

	return strings.TrimSpace(sb.String())
}

// Plan generates a plan for the given objective
func (p *PlanningAgent) Plan(ctx context.Context, req *PlanningRequest, userInputCb UserInputCallback) (*PlanningResponse, error) {
	return p.PlanWithProgress(ctx, req, userInputCb, nil)
}

// PlanWithProgress generates a plan and streams planning output via the provided progress callback.
func (p *PlanningAgent) PlanWithProgress(ctx context.Context, req *PlanningRequest, userInputCb UserInputCallback, progressCb progress.Callback) (*PlanningResponse, error) {
	return p.plan(ctx, req, userInputCb, progressCb)
}

func (p *PlanningAgent) plan(ctx context.Context, req *PlanningRequest, userInputCb UserInputCallback, progressCb progress.Callback) (*PlanningResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	streamPlanning := func(msg string) {
		if progressCb == nil {
			return
		}
		if strings.TrimSpace(msg) == "" {
			return
		}
		if err := progress.Dispatch(progressCb, progress.Normalize(progress.Update{
			Message: msg,
			Mode:    progress.ReportNoStatus,
		})); err != nil {
			logger.Debug("planning stream callback error: %v", err)
		}
	}

	updateStatus := func(msg string) {
		if progressCb == nil {
			return
		}
		if strings.TrimSpace(msg) == "" {
			return
		}
		if err := progress.Dispatch(progressCb, progress.Update{
			Message:   msg,
			Mode:      progress.ReportJustStatus,
			Ephemeral: true,
		}); err != nil {
			logger.Debug("planning status callback error: %v", err)
		}
	}

	// Validate request
	if req == nil {
		return nil, fmt.Errorf("planning request cannot be nil")
	}
	if strings.TrimSpace(req.Objective) == "" {
		return nil, fmt.Errorf("planning objective cannot be empty")
	}
	if p.client == nil {
		return nil, fmt.Errorf("no planning client available")
	}

	logger.Debug("Planning agent starting plan for objective: %s", req.Objective)

	// Build planning system prompt
	systemPrompt := p.buildSystemPrompt(req)

	// Start with the initial request
	messages := []*llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Create a plan for: %s", req.Objective)},
	}

	// Add context if provided
	if req.Context != "" {
		messages = append(messages, &llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("Additional context: %s", req.Context),
		})
	}

	// Add context file contents if provided
	if ctxFiles := p.collectContextFiles(ctx, req.ContextFiles); ctxFiles != "" {
		messages = append(messages, &llm.Message{
			Role:    "user",
			Content: ctxFiles,
		})
	}

	maxIterations := 5 // Prevent infinite loops
	questionsAsked := 0

	for iteration := 0; iteration < maxIterations; iteration++ {
		logger.Debug("Planning iteration %d/%d", iteration+1, maxIterations)

		// Create completion request
		completeReq := &llm.CompletionRequest{
			Messages:     messages,
			Tools:        p.toolRegistry.ToJSONSchema(),
			Temperature:  0.3, // Lower temperature for more consistent planning
			MaxTokens:    4096,
			SystemPrompt: systemPrompt,
		}

		// Get response from planning model
		response, err := p.client.CompleteWithRequest(ctx, completeReq)
		if err != nil {
			return nil, fmt.Errorf("planning completion failed: %w", err)
		}

		logger.Debug("Planning response received, tool_calls=%d", len(response.ToolCalls))

		// Stream planning content to the UI if available
		if response.Content != "" {
			streamPlanning(response.Content)
		}

		// Add assistant response to messages
		messages = append(messages, &llm.Message{
			Role:      "assistant",
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		})

		// Process tool calls if any
		if len(response.ToolCalls) > 0 {
			toolResults, needsInput, askedCount, err := p.processToolCalls(ctx, response.ToolCalls, userInputCb, req, questionsAsked, updateStatus)
			if err != nil {
				return nil, fmt.Errorf("tool execution failed: %w", err)
			}

			// Add tool results to messages
			for _, result := range toolResults {
				messages = append(messages, result)
			}

			// Update questions asked counter
			questionsAsked += askedCount

			// If we need user input and have exhausted questions, return partial plan
			if needsInput && (!req.AllowQuestions || (req.MaxQuestions > 0 && questionsAsked >= req.MaxQuestions)) {
				logger.Debug("Planning needs user input but max questions reached")
				return p.extractPartialPlan(messages), nil
			}

			// Continue to next iteration to get refined plan
			continue
		}

		// No tool calls, extract final plan
		planResp := p.extractPlan(response.Content)
		// Return if:
		// 1. We have actual plan content or it's marked complete
		// 2. It explicitly needs input (with or without questions) - this is a signal from LLM
		hasContent := len(planResp.Plan) > 0 || planResp.Complete
		needsUserInput := planResp.NeedsInput && req.AllowQuestions
		shouldReturn := hasContent || needsUserInput
		if planResp != nil && shouldReturn {
			logger.Debug("Planning completed successfully")
			return planResp, nil
		}

		// If we can't extract a plan, try asking for clarification
		if iteration < maxIterations-1 {
			messages = append(messages, &llm.Message{
				Role:    "user",
				Content: "Please provide a clearer plan with specific steps. Format your response with <answer> tags containing a JSON object with a 'plan' array containing the steps.",
			})
		}
	}

	// Max iterations reached, return best effort plan
	logger.Warn("Planning reached max iterations, returning partial plan")
	return p.extractPartialPlan(messages), nil
}

// processToolCalls executes the planning agent's tool calls
func (p *PlanningAgent) processToolCalls(ctx context.Context, toolCalls []map[string]interface{}, userInputCb UserInputCallback, req *PlanningRequest, currentQuestionsAsked int, statusCb func(string)) ([]*llm.Message, bool, int, error) {
	var results []*llm.Message
	needsInput := false
	questionsAsked := 0

	for _, toolCall := range toolCalls {
		toolID, _ := toolCall["id"].(string)
		toolType, _ := toolCall["type"].(string)

		if toolType != "function" {
			continue
		}

		function, ok := toolCall["function"].(map[string]interface{})
		if !ok {
			continue
		}

		toolName, _ := function["name"].(string)
		argsJSON, _ := function["arguments"].(string)

		logger.Debug("Planning agent executing tool: %s", toolName)
		if statusCb != nil {
			statusCb(fmt.Sprintf("Planning: executing %s", toolName))
		}

		// Parse arguments
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			results = append(results, &llm.Message{
				Role:     "tool",
				Content:  fmt.Sprintf("Error parsing tool arguments: %v", err),
				ToolID:   toolID,
				ToolName: toolName,
			})
			continue
		}

		// Execute tool
		var toolResult string

		switch toolName {
		case "ask_user":
			if !req.AllowQuestions {
				toolResult = "User questions are not allowed for this planning request"
			} else if userInputCb == nil {
				toolResult = "No user input callback available"
				needsInput = true
			} else if req.MaxQuestions > 0 && (currentQuestionsAsked+questionsAsked) >= req.MaxQuestions {
				// Already at max questions, can't ask more
				toolResult = "Maximum number of questions reached, cannot ask more questions"
				needsInput = true
			} else {
				question, _ := args["question"].(string)
				if question == "" {
					toolResult = "No question provided"
				} else {
					userResponse, err := userInputCb(question)
					if err != nil {
						toolResult = fmt.Sprintf("Failed to get user input: %v", err)
						needsInput = true
					} else {
						toolResult = userResponse
						questionsAsked++
					}
				}
			}

		case "read_file", "search_files", "search_file_content", "codebase_investigator":
			// Execute through planning tool registry
			result := p.toolRegistry.Execute(ctx, toolName, args)
			if result.Error != "" {
				toolResult = fmt.Sprintf("Error: %s", result.Error)
			} else {
				if resultMap, ok := result.Result.(map[string]interface{}); ok {
					if jsonBytes, err := json.Marshal(resultMap); err == nil {
						toolResult = string(jsonBytes)
					} else {
						toolResult = fmt.Sprintf("%v", result.Result)
					}
				} else {
					toolResult = fmt.Sprintf("%v", result.Result)
				}
			}

		default:
			toolResult = fmt.Sprintf("Unknown tool: %s", toolName)
		}

		results = append(results, &llm.Message{
			Role:     "tool",
			Content:  toolResult,
			ToolID:   toolID,
			ToolName: toolName,
		})
	}

	return results, needsInput, questionsAsked, nil
}

// buildSystemPrompt creates the system prompt for the planning agent
func (p *PlanningAgent) buildSystemPrompt(req *PlanningRequest) string {
	var prompt strings.Builder

	prompt.WriteString("You are a planning assistant that helps break down complex tasks into actionable steps.\n\n")
	prompt.WriteString("Your role is to:\n")
	prompt.WriteString("1. Analyze the given objective and context\n")
	prompt.WriteString("2. Create a detailed, step-by-step plan\n")
	prompt.WriteString("3. Ask clarifying questions if needed (when allowed)\n")
	prompt.WriteString("4. Use available tools to gather information\n\n")

	prompt.WriteString("Guidelines:\n")
	prompt.WriteString("- Break down complex tasks into manageable steps\n")
	prompt.WriteString("- Each step should be specific and actionable\n")
	prompt.WriteString("- Consider dependencies between steps\n")
	prompt.WriteString("- Use tools to read files or search for information when needed\n")

	if req.AllowQuestions {
		prompt.WriteString("- Ask questions when you need clarification\n")
		if req.MaxQuestions > 0 {
			prompt.WriteString(fmt.Sprintf("- Ask at most %d questions\n", req.MaxQuestions))
		}
	} else {
		prompt.WriteString("- Do not ask questions; work with the information provided\n")
	}

	prompt.WriteString("\n")
	prompt.WriteString("When you have enough information, provide your final response wrapped in <answer> tags.\n")
	prompt.WriteString("Inside the <answer> tags, format your response as a JSON object:\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"plan\": [\"Step 1: ...\", \"Step 2: ...\", \"Step 3: ...\"],\n")
	prompt.WriteString("  \"questions\": [\"Question 1...\", \"Question 2...\"], // optional\n")
	prompt.WriteString("  \"needs_input\": false,\n")
	prompt.WriteString("  \"complete\": true\n")
	prompt.WriteString("}\n\n")
	prompt.WriteString("Example format:\n")
	prompt.WriteString("<answer>\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"plan\": [\n")
	prompt.WriteString("    \"Step 1: Analyze requirements and existing codebase\",\n")
	prompt.WriteString("    \"Step 2: Design the new component architecture\",\n")
	prompt.WriteString("    \"Step 3: Implement core functionality\",\n")
	prompt.WriteString("    \"Step 4: Write tests and documentation\"\n")
	prompt.WriteString("  ],\n")
	prompt.WriteString("  \"questions\": [],\n")
	prompt.WriteString("  \"needs_input\": false,\n")
	prompt.WriteString("  \"complete\": true\n")
	prompt.WriteString("}\n")
	prompt.WriteString("</answer>\n\n")

	prompt.WriteString("If you need user input, set needs_input to true and include your questions.\n")

	return prompt.String()
}

// extractPlan extracts a structured plan from the LLM response
func (p *PlanningAgent) extractPlan(content string) *PlanningResponse {
	// First try to extract answer from <answer> tags like codebase_investigator
	if answer := extractAnswerFromTags(content); answer != "" {
		// Try to parse the extracted answer as JSON
		if planResp := p.tryParseJSONPlan(answer); planResp != nil {
			return planResp
		}
		// If not JSON, treat as a single-step plan
		return &PlanningResponse{
			Plan:     []string{answer},
			Complete: true,
		}
	}

	// Try to extract JSON from the response (original logic)
	content = strings.TrimSpace(content)

	// Look for JSON object in the response
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")

	if start >= 0 && end > start {
		jsonStr := content[start : end+1]
		if planResp := p.tryParseJSONPlan(jsonStr); planResp != nil {
			return planResp
		}
	}

	// Fallback: try to parse plan steps from plain text
	lines := strings.Split(content, "\n")
	var plan []string
	var questions []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Look for numbered steps or bullet points
		if strings.HasPrefix(line, "1.") || strings.HasPrefix(line, "2.") ||
			strings.HasPrefix(line, "3.") || strings.HasPrefix(line, "-") ||
			strings.HasPrefix(line, "*") {
			plan = append(plan, line)
		}

		// Look for questions
		if strings.Contains(line, "?") && (strings.Contains(line, "question") ||
			strings.Contains(line, "clarif") || strings.Contains(line, "need")) {
			questions = append(questions, line)
		}
	}

	if len(plan) > 0 || len(questions) > 0 {
		return &PlanningResponse{
			Plan:       plan,
			Questions:  questions,
			NeedsInput: len(questions) > 0,
			Complete:   len(questions) == 0 && len(plan) > 0,
		}
	}

	// Last resort: return the content as a single step
	return &PlanningResponse{
		Plan:     []string{content},
		Complete: true,
	}
}

// extractAnswerFromTags extracts content between <answer> tags (copied from codebase_investigator)
func extractAnswerFromTags(content string) string {
	startTag := "<answer>"
	endTag := "</answer>"
	start := strings.Index(content, startTag)
	if start == -1 {
		return ""
	}
	content = content[start+len(startTag):]
	end := strings.LastIndex(content, endTag)
	if end == -1 {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(content[:end])
}

// tryParseJSONPlan attempts to parse a string as a JSON PlanningResponse
func (p *PlanningAgent) tryParseJSONPlan(jsonStr string) *PlanningResponse {
	var resp PlanningResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		fmt.Printf("JSON parse error: %v for %s\n", err, jsonStr)
		return nil
	}
	fmt.Printf("Parsed JSON: %+v from %s\n", resp, jsonStr)
	// Validate that we have at least a plan or questions, or it's an explicit empty plan or needs input
	if len(resp.Plan) > 0 || len(resp.Questions) > 0 || resp.NeedsInput || (resp.Complete && strings.Contains(jsonStr, "\"plan\"")) {
		return &resp
	}
	fmt.Printf("JSON parsed but validation failed: plan=%d, questions=%d\n", len(resp.Plan), len(resp.Questions))
	return nil
}

// extractPartialPlan extracts a partial plan when we can't continue
func (p *PlanningAgent) extractPartialPlan(messages []*llm.Message) *PlanningResponse {
	// Look at the last assistant message for any plan content
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "assistant" && msg.Content != "" {
			// Skip messages that are tool call responses (they don't contain actual plans)
			if len(msg.ToolCalls) > 0 {
				continue
			}

			// First try to extract from <answer> tags
			if answer := extractAnswerFromTags(msg.Content); answer != "" {
				if plan := p.tryParseJSONPlan(answer); plan != nil {
					plan.NeedsInput = true
					plan.Complete = false
					return plan
				}
				// If not JSON, treat as single-step partial plan
				return &PlanningResponse{
					Plan:       []string{answer},
					NeedsInput: true,
					Complete:   false,
				}
			}

			// Fallback to original extraction logic
			plan := p.extractPlan(msg.Content)
			if plan != nil && len(plan.Plan) > 0 {
				plan.NeedsInput = true
				plan.Complete = false
				return plan
			}
		}
	}

	// Return minimal response
	return &PlanningResponse{
		Plan:       []string{},
		NeedsInput: true,
		Complete:   false,
	}
}

// Close cleans up the planning agent
func (p *PlanningAgent) Close(ctx context.Context) error {
	logger.Debug("Closing planning agent: %s", p.id)

	var errs []error

	// Close actor system if it exists
	if p.actorSystem != nil {
		if err := p.actorSystem.StopAll(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop actor system: %w", err))
		}
	}

	// Clear tool registry
	if p.toolRegistry != nil {
		p.toolRegistry.tools = make(map[string]PlanningTool)
	}

	if len(errs) > 0 {
		return fmt.Errorf("multiple errors during cleanup: %v", errs)
	}

	return nil
}

// RealToolAdapter adapts real tools to the PlanningTool interface
type RealToolAdapter struct {
	spec     realtools.ToolSpec
	executor realtools.ToolExecutor
}

func NewRealToolAdapter(spec realtools.ToolSpec, executor realtools.ToolExecutor) *RealToolAdapter {
	return &RealToolAdapter{spec: spec, executor: executor}
}

func (a *RealToolAdapter) Name() string                   { return a.spec.Name() }
func (a *RealToolAdapter) Description() string            { return a.spec.Description() }
func (a *RealToolAdapter) Parameters() map[string]interface{} { return a.spec.Parameters() }

func (a *RealToolAdapter) Execute(ctx context.Context, params map[string]interface{}) *PlanningToolResult {
	res := a.executor.Execute(ctx, params)
	if res.Error != "" {
		return &PlanningToolResult{Error: res.Error}
	}
	return &PlanningToolResult{Result: res.Result}
}

// Implementation of planning tools

// AskUserTool allows the planning agent to ask questions to the user
type AskUserTool struct{}

func NewAskUserTool() *AskUserTool {
	return &AskUserTool{}
}

func (t *AskUserTool) Name() string { return "ask_user" }
func (t *AskUserTool) Description() string {
	return "Ask a question to the user to get clarification or additional information"
}
func (t *AskUserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question": map[string]interface{}{
				"type":        "string",
				"description": "The question to ask the user",
			},
		},
		"required": []string{"question"},
	}
}

func (t *AskUserTool) Execute(ctx context.Context, params map[string]interface{}) *PlanningToolResult {
	question, ok := params["question"].(string)
	if !ok {
		return &PlanningToolResult{
			Error: "question parameter is required and must be a string",
		}
	}

	// This tool is handled specially by the planning agent
	// The actual user interaction happens through the callback
	return &PlanningToolResult{
		Result: map[string]interface{}{
			"question": question,
			"status":   "pending_user_input",
		},
	}
}
