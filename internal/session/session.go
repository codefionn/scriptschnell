package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// Message represents a conversation message
type Message struct {
	Role      string                   `json:"role"` // "user", "assistant", "tool"
	Content   string                   `json:"content"`
	ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolID    string                   `json:"tool_id,omitempty"`
	ToolName  string                   `json:"tool_name,omitempty"` // Name of the tool for tool responses
	Timestamp time.Time                `json:"timestamp"`

	// Native format storage (for prompt caching)
	NativeFormat      interface{} `json:"native_format,omitempty"`       // Provider-specific message format
	NativeProvider    string      `json:"native_provider,omitempty"`     // e.g., "anthropic", "openai"
	NativeModelFamily string      `json:"native_model_family,omitempty"` // e.g., "claude-3", "gpt-4"
	NativeTimestamp   time.Time   `json:"native_timestamp,omitempty"`    // When native format was created
}

// QuestionAnswer represents a question asked during planning and its answer
type QuestionAnswer struct {
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Timestamp time.Time `json:"timestamp"`
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

// Session manages a conversation session
type Session struct {
	ID                        string
	Title                     string // Auto-generated title for the session
	WorkingDir                string
	Messages                  []*Message
	FilesRead                 map[string]string // path -> content
	FilesModified             map[string]bool
	BackgroundJobs            map[string]*BackgroundJob
	AuthorizedDomains         map[string]bool  // domain -> authorized (session-level)
	AuthorizedCommands        []string         // command prefixes that are authorized (session-level)
	PlanningActive            bool             // whether planning phase is currently running
	PlanningObjective         string           // objective of current planning phase
	PlanningStartTime         time.Time        // when current planning phase started
	PlanningQuestionsAnswered []QuestionAnswer // questions asked and answered during planning
	PlanningBoard             *PlanningBoard   // hierarchical planning board with primary tasks and subtasks
	LastSandboxExitCode       int              // exit code from last sandbox execution
	LastSandboxStdout         string           // stdout from last sandbox execution
	LastSandboxStderr         string           // stderr from last sandbox execution
	CurrentProvider           string           // Current LLM provider for native message format
	CurrentModelFamily        string           // Current model family for native message format
	CurrentBranch             string           // Current Git branch (if in a repository)

	// Verification retry tracking
	VerificationAttempt      int  // Current verification attempt number (1-3)
	VerificationInProgress   bool // Whether verification is currently running
	LastUserMessageCount     int  // User message count at start of verification
	HasQueuedUserPromptCount int  // Number of queued user prompts at start of verification

	// Cost and usage tracking (accumulated across all LLM calls in the session)
	TotalCost                float64 // Total cost in dollars (sum of all calls)
	TotalTokens              int     // Total tokens used
	TotalPromptTokens        int     // Total input/prompt tokens
	TotalCompletionTokens    int     // Total output/completion tokens
	TotalCachedTokens        int     // Total cached/read-from-cache tokens
	TotalCacheCreationTokens int     // Total cache creation tokens
	TotalCacheReadTokens     int     // Total cache read tokens

	mu          sync.RWMutex
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastSavedAt time.Time
	Dirty       bool // true when there are unsaved changes
}

// BackgroundJob represents a running background process
type BackgroundJob struct {
	ID             string
	Command        string
	WorkingDir     string
	StartTime      time.Time
	Completed      bool
	ExitCode       int
	Stdout         []string
	Stderr         []string
	Process        *os.Process
	PID            int
	ProcessGroupID int
	Type           string
	CancelFunc     context.CancelFunc
	StopRequested  bool
	LastSignal     string
	Done           chan struct{}
	Mu             sync.RWMutex
}

// NewSession creates a new session
func NewSession(id, workingDir string) *Session {
	return &Session{
		ID:                        id,
		WorkingDir:                workingDir,
		Messages:                  make([]*Message, 0),
		FilesRead:                 make(map[string]string),
		FilesModified:             make(map[string]bool),
		BackgroundJobs:            make(map[string]*BackgroundJob),
		AuthorizedDomains:         make(map[string]bool),
		AuthorizedCommands:        make([]string, 0),
		PlanningQuestionsAnswered: make([]QuestionAnswer, 0),
		CreatedAt:                 time.Now(),
		UpdatedAt:                 time.Now(),
		Dirty:                     true, // new session needs an initial save
	}
}

// GenerateID creates a random session ID (base32-ish hex, 12 chars).
func GenerateID() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		timestamp := time.Now().UnixNano()
		return fmt.Sprintf("sess-%d", timestamp)
	}
	return hex.EncodeToString(buf[:])
}

// AddMessage adds a message to the session
func (s *Session) AddMessage(msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg.Timestamp = time.Now()
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetMessages returns all messages
func (s *Session) GetMessages() []*Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy
	messages := make([]*Message, len(s.Messages))
	copy(messages, s.Messages)
	return messages
}

// UserMessageCount returns how many user messages are present in the session.
func (s *Session) UserMessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, msg := range s.Messages {
		if msg != nil && msg.Role == "user" {
			count++
		}
	}
	return count
}

// TrackFileRead tracks that a file was read
func (s *Session) TrackFileRead(path, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FilesRead[path] = content
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// WasFileRead checks if a file was read in this session
func (s *Session) WasFileRead(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.FilesRead[path]
	return ok
}

// TrackFileModified tracks that a file was modified
func (s *Session) TrackFileModified(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FilesModified[path] = true
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetModifiedFiles returns list of modified files
func (s *Session) GetModifiedFiles() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	files := make([]string, 0, len(s.FilesModified))
	for path := range s.FilesModified {
		files = append(files, path)
	}
	return files
}

// AddBackgroundJob adds a background job
func (s *Session) AddBackgroundJob(job *BackgroundJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BackgroundJobs[job.ID] = job
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetBackgroundJob retrieves a background job
func (s *Session) GetBackgroundJob(id string) (*BackgroundJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.BackgroundJobs[id]
	return job, ok
}

// ListBackgroundJobs returns all background jobs
func (s *Session) ListBackgroundJobs() []*BackgroundJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]*BackgroundJob, 0, len(s.BackgroundJobs))
	for _, job := range s.BackgroundJobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// SetPlanningActive marks planning as active/inactive
func (s *Session) SetPlanningActive(active bool, objective string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PlanningActive = active
	if active {
		s.PlanningObjective = objective
		s.PlanningStartTime = time.Now()
		logger.Debug("Planning phase started: %s", objective)
	} else {
		if s.PlanningObjective != "" {
			duration := time.Since(s.PlanningStartTime)
			logger.Debug("Planning phase completed: %s (duration: %v)", s.PlanningObjective, duration)
		}
		s.PlanningObjective = ""
		s.PlanningStartTime = time.Time{}
	}
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetPlanningStatus returns the current planning status
func (s *Session) GetPlanningStatus() (active bool, objective string, startTime time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PlanningActive, s.PlanningObjective, s.PlanningStartTime
}

// SetPlanningBoard sets the planning board for the current session
func (s *Session) SetPlanningBoard(board *PlanningBoard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PlanningBoard = board
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetPlanningBoard returns the planning board for the current session
func (s *Session) GetPlanningBoard() *PlanningBoard {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PlanningBoard
}

// Clear clears the session but keeps working directory
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = make([]*Message, 0)
	s.FilesRead = make(map[string]string)
	s.FilesModified = make(map[string]bool)
	s.BackgroundJobs = make(map[string]*BackgroundJob)
	s.AuthorizedDomains = make(map[string]bool)
	s.AuthorizedCommands = make([]string, 0)
	s.PlanningActive = false
	s.PlanningObjective = ""
	s.PlanningStartTime = time.Time{}
	s.PlanningQuestionsAnswered = make([]QuestionAnswer, 0)
	s.PlanningBoard = nil
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// IsDirty reports whether the session has unsaved changes.
func (s *Session) IsDirty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Dirty
}

// MarkSaved updates bookkeeping after a successful save.
func (s *Session) MarkSaved(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastSavedAt = t
	s.Dirty = false
}

// AuthorizeDomain marks a domain as authorized for network access
func (s *Session) AuthorizeDomain(domain string) {
	domain = normalizeSessionDomain(domain)
	if domain == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.AuthorizedDomains[domain] = true
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// IsDomainAuthorized checks if a domain is authorized for network access
func (s *Session) IsDomainAuthorized(domain string) bool {
	domain = normalizeSessionDomain(domain)
	if domain == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.AuthorizedDomains[domain] {
		return true
	}

	for pattern := range s.AuthorizedDomains {
		if matchesWildcardDomain(pattern, domain) {
			return true
		}
	}

	return false
}

// GetAuthorizedDomains returns a list of all authorized domains
func (s *Session) GetAuthorizedDomains() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	domains := make([]string, 0, len(s.AuthorizedDomains))
	for domain := range s.AuthorizedDomains {
		domains = append(domains, domain)
	}
	return domains
}

func normalizeSessionDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimSuffix(d, "/")
	return d
}

func matchesWildcardDomain(pattern, domain string) bool {
	if pattern == "*" {
		return true
	}

	if !strings.HasPrefix(pattern, "*.") {
		return false
	}

	suffix := strings.TrimPrefix(pattern, "*.")
	if suffix == "" {
		return false
	}

	if domain == suffix {
		return true
	}

	return strings.HasSuffix(domain, "."+suffix)
}

// AuthorizeCommand adds a command prefix to the authorized list for this session
func (s *Session) AuthorizeCommand(commandPrefix string) {
	commandPrefix = strings.TrimSpace(commandPrefix)
	if commandPrefix == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already authorized
	for _, existing := range s.AuthorizedCommands {
		if existing == commandPrefix {
			return
		}
	}

	s.AuthorizedCommands = append(s.AuthorizedCommands, commandPrefix)
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// IsCommandAuthorized checks if a command is authorized based on session-level authorized prefixes
func (s *Session) IsCommandAuthorized(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, prefix := range s.AuthorizedCommands {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}

	return false
}

// GetAuthorizedCommands returns a list of all authorized command prefixes
func (s *Session) GetAuthorizedCommands() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	commands := make([]string, len(s.AuthorizedCommands))
	copy(commands, s.AuthorizedCommands)
	return commands
}

// CompactWithSummary replaces the oldest messages with a summary message.
// The original slice must correspond to the current head of the session when the
// compaction is applied; otherwise it will no-op and return false.
func (s *Session) CompactWithSummary(original []*Message, summary string) bool {
	if len(original) == 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Messages) < len(original) {
		return false
	}

	for i := range original {
		if s.Messages[i] != original[i] {
			return false
		}
	}

	summaryMsg := &Message{
		Role:      "system",
		Content:   summary,
		Timestamp: time.Now(),
	}

	newMessages := make([]*Message, 0, len(s.Messages)-len(original)+1)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, s.Messages[len(original):]...)
	s.Messages = newMessages
	s.UpdatedAt = time.Now()
	s.Dirty = true

	return true
}

// SetLastSandboxOutput stores the output from the last sandbox execution
func (s *Session) SetLastSandboxOutput(exitCode int, stdout, stderr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastSandboxExitCode = exitCode
	s.LastSandboxStdout = stdout
	s.LastSandboxStderr = stderr
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetLastSandboxOutput retrieves the output from the last sandbox execution
func (s *Session) GetLastSandboxOutput() (exitCode int, stdout, stderr string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastSandboxExitCode, s.LastSandboxStdout, s.LastSandboxStderr
}

// SetCurrentProvider updates the active provider/model family for native message storage
func (s *Session) SetCurrentProvider(provider, modelFamily string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentProvider = provider
	s.CurrentModelFamily = modelFamily
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetCurrentProvider returns the current provider/model family
func (s *Session) GetCurrentProvider() (provider, modelFamily string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentProvider, s.CurrentModelFamily
}

// SetCurrentBranch sets the current Git branch
func (s *Session) SetCurrentBranch(branch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentBranch = branch
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetCurrentBranch returns the current Git branch
func (s *Session) GetCurrentBranch() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentBranch
}

// NeedsConversion checks if messages need re-conversion for a new provider
func (s *Session) NeedsConversion(provider, modelFamily string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.CurrentProvider == "" {
		return true // First session
	}

	if s.CurrentProvider != provider || s.CurrentModelFamily != modelFamily {
		return true // Provider or family changed
	}

	return false
}

// SetTitle updates the session title
func (s *Session) SetTitle(title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Title = title
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetTitle returns the session title
func (s *Session) GetTitle() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Title
}

// AccumulateUsage adds usage data from an LLM call to the session totals
func (s *Session) AccumulateUsage(usage map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Helper to extract int value from interface
	toInt := func(v interface{}) int {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		case float32:
			return int(val)
		default:
			return 0
		}
	}

	// Helper to extract float64 value from interface
	toFloat64 := func(v interface{}) float64 {
		switch val := v.(type) {
		case float64:
			return val
		case float32:
			return float64(val)
		case int:
			return float64(val)
		case int64:
			return float64(val)
		default:
			return 0
		}
	}

	// Accumulate cost (may be "cost" or "total_cost" depending on provider)
	if cost, ok := usage["cost"]; ok {
		s.TotalCost += toFloat64(cost)
	}
	if cost, ok := usage["total_cost"]; ok {
		s.TotalCost += toFloat64(cost)
	}

	// Accumulate tokens
	if promptTokens, ok := usage["prompt_tokens"]; ok {
		s.TotalPromptTokens += toInt(promptTokens)
	}
	if completionTokens, ok := usage["completion_tokens"]; ok {
		s.TotalCompletionTokens += toInt(completionTokens)
	}
	if totalTokens, ok := usage["total_tokens"]; ok {
		s.TotalTokens += toInt(totalTokens)
	}

	// Alternative field names used by some providers
	if inputTokens, ok := usage["input_tokens"]; ok {
		s.TotalPromptTokens += toInt(inputTokens)
	}
	if outputTokens, ok := usage["output_tokens"]; ok {
		s.TotalCompletionTokens += toInt(outputTokens)
	}

	// Cache-related tokens
	if cachedTokens, ok := usage["cached_tokens"]; ok {
		s.TotalCachedTokens += toInt(cachedTokens)
	}
	if cacheCreationTokens, ok := usage["cache_creation_input_tokens"]; ok {
		s.TotalCacheCreationTokens += toInt(cacheCreationTokens)
	}
	if cacheReadTokens, ok := usage["cache_read_input_tokens"]; ok {
		s.TotalCacheReadTokens += toInt(cacheReadTokens)
	}

	// Recalculate total tokens if not directly provided
	if s.TotalTokens == 0 && (s.TotalPromptTokens > 0 || s.TotalCompletionTokens > 0) {
		s.TotalTokens = s.TotalPromptTokens + s.TotalCompletionTokens
	}

	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetTotalCost returns the total cost for the session
func (s *Session) GetTotalCost() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TotalCost
}

// GetTotalTokens returns the total tokens used in the session
func (s *Session) GetTotalTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TotalTokens
}

// GetUsageStats returns all usage statistics for the session
func (s *Session) GetUsageStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_cost"] = s.TotalCost
	stats["total_tokens"] = s.TotalTokens
	stats["total_prompt_tokens"] = s.TotalPromptTokens
	stats["total_completion_tokens"] = s.TotalCompletionTokens
	stats["total_cached_tokens"] = s.TotalCachedTokens
	stats["total_cache_creation_tokens"] = s.TotalCacheCreationTokens
	stats["total_cache_read_tokens"] = s.TotalCacheReadTokens

	return stats
}

// StartVerificationAttempt increments and returns the verification attempt counter
func (s *Session) StartVerificationAttempt() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VerificationAttempt++
	s.VerificationInProgress = true
	s.LastUserMessageCount = s.userMessageCountLocked()
	s.HasQueuedUserPromptCount = 0 // Reset queued prompt count
	s.UpdatedAt = time.Now()
	s.Dirty = true
	return s.VerificationAttempt
}

// GetVerificationAttempt returns the current verification attempt number
func (s *Session) GetVerificationAttempt() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VerificationAttempt
}

// ResetVerification resets verification tracking state
func (s *Session) ResetVerification() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VerificationAttempt = 0
	s.VerificationInProgress = false
	s.LastUserMessageCount = 0
	s.HasQueuedUserPromptCount = 0
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// IsVerificationInProgress returns whether verification is currently running
func (s *Session) IsVerificationInProgress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VerificationInProgress
}

// HasNewUserPrompt checks if a new user message was added since verification started
func (s *Session) HasNewUserPrompt(previousCount int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	currentCount := s.userMessageCountLocked()
	return currentCount > previousCount
}

// HasNewUserPromptOrQueued checks if a new user message was added or if there are queued prompts since verification started
func (s *Session) HasNewUserPromptOrQueued(previousCount int, previousQueuedCount int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	currentCount := s.userMessageCountLocked()
	return currentCount > previousCount || s.HasQueuedUserPromptCount > previousQueuedCount
}

// IncrementQueuedUserPromptCount increments the count of queued user prompts
func (s *Session) IncrementQueuedUserPromptCount() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.HasQueuedUserPromptCount++
	s.UpdatedAt = time.Now()
}

// DecrementQueuedUserPromptCount decrements the count of queued user prompts
func (s *Session) DecrementQueuedUserPromptCount() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.HasQueuedUserPromptCount > 0 {
		s.HasQueuedUserPromptCount--
		s.UpdatedAt = time.Now()
	}
}

// GetQueuedUserPromptCount returns the current count of queued user prompts
func (s *Session) GetQueuedUserPromptCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.HasQueuedUserPromptCount
}

// userMessageCountLocked counts user messages without acquiring the lock (assumes lock already held)
func (s *Session) userMessageCountLocked() int {
	count := 0
	for _, msg := range s.Messages {
		if msg != nil && msg.Role == "user" {
			count++
		}
	}
	return count
}

// AddPlanningQuestionAnswer adds a question and answer pair from the planning phase
func (s *Session) AddPlanningQuestionAnswer(question, answer string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	qa := QuestionAnswer{
		Question:  question,
		Answer:    answer,
		Timestamp: time.Now(),
	}
	s.PlanningQuestionsAnswered = append(s.PlanningQuestionsAnswered, qa)
	s.UpdatedAt = time.Now()
	s.Dirty = true
}

// GetPlanningQuestionsAnswered returns all questions and answers from the planning phase
func (s *Session) GetPlanningQuestionsAnswered() []QuestionAnswer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to prevent external modifications
	qas := make([]QuestionAnswer, len(s.PlanningQuestionsAnswered))
	copy(qas, s.PlanningQuestionsAnswered)
	return qas
}

// ClearPlanningQuestionsAnswered clears all stored planning questions and answers
func (s *Session) ClearPlanningQuestionsAnswered() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PlanningQuestionsAnswered = make([]QuestionAnswer, 0)
	s.UpdatedAt = time.Now()
	s.Dirty = true
}
