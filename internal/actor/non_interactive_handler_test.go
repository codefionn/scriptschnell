package actor

import (
	"context"
	"testing"
)

func TestNonInteractiveHandlerMode(t *testing.T) {
	handler := NewNonInteractiveHandler(nil)
	if handler.Mode() != "non-interactive" {
		t.Errorf("Expected mode 'non-interactive', got '%s'", handler.Mode())
	}
}

func TestNonInteractiveHandlerSupportsInteraction(t *testing.T) {
	handler := NewNonInteractiveHandler(nil)

	// Should support authorization
	if !handler.SupportsInteraction(InteractionTypeAuthorization) {
		t.Error("Expected to support InteractionTypeAuthorization")
	}

	// Should not support other types
	if handler.SupportsInteraction(InteractionTypePlanningQuestion) {
		t.Error("Expected not to support InteractionTypePlanningQuestion")
	}
	if handler.SupportsInteraction(InteractionTypeUserInputSingle) {
		t.Error("Expected not to support InteractionTypeUserInputSingle")
	}
	if handler.SupportsInteraction(InteractionTypeUserInputMultiple) {
		t.Error("Expected not to support InteractionTypeUserInputMultiple")
	}
}

func TestNonInteractiveHandlerDangerouslyAllowAll(t *testing.T) {
	handler := NewNonInteractiveHandler(&NonInteractiveOptions{
		DangerouslyAllowAll: true,
	})

	ctx := context.Background()
	req := &UserInteractionRequest{
		RequestID:       "test-1",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName:   "shell",
			Parameters: map[string]interface{}{"command": "rm -rf /"},
			Reason:     "dangerous command",
		},
	}

	resp, err := handler.HandleInteraction(ctx, req)
	if err != nil {
		t.Fatalf("HandleInteraction failed: %v", err)
	}

	if !resp.Approved {
		t.Error("Expected Approved with DangerouslyAllowAll")
	}
	if !resp.Acknowledged {
		t.Error("Expected Acknowledged")
	}
}

func TestNonInteractiveHandlerAllowedDirs(t *testing.T) {
	handler := NewNonInteractiveHandler(&NonInteractiveOptions{
		AllowedDirs: []string{"/tmp/", "/home/user/projects/"},
	})

	ctx := context.Background()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"allowed dir", "/tmp/test.txt", true},
		{"allowed subdir", "/home/user/projects/foo/bar.go", true},
		{"not allowed", "/etc/passwd", false},
		{"partial match", "/tmpfoo/test.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &UserInteractionRequest{
				RequestID:       "test-" + tt.name,
				InteractionType: InteractionTypeAuthorization,
				Payload: &AuthorizationPayload{
					ToolName:   "create_file",
					Parameters: map[string]interface{}{"path": tt.path},
					Reason:     "write file",
				},
			}

			resp, err := handler.HandleInteraction(ctx, req)
			if err != nil {
				t.Fatalf("HandleInteraction failed: %v", err)
			}

			if resp.Approved != tt.expected {
				t.Errorf("Expected Approved=%v for path %s, got %v", tt.expected, tt.path, resp.Approved)
			}
		})
	}
}

func TestNonInteractiveHandlerAllowedFiles(t *testing.T) {
	handler := NewNonInteractiveHandler(&NonInteractiveOptions{
		AllowedFiles: []string{"/etc/hosts", "/var/log/app.log"},
	})

	ctx := context.Background()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"exact match hosts", "/etc/hosts", true},
		{"exact match log", "/var/log/app.log", true},
		{"not allowed", "/etc/passwd", false},
		{"partial match", "/etc/hosts.bak", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &UserInteractionRequest{
				RequestID:       "test-" + tt.name,
				InteractionType: InteractionTypeAuthorization,
				Payload: &AuthorizationPayload{
					ToolName:   "edit_file",
					Parameters: map[string]interface{}{"path": tt.path},
					Reason:     "edit file",
				},
			}

			resp, err := handler.HandleInteraction(ctx, req)
			if err != nil {
				t.Fatalf("HandleInteraction failed: %v", err)
			}

			if resp.Approved != tt.expected {
				t.Errorf("Expected Approved=%v for path %s, got %v", tt.expected, tt.path, resp.Approved)
			}
		})
	}
}

func TestNonInteractiveHandlerAllowedCommands(t *testing.T) {
	handler := NewNonInteractiveHandler(&NonInteractiveOptions{
		AllowedCommands: []string{"git ", "go ", "npm "},
	})

	ctx := context.Background()

	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		{"git command", "git status", true},
		{"go command", "go build ./...", true},
		{"npm command", "npm install", true},
		{"not allowed", "rm -rf /", false},
		{"partial match", "gitfoo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &UserInteractionRequest{
				RequestID:       "test-" + tt.name,
				InteractionType: InteractionTypeAuthorization,
				Payload: &AuthorizationPayload{
					ToolName:   "shell",
					Parameters: map[string]interface{}{"command": tt.command},
					Reason:     "run command",
				},
			}

			resp, err := handler.HandleInteraction(ctx, req)
			if err != nil {
				t.Fatalf("HandleInteraction failed: %v", err)
			}

			if resp.Approved != tt.expected {
				t.Errorf("Expected Approved=%v for command '%s', got %v", tt.expected, tt.command, resp.Approved)
			}
		})
	}
}

func TestNonInteractiveHandlerAllowedDomains(t *testing.T) {
	handler := NewNonInteractiveHandler(&NonInteractiveOptions{
		AllowedDomains: []string{"github.com", "*.google.com", "api.*"},
	})

	ctx := context.Background()

	tests := []struct {
		name     string
		domain   string
		expected bool
	}{
		{"exact match", "github.com", true},
		{"wildcard subdomain", "api.google.com", true},
		{"wildcard prefix", "api.example.com", true},
		{"not allowed", "evil.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &UserInteractionRequest{
				RequestID:       "test-" + tt.name,
				InteractionType: InteractionTypeAuthorization,
				Payload: &AuthorizationPayload{
					ToolName:     "network_access",
					Parameters:   map[string]interface{}{"domain": tt.domain},
					Reason:       "access domain",
					IsDomainAuth: true,
				},
			}

			resp, err := handler.HandleInteraction(ctx, req)
			if err != nil {
				t.Fatalf("HandleInteraction failed: %v", err)
			}

			if resp.Approved != tt.expected {
				t.Errorf("Expected Approved=%v for domain '%s', got %v", tt.expected, tt.domain, resp.Approved)
			}
		})
	}
}

func TestNonInteractiveHandlerAllowAllNetwork(t *testing.T) {
	handler := NewNonInteractiveHandler(&NonInteractiveOptions{
		AllowAllNetwork: true,
	})

	ctx := context.Background()

	// Domain authorization should pass
	req := &UserInteractionRequest{
		RequestID:       "test-network",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName:     "network_access",
			Parameters:   map[string]interface{}{"domain": "any-domain.com"},
			Reason:       "access domain",
			IsDomainAuth: true,
		},
	}

	resp, err := handler.HandleInteraction(ctx, req)
	if err != nil {
		t.Fatalf("HandleInteraction failed: %v", err)
	}

	if !resp.Approved {
		t.Error("Expected Approved with AllowAllNetwork for domain auth")
	}

	// web_search should also pass
	req2 := &UserInteractionRequest{
		RequestID:       "test-websearch",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName:   "web_search",
			Parameters: map[string]interface{}{"query": "test"},
			Reason:     "search",
		},
	}

	resp2, err := handler.HandleInteraction(ctx, req2)
	if err != nil {
		t.Fatalf("HandleInteraction failed: %v", err)
	}

	if !resp2.Approved {
		t.Error("Expected Approved with AllowAllNetwork for web_search")
	}
}

func TestNonInteractiveHandlerUnsupportedInteractionType(t *testing.T) {
	handler := NewNonInteractiveHandler(nil)

	ctx := context.Background()
	req := &UserInteractionRequest{
		RequestID:       "test-unsupported",
		InteractionType: InteractionTypeUserInputSingle,
		Payload: &UserInputSinglePayload{
			Question: "What is your name?",
		},
	}

	resp, err := handler.HandleInteraction(ctx, req)
	if err != nil {
		t.Fatalf("HandleInteraction failed: %v", err)
	}

	if resp.Approved {
		t.Error("Expected not Approved for unsupported type")
	}
	if resp.Error == nil {
		t.Error("Expected error for unsupported type")
	}
}

func TestNonInteractiveHandlerInvalidPayload(t *testing.T) {
	handler := NewNonInteractiveHandler(nil)

	ctx := context.Background()
	req := &UserInteractionRequest{
		RequestID:       "test-invalid",
		InteractionType: InteractionTypeAuthorization,
		Payload:         "invalid payload", // Wrong type
	}

	_, err := handler.HandleInteraction(ctx, req)
	if err == nil {
		t.Error("Expected error for invalid payload type")
	}
}

func TestNonInteractiveHandlerDefaultDeny(t *testing.T) {
	// Empty options - should deny everything not explicitly allowed
	handler := NewNonInteractiveHandler(&NonInteractiveOptions{})

	ctx := context.Background()
	req := &UserInteractionRequest{
		RequestID:       "test-default",
		InteractionType: InteractionTypeAuthorization,
		Payload: &AuthorizationPayload{
			ToolName:   "shell",
			Parameters: map[string]interface{}{"command": "echo hello"},
			Reason:     "test",
		},
	}

	resp, err := handler.HandleInteraction(ctx, req)
	if err != nil {
		t.Fatalf("HandleInteraction failed: %v", err)
	}

	if resp.Approved {
		t.Error("Expected default deny when no options match")
	}
}

func TestMatchesDomainPattern(t *testing.T) {
	tests := []struct {
		domain   string
		pattern  string
		expected bool
	}{
		// Exact match
		{"github.com", "github.com", true},
		{"api.github.com", "github.com", false},

		// Wildcard subdomain
		{"api.github.com", "*.github.com", true},
		{"www.api.github.com", "*.github.com", true},
		{"github.com", "*.github.com", false},

		// Prefix wildcard
		{"api.example.com", "api.*", true},
		{"api.test.org", "api.*", true},
		{"www.example.com", "api.*", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain+"_"+tt.pattern, func(t *testing.T) {
			result := matchesDomainPattern(tt.domain, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesDomainPattern(%s, %s) = %v, expected %v",
					tt.domain, tt.pattern, result, tt.expected)
			}
		})
	}
}
