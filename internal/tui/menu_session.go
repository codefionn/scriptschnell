package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/session"
)

// SessionMenuItem represents a session in the menu
type SessionMenuItem struct {
	metadata session.SessionMetadata
	index    int
}

// FilterValue implements list.Item
func (s SessionMenuItem) FilterValue() string {
	title := s.metadata.Title
	if title == "" {
		title = s.metadata.Name
	}
	if title == "" {
		title = "Unnamed"
	}
	return title
}

// Title implements MenuItem
func (s SessionMenuItem) Title() string {
	title := s.metadata.Title
	if title == "" {
		title = s.metadata.Name
	}
	if title == "" {
		title = "Unnamed"
	}
	return title
}

// Description implements MenuItem
func (s SessionMenuItem) Description() string {
	var parts []string
	idPrefix := s.metadata.ID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	parts = append(parts, fmt.Sprintf("ID: %s", idPrefix))
	parts = append(parts, fmt.Sprintf("Messages: %d", s.metadata.MessageCount))
	parts = append(parts, fmt.Sprintf("Updated: %s", formatRelativeTime(s.metadata.UpdatedAt)))
	return strings.Join(parts, " • ")
}

// GetSessionID returns the session ID
func (s SessionMenuItem) GetSessionID() string {
	return s.metadata.ID
}

// formatRelativeTime formats a time.Time as a relative time string
func formatRelativeTime(t time.Time) string {
	now := time.Now()
	duration := now.Sub(t)

	if duration < time.Minute {
		return "just now"
	}
	if duration < time.Hour {
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	if duration < 7*24*time.Hour {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	return t.Format("2006-01-02")
}

// SessionMenuModel is the model for the session management menu
type SessionMenuModel struct {
	menu          *GenericMenu
	sessions      []SessionMenuItem
	storageRef    *actor.ActorRef
	workingDir    string
	ctx           context.Context
	action        string // "load" or "delete"
	selectedItem  SessionMenuItem
	width         int
	height        int
	deleteKeyMap  key.Binding
	confirmDelete bool
	deleteTarget  SessionMenuItem
}

// SessionMenuAction represents an action to perform on a session
type SessionMenuAction struct {
	Action    string // "load" or "delete"
	SessionID string
}

// NewSessionMenu creates a new session management menu
func NewSessionMenu(ctx context.Context, storageRef *actor.ActorRef, workingDir string, width, height int) *SessionMenuModel {
	config := DefaultMenuConfig()
	config.Title = "Session Management"
	config.Width = width
	config.Height = height
	config.HelpText = "↑/↓: Navigate • Enter: Load • d: Delete • Esc: Cancel"
	config.DisableQuitKeys = true

	sm := &SessionMenuModel{
		storageRef: storageRef,
		workingDir: workingDir,
		ctx:        ctx,
		width:      width,
		height:     height,
		deleteKeyMap: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
	}

	// Load sessions
	if err := sm.loadSessions(); err != nil {
		// Create empty menu with error message
		config.Title = "Session Management - Error"
		config.HelpText = fmt.Sprintf("Error loading sessions: %v • Press Esc to close", err)
	}

	// Handle empty sessions case
	if len(sm.sessions) == 0 {
		config.Title = "Session Management - No Sessions"
		config.HelpText = "No saved sessions found • Press Esc to close"
	}

	// Convert to MenuItem
	items := make([]MenuItem, len(sm.sessions))
	for i, sess := range sm.sessions {
		items[i] = sess
	}

	sm.menu = NewGenericMenu(items, config)

	// Set custom key handler for delete
	sm.menu.SetCustomKeyHandler("d", func() tea.Msg {
		// Get currently selected session
		if item, ok := sm.menu.list.SelectedItem().(SessionMenuItem); ok {
			sm.deleteTarget = item
			sm.confirmDelete = true
			return nil
		}
		return nil
	})

	return sm
}

// loadSessions fetches sessions from storage
func (sm *SessionMenuModel) loadSessions() error {
	sessions, err := actor.ListSessionsViaActor(sm.ctx, sm.storageRef, sm.workingDir)
	if err != nil {
		return err
	}

	// Sort sessions by update time (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	// Convert to menu items
	sm.sessions = make([]SessionMenuItem, len(sessions))
	for i, sess := range sessions {
		sm.sessions[i] = SessionMenuItem{
			metadata: sess,
			index:    i,
		}
	}

	return nil
}

// Init implements tea.Model
func (sm *SessionMenuModel) Init() tea.Cmd {
	return sm.menu.Init()
}

// Update implements tea.Model
func (sm *SessionMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle delete confirmation
	if sm.confirmDelete {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "y", "Y":
				// Perform deletion
				sm.action = "delete"
				sm.selectedItem = sm.deleteTarget
				sm.confirmDelete = false
				return sm, tea.Batch(
					tea.Quit,
					func() tea.Msg {
						return SessionMenuAction{
							Action:    "delete",
							SessionID: sm.deleteTarget.metadata.ID,
						}
					},
				)
			case "n", "N", "esc":
				// Cancel deletion
				sm.confirmDelete = false
				return sm, nil
			}
		}
		return sm, nil
	}

	// Handle quit keys
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "esc" || keyMsg.String() == "ctrl+c" {
			return sm, tea.Quit
		}
	}

	// Handle menu selection
	if menuMsg, ok := msg.(MenuSelectedMsg); ok {
		if item, ok := menuMsg.Item.(SessionMenuItem); ok {
			sm.action = "load"
			sm.selectedItem = item
			return sm, tea.Batch(
				tea.Quit,
				func() tea.Msg {
					return SessionMenuAction{
						Action:    "load",
						SessionID: item.metadata.ID,
					}
				},
			)
		}
	}

	// Pass to menu
	var cmd tea.Cmd
	menuModel, cmd := sm.menu.Update(msg)
	if menu, ok := menuModel.(*GenericMenu); ok {
		sm.menu = menu
	}
	return sm, cmd
}

// View implements tea.Model
func (sm *SessionMenuModel) View() string {
	if sm.confirmDelete {
		title := sm.deleteTarget.metadata.Title
		if title == "" {
			title = sm.deleteTarget.metadata.Name
		}
		if title == "" {
			title = "Unnamed"
		}

		confirmStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 2).
			Width(60)

		content := fmt.Sprintf("Delete session?\n\n"+
			"Title: %s\n"+
			"ID: %s\n"+
			"Messages: %d\n\n"+
			"This action cannot be undone.\n\n"+
			"[y] Yes, delete  [n] No, cancel",
			title,
			sm.deleteTarget.metadata.ID[:8],
			sm.deleteTarget.metadata.MessageCount)

		return confirmStyle.Render(content)
	}

	return sm.menu.View()
}

// GetAction returns the selected action and session
func (sm *SessionMenuModel) GetAction() (string, SessionMenuItem) {
	return sm.action, sm.selectedItem
}
