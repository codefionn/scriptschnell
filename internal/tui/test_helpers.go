package tui

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/actor"
)

// MockTUIProgram is a mock implementation of tea.Program for testing
type MockTUIProgram struct {
	sentMessages []tea.Msg
	mu           sync.Mutex
	quitChan     chan struct{}
}

// NewMockTUIProgram creates a new mock TUI program
func NewMockTUIProgram() *MockTUIProgram {
	return &MockTUIProgram{
		sentMessages: make([]tea.Msg, 0),
		quitChan:     make(chan struct{}),
	}
}

// Send implements the tea.Program interface for testing
func (m *MockTUIProgram) Send(msg tea.Msg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, msg)
}

// GetSentMessages returns all messages sent to this mock program
func (m *MockTUIProgram) GetSentMessages() []tea.Msg {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy to avoid race conditions
	messages := make([]tea.Msg, len(m.sentMessages))
	copy(messages, m.sentMessages)
	return messages
}

// ClearSentMessages clears the message history
func (m *MockTUIProgram) ClearSentMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = make([]tea.Msg, 0)
}

// Quit simulates program termination
func (m *MockTUIProgram) Quit() {
	close(m.quitChan)
}

// WaitForQuit waits for program to quit (with timeout)
func (m *MockTUIProgram) WaitForQuit(timeout time.Duration) bool {
	select {
	case <-m.quitChan:
		return true
	case <-time.After(timeout):
		return false
	}
}

// MockUserInteractionHandler is a mock implementation of UserInteractionHandler
type MockUserInteractionHandler struct {
	mode          string
	supportsAll   bool
	handleFunc    func(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error)
	handleCalls   []*actor.UserInteractionRequest
	mu            sync.Mutex
	responses     map[string]*actor.UserInteractionResponse // Pre-configured responses
	delay         time.Duration
	callsComplete chan struct{}
}

// NewMockUserInteractionHandler creates a new mock user interaction handler
func NewMockUserInteractionHandler() *MockUserInteractionHandler {
	return &MockUserInteractionHandler{
		mode:          "mock",
		supportsAll:   true,
		handleCalls:   make([]*actor.UserInteractionRequest, 0),
		responses:     make(map[string]*actor.UserInteractionResponse),
		callsComplete: make(chan struct{}, 100),
	}
}

// Mode returns the handler mode
func (m *MockUserInteractionHandler) Mode() string {
	return m.mode
}

// SetMode sets the handler mode
func (m *MockUserInteractionHandler) SetMode(mode string) {
	m.mode = mode
}

// SupportsInteraction indicates if this handler supports a given interaction type
func (m *MockUserInteractionHandler) SupportsInteraction(interactionType actor.InteractionType) bool {
	return m.supportsAll
}

// SetSupportsAll sets whether the handler supports all interaction types
func (m *MockUserInteractionHandler) SetSupportsAll(supportsAll bool) {
	m.supportsAll = supportsAll
}

// HandleInteraction processes a user interaction request
func (m *MockUserInteractionHandler) HandleInteraction(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error) {
	m.mu.Lock()
	m.handleCalls = append(m.handleCalls, req)
	m.mu.Unlock()

	// Apply delay if configured
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return &actor.UserInteractionResponse{
				RequestID: req.RequestID,
				Cancelled: true,
				Error:     ctx.Err(),
			}, nil
		}
	}

	// Check for pre-configured response
	m.mu.Lock()
	if resp, ok := m.responses[req.RequestID]; ok {
		m.mu.Unlock()
		m.callsComplete <- struct{}{}
		return resp, nil
	}
	m.mu.Unlock()

	// Use custom handler function if provided
	if m.handleFunc != nil {
		resp, err := m.handleFunc(ctx, req)
		m.callsComplete <- struct{}{}
		return resp, err
	}

	// Default response: approve/answer everything
	m.callsComplete <- struct{}{}
	return &actor.UserInteractionResponse{
		RequestID:    req.RequestID,
		Approved:     true,
		Answer:       "test answer",
		Acknowledged: true,
	}, nil
}

// GetHandleCalls returns all calls made to HandleInteraction
func (m *MockUserInteractionHandler) GetHandleCalls() []*actor.UserInteractionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]*actor.UserInteractionRequest, len(m.handleCalls))
	copy(calls, m.handleCalls)
	return calls
}

// ClearHandleCalls clears the call history
func (m *MockUserInteractionHandler) ClearHandleCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handleCalls = make([]*actor.UserInteractionRequest, 0)
}

// SetResponse configures a predefined response for a specific request ID
func (m *MockUserInteractionHandler) SetResponse(requestID string, resp *actor.UserInteractionResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[requestID] = resp
}

// ClearResponses clears all pre-configured responses
func (m *MockUserInteractionHandler) ClearResponses() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = make(map[string]*actor.UserInteractionResponse)
}

// SetHandleFunc sets a custom handler function
func (m *MockUserInteractionHandler) SetHandleFunc(fn func(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error)) {
	m.handleFunc = fn
}

// SetDelay sets a delay before handling interactions (useful for timeout tests)
func (m *MockUserInteractionHandler) SetDelay(delay time.Duration) {
	m.delay = delay
}

// WaitForCall waits for a handle call to occur
func (m *MockUserInteractionHandler) WaitForCall(timeout time.Duration) bool {
	select {
	case <-m.callsComplete:
		return true
	case <-time.After(timeout):
		return false
	}
}

// GetCallCount returns the number of handle calls
func (m *MockUserInteractionHandler) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.handleCalls)
}

// TestModelHelper provides helper methods for testing TUI models
type TestModelHelper struct {
	model tea.Model
}

// NewTestModelHelper creates a new test model helper
func NewTestModelHelper(model tea.Model) *TestModelHelper {
	return &TestModelHelper{model: model}
}

// UpdateWithMsg applies a message to the model and returns the updated model
func (h *TestModelHelper) UpdateWithMsg(msg tea.Msg) tea.Model {
	updated, _ := h.model.Update(msg)
	h.model = updated
	return updated
}

// UpdateWithMsgAndCmd applies a message to the model and returns both model and command
func (h *TestModelHelper) UpdateWithMsgAndCmd(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := h.model.Update(msg)
	h.model = updated
	return updated, cmd
}

// GetCurrentModel returns the current model state
func (h *TestModelHelper) GetCurrentModel() tea.Model {
	return h.model
}

// SendKeyPress sends a key press to the model
func (h *TestModelHelper) SendKeyPress(keyType tea.KeyType) tea.Model {
	return h.UpdateWithMsg(tea.KeyMsg{Type: keyType})
}

// SendEnter sends Enter key press
func (h *TestModelHelper) SendEnter() tea.Model {
	return h.SendKeyPress(tea.KeyEnter)
}

// SendEscape sends Escape key press
func (h *TestModelHelper) SendEscape() tea.Model {
	return h.SendKeyPress(tea.KeyEsc)
}

// SendUp sends Up arrow key press
func (h *TestModelHelper) SendUp() tea.Model {
	return h.SendKeyPress(tea.KeyUp)
}

// SendDown sends Down arrow key press
func (h *TestModelHelper) SendDown() tea.Model {
	return h.SendKeyPress(tea.KeyDown)
}

// SendRune sends a character key press
func (h *TestModelHelper) SendRune(r rune) tea.Model {
	return h.UpdateWithMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// SendString sends a string as individual key presses
func (h *TestModelHelper) SendString(s string) tea.Model {
	for _, r := range s {
		h.model = h.SendRune(r)
	}
	return h.model
}

// SendResize sends a window resize message
func (h *TestModelHelper) SendResize(width, height int) tea.Model {
	return h.UpdateWithMsg(tea.WindowSizeMsg{Width: width, Height: height})
}

// Helper functions for creating test scenarios

// CreateTestAuthorizationRequest creates a test authorization request
func CreateTestAuthorizationRequest(authID, toolName, reason string) *AuthorizationRequest {
	return &AuthorizationRequest{
		AuthID:       authID,
		TabID:        1,
		ToolName:     toolName,
		Parameters:   map[string]interface{}{"test": "param"},
		Reason:       reason,
		ResponseChan: make(chan bool, 1),
	}
}

// CreateTestUserInputRequest creates a test user input request
func CreateTestUserInputRequest(question string) UserInputRequestMsg {
	return UserInputRequestMsg{
		Question: question,
		Response: make(chan string, 1),
		Error:    make(chan error, 1),
	}
}

// CreateTestMultipleQuestionsRequest creates a test multiple questions request
// Note: The Response channel is chan string, not chan map[string]string
// This matches the legacy TUI-based system
func CreateTestMultipleQuestionsRequest(questions string) UserMultipleQuestionsRequestMsg {
	return UserMultipleQuestionsRequestMsg{
		Questions: questions,
		Response:  make(chan string, 1),
		Error:     make(chan error, 1),
	}
}

// CreateSampleMultipleChoicePrompt returns a sample multiple choice prompt for testing
func CreateSampleMultipleChoicePrompt() string {
	return `1. What is your preferred programming language?
   a. Go
   b. Python
   c. JavaScript
   d. Rust

2. What is your experience level?
   a. Beginner
   b. Intermediate
   c. Advanced
   d. Expert`
}

// CreateSampleSingleQuestionPrompt returns a sample single question for testing
func CreateSampleSingleQuestionPrompt() string {
	return `1. What is your name?`
}

// CreateSampleMalformedPrompt returns a malformed prompt for testing error handling
func CreateSampleMalformedPrompt() string {
	return `This is not a properly formatted prompt
and should fall back to single input mode`
}

// CreateSamplePromptWithTooManyOptions returns a prompt with more than 10 options
func CreateSamplePromptWithTooManyOptions() string {
	return `1. Which option?
   a. Option A
   b. Option B
   c. Option C
   d. Option D
   e. Option E
   f. Option F
   g. Option G
   h. Option H
   i. Option I
   j. Option J
   k. Option K`
}

// CreateSamplePromptWithTooFewOptions returns a prompt with only 1 option
func CreateSamplePromptWithTooFewOptions() string {
	return `1. Which option?
   a. Only one option`
}

// CreateSamplePromptWithUnicode returns a prompt with unicode characters
func CreateSamplePromptWithUnicode() string {
	return `1. What is your favorite emoji?
   a. 😊
   b. 🎉
   c. 🔥
   d. 🚀`
}
