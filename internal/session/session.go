package session

import (
	"strings"
	"sync"
	"time"
)

// Message represents a conversation message
type Message struct {
	Role      string                   `json:"role"` // "user", "assistant", "tool"
	Content   string                   `json:"content"`
	ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolID    string                   `json:"tool_id,omitempty"`
	ToolName  string                   `json:"tool_name,omitempty"` // Name of the tool for tool responses
	Timestamp time.Time                `json:"timestamp"`
}

// Session manages a conversation session
type Session struct {
	ID                 string
	WorkingDir         string
	Messages           []*Message
	FilesRead          map[string]string // path -> content
	FilesModified      map[string]bool
	BackgroundJobs     map[string]*BackgroundJob
	AuthorizedDomains  map[string]bool // domain -> authorized (session-level)
	AuthorizedCommands []string        // command prefixes that are authorized (session-level)
	mu                 sync.RWMutex
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// BackgroundJob represents a running background process
type BackgroundJob struct {
	ID         string
	Command    string
	WorkingDir string
	StartTime  time.Time
	Completed  bool
	ExitCode   int
	Stdout     []string
	Stderr     []string
}

// NewSession creates a new session
func NewSession(id, workingDir string) *Session {
	return &Session{
		ID:                 id,
		WorkingDir:         workingDir,
		Messages:           make([]*Message, 0),
		FilesRead:          make(map[string]string),
		FilesModified:      make(map[string]bool),
		BackgroundJobs:     make(map[string]*BackgroundJob),
		AuthorizedDomains:  make(map[string]bool),
		AuthorizedCommands: make([]string, 0),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
}

// AddMessage adds a message to the session
func (s *Session) AddMessage(msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg.Timestamp = time.Now()
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
}

// GetMessages returns all messages
func (s *Session) GetMessages() []*Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy
	messages := make([]*Message, len(s.Messages))
	copy(messages, s.Messages)
	return messages
}

// TrackFileRead tracks that a file was read
func (s *Session) TrackFileRead(path, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FilesRead[path] = content
	s.UpdatedAt = time.Now()
}

// WasFileRead checks if a file was read in this session
func (s *Session) WasFileRead(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.FilesRead[path]
	return ok
}

// TrackFileModified tracks that a file was modified
func (s *Session) TrackFileModified(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FilesModified[path] = true
	s.UpdatedAt = time.Now()
}

// GetModifiedFiles returns list of modified files
func (s *Session) GetModifiedFiles() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	files := make([]string, 0, len(s.FilesModified))
	for path := range s.FilesModified {
		files = append(files, path)
	}
	return files
}

// AddBackgroundJob adds a background job
func (s *Session) AddBackgroundJob(job *BackgroundJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BackgroundJobs[job.ID] = job
	s.UpdatedAt = time.Now()
}

// GetBackgroundJob retrieves a background job
func (s *Session) GetBackgroundJob(id string) (*BackgroundJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.BackgroundJobs[id]
	return job, ok
}

// ListBackgroundJobs returns all background jobs
func (s *Session) ListBackgroundJobs() []*BackgroundJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]*BackgroundJob, 0, len(s.BackgroundJobs))
	for _, job := range s.BackgroundJobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// Clear clears the session but keeps working directory
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = make([]*Message, 0)
	s.FilesRead = make(map[string]string)
	s.FilesModified = make(map[string]bool)
	s.BackgroundJobs = make(map[string]*BackgroundJob)
	s.AuthorizedDomains = make(map[string]bool)
	s.AuthorizedCommands = make([]string, 0)
	s.UpdatedAt = time.Now()
}

// AuthorizeDomain marks a domain as authorized for network access
func (s *Session) AuthorizeDomain(domain string) {
	domain = normalizeSessionDomain(domain)
	if domain == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.AuthorizedDomains[domain] = true
	s.UpdatedAt = time.Now()
}

// IsDomainAuthorized checks if a domain is authorized for network access
func (s *Session) IsDomainAuthorized(domain string) bool {
	domain = normalizeSessionDomain(domain)
	if domain == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.AuthorizedDomains[domain] {
		return true
	}

	for pattern := range s.AuthorizedDomains {
		if matchesWildcardDomain(pattern, domain) {
			return true
		}
	}

	return false
}

// GetAuthorizedDomains returns a list of all authorized domains
func (s *Session) GetAuthorizedDomains() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	domains := make([]string, 0, len(s.AuthorizedDomains))
	for domain := range s.AuthorizedDomains {
		domains = append(domains, domain)
	}
	return domains
}

func normalizeSessionDomain(domain string) string {
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

// AuthorizeCommand adds a command prefix to the authorized list for this session
func (s *Session) AuthorizeCommand(commandPrefix string) {
	commandPrefix = strings.TrimSpace(commandPrefix)
	if commandPrefix == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already authorized
	for _, existing := range s.AuthorizedCommands {
		if existing == commandPrefix {
			return
		}
	}

	s.AuthorizedCommands = append(s.AuthorizedCommands, commandPrefix)
	s.UpdatedAt = time.Now()
}

// IsCommandAuthorized checks if a command is authorized based on session-level authorized prefixes
func (s *Session) IsCommandAuthorized(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, prefix := range s.AuthorizedCommands {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}

	return false
}

// GetAuthorizedCommands returns a list of all authorized command prefixes
func (s *Session) GetAuthorizedCommands() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	commands := make([]string, len(s.AuthorizedCommands))
	copy(commands, s.AuthorizedCommands)
	return commands
}

// CompactWithSummary replaces the oldest messages with a summary message.
// The original slice must correspond to the current head of the session when the
// compaction is applied; otherwise it will no-op and return false.
func (s *Session) CompactWithSummary(original []*Message, summary string) bool {
	if len(original) == 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Messages) < len(original) {
		return false
	}

	for i := range original {
		if s.Messages[i] != original[i] {
			return false
		}
	}

	summaryMsg := &Message{
		Role:      "system",
		Content:   summary,
		Timestamp: time.Now(),
	}

	newMessages := make([]*Message, 0, len(s.Messages)-len(original)+1)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, s.Messages[len(original):]...)
	s.Messages = newMessages
	s.UpdatedAt = time.Now()

	return true
}
