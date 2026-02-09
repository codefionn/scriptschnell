// Package sandbox provides filesystem sandboxing capabilities using Linux Landlock.
// On Linux systems with Landlock support (kernel 5.13+), this restricts shell command
// execution to specific directories with configurable access permissions.
// On non-Linux systems or when Landlock is unavailable, operations proceed without sandboxing.
package sandbox

// AccessLevel represents the type of filesystem access granted to a path.
type AccessLevel int

const (
	// AccessReadOnly grants read-only access (read files, list directories)
	AccessReadOnly AccessLevel = iota
	// AccessReadWrite grants read and write access
	AccessReadWrite
	// AccessExecute grants execute access for binaries
	AccessExecute
)

// DirectoryPermission represents a directory path with its access level.
type DirectoryPermission struct {
	Path   string
	Access AccessLevel
}

// RequestedDirectory represents a directory request from the LLM that needs user authorization.
type RequestedDirectory struct {
	Path        string
	Access      AccessLevel
	Description string // Optional description of why access is needed
}

// AuthorizationDecision represents the user's decision on directory authorization.
type AuthorizationDecision int

const (
	// DecisionDenied denies the request for this session
	DecisionDenied AuthorizationDecision = iota
	// DecisionApprovedSession approves for this session only
	DecisionApprovedSession
	// DecisionApprovedWorkspace approves and persists for this workspace
	DecisionApprovedWorkspace
)

// AuthorizationResult contains the result of an authorization request.
type AuthorizationResult struct {
	Decision   AuthorizationDecision
	Path       string
	Access     AccessLevel
	Persistent bool // Whether this should be persisted to workspace config
}

// SandboxConfig holds configuration for additional sandbox paths.
// This is a simplified version of config.SandboxConfig to avoid import cycles.
type SandboxConfig struct {
	AdditionalReadOnlyPaths  []string
	AdditionalReadWritePaths []string
	DisableSandbox           bool
	BestEffort               bool
}
