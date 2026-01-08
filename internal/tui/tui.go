package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/htmlconv"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/provider"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/tools"
	"golang.org/x/term"
)

const (
	todoPanelTriggerWidth = 80
	todoPanelWidth        = 36
	todoPanelSpacing      = 2
	minContentWidth       = 80
)

var ErrQuitRequested = errors.New("quit requested")

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170")).
			MarginLeft(2)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginLeft(2)

	contextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	contextUsageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				MarginLeft(2)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			MarginLeft(2)

	todoPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("241")).
			Padding(0, 1).
			Width(todoPanelWidth)

	todoTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)

	todoItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	todoCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Strikethrough(true)

	todoInProgressStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true)

	todoEmptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	todoErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	toolCallStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")). // Greyer color for tool calls
			Bold(true)
)

const (
	errorDisplayDuration   = 5 * time.Second
	resizeViewportDebounce = 75 * time.Millisecond
)

const defaultInputPlaceholder = "Type your prompt here... (@ for files, Alt+Enter or Ctrl+J for newline, Ctrl+X for commands)"

// Message represents a chat message with metadata
type message struct {
	role       string
	content    string // raw markdown content
	timestamp  string
	toolName   string // for tool result messages
	toolID     string // for tool result messages
	fullResult string // stores the full tool result for potential summary replacement
	summarized bool   // indicates if this tool result has been summarized
}

type Model struct {
	viewport             viewport.Model
	textarea             textarea.Model
	messages             []message
	ready                bool
	width                int
	height               int
	contentWidth         int
	processingStatus     string // Current processing status (e.g., "Calling tool: write_file_diff")
	spinner              spinner.Model
	spinnerActive        bool
	animationsDisabled   bool
	queuedPrompts        map[int][]string // queued prompts per tab
	err                  error
	errVisibleUntil      time.Time
	ctrlCCount           int
	lastCtrlCTime        time.Time
	commandMode          bool
	overlayActive        bool
	onSubmit             func(string) error
	onCommand            func(string) error
	onStop               func() error
	onBackground         func() error
	onPromptActivity     func()
	currentModel         string
	lastUpdateHeight     int
	renderer             *glamour.TermRenderer
	renderWrapWidth      int
	rendererCache        map[int]*glamour.TermRenderer // Cache renderers by width
	rendererInitInFlight bool                          // Track if async init is running
	rendererInitMutex    sync.Mutex                    // Protect cache and flag
	contextFile          string
	suggestions          []string
	selectedSuggIndex    int
	filesystem           fs.FileSystem
	gitIgnore            gitIgnoreEvaluator
	workingDir           string
	originalSuggestions  []string // Store original suggestions for cycling
	originalInput        string   // Store original input before cycling
	tabCycleIndex        int      // Tracks current suggestion index for tab cycling
	contextFreePercent   int
	contextWindow        int
	openRouterUsage      map[string]interface{} // OpenRouter usage data (tokens, cost, etc.)
	thinkingTokens       int                    // Current thinking/reasoning tokens during generation
	contentReceived      bool                   // Track if any content has been received in current generation
	sanitizeState        ansiSanitizeState
	showTodoPanel        bool
	todoClient           *tools.TodoActorClient
	todoViewport         viewport.Model
	todoContent          string // Cached todo content for viewport
	todoContentHeight    int    // Height of todo content
	viewportDirty        bool
	viewportRefreshToken int
	config               *config.Config
	activeMCPProvider    func() []string

	// Multi-session tab state
	sessions         []*TabSession
	activeSessionIdx int
	sessionIDCounter int
	onSaveSession    func(*session.Session) error // Callback to save a session

	// Multi-tab concurrent generation state
	factory          *RuntimeFactory // Creates per-tab runtimes
	program          *tea.Program    // Reference to tea.Program for self-messaging
	concurrentGens   map[int]bool    // Track which tabs are generating (tabID -> bool)
	concurrentGensMu sync.RWMutex    // Protect concurrentGens map

	// Authorization state
	pendingAuthorizations   map[string]*AuthorizationRequest // authID -> request
	authorizationCounter    int                              // Counter for unique auth IDs
	authorizationMu         sync.Mutex                       // Protect authorization state
	activeAuthorizationID   string                           // Currently displayed authorization
	authorizationDialogOpen bool                             // Is authorization dialog visible
	authorizationDialog     AuthorizationDialog              // The authorization dialog component

	// User Question Dialog state
	userQuestionDialogOpen bool        // Is user question dialog visible
	userQuestionDialog     tea.Model   // The user question dialog component
	userQuestionResponse   chan string // Channel to send the answer back
	userQuestionError      chan error  // Channel to send any error
}

// EndUserQuestionsMsg is sent when the user finishes answering questions
type EndUserQuestionsMsg struct{}

// ErrMsg is an error message type
type ErrMsg error

// GeneratingMsg is sent when streaming chunks arrive
type GeneratingMsg struct {
	Content string
}

// CompleteMsg is sent when generation completes
type CompleteMsg struct{}

// ToolCallMsg is sent when a tool is being called
type ToolCallMsg struct {
	ToolName   string
	ToolID     string
	Parameters map[string]interface{}
}

// ToolResultMsg is sent when a tool execution completes
type ToolResultMsg struct {
	ToolName string
	ToolID   string
	Result   string
	Error    string
}

// ContextUsageMsg updates the UI with remaining context percentage
type ContextUsageMsg struct {
	FreePercent   int
	ContextWindow int
}

// UserInputRequestMsg is sent when the planning agent needs user input
type UserInputRequestMsg struct {
	Question string
	Response chan string // Channel to send the answer back
	Error    chan error  // Channel to send any error
}

// UserMultipleQuestionsRequestMsg is sent when the planning agent has multiple questions
type UserMultipleQuestionsRequestMsg struct {
	Questions string
	Response  chan string // Channel to send the answers back
	Error     chan error  // Channel to send any error
}

// OpenRouterUsageMsg updates the UI with OpenRouter usage data (tokens, cost, etc.)
type OpenRouterUsageMsg struct {
	TabID int
	Usage map[string]interface{}
}

type viewportRefreshMsg struct {
	token int
}

// AuthorizationRequestMsg is sent when a tool requires user authorization
type AuthorizationRequestMsg struct {
	ToolCall   map[string]any
	ToolName   string
	Parameters map[string]any
	Reason     string
}

// ProcessingStatusMsg is sent to update the processing status indicator
type ProcessingStatusMsg struct {
	Status string
}

// Tab-specific message types for concurrent generation

// TabGeneratingMsg is sent when a specific tab receives streaming content
type TabGeneratingMsg struct {
	TabID   int
	Content string
}

// TabProcessingStatusMsg updates the processing status for a specific tab
type TabProcessingStatusMsg struct {
	TabID  int
	Status string
}

// TabGenerationCompleteMsg is sent when a tab's generation completes
type TabGenerationCompleteMsg struct {
	TabID int
	Error error
}

// TabContextUsageMsg updates free context for a specific tab
type TabContextUsageMsg struct {
	TabID         int
	FreePercent   int
	ContextWindow int
}

// TabAuthorizationRequiredMsg is sent when a tab needs authorization
type TabAuthorizationRequiredMsg struct {
	TabID  int
	Reason string
}

// TabToolCallMsg is sent when a tool is called in a specific tab
type TabToolCallMsg struct {
	TabID      int
	ToolName   string
	ToolID     string
	Parameters map[string]interface{}
}

// TabToolResultMsg is sent when a tool execution completes in a specific tab
type TabToolResultMsg struct {
	TabID    int
	ToolName string
	ToolID   string
	Result   string
	Error    string
}

// AuthorizationRequest represents a pending authorization request
type AuthorizationRequest struct {
	AuthID       string
	TabID        int
	ToolName     string
	Parameters   map[string]interface{}
	Reason       string
	ResponseChan chan bool // Channel to send approval result
}

// AuthorizationResponseMsg is sent when user approves/denies authorization
type AuthorizationResponseMsg struct {
	AuthID   string
	Approved bool
}

// ShowAuthorizationDialogMsg is sent to display an authorization dialog
type ShowAuthorizationDialogMsg struct {
	Request *AuthorizationRequest
}

// RendererReadyMsg is sent when async renderer creation completes
type RendererReadyMsg struct {
	Renderer *glamour.TermRenderer
	Width    int
	Err      error
}

// NewTabMsg is sent to create a new tab
type NewTabMsg struct {
	Name string
}

func New(currentModel, contextFile string, disableAnimations bool) *Model {
	ta := textarea.New()
	ta.Placeholder = defaultInputPlaceholder
	ta.Focus()
	ta.Prompt = "│ "
	ta.CharLimit = 10000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false)

	vp := viewport.New(80, 20)
	vp.SetContent("")

	// Create markdown renderer with a default width
	// Will be updated when window size is received
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
		glamour.WithPreservedNewLines(),
	)

	sp := spinner.New(
		spinner.WithSpinner(spinner.Line),
		spinner.WithStyle(statusStyle.MarginLeft(0)),
	)

	// Initialize renderer cache
	rendererCache := make(map[int]*glamour.TermRenderer)
	if renderer != nil {
		rendererCache[80] = renderer
	}

	m := &Model{
		textarea:              ta,
		viewport:              vp,
		messages:              []message{},
		currentModel:          currentModel,
		contextFile:           contextFile,
		renderer:              renderer,
		rendererCache:         rendererCache,
		spinner:               sp,
		animationsDisabled:    disableAnimations,
		contextFreePercent:    100,
		renderWrapWidth:       80,
		queuedPrompts:         make(map[int][]string),
		concurrentGens:        make(map[int]bool),
		pendingAuthorizations: make(map[string]*AuthorizationRequest),
		authorizationCounter:  0,
	}

	if width, height, ok := detectTerminalSize(); ok {
		m.applyWindowSize(width, height)
	}

	return m
}

// NewWithFactory creates a new TUI model with RuntimeFactory for multi-tab concurrent generation
func NewWithFactory(factory *RuntimeFactory, cfg *config.Config, providerMgr *provider.Manager) *Model {
	// Get current model name from provider manager
	currentModel := providerMgr.GetOrchestrationModel()

	m := New(currentModel, "", cfg.DisableAnimations)
	m.factory = factory
	m.config = cfg
	m.workingDir = factory.GetWorkingDir()

	// Initialize context file without requiring orchestrator
	m.contextFile = m.getExtendedContextFileWithoutOrchestrator()

	return m
}

// getExtendedContextFileWithoutOrchestrator replicates GetExtendedContextFile logic without requiring an orchestrator
// This allows us to show the context file on startup before any runtime is created
func (m *Model) getExtendedContextFileWithoutOrchestrator() string {
	if m.factory == nil {
		return ""
	}

	filesystem := m.factory.GetSharedFilesystem()
	ctx := context.Background()

	// First try AGENTS.md (llm.AgentsFileName)
	exists, err := filesystem.Exists(ctx, "AGENTS.md")
	if err == nil && exists {
		return "AGENTS.md"
	}

	// Fall back to README variants
	candidates := []string{"README.md", "README.txt", "README"}
	for _, candidate := range candidates {
		exists, err := filesystem.Exists(ctx, candidate)
		if err == nil && exists {
			return candidate
		}
	}

	return ""
}

func detectTerminalSize() (int, int, bool) {
	candidates := []*os.File{os.Stdout, os.Stdin, os.Stderr}
	for _, f := range candidates {
		if f == nil {
			continue
		}
		fd := int(f.Fd())
		if !term.IsTerminal(fd) {
			continue
		}
		if width, height, err := term.GetSize(fd); err == nil && width > 0 && height > 0 {
			return width, height, true
		}
	}
	return 0, 0, false
}

func (m *Model) addToolMessageForTab(tabIdx int, toolName, toolID, content, fullResult string, summarized bool) {
	if !m.validTabIndex(tabIdx) {
		return
	}

	msg := message{
		role:       "Tool",
		content:    content,
		timestamp:  time.Now().Format("15:04:05"),
		toolName:   toolName,
		toolID:     toolID,
		fullResult: fullResult,
		summarized: summarized,
	}
	msgs := append(m.sessions[tabIdx].Messages, msg)
	m.storeMessagesForTab(tabIdx, msgs, tabIdx == m.activeSessionIdx)
}

// SetFilesystem sets the filesystem and working directory for filepath autocomplete
func (m *Model) SetFilesystem(fs fs.FileSystem, workingDir string) {
	m.filesystem = fs
	m.workingDir = workingDir

	if checker, err := newGitIgnoreChecker(workingDir); err == nil {
		m.gitIgnore = checker
	} else {
		m.gitIgnore = nil
	}

	// Initialize tabs after config and working directory are set
	if m.config != nil && len(m.sessions) == 0 {
		if err := m.restoreTabs(); err != nil {
			logger.Warn("Failed to restore tabs: %v", err)
		}
	}
}

func (m *Model) isGitIgnored(path string) bool {
	if m.gitIgnore == nil {
		return false
	}
	return m.gitIgnore.ignores(path)
}

// SetTodoClient configures the TodoActorClient for accessing todo state
func (m *Model) SetTodoClient(client *tools.TodoActorClient) {
	m.todoClient = client
	// Refresh todo content when client changes
	if m.showTodoPanel {
		m.refreshTodoContent()
	}
}

// SetConfig stores the application configuration for UI elements that need it.
func (m *Model) SetConfig(cfg *config.Config) {
	m.config = cfg
}

// SetActiveMCPProvider registers a callback that supplies currently active MCP servers.
func (m *Model) SetActiveMCPProvider(provider func() []string) {
	m.activeMCPProvider = provider
}

// SetOnSaveSession registers a callback for when a session needs to be saved
func (m *Model) SetOnSaveSession(callback func(*session.Session) error) {
	m.onSaveSession = callback
}

// SetProgram stores a reference to the tea.Program for self-messaging
func (m *Model) SetProgram(program *tea.Program) {
	m.program = program
}

// GetActiveTab returns the currently active tab session
func (m *Model) GetActiveTab() *TabSession {
	if m.activeSessionIdx >= 0 && m.activeSessionIdx < len(m.sessions) {
		return m.sessions[m.activeSessionIdx]
	}
	return nil
}

// GetAllTabs returns all tab sessions
func (m *Model) GetAllTabs() []*TabSession {
	return m.sessions
}

// setTabGenerating sets the generation state for a specific tab
func (m *Model) setTabGenerating(tabIdx int, generating bool) {
	m.concurrentGensMu.Lock()
	defer m.concurrentGensMu.Unlock()

	if tabIdx >= 0 && tabIdx < len(m.sessions) {
		m.sessions[tabIdx].Generating = generating
		m.concurrentGens[m.sessions[tabIdx].ID] = generating
		logger.Debug("Tab %d generation state set to %v", m.sessions[tabIdx].ID, generating)
	}
}

// isCurrentTabGenerating returns true if the currently active tab is generating
func (m *Model) isCurrentTabGenerating() bool {
	if m.activeSessionIdx < 0 || m.activeSessionIdx >= len(m.sessions) {
		return false
	}
	return m.sessions[m.activeSessionIdx].IsGenerating()
}

// findTabIndexByID finds the tab index by tab ID, returns -1 if not found
func (m *Model) findTabIndexByID(tabID int) int {
	for i, tab := range m.sessions {
		if tab.ID == tabID {
			return i
		}
	}
	return -1
}

func (m *Model) scheduleViewportRefresh() tea.Cmd {
	m.viewportRefreshToken++
	token := m.viewportRefreshToken
	return tea.Tick(resizeViewportDebounce, func(time.Time) tea.Msg {
		return viewportRefreshMsg{token: token}
	})
}

func (m *Model) createRendererAsync(wrapWidth int) tea.Cmd {
	return func() tea.Msg {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(wrapWidth),
			glamour.WithPreservedNewLines(),
		)
		return RendererReadyMsg{
			Renderer: renderer,
			Width:    wrapWidth,
			Err:      err,
		}
	}
}

func (m *Model) Init() tea.Cmd {
	initialWindowSize := func() tea.Msg {
		fd := int(os.Stdout.Fd())
		if !term.IsTerminal(fd) {
			return nil
		}
		if width, height, err := term.GetSize(fd); err == nil && width > 0 && height > 0 {
			return tea.WindowSizeMsg{
				Width:  width,
				Height: height,
			}
		}
		return nil
	}

	return tea.Batch(
		textarea.Blink,
		initialWindowSize,
	)
}

func (m *Model) applyWindowSize(width, height int) (bool, bool, tea.Cmd) {
	if width <= 0 || height <= 0 {
		return false, false, nil
	}

	widthChanged := !m.ready || width != m.width
	heightChanged := !m.ready || height != m.height

	m.width = width
	m.height = height

	available := width
	shouldShowTodos := width > todoPanelTriggerWidth
	if shouldShowTodos {
		if width-todoPanelWidth-todoPanelSpacing >= minContentWidth {
			m.showTodoPanel = true
			available = width - todoPanelWidth - todoPanelSpacing
		} else {
			m.showTodoPanel = false
		}
	} else {
		m.showTodoPanel = false
	}
	m.contentWidth = available

	vpHeight := height - 10
	if vpHeight < 1 {
		vpHeight = 1
	}
	if !m.ready {
		m.viewport = viewport.New(available, vpHeight)
	} else {
		m.viewport.Width = available
		m.viewport.Height = vpHeight
	}

	textareaWidth := available - 4
	if textareaWidth < 20 {
		textareaWidth = available
	}
	if textareaWidth < 10 {
		textareaWidth = 10
	}
	m.textarea.SetWidth(textareaWidth)

	wrapWidth := available - 4
	if wrapWidth < 20 {
		wrapWidth = available
	}
	if wrapWidth < 10 {
		wrapWidth = 10
	}

	// Check if we need a different renderer
	var rendererCmd tea.Cmd
	needsNewRenderer := wrapWidth != m.renderWrapWidth || m.renderer == nil

	if needsNewRenderer {
		m.rendererInitMutex.Lock()

		// Check cache first
		if cachedRenderer, exists := m.rendererCache[wrapWidth]; exists {
			m.renderer = cachedRenderer
			m.renderWrapWidth = wrapWidth
			m.rendererInitMutex.Unlock()
		} else if !m.rendererInitInFlight {
			// Mark that initialization is in flight and create renderer async
			m.rendererInitInFlight = true
			m.rendererInitMutex.Unlock()
			rendererCmd = m.createRendererAsync(wrapWidth)
		} else {
			// Renderer init already in flight, don't start another
			m.rendererInitMutex.Unlock()
		}
	}

	// Initialize todo viewport if panel is shown
	if m.showTodoPanel {
		// Calculate todo viewport height (main content area minus input area)
		todoVpHeight := height - 10 // Reserve space for input and status
		if todoVpHeight < 5 {
			todoVpHeight = 5
		}

		if !m.ready || m.todoViewport.Height == 0 {
			m.todoViewport = viewport.New(todoPanelWidth-4, todoVpHeight) // -4 for panel borders
		} else {
			m.todoViewport.Width = todoPanelWidth - 4
			m.todoViewport.Height = todoVpHeight
		}

		// Refresh todo content when size changes
		m.refreshTodoContent()
	}

	if !m.ready {
		m.ready = true
	}

	return widthChanged, heightChanged, rendererCmd
}

type ansiSeqMode int

const (
	ansiModeNone ansiSeqMode = iota
	ansiModeCSI
	ansiModeOSCTerminated
	ansiModeStringTerminated // DCS, SOS, PM, APC (terminated by ST)
)

type ansiSanitizeState struct {
	mode       ansiSeqMode
	pendingESC bool
	escContext ansiSeqMode
}

// sanitizePromptInput removes ANSI escape sequences (CSI, OSC, DCS, etc.)
// from the provided string so that terminal control queries aren't treated
// as literal user input. It persists state to handle fragmented escape
// sequences that arrive across multiple updates.
func sanitizePromptInput(input string, state *ansiSanitizeState) string {
	if input == "" {
		return input
	}

	var b strings.Builder
	b.Grow(len(input))

	for i := 0; i < len(input); {
		if state.pendingESC {
			if i >= len(input) {
				break
			}
			c := input[i]
			i++
			context := state.escContext
			state.pendingESC = false
			state.escContext = ansiModeNone
			switch context {
			case ansiModeNone:
				switch c {
				case '[':
					state.mode = ansiModeCSI
				case ']':
					state.mode = ansiModeOSCTerminated
				case 'P', 'X', '^', '_':
					state.mode = ansiModeStringTerminated
				}
				continue
			case ansiModeOSCTerminated:
				if c == '\\' {
					state.mode = ansiModeNone
				}
				continue
			case ansiModeStringTerminated:
				if c == '\\' {
					state.mode = ansiModeNone
				}
				continue
			}
		}

		if state.mode != ansiModeNone {
			switch state.mode {
			case ansiModeCSI:
				for i < len(input) {
					c := input[i]
					i++
					if c >= 0x40 && c <= 0x7e {
						state.mode = ansiModeNone
						break
					}
				}
				continue

			case ansiModeOSCTerminated:
				terminated := false
				for i < len(input) {
					c := input[i]
					i++
					if c == 0x07 { // BEL
						state.mode = ansiModeNone
						terminated = true
						break
					}
					if c == 0x1b {
						state.pendingESC = true
						state.escContext = ansiModeOSCTerminated
						terminated = true
						break
					}
				}
				if !terminated {
					state.mode = ansiModeNone
				}
				continue

			case ansiModeStringTerminated:
				terminated := false
				for i < len(input) {
					c := input[i]
					i++
					if c == 0x1b {
						state.pendingESC = true
						state.escContext = ansiModeStringTerminated
						terminated = true
						break
					}
				}
				if !terminated {
					state.mode = ansiModeNone
				}
				continue
			}
		}

		if input[i] == 0x1b {
			i++
			if i >= len(input) {
				state.pendingESC = true
				state.escContext = ansiModeNone
				break
			}
			state.pendingESC = true
			state.escContext = ansiModeNone
			continue
		}

		if state.mode == ansiModeNone && input[i] == ']' {
			j := i + 1
			hasDigit := false
			hasSemicolon := false
			for ; j < len(input); j++ {
				c := input[j]
				if c >= '0' && c <= '9' {
					hasDigit = true
					continue
				}
				if c == ';' {
					hasSemicolon = true
					continue
				}
				break
			}
			if hasDigit && hasSemicolon {
				state.mode = ansiModeOSCTerminated
				i++
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(input[i:])
		if r == utf8.RuneError && size == 1 {
			i += size
			continue
		}
		if r == 0x07 {
			i += size
			continue
		}

		b.WriteString(input[i : i+size])
		i += size
	}

	return b.String()
}

// getCommandSuggestions returns matching command suggestions based on input
func getCommandSuggestions(input string) []string {
	commandList := availableCommandSuggestions()
	if input == "" || input == "/" {
		return commandList
	}

	var matches []string
	inputLower := strings.ToLower(input)
	for _, cmd := range commandList {
		if strings.HasPrefix(strings.ToLower(cmd), inputLower) {
			matches = append(matches, cmd)
		}
	}

	return matches
}

// extractFilepathContext extracts the position and partial path after @ symbol
// Returns: atPos (index of @), partialPath, found
func extractFilepathContext(input string, cursorPos int) (int, string, bool) {
	// Find the last @ before cursor position
	atPos := -1
	for i := cursorPos - 1; i >= 0; i-- {
		if input[i] == '@' {
			atPos = i
			break
		}
		// Stop if we hit a space (new word boundary)
		if input[i] == ' ' || input[i] == '\n' {
			break
		}
	}

	if atPos == -1 {
		return -1, "", false
	}

	// Extract the partial path after @
	partialPath := ""
	if atPos+1 < len(input) {
		partialPath = input[atPos+1 : cursorPos]
	}

	return atPos, partialPath, true
}

// shouldTreatAsMultilineEnter returns true when the given key message represents
// a key combination that should insert a newline instead of submitting the prompt.
// Note: Most terminals don't send Shift modifier for Enter, so we use Alt+Enter and Ctrl+J instead.
func shouldTreatAsMultilineEnter(msg tea.KeyMsg) bool {
	// Alt+Enter is the primary newline key
	if msg.Type == tea.KeyEnter && msg.Alt {
		return true
	}

	// Ctrl+J is traditionally line feed
	if msg.Type == tea.KeyCtrlJ {
		return true
	}

	return false
}

// getFilepathSuggestions returns matching file/directory paths based on partial input
func (m *Model) getFilepathSuggestions(partialPath string) []string {
	if m.filesystem == nil {
		return nil
	}

	ctx := context.Background()

	// If the partial path contains a directory separator, do directory-based search
	if strings.Contains(partialPath, "/") {
		return m.getDirectoryBasedSuggestions(ctx, partialPath)
	}

	// Otherwise, search for matching filenames recursively
	return m.getRecursiveFilenameSuggestions(ctx, partialPath)
}

// getDirectoryBasedSuggestions searches within a specific directory path
func (m *Model) getDirectoryBasedSuggestions(ctx context.Context, partialPath string) []string {
	var searchDir string
	var prefix string

	if strings.HasSuffix(partialPath, "/") {
		// Path ends with / - list that directory
		searchDir = filepath.Join(m.workingDir, partialPath)
		prefix = ""
	} else {
		// Path has a partial filename - list parent dir and filter
		dir, file := filepath.Split(partialPath)
		if dir == "" {
			searchDir = m.workingDir
		} else {
			searchDir = filepath.Join(m.workingDir, dir)
		}
		prefix = file
	}

	// List directory contents
	entries, err := m.filesystem.ListDir(ctx, searchDir)
	if err != nil {
		return nil
	}

	// Filter and format suggestions
	var suggestions []string
	for _, entry := range entries {
		baseName := filepath.Base(entry.Path)

		if m.isGitIgnored(entry.Path) {
			continue
		}

		// Check if entry matches prefix
		if prefix != "" && !strings.HasPrefix(baseName, prefix) {
			continue
		}

		// Skip hidden files unless prefix starts with .
		if strings.HasPrefix(baseName, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}

		// Build the suggestion path
		var suggestionPath string
		if strings.HasSuffix(partialPath, "/") {
			suggestionPath = partialPath + baseName
		} else {
			dir, _ := filepath.Split(partialPath)
			suggestionPath = dir + baseName
		}

		// Add trailing / for directories
		if entry.IsDir {
			suggestionPath += "/"
		}

		suggestions = append(suggestions, suggestionPath)
	}

	return suggestions
}

// getRecursiveFilenameSuggestions searches for matching filenames in current dir and subdirectories
func (m *Model) getRecursiveFilenameSuggestions(ctx context.Context, prefix string) []string {
	var suggestions []string
	maxDepth := 3    // Limit recursion depth to avoid performance issues
	maxResults := 20 // Limit total results

	// If empty prefix, just show current directory contents
	if prefix == "" {
		entries, err := m.filesystem.ListDir(ctx, m.workingDir)
		if err != nil {
			return nil
		}

		for _, entry := range entries {
			baseName := filepath.Base(entry.Path)

			if m.isGitIgnored(entry.Path) {
				continue
			}

			// Skip hidden files for empty prefix
			if strings.HasPrefix(baseName, ".") {
				continue
			}

			if entry.IsDir {
				suggestions = append(suggestions, baseName+"/")
			} else {
				suggestions = append(suggestions, baseName)
			}

			if len(suggestions) >= maxResults {
				break
			}
		}
		return suggestions
	}

	// Recursive search helper
	var searchDir func(dir string, depth int)
	searchDir = func(dir string, depth int) {
		if depth > maxDepth || len(suggestions) >= maxResults {
			return
		}

		entries, err := m.filesystem.ListDir(ctx, dir)
		if err != nil {
			return
		}

		for _, entry := range entries {
			if len(suggestions) >= maxResults {
				return
			}

			baseName := filepath.Base(entry.Path)

			isHidden := strings.HasPrefix(baseName, ".")

			if m.isGitIgnored(entry.Path) {
				continue
			}

			// Skip hidden files unless prefix starts with .; still search hidden directories so their contents can match.
			if isHidden && !strings.HasPrefix(prefix, ".") {
				if entry.IsDir {
					searchDir(entry.Path, depth+1)
				}
				continue
			}

			// Check if basename matches prefix
			if strings.HasPrefix(baseName, prefix) {
				// Calculate relative path from working dir
				relPath, err := filepath.Rel(m.workingDir, entry.Path)
				if err != nil {
					relPath = entry.Path
				}

				if entry.IsDir {
					suggestions = append(suggestions, relPath+"/")
				} else {
					suggestions = append(suggestions, relPath)
				}
			}

			// Recurse into directories
			if entry.IsDir {
				searchDir(entry.Path, depth+1)
			}
		}
	}

	searchDir(m.workingDir, 0)
	return suggestions
}

// updateSuggestions updates the autocomplete suggestions based on current input
func (m *Model) updateSuggestions() {
	input := m.textarea.Value()
	cursorPos := len(input) // Textarea doesn't expose cursor position, use end of text

	// Check for filepath autocomplete (@ symbol)
	if _, partialPath, found := extractFilepathContext(input, cursorPos); found {
		m.suggestions = m.getFilepathSuggestions(partialPath)
		m.selectedSuggIndex = 0
		m.tabCycleIndex = 0
		return
	}

	// Check for command autocomplete (/ prefix or command mode)
	if m.commandMode || strings.HasPrefix(input, "/") {
		m.suggestions = getCommandSuggestions(input)
		m.selectedSuggIndex = 0
		m.tabCycleIndex = 0
		return
	}

	// No autocomplete applicable
	m.suggestions = nil
	m.selectedSuggIndex = 0
	m.tabCycleIndex = 0
}

// applySelectedSuggestion applies the currently selected autocomplete suggestion
func (m *Model) applySelectedSuggestion() {
	if len(m.suggestions) == 0 || m.selectedSuggIndex < 0 || m.selectedSuggIndex >= len(m.suggestions) {
		return
	}

	input := m.textarea.Value()
	selectedSugg := m.suggestions[m.selectedSuggIndex]

	var newValue string
	if atPos, _, found := extractFilepathContext(input, len(input)); found {
		prefix := input[:atPos]
		newValue = prefix + "@" + selectedSugg + " "
	} else {
		newValue = selectedSugg + " "
	}

	m.textarea.SetValue(newValue)
	m.suggestions = nil
	m.selectedSuggIndex = 0
	m.originalSuggestions = nil
	m.originalInput = ""
	m.tabCycleIndex = 0
}

// cycleSuggestion advances the autocomplete selection using Tab navigation.
// Positive direction cycles forward; negative direction cycles backward.
func (m *Model) cycleSuggestion(direction int) {
	if len(m.suggestions) == 0 {
		return
	}

	if direction == 0 {
		direction = 1
	} else if direction > 0 {
		direction = 1
	} else {
		direction = -1
	}

	input := m.textarea.Value()

	if len(m.originalSuggestions) == 0 || len(m.originalSuggestions) != len(m.suggestions) {
		m.originalSuggestions = make([]string, len(m.suggestions))
		copy(m.originalSuggestions, m.suggestions)
		m.originalInput = input
		m.tabCycleIndex = -1
	}

	if len(m.originalSuggestions) == 0 {
		return
	}

	if m.originalInput == "" {
		m.originalInput = input
	}

	if m.tabCycleIndex < 0 {
		if direction > 0 {
			m.tabCycleIndex = 0
		} else {
			m.tabCycleIndex = len(m.originalSuggestions) - 1
		}
	} else {
		m.tabCycleIndex = (m.tabCycleIndex + direction + len(m.originalSuggestions)) % len(m.originalSuggestions)
	}

	selectedSugg := m.originalSuggestions[m.tabCycleIndex]

	baseInput := m.originalInput
	if baseInput == "" {
		baseInput = input
	}

	var newValue string
	if atPos, _, found := extractFilepathContext(baseInput, len(baseInput)); found {
		prefix := baseInput[:atPos]
		newValue = prefix + "@" + selectedSugg
	} else {
		newValue = selectedSugg
	}

	m.textarea.SetValue(newValue)
	m.selectedSuggIndex = m.tabCycleIndex
}

// handleAuthorizationDialog handles messages when authorization dialog is open
func (m *Model) handleAuthorizationDialog(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			// Deny and close
			m.program.Send(AuthorizationResponseMsg{
				AuthID:   m.activeAuthorizationID,
				Approved: false,
			})
			return m, nil

		case "enter":
			// Check which option is selected
			if item, ok := m.authorizationDialog.list.SelectedItem().(authChoiceItem); ok {
				approved := item.value == "approve"
				m.program.Send(AuthorizationResponseMsg{
					AuthID:   m.activeAuthorizationID,
					Approved: approved,
				})
			}
			return m, nil

		case "y", "Y":
			// Quick approve with 'y'
			m.program.Send(AuthorizationResponseMsg{
				AuthID:   m.activeAuthorizationID,
				Approved: true,
			})
			return m, nil

		case "n", "N":
			// Quick deny with 'n'
			m.program.Send(AuthorizationResponseMsg{
				AuthID:   m.activeAuthorizationID,
				Approved: false,
			})
			return m, nil
		}

		// Update dialog list for navigation
		var cmd tea.Cmd
		m.authorizationDialog.list, cmd = m.authorizationDialog.list.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		// Update dialog size
		m.authorizationDialog.width = msg.Width
		m.authorizationDialog.height = msg.Height
		listWidth, listHeight := m.authorizationDialog.listSize()
		m.authorizationDialog.list.SetSize(listWidth, listHeight)
		return m, nil
	}

	return m, nil
}

// handleUserQuestionDialog handles messages when user question dialog is open
func (m *Model) handleUserQuestionDialog(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Check for EndUserQuestionsMsg
	if _, ok := msg.(EndUserQuestionsMsg); ok {
		// Collect answers based on dialog type
		var response string

		if dialog, ok := m.userQuestionDialog.(UserQuestionDialog); ok {
			// Multiple choice dialog
			answers := dialog.GetAnswers()
			var formattedAnswers strings.Builder
			for i, answer := range answers {
				if answer != "" {
					formattedAnswers.WriteString(fmt.Sprintf("Question %d: %s\n", i+1, answer))
				}
			}
			response = formattedAnswers.String()
		} else if dialog, ok := m.userQuestionDialog.(UserInputDialog); ok {
			// Single text input dialog
			response = dialog.GetAnswer()
		}

		// Send response
		if response != "" {
			select {
			case m.userQuestionResponse <- response:
			default:
			}
		}

		// Close dialog
		m.userQuestionDialogOpen = false
		m.userQuestionDialog = nil

		// Clear overlay and restore focus to textarea
		m.SetOverlayActive(false)
		m.textarea.Focus()

		return m, nil
	}

	// Update the dialog
	if m.userQuestionDialog != nil {
		m.userQuestionDialog, cmd = m.userQuestionDialog.Update(msg)
	}

	return m, cmd
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	// Handle authorization dialog if open
	if m.authorizationDialogOpen {
		return m.handleAuthorizationDialog(msg)
	}

	// Handle user question dialog if open
	if m.userQuestionDialogOpen {
		return m.handleUserQuestionDialog(msg)
	}

	wasReady := m.ready
	prevValue := m.textarea.Value()

	// Handle Alt+Enter and Ctrl+J before textarea processing to manually insert newline
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if shouldTreatAsMultilineEnter(keyMsg) {
			// Manually insert newline and return early
			currentValue := m.textarea.Value()
			m.textarea.SetValue(currentValue + "\n")
			return m, tea.Batch(tiCmd, vpCmd)
		}
	}

	shouldBlockTextarea := false
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if len(m.suggestions) > 0 {
			switch keyMsg.Type {
			case tea.KeyUp, tea.KeyDown, tea.KeyTab, tea.KeyShiftTab, tea.KeyEnter:
				shouldBlockTextarea = true
			}
		}
		// Block tab navigation keys from textarea
		keyStr := keyMsg.String()
		if len(keyStr) >= 5 && keyStr[:4] == "alt+" && keyStr[4] >= '1' && keyStr[4] <= '9' {
			shouldBlockTextarea = true
		}
		// Also block Ctrl+T (new tab) and Ctrl+W (close tab)
		if keyStr == "ctrl+t" || keyStr == "ctrl+w" {
			shouldBlockTextarea = true
		}
	}

	if !(m.overlayActive && isKeyMsg(msg)) && !shouldBlockTextarea {
		m.textarea, tiCmd = m.textarea.Update(msg)

		// Sanitize ANSI sequences
		currentValue := m.textarea.Value()
		if sanitized := sanitizePromptInput(currentValue, &m.sanitizeState); sanitized != currentValue {
			currentValue = sanitized
			m.textarea.SetValue(currentValue)
		}

		// Convert HTML to markdown if detected (on paste or large content changes)
		// Only check if there's a significant amount of new content (likely a paste)
		contentGrowth := len(currentValue) - len(prevValue)
		if contentGrowth > 100 || (len(currentValue) > 200 && contentGrowth > 50) {
			if converted, wasConverted := htmlconv.ConvertIfHTML(currentValue); wasConverted {
				m.textarea.SetValue(converted)
				// Show conversion message with size info
				m.AddSystemMessage(fmt.Sprintf("Converted HTML to markdown (%d → %d chars)", contentGrowth, len(converted)))
			}
		}

		valueChanged := m.textarea.Value() != prevValue
		if valueChanged && m.onPromptActivity != nil {
			m.onPromptActivity()
		}
		if _, ok := msg.(tea.KeyMsg); ok {
			m.originalSuggestions = nil
			m.originalInput = ""
			m.tabCycleIndex = 0
			m.updateSuggestions()
		}
	}
	m.viewport, vpCmd = m.viewport.Update(msg)

	// Update todo viewport if panel is shown
	var todoVpCmd tea.Cmd
	if m.showTodoPanel {
		m.todoViewport, todoVpCmd = m.todoViewport.Update(msg)
	}

	var spCmd tea.Cmd
	if !m.animationsDisabled {
		switch msg.(type) {
		case spinner.TickMsg:
			if m.spinnerActive {
				m.spinner, spCmd = m.spinner.Update(msg)
			}
		default:
			m.spinner, _ = m.spinner.Update(msg)
		}
	}

	baseCmd := tea.Batch(tiCmd, vpCmd, todoVpCmd, spCmd)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.overlayActive {
			return m, baseCmd
		}
		if cmdStr, ok := m.commandShortcutForKey(msg, prevValue); ok {
			m.commandMode = false
			m.resetInputState()
			return m, tea.Batch(baseCmd, m.handleCommand(cmdStr))
		}
		switch msg.String() {
		case "ctrl+c":
			now := time.Now()
			if now.Sub(m.lastCtrlCTime) < 500*time.Millisecond {
				return m, tea.Batch(baseCmd, tea.Quit)
			}
			m.lastCtrlCTime = now
			m.ctrlCCount++
			return m, baseCmd

		case "ctrl+d":
			if strings.TrimSpace(sanitizePromptInput(m.textarea.Value(), &m.sanitizeState)) == "" {
				return m, tea.Batch(baseCmd, tea.Quit)
			}
			return m, baseCmd

		case "esc":
			if m.isCurrentTabGenerating() {
				m.stopGeneration("Generation stopped.")
			} else if len(m.suggestions) > 0 {
				m.suggestions = nil
				m.selectedSuggIndex = 0
				m.originalSuggestions = nil
				m.originalInput = ""
				m.tabCycleIndex = 0
			}
			return m, baseCmd

		case "ctrl+x":
			m.commandMode = true
			m.textarea.Placeholder = commandModePlaceholder()
			m.updateSuggestions()
			return m, baseCmd

		case "ctrl+b":
			if m.onBackground != nil {
				if err := m.onBackground(); err != nil {
					m.AddSystemMessage(fmt.Sprintf("Unable to background shell job: %v", err))
				} else {
					m.AddSystemMessage("Requested to run current shell command in background.")
				}
			}
			return m, baseCmd

		case "ctrl+t":
			// Create new unnamed tab
			return m, m.handleNewTab("")

		case "ctrl+w":
			// Close current tab
			if len(m.sessions) > 1 {
				return m, m.handleCloseTab(m.activeSessionIdx)
			}
			return m, baseCmd

		case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5",
			"alt+6", "alt+7", "alt+8", "alt+9":
			// Switch to tab 1-9
			keyStr := msg.String()
			logger.Info("Alt key pressed: '%s', length: %d", keyStr, len(keyStr))

			// Parse the digit from the key string
			var digit rune
			for _, ch := range keyStr {
				if ch >= '0' && ch <= '9' {
					digit = ch
					break
				}
			}

			if digit == 0 {
				logger.Warn("Could not parse digit from key: %s", keyStr)
				return m, baseCmd
			}

			tabNum := int(digit-'0') - 1
			logger.Info("Switching to tab index %d (tab number %d)", tabNum, tabNum+1)

			if tabNum < len(m.sessions) {
				return m, m.handleSwitchTab(tabNum)
			}
			logger.Info("Tab index %d out of range (have %d sessions)", tabNum, len(m.sessions))
			return m, baseCmd

		case "tab":
			if len(m.suggestions) > 0 {
				m.cycleSuggestion(1)
				return m, baseCmd
			}
			return m, baseCmd
		case "shift+tab":
			if len(m.suggestions) > 0 {
				m.cycleSuggestion(-1)
				return m, baseCmd
			}
			return m, baseCmd

		case "up":
			if len(m.suggestions) > 0 {
				m.selectedSuggIndex--
				if m.selectedSuggIndex < 0 {
					m.selectedSuggIndex = len(m.suggestions) - 1
				}
				m.tabCycleIndex = m.selectedSuggIndex
				return m, baseCmd
			}
			// Handle todo scrolling when no suggestions
			if m.showTodoPanel && m.todoContentHeight > m.todoViewport.Height {
				m.todoViewport.ScrollUp(1)
				return m, baseCmd
			}

		case "down":
			if len(m.suggestions) > 0 {
				m.selectedSuggIndex++
				if m.selectedSuggIndex >= len(m.suggestions) {
					m.selectedSuggIndex = 0
				}
				m.tabCycleIndex = m.selectedSuggIndex
				return m, baseCmd
			}
			// Handle todo scrolling when no suggestions
			if m.showTodoPanel && m.todoContentHeight > m.todoViewport.Height {
				m.todoViewport.ScrollDown(1)
				return m, baseCmd
			}

		case "pgup":
			// Handle todo page up when todo panel is shown
			if m.showTodoPanel && m.todoContentHeight > m.todoViewport.Height {
				m.todoViewport.HalfPageUp()
				return m, baseCmd
			}

		case "pgdn":
			// Handle todo page down when todo panel is shown
			if m.showTodoPanel && m.todoContentHeight > m.todoViewport.Height {
				m.todoViewport.HalfPageDown()
				return m, baseCmd
			}

		case "home":
			// Handle todo scroll to top when todo panel is shown
			if m.showTodoPanel && m.todoContentHeight > m.todoViewport.Height {
				m.todoViewport.GotoTop()
				return m, baseCmd
			}

		case "end":
			// Handle todo scroll to bottom when todo panel is shown
			if m.showTodoPanel && m.todoContentHeight > m.todoViewport.Height {
				m.todoViewport.GotoBottom()
				return m, baseCmd
			}

		case "enter":
			// Process the prompt, but handle autocomplete first.
			rawInput := sanitizePromptInput(m.textarea.Value(), &m.sanitizeState)
			input := strings.TrimSpace(rawInput)

			// If we have autocomplete suggestions:
			// - file suggestions: Enter applies the suggestion
			// - command suggestions: Enter submits when already exact; otherwise apply suggestion
			if len(m.suggestions) > 0 {
				if (m.commandMode || strings.HasPrefix(input, "/")) && m.selectedSuggIndex >= 0 && m.selectedSuggIndex < len(m.suggestions) {
					selected := m.suggestions[m.selectedSuggIndex]
					if strings.EqualFold(input, selected) {
						// fall through and submit
					} else {
						m.applySelectedSuggestion()
						return m, baseCmd
					}
				} else {
					m.applySelectedSuggestion()
					return m, baseCmd
				}
			}

			// Otherwise, process the prompt normally
			if input == "" {
				return m, baseCmd
			}

			// Convert HTML to markdown if detected (fallback for non-paste scenarios)
			if converted, wasConverted := htmlconv.ConvertIfHTML(input); wasConverted {
				input = converted
				m.AddSystemMessage("Converted HTML to markdown")
			}

			m.resetInputState()

			isCommand := m.commandMode || strings.HasPrefix(input, "/")
			if isCommand {
				m.commandMode = false
				// In concurrent mode, commands can run anytime
				return m, tea.Batch(baseCmd, m.handleCommand(input))
			}

			// Check if current tab is generating
			if m.isCurrentTabGenerating() {
				// Queue the prompt for this tab
				m.queuePrompt(m.activeSessionIdx, input)
				return m, baseCmd
			}

			// Start prompt on active tab
			return m, tea.Batch(baseCmd, m.startPrompt(m.activeSessionIdx, input))
		}

		return m, baseCmd

	case tea.WindowSizeMsg:
		widthChanged, heightChanged, rendererCmd := m.applyWindowSize(msg.Width, msg.Height)
		if !widthChanged && heightChanged && m.viewport.AtBottom() {
			m.viewport.GotoBottom()
		}

		var extraCmds []tea.Cmd
		if rendererCmd != nil {
			extraCmds = append(extraCmds, rendererCmd)
		}

		if widthChanged || wasReady != m.ready {
			m.viewportDirty = true
			if cmd := m.scheduleViewportRefresh(); cmd != nil {
				extraCmds = append(extraCmds, cmd)
			}
		}

		if len(extraCmds) > 0 {
			return m, tea.Batch(append([]tea.Cmd{baseCmd}, extraCmds...)...)
		}
		return m, baseCmd

	case viewportRefreshMsg:
		if msg.token != m.viewportRefreshToken {
			return m, baseCmd
		}
		if m.viewportDirty {
			m.updateViewport()
		}
		return m, baseCmd

	case RendererReadyMsg:
		m.rendererInitMutex.Lock()
		if msg.Err == nil && msg.Renderer != nil {
			// Cache the new renderer
			m.rendererCache[msg.Width] = msg.Renderer

			// If this is the current width, update active renderer
			if msg.Width == m.renderWrapWidth || m.renderer == nil {
				m.renderer = msg.Renderer
				m.renderWrapWidth = msg.Width
				m.viewportDirty = true
			}
		}
		m.rendererInitInFlight = false
		m.rendererInitMutex.Unlock()

		// Refresh viewport if needed
		if m.viewportDirty {
			return m, tea.Batch(baseCmd, m.scheduleViewportRefresh())
		}
		return m, baseCmd

	// NOTE: Old GeneratingMsg and CompleteMsg handlers removed - replaced by tab-specific handlers below

	case ProcessingStatusMsg:
		m.processingStatus = msg.Status
		var extra tea.Cmd
		if !m.animationsDisabled {
			shouldSpin := msg.Status != ""
			if shouldSpin && !m.spinnerActive {
				m.spinnerActive = true
				extra = func() tea.Msg { return m.spinner.Tick() }
			} else if !shouldSpin && m.spinnerActive {
				m.spinnerActive = false
			}
		}
		return m, tea.Batch(baseCmd, extra)

	case ToolCallMsg:
		m.addToolCallMessage(msg.ToolName, msg.ToolID, msg.Parameters)
		return m, baseCmd

	case ToolResultMsg:
		m.addToolResultMessage(msg.ToolName, msg.ToolID, msg.Result, msg.Error)
		return m, baseCmd

	case ContextUsageMsg:
		m.contextFreePercent = msg.FreePercent
		m.contextWindow = msg.ContextWindow
		if m.activeSessionIdx >= 0 && m.activeSessionIdx < len(m.sessions) {
			m.sessions[m.activeSessionIdx].ContextFreePercent = msg.FreePercent
			m.sessions[m.activeSessionIdx].ContextWindow = msg.ContextWindow
		}
		return m, baseCmd

	case OpenRouterUsageMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx >= 0 {
			m.processOpenRouterUsage(tabIdx, msg.Usage)
		} else {
			logger.Warn("Received OpenRouterUsageMsg for unknown tab ID: %d", msg.TabID)
		}
		return m, baseCmd

	case ErrMsg:
		if errors.Is(error(msg), ErrQuitRequested) {
			return m, tea.Batch(baseCmd, tea.Quit)
		}
		m.err = msg
		m.errVisibleUntil = time.Now().Add(errorDisplayDuration)
		return m, baseCmd

	case NewTabMsg:
		return m, m.handleNewTab(msg.Name)

	case UserInputRequestMsg:
		cmd := m.handleUserInputRequest(msg)
		return m, cmd

	case UserMultipleQuestionsRequestMsg:
		cmd := m.handleUserMultipleQuestionsRequest(msg)
		return m, cmd

	// Tab-specific message handlers for concurrent generation
	case TabGeneratingMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx < 0 {
			logger.Warn("Received TabGeneratingMsg for unknown tab ID: %d", msg.TabID)
			return m, baseCmd
		}

		if msg.Content != "" {
			if tabIdx == m.activeSessionIdx {
				m.contentReceived = true
			}
			m.appendAssistantChunkForTab(tabIdx, msg.Content)
		}
		return m, baseCmd

	case TabProcessingStatusMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx < 0 {
			return m, baseCmd
		}

		// Only update status if this is the active tab
		if tabIdx == m.activeSessionIdx {
			m.processingStatus = msg.Status
			var extra tea.Cmd
			if !m.animationsDisabled {
				shouldSpin := msg.Status != ""
				if shouldSpin && !m.spinnerActive {
					m.spinnerActive = true
					extra = func() tea.Msg { return m.spinner.Tick() }
				} else if !shouldSpin && m.spinnerActive {
					m.spinnerActive = false
				}
			}
			return m, tea.Batch(baseCmd, extra)
		}
		return m, baseCmd

	case TabGenerationCompleteMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx < 0 {
			return m, baseCmd
		}

		// Mark tab as no longer generating
		m.setTabGenerating(tabIdx, false)
		m.sessions[tabIdx].ThinkingTokens = 0

		// Clear status if this was the active tab
		if tabIdx == m.activeSessionIdx {
			m.processingStatus = ""
			m.thinkingTokens = 0
			m.contentReceived = false
			if !m.animationsDisabled {
				m.spinnerActive = false
			}
			if msg.Error != nil {
				m.err = msg.Error
				m.errVisibleUntil = time.Now().Add(errorDisplayDuration)
			}
		}

		// Process queued prompts for this tab
		return m, m.processNextQueuedPromptForTab(tabIdx)

	case TabAuthorizationRequiredMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx >= 0 && tabIdx < len(m.sessions) {
			m.sessions[tabIdx].WaitingForAuth = true
			logger.Debug("Tab %d waiting for authorization", msg.TabID)
		}
		return m, baseCmd

	case ShowAuthorizationDialogMsg:
		// Get tab name for display
		tabIdx := m.findTabIndexByID(msg.Request.TabID)
		tabName := fmt.Sprintf("Tab %d", tabIdx+1)
		if tabIdx >= 0 && tabIdx < len(m.sessions) && m.sessions[tabIdx].Name != "" {
			tabName = m.sessions[tabIdx].Name
		}

		// Create authorization dialog BEFORE setting the flag
		authDialog := NewAuthorizationDialog(msg.Request, tabName)

		// Now atomically update the state
		m.authorizationMu.Lock()
		m.activeAuthorizationID = msg.Request.AuthID
		m.authorizationDialog = authDialog
		m.authorizationDialogOpen = true
		m.authorizationMu.Unlock()

		logger.Info("Showing authorization dialog for tab %d (authID: %s)", msg.Request.TabID, msg.Request.AuthID)

		return m, baseCmd

	case AuthorizationResponseMsg:
		// User responded to authorization
		m.authorizationMu.Lock()
		request, ok := m.pendingAuthorizations[msg.AuthID]
		if ok {
			// Send response to waiting callback
			select {
			case request.ResponseChan <- msg.Approved:
				logger.Info("Authorization response sent for authID %s: %v", msg.AuthID, msg.Approved)
			default:
				logger.Warn("Failed to send authorization response for authID %s (channel not ready)", msg.AuthID)
			}

			// Mark tab as no longer waiting for auth
			tabIdx := m.findTabIndexByID(request.TabID)
			if tabIdx >= 0 && tabIdx < len(m.sessions) {
				m.sessions[tabIdx].WaitingForAuth = false
			}
		}

		// Close authorization dialog
		m.authorizationDialogOpen = false
		m.activeAuthorizationID = ""
		m.authorizationMu.Unlock()

		return m, baseCmd

	case TabContextUsageMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx >= 0 {
			m.sessions[tabIdx].ContextFreePercent = msg.FreePercent
			m.sessions[tabIdx].ContextWindow = msg.ContextWindow
			if tabIdx == m.activeSessionIdx {
				m.contextFreePercent = msg.FreePercent
				m.contextWindow = msg.ContextWindow
			}
		} else {
			logger.Warn("Received TabContextUsageMsg for unknown tab ID: %d", msg.TabID)
		}
		return m, baseCmd

	case TabToolCallMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx >= 0 {
			m.addToolCallMessageForTab(tabIdx, msg.ToolName, msg.ToolID, msg.Parameters)
		}
		return m, baseCmd

	case TabToolResultMsg:
		tabIdx := m.findTabIndexByID(msg.TabID)
		if tabIdx >= 0 {
			m.addToolResultMessageForTab(tabIdx, msg.ToolName, msg.ToolID, msg.Result, msg.Error)
		}
		return m, baseCmd
	}

	return m, baseCmd
}

// CreateProgressCallbackForTab creates a progress callback that tags messages with tab ID (public method)
func (m *Model) CreateProgressCallbackForTab(tabID int) progress.Callback {
	return m.createProgressCallbackForTab(tabID)
}

// createProgressCallbackForTab creates a progress callback that tags messages with tab ID
func (m *Model) createProgressCallbackForTab(tabID int) progress.Callback {
	return func(update progress.Update) error {
		// Normalize the update
		normalized := progress.Normalize(update)

		// Handle status messages
		if normalized.ShouldStatus() {
			m.program.Send(TabProcessingStatusMsg{
				TabID:  tabID,
				Status: strings.TrimRight(normalized.Message, "\n"),
			})
		}

		// Handle streaming content
		if normalized.Message != "" && normalized.ShouldStream() {
			m.program.Send(TabGeneratingMsg{
				TabID:   tabID,
				Content: normalized.Message,
			})
		}

		return nil
	}
}

// Old implementation below - keeping for reference during transition
// startPromptForTab initiates generation for a specific tab using its own orchestrator
func (m *Model) startPromptForTab(tabIdx int, input string) tea.Cmd {
	if tabIdx < 0 || tabIdx >= len(m.sessions) {
		logger.Warn("startPromptForTab: invalid tab index %d", tabIdx)
		return nil
	}

	if m.factory == nil {
		logger.Error("RuntimeFactory not initialized")
		return nil
	}

	tab := m.sessions[tabIdx]

	// Get or create runtime for this tab
	runtime, ok := m.factory.GetTabRuntime(tab.ID)
	if !ok {
		var err error
		runtime, err = m.factory.CreateTabRuntime(tab.ID, tab.Session)
		if err != nil {
			logger.Error("Failed to create runtime for tab %d: %v", tab.ID, err)
			return func() tea.Msg {
				return TabGenerationCompleteMsg{
					TabID: tab.ID,
					Error: fmt.Errorf("failed to create runtime: %w", err),
				}
			}
		}
		tab.Runtime = runtime

		// Update context file if this is the active tab and it's the first runtime
		if tabIdx == m.activeSessionIdx {
			m.contextFile = runtime.Orchestrator.GetExtendedContextFile()
		}
	}

	if tabIdx == m.activeSessionIdx {
		m.updateTodoClientForTab(tabIdx)
	}

	// Mark tab as generating
	m.setTabGenerating(tabIdx, true)
	m.addMessageForTab(tabIdx, "You", input)

	// Reset usage/thinking state for this generation
	tab.OpenRouterUsage = nil
	tab.ThinkingTokens = 0
	if tabIdx == m.activeSessionIdx {
		m.openRouterUsage = nil
		m.thinkingTokens = 0
	}

	// Reset state for this generation if active tab
	if tabIdx == m.activeSessionIdx {
		m.processingStatus = ""
		m.contentReceived = false
		if !m.animationsDisabled {
			m.spinnerActive = true
		}
	}

	// Create per-tab progress callback that tags messages with tab ID
	progressCallback := m.createProgressCallbackForTab(tab.ID)

	// Create authorization callback for this tab
	authorizationCallback := func(toolName string, params map[string]interface{}, reason string) (bool, error) {
		logger.Debug("Tab %d: authorization requested for tool %s: %s", tab.ID, toolName, reason)

		// Generate unique authorization ID
		m.authorizationMu.Lock()
		m.authorizationCounter++
		authID := fmt.Sprintf("auth-%d-%d", tab.ID, m.authorizationCounter)
		m.authorizationMu.Unlock()

		// Create authorization request with response channel
		responseChan := make(chan bool, 1)
		request := &AuthorizationRequest{
			AuthID:       authID,
			TabID:        tab.ID,
			ToolName:     toolName,
			Parameters:   params,
			Reason:       reason,
			ResponseChan: responseChan,
		}

		// Store pending authorization
		m.authorizationMu.Lock()
		m.pendingAuthorizations[authID] = request
		m.authorizationMu.Unlock()

		// Mark tab as waiting for authorization
		m.program.Send(TabAuthorizationRequiredMsg{
			TabID:  tab.ID,
			Reason: reason,
		})

		// Send message to display authorization dialog
		m.program.Send(ShowAuthorizationDialogMsg{
			Request: request,
		})

		// Block until user responds
		logger.Debug("Tab %d: waiting for authorization response (authID: %s)", tab.ID, authID)
		approved := <-responseChan
		logger.Debug("Tab %d: authorization response received: %v (authID: %s)", tab.ID, approved, authID)

		// Cleanup
		m.authorizationMu.Lock()
		delete(m.pendingAuthorizations, authID)
		m.authorizationMu.Unlock()

		return approved, nil
	}

	// Create tool call callback for this tab
	toolCallCallback := func(toolName, toolID string, parameters map[string]interface{}) error {
		logger.Debug("Tab %d: tool call %s (ID: %s)", tab.ID, toolName, toolID)
		m.program.Send(TabToolCallMsg{
			TabID:      tab.ID,
			ToolName:   toolName,
			ToolID:     toolID,
			Parameters: parameters,
		})
		return nil
	}

	// Create tool result callback for this tab
	toolResultCallback := func(toolName, toolID, result, errorMsg string) error {
		logger.Debug("Tab %d: tool result %s (ID: %s)", tab.ID, toolName, toolID)
		m.program.Send(TabToolResultMsg{
			TabID:    tab.ID,
			ToolName: toolName,
			ToolID:   toolID,
			Result:   result,
			Error:    errorMsg,
		})
		return nil
	}

	// Create context usage callback for this tab
	contextCallback := func(freePercent int, contextWindow int) error {
		m.program.Send(TabContextUsageMsg{
			TabID:         tab.ID,
			FreePercent:   freePercent,
			ContextWindow: contextWindow,
		})
		return nil
	}

	// Create usage callback for this tab
	usageCallback := func(usage map[string]interface{}) error {
		logger.Debug("Tab %d: usage callback received: %v", tab.ID, usage)
		m.program.Send(OpenRouterUsageMsg{
			TabID: tab.ID,
			Usage: usage,
		})
		return nil
	}

	// Wire planning user-input callback so the planning agent can ask clarifying questions
	runtime.Orchestrator.SetUserInputCallback(func(question string) (string, error) {
		responseChan := make(chan string, 1)
		errChan := make(chan error, 1)

		// Choose dialog based on the question format
		if isLikelyMultipleChoicePrompt(question) {
			m.program.Send(UserMultipleQuestionsRequestMsg{
				Questions: question,
				Response:  responseChan,
				Error:     errChan,
			})
		} else {
			m.program.Send(UserInputRequestMsg{
				Question: question,
				Response: responseChan,
				Error:    errChan,
			})
		}

		return m.awaitUserResponse(runtime.ctx, responseChan, errChan)
	})

	// Submit to tab's orchestrator in goroutine
	return func() tea.Msg {
		go func() {
			ctx := runtime.ctx
			err := runtime.Orchestrator.ProcessPrompt(
				ctx,
				input,
				progressCallback,
				contextCallback,       // context usage callback (tab-aware)
				authorizationCallback, // authorization callback
				toolCallCallback,      // tool call callback
				toolResultCallback,    // tool result callback
				usageCallback,         // usage callback
			)

			if err != nil {
				m.program.Send(TabGenerationCompleteMsg{
					TabID: tab.ID,
					Error: err,
				})
			} else {
				m.program.Send(TabGenerationCompleteMsg{
					TabID: tab.ID,
				})
			}
		}()
		return nil
	}
}

func (m *Model) startPrompt(tabIdx int, input string) tea.Cmd {
	// In concurrent mode, directly start the prompt for the specified tab
	return m.startPromptForTab(tabIdx, input)
}

func (m *Model) queuePrompt(tabIdx int, input string) {
	m.queuedPrompts[tabIdx] = append(m.queuedPrompts[tabIdx], input)
	preview := formatQueuedPreview(input)
	m.addMessageForTab(tabIdx, "System", fmt.Sprintf("Queued prompt #%d: %s", len(m.queuedPrompts[tabIdx]), preview))
}

// processNextQueuedPromptForTab processes the next queued prompt for a specific tab (tab-aware version)
func (m *Model) processNextQueuedPromptForTab(tabIdx int) tea.Cmd {
	if tabIdx < 0 || tabIdx >= len(m.sessions) {
		return nil
	}

	queue := m.queuedPrompts[tabIdx]
	if len(queue) == 0 {
		return nil
	}

	// Dequeue the next prompt
	next := queue[0]
	m.queuedPrompts[tabIdx] = queue[1:]

	preview := formatQueuedPreview(next)
	remaining := len(m.queuedPrompts[tabIdx])
	if remaining > 0 {
		m.addMessageForTab(tabIdx, "System", fmt.Sprintf("Processing queued prompt: %s (%d remaining)", preview, remaining))
	} else {
		m.addMessageForTab(tabIdx, "System", fmt.Sprintf("Processing queued prompt: %s", preview))
	}

	// Start prompt for this specific tab
	return m.startPromptForTab(tabIdx, next)
}

// processOpenRouterUsage processes OpenRouter usage data and stores it for display
func (m *Model) processOpenRouterUsage(tabIdx int, usage map[string]interface{}) {
	if !m.validTabIndex(tabIdx) {
		logger.Warn("processOpenRouterUsage called with invalid tab index: %d", tabIdx)
		return
	}

	logger.Debug("OpenRouter usage data received for tab %d: %v", tabIdx, usage)
	m.sessions[tabIdx].OpenRouterUsage = usage

	thinkingTokens := extractThinkingTokens(usage)
	m.sessions[tabIdx].ThinkingTokens = thinkingTokens

	if tabIdx == m.activeSessionIdx {
		m.openRouterUsage = usage
		m.thinkingTokens = thinkingTokens
		if thinkingTokens > 0 {
			logger.Debug("Extracted thinking tokens for active tab: %d", thinkingTokens)
		}
		// Refresh todo content to show updated usage data
		if m.showTodoPanel {
			m.refreshTodoContent()
		}
	}
}

// extractThinkingTokens extracts thinking/reasoning tokens from usage data
// Supports multiple provider formats (OpenAI, Anthropic, etc.)
func extractThinkingTokens(usage map[string]interface{}) int {
	if usage == nil {
		return 0
	}

	// OpenAI format: completion_tokens_details.reasoning_tokens
	if details, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
		if reasoningTokens, ok := details["reasoning_tokens"].(float64); ok {
			return int(reasoningTokens)
		}
	}

	// Alternative format: reasoning_tokens at top level
	if reasoningTokens, ok := usage["reasoning_tokens"].(float64); ok {
		return int(reasoningTokens)
	}

	// Anthropic extended thinking format (if available)
	if thinkingTokens, ok := usage["thinking_tokens"].(float64); ok {
		return int(thinkingTokens)
	}

	return 0
}

func formatQueuedPreview(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "(empty)"
	}
	const limit = 80
	if utf8.RuneCountInString(trimmed) <= limit {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	return string(runes[:limit-3]) + "..."
}

func isKeyMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyMsg)
	return ok
}

func (m *Model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	var sb strings.Builder

	// Title
	title := titleStyle.Render("scriptschnell - AI-Powered Coding Assistant")
	status := statusStyle.Render(fmt.Sprintf("Model: %s", m.currentModel))

	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(status)
	sb.WriteString("\n")

	// Tab bar (if multiple sessions)
	tabBar := m.renderTabBar()
	if tabBar != "" {
		sb.WriteString(tabBar)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Viewport with messages
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n\n")

	// Textarea
	sb.WriteString(m.textarea.View())
	sb.WriteString("\n")

	// Context usage indicator below the prompt
	sb.WriteString(m.renderContextUsage())
	sb.WriteString("\n")

	// Autocomplete suggestions
	if len(m.suggestions) > 0 {
		sb.WriteString(m.renderSuggestions())
		sb.WriteString("\n")
	}

	footerLeft := ""
	if m.processingStatus != "" {
		statusText := m.processingStatus
		if m.thinkingTokens > 0 {
			statusText = fmt.Sprintf("%s (%d tokens)", statusText, m.thinkingTokens)
		}
		if !m.animationsDisabled && m.spinnerActive {
			footerLeft = statusStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), statusText))
		} else {
			footerLeft = statusStyle.Render(fmt.Sprintf("⚙️  %s", statusText))
		}
	} else if m.isCurrentTabGenerating() {
		var generatingText string

		// Show thinking tokens only if we haven't received content yet (still in thinking phase)
		if m.thinkingTokens > 0 && !m.contentReceived {
			generatingText = fmt.Sprintf("Thinking... (%d thinking tokens)", m.thinkingTokens)
		} else {
			generatingText = "Generating..."
		}

		if !m.animationsDisabled && m.spinnerActive {
			footerLeft = statusStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), generatingText))
		} else {
			footerLeft = statusStyle.Render(fmt.Sprintf("⏳ %s", generatingText))
		}
	} else if m.err != nil {
		if m.errVisibleUntil.IsZero() || time.Now().Before(m.errVisibleUntil) {
			footerLeft = errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
		}
	}

	footerRight := contextStyle.Render(m.contextDisplay())

	sb.WriteString(m.renderFooter(footerLeft, footerRight))

	if m.err != nil && !m.errVisibleUntil.IsZero() && time.Now().After(m.errVisibleUntil) {
		m.err = nil
		m.errVisibleUntil = time.Time{}
	}

	mainContent := sb.String()

	// Show authorization dialog as overlay if open
	if m.authorizationDialogOpen {
		dialogView := m.authorizationDialog.View()
		// Center the dialog
		overlay := lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			dialogView,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
		)
		return overlay
	}

	// Show user question dialog as overlay if open
	if m.userQuestionDialogOpen && m.userQuestionDialog != nil {
		dialogView := m.userQuestionDialog.View()
		// Center the dialog
		overlay := lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			dialogView,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
		)
		return overlay
	}

	if !m.showTodoPanel {
		return mainContent
	}

	todoPanel := m.renderTodoPanel()
	if todoPanel == "" {
		return mainContent
	}

	spacing := lipgloss.NewStyle().Width(todoPanelSpacing).Render("")
	return lipgloss.JoinHorizontal(lipgloss.Top, mainContent, spacing, todoPanel)
}

func (m *Model) renderFooter(left, right string) string {
	width := m.contentWidth
	if width <= 0 {
		switch {
		case left == "":
			return right
		case right == "":
			return left
		default:
			return left + " " + right
		}
	}

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	space := width - leftWidth - rightWidth
	if space < 1 {
		space = 1
	}

	return left + strings.Repeat(" ", space) + right
}

func (m *Model) contextDisplay() string {
	var parts []string

	// Get current session's accumulated usage
	var totalCost float64
	var totalTokens int
	var cachedTokens int

	if m.activeSessionIdx >= 0 && m.activeSessionIdx < len(m.sessions) {
		sess := m.sessions[m.activeSessionIdx].Session
		if sess != nil {
			totalCost = sess.GetTotalCost()
			totalTokens = sess.GetTotalTokens()
			cachedTokens = sess.TotalCachedTokens + sess.TotalCacheReadTokens
		}
	}

	// Determine caching state
	cacheActive := cachedTokens > 0

	// Add context file info
	if strings.TrimSpace(m.contextFile) == "" {
		parts = append(parts, "Context: (none)")
	} else {
		contextLabel := fmt.Sprintf("Context: %s", m.contextFile)
		if cacheActive {
			contextLabel += " (cache on)"
		}
		parts = append(parts, contextLabel)
	}

	// Add accumulated usage from session
	if totalCost > 0 {
		parts = append(parts, fmt.Sprintf("Cost: $%.6f", totalCost))
	}

	if cachedTokens > 0 && totalTokens > 0 {
		cachePercent := float64(cachedTokens) / float64(totalTokens) * 100
		parts = append(parts, fmt.Sprintf("Cached: %.0f%%", cachePercent))
	}

	return strings.Join(parts, " | ")
}

// formatOpenRouterUsage formats OpenRouter usage data for display
func (m *Model) formatOpenRouterUsage() string {
	// Get current session's accumulated usage
	var totalCost float64
	var totalTokens int
	var promptTokens int
	var completionTokens int
	var cachedTokens int

	if m.activeSessionIdx >= 0 && m.activeSessionIdx < len(m.sessions) {
		sess := m.sessions[m.activeSessionIdx].Session
		if sess != nil {
			totalCost = sess.GetTotalCost()
			totalTokens = sess.GetTotalTokens()
			promptTokens = sess.TotalPromptTokens
			completionTokens = sess.TotalCompletionTokens
			cachedTokens = sess.TotalCachedTokens + sess.TotalCacheReadTokens
		}
	}

	if totalTokens == 0 && totalCost == 0 {
		return ""
	}

	var parts []string

	// Add token usage
	if promptTokens > 0 || completionTokens > 0 {
		if totalTokens > 0 {
			parts = append(parts, fmt.Sprintf("Tokens: %d", totalTokens))
		}
	}

	// Add caching information if available
	if cachedTokens > 0 && totalTokens > 0 {
		cachePercent := float64(cachedTokens) / float64(totalTokens) * 100
		parts = append(parts, fmt.Sprintf("Cached: %.0f%%", cachePercent))
	}

	// Add cost
	if totalCost > 0 {
		parts = append(parts, fmt.Sprintf("Cost: $%.6f", totalCost))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, " | ")
}

func (m *Model) renderContextUsage() string {
	percent := m.contextFreePercent
	if percent < 0 {
		return contextUsageStyle.Render("Free context: unknown")
	}
	if percent > 100 {
		percent = 100
	}

	// Format context window size in K tokens
	if m.contextWindow > 0 {
		contextWindowK := m.contextWindow / 1000
		return contextUsageStyle.Render(fmt.Sprintf("Free context: %d%% (%dK)", percent, contextWindowK))
	}

	return contextUsageStyle.Render(fmt.Sprintf("Free context: %d%%", percent))
}

func (m *Model) renderSuggestions() string {
	if len(m.suggestions) == 0 {
		return ""
	}

	var sb strings.Builder

	// Suggestion box style
	suggestionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		MarginLeft(2)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("170")).
		Background(lipgloss.Color("238")).
		Bold(true).
		MarginLeft(2)

	sb.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginLeft(2).
		Render("Suggestions (↑↓ to navigate, Tab/Shift+Tab to select, ESC to dismiss):"))
	sb.WriteString("\n")

	// Show max 5 suggestions at a time
	maxDisplay := 5
	start := 0
	if len(m.suggestions) > maxDisplay {
		// Center the selected item if possible
		start = m.selectedSuggIndex - maxDisplay/2
		if start < 0 {
			start = 0
		}
		if start+maxDisplay > len(m.suggestions) {
			start = len(m.suggestions) - maxDisplay
		}
	}

	end := start + maxDisplay
	if end > len(m.suggestions) {
		end = len(m.suggestions)
	}

	for i := start; i < end; i++ {
		suggestion := m.suggestions[i]
		if i == m.selectedSuggIndex {
			sb.WriteString(selectedStyle.Render(fmt.Sprintf("▶ %s", suggestion)))
		} else {
			sb.WriteString(suggestionStyle.Render(fmt.Sprintf("  %s", suggestion)))
		}
		sb.WriteString("\n")
	}

	// Show indicator if there are more suggestions
	if len(m.suggestions) > maxDisplay {
		more := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginLeft(2).
			Render(fmt.Sprintf("  ... (%d/%d)", m.selectedSuggIndex+1, len(m.suggestions)))
		sb.WriteString(more)
	}

	return sb.String()
}

// refreshTodoContent updates the todo content for the viewport
func (m *Model) refreshTodoContent() {
	var todoList *tools.TodoList
	var err error
	if m.todoClient != nil {
		todoList, err = m.todoClient.List()
	}

	var content strings.Builder

	// Add keyboard shortcuts at the top of the todo panel so they're always visible
	content.WriteString(todoTitleStyle.Render("Keybinds"))
	content.WriteString("\n")

	keybinds := []string{
		"Ctrl+X: Commands",
		"Ctrl+B: Background",
		"ESC: Stop",
		"Ctrl+C×2/Ctrl+D: Quit",
	}

	// Add tab shortcuts only if multiple tabs exist
	if len(m.sessions) > 1 {
		keybinds = append(keybinds, "Ctrl+T: New Tab")
		keybinds = append(keybinds, "Ctrl+W: Close Tab")
		keybinds = append(keybinds, "Alt+1-9: Switch Tab")
	}

	for _, keybind := range keybinds {
		content.WriteString(todoItemStyle.Render(fmt.Sprintf("• %s", keybind)))
		content.WriteString("\n")
	}

	content.WriteString("\n")
	content.WriteString(todoTitleStyle.Render("Todo Tasks"))
	content.WriteString("\n")

	switch {
	case m.todoClient == nil:
		content.WriteString(todoEmptyStyle.Render("Todo list not available yet."))
	case err != nil:
		content.WriteString(todoErrorStyle.Render(fmt.Sprintf("Error: %v", err)))
	case todoList == nil || len(todoList.Items) == 0:
		content.WriteString(todoEmptyStyle.Render("No todo items yet."))
	default:
		m.renderTodoTree(&content, todoList.Items, "", 0)
	}

	content.WriteString("\n")
	content.WriteString(todoTitleStyle.Render("MCP Servers"))
	content.WriteString("\n")

	var names []string
	if m.activeMCPProvider != nil {
		names = m.activeMCPProvider()
	}

	if len(names) == 0 {
		content.WriteString(todoEmptyStyle.Render("No MCP servers selected."))
		content.WriteString("\n")
	} else {
		sort.Strings(names)
		for _, name := range names {
			content.WriteString(todoItemStyle.Render(fmt.Sprintf("• %s", name)))
			content.WriteString("\n")
		}
	}

	// Add usage data from session
	if m.activeSessionIdx >= 0 && m.activeSessionIdx < len(m.sessions) {
		sess := m.sessions[m.activeSessionIdx].Session
		if sess != nil && (sess.GetTotalCost() > 0 || sess.GetTotalTokens() > 0) {
			content.WriteString("\n")
			content.WriteString(todoTitleStyle.Render("Usage Statistics"))
			content.WriteString("\n")

			usageInfo := m.formatOpenRouterUsage()
			if usageInfo != "" {
				content.WriteString(todoItemStyle.Render(usageInfo))
			} else {
				content.WriteString(todoEmptyStyle.Render("No usage data available."))
			}
		}
	}

	// Update the viewport content
	m.todoContent = content.String()
	m.todoContentHeight = strings.Count(m.todoContent, "\n") + 1
	m.todoViewport.SetContent(m.todoContent)
}

func (m *Model) renderTodoPanel() string {
	// Always refresh todo content to ensure it's up to date
	m.refreshTodoContent()

	// If viewport is not initialized or content fits, show content directly
	if m.todoViewport.Height == 0 || m.todoContentHeight <= m.todoViewport.Height {
		return todoPanelStyle.Render(strings.TrimRight(m.todoContent, "\n"))
	}

	// Use viewport for scrollable content
	viewportContent := m.todoViewport.View()

	// Add scroll indicators
	var scrollIndicator string
	if m.todoViewport.AtTop() && m.todoViewport.AtBottom() {
		scrollIndicator = ""
	} else if m.todoViewport.AtTop() {
		scrollIndicator = "\n⬇"
	} else if m.todoViewport.AtBottom() {
		scrollIndicator = "⬆\n"
	} else {
		scrollIndicator = "⬆\n⬇"
	}

	content := viewportContent
	if scrollIndicator != "" {
		content += scrollIndicator
	}

	return todoPanelStyle.Render(strings.TrimRight(content, "\n"))
}

// renderTodoTree renders todos hierarchically
func (m *Model) renderTodoTree(content *strings.Builder, items []*tools.TodoItem, parentID string, depth int) {
	// Find all items with the specified parent
	for _, item := range items {
		if item.ParentID != parentID {
			continue
		}

		var marker string
		itemStyle := todoItemStyle

		// Determine style and marker based on status
		switch item.Status {
		case "completed":
			marker = "✓"
			itemStyle = todoCompletedStyle
		case "in_progress":
			marker = "▶"
			itemStyle = todoInProgressStyle
		case "pending":
			marker = "○"
			itemStyle = todoItemStyle
		default:
			// Fallback to old Completed field for backward compatibility
			if item.Completed {
				marker = "✓"
				itemStyle = todoCompletedStyle
			} else {
				marker = "○"
			}
		}

		// Add indentation for nested items
		indent := strings.Repeat("  ", depth)
		prefix := ""
		if depth > 0 {
			prefix = "└ "
		}

		content.WriteString(itemStyle.Render(fmt.Sprintf("%s%s[%s] %s", indent, prefix, marker, item.Text)))
		content.WriteString("\n")

		// Recursively render children
		m.renderTodoTree(content, items, item.ID, depth+1)
	}
}

// updateTodoClientForTab syncs the todo client with the active tab's runtime
func (m *Model) updateTodoClientForTab(tabIdx int) {
	if !m.validTabIndex(tabIdx) {
		return
	}

	runtime := m.sessions[tabIdx].Runtime
	if runtime != nil && runtime.Orchestrator != nil {
		m.todoClient = runtime.Orchestrator.GetTodoClient()
	} else if tabIdx == m.activeSessionIdx {
		m.todoClient = nil
	}

	// Refresh todo content when tab changes
	if tabIdx == m.activeSessionIdx && m.showTodoPanel {
		m.refreshTodoContent()
	}
}

func (m *Model) validTabIndex(tabIdx int) bool {
	return tabIdx >= 0 && tabIdx < len(m.sessions)
}

func (m *Model) storeMessagesForTab(tabIdx int, msgs []message, updateViewport bool) {
	if !m.validTabIndex(tabIdx) {
		return
	}

	m.sessions[tabIdx].Messages = msgs
	if tabIdx == m.activeSessionIdx {
		m.messages = msgs
		if updateViewport {
			m.updateViewport()
		}
	}
}

func (m *Model) targetGenerationTab() int {
	// In concurrent mode, just return active tab (legacy function for compatibility)
	if m.validTabIndex(m.activeSessionIdx) {
		return m.activeSessionIdx
	}
	return -1
}

// applyPendingSessionSwitch - REMOVED: Obsolete in concurrent generation system

func (m *Model) addMessageForTab(tabIdx int, role, content string) {
	timestamp := time.Now().Format("15:04:05")

	if !m.validTabIndex(tabIdx) {
		m.messages = append(m.messages, message{
			role:      role,
			content:   content,
			timestamp: timestamp,
		})
		m.updateViewport()
		return
	}

	msgs := append(m.sessions[tabIdx].Messages, message{
		role:      role,
		content:   content,
		timestamp: timestamp,
	})
	m.storeMessagesForTab(tabIdx, msgs, true)
}

func (m *Model) addMessage(role, content string) {
	m.addMessageForTab(m.activeSessionIdx, role, content)
}

func (m *Model) appendToLastMessageForTab(tabIdx int, content string) {
	if !m.validTabIndex(tabIdx) {
		return
	}

	msgs := m.sessions[tabIdx].Messages
	if len(msgs) == 0 {
		m.addMessageForTab(tabIdx, "Assistant", content)
		return
	}

	// Check if the last message is an unsimplified tool result and summarize it
	lastMsg := &msgs[len(msgs)-1]
	if lastMsg.role == "Tool" && !lastMsg.summarized && lastMsg.fullResult != "" {
		// Replace with summary
		summary := m.generateEnhancedToolSummary(lastMsg.toolName, lastMsg.fullResult, false)
		lastMsg.content = summary
		lastMsg.summarized = true
	}

	msgs[len(msgs)-1].content += content
	m.storeMessagesForTab(tabIdx, msgs, tabIdx == m.activeSessionIdx)
}

func (m *Model) summarizeLastToolResultForTab(tabIdx int) {
	if !m.validTabIndex(tabIdx) {
		return
	}

	msgs := m.sessions[tabIdx].Messages
	if len(msgs) == 0 {
		return
	}

	lastMsg := &msgs[len(msgs)-1]
	if lastMsg.role == "Tool" && !lastMsg.summarized && lastMsg.fullResult != "" {
		summary := m.generateEnhancedToolSummary(lastMsg.toolName, lastMsg.fullResult, false)
		lastMsg.content = summary
		lastMsg.summarized = true
		m.storeMessagesForTab(tabIdx, msgs, tabIdx == m.activeSessionIdx)
	}
}

func (m *Model) appendAssistantChunkForTab(tabIdx int, content string) {
	if !m.validTabIndex(tabIdx) || content == "" {
		return
	}

	msgs := m.sessions[tabIdx].Messages
	if len(msgs) == 0 || msgs[len(msgs)-1].role != "Assistant" {
		m.summarizeLastToolResultForTab(tabIdx)
		msgs = m.sessions[tabIdx].Messages
		if len(msgs) == 0 || msgs[len(msgs)-1].role != "Assistant" {
			m.addMessageForTab(tabIdx, "Assistant", "")
		}
	}

	m.appendToLastMessageForTab(tabIdx, content)
}

func (m *Model) addToolCallMessage(toolName, toolID string, parameters map[string]interface{}) {
	m.addToolCallMessageForTab(m.targetGenerationTab(), toolName, toolID, parameters)
}

func (m *Model) addToolCallMessageForTab(tabIdx int, toolName, toolID string, parameters map[string]interface{}) {
	isPlanning := strings.HasPrefix(toolName, "Planning: ")
	realToolName := toolName
	if isPlanning {
		realToolName = strings.TrimPrefix(toolName, "Planning: ")
	}

	// Create more descriptive content based on tool type
	var content string
	switch realToolName {
	case tools.ToolNameReadFile:
		if path, ok := parameters["path"].(string); ok {
			content = fmt.Sprintf("📖 **Reading file:** `%s`", path)
		} else {
			content = "📖 **Reading file**"
		}
	case tools.ToolNameCreateFile:
		if path, ok := parameters["path"].(string); ok {
			content = fmt.Sprintf("📝 **Creating file:** `%s`", path)
		} else {
			content = "📝 **Creating file**"
		}
	case tools.ToolNameEditFile:
		if path, ok := parameters["path"].(string); ok {
			content = fmt.Sprintf("✏️  **Updating file:** `%s`", path)
		} else {
			content = "✏️  **Updating file**"
		}
	case tools.ToolNameShell:
		if command, ok := parameters["command"].(string); ok {
			content = fmt.Sprintf("💻 **Executing command:** `%s`", command)
		} else {
			content = "💻 **Executing command**"
		}
	case tools.ToolNameGoSandbox:
		content = "🔧 **Running Go sandbox**"
		if code, ok := parameters["code"].(string); ok {
			// Show the code in a code block
			content = "🔧 **Running Go sandbox:**\n```go\n" + code + "\n```"
		}
		// Add timeout info if specified
		if timeout, ok := parameters["timeout"]; ok {
			if timeoutInt, ok := timeout.(float64); ok && timeoutInt != 30 {
				content += fmt.Sprintf("\n**Timeout:** %d seconds", int(timeoutInt))
			}
		}
		// Add libraries info if specified
		if libs, ok := parameters["libraries"].([]interface{}); ok && len(libs) > 0 {
			libStrs := make([]string, len(libs))
			for i, lib := range libs {
				if libStr, ok := lib.(string); ok {
					libStrs[i] = libStr
				}
			}
			if len(libStrs) > 0 {
				content += fmt.Sprintf("\n**Libraries:** %s", strings.Join(libStrs, ", "))
			}
		}
	default:
		content = fmt.Sprintf("🔧 **Calling tool:** `%s`", realToolName)
	}

	if isPlanning {
		content = "📋 **Planning:** " + content
	}

	m.addMessageForTab(tabIdx, "Tool", content)
}

func (m *Model) addToolResultMessage(toolName, toolID, result, errorMsg string) {
	m.addToolResultMessageForTab(m.targetGenerationTab(), toolName, toolID, result, errorMsg)
}

func (m *Model) addToolResultMessageForTab(tabIdx int, toolName, toolID, result, errorMsg string) {
	isPlanning := strings.HasPrefix(toolName, "Planning: ")
	realToolName := toolName
	if isPlanning {
		realToolName = strings.TrimPrefix(toolName, "Planning: ")
	}

	var content string
	if errorMsg != "" {
		content = fmt.Sprintf("❌ **Error:** %s", errorMsg)
		if isPlanning {
			content = "📋 **Planning Error:** " + errorMsg
		}
		m.addToolMessageForTab(tabIdx, realToolName, toolID, content, "", false)
	} else if result == "" {
		content = m.generateToolSummary(realToolName, result, true)
		if isPlanning {
			content = "📋 **Planning:** " + content
		}
		m.addToolMessageForTab(tabIdx, realToolName, toolID, content, "", true)
	} else {
		// Check if this is a pre-formatted UIResult (already has markdown formatting)
		if isAlreadyFormatted(result) {
			// Tool already formatted UIResult - use as-is
			if isPlanning {
				content = "📋 **Planning:** " + result
			} else {
				content = result
			}
			m.addToolMessageForTab(tabIdx, realToolName, toolID, content, result, false)
		} else if strings.HasPrefix(result, "---") || strings.HasPrefix(result, "diff --git") {
			// Git diff format - wrap in diff code block
			summary := m.generateToolSummary(realToolName, result, false)
			if isPlanning {
				summary = "📋 **Planning:** " + summary
			}
			displayResult := m.truncateToolResult(result)
			fullContent := fmt.Sprintf("%s\n```diff\n%s\n```", summary, displayResult)
			m.addToolMessageForTab(tabIdx, realToolName, toolID, fullContent, result, false)
		} else {
			// Raw output - wrap in code block
			displayResult := m.truncateToolResult(result)
			fullContent := fmt.Sprintf("✓ **Result:**\n```\n%s\n```", displayResult)
			if isPlanning {
				fullContent = "📋 **Planning Result:**\n```\n" + displayResult + "\n```"
			}
			m.addToolMessageForTab(tabIdx, realToolName, toolID, fullContent, result, false)
		}
	}
}

// isAlreadyFormatted checks if a tool result is already formatted with markdown
func isAlreadyFormatted(result string) bool {
	// Check for markdown code blocks with language tags and tool-specific formatting
	// Tools like read_file include 📖 and 📊 emoji indicators along with code blocks
	return strings.Contains(result, "```") &&
		(strings.Contains(result, "📖") || strings.Contains(result, "📊"))
}

// generateToolSummary creates a simple one-line summary for tool results
func (m *Model) generateToolSummary(toolName, result string, noOutput bool) string {
	if noOutput {
		return "✓ **Executed successfully**"
	}

	switch toolName {
	case tools.ToolNameReadFile:
		// Count lines in the result
		lines := strings.Count(result, "\n") + 1
		return fmt.Sprintf("✓ **Read %d lines**", lines)

	case tools.ToolNameCreateFile:
		// Count lines in the created content
		lines := strings.Count(result, "\n") + 1
		if lines == 1 {
			return "✓ **Created file**"
		}
		return fmt.Sprintf("✓ **Created file with %d lines**", lines)

	case tools.ToolNameEditFile:
		// Count additions and deletions in the diff
		additions := strings.Count(result, "\n+")
		deletions := strings.Count(result, "\n-")
		if additions > 0 && deletions > 0 {
			return fmt.Sprintf("✓ **Changed: +%d/-%d lines**", additions, deletions)
		} else if additions > 0 {
			return fmt.Sprintf("✓ **Added %d lines**", additions)
		} else if deletions > 0 {
			return fmt.Sprintf("✓ **Deleted %d lines**", deletions)
		} else {
			return "✓ **File updated**"
		}

	case tools.ToolNameShell:
		// For shell commands, just indicate successful execution
		return "✓ **Executed successfully**"

	case tools.ToolNameGoSandbox:
		return "✓ **Code executed successfully**"

	default:
		return "✓ **Completed successfully**"
	}
}

// generateEnhancedToolSummary creates detailed summaries using execution metadata when available
func (m *Model) generateEnhancedToolSummary(toolName, result string, noOutput bool) string {
	if noOutput {
		return "✓ **Executed successfully**"
	}

	// Try to extract metadata from the result if it's in JSON/map format
	if metadata := m.extractExecutionMetadata(result); metadata != nil {
		return m.generateMetadataAwareSummary(toolName, metadata)
	}

	// Fall back to the original summary logic
	return m.generateToolSummary(toolName, result, noOutput)
}

// parseToolResult attempts to parse a tool result string into a map
func (m *Model) parseToolResult(result string) (map[string]interface{}, error) {
	// Try to parse as JSON first
	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resultMap); err == nil {
		return resultMap, nil
	}

	// If it's not valid JSON, try to extract JSON from within the string
	if jsonStart := strings.Index(result, "{"); jsonStart >= 0 {
		if jsonEnd := strings.LastIndex(result, "}"); jsonEnd > jsonStart {
			jsonStr := result[jsonStart : jsonEnd+1]
			if err := json.Unmarshal([]byte(jsonStr), &resultMap); err == nil {
				return resultMap, nil
			}
		}
	}

	return nil, fmt.Errorf("could not parse tool result as map")
}

func (m *Model) extractExecutionMetadata(result string) *tools.ExecutionMetadata {
	resultMap, err := m.parseToolResult(result)
	if err != nil {
		return nil
	}

	metadataValue, ok := resultMap["_execution_metadata"]
	if !ok {
		return nil
	}

	return mapExecutionMetadata(metadataValue)
}

func mapExecutionMetadata(value interface{}) *tools.ExecutionMetadata {
	switch v := value.(type) {
	case *tools.ExecutionMetadata:
		return v
	case map[string]interface{}:
		return mapToExecutionMetadata(v)
	default:
		return nil
	}
}

func mapToExecutionMetadata(raw map[string]interface{}) *tools.ExecutionMetadata {
	if raw == nil {
		return nil
	}

	meta := &tools.ExecutionMetadata{}

	if start := parseTimeField(raw["start_time"]); start != nil {
		meta.StartTime = start
	}
	if end := parseTimeField(raw["end_time"]); end != nil {
		meta.EndTime = end
		if meta.StartTime != nil {
			meta.DurationMs = end.Sub(*meta.StartTime).Milliseconds()
		}
	}

	if v, ok := raw["duration_ms"]; ok {
		meta.DurationMs = int64(intValue(v))
	}
	if v, ok := raw["command"]; ok {
		meta.Command = fmt.Sprintf("%v", v)
	}
	if v, ok := raw["exit_code"]; ok {
		meta.ExitCode = intValue(v)
	}
	if v, ok := raw["pid"]; ok {
		meta.PID = intValue(v)
	}
	if v, ok := raw["process_id"]; ok {
		meta.ProcessID = fmt.Sprintf("%v", v)
	}
	if v, ok := raw["output_size_bytes"]; ok {
		meta.OutputSizeBytes = intValue(v)
	}
	if v, ok := raw["output_line_count"]; ok {
		meta.OutputLineCount = intValue(v)
	}
	if v, ok := raw["has_stderr"]; ok {
		meta.HasStderr = boolValue(v)
	}
	if v, ok := raw["stderr_size_bytes"]; ok {
		meta.StderrSizeBytes = intValue(v)
	}
	if v, ok := raw["stderr_line_count"]; ok {
		meta.StderrLineCount = intValue(v)
	}
	if v, ok := raw["working_dir"]; ok {
		meta.WorkingDir = fmt.Sprintf("%v", v)
	}
	if v, ok := raw["timeout_seconds"]; ok {
		meta.TimeoutSeconds = intValue(v)
	}
	if v, ok := raw["was_timed_out"]; ok {
		meta.WasTimedOut = boolValue(v)
	}
	if v, ok := raw["was_backgrounded"]; ok {
		meta.WasBackgrounded = boolValue(v)
	}
	if v, ok := raw["tool_type"]; ok {
		meta.ToolType = fmt.Sprintf("%v", v)
	}
	if v, ok := raw["details"].(map[string]interface{}); ok {
		meta.Details = v
	}
	if v, ok := raw["error_type"]; ok {
		meta.ErrorType = fmt.Sprintf("%v", v)
	}
	if v, ok := raw["error_context"]; ok {
		meta.ErrorContext = fmt.Sprintf("%v", v)
	}

	return meta
}

func parseTimeField(val interface{}) *time.Time {
	switch v := val.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return &parsed
		}
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return &parsed
		}
	case time.Time:
		return &v
	case *time.Time:
		return v
	}
	return nil
}

func intValue(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
}

func boolValue(val interface{}) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return strings.ToLower(v) == "true"
	default:
		return false
	}
}

// generateMetadataAwareSummary creates summaries using execution metadata
func (m *Model) generateMetadataAwareSummary(toolName string, metadata *tools.ExecutionMetadata) string {
	switch toolName {
	case tools.ToolNameShell:
		return m.generateShellSummary(metadata)
	case tools.ToolNameGoSandbox:
		return m.generateSandboxSummary(metadata)
	case tools.ToolNameReadFile:
		return m.generateReadFileSummary(metadata)
	case tools.ToolNameCreateFile, tools.ToolNameEditFile:
		return m.generateFileOperationSummary(toolName, metadata)
	default:
		return m.generateGenericSummary(metadata)
	}
}

// generateShellSummary creates detailed shell command summaries
func (m *Model) generateShellSummary(metadata *tools.ExecutionMetadata) string {
	if metadata.ExitCode == 0 {
		// Successful execution
		summary := "✓ **Command executed successfully**"

		if metadata.DurationMs > 0 {
			summary += fmt.Sprintf(" (%.1fs)", float64(metadata.DurationMs)/1000)
		}

		if metadata.OutputSizeBytes > 0 {
			if metadata.OutputLineCount > 1 {
				summary += fmt.Sprintf(" • %d lines output", metadata.OutputLineCount)
			} else {
				summary += " • output produced"
			}
		}

		if metadata.HasStderr {
			if metadata.StderrSizeBytes > 0 {
				summary += fmt.Sprintf(" • %d lines stderr", metadata.StderrLineCount)
			} else {
				summary += " • stderr output"
			}
		}

		return summary
	} else {
		// Failed execution - provide detailed error information
		summary := "❌ **Command failed**"

		// Add exit code information
		if metadata.ExitCode > 0 {
			summary += fmt.Sprintf(" (exit %d)", metadata.ExitCode)
		}

		// Add error classification if available
		if metadata.ErrorType != "" {
			switch metadata.ErrorType {
			case "timeout":
				summary += " • **timed out**"
			case "permission":
				summary += " • **permission denied**"
			case "not_found":
				summary += " • **command not found**"
			case "syntax":
				summary += " • **syntax error**"
			case "network":
				summary += " • **network error**"
			default:
				summary += fmt.Sprintf(" • **%s**", metadata.ErrorType)
			}
		}

		// Add timing information for long-running commands
		if metadata.DurationMs > 5000 { // > 5 seconds
			summary += fmt.Sprintf(" • after %.1fs", float64(metadata.DurationMs)/1000)
		}

		// Add context if available
		if metadata.ErrorContext != "" {
			summary += fmt.Sprintf(" • `%s`", metadata.ErrorContext)
		}

		// Add stderr hint if there's error output
		if metadata.HasStderr && metadata.StderrSizeBytes > 0 {
			summary += fmt.Sprintf(" • %d lines stderr", metadata.StderrLineCount)
		}

		return summary
	}
}

// generateSandboxSummary creates summaries for Go sandbox execution
func (m *Model) generateSandboxSummary(metadata *tools.ExecutionMetadata) string {
	callSummary := m.sandboxFunctionCallSummary(metadata)

	if metadata.ExitCode == 0 {
		summary := "✓ **Go code executed successfully**"
		if metadata.DurationMs > 0 {
			summary += fmt.Sprintf(" (%.1fs)", float64(metadata.DurationMs)/1000)
		}
		if callSummary != "" {
			summary += " • " + callSummary
		}
		return summary
	} else {
		summary := "❌ **Go execution failed**"
		if metadata.ExitCode > 0 {
			summary += fmt.Sprintf(" (exit %d)", metadata.ExitCode)
		}
		if metadata.ErrorType != "" {
			summary += fmt.Sprintf(" • %s", metadata.ErrorType)
		}
		if callSummary != "" {
			summary += " • " + callSummary
		}
		return summary
	}
}

func (m *Model) sandboxFunctionCallSummary(metadata *tools.ExecutionMetadata) string {
	if metadata == nil || metadata.Details == nil {
		return ""
	}

	counts := make(map[string]int)

	if rawCounts, ok := metadata.Details["function_call_counts"].(map[string]interface{}); ok {
		for name, val := range rawCounts {
			if name == "" {
				continue
			}
			if count := intValue(val); count > 0 {
				counts[name] = count
			}
		}
	}

	if len(counts) == 0 {
		if rawCalls, ok := metadata.Details["function_calls"].([]interface{}); ok {
			for _, entry := range rawCalls {
				if callMap, ok := entry.(map[string]interface{}); ok {
					if name, ok := callMap["name"].(string); ok && name != "" {
						counts[name]++
					}
				}
			}
		}
	}

	if len(counts) == 0 {
		return ""
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s x%d", name, counts[name]))
	}

	return "functions: " + strings.Join(parts, ", ")
}

// generateReadFileSummary creates summaries for file reading operations
func (m *Model) generateReadFileSummary(metadata *tools.ExecutionMetadata) string {
	summary := "✓ **File read**"
	if metadata.OutputSizeBytes > 0 {
		if metadata.OutputLineCount > 1 {
			summary += fmt.Sprintf(" • %d lines", metadata.OutputLineCount)
		}
		summary += fmt.Sprintf(" • %d bytes", metadata.OutputSizeBytes)
	}
	return summary
}

// generateFileOperationSummary creates summaries for file creation/modification
func (m *Model) generateFileOperationSummary(toolName string, metadata *tools.ExecutionMetadata) string {
	var action string
	switch toolName {
	case tools.ToolNameCreateFile:
		action = "created"
	case tools.ToolNameEditFile:
		action = "updated"
	default:
		action = "processed"
	}

	summary := fmt.Sprintf("✓ **File %s**", action)
	if metadata.OutputSizeBytes > 0 {
		if metadata.OutputLineCount > 1 {
			summary += fmt.Sprintf(" • %d lines", metadata.OutputLineCount)
		}
	}
	return summary
}

// generateGenericSummary creates summaries for unknown tool types
func (m *Model) generateGenericSummary(metadata *tools.ExecutionMetadata) string {
	if metadata.ErrorType != "" {
		return fmt.Sprintf("❌ **Failed** • %s", metadata.ErrorType)
	}
	return "✓ **Completed successfully**"
}

func (m *Model) truncateToolResult(result string) string {
	// First, try to split by double newlines (paragraphs)
	paragraphs := strings.Split(result, "\n\n")
	if len(paragraphs) > 1 {
		firstParagraph := strings.TrimSpace(paragraphs[0])
		if len(firstParagraph) > 300 {
			// If first paragraph is too long, truncate it
			return firstParagraph[:297] + "..."
		}
		return firstParagraph
	}

	// If no paragraphs, split by single newlines and take first few lines
	lines := strings.Split(result, "\n")
	if len(lines) > 10 {
		// Take first 10 lines
		firstLines := strings.Join(lines[:10], "\n")
		if len(firstLines) > 300 {
			return firstLines[:297] + "..."
		}
		return firstLines
	}

	// If result is short, return as-is
	if len(result) <= 300 {
		return strings.TrimSpace(result)
	}

	// Otherwise truncate to ~300 characters
	return result[:297] + "..."
}

func (m *Model) updateViewport() {
	var rendered strings.Builder

	for i, msg := range m.messages {
		if i > 0 {
			rendered.WriteString("\n\n")
		}

		// Render header with timestamp and role
		var headerStyle lipgloss.Style
		if msg.role == "You" {
			headerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86")).
				Bold(true)
		} else if msg.role == "Assistant" {
			headerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true)
		} else if msg.role == "Tool" {
			headerStyle = toolCallStyle
		} else {
			headerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
		}

		header := headerStyle.Render(fmt.Sprintf("[%s] %s:", msg.timestamp, msg.role))
		rendered.WriteString(header)
		rendered.WriteString("\n")

		// Render content with markdown for Assistant and Tool messages
		if (msg.role == "Assistant" || msg.role == "Tool") && m.renderer != nil && msg.content != "" {
			// Render markdown with Glamour, which handles syntax highlighting internally via Chroma
			var renderedContent string
			if mdRendered, err := m.renderer.Render(msg.content); err == nil {
				renderedContent = strings.TrimRight(mdRendered, "\n")
			} else {
				// Fallback to plain text if rendering fails
				renderedContent = msg.content
			}

			rendered.WriteString(renderedContent)
		} else {
			// Plain text for user and system messages
			rendered.WriteString(msg.content)
		}
	}

	// Check if we should auto-scroll (user is at or near bottom)
	shouldScroll := m.viewport.AtBottom() || m.isCurrentTabGenerating()

	m.viewport.SetContent(rendered.String())

	// Auto-scroll to bottom when generating or if user was already at bottom
	if shouldScroll {
		m.viewport.GotoBottom()
	}

	m.lastUpdateHeight = len(m.messages)
	m.viewportDirty = false
}

func (m *Model) handleCommand(input string) tea.Cmd {
	return func() tea.Msg {
		if m.onCommand != nil {
			if err := m.onCommand(input); err != nil {
				return ErrMsg(err)
			}
		}
		return CompleteMsg{}
	}
}

func (m *Model) resetInputState() {
	m.textarea.Reset()
	m.textarea.Placeholder = defaultInputPlaceholder
	m.sanitizeState = ansiSanitizeState{}
	m.suggestions = nil
	m.selectedSuggIndex = 0
	m.originalSuggestions = nil
	m.originalInput = ""
	m.tabCycleIndex = 0
}

func (m *Model) commandShortcutForKey(msg tea.KeyMsg, prevValue string) (string, bool) {
	if !m.commandMode {
		return "", false
	}
	if strings.TrimSpace(prevValue) != "" {
		return "", false
	}

	switch strings.ToLower(msg.String()) {
	case "m":
		return "/models", true
	case "p":
		return "/provider", true
	case "i":
		return "/init", true
	case "h":
		return "/help", true
	case "q":
		return "/quit", true
	default:
		return "", false
	}
}

func (m *Model) SetOnSubmit(fn func(string) error) {
	m.onSubmit = fn
}

func (m *Model) SetOnCommand(fn func(string) error) {
	m.onCommand = fn
}

func (m *Model) SetOverlayActive(active bool) {
	m.overlayActive = active
	if active {
		m.textarea.Blur()
	} else {
		m.textarea.Focus()
	}
}

func (m *Model) SetOnStop(fn func() error) {
	m.onStop = fn
}

func (m *Model) SetOnBackground(fn func() error) {
	m.onBackground = fn
}

func (m *Model) SetOnPromptActivity(fn func()) {
	m.onPromptActivity = fn
}

func (m *Model) SetContextFile(path string) {
	m.contextFile = path
}

func (m *Model) AppendAssistantChunk(chunk string) {
	targetTab := m.targetGenerationTab()
	if targetTab < 0 {
		targetTab = m.activeSessionIdx
	}
	m.appendAssistantChunkForTab(targetTab, chunk)
}

func (m *Model) stopGeneration(reason string) {
	// In concurrent mode, stop the current tab's generation
	if !m.isCurrentTabGenerating() {
		return
	}

	currentTab := m.activeSessionIdx
	m.processingStatus = ""
	m.contentReceived = false
	m.thinkingTokens = 0
	if !m.animationsDisabled {
		m.spinnerActive = false
	}

	if m.onStop != nil {
		if err := m.onStop(); err != nil {
			m.addMessageForTab(m.activeSessionIdx, "System", fmt.Sprintf("Failed to stop generation: %v", err))
		}
	}

	if reason != "" && currentTab >= 0 {
		m.addMessageForTab(currentTab, "System", reason)
	}

	if currentTab >= 0 && currentTab < len(m.sessions) {
		m.sessions[currentTab].ThinkingTokens = 0
	}
}

func (m *Model) AddSystemMessage(msg string) {
	m.addMessage("System", msg)
}

func (m *Model) UpdateModel(modelName string) {
	m.currentModel = modelName
}

func (m *Model) ClearMessages() {
	if m.validTabIndex(m.activeSessionIdx) {
		m.storeMessagesForTab(m.activeSessionIdx, []message{}, true)
	} else {
		m.messages = []message{}
		m.updateViewport()
	}
}

// RestoreLoadedSession replaces the active tab's session and UI messages with a saved session
func (m *Model) RestoreLoadedSession(info *LoadedSessionInfo) {
	if info == nil || info.Session == nil {
		return
	}

	if len(m.sessions) == 0 || m.activeSessionIdx < 0 || m.activeSessionIdx >= len(m.sessions) {
		return
	}

	// Reset generation-related state
	m.processingStatus = ""
	m.contentReceived = false
	m.thinkingTokens = 0
	if !m.animationsDisabled {
		m.spinnerActive = false
	}

	// Convert persisted messages into TUI message format
	converted := sessionMessagesToTuiMessages(info.Session.GetMessages())
	m.messages = converted

	// Update active tab to reference the loaded session
	activeTab := m.sessions[m.activeSessionIdx]
	activeTab.Session = info.Session
	activeTab.Messages = converted
	activeTab.LastActiveAt = time.Now()
	if info.Name != "" {
		activeTab.Name = info.Name
	}

	// Persist tab metadata if possible
	if err := m.saveTabState(); err != nil {
		logger.Warn("Failed to save tab state after session restore: %v", err)
	}

	m.updateViewport()
}

// sessionMessagesToTuiMessages converts persisted session messages into the TUI's display format
func sessionMessagesToTuiMessages(stored []*session.Message) []message {
	messages := make([]message, 0, len(stored))
	for _, msg := range stored {
		role := msg.Role
		switch strings.ToLower(msg.Role) {
		case "user":
			role = "You"
		case "assistant":
			role = "Assistant"
		case "tool":
			role = "Tool"
		}

		timestamp := ""
		if !msg.Timestamp.IsZero() {
			timestamp = msg.Timestamp.Format("15:04:05")
		}

		messages = append(messages, message{
			role:      role,
			content:   msg.Content,
			timestamp: timestamp,
			toolName:  msg.ToolName,
			toolID:    msg.ToolID,
		})
	}
	return messages
}

// handleUserInputRequest handles single question user input requests
func (m *Model) handleUserInputRequest(msg UserInputRequestMsg) tea.Cmd {
	// Create user input dialog
	dialog := NewUserInputDialog(msg.Question)

	// Initialize with current window size
	var cmd tea.Cmd
	var model tea.Model
	model, cmd = dialog.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})

	// Store dialog state
	m.userQuestionDialog = model
	m.userQuestionDialogOpen = true
	m.userQuestionResponse = msg.Response
	m.userQuestionError = msg.Error

	// Set overlay active to blur textarea and prevent input
	m.SetOverlayActive(true)

	return cmd
}

// handleUserMultipleQuestionsRequest handles multiple choice question requests
func (m *Model) handleUserMultipleQuestionsRequest(msg UserMultipleQuestionsRequestMsg) tea.Cmd {
	// Parse the questions from the formatted string
	// The format is expected to be:
	// 1. Question text?
	//    a) Option 1
	//    b) Option 2
	//    c) Option 3
	//
	// 2. Another question?
	//    a) Option 1
	//    b) Option 2
	//    c) Option 3

	lines := strings.Split(msg.Questions, "\n")
	var questions []QuestionWithOptions
	var currentQuestion QuestionWithOptions

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if this is a question line (starts with number followed by separator)
		// Support formats: "1. ", "1) ", "1: ", "1- ", etc.
		if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
			// Find the separator after the number
			sepIdx := -1
			for i := 1; i < len(line) && i < 4; i++ { // Check up to 3 digits (for question numbers up to 999)
				if line[i] < '0' || line[i] > '9' {
					if line[i] == '.' || line[i] == ')' || line[i] == ':' || line[i] == '-' {
						sepIdx = i
					}
					break
				}
			}

			if sepIdx > 0 && sepIdx+1 < len(line) {
				// Save previous question if exists
				if currentQuestion.Question != "" {
					questions = append(questions, currentQuestion)
				}
				// Start new question
				currentQuestion = QuestionWithOptions{
					Question: strings.TrimSpace(line[sepIdx+1:]),
					Options:  make([]string, 0),
				}
			}
		} else if len(line) > 1 {
			// Check if this is an option line (starts with letter followed by separator)
			// Support formats: "a) ", "a. ", "a: ", "a- ", "A) ", "A. ", etc.
			firstChar := line[0]
			isOptionLetter := (firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z')

			if isOptionLetter && (line[1] == ')' || line[1] == '.' || line[1] == ':' || line[1] == '-') {
				option := strings.TrimSpace(line[2:])
				currentQuestion.Options = append(currentQuestion.Options, option)
			}
		}
	}

	// Add the last question
	if currentQuestion.Question != "" {
		questions = append(questions, currentQuestion)
	}

	// Validate questions - each must have at least 2 options, max 10
	for _, q := range questions {
		if len(q.Options) < 2 || len(q.Options) > 10 {
			// If parsing failed, fall back to asking as single question
			return m.handleUserInputRequest(UserInputRequestMsg{
				Question: msg.Questions,
				Response: msg.Response,
				Error:    msg.Error,
			})
		}
	}

	if len(questions) == 0 {
		msg.Error <- fmt.Errorf("no questions found")
		return nil
	}

	// Create user questions dialog
	dialog := NewUserQuestionDialog(questions)

	// Initialize with current window size
	var cmd tea.Cmd
	var model tea.Model
	model, cmd = dialog.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})

	m.userQuestionDialog = model
	m.userQuestionDialogOpen = true
	m.userQuestionResponse = msg.Response
	m.userQuestionError = msg.Error

	// Set overlay active to blur textarea and prevent input
	m.SetOverlayActive(true)

	return cmd
}

// isLikelyMultipleChoicePrompt detects the formatted prompt produced by the planning agent's ask_user_multiple tool.
func isLikelyMultipleChoicePrompt(question string) bool {
	q := strings.TrimSpace(question)
	if !strings.Contains(q, "\n") {
		return false // Must be multi-line
	}

	// Check for numbered questions (1., 1), 1:, 10., etc.)
	// Look for a digit followed by a separator at the start of a line
	hasNumberedQuestions := false
	lines := strings.Split(q, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 1 && line[0] >= '0' && line[0] <= '9' {
			// Check if there's a separator after the number(s)
			for i := 1; i < len(line) && i < 4; i++ {
				if line[i] < '0' || line[i] > '9' {
					if line[i] == '.' || line[i] == ')' || line[i] == ':' || line[i] == '-' {
						hasNumberedQuestions = true
						break
					}
					break
				}
			}
			if hasNumberedQuestions {
				break
			}
		}
	}
	if !hasNumberedQuestions {
		return false
	}

	// Check for at least 2 lettered options (a/b, A/B with various separators)
	optionCount := 0
	for _, letter := range []string{"a", "b", "c", "d", "A", "B", "C", "D"} {
		for _, sep := range []string{")", ".", ":", "-"} {
			pattern := letter + sep
			if strings.Contains(q, pattern) {
				optionCount++
				break // Found this letter, check next one
			}
		}
	}

	return optionCount >= 2 // Need at least 2 options to be considered multiple choice
}

// awaitUserResponse waits for a response/err with a safeguard timeout to prevent planner hangs.
func (m *Model) awaitUserResponse(ctx context.Context, resp <-chan string, errc <-chan error) (string, error) {
	timeout := time.After(2 * time.Minute)
	select {
	case r := <-resp:
		return r, nil
	case err := <-errc:
		return "", err
	case <-timeout:
		return "", fmt.Errorf("timed out waiting for user input")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
