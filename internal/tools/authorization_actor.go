package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/actor"
	"github.com/statcode-ai/statcode-ai/internal/fs"
	"github.com/statcode-ai/statcode-ai/internal/llm"
	"github.com/statcode-ai/statcode-ai/internal/session"
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
}

// AuthorizationActor handles policy decisions for tool calls in a centralized manner.
type AuthorizationActor struct {
	id              string
	fs              fs.FileSystem
	session         *session.Session
	summarizeClient llm.Client

	options         AuthorizationOptions
	allowedFiles    map[string]struct{}
	allowedDirs     []string
	allowedDomains  map[string]struct{}
	allowedCommands []string // Prefixes of commands that are pre-authorized
	workingDir      string
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
	if a.options.DangerouslyAllowAll {
		return &AuthorizationDecision{Allowed: true}, nil
	}

	switch toolName {
	case "write_file_diff":
		return a.authorizeWriteFileDiff(ctx, params)
	case "create_file":
		return a.authorizeCreateFile(ctx, params)
	case "go_sandbox_domain":
		return a.authorizeSandboxDomain(ctx, params)
	case "shell":
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
			Reason:  fmt.Sprintf("File %s does not exist. Use the create_file tool to create new files before applying diffs.", path),
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
