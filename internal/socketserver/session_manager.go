package socketserver

import (
	"fmt"
	"sync"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

// SessionInternalInfo contains metadata about an active session (internal use)
type SessionInternalInfo struct {
	ID            string
	Title         string
	WorkingDir    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	OwnerClientID string // ID of the client that owns this session
	MessageCount  int
	Dirty         bool
}

// SessionManager manages the lifecycle of sessions over the Unix socket
type SessionManager struct {
	// Session registry: sessionID -> SessionInternalInfo
	sessions map[string]*SessionInternalInfo
	mu       sync.RWMutex

	// Session objects: sessionID -> *session.Session
	sessionObjects map[string]*session.Session
	objectsMu      sync.RWMutex

	// Client to session mapping: clientID -> sessionID
	clientSessions map[string]string
	clientMu       sync.RWMutex

	// Session storage
	storage *session.SessionStorage

	// Auto-save ticker
	autoSaveTicker *time.Ticker
	autoSaveStop   chan struct{}
	autoSaveActive bool

	// Configuration reference
	cfg *config.Config
}

// NewSessionManager creates a new session manager
func NewSessionManager(cfg *config.Config) (*SessionManager, error) {
	storage, err := session.NewSessionStorageWithConfig(func() *config.AutoSaveConfig {
		return &cfg.AutoSave
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session storage: %w", err)
	}

	sm := &SessionManager{
		sessions:       make(map[string]*SessionInternalInfo),
		sessionObjects: make(map[string]*session.Session),
		clientSessions: make(map[string]string),
		storage:        storage,
		cfg:            cfg,
		autoSaveStop:   make(chan struct{}),
	}

	// Start auto-save if configured
	if cfg.AutoSave.Enabled {
		sm.startAutoSave()
	}

	logger.Info("Session manager initialized with auto-save=%v", cfg.AutoSave.Enabled)
	return sm, nil
}

// startAutoSave starts the auto-save background process
func (sm *SessionManager) startAutoSave() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.autoSaveActive {
		return
	}

	interval := time.Duration(sm.cfg.AutoSave.SaveIntervalSeconds) * time.Second
	sm.autoSaveTicker = time.NewTicker(interval)
	sm.autoSaveActive = true

	go sm.autoSaveLoop()

	logger.Debug("Auto-save started with interval %v", interval)
}

// stopAutoSave stops the auto-save background process
func (sm *SessionManager) stopAutoSave() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.autoSaveActive {
		return
	}

	if sm.autoSaveTicker != nil {
		sm.autoSaveTicker.Stop()
	}
	close(sm.autoSaveStop)
	sm.autoSaveStop = make(chan struct{})
	sm.autoSaveActive = false

	logger.Debug("Auto-save stopped")
}

// autoSaveLoop runs the auto-save loop
func (sm *SessionManager) autoSaveLoop() {
	for {
		select {
		case <-sm.autoSaveTicker.C:
			sm.saveDirtySessions()
		case <-sm.autoSaveStop:
			return
		}
	}
}

// saveDirtySessions saves all dirty sessions
func (sm *SessionManager) saveDirtySessions() {
	sm.objectsMu.RLock()
	sessionIDs := make([]string, 0, len(sm.sessionObjects))
	for id := range sm.sessionObjects {
		sessionIDs = append(sessionIDs, id)
	}
	sm.objectsMu.RUnlock()

	for _, id := range sessionIDs {
		if err := sm.SaveSession(id, ""); err != nil {
			logger.Warn("Auto-save failed for session %s: %v", id, err)
		}
	}
}

// Shutdown gracefully shuts down the session manager
func (sm *SessionManager) Shutdown() {
	// Stop auto-save
	sm.stopAutoSave()

	// Save all dirty sessions before shutdown
	sm.saveDirtySessions()

	logger.Info("Session manager shut down")
}

// CreateSession creates a new session and returns its ID
func (sm *SessionManager) CreateSession(workingDir string) (string, *session.Session, error) {
	sessionID := session.GenerateID()
	sess := session.NewSession(sessionID, workingDir)

	now := time.Now()

	sm.mu.Lock()
	sm.sessions[sessionID] = &SessionInternalInfo{
		ID:           sessionID,
		Title:        "", // Will be auto-generated later
		WorkingDir:   workingDir,
		CreatedAt:    now,
		UpdatedAt:    now,
		MessageCount: 0,
		Dirty:        true, // New sessions are dirty
	}
	sm.mu.Unlock()

	sm.objectsMu.Lock()
	sm.sessionObjects[sessionID] = sess
	sm.objectsMu.Unlock()

	logger.Info("Created new session %s for workspace %s", sessionID, workingDir)
	return sessionID, sess, nil
}

// LoadSession loads an existing session from storage
func (sm *SessionManager) LoadSession(workingDir, sessionID string) (*session.Session, error) {
	// Check if session is already loaded
	sm.objectsMu.RLock()
	if sess, exists := sm.sessionObjects[sessionID]; exists {
		sm.objectsMu.RUnlock()
		logger.Info("Session %s already loaded, returning cached version", sessionID)
		return sess, nil
	}
	sm.objectsMu.RUnlock()

	// Load from storage
	sess, err := sm.storage.LoadSession(workingDir, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	// Add to registry
	sm.mu.Lock()
	sm.sessions[sessionID] = &SessionInternalInfo{
		ID:           sessionID,
		Title:        sess.Title,
		WorkingDir:   workingDir,
		CreatedAt:    sess.CreatedAt,
		UpdatedAt:    sess.UpdatedAt,
		MessageCount: len(sess.GetMessages()),
		Dirty:        false, // Just loaded from storage
	}
	sm.mu.Unlock()

	sm.objectsMu.Lock()
	sm.sessionObjects[sessionID] = sess
	sm.objectsMu.Unlock()

	logger.Info("Loaded session %s from storage (workspace: %s)", sessionID, workingDir)
	return sess, nil
}

// SaveSession saves a session to storage
func (sm *SessionManager) SaveSession(sessionID, name string) error {
	sm.objectsMu.RLock()
	sess, exists := sm.sessionObjects[sessionID]
	sm.objectsMu.RUnlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Update title if name provided
	if name != "" && sess.Title != name {
		sess.SetTitle(name)
	}

	if err := sm.storage.SaveSession(sess, name); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	// Update session info
	sm.mu.Lock()
	if info, exists := sm.sessions[sessionID]; exists {
		info.Dirty = false
		info.UpdatedAt = time.Now()
	}
	sm.mu.Unlock()

	logger.Debug("Saved session %s", sessionID)
	return nil
}

// DeleteSession deletes a session from storage and registry
func (sm *SessionManager) DeleteSession(workingDir, sessionID string) error {
	// Check if session has an owner
	sm.mu.RLock()
	info, hasInfo := sm.sessions[sessionID]
	owner := ""
	if hasInfo && info.OwnerClientID != "" {
		owner = info.OwnerClientID
	}
	sm.mu.RUnlock()

	if owner != "" {
		return fmt.Errorf("cannot delete session %s: owned by client %s", sessionID, owner)
	}

	// Delete from storage
	if err := sm.storage.DeleteSession(workingDir, sessionID); err != nil {
		return fmt.Errorf("failed to delete session from storage: %w", err)
	}

	// Remove from registry
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	sm.objectsMu.Lock()
	delete(sm.sessionObjects, sessionID)
	sm.objectsMu.Unlock()

	logger.Info("Deleted session %s", sessionID)
	return nil
}

// ListSessions lists all sessions for a workspace
func (sm *SessionManager) ListSessions(workingDir string) ([]session.SessionMetadata, error) {
	return sm.storage.ListSessions(workingDir)
}

// GetSession retrieves a session object by ID
func (sm *SessionManager) GetSession(sessionID string) (*session.Session, bool) {
	sm.objectsMu.RLock()
	defer sm.objectsMu.RUnlock()
	sess, exists := sm.sessionObjects[sessionID]
	return sess, exists
}

// GetSessionInfo retrieves session metadata by ID
func (sm *SessionManager) GetSessionInfo(sessionID string) (*SessionInternalInfo, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	info, exists := sm.sessions[sessionID]
	return info, exists
}

// AttachClient attaches a client to a session (makes the client the owner)
func (sm *SessionManager) AttachClient(clientID, sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session exists
	info, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Check if session already has an owner
	if info.OwnerClientID != "" && info.OwnerClientID != clientID {
		return fmt.Errorf("session %s is already owned by client %s", sessionID, info.OwnerClientID)
	}

	// Update client session mapping
	sm.clientMu.Lock()
	oldSessionID, hasOldSession := sm.clientSessions[clientID]
	if hasOldSession && oldSessionID != sessionID {
		// Detach from old session
		if oldInfo, ok := sm.sessions[oldSessionID]; ok && oldInfo.OwnerClientID == clientID {
			oldInfo.OwnerClientID = ""
		}
	}
	sm.clientSessions[clientID] = sessionID
	sm.clientMu.Unlock()

	// Update session owner
	info.OwnerClientID = clientID
	info.UpdatedAt = time.Now()

	logger.Info("Client %s attached to session %s", clientID, sessionID)
	return nil
}

// DetachClient detaches a client from its session
func (sm *SessionManager) DetachClient(clientID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.clientMu.Lock()
	sessionID, hasSession := sm.clientSessions[clientID]
	if hasSession {
		delete(sm.clientSessions, clientID)
	}
	sm.clientMu.Unlock()

	if hasSession {
		if info, exists := sm.sessions[sessionID]; exists {
			if info.OwnerClientID == clientID {
				info.OwnerClientID = ""
				info.UpdatedAt = time.Now()
			}
		}
		logger.Info("Client %s detached from session %s", clientID, sessionID)
	}
}

// GetClientSessionID returns the session ID for a client
func (sm *SessionManager) GetClientSessionID(clientID string) (string, bool) {
	sm.clientMu.RLock()
	defer sm.clientMu.RUnlock()
	sessionID, exists := sm.clientSessions[clientID]
	return sessionID, exists
}

// GetSessionOwner returns the owner client ID for a session
func (sm *SessionManager) GetSessionOwner(sessionID string) (string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	info, exists := sm.sessions[sessionID]
	if !exists {
		return "", false
	}
	return info.OwnerClientID, true
}

// ListActiveSessions returns all active (in-memory) sessions
func (sm *SessionManager) ListActiveSessions() []*SessionInternalInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]*SessionInternalInfo, 0, len(sm.sessions))
	for _, info := range sm.sessions {
		sessions = append(sessions, info)
	}
	return sessions
}

// MarkSessionDirty marks a session as dirty (has unsaved changes)
func (sm *SessionManager) MarkSessionDirty(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if info, exists := sm.sessions[sessionID]; exists {
		info.Dirty = true
		info.UpdatedAt = time.Now()
	}
}

// UpdateSessionMessageCount updates the message count for a session
func (sm *SessionManager) UpdateSessionMessageCount(sessionID string, count int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if info, exists := sm.sessions[sessionID]; exists {
		info.MessageCount = count
		info.UpdatedAt = time.Now()
	}
}

// UpdateSessionTitle updates the title for a session
func (sm *SessionManager) UpdateSessionTitle(sessionID, title string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if info, exists := sm.sessions[sessionID]; exists {
		info.Title = title
		info.UpdatedAt = time.Now()
	}

	sm.objectsMu.Lock()
	if sess, exists := sm.sessionObjects[sessionID]; exists {
		sess.SetTitle(title)
	}
	sm.objectsMu.Unlock()
}
