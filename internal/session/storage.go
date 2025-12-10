package session

import (
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
)

// Storage format version for forward compatibility
const (
	SessionStorageVersion = 1
)

func init() {
	// Register all types for gob encoding
	gob.Register(&StoredSession{})
	gob.Register(&StoredMessage{})
	gob.Register(&StoredBackgroundJob{})
	gob.Register(map[string]string{})
	gob.Register(map[string]bool{})
	gob.Register([]string{})
	gob.Register(map[string]interface{}{})
	gob.Register([]map[string]interface{}{})
	gob.Register([]interface{}{})
}

// StoredMessage represents a message in storage format
type StoredMessage struct {
	Role              string
	Content           string
	ToolCalls         []map[string]interface{}
	ToolID            string
	ToolName          string
	Timestamp         time.Time
	NativeFormat      interface{}
	NativeProvider    string
	NativeModelFamily string
	NativeTimestamp   time.Time
}

// StoredBackgroundJob represents a background job in storage format
type StoredBackgroundJob struct {
	ID         string
	Command    string
	WorkingDir string
	StartTime  time.Time
	Completed  bool
	ExitCode   int
	Stdout     []string
	Stderr     []string
	Type       string
	LastSignal string
	// Note: Process, PID, ProcessGroupID, CancelFunc, StopRequested, Done, Mu are not serialized
	// as they are runtime-specific and not relevant for session persistence
}

// StoredSession represents a session in storage format
type StoredSession struct {
	Version             int
	ID                  string
	WorkingDir          string
	Name                string // Optional human-readable name (deprecated, use Title)
	Title               string // Auto-generated title for the session
	Messages            []*StoredMessage
	FilesRead           map[string]string
	FilesModified       map[string]bool
	BackgroundJobs      map[string]*StoredBackgroundJob
	AuthorizedDomains   map[string]bool
	AuthorizedCommands  []string
	PlanningActive      bool
	PlanningObjective   string
	LastSandboxExitCode int
	LastSandboxStdout   string
	LastSandboxStderr   string
	CurrentProvider     string
	CurrentModelFamily  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	LastSavedAt         time.Time
}

// SessionMetadata contains lightweight session information for listing
type SessionMetadata struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"` // Deprecated, use Title
	Title        string    `json:"title"`
	WorkingDir   string    `json:"working_dir"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

// SessionStorage manages session persistence
type SessionStorage struct {
	baseDir string

	// Auto-save configuration
	autoSave       bool
	saveInterval   time.Duration
	mu             sync.RWMutex
	autoSaveActive bool
	stopChan       chan struct{}
	wg             sync.WaitGroup
	activeSaves    int
	maxSaves       int
	configFunc     func() *config.AutoSaveConfig
}

// NewSessionStorage creates a new SessionStorage instance
func NewSessionStorage() (*SessionStorage, error) {
	dir, err := getSessionStorageDir()
	if err != nil {
		return nil, err
	}

	// Ensure base directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session storage directory: %w", err)
	}

	return &SessionStorage{baseDir: dir}, nil
}

// NewSessionStorageWithConfig creates a new SessionStorage instance with auto-save configuration
func NewSessionStorageWithConfig(configFunc func() *config.AutoSaveConfig) (*SessionStorage, error) {
	storage, err := NewSessionStorage()
	if err != nil {
		return nil, err
	}

	// Set up auto-save configuration
	cfg := configFunc()
	storage.configFunc = configFunc
	storage.autoSave = cfg.Enabled
	storage.saveInterval = time.Duration(cfg.SaveIntervalSeconds) * time.Second
	storage.maxSaves = cfg.MaxConcurrentSaves
	if storage.maxSaves == 0 {
		storage.maxSaves = 1
	}
	storage.stopChan = make(chan struct{})

	return storage, nil
}

// GetSessionStorageDir returns the platform-specific session storage directory (public API)
func GetSessionStorageDir() (string, error) {
	return getSessionStorageDir()
}

// getSessionStorageDir returns the platform-specific session storage directory
func getSessionStorageDir() (string, error) {
	switch runtime.GOOS {
	case "linux":
		// Use XDG_STATE_HOME if available, otherwise ~/.local/state
		if stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); stateHome != "" {
			return filepath.Join(stateHome, "scriptschnell", "sessions"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".local", "state", "scriptschnell", "sessions"), nil
	case "windows":
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "scriptschnell", "sessions"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, "AppData", "Local", "scriptschnell", "sessions"), nil
	default:
		// macOS and other Unix-like systems
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".config", "scriptschnell", "sessions"), nil
	}
}

// hashWorkspace creates a safe directory name from workspace path
func hashWorkspace(workingDir string) string {
	// Convert to absolute path and normalize
	absPath := workingDir
	if !filepath.IsAbs(workingDir) {
		if abs, err := filepath.Abs(workingDir); err == nil {
			absPath = abs
		}
	}
	absPath = filepath.Clean(absPath)

	// Create SHA256 hash of the path
	hash := sha256.Sum256([]byte(absPath))
	return fmt.Sprintf("%x", hash)[:16] // Use first 16 characters for readability
}

// getWorkspaceDir returns the directory for a specific workspace
func (s *SessionStorage) getWorkspaceDir(workingDir string) string {
	workspaceHash := hashWorkspace(workingDir)
	return filepath.Join(s.baseDir, workspaceHash)
}

// getSessionPath returns the file path for a specific session
func (s *SessionStorage) getSessionPath(workingDir, sessionID string) string {
	workspaceDir := s.getWorkspaceDir(workingDir)
	return filepath.Join(workspaceDir, sessionID+".gob")
}

// sanitizeSessionID produces a filesystem-safe session ID and falls back to a timestamp if needed.
// The session ID is the single source of truth for the filename; we do not
// derive it from the display name to avoid mismatches.
func sanitizeSessionID(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		id = fmt.Sprintf("session-%d", time.Now().Unix())
	}

	id = strings.ReplaceAll(id, string(os.PathSeparator), "-")
	nonAlnum := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	id = nonAlnum.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")

	if id == "" {
		id = fmt.Sprintf("session-%d", time.Now().Unix())
	}

	return id
}

// SaveSession saves a session to disk
func (s *SessionStorage) SaveSession(session *Session, name string) error {
	logger.Debug("SaveSession: starting save for session %s to workspace %s", session.ID, session.WorkingDir)

	// Skip if there is nothing new to persist
	if !session.IsDirty() {
		logger.Debug("SaveSession: session %s is already persisted; skipping", session.ID)
		return nil
	}

	// Skip saving sessions with no messages
	messages := session.GetMessages()
	if len(messages) == 0 {
		logger.Debug("SaveSession: session %s has no messages; skipping save", session.ID)
		return nil
	}

	// Ensure workspace directory exists
	workspaceDir := s.getWorkspaceDir(session.WorkingDir)
	logger.Debug("SaveSession: workspace dir: %s", workspaceDir)

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		logger.Error("SaveSession: failed to create directory %s: %v", workspaceDir, err)
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}
	logger.Debug("SaveSession: directory created successfully")

	// Convert to storage format
	stored := s.ToStoredSession(session, name)
	logger.Debug("SaveSession: converted to storage format")

	// Ensure we have a safe, non-empty session ID for the filename
	storedID := sanitizeSessionID(stored.ID)
	if storedID != stored.ID {
		logger.Warn("SaveSession: normalized session ID from '%s' to '%s' for storage", stored.ID, storedID)
		stored.ID = storedID
		// Keep the live session in sync so future saves use the normalized ID
		session.ID = storedID
	}

	// Write to temporary file first (atomic write)
	tempPath := s.getSessionPath(session.WorkingDir, storedID) + ".tmp"
	logger.Debug("SaveSession: creating temp file: %s", tempPath)

	file, err := os.Create(tempPath)
	if err != nil {
		logger.Error("SaveSession: failed to create temp file %s: %v", tempPath, err)
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	// Encode with gob
	logger.Debug("SaveSession: encoding session with gob")
	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(stored); err != nil {
		file.Close()
		os.Remove(tempPath)
		logger.Error("SaveSession: failed to encode session: %v", err)
		return fmt.Errorf("failed to encode session: %w", err)
	}
	logger.Debug("SaveSession: session encoded successfully")

	file.Close()

	// Atomic rename
	finalPath := s.getSessionPath(session.WorkingDir, storedID)
	logger.Debug("SaveSession: renaming %s to %s", tempPath, finalPath)

	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		logger.Error("SaveSession: failed to rename temp file: %v", err)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	logger.Info("SaveSession: successfully saved session %s to %s", session.ID, finalPath)

	// Mark the session as saved so we can skip redundant saves
	session.MarkSaved(time.Now())
	return nil
}

// LoadSession loads a session from disk
func (s *SessionStorage) LoadSession(workingDir, sessionID string) (*Session, error) {
	path := s.getSessionPath(workingDir, sessionID)

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	var stored StoredSession
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&stored); err != nil {
		return nil, fmt.Errorf("failed to decode session: %w", err)
	}

	// Check version compatibility
	if stored.Version != SessionStorageVersion {
		return nil, fmt.Errorf("session version mismatch: expected %d, got %d", SessionStorageVersion, stored.Version)
	}

	return s.FromStoredSession(&stored), nil
}

// ListSessions returns metadata for all sessions in a workspace
func (s *SessionStorage) ListSessions(workingDir string) ([]SessionMetadata, error) {
	workspaceDir := s.getWorkspaceDir(workingDir)

	// Check if workspace directory exists
	if _, err := os.Stat(workspaceDir); os.IsNotExist(err) {
		return []SessionMetadata{}, nil
	}

	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read workspace directory: %w", err)
	}

	var sessions []SessionMetadata
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gob") {
			continue
		}

		path := filepath.Join(workspaceDir, entry.Name())

		// Read metadata efficiently by decoding just the stored session
		file, err := os.Open(path)
		if err != nil {
			continue // Skip unreadable files
		}

		var stored StoredSession
		decoder := gob.NewDecoder(file)
		if err := decoder.Decode(&stored); err != nil {
			file.Close()
			continue // Skip corrupted files
		}
		file.Close()

		// Check version compatibility
		if stored.Version != SessionStorageVersion {
			continue // Skip incompatible versions
		}

		sessions = append(sessions, SessionMetadata{
			ID:           stored.ID,
			Name:         stored.Name,
			Title:        stored.Title,
			WorkingDir:   stored.WorkingDir,
			CreatedAt:    stored.CreatedAt,
			UpdatedAt:    stored.UpdatedAt,
			MessageCount: len(stored.Messages),
		})
	}

	return sessions, nil
}

// DeleteSession removes a session from disk
func (s *SessionStorage) DeleteSession(workingDir, sessionID string) error {
	path := s.getSessionPath(workingDir, sessionID)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	return nil
}

// ToStoredSession converts a runtime Session to a storable format
func (s *SessionStorage) ToStoredSession(session *Session, name string) *StoredSession {
	// Convert messages
	storedMessages := make([]*StoredMessage, len(session.GetMessages()))
	messages := session.GetMessages()
	for i, msg := range messages {
		storedMessages[i] = &StoredMessage{
			Role:              msg.Role,
			Content:           msg.Content,
			ToolCalls:         msg.ToolCalls,
			ToolID:            msg.ToolID,
			ToolName:          msg.ToolName,
			Timestamp:         msg.Timestamp,
			NativeFormat:      msg.NativeFormat,
			NativeProvider:    msg.NativeProvider,
			NativeModelFamily: msg.NativeModelFamily,
			NativeTimestamp:   msg.NativeTimestamp,
		}
	}

	// Convert background jobs
	storedJobs := make(map[string]*StoredBackgroundJob)
	for _, job := range session.ListBackgroundJobs() {
		storedJobs[job.ID] = &StoredBackgroundJob{
			ID:         job.ID,
			Command:    job.Command,
			WorkingDir: job.WorkingDir,
			StartTime:  job.StartTime,
			Completed:  job.Completed,
			ExitCode:   job.ExitCode,
			Stdout:     job.Stdout,
			Stderr:     job.Stderr,
			Type:       job.Type,
			LastSignal: job.LastSignal,
		}
	}

	// Get session snapshot
	session.mu.RLock()
	defer session.mu.RUnlock()

	return &StoredSession{
		Version:             SessionStorageVersion,
		ID:                  session.ID,
		WorkingDir:          session.WorkingDir,
		Name:                name,
		Title:               session.Title,
		Messages:            storedMessages,
		FilesRead:           session.FilesRead,
		FilesModified:       session.FilesModified,
		BackgroundJobs:      storedJobs,
		AuthorizedDomains:   session.AuthorizedDomains,
		AuthorizedCommands:  session.AuthorizedCommands,
		PlanningActive:      session.PlanningActive,
		PlanningObjective:   session.PlanningObjective,
		LastSandboxExitCode: session.LastSandboxExitCode,
		LastSandboxStdout:   session.LastSandboxStdout,
		LastSandboxStderr:   session.LastSandboxStderr,
		CurrentProvider:     session.CurrentProvider,
		CurrentModelFamily:  session.CurrentModelFamily,
		CreatedAt:           session.CreatedAt,
		UpdatedAt:           session.UpdatedAt,
		LastSavedAt:         session.LastSavedAt,
	}
}

// FromStoredSession converts a stored session back to a runtime Session
func (s *SessionStorage) FromStoredSession(stored *StoredSession) *Session {
	session := NewSession(stored.ID, stored.WorkingDir)

	// Convert messages
	session.Messages = make([]*Message, len(stored.Messages))
	for i, storedMsg := range stored.Messages {
		session.Messages[i] = &Message{
			Role:              storedMsg.Role,
			Content:           storedMsg.Content,
			ToolCalls:         storedMsg.ToolCalls,
			ToolID:            storedMsg.ToolID,
			ToolName:          storedMsg.ToolName,
			Timestamp:         storedMsg.Timestamp,
			NativeFormat:      storedMsg.NativeFormat,
			NativeProvider:    storedMsg.NativeProvider,
			NativeModelFamily: storedMsg.NativeModelFamily,
			NativeTimestamp:   storedMsg.NativeTimestamp,
		}
	}

	// Copy other fields (they're shallow copies, which is fine)
	session.Title = stored.Title
	session.FilesRead = stored.FilesRead
	session.FilesModified = stored.FilesModified
	session.AuthorizedDomains = stored.AuthorizedDomains
	session.AuthorizedCommands = stored.AuthorizedCommands
	session.PlanningActive = stored.PlanningActive
	session.PlanningObjective = stored.PlanningObjective
	session.LastSandboxExitCode = stored.LastSandboxExitCode
	session.LastSandboxStdout = stored.LastSandboxStdout
	session.LastSandboxStderr = stored.LastSandboxStderr
	session.CurrentProvider = stored.CurrentProvider
	session.CurrentModelFamily = stored.CurrentModelFamily
	session.CreatedAt = stored.CreatedAt
	session.UpdatedAt = stored.UpdatedAt
	session.LastSavedAt = stored.LastSavedAt
	session.Dirty = false

	// Convert background jobs
	session.BackgroundJobs = make(map[string]*BackgroundJob)
	for id, storedJob := range stored.BackgroundJobs {
		// Note: We don't restore the actual process, just metadata
		session.BackgroundJobs[id] = &BackgroundJob{
			ID:         storedJob.ID,
			Command:    storedJob.Command,
			WorkingDir: storedJob.WorkingDir,
			StartTime:  storedJob.StartTime,
			Completed:  storedJob.Completed,
			ExitCode:   storedJob.ExitCode,
			Stdout:     storedJob.Stdout,
			Stderr:     storedJob.Stderr,
			Type:       storedJob.Type,
			LastSignal: storedJob.LastSignal,
		}
	}

	return session
}

// StartAutoSave starts the automatic session saving process
func (s *SessionStorage) StartAutoSave(session *Session, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.autoSave || s.autoSaveActive {
		return
	}

	// Ensure we always have a fresh stop channel (may have been closed previously)
	if s.stopChan == nil {
		s.stopChan = make(chan struct{})
	}

	s.autoSaveActive = true
	s.wg.Add(1)

	stopChan := s.stopChan
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.saveInterval)
		defer ticker.Stop()

		logger.Info("Auto-save started for session %s", session.ID)

		for {
			select {
			case <-ticker.C:
				s.mu.RLock()
				hasAvailableSlots := s.activeSaves < s.maxSaves
				s.mu.RUnlock()

				if hasAvailableSlots {
					s.wg.Add(1)
					go func() {
						defer s.wg.Done()
						s.mu.Lock()
						s.activeSaves++
						s.mu.Unlock()

						logger.Debug("Auto-saving session %s", session.ID)
						err := s.SaveSession(session, name)
						if err != nil {
							logger.Error("Auto-save failed for session %s: %v", session.ID, err)
						}

						s.mu.Lock()
						s.activeSaves--
						s.mu.Unlock()
					}()
				}

			case <-stopChan:
				logger.Info("Auto-save stopped for session %s", session.ID)
				return
			}
		}
	}()
}

// StopAutoSave stops the automatic session saving process
func (s *SessionStorage) StopAutoSave() {
	s.mu.Lock()

	if !s.autoSaveActive {
		s.mu.Unlock()
		return
	}

	stopChan := s.stopChan
	// Prepare a new stop channel so autosave can be restarted after stopping
	s.stopChan = make(chan struct{})
	s.autoSaveActive = false
	s.mu.Unlock()

	// Close outside the lock so in-flight saves can finish and decrement counters
	if stopChan != nil {
		close(stopChan)
	}
	s.wg.Wait()
}

// RefreshAutoSaveConfig updates the auto-save configuration
func (s *SessionStorage) RefreshAutoSaveConfig() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.configFunc == nil {
		return false
	}

	cfg := s.configFunc()
	originalEnabled := s.autoSave

	s.autoSave = cfg.Enabled
	s.saveInterval = time.Duration(cfg.SaveIntervalSeconds) * time.Second
	s.maxSaves = cfg.MaxConcurrentSaves
	if s.maxSaves == 0 {
		s.maxSaves = 1
	}

	return s.autoSave != originalEnabled
}
