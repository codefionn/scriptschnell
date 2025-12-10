package actor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/codefionn/scriptschnell/internal/session"
)

// SessionStorageActor handles persistent storage of session data
type SessionStorageActor struct {
	name    string
	storage *session.SessionStorage
}

func NewSessionStorageActor(name string) (*SessionStorageActor, error) {
	return NewSessionStorageActorWithConfig(name, nil)
}

func NewSessionStorageActorWithConfig(name string, configFunc func() *config.AutoSaveConfig) (*SessionStorageActor, error) {
	var storage *session.SessionStorage
	var err error

	if configFunc != nil {
		storage, err = session.NewSessionStorageWithConfig(configFunc)
	} else {
		storage, err = session.NewSessionStorage()
	}
	if err != nil {
		return nil, err
	}

	return &SessionStorageActor{
		name:    name,
		storage: storage,
	}, nil
}

func (a *SessionStorageActor) ID() string { return a.name }

func (a *SessionStorageActor) Start(ctx context.Context) error {
	return nil
}

func (a *SessionStorageActor) Stop(ctx context.Context) error {
	return nil
}

func (a *SessionStorageActor) Receive(ctx context.Context, msg Message) error {
	switch m := msg.(type) {
	case SessionStorageSaveMsg:
		logger.Debug("SessionStorageActor: received save message for session %s", m.Session.ID)
		err := a.storage.SaveSession(m.Session, m.Name)
		logger.Debug("SessionStorageActor: SaveSession returned err=%v", err)
		m.ResponseChan <- SessionStorageSaveResponse{Err: err}
		return nil
	case SessionStorageLoadMsg:
		logger.Debug("SessionStorageActor: received load message for session %s", m.SessionID)
		session, err := a.storage.LoadSession(m.WorkingDir, m.SessionID)
		logger.Debug("SessionStorageActor: LoadSession returned err=%v", err)
		m.ResponseChan <- SessionStorageLoadResponse{Session: session, Err: err}
		return nil
	case SessionStorageListMsg:
		logger.Debug("SessionStorageActor: received list message for workspace %s", m.WorkingDir)
		sessions, err := a.storage.ListSessions(m.WorkingDir)
		logger.Debug("SessionStorageActor: ListSessions returned %d sessions, err=%v", len(sessions), err)
		m.ResponseChan <- SessionStorageListResponse{Sessions: sessions, Err: err}
		return nil
	case SessionStorageDeleteMsg:
		logger.Debug("SessionStorageActor: received delete message for session %s", m.SessionID)
		err := a.storage.DeleteSession(m.WorkingDir, m.SessionID)
		logger.Debug("SessionStorageActor: DeleteSession returned err=%v", err)
		m.ResponseChan <- SessionStorageDeleteResponse{Err: err}
		return nil
	case SessionStorageStartAutoSaveMsg:
		logger.Debug("SessionStorageActor: received start autosave message for session %s", m.Session.ID)
		a.storage.StartAutoSave(m.Session, m.Name)
		logger.Debug("SessionStorageActor: StartAutoSave completed")
		m.ResponseChan <- SessionStorageStartAutoSaveResponse{Err: nil}
		return nil
	case SessionStorageStopAutoSaveMsg:
		logger.Debug("SessionStorageActor: received stop autosave message")
		a.storage.StopAutoSave()
		logger.Debug("SessionStorageActor: StopAutoSave completed")
		m.ResponseChan <- SessionStorageStopAutoSaveResponse{Err: nil}
		return nil
	default:
		return fmt.Errorf("unknown session storage actor message type: %T", msg)
	}
}

// Message types

type SessionStorageSaveMsg struct {
	Session      *session.Session
	Name         string
	ResponseChan chan SessionStorageSaveResponse
}

func (SessionStorageSaveMsg) Type() string { return "sessionStorageSaveMsg" }

type SessionStorageSaveResponse struct {
	Err error
}

type SessionStorageLoadMsg struct {
	WorkingDir   string
	SessionID    string
	ResponseChan chan SessionStorageLoadResponse
}

func (SessionStorageLoadMsg) Type() string { return "sessionStorageLoadMsg" }

type SessionStorageLoadResponse struct {
	Session *session.Session
	Err     error
}

type SessionStorageListMsg struct {
	WorkingDir   string
	ResponseChan chan SessionStorageListResponse
}

func (SessionStorageListMsg) Type() string { return "sessionStorageListMsg" }

type SessionStorageListResponse struct {
	Sessions []session.SessionMetadata
	Err      error
}

type SessionStorageDeleteMsg struct {
	WorkingDir   string
	SessionID    string
	ResponseChan chan SessionStorageDeleteResponse
}

func (SessionStorageDeleteMsg) Type() string { return "sessionStorageDeleteMsg" }

type SessionStorageDeleteResponse struct {
	Err error
}

type SessionStorageStartAutoSaveMsg struct {
	Session      *session.Session
	Name         string
	ResponseChan chan SessionStorageStartAutoSaveResponse
}

func (SessionStorageStartAutoSaveMsg) Type() string { return "sessionStorageStartAutoSaveMsg" }

type SessionStorageStartAutoSaveResponse struct {
	Err error
}

type SessionStorageStopAutoSaveMsg struct {
	ResponseChan chan SessionStorageStopAutoSaveResponse
}

func (SessionStorageStopAutoSaveMsg) Type() string { return "sessionStorageStopAutoSaveMsg" }

type SessionStorageStopAutoSaveResponse struct {
	Err error
}

// Helper functions for sending messages to the session storage actor

// SaveSession saves a session to persistent storage
func SaveSessionViaActor(ctx context.Context, storageRef *ActorRef, session *session.Session, name string) error {
	logger.Debug("SaveSessionViaActor: starting save for session %s with name %s", session.ID, name)

	responseChan := make(chan SessionStorageSaveResponse, 1)

	msg := SessionStorageSaveMsg{
		Session:      session,
		Name:         name,
		ResponseChan: responseChan,
	}

	if err := storageRef.Send(msg); err != nil {
		logger.Error("SaveSessionViaActor: failed to send message: %v", err)
		return err
	}
	logger.Debug("SaveSessionViaActor: message sent, waiting for response")

	select {
	case response := <-responseChan:
		logger.Debug("SaveSessionViaActor: received response with err=%v", response.Err)
		return response.Err
	case <-ctx.Done():
		logger.Error("SaveSessionViaActor: context cancelled: %v", ctx.Err())
		return ctx.Err()
	}
}

// LoadSession loads a session from persistent storage
func LoadSessionViaActor(ctx context.Context, storageRef *ActorRef, workingDir, sessionID string) (*session.Session, error) {
	responseChan := make(chan SessionStorageLoadResponse, 1)

	msg := SessionStorageLoadMsg{
		WorkingDir:   workingDir,
		SessionID:    sessionID,
		ResponseChan: responseChan,
	}

	if err := storageRef.Send(msg); err != nil {
		return nil, err
	}

	select {
	case response := <-responseChan:
		return response.Session, response.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ListSessions returns all sessions for a workspace
func ListSessionsViaActor(ctx context.Context, storageRef *ActorRef, workingDir string) ([]session.SessionMetadata, error) {
	responseChan := make(chan SessionStorageListResponse, 1)

	msg := SessionStorageListMsg{
		WorkingDir:   workingDir,
		ResponseChan: responseChan,
	}

	if err := storageRef.Send(msg); err != nil {
		return nil, err
	}

	select {
	case response := <-responseChan:
		return response.Sessions, response.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DeleteSession removes a session from persistent storage
func DeleteSessionViaActor(ctx context.Context, storageRef *ActorRef, workingDir, sessionID string) error {
	responseChan := make(chan SessionStorageDeleteResponse, 1)

	msg := SessionStorageDeleteMsg{
		WorkingDir:   workingDir,
		SessionID:    sessionID,
		ResponseChan: responseChan,
	}

	if err := storageRef.Send(msg); err != nil {
		return err
	}

	select {
	case response := <-responseChan:
		return response.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StartAutoSaveViaActor starts automatic saving for a session
func StartAutoSaveViaActor(ctx context.Context, storageRef *ActorRef, session *session.Session, name string) error {
	logger.Debug("StartAutoSaveViaActor: starting autosave for session %s with name %s", session.ID, name)

	responseChan := make(chan SessionStorageStartAutoSaveResponse, 1)

	msg := SessionStorageStartAutoSaveMsg{
		Session:      session,
		Name:         name,
		ResponseChan: responseChan,
	}

	if err := storageRef.Send(msg); err != nil {
		logger.Error("StartAutoSaveViaActor: failed to send message: %v", err)
		return err
	}
	logger.Debug("StartAutoSaveViaActor: message sent, waiting for response")

	select {
	case response := <-responseChan:
		logger.Debug("StartAutoSaveViaActor: received response with err=%v", response.Err)
		return response.Err
	case <-ctx.Done():
		logger.Error("StartAutoSaveViaActor: context cancelled: %v", ctx.Err())
		return ctx.Err()
	}
}

// StopAutoSaveViaActor stops automatic saving for the current session
func StopAutoSaveViaActor(ctx context.Context, storageRef *ActorRef) error {
	logger.Debug("StopAutoSaveViaActor: stopping autosave")

	responseChan := make(chan SessionStorageStopAutoSaveResponse, 1)

	msg := SessionStorageStopAutoSaveMsg{
		ResponseChan: responseChan,
	}

	if err := storageRef.Send(msg); err != nil {
		logger.Error("StopAutoSaveViaActor: failed to send message: %v", err)
		return err
	}
	logger.Debug("StopAutoSaveViaActor: message sent, waiting for response")

	select {
	case response := <-responseChan:
		logger.Debug("StopAutoSaveViaActor: received response with err=%v", response.Err)
		return response.Err
	case <-ctx.Done():
		logger.Error("StopAutoSaveViaActor: context cancelled: %v", ctx.Err())
		return ctx.Err()
	}
}

// GenerateSessionName generates a session name with timestamp
func GenerateSessionName(baseName string) string {
	timestamp := time.Now().Format("2006-01-02-15-04-05")

	if baseName == "" {
		return fmt.Sprintf("Session %s", timestamp)
	}

	// Clean the base name
	cleanName := strings.TrimSpace(baseName)
	cleanName = strings.ReplaceAll(cleanName, "/", "-")
	cleanName = strings.ReplaceAll(cleanName, "\\", "-")

	return fmt.Sprintf("%s (%s)", cleanName, timestamp)
}
