package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/fs"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestAuthorizationActorAuthorizeCreateFileNewFileAllowed(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameCreateFile, map[string]interface{}{"path": "new.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if !decision.Allowed {
		t.Fatalf("expected create_file to be allowed for new file")
	}
	if decision.RequiresUserInput {
		t.Fatalf("new file should not require user input")
	}
}

func TestAuthorizationActorAuthorizeCreateFileExistingDenied(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	if err := mockFS.WriteFile(ctx, "existing.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	decision, err := actor.authorize(ctx, ToolNameCreateFile, map[string]interface{}{"path": "existing.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected create_file to be denied for existing file")
	}
	if decision.Reason == "" {
		t.Fatalf("expected denial reason for existing file")
	}
}

func TestAuthorizationActorAuthorizeWriteFileDiffExistingNotRead(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	if err := mockFS.WriteFile(ctx, "existing.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	decision, err := actor.authorize(ctx, ToolNameEditFile, map[string]interface{}{"path": "existing.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected diff write to be blocked when not read")
	}
	if !decision.RequiresUserInput {
		t.Fatalf("expected existing unread file to require user input")
	}
	if decision.Reason == "" {
		t.Fatalf("expected reason explaining denial")
	}
}

func TestAuthorizationActorAuthorizeWriteFileDiffMissingFileDenied(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameEditFile, map[string]interface{}{"path": "missing.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected diff write to be denied for missing file")
	}
	if decision.Reason == "" {
		t.Fatalf("expected denial reason for missing file")
	}
}

func TestAuthorizationActorAuthorizeWriteFileDiffPreauthorizedFile(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	if err := mockFS.WriteFile(ctx, "allowed.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	opts := &AuthorizationOptions{
		AllowedFiles: []string{"allowed.txt"},
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	decision, err := actor.authorize(ctx, ToolNameEditFile, map[string]interface{}{"path": "allowed.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected preauthorized file write to be allowed")
	}
	if decision.RequiresUserInput {
		t.Fatalf("preauthorized file write should not require user input")
	}
}

func TestAuthorizationActorAuthorizeWriteFileDiffPreauthorizedDir(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	if err := mockFS.WriteFile(ctx, "outside/data.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	opts := &AuthorizationOptions{
		AllowedDirs: []string{"outside"},
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	decision, err := actor.authorize(ctx, ToolNameEditFile, map[string]interface{}{"path": "outside/data.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected preauthorized directory write to be allowed")
	}
	if decision.RequiresUserInput {
		t.Fatalf("preauthorized directory write should not require user input")
	}
}

func TestAuthorizationActorAuthorizeWriteFileDiffExistingReadAllowed(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	if err := mockFS.WriteFile(ctx, "existing.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	sess.TrackFileRead("existing.txt", "content")

	decision, err := actor.authorize(ctx, ToolNameEditFile, map[string]interface{}{"path": "existing.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if !decision.Allowed {
		t.Fatalf("expected write to be allowed after file was read")
	}
	if decision.RequiresUserInput {
		t.Fatalf("no user input should be required once file was read")
	}
}

func TestAuthorizationActorAuthorizeSandboxDomainRequiresApproval(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameGoSandboxDomain, map[string]interface{}{"domain": "example.com"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected new domain access to be denied pending approval")
	}
	if !decision.RequiresUserInput {
		t.Fatalf("expected domain access to require user input")
	}
	if decision.Reason == "" {
		t.Fatalf("expected reason explaining the authorization requirement")
	}
}

func TestAuthorizationActorAuthorizeSandboxDomainSessionAuthorized(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	sess.AuthorizeDomain("example.com")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameGoSandboxDomain, map[string]interface{}{"domain": "example.com"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if !decision.Allowed {
		t.Fatalf("expected authorized domain to be allowed")
	}
	if decision.RequiresUserInput {
		t.Fatalf("authorized domain should not require user input")
	}
}

func TestAuthorizationActorAuthorizeSandboxDomainAllowedByOptions(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	opts := &AuthorizationOptions{
		AllowedDomains: []string{"example.com"},
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	decision, err := actor.authorize(ctx, ToolNameGoSandboxDomain, map[string]interface{}{"domain": "example.com"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected domain allowed by options to be authorized")
	}
	if decision.RequiresUserInput {
		t.Fatalf("preauthorized domain should not require user input")
	}
}

func TestAuthorizationActorAuthorizeReplaceFileExistingNotRead(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	if err := mockFS.WriteFile(ctx, "existing.txt", []byte("original content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	decision, err := actor.authorize(ctx, ToolNameReplaceFile, map[string]interface{}{
		"path":    "existing.txt",
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected replace_file to be blocked when file not read")
	}
	if !decision.RequiresUserInput {
		t.Fatalf("expected existing unread file to require user input")
	}
	if decision.Reason == "" {
		t.Fatalf("expected reason explaining denial")
	}
}

func TestAuthorizationActorAuthorizeReplaceFileMissingFileDenied(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameReplaceFile, map[string]interface{}{
		"path":    "missing.txt",
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected replace_file to be denied for missing file")
	}
	if decision.Reason == "" {
		t.Fatalf("expected denial reason for missing file")
	}
	// Should mention create_file in the error
	if !strings.Contains(decision.Reason, "create_file") {
		t.Fatalf("expected reason to mention create_file, got: %s", decision.Reason)
	}
}

func TestAuthorizationActorAuthorizeReplaceFileExistingReadAllowed(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	if err := mockFS.WriteFile(ctx, "existing.txt", []byte("original content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	sess.TrackFileRead("existing.txt", "original content")

	decision, err := actor.authorize(ctx, ToolNameReplaceFile, map[string]interface{}{
		"path":    "existing.txt",
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if !decision.Allowed {
		t.Fatalf("expected replace_file to be allowed after file was read")
	}
	if decision.RequiresUserInput {
		t.Fatalf("no user input should be required once file was read")
	}
}

func TestAuthorizationActorAuthorizeReplaceFilePreauthorizedFile(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	if err := mockFS.WriteFile(ctx, "allowed.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	opts := &AuthorizationOptions{
		AllowedFiles: []string{"allowed.txt"},
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	decision, err := actor.authorize(ctx, ToolNameReplaceFile, map[string]interface{}{
		"path":    "allowed.txt",
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected preauthorized file replace to be allowed")
	}
	if decision.RequiresUserInput {
		t.Fatalf("preauthorized file replace should not require user input")
	}
}

func TestAuthorizationActorAuthorizeReplaceFileMissingPath(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameReplaceFile, map[string]interface{}{
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected replace_file to be denied for missing path")
	}
	if decision.Reason != "path is required" {
		t.Fatalf("expected 'path is required' error, got: %s", decision.Reason)
	}
}

func TestAuthorizationActorAuthorizeReplaceFilePreauthorizedDir(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	if err := mockFS.WriteFile(ctx, "outside/data.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	opts := &AuthorizationOptions{
		AllowedDirs: []string{"outside"},
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	decision, err := actor.authorize(ctx, ToolNameReplaceFile, map[string]interface{}{
		"path":    "outside/data.txt",
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected preauthorized directory replace to be allowed")
	}
	if decision.RequiresUserInput {
		t.Fatalf("preauthorized directory replace should not require user input")
	}
}

func TestAuthorizationActorAuthorizeSandboxDomainWildcard(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	opts := &AuthorizationOptions{
		AllowedDomains: []string{"*.example.com"},
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	decision, err := actor.authorize(ctx, ToolNameGoSandboxDomain, map[string]interface{}{"domain": "api.example.com"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected wildcard domain to be authorized")
	}
}

func TestAuthorizationActorAuthorizeSandboxDomainAllowAllNetwork(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	opts := &AuthorizationOptions{
		AllowAllNetwork: true,
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	decision, err := actor.authorize(ctx, ToolNameGoSandboxDomain, map[string]interface{}{"domain": "unlisted.example"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected allow-all-network to authorize domain")
	}
}

func TestAuthorizationActorAuthorizeWebFetchRequiresApproval(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameWebFetch, map[string]interface{}{"url": "https://example.com/docs"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected domain access to require approval for new domain")
	}
	if !decision.RequiresUserInput {
		t.Fatalf("expected decision to require user input")
	}
}

func TestAuthorizationActorDangerouslyAllowAllBypassesChecks(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")

	if err := mockFS.WriteFile(ctx, "existing.txt", []byte("content")); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	opts := &AuthorizationOptions{
		DangerouslyAllowAll: true,
	}
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, opts)

	// Should allow writing without prior read
	decision, err := actor.authorize(ctx, ToolNameEditFile, map[string]interface{}{"path": "existing.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected dangerous allow-all to permit diff write")
	}

	decision, err = actor.authorize(ctx, ToolNameCreateFile, map[string]interface{}{"path": "new.txt"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected dangerous allow-all to permit create_file")
	}

	// Should allow network domain automatically
	decision, err = actor.authorize(ctx, ToolNameGoSandboxDomain, map[string]interface{}{"domain": "example.com"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected dangerous allow-all to permit domain access")
	}
}

func TestAuthorizationActorReceiveUnknownMessage(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	err := actor.Receive(ctx, dummyMessage{})
	if err == nil {
		t.Fatalf("expected error on unknown message type")
	}
}

func TestAuthorizationActorAuthorizeAddContextDirectoryRequiresApproval(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameAddContextDirectory, map[string]interface{}{"directory": "/some/path"})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected add_context_directory to require user approval")
	}
	if !decision.RequiresUserInput {
		t.Fatalf("expected add_context_directory to require user input")
	}
	if !strings.Contains(decision.Reason, "/some/path") {
		t.Fatalf("expected reason to contain directory path, got: %s", decision.Reason)
	}
}

func TestAuthorizationActorAuthorizeAddContextDirectoryWithReason(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameAddContextDirectory, map[string]interface{}{
		"directory": "/docs/go",
		"reason":    "Need Go documentation for reference",
	})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected add_context_directory to require user approval")
	}
	if !decision.RequiresUserInput {
		t.Fatalf("expected add_context_directory to require user input")
	}
	if !strings.Contains(decision.Reason, "/docs/go") {
		t.Fatalf("expected reason to contain directory path, got: %s", decision.Reason)
	}
	if !strings.Contains(decision.Reason, "Need Go documentation for reference") {
		t.Fatalf("expected reason to contain the provided reason, got: %s", decision.Reason)
	}
}

func TestAuthorizationActorAuthorizeAddContextDirectoryMissingDirectory(t *testing.T) {
	ctx := context.Background()
	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actor := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	decision, err := actor.authorize(ctx, ToolNameAddContextDirectory, map[string]interface{}{})
	if err != nil {
		t.Fatalf("authorize returned error: %v", err)
	}
	if decision == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Allowed {
		t.Fatalf("expected add_context_directory without directory to be denied")
	}
	if decision.RequiresUserInput {
		t.Fatalf("expected missing directory to not require user input (just deny)")
	}
}

type dummyMessage struct{}

func (dummyMessage) Type() string { return "dummy" }

func TestAuthorizationActorClientAuthorize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockFS := fs.NewMockFS()
	sess := session.NewSession("test", ".")
	actorInstance := NewAuthorizationActor("auth", mockFS, sess, nil, nil)

	ref := actor.NewActorRef("auth", actorInstance, 4)
	if err := ref.Start(ctx); err != nil {
		t.Fatalf("failed to start actor ref: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		if err := ref.Stop(stopCtx); err != nil {
			t.Fatalf("failed to stop actor ref: %v", err)
		}
	}()

	client := NewAuthorizationActorClient(ref)
	decision, err := client.Authorize(context.Background(), ToolNameCreateFile, map[string]interface{}{"path": "new.txt"})
	if err != nil {
		t.Fatalf("client authorize returned error: %v", err)
	}
	if decision == nil || !decision.Allowed {
		t.Fatalf("expected authorization to allow creating new file")
	}
}
