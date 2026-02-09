package tools

import (
	"context"
	"fmt"

	"github.com/codefionn/scriptschnell/internal/sandbox"
)

const ToolNameRequestDirectoryAccess = "request_directory_access"

// RequestDirectoryAccessToolSpec is the static specification for the request_directory_access tool
type RequestDirectoryAccessToolSpec struct{}

func (s *RequestDirectoryAccessToolSpec) Name() string {
	return ToolNameRequestDirectoryAccess
}

func (s *RequestDirectoryAccessToolSpec) Description() string {
	return `Request access to an additional directory for sandboxed shell execution.

Shell commands are sandboxed using Linux Landlock for security. By default, they can only
access the workspace directory and common package manager directories (go, npm, cargo, pip, etc.).

Use this tool when you need to:
- Read files from directories outside the workspace
- Write files to directories outside the workspace
- Access custom project directories or caches

The user will be prompted to authorize the request with three options:
1. Approve for this session - Access granted until the session ends
2. Approve for this workspace - Access granted and saved for future sessions
3. Deny - Access not granted

Parameters:
- path: The absolute or relative directory path to access
- access_level: 'read' for read-only, 'readwrite' for full access
- description: Brief explanation of why access is needed (helps user decide)

Returns the authorization decision. If denied, you cannot access that directory.`
}

func (s *RequestDirectoryAccessToolSpec) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The directory path to request access to",
			},
			"access_level": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"read", "readwrite"},
				"description": "The access level needed: 'read' for read-only, 'readwrite' for full access",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Optional description of why access is needed",
			},
		},
		"required": []string{"path", "access_level"},
	}
}

// RequestDirectoryAccessTool is the executor with runtime dependencies
type RequestDirectoryAccessTool struct {
	sandboxManager *sandbox.Manager
}

// NewRequestDirectoryAccessTool creates a new request_directory_access tool
func NewRequestDirectoryAccessTool(sb *sandbox.Manager) *RequestDirectoryAccessTool {
	return &RequestDirectoryAccessTool{
		sandboxManager: sb,
	}
}

// Legacy interface implementation
func (t *RequestDirectoryAccessTool) Name() string {
	return ToolNameRequestDirectoryAccess
}

func (t *RequestDirectoryAccessTool) Description() string {
	return (&RequestDirectoryAccessToolSpec{}).Description()
}

func (t *RequestDirectoryAccessTool) Parameters() map[string]interface{} {
	return (&RequestDirectoryAccessToolSpec{}).Parameters()
}

func (t *RequestDirectoryAccessTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	path := GetStringParam(params, "path", "")
	if path == "" {
		return &ToolResult{Error: "path is required"}
	}

	accessLevelStr := GetStringParam(params, "access_level", "read")
	description := GetStringParam(params, "description", "")

	var accessLevel sandbox.AccessLevel
	switch accessLevelStr {
	case "read":
		accessLevel = sandbox.AccessReadOnly
	case "readwrite":
		accessLevel = sandbox.AccessReadWrite
	default:
		return &ToolResult{Error: fmt.Sprintf("invalid access_level: %s (must be 'read' or 'readwrite')", accessLevelStr)}
	}

	if t.sandboxManager == nil {
		return &ToolResult{Error: "sandbox manager not configured"}
	}

	// Request access - this will trigger authorization if needed
	decision := t.sandboxManager.RequestPathAccess(path, accessLevel, description)

	var decisionStr string
	switch decision {
	case sandbox.DecisionDenied:
		decisionStr = "denied"
	case sandbox.DecisionApprovedSession:
		decisionStr = "approved for this session"
	case sandbox.DecisionApprovedWorkspace:
		decisionStr = "approved and saved for this workspace"
	}

	result := map[string]interface{}{
		"path":         path,
		"access_level": accessLevelStr,
		"decision":     decisionStr,
		"approved":     decision != sandbox.DecisionDenied,
	}

	if decision == sandbox.DecisionDenied {
		return &ToolResult{
			Result: result,
			Error:  "directory access request denied by user",
		}
	}

	return &ToolResult{
		Result: result,
	}
}

// RequestDirectoryAccessToolFactory creates a factory for RequestDirectoryAccessTool
func RequestDirectoryAccessToolFactory(sb *sandbox.Manager) ToolFactory {
	return func(reg *Registry) ToolExecutor {
		return NewRequestDirectoryAccessTool(sb)
	}
}
