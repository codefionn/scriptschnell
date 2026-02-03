package actor

import (
	"context"
	"fmt"
	"strings"

	"github.com/codefionn/scriptschnell/internal/logger"
)

// NonInteractiveOptions configures the non-interactive handler behavior
type NonInteractiveOptions struct {
	// DangerouslyAllowAll auto-approves all authorization requests
	DangerouslyAllowAll bool
	// AllowedCommands are command prefixes that are pre-authorized
	AllowedCommands []string
	// AllowedDomains are domain patterns that are pre-authorized for network access
	AllowedDomains []string
	// AllowedDirs are directory paths that are pre-authorized for write operations
	AllowedDirs []string
	// AllowedFiles are specific file paths that are pre-authorized for write operations
	AllowedFiles []string
	// AllowAllNetwork auto-approves all network operations
	AllowAllNetwork bool
	// RequireSandboxAuth requires authorization for every go_sandbox and shell call
	RequireSandboxAuth bool
}

// NonInteractiveHandler handles user interactions in non-interactive mode (CLI).
// It auto-responds based on pre-configured rules without user input.
type NonInteractiveHandler struct {
	opts *NonInteractiveOptions
}

// NewNonInteractiveHandler creates a new non-interactive handler
func NewNonInteractiveHandler(opts *NonInteractiveOptions) *NonInteractiveHandler {
	if opts == nil {
		opts = &NonInteractiveOptions{}
	}
	return &NonInteractiveHandler{opts: opts}
}

// Mode returns the handler mode name
func (h *NonInteractiveHandler) Mode() string {
	return "non-interactive"
}

// SupportsInteraction indicates whether this handler can handle the given type.
// Non-interactive mode only handles authorization requests.
func (h *NonInteractiveHandler) SupportsInteraction(interactionType InteractionType) bool {
	return interactionType == InteractionTypeAuthorization
}

// HandleInteraction processes a user interaction request
func (h *NonInteractiveHandler) HandleInteraction(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
	switch req.InteractionType {
	case InteractionTypeAuthorization:
		return h.handleAuthorization(ctx, req)
	default:
		logger.Debug("NonInteractiveHandler: unsupported interaction type %s", req.InteractionType)
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     false,
			Acknowledged: true,
			Error:        fmt.Errorf("non-interactive mode cannot handle %s interactions", req.InteractionType),
		}, nil
	}
}

// handleAuthorization handles authorization requests
func (h *NonInteractiveHandler) handleAuthorization(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
	payload, ok := req.Payload.(*AuthorizationPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload type for authorization: expected *AuthorizationPayload, got %T", req.Payload)
	}

	// If DangerouslyAllowAll is set, auto-approve everything
	if h.opts.DangerouslyAllowAll {
		logger.Debug("NonInteractiveHandler: auto-approved %s (DangerouslyAllowAll)", payload.ToolName)
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     true,
			Acknowledged: true,
		}, nil
	}

	// Check domain authorization
	if payload.IsDomainAuth {
		return h.handleDomainAuthorization(req.RequestID, payload)
	}

	// Check command/tool authorization
	return h.handleToolAuthorization(req.RequestID, payload)
}

// handleDomainAuthorization handles network domain authorization
func (h *NonInteractiveHandler) handleDomainAuthorization(requestID string, payload *AuthorizationPayload) (*UserInteractionResponse, error) {
	// Get domain from parameters
	domain, _ := payload.Parameters["domain"].(string)
	if domain == "" {
		domain = payload.SuggestedDomain
	}

	// If AllowAllNetwork is set, auto-approve
	if h.opts.AllowAllNetwork {
		logger.Debug("NonInteractiveHandler: auto-approved domain %s (AllowAllNetwork)", domain)
		return &UserInteractionResponse{
			RequestID:    requestID,
			Approved:     true,
			Acknowledged: true,
		}, nil
	}

	// Check allowed domains
	for _, allowed := range h.opts.AllowedDomains {
		if matchesDomainPattern(domain, allowed) {
			logger.Debug("NonInteractiveHandler: domain %s matched allowed pattern %s", domain, allowed)
			return &UserInteractionResponse{
				RequestID:    requestID,
				Approved:     true,
				Acknowledged: true,
			}, nil
		}
	}

	logger.Debug("NonInteractiveHandler: domain %s not in allowed list, denying", domain)
	return &UserInteractionResponse{
		RequestID:    requestID,
		Approved:     false,
		Acknowledged: true,
		Error:        fmt.Errorf("domain '%s' not pre-authorized for non-interactive mode", domain),
	}, nil
}

// handleToolAuthorization handles tool/command authorization
func (h *NonInteractiveHandler) handleToolAuthorization(requestID string, payload *AuthorizationPayload) (*UserInteractionResponse, error) {
	toolName := payload.ToolName

	// Check file write operations
	if toolName == "create_file" || toolName == "edit_file" || toolName == "write_file_replace" {
		var filePath string
		if v, ok := payload.Parameters["path"].(string); ok {
			filePath = v
		} else if v, ok := payload.Parameters["file_path"].(string); ok {
			filePath = v
		}

		if filePath != "" {
			// Check allowed directories
			for _, allowedDir := range h.opts.AllowedDirs {
				if strings.HasPrefix(filePath, allowedDir) {
					logger.Debug("NonInteractiveHandler: file %s in allowed dir %s", filePath, allowedDir)
					return &UserInteractionResponse{
						RequestID:    requestID,
						Approved:     true,
						Acknowledged: true,
					}, nil
				}
			}

			// Check allowed files
			for _, allowedFile := range h.opts.AllowedFiles {
				if filePath == allowedFile {
					logger.Debug("NonInteractiveHandler: file %s is explicitly allowed", filePath)
					return &UserInteractionResponse{
						RequestID:    requestID,
						Approved:     true,
						Acknowledged: true,
					}, nil
				}
			}
		}
	}

	// Check go_sandbox tool
	if toolName == "go_sandbox" {
		// If RequireSandboxAuth is set, deny go_sandbox in non-interactive mode
		if h.opts.RequireSandboxAuth {
			logger.Debug("NonInteractiveHandler: go_sandbox requires authorization but running in non-interactive mode")
			return &UserInteractionResponse{
				RequestID:    requestID,
				Approved:     false,
				Acknowledged: true,
				Error:        fmt.Errorf("go_sandbox requires authorization (--require-sandbox-auth flag is set, but non-interactive mode cannot prompt for approval)"),
			}, nil
		}
		// Otherwise, allow go_sandbox in non-interactive mode
		return &UserInteractionResponse{
			RequestID:    requestID,
			Approved:     true,
			Acknowledged: true,
		}, nil
	}

	// Check shell commands
	if toolName == "shell" || toolName == "command" {
		var command string
		if v, ok := payload.Parameters["command"].(string); ok {
			command = v
		}

		// If RequireSandboxAuth is set, deny shell in non-interactive mode
		if h.opts.RequireSandboxAuth && command != "" {
			logger.Debug("NonInteractiveHandler: shell command '%s' requires authorization but running in non-interactive mode", command)
			return &UserInteractionResponse{
				RequestID:    requestID,
				Approved:     false,
				Acknowledged: true,
				Error:        fmt.Errorf("shell command requires authorization (--require-sandbox-auth flag is set, but non-interactive mode cannot prompt for approval)"),
			}, nil
		}

		if command != "" {
			for _, allowedCmd := range h.opts.AllowedCommands {
				if strings.HasPrefix(command, allowedCmd) {
					logger.Debug("NonInteractiveHandler: command '%s' matches allowed prefix '%s'", command, allowedCmd)
					return &UserInteractionResponse{
						RequestID:    requestID,
						Approved:     true,
						Acknowledged: true,
					}, nil
				}
			}
		}
	}

	// Check web search
	if toolName == "web_search" || toolName == "web_fetch" {
		if h.opts.AllowAllNetwork {
			return &UserInteractionResponse{
				RequestID:    requestID,
				Approved:     true,
				Acknowledged: true,
			}, nil
		}
	}

	// Default: deny
	logger.Debug("NonInteractiveHandler: %s not authorized in non-interactive mode", toolName)
	return &UserInteractionResponse{
		RequestID:    requestID,
		Approved:     false,
		Acknowledged: true,
		Error:        fmt.Errorf("tool '%s' requires authorization but not granted via CLI flags", toolName),
	}, nil
}

// matchesDomainPattern checks if a domain matches a pattern (supports wildcards)
func matchesDomainPattern(domain, pattern string) bool {
	// Direct match
	if domain == pattern {
		return true
	}

	// Wildcard patterns like *.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // Remove the * but keep the .
		if strings.HasSuffix(domain, suffix) {
			return true
		}
	}

	// Prefix wildcard like example.*
	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-2]
		if strings.HasPrefix(domain, prefix) {
			return true
		}
	}

	return false
}
