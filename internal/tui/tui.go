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
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/statcode-ai/statcode-ai/internal/config"
	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/htmlconv"
	"github.com/statcode-ai/statcode-ai/internal/tools"
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
	generating           bool
	processingStatus     string // Current processing status (e.g., "Calling tool: write_file_diff")
	spinner              spinner.Model
	spinnerActive        bool
	animationsDisabled   bool
	queuedPrompts        []string
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
	contextFile          string
	suggestions          []string
	selectedSuggIndex    int
	filesystem           fs.FileSystem
	workingDir           string
	originalSuggestions  []string // Store original suggestions for cycling
	originalInput        string   // Store original input before cycling
	tabCycleIndex        int      // Tracks current suggestion index for tab cycling
	contextFreePercent   int
	contextWindow        int
	sanitizeState        ansiSanitizeState
	showTodoPanel        bool
	todoClient           *tools.TodoActorClient
	viewportDirty        bool
	viewportRefreshToken int
	config               *config.Config
	activeMCPProvider    func() []string
}

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

func New(currentModel, contextFile string, disableAnimations bool) *Model {
	ta := textarea.New()
	ta.Placeholder = "Type your prompt here... (@ for files, Shift+Enter (or Alt+Enter) for newline, Ctrl+X for commands, Ctrl+B to background shell, Ctrl+D or Ctrl+C×2 to quit)"
	ta.Focus()
	ta.Prompt = "│ "
	ta.CharLimit = 10000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(true)
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "enter", "ctrl+m")

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

	m := &Model{
		textarea:           ta,
		viewport:           vp,
		messages:           []message{},
		currentModel:       currentModel,
		contextFile:        contextFile,
		renderer:           renderer,
		spinner:            sp,
		animationsDisabled: disableAnimations,
		contextFreePercent: 100,
		renderWrapWidth:    80,
	}

	if width, height, ok := detectTerminalSize(); ok {
		m.applyWindowSize(width, height)
	}

	return m
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

// addToolMessage adds a tool message with metadata for potential summary replacement
func (m *Model) addToolMessage(toolName, toolID, content, fullResult string, summarized bool) {
	msg := message{
		role:       "Tool",
		content:    content,
		timestamp:  time.Now().Format("15:04:05"),
		toolName:   toolName,
		toolID:     toolID,
		fullResult: fullResult,
		summarized: summarized,
	}
	m.messages = append(m.messages, msg)
	m.updateViewport()
}

// SetFilesystem sets the filesystem and working directory for filepath autocomplete
func (m *Model) SetFilesystem(fs fs.FileSystem, workingDir string) {
	m.filesystem = fs
	m.workingDir = workingDir
}

// SetTodoClient configures the TodoActorClient for accessing todo state
func (m *Model) SetTodoClient(client *tools.TodoActorClient) {
	m.todoClient = client
}

// SetConfig stores the application configuration for UI elements that need it.
func (m *Model) SetConfig(cfg *config.Config) {
	m.config = cfg
}

// SetActiveMCPProvider registers a callback that supplies currently active MCP servers.
func (m *Model) SetActiveMCPProvider(provider func() []string) {
	m.activeMCPProvider = provider
}

func (m *Model) scheduleViewportRefresh() tea.Cmd {
	m.viewportRefreshToken++
	token := m.viewportRefreshToken
	return tea.Tick(resizeViewportDebounce, func(time.Time) tea.Msg {
		return viewportRefreshMsg{token: token}
	})
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

func (m *Model) applyWindowSize(width, height int) (bool, bool) {
	if width <= 0 || height <= 0 {
		return false, false
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

	if wrapWidth != m.renderWrapWidth || m.renderer == nil {
		if renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(wrapWidth),
			glamour.WithPreservedNewLines(),
		); err == nil {
			m.renderer = renderer
			m.renderWrapWidth = wrapWidth
		}
	}

	if !m.ready {
		m.ready = true
	}

	return widthChanged, heightChanged
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
// an Enter press that should insert a newline instead of submitting the prompt.
// Terminals vary in how they encode Shift+Enter, so we consider multiple
// representations (explicit shift modifier, alt-modified enter, and raw newline runes).
func shouldTreatAsMultilineEnter(msg tea.KeyMsg) bool {
	keyStr := msg.String()
	if strings.Contains(keyStr, "shift+enter") {
		return true
	}

	if msg.Type == tea.KeyCtrlJ {
		return true
	}

	// Some terminals send a bare newline rune when Shift+Enter is pressed.
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '\n' {
		return true
	}

	// Alt+Enter often maps to the same intent; treat it as newline to give users a fallback.
	if msg.Type == tea.KeyEnter && msg.Alt {
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

			// Skip hidden files unless prefix starts with .
			if strings.HasPrefix(baseName, ".") && !strings.HasPrefix(prefix, ".") {
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
		newValue = prefix + "@" + selectedSugg
	} else {
		newValue = selectedSugg
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

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	wasReady := m.ready

	shouldBlockTextarea := false
	if keyMsg, ok := msg.(tea.KeyMsg); ok && len(m.suggestions) > 0 {
		switch keyMsg.Type {
		case tea.KeyUp, tea.KeyDown, tea.KeyTab, tea.KeyShiftTab, tea.KeyEnter:
			shouldBlockTextarea = true
		}
	}

	if !(m.overlayActive && isKeyMsg(msg)) && !shouldBlockTextarea {
		prevValue := m.textarea.Value()
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
				currentValue = converted // Update currentValue to reflect the conversion
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

	baseCmd := tea.Batch(tiCmd, vpCmd, spCmd)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.overlayActive {
			return m, baseCmd
		}
		if shouldTreatAsMultilineEnter(msg) {
			return m, baseCmd
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
			if m.generating && m.onStop != nil {
				m.generating = false
				m.processingStatus = ""
				if !m.animationsDisabled {
					m.spinnerActive = false
				}
				if err := m.onStop(); err != nil {
					fmt.Printf("failed to stop generation: %v\n", err)
				}
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

		case "down":
			if len(m.suggestions) > 0 {
				m.selectedSuggIndex++
				if m.selectedSuggIndex >= len(m.suggestions) {
					m.selectedSuggIndex = 0
				}
				m.tabCycleIndex = m.selectedSuggIndex
				return m, baseCmd
			}

		case "enter":
			// If we have autocomplete suggestions, apply the selected one and clear suggestions
			if len(m.suggestions) > 0 {
				m.applySelectedSuggestion()
				return m, baseCmd
			}

			// Otherwise, process the prompt normally
			rawInput := sanitizePromptInput(m.textarea.Value(), &m.sanitizeState)
			input := strings.TrimSpace(rawInput)
			if input == "" {
				return m, baseCmd
			}

			// Convert HTML to markdown if detected (fallback for non-paste scenarios)
			if converted, wasConverted := htmlconv.ConvertIfHTML(input); wasConverted {
				input = converted
				m.AddSystemMessage("Converted HTML to markdown")
			}

			m.textarea.Reset()
			m.textarea.Placeholder = "Type your prompt here... (@ for files, Shift+Enter (or Alt+Enter) for newline, Ctrl+X for commands, Ctrl+B to background shell, Ctrl+D or Ctrl+C×2 to quit)"
			m.sanitizeState = ansiSanitizeState{}
			m.suggestions = nil
			m.selectedSuggIndex = 0
			m.originalSuggestions = nil
			m.originalInput = ""

			isCommand := m.commandMode || strings.HasPrefix(input, "/")
			if isCommand {
				m.commandMode = false
				if m.generating {
					m.AddSystemMessage("Finish current response before running commands.")
					return m, baseCmd
				}
				return m, tea.Batch(baseCmd, m.handleCommand(input))
			}

			if m.generating {
				m.queuePrompt(input)
				return m, baseCmd
			}

			return m, tea.Batch(baseCmd, m.startPrompt(input))
		}

		return m, baseCmd

	case tea.WindowSizeMsg:
		widthChanged, heightChanged := m.applyWindowSize(msg.Width, msg.Height)
		if !widthChanged && heightChanged && m.viewport.AtBottom() {
			m.viewport.GotoBottom()
		}

		if widthChanged || wasReady != m.ready {
			m.viewportDirty = true
			if cmd := m.scheduleViewportRefresh(); cmd != nil {
				return m, tea.Batch(baseCmd, cmd)
			}
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

	case GeneratingMsg:
		if msg.Content != "" {
			// If this is the first chunk of assistant content, summarize any previous tool results
			if len(m.messages) == 0 || m.messages[len(m.messages)-1].role != "Assistant" {
				m.summarizeLastToolResult()
			}
			if len(m.messages) == 0 || m.messages[len(m.messages)-1].role != "Assistant" {
				m.addMessage("Assistant", "")
			}
			m.appendToLastMessage(msg.Content)
			m.updateViewport()
		}
		return m, baseCmd

	case CompleteMsg:
		m.generating = false
		m.processingStatus = ""
		if !m.animationsDisabled {
			m.spinnerActive = false
		}
		next := m.processNextQueuedPrompt()
		if next != nil {
			return m, tea.Batch(baseCmd, next)
		}
		return m, baseCmd

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
		return m, baseCmd

	case ErrMsg:
		if errors.Is(error(msg), ErrQuitRequested) {
			return m, tea.Batch(baseCmd, tea.Quit)
		}
		m.err = msg
		m.errVisibleUntil = time.Now().Add(errorDisplayDuration)
		m.generating = false
		if !m.animationsDisabled {
			m.spinnerActive = false
		}
		next := m.processNextQueuedPrompt()
		if next != nil {
			return m, tea.Batch(baseCmd, next)
		}
		return m, baseCmd
	}

	return m, baseCmd
}

func (m *Model) startPrompt(input string) tea.Cmd {
	m.processingStatus = ""
	m.addMessage("You", input)
	m.generating = true
	var cmds []tea.Cmd
	if !m.animationsDisabled {
		m.spinnerActive = true
		cmds = append(cmds, func() tea.Msg { return m.spinner.Tick() })
	}
	cmds = append(cmds, m.handleSubmit(input))
	return tea.Batch(cmds...)
}

func (m *Model) queuePrompt(input string) {
	m.queuedPrompts = append(m.queuedPrompts, input)
	preview := formatQueuedPreview(input)
	m.AddSystemMessage(fmt.Sprintf("Queued prompt #%d: %s", len(m.queuedPrompts), preview))
}

func (m *Model) processNextQueuedPrompt() tea.Cmd {
	if len(m.queuedPrompts) == 0 {
		return nil
	}
	next := m.queuedPrompts[0]
	m.queuedPrompts = m.queuedPrompts[1:]
	preview := formatQueuedPreview(next)
	remaining := len(m.queuedPrompts)
	if remaining > 0 {
		m.AddSystemMessage(fmt.Sprintf("Processing queued prompt: %s (%d remaining)", preview, remaining))
	} else {
		m.AddSystemMessage(fmt.Sprintf("Processing queued prompt: %s", preview))
	}
	return m.startPrompt(next)
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
	title := titleStyle.Render("StatCode AI - AI-Powered Coding Assistant")
	status := statusStyle.Render(fmt.Sprintf("Model: %s | Ctrl+X: Commands | Ctrl+B: Background shell | ESC: Stop | Ctrl+D/Ctrl+C×2: Quit", m.currentModel))

	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(status)
	sb.WriteString("\n\n")

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
		if !m.animationsDisabled && m.spinnerActive {
			footerLeft = statusStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), m.processingStatus))
		} else {
			footerLeft = statusStyle.Render(fmt.Sprintf("⚙️  %s", m.processingStatus))
		}
	} else if m.generating {
		if !m.animationsDisabled && m.spinnerActive {
			footerLeft = statusStyle.Render(fmt.Sprintf("%s Generating...", m.spinner.View()))
		} else {
			footerLeft = statusStyle.Render("⏳ Generating...")
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
	if strings.TrimSpace(m.contextFile) == "" {
		return "Context: (none)"
	}
	return fmt.Sprintf("Context: %s", m.contextFile)
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

func (m *Model) renderTodoPanel() string {
	if m.todoClient == nil {
		return ""
	}

	todoList, err := m.todoClient.List()

	var content strings.Builder
	content.WriteString(todoTitleStyle.Render("Todo Tasks"))
	content.WriteString("\n")

	switch {
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
		return todoPanelStyle.Render(strings.TrimRight(content.String(), "\n"))
	}

	sort.Strings(names)
	for _, name := range names {
		content.WriteString(todoItemStyle.Render(fmt.Sprintf("• %s", name)))
		content.WriteString("\n")
	}

	return todoPanelStyle.Render(strings.TrimRight(content.String(), "\n"))
}

// renderTodoTree renders todos hierarchically
func (m *Model) renderTodoTree(content *strings.Builder, items []*tools.TodoItem, parentID string, depth int) {
	// Find all items with the specified parent
	for _, item := range items {
		if item.ParentID != parentID {
			continue
		}

		marker := " "
		itemStyle := todoItemStyle
		if item.Completed {
			marker = "x"
			itemStyle = todoCompletedStyle
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

func (m *Model) addMessage(role, content string) {
	timestamp := time.Now().Format("15:04:05")
	m.messages = append(m.messages, message{
		role:      role,
		content:   content,
		timestamp: timestamp,
	})
	m.updateViewport()
}

func (m *Model) appendToLastMessage(content string) {
	if len(m.messages) == 0 {
		m.addMessage("Assistant", content)
		return
	}

	// Check if the last message is an unsimplified tool result and summarize it
	lastMsg := &m.messages[len(m.messages)-1]
	if lastMsg.role == "Tool" && !lastMsg.summarized && lastMsg.fullResult != "" {
		// Replace with summary
		summary := m.generateEnhancedToolSummary(lastMsg.toolName, lastMsg.fullResult, false)
		lastMsg.content = summary
		lastMsg.summarized = true
	}

	m.messages[len(m.messages)-1].content += content
}

// summarizeLastToolResult summarizes the last tool result if it hasn't been summarized yet
func (m *Model) summarizeLastToolResult() {
	if len(m.messages) == 0 {
		return
	}

	lastMsg := &m.messages[len(m.messages)-1]
	if lastMsg.role == "Tool" && !lastMsg.summarized && lastMsg.fullResult != "" {
		summary := m.generateEnhancedToolSummary(lastMsg.toolName, lastMsg.fullResult, false)
		lastMsg.content = summary
		lastMsg.summarized = true
		m.updateViewport()
	}
}

func (m *Model) addToolCallMessage(toolName, toolID string, parameters map[string]interface{}) {
	// Format parameters as JSON for display
	var paramsBuf strings.Builder
	if len(parameters) > 0 {
		// Format parameters in a more readable way
		paramsBuf.WriteString("\n**Parameters:**\n")
		for k, v := range parameters {
			paramsBuf.WriteString(fmt.Sprintf("  • %s: %v\n", k, v))
		}
	}

	// Create more descriptive content based on tool type
	var content string
	switch toolName {
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
	case tools.ToolNameWriteFileDiff:
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
	default:
		content = fmt.Sprintf("🔧 **Calling tool:** `%s`", toolName)
	}

	content += paramsBuf.String()

	m.addMessage("Tool", content)
}

func (m *Model) addToolResultMessage(toolName, toolID, result, errorMsg string) {
	var content string
	if errorMsg != "" {
		content = fmt.Sprintf("❌ **Error:** %s", errorMsg)
		m.addToolMessage(toolName, toolID, content, "", false)
	} else if result == "" {
		content = m.generateToolSummary(toolName, result, true)
		m.addToolMessage(toolName, toolID, content, "", true)
	} else {
		// Create the full result display
		displayResult := m.truncateToolResult(result)
		fullContent := fmt.Sprintf("✓ **Result:**\n```\n%s\n```", displayResult)
		m.addToolMessage(toolName, toolID, fullContent, result, false)
	}
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

	case tools.ToolNameWriteFileDiff:
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
	var metadata *tools.ExecutionMetadata
	if resultMap, err := m.parseToolResult(result); err == nil {
		if metadataValue, hasMetadata := resultMap["_execution_metadata"]; hasMetadata {
			if metadataObj, ok := metadataValue.(*tools.ExecutionMetadata); ok {
				metadata = metadataObj
			}
		}
	}

	// If we have enhanced metadata, use it for better summaries
	if metadata != nil {
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

// generateMetadataAwareSummary creates summaries using execution metadata
func (m *Model) generateMetadataAwareSummary(toolName string, metadata *tools.ExecutionMetadata) string {
	switch toolName {
	case tools.ToolNameShell:
		return m.generateShellSummary(metadata)
	case tools.ToolNameGoSandbox:
		return m.generateSandboxSummary(metadata)
	case tools.ToolNameReadFile:
		return m.generateReadFileSummary(metadata)
	case tools.ToolNameCreateFile, tools.ToolNameWriteFileDiff:
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
	if metadata.ExitCode == 0 {
		summary := "✓ **Go code executed successfully**"
		if metadata.DurationMs > 0 {
			summary += fmt.Sprintf(" (%.1fs)", float64(metadata.DurationMs)/1000)
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
		return summary
	}
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
	case tools.ToolNameWriteFileDiff:
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
	if len(lines) > 3 {
		// Take first 3 lines
		firstLines := strings.Join(lines[:3], "\n")
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

		// Render content with markdown for Assistant messages only
		if msg.role == "Assistant" && m.renderer != nil && msg.content != "" {
			// Render markdown
			if mdRendered, err := m.renderer.Render(msg.content); err == nil {
				rendered.WriteString(strings.TrimRight(mdRendered, "\n"))
			} else {
				// Fallback to plain text if rendering fails
				rendered.WriteString(msg.content)
			}
		} else {
			// Plain text for user and system messages
			rendered.WriteString(msg.content)
		}
	}

	// Check if we should auto-scroll (user is at or near bottom)
	shouldScroll := m.viewport.AtBottom() || m.generating

	m.viewport.SetContent(rendered.String())

	// Auto-scroll to bottom when generating or if user was already at bottom
	if shouldScroll {
		m.viewport.GotoBottom()
	}

	m.lastUpdateHeight = len(m.messages)
	m.viewportDirty = false
}

func (m *Model) handleSubmit(input string) tea.Cmd {
	return func() tea.Msg {
		if m.onSubmit != nil {
			if err := m.onSubmit(input); err != nil {
				return ErrMsg(err)
			}
		}
		// Don't send CompleteMsg here - it will be sent by the streaming callback
		return nil
	}
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
	if len(m.messages) == 0 || m.messages[len(m.messages)-1].role != "Assistant" {
		m.addMessage("Assistant", chunk)
	} else {
		m.appendToLastMessage(chunk)
	}
	m.updateViewport()
}

func (m *Model) AddSystemMessage(msg string) {
	m.addMessage("System", msg)
}

func (m *Model) UpdateModel(modelName string) {
	m.currentModel = modelName
}

func (m *Model) ClearMessages() {
	m.messages = []message{}
	m.updateViewport()
}
