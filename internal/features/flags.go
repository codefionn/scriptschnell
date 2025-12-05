package features

import "sync"

// FeatureFlags manages runtime feature flags for tools and capabilities.
// This structure is NOT persisted to disk - it's in-memory only.
// Each tool has its own boolean flag with a default value of true (enabled).
type FeatureFlags struct {
	mu sync.RWMutex

	// Planning mode
	PlanningEnabled bool

	// Tool flags - all default to true (enabled)
	ReadFile             bool
	ReadFileSummarized   bool
	CreateFile           bool
	WriteFileDiff        bool
	WriteFileReplace     bool
	WriteFileJSON        bool
	Shell                bool
	StatusProgram        bool
	WaitProgram          bool
	StopProgram          bool
	Parallel             bool
	GoSandbox            bool
	WebSearch            bool
	ToolSummarize        bool
	Todo                 bool
	SearchFiles          bool
	SearchFileContent    bool
	CodebaseInvestigator bool
	SearchContextFiles   bool
	GrepContextFiles     bool
	ReadContextFile      bool
}

// NewFeatureFlags creates a new FeatureFlags instance with default values (all enabled)
func NewFeatureFlags() *FeatureFlags {
	return &FeatureFlags{
		PlanningEnabled:      true,
		ReadFile:             true,
		ReadFileSummarized:   true,
		CreateFile:           true,
		WriteFileDiff:        true,
		WriteFileReplace:     true,
		WriteFileJSON:        true,
		Shell:                true,
		StatusProgram:        true,
		WaitProgram:          true,
		StopProgram:          true,
		Parallel:             true,
		GoSandbox:            true,
		WebSearch:            true,
		ToolSummarize:        true,
		Todo:                 true,
		SearchFiles:          true,
		SearchFileContent:    true,
		CodebaseInvestigator: true,
		SearchContextFiles:   true,
		GrepContextFiles:     true,
		ReadContextFile:      true,
	}
}

// IsToolEnabled checks if a tool is enabled
func (f *FeatureFlags) IsToolEnabled(toolName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	switch toolName {
	case "read_file":
		return f.ReadFile
	case "read_file_summarized":
		return f.ReadFileSummarized
	case "create_file":
		return f.CreateFile
	case "write_file_diff":
		return f.WriteFileDiff
	case "write_file_replace":
		return f.WriteFileReplace
	case "write_file_json":
		return f.WriteFileJSON
	case "shell":
		return f.Shell
	case "status_program":
		return f.StatusProgram
	case "wait_program":
		return f.WaitProgram
	case "stop_program":
		return f.StopProgram
	case "parallel_tool_execution":
		return f.Parallel
	case "go_sandbox":
		return f.GoSandbox
	case "web_search":
		return f.WebSearch
	case "tool_summarize":
		return f.ToolSummarize
	case "todo":
		return f.Todo
	case "search_files":
		return f.SearchFiles
	case "search_file_content":
		return f.SearchFileContent
	case "codebase_investigator":
		return f.CodebaseInvestigator
	case "search_context_files":
		return f.SearchContextFiles
	case "grep_context_files":
		return f.GrepContextFiles
	case "read_context_file":
		return f.ReadContextFile
	default:
		// Unknown tools default to enabled
		return true
	}
}

// EnableTool enables a specific tool
func (f *FeatureFlags) EnableTool(toolName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setToolFlag(toolName, true)
}

// DisableTool disables a specific tool
func (f *FeatureFlags) DisableTool(toolName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setToolFlag(toolName, false)
}

// IsPlanningEnabled checks if planning mode is enabled
func (f *FeatureFlags) IsPlanningEnabled() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.PlanningEnabled
}

// SetPlanningEnabled enables or disables planning mode
func (f *FeatureFlags) SetPlanningEnabled(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PlanningEnabled = enabled
}

// EnableAllTools sets all tools to enabled
func (f *FeatureFlags) EnableAllTools() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ReadFile = true
	f.ReadFileSummarized = true
	f.CreateFile = true
	f.WriteFileDiff = true
	f.WriteFileReplace = true
	f.WriteFileJSON = true
	f.Shell = true
	f.StatusProgram = true
	f.WaitProgram = true
	f.StopProgram = true
	f.Parallel = true
	f.GoSandbox = true
	f.WebSearch = true
	f.ToolSummarize = true
	f.Todo = true
	f.SearchFiles = true
	f.SearchFileContent = true
	f.CodebaseInvestigator = true
	f.SearchContextFiles = true
	f.GrepContextFiles = true
	f.ReadContextFile = true
}

// setToolFlag sets the tool's flag (must be called with lock held)
func (f *FeatureFlags) setToolFlag(toolName string, enabled bool) {
	switch toolName {
	case "read_file":
		f.ReadFile = enabled
	case "read_file_summarized":
		f.ReadFileSummarized = enabled
	case "create_file":
		f.CreateFile = enabled
	case "write_file_diff":
		f.WriteFileDiff = enabled
	case "write_file_replace":
		f.WriteFileReplace = enabled
	case "write_file_json":
		f.WriteFileJSON = enabled
	case "shell":
		f.Shell = enabled
	case "status_program":
		f.StatusProgram = enabled
	case "wait_program":
		f.WaitProgram = enabled
	case "stop_program":
		f.StopProgram = enabled
	case "parallel_tool_execution":
		f.Parallel = enabled
	case "go_sandbox":
		f.GoSandbox = enabled
	case "web_search":
		f.WebSearch = enabled
	case "tool_summarize":
		f.ToolSummarize = enabled
	case "todo":
		f.Todo = enabled
	case "search_files":
		f.SearchFiles = enabled
	case "search_file_content":
		f.SearchFileContent = enabled
	case "codebase_investigator":
		f.CodebaseInvestigator = enabled
	case "search_context_files":
		f.SearchContextFiles = enabled
	case "grep_context_files":
		f.GrepContextFiles = enabled
	case "read_context_file":
		f.ReadContextFile = enabled
	}
	// Unknown tools are ignored
}
