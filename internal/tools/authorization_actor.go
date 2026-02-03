package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/llm"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/secretdetect"
	"github.com/codefionn/scriptschnell/internal/session"
)

// AuthorizationDecision captures the result of an authorization check.
type AuthorizationDecision struct {
	Allowed                bool
	Reason                 string
	RequiresUserInput      bool   // If true, the caller should prompt the user for approval
	SuggestedCommandPrefix string // Optional command prefix that can be authorized for future runs
}

// Authorizer defines the contract for authorizing tool calls before execution.
type Authorizer interface {
	Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error)
}

// AuthorizationOptions configure pre-authorizations for the actor.
type AuthorizationOptions struct {
	DangerouslyAllowAll bool
	AllowAllNetwork     bool
	AllowedDirs         []string
	AllowedFiles        []string
	AllowedDomains      []string
	AllowedCommands     []string // Command prefixes that are pre-authorized
	RequireSandboxAuth  bool     // Require authorization for every go_sandbox and shell call
}

// AuthorizationActor handles policy decisions for tool calls in a centralized manner.
type AuthorizationActor struct {
	id              string
	fs              fs.FileSystem
	session         *session.Session
	summarizeClient llm.Client

	options            AuthorizationOptions
	allowedFiles       map[string]struct{}
	allowedDirs        []string
	allowedDomains     map[string]struct{}
	allowedCommands    []string // Prefixes of commands that are pre-authorized
	requireSandboxAuth bool
	workingDir         string
	lastLLMSuccesses   []authorizationRecord
	lastLLMDeclines    []authorizationRecord
}

// NewAuthorizationActor constructs a new authorization actor instance.
func NewAuthorizationActor(id string, filesystem fs.FileSystem, sess *session.Session, summarizeClient llm.Client, opts *AuthorizationOptions) *AuthorizationActor {
	actor := &AuthorizationActor{
		id:              id,
		fs:              filesystem,
		session:         sess,
		summarizeClient: summarizeClient,
	}
	if opts != nil {
		actor.options = *opts
	}
	actor.initPreauthorizations()
	return actor
}

func (a *AuthorizationActor) initPreauthorizations() {
	logger.Debug("AuthorizationActor: initPreauthorizations called with RequireSandboxAuth=%v", a.requireSandboxAuth)
	if a.session != nil && a.session.WorkingDir != "" {
		if abs, err := filepath.Abs(a.session.WorkingDir); err == nil {
			a.workingDir = abs
		} else {
			a.workingDir = filepath.Clean(a.session.WorkingDir)
		}
	} else if cwd, err := os.Getwd(); err == nil {
		a.workingDir = cwd
	}

	if a.options.DangerouslyAllowAll {
		a.options.AllowAllNetwork = true
	}

	if len(a.options.AllowedFiles) > 0 {
		a.allowedFiles = make(map[string]struct{}, len(a.options.AllowedFiles))
		for _, path := range a.options.AllowedFiles {
			if abs, err := a.normalizePath(path); err == nil {
				a.allowedFiles[abs] = struct{}{}
			}
		}
	}

	for _, dir := range a.options.AllowedDirs {
		if abs, err := a.normalizePath(dir); err == nil {
			a.allowedDirs = append(a.allowedDirs, abs)
		}
	}

	if len(a.options.AllowedDomains) > 0 {
		a.allowedDomains = make(map[string]struct{})
		for _, domain := range a.options.AllowedDomains {
			normalized := normalizeAuthorizationDomain(domain)
			if normalized != "" {
				a.allowedDomains[normalized] = struct{}{}
			}
		}
	}

	for _, prefix := range a.options.AllowedCommands {
		if prefix != "" {
			a.allowedCommands = append(a.allowedCommands, prefix)
		}
	}

	a.requireSandboxAuth = a.options.RequireSandboxAuth
}

func (a *AuthorizationActor) normalizePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	base := a.workingDir
	if base == "" {
		if cwd, err := os.Getwd(); err == nil {
			base = cwd
		}
	}

	if base == "" {
		return filepath.Clean(path), nil
	}

	return filepath.Clean(filepath.Join(base, path)), nil
}

func (a *AuthorizationActor) isPathPreauthorized(path string) bool {
	if len(a.allowedFiles) == 0 && len(a.allowedDirs) == 0 {
		return false
	}

	abs, err := a.normalizePath(path)
	if err != nil {
		return false
	}

	if _, ok := a.allowedFiles[abs]; ok {
		return true
	}

	for _, dir := range a.allowedDirs {
		if isWithinDir(abs, dir) {
			return true
		}
	}

	return false
}

func (a *AuthorizationActor) isDomainAuthorized(domain string) bool {
	if a.options.DangerouslyAllowAll || a.options.AllowAllNetwork {
		return true
	}

	normalized := normalizeAuthorizationDomain(domain)
	if normalized == "" {
		return false
	}

	if a.allowedDomains != nil {
		if _, ok := a.allowedDomains[normalized]; ok {
			return true
		}

		for pattern := range a.allowedDomains {
			if matchesWildcardDomain(pattern, normalized) {
				return true
			}
		}
	}

	if a.session != nil && a.session.IsDomainAuthorized(normalized) {
		return true
	}

	return false
}

func (a *AuthorizationActor) isCommandAuthorized(command string) bool {
	if a.options.DangerouslyAllowAll {
		return true
	}

	trimmedCmd := strings.TrimSpace(command)
	if trimmedCmd == "" {
		return false
	}

	// Check against pre-authorized command prefixes
	for _, prefix := range a.allowedCommands {
		if strings.HasPrefix(trimmedCmd, prefix) {
			return true
		}
	}

	// Check session-level authorized commands
	if a.session != nil && a.session.IsCommandAuthorized(trimmedCmd) {
		return true
	}

	return false
}

func isWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	return !strings.HasPrefix(rel, "..")
}

func normalizeAuthorizationDomain(domain string) string {
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

// ID returns the actor identifier.
func (a *AuthorizationActor) ID() string {
	return a.id
}

// Start implements the actor.Actor interface. No initialization needed for now.
func (a *AuthorizationActor) Start(ctx context.Context) error {
	return nil
}

// Stop implements the actor.Actor interface. Nothing to clean up currently.
func (a *AuthorizationActor) Stop(ctx context.Context) error {
	return nil
}

// Receive processes incoming authorization requests.
func (a *AuthorizationActor) Receive(ctx context.Context, message actor.Message) error {
	switch msg := message.(type) {
	case *AuthorizeToolCallMessage:
		decision, err := a.authorize(msg.RequestCtx, msg.ToolName, msg.Params)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-msg.RequestCtx.Done():
			return nil
		case msg.ResponseChan <- AuthorizationResponse{Decision: decision, Err: err}:
			return nil
		}
	default:
		return fmt.Errorf("authorization actor received unknown message type: %s", message.Type())
	}
}

// AuthorizationResponse contains the outcome of an authorization request.
type AuthorizationResponse struct {
	Decision *AuthorizationDecision
	Err      error
}

// AuthorizeToolCallMessage requests authorization for a tool invocation.
type AuthorizeToolCallMessage struct {
	ToolName     string
	Params       map[string]interface{}
	RequestCtx   context.Context
	ResponseChan chan AuthorizationResponse
}

// Type implements actor.Message for AuthorizeToolCallMessage.
func (m *AuthorizeToolCallMessage) Type() string {
	return "tools_authorize_tool_call"
}

// AuthorizationActorClient is the synchronous facade used by callers to reach the actor.
type AuthorizationActorClient struct {
	ref *actor.ActorRef
}

// NewAuthorizationActorClient wraps an actor reference with the Authorizer interface.
func NewAuthorizationActorClient(ref *actor.ActorRef) Authorizer {
	return &AuthorizationActorClient{ref: ref}
}

// Authorize sends a request to the authorization actor and waits for the decision.
func (c *AuthorizationActorClient) Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	response := make(chan AuthorizationResponse, 1)

	msg := &AuthorizeToolCallMessage{
		ToolName:     toolName,
		Params:       params,
		RequestCtx:   ctx,
		ResponseChan: response,
	}

	if err := c.ref.Send(msg); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-response:
		if resp.Err != nil {
			return nil, resp.Err
		}
		return resp.Decision, nil
	}
}

// authorize evaluates tool-specific authorization policies.
func (a *AuthorizationActor) authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error) {
	logger.Debug("AuthorizationActor: authorize called for tool=%s, requireSandboxAuth=%v", toolName, a.requireSandboxAuth)
	if a.options.DangerouslyAllowAll {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	// Handle go_sandbox - always require user authorization if RequireSandboxAuth is set
	if toolName == ToolNameGoSandbox && a.requireSandboxAuth {
		logger.Info("Authorization: go_sandbox requires authorization (RequireSandboxAuth=true)")
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            "go_sandbox requires user authorization (--require-sandbox-auth flag is set)",
			RequiresUserInput: true,
		}, nil
	}

	// Handle shell and command - always require user authorization if RequireSandboxAuth is set
	if (toolName == ToolNameShell || toolName == ToolNameCommand) && a.requireSandboxAuth {
		logger.Info("Authorization: %s requires authorization (RequireSandboxAuth=true)", toolName)
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            "shell command requires user authorization (--require-sandbox-auth flag is set)",
			RequiresUserInput: true,
		}, nil
	}

	switch toolName {
	case ToolNameEditFile:
		return a.authorizeWriteFileDiff(ctx, params)
	case ToolNameReplaceFile:
		return a.authorizeReplaceFile(ctx, params)
	case ToolNameCreateFile:
		return a.authorizeCreateFile(ctx, params)
	case ToolNameGoSandboxDomain:
		return a.authorizeSandboxDomain(ctx, params)
	case ToolNameWebFetch:
		return a.authorizeWebFetch(ctx, params)
	case ToolNameShell:
		return a.authorizeShell(ctx, params)
	case ToolNameCommand:
		return a.authorizeShell(ctx, params)
	default:
		return &AuthorizationDecision{Allowed: true}, nil
	}
}

// authorizeWriteFileDiff enforces the read-before-write policy for existing files.
func (a *AuthorizationActor) authorizeWriteFileDiff(ctx context.Context, params map[string]interface{}) (*AuthorizationDecision, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &AuthorizationDecision{Allowed: false, Reason: "path is required"}, nil
	}

	if a.isPathPreauthorized(path) {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	if a.fs == nil {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	exists, err := a.fs.Exists(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("authorization failed while checking file existence: %w", err)
	}

	if !exists {
		return &AuthorizationDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("File %s does not exist. Use the %s tool to create new files before applying diffs.", path, ToolNameCreateFile),
		}, nil
	}

	if a.session != nil && !a.session.WasFileRead(path) {
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("File %s exists but was not read in this session. The LLM is trying to apply a diff without reading it first.", path),
			RequiresUserInput: true,
		}, nil
	}

	return &AuthorizationDecision{Allowed: true}, nil
}

// authorizeReplaceFile enforces the read-before-write policy for replacing entire file content.
func (a *AuthorizationActor) authorizeReplaceFile(ctx context.Context, params map[string]interface{}) (*AuthorizationDecision, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &AuthorizationDecision{Allowed: false, Reason: "path is required"}, nil
	}

	if a.isPathPreauthorized(path) {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	if a.fs == nil {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	// Check file exists first
	exists, err := a.fs.Exists(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("authorization failed while checking file existence: %w", err)
	}

	if !exists {
		return &AuthorizationDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("File %s does not exist. Use the %s tool to create new files before replacing content.", path, ToolNameCreateFile),
		}, nil
	}

	// Enforce read-before-write policy
	if a.session != nil && !a.session.WasFileRead(path) {
		return &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("File %s exists but was not read in this session. The LLM is trying to replace content without reading it first.", path),
			RequiresUserInput: true,
		}, nil
	}

	return &AuthorizationDecision{Allowed: true}, nil
}

// authorizeCreateFile ensures new files are created safely.
func (a *AuthorizationActor) authorizeCreateFile(ctx context.Context, params map[string]interface{}) (*AuthorizationDecision, error) {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &AuthorizationDecision{Allowed: false, Reason: "path is required"}, nil
	}

	if a.isPathPreauthorized(path) {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	if a.fs == nil {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	exists, err := a.fs.Exists(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("authorization failed while checking file existence: %w", err)
	}

	if exists {
		return &AuthorizationDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("File %s already exists. Use the write_file_diff tool to update existing files.", path),
		}, nil
	}

	return &AuthorizationDecision{Allowed: true}, nil
}

// authorizeSandboxDomain checks if a domain is authorized for network access in sandbox
func (a *AuthorizationActor) authorizeSandboxDomain(ctx context.Context, params map[string]interface{}) (*AuthorizationDecision, error) {
	domain := GetStringParam(params, "domain", "")
	if domain == "" {
		return &AuthorizationDecision{Allowed: false, Reason: "domain is required"}, nil
	}

	// Check if domain is already authorized
	if a.isDomainAuthorized(domain) {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	displayDomain := normalizeAuthorizationDomain(domain)
	if displayDomain == "" {
		displayDomain = domain
	}

	return a.judgeDomainWithLLM(ctx, displayDomain)
}

func (a *AuthorizationActor) authorizeWebFetch(ctx context.Context, params map[string]interface{}) (*AuthorizationDecision, error) {
	rawURL := GetStringParam(params, "url", "")
	if rawURL == "" {
		return &AuthorizationDecision{Allowed: false, Reason: "url is required"}, nil
	}

	parsed, err := normalizeFetchURL(rawURL)
	if err != nil {
		return &AuthorizationDecision{Allowed: false, Reason: fmt.Sprintf("invalid url: %v", err)}, nil
	}

	domain := parsed.Hostname()
	if domain == "" {
		return &AuthorizationDecision{Allowed: false, Reason: "url missing host"}, nil
	}

	return a.authorizeSandboxDomain(ctx, map[string]interface{}{"domain": domain})
}

// authorizeShell checks if a shell command is safe to execute
func (a *AuthorizationActor) authorizeShell(ctx context.Context, params map[string]interface{}) (*AuthorizationDecision, error) {
	command := GetStringParam(params, "command", "")
	if command == "" {
		return &AuthorizationDecision{Allowed: false, Reason: "command is required"}, nil
	}

	// Check if command is already authorized
	if a.isCommandAuthorized(command) {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	return a.judgeShellCommandWithLLM(ctx, command)
}

// judgeToolCallWithSecrets delegates tool call authorization decisions with detected secrets to the summarize client.
func (a *AuthorizationActor) judgeToolCallWithSecrets(ctx context.Context, toolName string, params map[string]interface{}, secrets []secretdetect.SecretMatch) (*AuthorizationDecision, error) {
	if a.summarizeClient == nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Tool %s contains detected secrets and requires user approval", toolName),
			RequiresUserInput: true,
		}
		return decision, nil
	}

	// Format the secrets for the prompt
	var secretDetails []string
	for _, secret := range secrets {
		secretDetails = append(secretDetails, fmt.Sprintf("- %s: %s (line %d)", secret.PatternName, secret.MatchedText, secret.LineNumber))
	}

	prompt := fmt.Sprintf(`You are a security analyzer for AI tool execution. Analyze a tool call that contains detected secrets and determine if it should be allowed or requires user approval.

Tool Name: %s
Parameters: %s

Detected Secrets:
%s

Consider the following when evaluating:
- Is this tool call attempting to use secrets in a legitimate way (e.g., API keys for authentication)?
- Are the secrets being used in read-only operations or potentially dangerous ones?
- Is the tool call part of normal development workflows (e.g., using API keys for external services)?
- Could this be an attempt to exfiltrate or misuse sensitive data?
- Are the secrets being exposed inappropriately (e.g., in logs, output to files)?

Common legitimate uses of secrets in tools:
- API keys for external services in web_fetch or API calls
- Authentication tokens for accessing private repositories or services
- Database credentials for data operations
- Deployment keys for infrastructure management

Potentially dangerous uses:
- Writing secrets to files that might be committed to version control
- Including secrets in logs or output
- Using secrets with destructive commands
- Exfiltrating secrets to external services

Respond with ONLY a JSON object in this exact format (no markdown, no code blocks):
{"allowed": true/false, "reason": "brief explanation"}

If "allowed" is true, the tool will execute with the secrets.
If "allowed" is false, the user will be prompted for approval.`, toolName, paramsToString(params), strings.Join(secretDetails, "\n"))

	response, err := a.summarizeClient.Complete(ctx, prompt)
	if err != nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Tool %s contains secrets and requires user approval (LLM analysis failed)", toolName),
			RequiresUserInput: true,
		}
		return decision, nil
	}

	var result struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}

	if err := llm.ParseLLMJSONResponse(response, &result); err != nil {
		decision := &AuthorizationDecision{
			Allowed:           false,
			Reason:            fmt.Sprintf("Tool %s contains secrets and requires user approval (failed to parse LLM response)", toolName),
			RequiresUserInput: true,
		}
		return decision, nil
	}

	if result.Allowed {
		decision := &AuthorizationDecision{Allowed: true}
		return decision, nil
	}

	reason := result.Reason
	if reason == "" {
		reason = fmt.Sprintf("Tool %s contains detected secrets and requires user approval", toolName)
	}

	decision := &AuthorizationDecision{
		Allowed:           false,
		Reason:            reason,
		RequiresUserInput: true,
	}
	return decision, nil
}
