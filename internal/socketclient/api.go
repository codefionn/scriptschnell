package socketclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CreateSession creates a new session
func (c *Client) CreateSession(ctx context.Context, workspace, sessionID string, workingDir string) (string, error) {
	if !c.IsConnected() {
		return "", NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	data := map[string]interface{}{}
	if workspace != "" {
		data["workspace"] = workspace
	}
	if sessionID != "" {
		data["session_id"] = sessionID
	}
	if workingDir != "" {
		data["working_dir"] = workingDir
	}

	msg := NewMessage("session_create", data)
	resp, err := c.SendRequest(msg)
	if err != nil {
		return "", err
	}

	var result struct {
		SessionID  string `json:"session_id"`
		Status     string `json:"status"`
		Workspace  string `json:"workspace"`
		WorkingDir string `json:"working_dir"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Update current session tracking
	c.currentSessionID.Store(result.SessionID)
	c.currentWorkspace.Store(result.Workspace)

	return result.SessionID, nil
}

// AttachSession attaches to an existing session
func (c *Client) AttachSession(ctx context.Context, sessionID string) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if sessionID == "" {
		return NewSocketError("INVALID_REQUEST", "Session ID is required", "")
	}

	msg := NewMessage("session_attach", map[string]interface{}{
		"session_id": sessionID,
	})

	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	// Update current session tracking
	c.currentSessionID.Store(sessionID)

	return nil
}

// DetachSession detaches from the current session
func (c *Client) DetachSession(ctx context.Context) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	msg := NewMessage("session_detach", nil)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	// Clear current session tracking
	c.currentSessionID.Store("")

	return nil
}

// ListSessions lists sessions for a workspace
func (c *Client) ListSessions(ctx context.Context, workspace string) ([]SessionInfo, error) {
	if !c.IsConnected() {
		return nil, NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	data := map[string]interface{}{}
	if workspace != "" {
		data["workspace"] = workspace
	}

	msg := NewMessage("session_list", data)
	resp, err := c.SendRequest(msg)
	if err != nil {
		return nil, err
	}

	var result struct {
		Sessions []SessionInfo `json:"sessions"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Sessions, nil
}

// DeleteSession deletes a session
func (c *Client) DeleteSession(ctx context.Context, sessionID, workspace string) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if sessionID == "" {
		return NewSocketError("INVALID_REQUEST", "Session ID is required", "")
	}

	data := map[string]interface{}{
		"session_id": sessionID,
	}

	if workspace != "" {
		data["workspace"] = workspace
	}

	msg := NewMessage("session_delete", data)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	// Clear current session tracking if we deleted it
	if c.GetCurrentSessionID() == sessionID {
		c.currentSessionID.Store("")
	}

	return nil
}

// SaveSession saves the current session
func (c *Client) SaveSession(ctx context.Context, name string) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	data := map[string]interface{}{}
	if name != "" {
		data["name"] = name
	}

	msg := NewMessage("session_save", data)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	return nil
}

// LoadSession loads a saved session
func (c *Client) LoadSession(ctx context.Context, sessionID, workspace string) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if sessionID == "" {
		return NewSocketError("INVALID_REQUEST", "Session ID is required", "")
	}

	data := map[string]interface{}{
		"session_id": sessionID,
	}

	if workspace != "" {
		data["workspace"] = workspace
	}

	msg := NewMessage("session_load", data)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	// Update current session tracking
	c.currentSessionID.Store(sessionID)
	if workspace != "" {
		c.currentWorkspace.Store(workspace)
	}

	return nil
}

// SendChat sends a chat message to the server
func (c *Client) SendChat(ctx context.Context, content string, options map[string]interface{}) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if content == "" {
		return NewSocketError("INVALID_REQUEST", "Content is required", "")
	}

	data := map[string]interface{}{
		"content": content,
	}

	if options != nil {
		data["options"] = options
	}

	msg := NewMessage("chat_send", data)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	return nil
}

// StopChat stops the current chat operation
func (c *Client) StopChat(ctx context.Context) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	msg := NewMessage("chat_stop", nil)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	return nil
}

// ClearChat clears the current chat history
func (c *Client) ClearChat(ctx context.Context) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	msg := NewMessage("chat_clear", nil)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	return nil
}

// ListWorkspaces lists all workspaces
func (c *Client) ListWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	if !c.IsConnected() {
		return nil, NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	msg := NewMessage("workspace_list", nil)
	resp, err := c.SendRequest(msg)
	if err != nil {
		return nil, err
	}

	var result struct {
		Workspaces []WorkspaceInfo `json:"workspaces"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Workspaces, nil
}

// SetWorkspace sets the active workspace
func (c *Client) SetWorkspace(ctx context.Context, workspace string) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if workspace == "" {
		return NewSocketError("INVALID_REQUEST", "Workspace is required", "")
	}

	msg := NewMessage("workspace_set", map[string]interface{}{
		"workspace": workspace,
	})

	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	// Update current workspace tracking
	c.currentWorkspace.Store(workspace)

	return nil
}

// GetConfig gets configuration values
func (c *Client) GetConfig(ctx context.Context, keys []string) (map[string]ConfigValue, error) {
	if !c.IsConnected() {
		return nil, NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	data := map[string]interface{}{}
	if len(keys) > 0 {
		data["keys"] = keys
	}

	msg := NewMessage("config_get", data)
	resp, err := c.SendRequest(msg)
	if err != nil {
		return nil, err
	}

	var result map[string]ConfigValue
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// SetConfig sets configuration values
func (c *Client) SetConfig(ctx context.Context, values map[string]interface{}) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if len(values) == 0 {
		return NewSocketError("INVALID_REQUEST", "Values are required", "")
	}

	msg := NewMessage("config_set", map[string]interface{}{
		"values": values,
	})

	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	return nil
}

// SendAuthorizationResponse sends an authorization response
func (c *Client) SendAuthorizationResponse(authID string, approved bool) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if authID == "" {
		return NewSocketError("INVALID_REQUEST", "Auth ID is required", "")
	}

	msg := NewMessage("authorization_response", map[string]interface{}{
		"auth_id":  authID,
		"approved": approved,
	})

	return c.SendMessage(msg)
}

// SendAuthorizationAck sends an authorization acknowledgment
func (c *Client) SendAuthorizationAck(authID string) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if authID == "" {
		return NewSocketError("INVALID_REQUEST", "Auth ID is required", "")
	}

	msg := NewMessage("authorization_ack", map[string]interface{}{
		"auth_id": authID,
	})

	return c.SendMessage(msg)
}

// SendQuestionResponse sends a question response
func (c *Client) SendQuestionResponse(questionID string, answer string, answers map[string]string) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if questionID == "" {
		return NewSocketError("INVALID_REQUEST", "Question ID is required", "")
	}

	data := map[string]interface{}{
		"question_id": questionID,
	}

	if answers != nil {
		data["answers"] = answers
	} else {
		data["answer"] = answer
	}

	msg := NewMessage("question_response", data)
	return c.SendMessage(msg)
}

// Ping sends a ping message
func (c *Client) Ping() error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	msg := NewMessage("ping", nil)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	return nil
}

// CloseSession closes the current session gracefully
func (c *Client) CloseSession(ctx context.Context, reason string, preserveSession bool) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	data := map[string]interface{}{
		"reason":           reason,
		"preserve_session": preserveSession,
	}

	msg := NewMessage("close", data)
	_, err := c.SendRequest(msg)
	if err != nil {
		return err
	}

	// Clear current session tracking
	c.currentSessionID.Store("")

	return nil
}

// CreateWorkspace creates a new workspace (e.g., git worktree)
func (c *Client) CreateWorkspace(ctx context.Context, baseWorkspace, name string) (workspaceID, path string, err error) {
	if !c.IsConnected() {
		return "", "", NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if baseWorkspace == "" || name == "" {
		return "", "", NewSocketError("INVALID_REQUEST", "Base workspace and name are required", "")
	}

	msg := NewMessage("workspace_create", map[string]interface{}{
		"base_workspace": baseWorkspace,
		"name":           name,
	})

	resp, err := c.SendRequest(msg)
	if err != nil {
		return "", "", err
	}

	var result struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
		IsWorktree  bool   `json:"is_worktree"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.WorkspaceID, result.Path, nil
}

// GetSessionInfo gets information about the current session
func (c *Client) GetSessionInfo(ctx context.Context) (*SessionInfo, error) {
	sessionID := c.GetCurrentSessionID()
	if sessionID == "" {
		return nil, NewSocketError("NO_SESSION", "No active session", "")
	}

	// List sessions and find the current one
	sessions, err := c.ListSessions(ctx, c.GetCurrentWorkspace())
	if err != nil {
		return nil, err
	}

	for _, sess := range sessions {
		if sess.SessionID == sessionID {
			return &sess, nil
		}
	}

	return nil, NewSocketError("SESSION_NOT_FOUND", "Current session not found", sessionID)
}

// WaitForCompletion waits for a chat operation to complete
func (c *Client) WaitForCompletion(ctx context.Context, timeout time.Duration) error {
	sessionID := c.GetCurrentSessionID()
	if sessionID == "" {
		return NewSocketError("NO_SESSION", "No active session", "")
	}

	// Create a channel to receive final message
	doneCh := make(chan struct{})
	oldCallback := c.chatMessageCallback

	// Set temporary callback to detect completion
	c.SetChatMessageCallback(func(msg ChatMessage) {
		// Call original callback if set
		if oldCallback != nil {
			oldCallback(msg)
		}

		// Check if this is the final message
		if msg.IsFinal {
			close(doneCh)
		}
	})

	defer c.SetChatMessageCallback(oldCallback)

	// Wait for completion or timeout
	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return NewSocketError("TIMEOUT", "Operation timed out", "")
	}
}

// StreamChat sends a chat message and streams responses via callback
func (c *Client) StreamChat(ctx context.Context, content string, options map[string]interface{}, callback func(ChatMessage)) error {
	if !c.IsConnected() {
		return NewSocketError("NOT_CONNECTED", "Not connected to server", "")
	}

	if content == "" {
		return NewSocketError("INVALID_REQUEST", "Content is required", "")
	}

	// Set temporary callback
	oldCallback := c.chatMessageCallback
	c.SetChatMessageCallback(callback)
	defer c.SetChatMessageCallback(oldCallback)

	return c.SendChat(ctx, content, options)
}