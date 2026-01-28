package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/loopdetector"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/project"
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

func (r *PlanningToolRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = make(map[string]PlanningTool)
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
	loopDetector *loopdetector.LoopDetector
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

// PlanningTask represents a task in the planning board
type PlanningTask struct {
	ID          string         `json:"id"`
	Text        string         `json:"text"`
	Subtasks    []PlanningTask `json:"subtasks,omitempty"`
	Priority    string         `json:"priority,omitempty"` // "high", "medium", "low"
	Status      string         `json:"status,omitempty"`   // "pending", "in_progress", "completed"
	Description string         `json:"description,omitempty"`
}

// PlanningBoard represents a hierarchical planning board with primary tasks and subtasks
type PlanningBoard struct {
	PrimaryTasks []PlanningTask `json:"primary_tasks"`
	Description  string         `json:"description,omitempty"`
}

// PlanningMode represents the output mode of the planning agent
type PlanningMode string

const (
	// PlanningModeSimple is the traditional simple task list output
	PlanningModeSimple PlanningMode = "simple"
	// PlanningModeBoard is the hierarchical planning board output
	PlanningModeBoard PlanningMode = "board"
)

// PlanningResponse represents a planning response
type PlanningResponse struct {
	Mode       PlanningMode   `json:"mode"`
	Plan       []string       `json:"plan,omitempty"`  // For simple mode
	Board      *PlanningBoard `json:"board,omitempty"` // For board mode
	Questions  []string       `json:"questions,omitempty"`
	NeedsInput bool           `json:"needs_input"`
	Complete   bool           `json:"complete"`
}

// UserInputCallback is called when the planning agent needs user input
type UserInputCallback func(question string) (string, error)

// ToolCallCallback is called when a planning tool is being executed
type ToolCallCallback func(toolName, toolID string, parameters map[string]interface{}) error

// ToolResultCallback is called when a planning tool execution completes
type ToolResultCallback func(toolName, toolID, result, errorMsg string) error

// NewPlanningAgent creates a new planning agent
func NewPlanningAgent(id string, filesystem fs.FileSystem, sess *session.Session, client llm.Client, investigator realtools.Investigator) *PlanningAgent {
	agent := &PlanningAgent{
		id:           id,
		fs:           filesystem,
		session:      sess,
		client:       client,
		investigator: investigator,
		loopDetector: loopdetector.NewLoopDetector(),
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
	if p.toolRegistry == nil {
		p.toolRegistry = NewPlanningToolRegistry()
	} else {
		p.toolRegistry.Reset()
	}

	// Register planning-specific tools
	p.toolRegistry.Register(NewAskUserTool())
	p.toolRegistry.Register(NewAskUserMultipleTool())

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
	return p.PlanWithProgress(ctx, req, userInputCb, nil, nil, nil)
}

// PlanWithProgress generates a plan and streams planning output via the provided progress callback.
func (p *PlanningAgent) PlanWithProgress(ctx context.Context, req *PlanningRequest, userInputCb UserInputCallback, progressCb progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback) (*PlanningResponse, error) {
	return p.plan(ctx, req, userInputCb, progressCb, toolCallCb, toolResultCb)
}

func (p *PlanningAgent) plan(ctx context.Context, req *PlanningRequest, userInputCb UserInputCallback, progressCb progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback) (*PlanningResponse, error) {
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

	// Build ALL initial messages upfront to preserve prompt cache stability.
	// The message prefix (initial user messages + context) must remain immutable
	// throughout the agent's lifecycle for Anthropic's prompt caching to work.
	messages := make([]*llm.Message, 0, 4)
	var prefixMessage *llm.Message
	addPrefixMessage := func(content string) {
		msg := &llm.Message{
			Role:    "user",
			Content: content,
		}
		messages = append(messages, msg)
		prefixMessage = msg
	}

	// Build the immutable prefix that Anthropic can cache.
	addPrefixMessage(fmt.Sprintf("Create a plan for: %s", req.Objective))

	// Add context if provided - MUST be added before the loop starts
	if req.Context != "" {
		addPrefixMessage(fmt.Sprintf("Additional context: %s", req.Context))
	}

	// Add context file contents if provided - MUST be added before the loop starts
	if ctxFiles := p.collectContextFiles(ctx, req.ContextFiles); ctxFiles != "" {
		addPrefixMessage(ctxFiles)
	}

	if prefixMessage != nil {
		prefixMessage.CacheControl = true
	}

	maxIterations := 96 // Prevent infinite loops
	questionsAsked := 0

	for iteration := 0; iteration < maxIterations; iteration++ {
		logger.Debug("Planning iteration %d/%d", iteration+1, maxIterations)

		// Create completion request
		completeReq := &llm.CompletionRequest{
			Messages:      messages,
			Tools:         p.toolRegistry.ToJSONSchema(),
			Temperature:   0.3, // Lower temperature for more consistent planning
			MaxTokens:     4096,
			SystemPrompt:  systemPrompt,
			EnableCaching: true, // Enable caching for planning to speed up multi-turn conversations
			CacheTTL:      "5m",
		}

		// Get response from planning model
		response, err := p.client.CompleteWithRequest(ctx, completeReq)
		if err != nil {
			return nil, fmt.Errorf("planning completion failed: %w", err)
		}

		// Ensure tool calls always carry an ID before we echo them back.
		response.ToolCalls = llm.NormalizeToolCallIDs(response.ToolCalls)

		logger.Debug("Planning response received, tool_calls=%d", len(response.ToolCalls))

		// Check for text loops in the response
		if response.Content != "" {
			isLoop, pattern, count := p.loopDetector.AddText(response.Content)
			if isLoop {
				logger.Warn("Text loop detected in planning at iteration %d: pattern repeated %d times", iteration, count)
				// Show a truncated version of the pattern to the user
				displayPattern := pattern
				if len(displayPattern) > 100 {
					displayPattern = displayPattern[:100] + "..."
				}
				if err := progress.Dispatch(progressCb, progress.Normalize(progress.Update{
					Message: fmt.Sprintf("\n\n🔁 Loop detected! The planning model is repeating the same text pattern %d times.\nPattern: %s\nStopping planning to prevent infinite loop.\n", count, displayPattern),
					Mode:    progress.ReportNoStatus,
				})); err != nil {
					logger.Debug("planning loop detection callback error: %v", err)
				}
				logger.Debug("Breaking out of planning loop due to text repetition (iteration %d)", iteration)
				// Return partial plan indicating the loop issue
				return &PlanningResponse{
					Plan:       []string{"Planning was stopped due to text repetition in the LLM response"},
					NeedsInput: false,
					Complete:   false,
				}, nil
			}
		}

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
			toolResults, needsInput, askedCount, err := p.processToolCalls(ctx, response.ToolCalls, userInputCb, req, questionsAsked, updateStatus, toolCallCb, toolResultCb)
			if err != nil {
				return nil, fmt.Errorf("tool execution failed: %w", err)
			}

			// Add tool results to messages
			messages = append(messages, toolResults...)

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
		var hasContent, needsUserInput bool
		if planResp != nil {
			// Check for content in both simple mode (Plan) and board mode (Board.PrimaryTasks)
			hasPlanContent := len(planResp.Plan) > 0
			hasBoardContent := planResp.Board != nil && len(planResp.Board.PrimaryTasks) > 0
			hasContent = hasPlanContent || hasBoardContent || planResp.Complete
			needsUserInput = planResp.NeedsInput && req.AllowQuestions
		}
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

// toolCallResult represents the result of a single tool execution
type toolCallResult struct {
	idx            int
	message        *llm.Message
	needsInput     bool
	questionsAsked int
	err            error
}

// processToolCalls executes the planning agent's tool calls
func (p *PlanningAgent) processToolCalls(ctx context.Context, toolCalls []map[string]interface{}, userInputCb UserInputCallback, req *PlanningRequest, currentQuestionsAsked int, statusCb func(string), toolCallCb ToolCallCallback, toolResultCb ToolResultCallback) ([]*llm.Message, bool, int, error) {
	var (
		wg                    sync.WaitGroup
		results               = make([]*toolCallResult, len(toolCalls))
		askUserMu             sync.Mutex
		questionsAskedInBatch int
	)

	for i, toolCall := range toolCalls {
		wg.Add(1)
		go func(idx int, call map[string]interface{}) {
			defer wg.Done()

			res := &toolCallResult{idx: idx}
			results[idx] = res

			if ctx.Err() != nil {
				res.err = ctx.Err()
				return
			}

			toolID, _ := call["id"].(string)
			toolType, _ := call["type"].(string)

			if toolType != "function" {
				errorMsg := fmt.Sprintf("Invalid tool type: %s", toolType)
				res.message = &llm.Message{
					Role:     "tool",
					Content:  errorMsg,
					ToolID:   toolID,
					ToolName: "unknown",
				}
				if toolResultCb != nil {
					if err := toolResultCb("unknown", toolID, errorMsg, errorMsg); err != nil {
						logger.Warn("Failed to send planning tool result message: %v", err)
					}
				}
				return
			}

			function, ok := call["function"].(map[string]interface{})
			if !ok {
				errorMsg := "Invalid function format in tool call"
				res.message = &llm.Message{
					Role:     "tool",
					Content:  errorMsg,
					ToolID:   toolID,
					ToolName: "unknown",
				}
				if toolResultCb != nil {
					if err := toolResultCb("unknown", toolID, errorMsg, errorMsg); err != nil {
						logger.Warn("Failed to send planning tool result message: %v", err)
					}
				}
				return
			}

			toolName, _ := function["name"].(string)
			argsJSON, _ := function["arguments"].(string)

			logger.Debug("Planning agent executing tool: %s", toolName)
			if statusCb != nil {
				statusCb(fmt.Sprintf("Planning: executing %s", toolName))
			}

			var args map[string]interface{}
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				res.message = &llm.Message{
					Role:     "tool",
					Content:  fmt.Sprintf("Error parsing tool arguments: %v", err),
					ToolID:   toolID,
					ToolName: toolName,
				}
				return
			}

			// Notify UI about tool call details
			if toolCallCb != nil {
				if err := toolCallCb(toolName, toolID, args); err != nil {
					logger.Warn("Failed to send planning tool call message: %v", err)
				}
			}

			var toolResult string

			switch toolName {
			case "ask_user":
				askUserMu.Lock()
				func() {
					defer askUserMu.Unlock()

					if !req.AllowQuestions {
						toolResult = "User questions are not allowed for this planning request"
					} else if userInputCb == nil {
						toolResult = "No user input callback available"
						res.needsInput = true
					} else if req.MaxQuestions > 0 && (currentQuestionsAsked+questionsAskedInBatch) >= req.MaxQuestions {
						toolResult = "Maximum number of questions reached, cannot ask more questions"
						res.needsInput = true
					} else {
						question, _ := args["question"].(string)
						if question == "" {
							toolResult = "No question provided"
						} else {
							// Check if options are provided
							var questionText string
							if opts, hasOptions := args["options"]; hasOptions {
								// Format with options like ask_user_multiple
								if optsArray, ok := opts.([]interface{}); ok && len(optsArray) == 3 {
									var questionsText strings.Builder
									questionsText.WriteString(fmt.Sprintf("1. %s\n", question))
									for j, opt := range optsArray {
										if optStr, ok := opt.(string); ok {
											questionsText.WriteString(fmt.Sprintf("   %c. %s\n", 'a'+j, optStr))
										}
									}
									questionText = questionsText.String()
								} else {
									// Fallback to just the question if options are invalid
									questionText = question
								}
							} else {
								questionText = question
							}

							userResponse, err := userInputCb(questionText)
							if err != nil {
								toolResult = fmt.Sprintf("Failed to get user input: %v", err)
								res.needsInput = true
							} else {
								toolResult = userResponse
								res.questionsAsked = 1
								questionsAskedInBatch++
								// Store the question and answer in session
								if p.session != nil {
									p.session.AddPlanningQuestionAnswer(question, userResponse)
								}
							}
						}
					}
				}()

			case "ask_user_multiple":
				askUserMu.Lock()
				func() {
					defer askUserMu.Unlock()

					if !req.AllowQuestions {
						toolResult = "User questions are not allowed for this planning request"
					} else if userInputCb == nil {
						toolResult = "No user input callback available"
						res.needsInput = true
					} else if req.MaxQuestions > 0 && (currentQuestionsAsked+questionsAskedInBatch) >= req.MaxQuestions {
						toolResult = "Maximum number of questions reached, cannot ask more questions"
						res.needsInput = true
					} else {
						questions, _ := args["questions"].([]interface{})
						if len(questions) == 0 {
							toolResult = "No questions provided"
						} else {
							// Format questions for user callback
							var questionsText strings.Builder
							for i, q := range questions {
								if questionMap, ok := q.(map[string]interface{}); ok {
									question, _ := questionMap["question"].(string)
									options, _ := questionMap["options"].([]interface{})
									questionsText.WriteString(fmt.Sprintf("%d. %s\n", i+1, question))
									for j, opt := range options {
										if optStr, ok := opt.(string); ok {
											questionsText.WriteString(fmt.Sprintf("   %c. %s\n", 'a'+j, optStr))
										}
									}
									questionsText.WriteString("\n")
								}
							}

							userResponse, err := userInputCb(questionsText.String())
							if err != nil {
								toolResult = fmt.Sprintf("Failed to get user input: %v", err)
								res.needsInput = true
							} else {
								toolResult = userResponse
								res.questionsAsked = len(questions)
								questionsAskedInBatch += len(questions)
								// Store all questions and the combined answer in session
								if p.session != nil {
									// For multiple questions, we store each question with the full response
									// The response contains answers to all questions in formatted form
									for _, q := range questions {
										if questionMap, ok := q.(map[string]interface{}); ok {
											question, _ := questionMap["question"].(string)
											if question != "" {
												// Store each question with the full response text
												// (in practice, the full response contains all answers)
												p.session.AddPlanningQuestionAnswer(question, userResponse)
											}
										}
									}
								}
							}
						}
					}
				}()

			default:
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
			}

			// Notify UI about tool result
			if toolResultCb != nil {
				// Determine error message for the callback
				// We don't have a direct error object here for some cases, but if toolResult starts with "Error:", we can assume it's an error
				var errorMsg string
				if strings.HasPrefix(toolResult, "Error:") {
					errorMsg = toolResult
				}
				if err := toolResultCb(toolName, toolID, toolResult, errorMsg); err != nil {
					logger.Warn("Failed to send planning tool result message: %v", err)
				}
			}

			res.message = &llm.Message{
				Role:     "tool",
				Content:  toolResult,
				ToolID:   toolID,
				ToolName: toolName,
			}
		}(i, toolCall)
	}

	wg.Wait()

	var finalResults []*llm.Message
	var needsInput bool
	var questionsAsked int

	for _, res := range results {
		if res == nil {
			continue
		}
		if res.err != nil {
			return nil, false, 0, res.err
		}
		if res.message != nil {
			finalResults = append(finalResults, res.message)
		}
		if res.needsInput {
			needsInput = true
		}
		questionsAsked += res.questionsAsked
	}

	return finalResults, needsInput, questionsAsked, nil
}

// buildFileList creates a formatted list of files in the working directory
func (p *PlanningAgent) buildFileList(workingDir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entries, err := p.fs.ListDir(ctx, workingDir)
	if err != nil {
		logger.Debug("Failed to list working directory: %v", err)
		return ""
	}

	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, entry := range entries {
		if entry.IsDir {
			sb.WriteString(fmt.Sprintf("  [DIR]  %s/\n", entry.Path))
		} else {
			sb.WriteString(fmt.Sprintf("  [FILE] %s\n", entry.Path))
		}
	}

	return sb.String()
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

	// Add working directory context if available
	if p.session != nil && p.session.WorkingDir != "" {
		prompt.WriteString(fmt.Sprintf("Working Directory: %s\n\n", p.session.WorkingDir))

		// List files in the working directory
		if p.fs != nil {
			if fileList := p.buildFileList(p.session.WorkingDir); fileList != "" {
				prompt.WriteString("Files in working directory:\n")
				prompt.WriteString(fileList)
				prompt.WriteString("\n")
			}
		}

		// Detect project language/framework
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		detector := project.NewDetector(p.session.WorkingDir)
		projectTypes, err := detector.Detect(ctx)
		if err == nil && len(projectTypes) > 0 {
			bestMatch := projectTypes[0]
			prompt.WriteString(fmt.Sprintf("Project Language/Framework: %s", bestMatch.Name))
			if bestMatch.Description != "" {
				prompt.WriteString(fmt.Sprintf(" (%s)", bestMatch.Description))
			}
			prompt.WriteString("\n\n")
		}
	}

	// Add context files if provided
	if len(req.ContextFiles) > 0 {
		prompt.WriteString("Context files have been provided with the task. Review them to understand the current state.\n\n")
	}

	prompt.WriteString("Guidelines:\n")
	prompt.WriteString("- Break down complex tasks into manageable steps\n")
	prompt.WriteString("- Each step should be specific and actionable\n")
	prompt.WriteString("- Consider dependencies between steps\n")
	prompt.WriteString("- Use tools to read files or search for information when needed\n")

	if req.AllowQuestions {
		prompt.WriteString("- Ask questions when you need clarification\n")
		prompt.WriteString("- CONSERVATIVE: Ask all your questions AT ONCE using the ask_user_multiple tool to minimize user interaction\n")
		prompt.WriteString("- Use ask_user_multiple with 3 response options per question for better user experience\n")
		prompt.WriteString("- Only use ask_user for single, simple questions\n")
		if req.MaxQuestions > 0 {
			prompt.WriteString(fmt.Sprintf("- Ask at most %d questions total\n", req.MaxQuestions))
		}
	} else {
		prompt.WriteString("- Do not ask questions; work with the information provided\n")
	}

	prompt.WriteString("\n")
	prompt.WriteString("**OUTPUT MODES:**\n\n")
	prompt.WriteString("You have two possible output modes. Choose the appropriate one based on the task complexity:\n\n")
	prompt.WriteString("1. **SIMPLE MODE** (mode: \"simple\"):\n")
	prompt.WriteString("   - Use for simple, straightforward tasks or small changes\n")
	prompt.WriteString("   - Returns a flat list of steps\n\n")
	prompt.WriteString("2. **BOARD MODE** (mode: \"board\"):\n")
	prompt.WriteString("   - Use for complex, multi-step tasks, architectural changes, or creating new codebases\n")
	prompt.WriteString("   - Returns hierarchical primary tasks with subtasks\n")
	prompt.WriteString("   - Primary tasks are executed one by one with their subtasks in the orchestrator\n\n")

	prompt.WriteString("When you have enough information, provide your final response wrapped in <answer> tags.\n\n")
	prompt.WriteString("For SIMPLE MODE, use this format:\n")
	prompt.WriteString("<answer>\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"mode\": \"simple\",\n")
	prompt.WriteString("  \"plan\": [\"Step 1: ...\", \"Step 2: ...\", \"Step 3: ...\"],\n")
	prompt.WriteString("  \"questions\": [\"Question 1...\", \"Question 2...\"], // optional\n")
	prompt.WriteString("  \"needs_input\": false,\n")
	prompt.WriteString("  \"complete\": true\n")
	prompt.WriteString("}\n")
	prompt.WriteString("</answer>\n\n")

	prompt.WriteString("For BOARD MODE, use this format:\n")
	prompt.WriteString("<answer>\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"mode\": \"board\",\n")
	prompt.WriteString("  \"board\": {\n")
	prompt.WriteString("    \"description\": \"Brief overview of what this plan accomplishes\",\n")
	prompt.WriteString("    \"primary_tasks\": [\n")
	prompt.WriteString("      {\n")
	prompt.WriteString("        \"id\": \"task_1\",\n")
	prompt.WriteString("        \"text\": \"Primary task description\",\n")
	prompt.WriteString("        \"priority\": \"high\",  // optional: \"high\", \"medium\", \"low\"\n")
	prompt.WriteString("        \"description\": \"More detailed description\",\n")
	prompt.WriteString("        \"subtasks\": [\n")
	prompt.WriteString("          {\n")
	prompt.WriteString("            \"id\": \"task_1_1\",\n")
	prompt.WriteString("            \"text\": \"Subtask 1 description\",\n")
	prompt.WriteString("            \"priority\": \"high\",\n")
	prompt.WriteString("            \"status\": \"pending\"\n")
	prompt.WriteString("          },\n")
	prompt.WriteString("          {\n")
	prompt.WriteString("            \"id\": \"task_1_2\",\n")
	prompt.WriteString("            \"text\": \"Subtask 2 description\",\n")
	prompt.WriteString("            \"priority\": \"medium\",\n")
	prompt.WriteString("            \"status\": \"pending\"\n")
	prompt.WriteString("          }\n")
	prompt.WriteString("        ]\n")
	prompt.WriteString("      }\n")
	prompt.WriteString("    ]\n")
	prompt.WriteString("  },\n")
	prompt.WriteString("  \"questions\": [],  // optional, for clarifying questions\n")
	prompt.WriteString("  \"needs_input\": false,\n")
	prompt.WriteString("  \"complete\": true\n")
	prompt.WriteString("}\n")
	prompt.WriteString("</answer>\n\n")

	prompt.WriteString("Examples:\n\n")
	prompt.WriteString("SIMPLE MODE Example:\n")
	prompt.WriteString("<answer>\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"mode\": \"simple\",\n")
	prompt.WriteString("  \"plan\": [\n")
	prompt.WriteString("    \"Step 1: Analyze the function signature\",\n")
	prompt.WriteString("    \"Step 2: Fix the bug in the conditional logic\",\n")
	prompt.WriteString("    \"Step 3: Add error handling\"\n")
	prompt.WriteString("  ],\n")
	prompt.WriteString("  \"questions\": [],\n")
	prompt.WriteString("  \"needs_input\": false,\n")
	prompt.WriteString("  \"complete\": true\n")
	prompt.WriteString("}\n")
	prompt.WriteString("</answer>\n\n")

	prompt.WriteString("BOARD MODE Example:\n")
	prompt.WriteString("<answer>\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"mode\": \"board\",\n")
	prompt.WriteString("  \"board\": {\n")
	prompt.WriteString("    \"description\": \"Create a new REST API service with authentication and database integration\",\n")
	prompt.WriteString("    \"primary_tasks\": [\n")
	prompt.WriteString("      {\n")
	prompt.WriteString("        \"id\": \"task_1\",\n")
	prompt.WriteString("        \"text\": \"Set up project structure and dependencies\",\n")
	prompt.WriteString("        \"priority\": \"high\",\n")
	prompt.WriteString("        \"subtasks\": [\n")
	prompt.WriteString("          {\"id\": \"task_1_1\", \"text\": \"Initialize Go module\", \"status\": \"pending\"},\n")
	prompt.WriteString("          {\"id\": \"task_1_2\", \"text\": \"Create directory structure\", \"status\": \"pending\"},\n")
	prompt.WriteString("          {\"id\": \"task_1_3\", \"text\": \"Add dependencies to go.mod\", \"status\": \"pending\"}\n")
	prompt.WriteString("        ]\n")
	prompt.WriteString("      },\n")
	prompt.WriteString("      {\n")
	prompt.WriteString("        \"id\": \"task_2\",\n")
	prompt.WriteString("        \"text\": \"Implement authentication system\",\n")
	prompt.WriteString("        \"priority\": \"high\",\n")
	prompt.WriteString("        \"subtasks\": [\n")
	prompt.WriteString("          {\"id\": \"task_2_1\", \"text\": \"Design JWT token flow\", \"status\": \"pending\"},\n")
	prompt.WriteString("          {\"id\": \"task_2_2\", \"text\": \"Implement auth middleware\", \"status\": \"pending\"}\n")
	prompt.WriteString("        ]\n")
	prompt.WriteString("      }\n")
	prompt.WriteString("    ]\n")
	prompt.WriteString("  },\n")
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
		// If not JSON, treat as a single-step plan with default simple mode
		return &PlanningResponse{
			Mode:     PlanningModeSimple,
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
			Mode:       PlanningModeSimple,
			Plan:       plan,
			Questions:  questions,
			NeedsInput: len(questions) > 0,
			Complete:   len(questions) == 0 && len(plan) > 0,
		}
	}

	// Last resort: return the content as a single step with default simple mode
	return &PlanningResponse{
		Mode:     PlanningModeSimple,
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
		logger.Debug("JSON parse error: %v for %s", err, jsonStr)
		return nil
	}
	logger.Debug("Parsed JSON: %+v from %s", resp, jsonStr)

	// Validate based on mode
	switch resp.Mode {
	case PlanningModeSimple:
		// Simple mode: validate that we have a plan array or questions
		if len(resp.Plan) > 0 || len(resp.Questions) > 0 || resp.NeedsInput {
			return &resp
		}
		logger.Debug("Simple mode validation failed: plan=%d, questions=%d", len(resp.Plan), len(resp.Questions))
		return nil

	case PlanningModeBoard:
		// Board mode: validate that we have a board with primary tasks
		if resp.Board == nil {
			logger.Debug("Board mode validation failed: board is nil")
			return nil
		}
		if len(resp.Board.PrimaryTasks) == 0 && !resp.NeedsInput && !resp.Complete {
			logger.Debug("Board mode validation failed: no primary tasks and not marked as complete")
			return nil
		}
		return &resp

	case "":
		// Backward compatibility: no mode specified, try to infer
		if len(resp.Plan) > 0 || len(resp.Questions) > 0 {
			// Has plan array, assume simple mode
			resp.Mode = PlanningModeSimple
			return &resp
		}
		if resp.Board != nil && len(resp.Board.PrimaryTasks) > 0 {
			// Has board, assume board mode
			resp.Mode = PlanningModeBoard
			return &resp
		}
		if resp.Complete || resp.NeedsInput {
			// Valid but minimal response, default to simple mode
			resp.Mode = PlanningModeSimple
			return &resp
		}
		logger.Debug("Cannot infer mode, validation failed")
		return nil

	default:
		logger.Debug("Unknown mode: %s", resp.Mode)
		return nil
	}
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
				// If not JSON, treat as single-step partial plan with default simple mode
				return &PlanningResponse{
					Mode:       PlanningModeSimple,
					Plan:       []string{answer},
					NeedsInput: true,
					Complete:   false,
				}
			}

			// Fallback to original extraction logic
			plan := p.extractPlan(msg.Content)
			// Check for content in both simple mode (Plan) and board mode (Board.PrimaryTasks)
			hasPlanContent := plan != nil && len(plan.Plan) > 0
			hasBoardContent := plan != nil && plan.Board != nil && len(plan.Board.PrimaryTasks) > 0
			if hasPlanContent || hasBoardContent {
				plan.NeedsInput = true
				plan.Complete = false
				return plan
			}
		}
	}

	// Return minimal response with default simple mode
	return &PlanningResponse{
		Mode:       PlanningModeSimple,
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

func (a *RealToolAdapter) Name() string                       { return a.spec.Name() }
func (a *RealToolAdapter) Description() string                { return a.spec.Description() }
func (a *RealToolAdapter) Parameters() map[string]interface{} { return a.spec.Parameters() }

func (a *RealToolAdapter) Execute(ctx context.Context, params map[string]interface{}) *PlanningToolResult {
	res := a.executor.Execute(ctx, params)
	if res.RequiresUserInput {
		if res.AuthReason != "" {
			return &PlanningToolResult{Error: res.AuthReason}
		}
		return &PlanningToolResult{Error: "tool execution requires user approval"}
	}
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
			"options": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional: Three predefined response options for the user (e.g., [\"Option A\", \"Option B\", \"Option C\"])",
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

	// Validate options if provided
	var options []string
	if opts, hasOptions := params["options"]; hasOptions {
		optsArray, ok := opts.([]interface{})
		if !ok {
			return &PlanningToolResult{
				Error: "options parameter must be an array of strings",
			}
		}
		if len(optsArray) != 3 {
			return &PlanningToolResult{
				Error: "options parameter must contain exactly 3 options",
			}
		}
		options = make([]string, 3)
		for i, opt := range optsArray {
			optStr, ok := opt.(string)
			if !ok {
				return &PlanningToolResult{
					Error: fmt.Sprintf("option %d must be a string", i),
				}
			}
			options[i] = optStr
		}
	}

	// This tool is handled specially by the planning agent
	// The actual user interaction happens through the callback
	result := map[string]interface{}{
		"question": question,
		"status":   "pending_user_input",
	}
	if len(options) > 0 {
		result["options"] = options
	}

	return &PlanningToolResult{
		Result: result,
	}
}

// AskUserMultipleTool allows the planning agent to ask multiple questions with response options
type AskUserMultipleTool struct{}

func NewAskUserMultipleTool() *AskUserMultipleTool {
	return &AskUserMultipleTool{}
}

func (t *AskUserMultipleTool) Name() string { return "ask_user_multiple" }
func (t *AskUserMultipleTool) Description() string {
	return "Ask multiple questions with predefined response options to get user clarification efficiently"
}
func (t *AskUserMultipleTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"questions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"question": map[string]interface{}{
							"type":        "string",
							"description": "The question to ask the user",
						},
						"options": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string",
							},
							"description": "Three predefined response options for the user",
						},
					},
					"required": []string{"question", "options"},
				},
				"description": "Array of questions with their response options",
			},
		},
		"required": []string{"questions"},
	}
}

func (t *AskUserMultipleTool) Execute(ctx context.Context, params map[string]interface{}) *PlanningToolResult {
	questions, ok := params["questions"].([]interface{})
	if !ok {
		return &PlanningToolResult{
			Error: "questions parameter is required and must be an array",
		}
	}

	// Validate questions structure
	for i, q := range questions {
		questionMap, ok := q.(map[string]interface{})
		if !ok {
			return &PlanningToolResult{
				Error: fmt.Sprintf("question %d must be an object", i),
			}
		}

		// Validate question text
		questionText, ok := questionMap["question"].(string)
		if !ok || questionText == "" {
			return &PlanningToolResult{
				Error: fmt.Sprintf("question %d must have a non-empty 'question' string", i),
			}
		}

		// Validate options
		options, ok := questionMap["options"].([]interface{})
		if !ok || len(options) != 3 {
			return &PlanningToolResult{
				Error: fmt.Sprintf("question %d must have exactly 3 options", i),
			}
		}

		for j, opt := range options {
			if _, ok := opt.(string); !ok {
				return &PlanningToolResult{
					Error: fmt.Sprintf("question %d option %d must be a string", i, j),
				}
			}
		}
	}

	// This tool is handled specially by the planning agent
	// The actual user interaction happens through the callback
	return &PlanningToolResult{
		Result: map[string]interface{}{
			"questions": questions,
			"status":    "pending_user_input",
		},
	}
}
