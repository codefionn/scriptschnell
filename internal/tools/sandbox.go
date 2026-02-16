package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
	"github.com/codefionn/scriptschnell/internal/session"
)

// ShellExecutor is an interface for executing shell commands
// This allows the sandbox to use either direct execution or actor-based execution
type ShellExecutor interface {
	// ExecuteCommand executes a shell command and returns stdout, stderr, and exit code
	ExecuteCommand(ctx context.Context, args []string, workingDir string, timeout time.Duration, stdin string) (stdout string, stderr string, exitCode int, err error)
}

// Note: The actor.ShellActor interface already matches this signature perfectly,
// so we can use it directly as a ShellExecutor without needing an adapter

// SandboxTool executes Go code in a sandboxed WebAssembly environment
type SandboxTool struct {
	workingDir          string
	tempDir             string
	filesystem          fs.FileSystem
	session             *session.Session
	authorizer          Authorizer
	tinygoManager       *TinyGoManager
	summarizeClient     llm.Client
	shellExecutor       ShellExecutor
	progressCb          progress.Callback
	detector            secretdetect.Detector
	featureFlags        FeatureFlagsProvider                // Interface to check feature flags
	compactor           *OutputCompactor                    // Output compaction handler
	contextWindow       int                                 // Model's context window in tokens
	userInteractionFunc func() *actor.UserInteractionClient // Lazy accessor for user interaction client
	tabIDFunc           func() int                          // Returns current tab ID for user interaction
	authConfig          AuthorizationPersistenceConfig      // Config for persisting authorized commands/domains
	parentCtx           context.Context                     // Parent context without sandbox timeout, used for user interaction
	deadline            ExecDeadline                        // Pausable execution deadline, paused during user interaction
}

func NewSandboxTool(workingDir, tempDir string) *SandboxTool {
	tinygoMgr, err := NewTinyGoManager()
	if err != nil {
		// Log error - TinyGo is required for WASI compilation
		fmt.Fprintf(os.Stderr, "Error: Failed to initialize TinyGo manager: %v\n", err)
		fmt.Fprintf(os.Stderr, "The %s tool will not be available until this is resolved.\n", ToolNameGoSandbox)
	}

	return &SandboxTool{
		workingDir:    workingDir,
		tempDir:       tempDir,
		tinygoManager: tinygoMgr,
	}
}

// NewSandboxToolWithFS creates a sandbox with filesystem and session support
func NewSandboxToolWithFS(workingDir, tempDir string, filesystem fs.FileSystem, sess *session.Session, shellExecutor ShellExecutor) *SandboxTool {
	tinygoMgr, err := NewTinyGoManager()
	if err != nil {
		// Log error - TinyGo is required for WASI compilation
		fmt.Fprintf(os.Stderr, "Error: Failed to initialize TinyGo manager: %v\n", err)
		fmt.Fprintf(os.Stderr, "The %s tool will not be available until this is resolved.\n", ToolNameGoSandbox)
	}

	return &SandboxTool{
		workingDir:    workingDir,
		tempDir:       tempDir,
		filesystem:    filesystem,
		session:       sess,
		tinygoManager: tinygoMgr,
		shellExecutor: shellExecutor,
	}
}

// SetAuthorizer sets the authorizer for domain authorization at runtime
func (t *SandboxTool) SetAuthorizer(auth Authorizer) {
	t.authorizer = auth
}

// SetSummarizeClient sets the summarization LLM client
func (t *SandboxTool) SetSummarizeClient(client llm.Client) {
	t.summarizeClient = client
	// Also update the compactor if it exists
	if t.compactor != nil {
		t.compactor.SetSummarizeClient(client)
	}
}

// SetSecretDetector sets the secret detector for scanning web requests
func (t *SandboxTool) SetSecretDetector(detector secretdetect.Detector) {
	t.detector = detector
}

// SetFeatureFlags sets the feature flags provider
func (t *SandboxTool) SetFeatureFlags(featureFlags FeatureFlagsProvider) {
	t.featureFlags = featureFlags
}

// SetProgressCallback sets a callback for streaming status/output messages.
func (t *SandboxTool) SetProgressCallback(cb progress.Callback) {
	t.progressCb = cb
}

// SetShellExecutor sets the shell executor for command execution
func (t *SandboxTool) SetShellExecutor(executor ShellExecutor) {
	t.shellExecutor = executor
}

// SetCompactionConfig sets the output compaction configuration
func (t *SandboxTool) SetCompactionConfig(compactionConfig config.SandboxOutputCompactionConfig) {
	t.compactor = NewOutputCompactor(compactionConfig, t.contextWindow)
	if t.summarizeClient != nil {
		t.compactor.SetSummarizeClient(t.summarizeClient)
	}
}

// SetContextWindow sets the model's context window in tokens for compaction decisions
func (t *SandboxTool) SetContextWindow(contextWindow int) {
	t.contextWindow = contextWindow
	if t.compactor != nil {
		// Update compactor with new context window
		*t.compactor = *NewOutputCompactor(config.SandboxOutputCompactionConfig{
			Enabled:              t.compactor.compactionConfig.Enabled,
			ContextWindowPercent: t.compactor.compactionConfig.ContextWindowPercent,
			ChunkSize:            t.compactor.compactionConfig.ChunkSize,
		}, contextWindow)
		if t.summarizeClient != nil {
			t.compactor.SetSummarizeClient(t.summarizeClient)
		}
	}
}

// AuthorizationPersistenceConfig holds references needed to persist authorization decisions
type AuthorizationPersistenceConfig struct {
	Config     *config.Config
	ConfigPath string
}

// SetUserInteractionClient sets a lazy accessor for the user interaction client and tab ID function
// for prompting the user during WASM execution (e.g., for sandbox command/domain authorization).
// A function is used instead of a direct reference because the client may be set on the orchestrator
// after the sandbox tool is constructed.
func (t *SandboxTool) SetUserInteractionClient(clientFunc func() *actor.UserInteractionClient, tabIDFunc func() int) {
	t.userInteractionFunc = clientFunc
	t.tabIDFunc = tabIDFunc
}

// SetAuthorizationPersistence sets the config used to persist authorized commands/domains
func (t *SandboxTool) SetAuthorizationPersistence(cfg *config.Config, configPath string) {
	t.authConfig = AuthorizationPersistenceConfig{Config: cfg, ConfigPath: configPath}
}

// buildSandboxEnv returns os.Environ() with TMPDIR/TEMP/TMP overridden to the
// session's shell temp directory so that TinyGo compilation and direct command
// execution use the session-specific temp dir.
func (t *SandboxTool) buildSandboxEnv() []string {
	env := os.Environ()
	if t.session != nil {
		if shellTmpDir := t.session.GetShellTempDir(); shellTmpDir != "" {
			env = sandboxReplaceOrAppendEnv(env, "TMPDIR", shellTmpDir)
			env = sandboxReplaceOrAppendEnv(env, "TEMP", shellTmpDir)
			env = sandboxReplaceOrAppendEnv(env, "TMP", shellTmpDir)
			env = sandboxReplaceOrAppendEnv(env, "SCRIPTSCHNELL_SHELL_TEMP", shellTmpDir)
		}
	}
	return env
}

// sandboxReplaceOrAppendEnv replaces an existing environment variable or appends it.
func sandboxReplaceOrAppendEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// interactionCtx returns the parent context (without sandbox timeout) for user
// interaction calls. If parentCtx was not set (e.g. during tests), it falls
// back to the provided context.
func (t *SandboxTool) interactionCtx(fallback context.Context) context.Context {
	if t.parentCtx != nil {
		return t.parentCtx
	}
	return fallback
}

// ListFilesInDir returns the entries inside the provided directory.
// Paths are returned relative to the supplied dir (or working directory if dir is empty).
func (t *SandboxTool) ListFilesInDir(dir string) ([]string, error) {
	if dir == "" {
		dir = "."
	}

	if t.filesystem == nil {
		return nil, fmt.Errorf("filesystem not available")
	}

	entries, err := t.filesystem.ListDir(context.Background(), dir)
	if err != nil {
		return nil, err
	}

	results := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Path
		results = append(results, name)
	}

	return results, nil
}

// Mkdir creates a directory, optionally recursively.
func (t *SandboxTool) Mkdir(dir string, recursive bool) error {
	if dir == "" {
		return fmt.Errorf("directory path is required")
	}
	if t.filesystem == nil {
		return fmt.Errorf("filesystem not available")
	}

	if !recursive {
		parent := filepath.Dir(dir)
		exists, err := t.filesystem.Exists(context.Background(), parent)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("parent directory does not exist: %s", parent)
		}
	}

	return t.filesystem.MkdirAll(context.Background(), dir, 0755)
}

// Move renames or moves a file or directory.
func (t *SandboxTool) Move(src, dst string) error {
	if src == "" || dst == "" {
		return fmt.Errorf("source and destination paths are required")
	}
	if t.filesystem == nil {
		return fmt.Errorf("filesystem not available")
	}

	return t.filesystem.Move(context.Background(), src, dst)
}

// GetTinyGoManager returns the TinyGo manager instance (can be nil)
func (t *SandboxTool) GetTinyGoManager() *TinyGoManager {
	return t.tinygoManager
}

func (t *SandboxTool) Name() string {
	return ToolNameGoSandbox
}

func (t *SandboxTool) Description() string {
	var b strings.Builder
	b.WriteString("Execute Go code in a strongly sandboxed WebAssembly environment. ")
	b.WriteString("Basic standard library packages available (don't use the `os`, `ioutil, `net`, `exec` package instead use methods provided below). Timeout enforced.\n\n")
	b.WriteString("Every program **must** declare `package main`, define `func main()`, and print results (e.g., via `fmt.Println`) so the orchestrator receives the output.\n\n")
	b.WriteString("**Description Field**: Use the `description` parameter to provide a human-readable explanation of what your sandbox code does. This description will be displayed in the TUI and Web UI to explain what you're doing to the user.\n\n")
	b.WriteString("Try to reduce the output of shell programs by e.g. only searching and outputting errors.\n\n")
	b.WriteString("Output is limited to 4096 lines. When truncated, consider parsing specific parts with Go (e.g., only output lines around error messages).\n\n")
	b.WriteString("Don't use this tool calls for just outputing the summary text at the end.\n\n")
	b.WriteString("# Previous Execution State\n\n")
	b.WriteString("Three global variables are automatically available that preserve output from the previous sandbox execution:\n")
	b.WriteString("- `last_exit_code` (int): Exit code from the last sandbox run (0 on first run)\n")
	b.WriteString("- `last_stdout` (string): Standard output from the last sandbox run (empty on first run)\n")
	b.WriteString("- `last_stderr` (string): Standard error from the last sandbox run (empty on first run)\n\n")
	b.WriteString("These variables are useful when output is truncated and you want to process specific parts in subsequent runs.\n")
	b.WriteString("Example:\n")
	b.WriteString("```go\n")
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("    \"fmt\"\n")
	b.WriteString("    \"strings\"\n")
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("    if last_exit_code != 0 {\n")
	b.WriteString("        fmt.Printf(\"Previous run failed with exit code %d\\n\", last_exit_code)\n")
	b.WriteString("        // Parse errors from last_stderr\n")
	b.WriteString("        for _, line := range strings.Split(last_stderr, \"\\n\") {\n")
	b.WriteString("            if strings.Contains(line, \"error\") {\n")
	b.WriteString("                fmt.Println(line)\n")
	b.WriteString("            }\n")
	b.WriteString("        }\n")
	b.WriteString("    } else if len(last_stdout) > 0 {\n")
	b.WriteString("        fmt.Printf(\"Processing previous output (%d bytes)\\n\", len(last_stdout))\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")
	b.WriteString("# Available Host Functions\n\n")
	b.WriteString("Custom functions are automatically available in your code:\n\n")

	b.WriteString("1. Fetch(method, url, body string) (responseBody string, statusCode int)\n")
	b.WriteString("   - Make HTTP requests (GET, POST, PUT, DELETE, etc.)\n")
	b.WriteString("   - Requires domain authorization\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         response, status := Fetch(\"GET\", \"https://example.com\", \"\")\n")
	b.WriteString("         fmt.Printf(\"Status: %d, Body: %s\\n\", status, response)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("2. ExecuteCommand(command []string, stdin string) (stdout string, stderr string, exitCode int)\n")
	b.WriteString("   - Execute shell commands with optional stdin input\n")
	b.WriteString("   - Requires command authorization\n")
	b.WriteString("   - Pass empty string for stdin if not needed\n")
	b.WriteString("   - Examples:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         out, err, code := ExecuteCommand([]string{\"ls\", \"-la\"}, \"\")\n")
	b.WriteString("         fmt.Printf(\"ls output: %s, stderr: %s, exit: %d\\n\", out, err, code)\n\n")
	b.WriteString("         out, err, code = ExecuteCommand([]string{\"grep\", \"pattern\"}, \"line1\\nline2\\npattern here\\n\")\n")
	b.WriteString("         fmt.Printf(\"grep output: %s, stderr: %s, exit: %d\\n\", out, err, code)\n\n")
	b.WriteString("         out, err, code = ExecuteCommand([]string{\"go\", \"build\", \"./cmd/statcode-ai\"}, \"\")\n")
	b.WriteString("         fmt.Printf(\"go build output: %s, stderr: %s, exit: %d\\n\", out, err, code)\n\n")
	b.WriteString("         _, err, code = ExecuteCommand([]string{\"mkdir\", \"-p\", \"tmp/build/cache\"}, \"\")\n")
	b.WriteString("         fmt.Printf(\"mkdir stderr: %s, exit: %d\\n\", err, code)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("3. Summarize(prompt, text string) (summary string)\n")
	b.WriteString("   - Summarize text using AI (fast summarization model)\n")
	b.WriteString("   - No authorization required\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         summary := Summarize(\"Extract the main points\", longText)\n")
	b.WriteString("         fmt.Printf(\"Summary: %s\\n\", summary)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("4. ReadFile(path string, fromLine, toLine int) (content string)\n")
	b.WriteString("   - Read file from filesystem (relative to working directory)\n")
	b.WriteString("   - fromLine and toLine are 1-indexed (use 0, 0 to read entire file)\n")
	b.WriteString("   - Automatically tracked for session (enables write operations)\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         content := ReadFile(\"config.json\", 0, 0)\n")
	b.WriteString("         fmt.Println(content)\n\n")
	b.WriteString("         lines := ReadFile(\"main.go\", 10, 20)\n")
	b.WriteString("         fmt.Println(lines)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("5. CreateFile(path, content string) (errorMsg string)\n")
	b.WriteString("   - Create a new file with given content\n")
	b.WriteString("   - Returns empty string on success, error message on failure\n")
	b.WriteString("   - Fails if file already exists\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         if err := CreateFile(\"output.txt\", \"Hello, World!\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"create error:\", err)\n")
	b.WriteString("             return\n")
	b.WriteString("         }\n")
	b.WriteString("         fmt.Println(\"file created\")\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("6. WriteFile(path string, append bool, content string) (errorMsg string)\n")
	b.WriteString("   - Write or append content to existing file (must be read first)\n")
	b.WriteString("   - If append is true, content is appended; otherwise file is overwritten\n")
	b.WriteString("   - Enforces read-before-write safety rule\n")
	b.WriteString("   - Returns empty string on success, error message on failure\n")
	b.WriteString("   - Examples:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         // Read file first (required)\n")
	b.WriteString("         _ = ReadFile(\"file.txt\", 0, 0)\n\n")
	b.WriteString("         // Overwrite file with new content\n")
	b.WriteString("         if err := WriteFile(\"file.txt\", false, \"new content\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"write error:\", err)\n")
	b.WriteString("         }\n\n")
	b.WriteString("         // Append content to file\n")
	b.WriteString("         if err := WriteFile(\"file.txt\", true, \"\\nadditional line\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"append error:\", err)\n")
	b.WriteString("         }\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("7. ListFiles(pattern string) (files string)\n")
	b.WriteString("   - List files matching glob pattern (e.g., \"*.go\", \"**/*.txt\")\n")
	b.WriteString("   - Returns newline-separated list of file paths\n")
	b.WriteString("   - Paths are relative to working directory\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import (\n")
	b.WriteString("         \"fmt\"\n")
	b.WriteString("         \"strings\"\n")
	b.WriteString("     )\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         files := ListFiles(\"*.go\")\n")
	b.WriteString("         for _, f := range strings.Split(files, \"\\n\") {\n")
	b.WriteString("             if f == \"\" {\n")
	b.WriteString("                 continue\n")
	b.WriteString("             }\n")
	b.WriteString("             fmt.Println(f)\n")
	b.WriteString("         }\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("8. RemoveFile(path string) (errorMsg string)\n")
	b.WriteString("   - Remove a file from the filesystem\n")
	b.WriteString("   - Returns empty string on success, error message on failure\n")
	b.WriteString("   - File must have been read earlier in the session (read-before-write rule)\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         // Read the file first (required by read-before-write rule)\n")
	b.WriteString("         content := ReadFile(\"old_file.txt\", 0, 0)\n")
	b.WriteString("         fmt.Println(\"File content:\", content)\n\n")
	b.WriteString("         // Now we can remove it\n")
	b.WriteString("         if err := RemoveFile(\"old_file.txt\"); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"remove error:\", err)\n")
	b.WriteString("             return\n")
	b.WriteString("         }\n")
	b.WriteString("         fmt.Println(\"file removed successfully\")\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("9. RemoveDir(path string, recursive bool) (errorMsg string)\n")
	b.WriteString("   - Remove a directory from the filesystem\n")
	b.WriteString("   - Returns empty string on success, error message on failure\n")
	b.WriteString("   - If recursive is true, removes directory and all contents\n")
	b.WriteString("   - If recursive is false, only removes empty directories\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         // Remove an empty directory\n")
	b.WriteString("         if err := RemoveDir(\"empty_dir\", false); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"remove error:\", err)\n")
	b.WriteString("             return\n")
	b.WriteString("         }\n\n")
	b.WriteString("         // Remove a directory and all its contents\n")
	b.WriteString("         if err := RemoveDir(\"temp_dir\", true); err != \"\" {\n")
	b.WriteString("             fmt.Println(\"remove error:\", err)\n")
	b.WriteString("             return\n")
	b.WriteString("         }\n")
	b.WriteString("         fmt.Println(\"directories removed successfully\")\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("10. Mkdir(path string, recursive bool) (errorMsg string)\n")
	b.WriteString("    - Create a directory (set recursive true to create parents)\n")
	b.WriteString("    - Returns empty string on success, error message on failure\n")
	b.WriteString("    - Example:\n")
	b.WriteString("      ```go\n")
	b.WriteString("      package main\n\n")
	b.WriteString("      func main() {\n")
	b.WriteString("          if err := Mkdir(\"tmp/nested\", true); err != \"\" {\n")
	b.WriteString("              println(err)\n")
	b.WriteString("          }\n")
	b.WriteString("      }\n")
	b.WriteString("      ```\n\n")

	b.WriteString("11. Move(src string, dst string) (errorMsg string)\n")
	b.WriteString("    - Move or rename a file or directory\n")
	b.WriteString("    - For files, the read-before-write rule applies (must be read first)\n")
	b.WriteString("    - Example:\n")
	b.WriteString("      ```go\n")
	b.WriteString("      package main\n\n")
	b.WriteString("      func main() {\n")
	b.WriteString("          _ = ReadFile(\"a.txt\", 0, 0)\n")
	b.WriteString("          if err := Move(\"a.txt\", \"archive/a.txt\"); err != \"\" {\n")
	b.WriteString("              println(err)\n")
	b.WriteString("          }\n")
	b.WriteString("      }\n")
	b.WriteString("      ```\n\n")

	b.WriteString("12. GrepFile(pattern, path, glob string, context int) (content string)\n")
	b.WriteString("   - Search for a regex pattern in files, returning matches with line numbers and context.\n")
	b.WriteString("   - `pattern`: Regex pattern to search for.\n")
	b.WriteString("   - `path`: Directory or file to search in (default: '.').\n")
	b.WriteString("   - `glob`: Glob pattern to filter file names (e.g., '*.go', '**/*.js').\n")
	b.WriteString("   - `context`: Number of context lines to show around the match (default: 2).\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         results := GrepFile(\"func main\", \".\", \"*.go\", 1)\n")
	b.WriteString("         fmt.Println(results)\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("13. ConvertHTML(html string) (markdown string)\n")
	b.WriteString("   - Convert HTML content to markdown format\n")
	b.WriteString("   - Uses automatic HTML detection (returns original text if not HTML)\n")
	b.WriteString("   - No authorization required\n")
	b.WriteString("   - Example:\n")
	b.WriteString("     ```go\n")
	b.WriteString("     package main\n\n")
	b.WriteString("     import \"fmt\"\n\n")
	b.WriteString("     func main() {\n")
	b.WriteString("         html := `<html><body><h1>Title</h1><p>Text with <strong>bold</strong>.</p></body></html>`\n")
	b.WriteString("         markdown := ConvertHTML(html)\n")
	b.WriteString("         fmt.Println(markdown)\n")
	b.WriteString("         // Output:\n")
	b.WriteString("         // # Title\n")
	b.WriteString("         //\n")
	b.WriteString("         // Text with **bold**.\n\n")
	b.WriteString("         // Works with Fetch for web scraping:\n")
	b.WriteString("         response, status := Fetch(\"GET\", \"https://example.com\", \"\")\n")
	b.WriteString("         if status == 200 {\n")
	b.WriteString("             markdown := ConvertHTML(response)\n")
	b.WriteString("             fmt.Println(markdown)\n")
	b.WriteString("         }\n")
	b.WriteString("     }\n")
	b.WriteString("     ```\n\n")

	b.WriteString("Example Build Go Program:\n")
	b.WriteString("package main\n")
	b.WriteString("\n")
	b.WriteString("import (\n")
	b.WriteString("	\"fmt\"\n")
	b.WriteString(")\n")
	b.WriteString("\n")
	b.WriteString("func main() {\n")
	b.WriteString("  stdout, stderr, code := ExecuteCommand([]string{\"go\", \"build\", \"./...\"}, \"\")\n")
	b.WriteString("  if code == 0 {\n")
	b.WriteString("    fmt.Println(\"Compiled successfully\")\n")
	b.WriteString("    return\n")
	b.WriteString("  }\n")
	b.WriteString("\n")
	b.WriteString("  fmt.Printf(\"Compilation failed (exit %d)\\nstdout:\\n%s\\nstderr:\\n%s\\n\", code, stdout, stderr)\n")
	b.WriteString("}\n\n")

	b.WriteString("Example Build & Test C# Program (dotnet 8):\n")
	b.WriteString("package main\n")
	b.WriteString("\n")
	b.WriteString("import \"fmt\"\n")
	b.WriteString("\n")
	b.WriteString("func main() {\n")
	b.WriteString("  // Restore dependencies targeting .NET 8\n")
	b.WriteString("  if stdout, stderr, code := ExecuteCommand([]string{\"dotnet\", \"restore\", \"./MySolution.sln\", \"-p:TargetFramework=net8.0\"}, \"\"); code != 0 {\n")
	b.WriteString("    fmt.Printf(\"restore failed (exit %d)\\nstdout:\\n%s\\nstderr:\\n%s\\n\", code, stdout, stderr)\n")
	b.WriteString("    return\n")
	b.WriteString("  }\n\n")
	b.WriteString("  // Build solution\n")
	b.WriteString("  if stdout, stderr, code := ExecuteCommand([]string{\"dotnet\", \"build\", \"./MySolution.sln\", \"-p:TargetFramework=net8.0\"}, \"\"); code != 0 {\n")
	b.WriteString("    fmt.Printf(\"build failed (exit %d)\\nstdout:\\n%s\\nstderr:\\n%s\\n\", code, stdout, stderr)\n")
	b.WriteString("    return\n")
	b.WriteString("  }\n\n")
	b.WriteString("  // Run tests\n")
	b.WriteString("  stdout, stderr, code := ExecuteCommand([]string{\"dotnet\", \"test\", \"./MySolution.sln\", \"-p:TargetFramework=net8.0\"}, \"\")\n")
	b.WriteString("  if code == 0 {\n")
	b.WriteString("    fmt.Println(\"Tests passed\")\n")
	b.WriteString("    return\n")
	b.WriteString("  }\n\n")
	b.WriteString("  fmt.Printf(\"tests failed (exit %d)\\nstdout:\\n%s\\nstderr:\\n%s\\n\", code, stdout, stderr)\n")
	b.WriteString("}\n\n")

	b.WriteString("Example Web Scraping with HTML to Markdown Conversion and Summarization:\n")
	b.WriteString("package main\n")
	b.WriteString("\n")
	b.WriteString("import (\n")
	b.WriteString("	\"fmt\"\n")
	b.WriteString("	\"strings\"\n")
	b.WriteString(")\n")
	b.WriteString("\n")
	b.WriteString("func main() {\n")
	b.WriteString("	// List of documentation pages to fetch and analyze\n")
	b.WriteString("	urls := []string{\n")
	b.WriteString("		\"https://pkg.go.dev/encoding/json\",\n")
	b.WriteString("		\"https://pkg.go.dev/net/http\",\n")
	b.WriteString("		\"https://pkg.go.dev/context\",\n")
	b.WriteString("	}\n\n")
	b.WriteString("	var allContent strings.Builder\n")
	b.WriteString("	successCount := 0\n\n")
	b.WriteString("	fmt.Println(\"Fetching and converting documentation pages...\")\n")
	b.WriteString("	fmt.Println()\n\n")
	b.WriteString("	for _, url := range urls {\n")
	b.WriteString("		// Fetch the webpage\n")
	b.WriteString("		html, status := Fetch(\"GET\", url, \"\")\n")
	b.WriteString("		if status != 200 {\n")
	b.WriteString("			fmt.Printf(\"Failed to fetch %s (status %d)\\n\", url, status)\n")
	b.WriteString("			continue\n")
	b.WriteString("		}\n\n")
	b.WriteString("		// Convert HTML to markdown\n")
	b.WriteString("		markdown := ConvertHTML(html)\n")
	b.WriteString("		fmt.Printf(\"✓ Fetched and converted: %s (%d chars)\\n\", url, len(markdown))\n\n")
	b.WriteString("		// Accumulate content with section headers\n")
	b.WriteString("		allContent.WriteString(fmt.Sprintf(\"\\n\\n## Content from %s\\n\\n\", url))\n")
	b.WriteString("		allContent.WriteString(markdown)\n")
	b.WriteString("		successCount++\n")
	b.WriteString("	}\n\n")
	b.WriteString("	fmt.Println()\n")
	b.WriteString("	fmt.Printf(\"Successfully processed %d/%d pages\\n\", successCount, len(urls))\n")
	b.WriteString("	fmt.Println()\n\n")
	b.WriteString("	if successCount == 0 {\n")
	b.WriteString("		fmt.Println(\"No pages were successfully fetched\")\n")
	b.WriteString("		return\n")
	b.WriteString("	}\n\n")
	b.WriteString("	// Summarize all the collected markdown content\n")
	b.WriteString("	fmt.Println(\"Generating summary...\")\n")
	b.WriteString("	summary := Summarize(\n")
	b.WriteString("		\"Extract the main topics and key functions mentioned across these Go documentation pages\",\n")
	b.WriteString("		allContent.String())\n\n")
	b.WriteString("	fmt.Println(\"=== SUMMARY ===\")\n")
	b.WriteString("	fmt.Println(summary)\n")
	b.WriteString("	fmt.Println(\"=== END ===\")\n")
	b.WriteString("}\n")

	b.WriteString(`
# Critical note
- Do NOT use 'exec.Command' or 'os/exec' to run commands in the sandbox. This will not work as expected
  (use the provided ExecuteCommand function instead)
`)

	return b.String()
}

func (t *SandboxTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "Go code to execute. Must include package main and func main()",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (default 30, max 3600). When running testsuites or compute intensive tasks, consider increasing the timeout.",
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for sandbox execution (optional, defaults to the working directory). Only use in multi-repository projects and if really important for execution, otherwise ddo not use/leave empty.",
			},
			"libraries": map[string]interface{}{
				"type":        "array",
				"description": "Additional Go module dependencies (e.g., ['github.com/foo/bar@v1.0.0'])",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"background": map[string]interface{}{
				"type":        "boolean",
				"description": "Run the sandbox in the background and stream output via the status tool.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Optional human-readable description of what the sandbox code does. This will be displayed in the TUI and Web UI to explain what the LLM is doing.",
			},
		},
		"required": []string{"code"},
	}
}

func (t *SandboxTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	// Extract parameters
	code := GetStringParam(params, "code", "")
	if code == "" {
		return &ToolResult{Error: "code is required"}
	}

	logger.Debug("go_sandbox code:\n%s", code)

	timeout := GetIntParam(params, "timeout", 30)
	if timeout > 3600 {
		timeout = 3600
	}

	// Get libraries if specified
	var libraries []string
	if libsRaw, ok := params["libraries"]; ok {
		if libsArr, ok := libsRaw.([]interface{}); ok {
			for _, lib := range libsArr {
				if libStr, ok := lib.(string); ok {
					libraries = append(libraries, libStr)
				}
			}
		}
	}

	workingDirParam := GetStringParam(params, "working_dir", "")

	sessionDir := ""
	if t.session != nil {
		sessionDir = t.session.WorkingDir
	}

	defaultWorkingDir := t.workingDir
	if defaultWorkingDir == "" {
		defaultWorkingDir = sessionDir
	}
	if defaultWorkingDir == "" {
		defaultWorkingDir = "."
	}

	workingDir := workingDirParam
	if workingDir == "" {
		workingDir = defaultWorkingDir
	}

	if sessionDir != "" {
		sessionAbs, err := filepath.Abs(sessionDir)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("failed to resolve session working directory: %v", err)}
		}

		if !filepath.IsAbs(workingDir) {
			workingDir = filepath.Join(sessionAbs, workingDir)
		}

		absWorkingDir, err := filepath.Abs(workingDir)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("failed to resolve working directory: %v", err)}
		}

		rel, err := filepath.Rel(sessionAbs, absWorkingDir)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("failed to validate working directory: %v", err)}
		}

		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return &ToolResult{Error: fmt.Sprintf("working_dir must be a subdirectory of the session working directory (%s)", sessionAbs)}
		}

		workingDir = absWorkingDir
	} else {
		absWorkingDir, err := filepath.Abs(workingDir)
		if err != nil {
			return &ToolResult{Error: fmt.Sprintf("failed to resolve working directory: %v", err)}
		}
		workingDir = absWorkingDir
	}

	// Extract optional description parameter
	description := GetStringParam(params, "description", "")

	background := GetBoolParam(params, "background", false)

	if background {
		result, err := t.executeBackground(ctx, code, timeout, libraries, workingDir, description)
		if err != nil {
			return &ToolResult{Error: err.Error()}
		}
		return &ToolResult{Result: result}
	}

	sendProgress := func(update progress.Update) {
		if update.Message == "" {
			return
		}
		if err := progress.Dispatch(t.progressCb, update); err != nil {
			logger.Debug("sandbox progress callback error: %v", err)
		}
	}

	sendStatus := func(msg string) {
		sendProgress(progress.Update{
			Message:    msg,
			AddNewLine: true,
			Mode:       progress.ReportJustStatus,
			Ephemeral:  true,
		})
	}

	sendStatus("→ Compiling sandbox program with TinyGo")

	// Use builder to execute
	result, err := t.executeWithBuilderWithDescription(ctx, code, timeout, libraries, workingDir, description)
	if err != nil {
		sendStatus(fmt.Sprintf("✗ Sandbox failed: %v", err))
		return &ToolResult{Error: err.Error()}
	}

	// Apply output compaction if enabled and needed
	if t.compactor != nil {
		if resMap, ok := result.(map[string]interface{}); ok {
			// Compact stdout if present and large enough
			if stdout, hasStdout := resMap["stdout"]; hasStdout {
				if stdoutStr, ok := stdout.(string); ok && t.compactor.ShouldCompact(stdoutStr) {
					sendProgress(progress.Update{
						Message:    "→ Compacting large sandbox output...",
						AddNewLine: true,
						Mode:       progress.ReportJustStatus,
						Ephemeral:  true,
					})
					compactResult, err := t.compactor.Compact(ctx, stdoutStr)
					if err != nil {
						logger.Debug("sandbox compaction failed: %v", err)
					} else if compactResult.WasCompacted {
						resMap["stdout"] = compactResult.Output
						// Add compaction metadata to execution metadata
						if metaVal, ok := resMap["_execution_metadata"]; ok {
							if metaObj, ok := metaVal.(*ExecutionMetadata); ok {
								if metaObj.Details == nil {
									metaObj.Details = make(map[string]interface{})
								}
								metaObj.Details["output_compaction"] = map[string]interface{}{
									"was_compacted":  compactResult.WasCompacted,
									"original_size":  compactResult.OriginalSize,
									"compacted_size": compactResult.CompactedSize,
									"summary_count":  compactResult.SummaryCount,
									"chunks_kept":    compactResult.ChunksKept,
								}
							}
						}
						sendStatus(fmt.Sprintf("✓ Output compacted from %d to %d chars", compactResult.OriginalSize, compactResult.CompactedSize))
					}
				}
			}

			// Compact stderr if present and large enough
			if stderr, hasStderr := resMap["stderr"]; hasStderr {
				if stderrStr, ok := stderr.(string); ok && t.compactor.ShouldCompact(stderrStr) {
					compactResult, err := t.compactor.Compact(ctx, stderrStr)
					if err != nil {
						logger.Debug("sandbox stderr compaction failed: %v", err)
					} else if compactResult.WasCompacted {
						resMap["stderr"] = compactResult.Output
					}
				}
			}

			// Update result with possibly compacted values
			result = resMap
		}
	}

	var (
		uiResult interface{}
		metadata *ExecutionMetadata
	)

	exitCode := 0
	timedOut := false

	if resMap, ok := result.(map[string]interface{}); ok {
		// Format a UI-friendly result to ensure ACP/TUI clients show sandbox output
		if formatted := formatSandboxUIResult(resMap); formatted != "" {
			uiResult = formatted
		}

		// Propagate execution metadata if present, or create new one if description is provided
		if metaVal, ok := resMap["_execution_metadata"]; ok {
			if metaObj, ok := metaVal.(*ExecutionMetadata); ok {
				metadata = metaObj
				// Add description to metadata if provided
				if description != "" {
					if metadata.Details == nil {
						metadata.Details = make(map[string]interface{})
					}
					metadata.Details["description"] = description
				}
			}
		} else if description != "" {
			// Create metadata with description if not already present
			metadata = &ExecutionMetadata{
				ToolType: "sandbox",
				Details: map[string]interface{}{
					"description": description,
				},
			}
		}

		exitCode = coerceExitCode(resMap["exit_code"])
		if timeoutVal, ok := resMap["timeout"].(bool); ok && timeoutVal {
			timedOut = true
		}
	}

	switch {
	case timedOut:
		sendStatus("✗ Sandbox timed out")
	case exitCode == 0:
		sendStatus("✓ Sandbox completed successfully")
	default:
		sendStatus(fmt.Sprintf("⚠️ Sandbox completed with exit code %d", exitCode))
	}

	return &ToolResult{
		Result:            result,
		UIResult:          uiResult,
		ExecutionMetadata: metadata,
	}
}

// executeWithBuilder uses the SandboxBuilder to execute code
func (t *SandboxTool) executeWithBuilder(ctx context.Context, code string, timeout int, libraries []string, workingDir string) (interface{}, error) {
	// Create builder with current tool configuration
	builder := NewSandboxBuilder().
		SetCode(code).
		SetTimeout(timeout).
		SetWorkingDir(workingDir).
		SetTempDir(t.tempDir)

	// Add libraries if any
	if len(libraries) > 0 {
		builder = builder.AddLibraries(libraries...)
	}

	// Set filesystem and session if available
	if t.filesystem != nil {
		builder = builder.SetFilesystem(t.filesystem)
	}
	if t.session != nil {
		builder = builder.SetSession(t.session)
	}

	// Set authorization if available
	if t.authorizer != nil {
		builder = builder.SetAuthorization(t.authorizer)
	}

	// Build and execute using internal compilation method
	// We need to use the internal method to preserve TinyGo manager connection
	return t.executeInternal(ctx, builder)
}

// executeWithBuilderWithDescription uses the SandboxBuilder to execute code with description
func (t *SandboxTool) executeWithBuilderWithDescription(ctx context.Context, code string, timeout int, libraries []string, workingDir string, description string) (interface{}, error) {
	// Create builder with current tool configuration
	builder := NewSandboxBuilder().
		SetCode(code).
		SetTimeout(timeout).
		SetWorkingDir(workingDir).
		SetTempDir(t.tempDir).
		SetDescription(description)

	// Add libraries if any
	if len(libraries) > 0 {
		builder = builder.AddLibraries(libraries...)
	}

	// Set filesystem and session if available
	if t.filesystem != nil {
		builder = builder.SetFilesystem(t.filesystem)
	}
	if t.session != nil {
		builder = builder.SetSession(t.session)
	}

	// Set authorization if available
	if t.authorizer != nil {
		builder = builder.SetAuthorization(t.authorizer)
	}

	// Build and execute using internal compilation method
	// We need to use the internal method to preserve TinyGo manager connection
	return t.executeInternal(ctx, builder)
}
