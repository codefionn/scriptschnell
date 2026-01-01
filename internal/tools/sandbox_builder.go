package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

// SandboxBuilder provides a fluent interface for building and executing sandboxed Go code.
//
// The builder pattern offers a type-safe, intuitive API for configuring and executing
// Go code in an isolated WebAssembly environment with full control over:
//   - Code execution and timeouts
//   - External library dependencies
//   - Network access authorization
//   - Filesystem access permissions
//
// Key Features:
//   - Automatic TinyGo compilation (WASI P2 target)
//   - LLM-based domain authorization for network access
//   - Controlled filesystem access through session tracking
//   - Method chaining for clean, readable code
//   - Validation and error accumulation
//   - Clone support for batch operations
//
// Basic Usage:
//
//	result, err := tools.NewSandboxBuilder().
//	    SetCode("package main\nfunc main() { println(\"Hello!\") }").
//	    SetTimeout(30).
//	    Execute(context.Background())
//
// With Authorization:
//
//	result, err := tools.NewSandboxBuilder().
//	    SetCode(networkCode).
//	    SetAuthorization(authActor).
//	    AllowDomain("api.github.com").
//	    Execute(ctx)
//
// Batch Execution:
//
//	base := tools.NewSandboxBuilder().SetTimeout(30)
//	for _, code := range testCases {
//	    result, _ := base.Clone().SetCode(code).Execute(ctx)
//	}
//
// Thread Safety:
//
//	SandboxBuilder instances are NOT thread-safe. Each goroutine should use
//	its own builder instance or clone an existing one.
//
// Error Handling:
//
//	The builder accumulates errors during configuration. Once an error occurs,
//	subsequent method calls are no-ops. Check the error in Execute() or Validate().
type SandboxBuilder struct {
	code          string           // Go source code to execute
	libraries     []string         // External Go module dependencies
	timeout       int              // Execution timeout in seconds (1-120)
	workingDir    string           // Working directory for the sandbox
	tempDir       string           // Temporary directory for compilation
	filesystem    fs.FileSystem    // Filesystem for controlled file access
	session       *session.Session // Session for tracking read/write operations
	authorizer    Authorizer       // Authorization actor for network/command checks
	allowDomains  []string         // Pre-authorized domains (bypass LLM checks)
	allowAll      bool             // Allow all network access (dangerous)
	shellExecutor ShellExecutor    // Shell executor for command execution
	err           error            // Accumulated validation errors
}

// NewSandboxBuilder creates a new sandbox builder with sensible defaults.
//
// Default Configuration:
//   - Timeout: 30 seconds
//   - Working Directory: "." (current directory)
//   - Temp Directory: "/tmp"
//   - Libraries: empty
//   - No filesystem/session (limited file access)
//   - No authorization (no network access)
//   - No pre-authorized domains
//
// The builder uses a fluent interface where methods return *SandboxBuilder
// to enable method chaining:
//
//	builder := tools.NewSandboxBuilder().
//	    SetCode(code).
//	    SetTimeout(60).
//	    AddLibrary("github.com/google/uuid@latest")
//
// Example - Simple execution:
//
//	result, err := tools.NewSandboxBuilder().
//	    SetCode(`package main
//	import "fmt"
//	func main() {
//	    fmt.Println("Hello, World!")
//	}`).
//	    Execute(context.Background())
//
// Example - With timeout and validation:
//
//	builder := tools.NewSandboxBuilder().
//	    SetCode(code).
//	    SetTimeout(120)
//
//	if err := builder.Validate(); err != nil {
//	    return fmt.Errorf("invalid config: %w", err)
//	}
//
//	result, err := builder.Execute(ctx)
//
// Example - Reusable configuration:
//
//	base := tools.NewSandboxBuilder().
//	    SetTimeout(30).
//	    SetWorkingDir("/project")
//
//	// Execute multiple code snippets with same config
//	for _, code := range codes {
//	    result, _ := base.Clone().SetCode(code).Execute(ctx)
//	}
//
// Returns:
//
//	A new SandboxBuilder instance ready for configuration
func NewSandboxBuilder() *SandboxBuilder {
	return &SandboxBuilder{
		timeout:    30, // Default timeout
		libraries:  make([]string, 0),
		workingDir: ".",
		tempDir:    "/tmp",
	}
}

// SetCode sets the Go code to execute in the sandbox.
//
// The code must be a complete Go program including:
//   - package main declaration
//   - func main() entry point
//
// The code will be compiled to WebAssembly using TinyGo and executed in an
// isolated wazero runtime with controlled access to filesystem and network.
//
// Validation:
//   - Code cannot be empty (will set builder error)
//   - Code should be valid Go syntax (checked during compilation)
//
// Example - Simple code:
//
//	builder.SetCode(`package main
//	import "fmt"
//	func main() {
//	    fmt.Println("Hello!")
//	}`)
//
// Example - Multi-line with variables:
//
//	code := `package main
//	import (
//	    "fmt"
//	    "time"
//	)
//	func main() {
//	    fmt.Println("Current time:", time.Now())
//	}`
//	builder.SetCode(code)
//
// Network Access:
//
//	Network code requires SetAuthorization() for LLM-based safety checks:
//
//	code := `package main
//	import (
//	    "fmt"
//	    "net/http"
//	)
//	func main() {
//	    resp, _ := http.Get("https://api.github.com")
//	    fmt.Println("Status:", resp.Status)
//	}`
//	builder.SetCode(code).SetAuthorization(authActor)
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) SetCode(code string) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	if code == "" {
		b.err = fmt.Errorf("code cannot be empty")
		return b
	}
	b.code = code
	return b
}

// AddLibrary adds a single Go module dependency to the sandbox.
//
// Format: "module/path@version"
// Examples:
//   - "github.com/google/uuid@v1.3.0"
//   - "golang.org/x/crypto@latest"
//   - "github.com/gin-gonic/gin@v1.9.1"
//
// IMPORTANT - TinyGo Compatibility:
//
//	TinyGo has limited support for external libraries. Most standard library
//	packages work, but many third-party libraries may not compile.
//	Test compatibility before deploying to production.
//
// Supported Libraries:
//   - Most stdlib packages (fmt, strings, time, etc.)
//   - Simple pure-Go libraries without CGO
//   - Libraries that don't use unsupported reflection
//
// Example - Single library:
//
//	builder.AddLibrary("github.com/google/uuid@v1.3.0")
//
// Example - Multiple libraries:
//
//	builder.
//	    AddLibrary("github.com/google/uuid@v1.3.0").
//	    AddLibrary("golang.org/x/crypto@latest")
//
// Alternative - Use AddLibraries() for multiple:
//
//	builder.AddLibraries(
//	    "github.com/google/uuid@v1.3.0",
//	    "golang.org/x/crypto@latest",
//	)
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) AddLibrary(library string) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	if library == "" {
		b.err = fmt.Errorf("library cannot be empty")
		return b
	}
	b.libraries = append(b.libraries, library)
	return b
}

// AddLibraries adds multiple Go module dependencies at once.
//
// This is a convenience method for adding multiple libraries in a single call.
// Empty strings in the list are automatically filtered out.
//
// Example - Multiple libraries:
//
//	builder.AddLibraries(
//	    "github.com/google/uuid@v1.3.0",
//	    "golang.org/x/crypto@latest",
//	    "github.com/stretchr/testify@v1.8.4",
//	)
//
// Example - Conditional libraries:
//
//	libs := []string{"github.com/google/uuid@v1.3.0"}
//	if needsCrypto {
//	    libs = append(libs, "golang.org/x/crypto@latest")
//	}
//	builder.AddLibraries(libs...)
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) AddLibraries(libraries ...string) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	for _, lib := range libraries {
		if lib != "" {
			b.libraries = append(b.libraries, lib)
		}
	}
	return b
}

const MaxTimeoutSeconds = 600

// SetTimeout sets the execution timeout in seconds.
//
// The timeout applies to the total execution time including:
//   - Code compilation (TinyGo build)
//   - WASM module execution
//
// Constraints:
//   - Minimum: 1 second
//   - Maximum: 600 seconds (10 minutes)
//   - Default: 30 seconds (if not set)
//
// Timeout Behavior:
//   - If execution exceeds timeout, WASM runtime is terminated
//   - Result will have timeout=true and exit_code=-1
//   - Partial output may still be available in stdout
//
// Recommended Values:
//   - Simple scripts: 5-10 seconds
//   - I/O operations: 30-60 seconds
//   - Complex computations: 60-600 seconds
//
// Example - Quick timeout for tests:
//
//	builder.SetTimeout(5) // 5 seconds
//
// Example - Long timeout for complex operations:
//
//	builder.SetTimeout(600) // 6 minutes (maximum)
//
// Example - Handling timeout results:
//
//	builder.SetTimeout(10)
//	result, _ := builder.Execute(ctx)
//	resultMap := result.(map[string]interface{})
//	if resultMap["timeout"].(bool) {
//	    log.Println("Execution timed out!")
//	}
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) SetTimeout(seconds int) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	if seconds <= 0 {
		b.err = fmt.Errorf("timeout must be positive")
		return b
	}
	if seconds > MaxTimeoutSeconds {
		b.err = fmt.Errorf("timeout cannot exceed %d seconds", MaxTimeoutSeconds)
		return b
	}
	b.timeout = seconds
	return b
}

// SetWorkingDir sets the working directory for the sandbox
func (b *SandboxBuilder) SetWorkingDir(dir string) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	if dir == "" {
		b.err = fmt.Errorf("working directory cannot be empty")
		return b
	}
	b.workingDir = dir
	return b
}

// SetTempDir sets the temporary directory for compilation
func (b *SandboxBuilder) SetTempDir(dir string) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	if dir == "" {
		b.err = fmt.Errorf("temp directory cannot be empty")
		return b
	}
	b.tempDir = dir
	return b
}

// SetFilesystem sets the filesystem for controlled file access
// Only files read through this filesystem will be accessible in the sandbox
func (b *SandboxBuilder) SetFilesystem(fs fs.FileSystem) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	b.filesystem = fs
	return b
}

// SetSession sets the session for tracking files read/modified
func (b *SandboxBuilder) SetSession(sess *session.Session) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	b.session = sess
	return b
}

// SetShellExecutor sets the shell executor for command execution in sandbox
func (b *SandboxBuilder) SetShellExecutor(executor ShellExecutor) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	b.shellExecutor = executor
	return b
}

// SetAuthorization sets the authorization actor for network access control.
//
// The authorizer enables network access in sandboxed code with LLM-based
// safety checks. Without an authorizer, all network operations will fail.
//
// How It Works:
//  1. Code attempts HTTP request (e.g., http.Get("https://example.com"))
//  2. Sandbox intercepts the request before it's sent
//  3. Authorizer analyzes the domain using LLM (safety check)
//  4. If approved: Request proceeds normally
//  5. If denied: Request fails with authorization error
//
// Safety Checks:
//   - Analyzes domain reputation and purpose
//   - Checks for suspicious patterns
//   - Requires user approval for unknown domains
//   - Auto-approves common safe domains (github.com, googleapis.com, etc.)
//
// Pre-Authorization:
//
//	Use AllowDomain() or AllowDomains() to bypass LLM checks for trusted domains:
//
//	builder.
//	    SetAuthorization(authActor).
//	    AllowDomain("api.github.com")  // Bypass LLM check
//
// Example - Basic network access:
//
//	code := `package main
//	import (
//	    "fmt"
//	    "net/http"
//	)
//	func main() {
//	    resp, err := http.Get("https://api.github.com")
//	    if err != nil {
//	        fmt.Println("Error:", err)
//	        return
//	    }
//	    fmt.Println("Status:", resp.Status)
//	}`
//
//	builder.
//	    SetCode(code).
//	    SetAuthorization(authActor).  // Required for network
//	    Execute(ctx)
//
// Example - With pre-authorized domains:
//
//	builder.
//	    SetCode(code).
//	    SetAuthorization(authActor).
//	    AllowDomains("github.com", "googleapis.com"). // Skip LLM checks
//	    Execute(ctx)
//
// Security Warning:
//
//	Without an authorizer, network code will fail at runtime.
//	With AllowAllDomains(), ALL network access is permitted (dangerous).
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) SetAuthorization(auth Authorizer) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	b.authorizer = auth
	return b
}

// AllowDomain pre-authorizes access to a specific domain without LLM checks.
//
// Pre-authorized domains bypass the LLM safety analysis and are allowed
// immediately. This is useful for:
//   - Known safe domains (github.com, googleapis.com, npmjs.org)
//   - Internal company APIs
//   - Frequently accessed APIs
//   - Performance optimization (skip LLM call)
//
// REQUIRES: SetAuthorization() must be called for network access
//
// Domain Format:
//   - Use base domain without protocol: "github.com" not "https://github.com"
//   - Subdomains are matched exactly: "api.github.com" != "github.com"
//   - Wildcards not supported (use multiple calls for subdomains)
//
// Example - Single domain:
//
//	builder.
//	    SetAuthorization(authActor).
//	    AllowDomain("github.com")
//
// Example - Multiple domains (method chaining):
//
//	builder.
//	    SetAuthorization(authActor).
//	    AllowDomain("github.com").
//	    AllowDomain("googleapis.com").
//	    AllowDomain("npmjs.org")
//
// Example - API endpoints:
//
//	builder.
//	    SetAuthorization(authActor).
//	    AllowDomain("api.github.com").
//	    AllowDomain("api.openai.com")
//
// Alternative - Use AllowDomains() for multiple:
//
//	builder.AllowDomains("github.com", "googleapis.com", "npmjs.org")
//
// Security Note:
//
//	Only add domains you trust. Pre-authorized domains bypass all safety checks.
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) AllowDomain(domain string) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	if domain == "" {
		b.err = fmt.Errorf("domain cannot be empty")
		return b
	}
	b.allowDomains = append(b.allowDomains, domain)
	return b
}

// AllowDomains pre-authorizes access to multiple domains at once.
//
// This is a convenience method for adding multiple trusted domains.
// Empty strings in the list are automatically filtered out.
//
// Example - Common safe domains:
//
//	builder.
//	    SetAuthorization(authActor).
//	    AllowDomains(
//	        "github.com",
//	        "googleapis.com",
//	        "npmjs.org",
//	        "pkg.go.dev",
//	    )
//
// Example - API endpoints:
//
//	builder.AllowDomains(
//	    "api.github.com",
//	    "api.openai.com",
//	    "api.anthropic.com",
//	)
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) AllowDomains(domains ...string) *SandboxBuilder {
	if b.err != nil {
		return b
	}
	for _, domain := range domains {
		if domain != "" {
			b.allowDomains = append(b.allowDomains, domain)
		}
	}
	return b
}

// AllowAllDomains allows unrestricted network access to ALL domains.
//
// ⚠️  SECURITY WARNING ⚠️
//
// This method bypasses ALL authorization checks including:
//   - LLM-based domain safety analysis
//   - User approval prompts
//   - Domain allowlists
//
// USE ONLY FOR:
//   - Testing in isolated environments
//   - Fully trusted code (code you wrote yourself)
//   - Development/debugging purposes
//
// NEVER USE FOR:
//   - LLM-generated code
//   - User-submitted code
//   - Production environments
//   - Code from untrusted sources
//
// Risks:
//   - Code can access any website
//   - Data exfiltration possible
//   - Malicious API calls
//   - DDoS participation
//   - Privacy violations
//
// Safer Alternative:
//
//	Use AllowDomains() to explicitly list trusted domains:
//
//	builder.AllowDomains("github.com", "googleapis.com")  // Safe
//
// Example - Testing only:
//
//	// Only use in test environments!
//	builder.
//	    SetCode(trustedTestCode).
//	    SetAuthorization(authActor).
//	    AllowAllDomains()  // ⚠️  Dangerous!
//
// Returns:
//
//	The builder instance for method chaining
func (b *SandboxBuilder) AllowAllDomains() *SandboxBuilder {
	if b.err != nil {
		return b
	}
	b.allowAll = true
	return b
}

// Build creates a SandboxTool from the builder configuration
// Returns an error if validation fails
func (b *SandboxBuilder) Build() (*SandboxTool, error) {
	if b.err != nil {
		return nil, b.err
	}

	// Validate required fields
	if b.code == "" {
		return nil, fmt.Errorf("code is required (use SetCode)")
	}

	// Create sandbox tool
	var tool *SandboxTool
	if b.filesystem != nil && b.session != nil {
		tool = NewSandboxToolWithFS(b.workingDir, b.tempDir, b.filesystem, b.session, b.shellExecutor)
	} else {
		tool = NewSandboxTool(b.workingDir, b.tempDir)
		if b.shellExecutor != nil {
			tool.SetShellExecutor(b.shellExecutor)
		}
	}

	// Set authorization if provided
	if b.authorizer != nil {
		tool.SetAuthorizer(b.authorizer)
	}

	return tool, nil
}

// Execute builds and executes the sandboxed Go code
// Returns the execution result or an error
func (b *SandboxBuilder) Execute(ctx context.Context) (interface{}, error) {
	tool, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("build failed: %w", err)
	}

	// Use the tool's internal execution method directly
	// This preserves TinyGo manager connection and avoids double validation
	return tool.executeInternal(ctx, b)
}

// ExecuteWithTimeout is a convenience method that creates a context with timeout and executes
func (b *SandboxBuilder) ExecuteWithTimeout() (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(b.timeout+10)*time.Second)
	defer cancel()
	return b.Execute(ctx)
}

// MustExecute executes the code and panics if there's an error
// Useful for testing or scripts where errors should be fatal
func (b *SandboxBuilder) MustExecute(ctx context.Context) interface{} {
	result, err := b.Execute(ctx)
	if err != nil {
		panic(fmt.Sprintf("sandbox execution failed: %v", err))
	}
	return result
}

// Validate checks if the builder configuration is valid without executing
func (b *SandboxBuilder) Validate() error {
	if b.err != nil {
		return b.err
	}
	if b.code == "" {
		return fmt.Errorf("code is required")
	}
	if execCommandPattern.MatchString(b.code) {
		return fmt.Errorf("direct exec.Command usage is blocked; use ExecuteCommand instead")
	}
	if isTrivialCode(b.code) {
		return fmt.Errorf("code that only contains imports, comments, and fmt print statements is not allowed")
	}
	return nil
}

// Reset clears all builder state for reuse
func (b *SandboxBuilder) Reset() *SandboxBuilder {
	b.code = ""
	b.libraries = make([]string, 0)
	b.timeout = 30
	b.allowDomains = nil
	b.allowAll = false
	b.err = nil
	// Keep workingDir, tempDir, filesystem, session, and authorizer
	return b
}

// Clone creates a copy of the builder with the same configuration
// Useful for creating variations of a base configuration
func (b *SandboxBuilder) Clone() *SandboxBuilder {
	clone := &SandboxBuilder{
		code:          b.code,
		timeout:       b.timeout,
		workingDir:    b.workingDir,
		tempDir:       b.tempDir,
		filesystem:    b.filesystem,
		session:       b.session,
		authorizer:    b.authorizer,
		shellExecutor: b.shellExecutor,
		allowAll:      b.allowAll,
		err:           b.err,
	}

	// Deep copy slices
	clone.libraries = make([]string, len(b.libraries))
	copy(clone.libraries, b.libraries)

	if b.allowDomains != nil {
		clone.allowDomains = make([]string, len(b.allowDomains))
		copy(clone.allowDomains, b.allowDomains)
	}

	return clone
}

// Helper functions and types moved from sandbox.go

var (
	stringLiteralPattern = regexp.MustCompile(`"([^"\\]*(?:\\.[^"\\]*)*)"`)
	execCommandPattern   = regexp.MustCompile(`\bexec\s*\.?\s*Command`)
)

// extractDomainsFromCode scans Go code for domain strings
func extractDomainsFromCode(code string) []string {
	domainSet := make(map[string]struct{})

	for _, match := range stringLiteralPattern.FindAllStringSubmatch(code, -1) {
		raw := strings.TrimSpace(match[1])
		if raw == "" {
			continue
		}

		if strings.Contains(raw, "://") {
			if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
				domain := normalizeDomain(parsed.Host)
				if domain != "" {
					domainSet[domain] = struct{}{}
				}
			}
			continue
		}

		if looksLikeDomain(raw) {
			domain := normalizeDomain(raw)
			if domain != "" {
				domainSet[domain] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		result = append(result, domain)
	}

	return result
}

// normalizeDomain extracts and normalizes a domain name
func normalizeDomain(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			trimmed = parsed.Host
		}
	}

	if idx := strings.Index(trimmed, "/"); idx != -1 {
		trimmed = trimmed[:idx]
	}

	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		trimmed = host
	} else if parts := strings.Split(trimmed, ":"); len(parts) == 2 && parts[1] != "" {
		trimmed = parts[0]
	}

	trimmed = strings.TrimPrefix(trimmed, "www.")
	return strings.ToLower(trimmed)
}

// looksLikeDomain checks if a string appears to be a domain name
func looksLikeDomain(value string) bool {
	candidate := strings.ToLower(strings.TrimSpace(value))
	if candidate == "" {
		return false
	}
	if idx := strings.Index(candidate, "/"); idx != -1 {
		candidate = candidate[:idx]
	}
	if strings.ContainsAny(candidate, " \t\n\r") {
		return false
	}
	if strings.Count(candidate, ".") == 0 {
		return false
	}
	parts := strings.Split(candidate, ".")
	last := parts[len(parts)-1]
	if len(last) < 2 {
		return false
	}
	for _, r := range last {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// stubAuthorizer is a no-op authorizer that allows all operations
// Used in tests when no real authorizer is configured
type stubAuthorizer struct{}

func (s *stubAuthorizer) Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error) {
	// Allow all operations without prompting
	return &AuthorizationDecision{
		Allowed:           true,
		Reason:            "stub authorizer - allows all",
		RequiresUserInput: false,
	}, nil
}

// wasiAuthorizerAdapter adapts tools.Authorizer for use by WASI host functions.
type wasiAuthorizerAdapter struct {
	authorizer Authorizer
}

func (a *wasiAuthorizerAdapter) Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error) {
	decision, err := a.authorizer.Authorize(ctx, toolName, params)
	if err != nil {
		return nil, err
	}
	return decision, nil
}

// containsDangerousOps performs a lightweight scan for APIs that should never
// be allowed inside the sandbox. This is a defensive check used primarily by
// tests to exercise the policy surface; it intentionally looks for a curated
// list of risky functions and packages rather than attempting full parsing.
func containsDangerousOps(code string) bool {
	if code == "" {
		return false
	}

	lower := strings.ToLower(code)

	dangerous := []string{
		"os.removeall",
		"exec.command",
		"syscall.",
		"unsafe.",
		"net.listen",
		"http.listenandserve",
	}

	for _, needle := range dangerous {
		if strings.Contains(lower, needle) {
			return true
		}
	}

	return false
}

// isTrivialCode checks if the code only contains imports, comments, and fmt print statements
// without any substantial logic or operations using AST parsing for accurate results
func isTrivialCode(code string) bool {
	if code == "" {
		return true
	}

	// Parse the code into an AST
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", code, parser.ParseComments|parser.AllErrors)
	if err != nil {
		// If code can't be parsed, it's likely substantial
		return false
	}

	// Walk the AST to check for substantial content
	checker := &trivialCodeChecker{}
	ast.Walk(checker, file)

	// Special case: allow minimal programs with empty main function
	if checker.hasEmptyMainOnly(file) {
		return false // Not trivial - it's a valid minimal program
	}

	return !checker.hasSubstantialContent
}

// trivialCodeChecker walks the AST to detect substantial content
type trivialCodeChecker struct {
	hasSubstantialContent bool
	hasMainFunc           bool
	mainFuncIsEmpty       bool
}

func (c *trivialCodeChecker) Visit(node ast.Node) ast.Visitor {
	if c.hasSubstantialContent {
		return nil // Already found substantial content, stop walking
	}

	switch n := node.(type) {
	case *ast.FuncDecl:
		// Track if we have a main function
		if n.Name.Name == "main" {
			c.hasMainFunc = true
			c.mainFuncIsEmpty = (n.Body == nil) || (len(n.Body.List) == 0)
		}

		// Check function bodies for substantial content
		if n.Body != nil {
			// Only check if the function has non-zero statements
			// Empty function bodies are not substantial
			for _, stmt := range n.Body.List {
				if c.isStatementSubstantial(stmt) {
					c.hasSubstantialContent = true
					return nil
				}
			}
		}
		return nil

	case *ast.GenDecl:
		// Import declarations are not substantial
		if n.Tok == token.IMPORT {
			return nil
		}
		// Other declarations (var, const, type) are substantial
		c.hasSubstantialContent = true
		return nil

	default:
		// For all other nodes, only continue visiting if they might contain substantial content
		// Don't mark unknown nodes as substantial here - let them be visited instead
		return c
	}
}

// hasEmptyMainOnly checks if the program only contains an empty main function
func (c *trivialCodeChecker) hasEmptyMainOnly(file *ast.File) bool {
	// If there's substantial content, it's not minimal
	if c.hasSubstantialContent {
		return false
	}

	// Must have a main function that's empty
	if !c.hasMainFunc || !c.mainFuncIsEmpty {
		return false
	}

	// Check if there are any other function declarations with content
	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			// Skip main function, we already know it's empty
			if funcDecl.Name.Name == "main" {
				continue
			}
			// Any other function with a body makes it substantial
			if funcDecl.Body != nil && len(funcDecl.Body.List) > 0 {
				return false
			}
		}
		// Any other declaration type (var, const, type) makes it substantial
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok != token.IMPORT {
			return false
		}
	}

	// Only has empty main function and optional imports
	return true
}

// isStatementSubstantial checks if an individual statement contains substantial content
func (c *trivialCodeChecker) isStatementSubstantial(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		// Check expression statements (like function calls)
		return c.isExpressionSubstantial(s.X)

	case *ast.DeclStmt:
		// Variable declarations are substantial
		return true

	case *ast.AssignStmt:
		// Variable assignments are substantial
		return true

	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.SelectStmt:
		// Control flow statements are substantial
		return true

	case *ast.ReturnStmt:
		// Return statements with values are substantial
		return len(s.Results) > 0

	case *ast.GoStmt, *ast.DeferStmt:
		// Go and defer statements are substantial
		return true

	case *ast.BlockStmt:
		// Check block statements recursively
		for _, blockStmt := range s.List {
			if c.isStatementSubstantial(blockStmt) {
				return true
			}
		}
		return false

	default:
		// Any other statement type is substantial
		return true
	}
}

// isExpressionSubstantial checks if an expression contains substantial content
func (c *trivialCodeChecker) isExpressionSubstantial(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		// Check function calls
		if c.isFunctionCallSubstantial(e) {
			return true
		}
		return false

	case *ast.Ident:
		// Just identifiers by themselves aren't substantial
		return false

	case *ast.BasicLit:
		// Basic literals (strings, numbers) aren't substantial
		return false

	default:
		// Any other expression type is substantial
		return true
	}
}

// isFunctionCallSubstantial checks if a function call contains substantial content
func (c *trivialCodeChecker) isFunctionCallSubstantial(call *ast.CallExpr) bool {
	// Check if it's a fmt print function
	if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
		if x, ok := fun.X.(*ast.Ident); ok && x.Name == "fmt" {
			switch fun.Sel.Name {
			case "Print", "Println", "Printf", "println", "printf", "print":
				// Check if the arguments contain substantial expressions
				for _, arg := range call.Args {
					if c.isExpressionSubstantial(arg) {
						return true
					}
				}
				return false // Only string literals, not substantial
			}
		}
	}

	// Any function call is substantial (even if arguments are literals)
	return true
}
