package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/session"
)

func TestIsValidSessionName(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "alphanumeric", value: "session_01", want: true},
		{name: "with hyphen", value: "feature-tab", want: true},
		{name: "spaces", value: "bad name", want: false},
		{name: "symbols", value: "oops!", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidSessionName(tt.value); got != tt.want {
				t.Fatalf("isValidSessionName(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestHandleNewTabValidation(t *testing.T) {
	t.Run("rejects invalid name", func(t *testing.T) {
		m := New("test-model", "", true)

		m.handleNewTab("bad name")

		if len(m.sessions) != 0 {
			t.Fatalf("expected no sessions to be created, got %d", len(m.sessions))
		}
		if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].content, "Invalid session name") {
			t.Fatalf("expected invalid name error message, got messages: %+v", m.messages)
		}
	})

	t.Run("rejects duplicate name", func(t *testing.T) {
		m := New("test-model", "", true)
		m.sessions = []*TabSession{{
			ID:             1,
			Session:        session.NewSession("tab-1", ""),
			Name:           "duplicate",
			Messages:       []message{},
			Generating:     false,
			WaitingForAuth: false,
		}}
		m.activeSessionIdx = 0

		m.handleNewTab("duplicate")

		if len(m.sessions) != 1 {
			t.Fatalf("expected sessions to remain unchanged, got %d", len(m.sessions))
		}
		if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].content, "already exists") {
			t.Fatalf("expected duplicate name warning, got messages: %+v", m.messages)
		}
	})
}

func TestHandleNewTabMaxLimit(t *testing.T) {
	m := newModelWithTabs(t, maxTabs)

	m.handleNewTab("extra")

	if len(m.sessions) != maxTabs {
		t.Fatalf("expected tab count to stay at max (%d), got %d", maxTabs, len(m.sessions))
	}
	if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].content, "Maximum") {
		t.Fatalf("expected max tab warning message, got %+v", m.messages)
	}
}

func TestHandleNewTabCreatesSession(t *testing.T) {
	m := New("test-model", "", true)

	cmd := m.handleNewTab("")

	if cmd != nil {
		t.Fatalf("expected nil command for new tab, got %v", cmd)
	}
	if len(m.sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(m.sessions))
	}

	tab := m.sessions[0]
	if tab.ID != 1 {
		t.Fatalf("expected tab ID 1, got %d", tab.ID)
	}
	if tab.Session == nil {
		t.Fatal("expected session to be initialized")
	}
	if tab.DisplayName() != "Tab 1" {
		t.Fatalf("expected display name 'Tab 1', got %q", tab.DisplayName())
	}
	if tab.WorktreePath != "" {
		t.Fatalf("expected empty worktree path for unnamed tab, got %q", tab.WorktreePath)
	}
	if m.sessionIDCounter != 1 {
		t.Fatalf("expected sessionIDCounter to be 1, got %d", m.sessionIDCounter)
	}
}

func TestHandleSwitchTabTransfersState(t *testing.T) {
	m := New("test-model", "", true)

	usage := map[string]interface{}{"tokens": 5}
	tab0 := &TabSession{
		ID:       1,
		Session:  session.NewSession("tab-1", ""),
		Messages: []message{},
	}
	tab1 := &TabSession{
		ID:              2,
		Session:         session.NewSession("tab-2", ""),
		Messages:        []message{{role: "System", content: "tab1 message"}},
		Generating:      true,
		ThinkingTokens:  7,
		OpenRouterUsage: usage,
	}

	m.sessions = []*TabSession{tab0, tab1}
	m.activeSessionIdx = 0
	m.messages = []message{{role: "You", content: "cached"}}

	cmd := m.handleSwitchTab(1)
	if cmd != nil {
		t.Fatalf("expected nil command when switching tab, got %v", cmd)
	}

	if m.activeSessionIdx != 1 {
		t.Fatalf("expected active tab index 1, got %d", m.activeSessionIdx)
	}
	if len(m.sessions[0].Messages) != 1 || m.sessions[0].Messages[0].content != "cached" {
		t.Fatalf("expected tab 0 messages to be cached, got %+v", m.sessions[0].Messages)
	}
	if len(m.messages) != 1 || m.messages[0].content != "tab1 message" {
		t.Fatalf("expected messages from tab 1 to be loaded, got %+v", m.messages)
	}
	if m.processingStatus != "Generating..." {
		t.Fatalf("expected processing status to reflect generating state, got %q", m.processingStatus)
	}
	if m.thinkingTokens != 7 {
		t.Fatalf("expected thinking tokens from new tab to be applied, got %d", m.thinkingTokens)
	}
	if !reflect.DeepEqual(m.openRouterUsage, usage) {
		t.Fatal("expected OpenRouter usage to follow active tab")
	}
}

func TestHandleCloseTabPreventsGenerating(t *testing.T) {
	m := newModelWithTabs(t, 2)
	m.setTabGenerating(0, true)

	m.handleCloseTab(0)

	if len(m.sessions) != 2 {
		t.Fatalf("expected tabs to remain when closing generating tab, got %d", len(m.sessions))
	}
	if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].content, "Cannot close tab during generation") {
		t.Fatalf("expected warning when closing generating tab, got %+v", m.messages)
	}
}

func TestHandleCloseLastTabBlocked(t *testing.T) {
	m := newModelWithTabs(t, 1)

	m.handleCloseTab(0)

	if len(m.sessions) != 1 {
		t.Fatalf("expected last tab to remain, got %d", len(m.sessions))
	}
	if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].content, "Cannot close last tab") {
		t.Fatalf("expected warning when closing last tab, got %+v", m.messages)
	}
}

func TestHandleCloseTabRemovesTabAndState(t *testing.T) {
	m := newModelWithTabs(t, 2)
	m.queuedPrompts[0] = []string{"queued work"}
	m.concurrentGens[m.sessions[0].ID] = true

	m.handleCloseTab(0)

	if len(m.sessions) != 1 {
		t.Fatalf("expected one tab after closing, got %d", len(m.sessions))
	}
	if m.sessions[0].ID != 2 {
		t.Fatalf("expected remaining tab ID 2, got %d", m.sessions[0].ID)
	}
	if m.activeSessionIdx != 0 {
		t.Fatalf("expected active session index to be 0, got %d", m.activeSessionIdx)
	}
	if _, ok := m.queuedPrompts[0]; ok {
		t.Fatal("expected queued prompts for closed tab to be removed")
	}
	if _, ok := m.concurrentGens[1]; ok {
		t.Fatal("expected concurrent generation state for closed tab to be removed")
	}
}

func TestRenderTabBarIndicators(t *testing.T) {
	m := newModelWithTabs(t, 3)
	m.sessions[0].Generating = true
	m.sessions[1].Messages = []message{{role: "Assistant", content: "hi"}}
	m.sessions[2].WaitingForAuth = true

	tabBar := m.renderTabBar()

	if tabBar == "" {
		t.Fatal("expected tab bar to render when multiple tabs exist")
	}
	if !strings.Contains(tabBar, "Tab 1") || !strings.Contains(tabBar, "Tab 2") || !strings.Contains(tabBar, "Tab 3") {
		t.Fatalf("expected all tab labels in tab bar, got %q", tabBar)
	}
	if strings.Count(tabBar, "●") < 2 {
		t.Fatalf("expected indicators for generating and message state, got %q", tabBar)
	}
	if !strings.Contains(tabBar, "◉") {
		t.Fatalf("expected authorization indicator for tab needing auth, got %q", tabBar)
	}
	if !strings.Contains(tabBar, "[+]") {
		t.Fatalf("expected new tab button in tab bar, got %q", tabBar)
	}
}

func TestRenderTabBarHiddenWithSingleTab(t *testing.T) {
	m := newModelWithTabs(t, 1)

	if tabBar := m.renderTabBar(); tabBar != "" {
		t.Fatalf("expected no tab bar for single tab, got %q", tabBar)
	}
}

func TestCreateDefaultTabInitializesState(t *testing.T) {
	m := New("test-model", "", true)
	m.workingDir = t.TempDir()

	if err := m.createDefaultTab(); err != nil {
		t.Fatalf("createDefaultTab returned error: %v", err)
	}

	if len(m.sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(m.sessions))
	}
	if m.activeSessionIdx != 0 {
		t.Fatalf("expected active session index 0, got %d", m.activeSessionIdx)
	}
	tab := m.sessions[0]
	if tab.ID != 1 || tab.Session == nil {
		t.Fatalf("expected tab ID 1 with session, got ID %d session nil %v", tab.ID, tab.Session == nil)
	}
	if tab.Session.WorkingDir != m.workingDir {
		t.Fatalf("expected session working dir %q, got %q", m.workingDir, tab.Session.WorkingDir)
	}
	if m.sessionIDCounter != 2 {
		t.Fatalf("expected sessionIDCounter 2, got %d", m.sessionIDCounter)
	}
}

func TestRestoreTabsFromConfig(t *testing.T) {
	m := New("test-model", "", true)
	workspaceDir := t.TempDir()
	worktreeDir := t.TempDir()
	m.workingDir = workspaceDir
	m.config = &config.Config{
		OpenTabs: map[string]*config.WorkspaceTabState{
			workspaceDir: {
				ActiveTabID: 6,
				TabIDs:      []int{5, 6},
				TabNames: map[int]string{
					5: "alpha",
				},
				WorktreePaths: map[int]string{
					6: worktreeDir,
				},
			},
		},
	}

	if err := m.restoreTabs(); err != nil {
		t.Fatalf("restoreTabs returned error: %v", err)
	}

	if len(m.sessions) != 2 {
		t.Fatalf("expected 2 restored sessions, got %d", len(m.sessions))
	}
	if m.activeSessionIdx != 1 {
		t.Fatalf("expected active session index 1, got %d", m.activeSessionIdx)
	}

	tab0 := m.sessions[0]
	if tab0.ID != 5 || tab0.Name != "alpha" || tab0.WorktreePath != "" {
		t.Fatalf("unexpected tab0 values: %+v", tab0)
	}
	if tab0.Session.WorkingDir != workspaceDir {
		t.Fatalf("expected tab0 working dir %q, got %q", workspaceDir, tab0.Session.WorkingDir)
	}

	tab1 := m.sessions[1]
	if tab1.ID != 6 || tab1.WorktreePath != worktreeDir {
		t.Fatalf("unexpected tab1 values: %+v", tab1)
	}
	if tab1.Session.WorkingDir != worktreeDir {
		t.Fatalf("expected tab1 working dir %q, got %q", worktreeDir, tab1.Session.WorkingDir)
	}
	if m.sessionIDCounter != 7 {
		t.Fatalf("expected sessionIDCounter 7 after restore, got %d", m.sessionIDCounter)
	}
}

func TestTabSessionHelpers(t *testing.T) {
	ts := &TabSession{ID: 3}

	if ts.DisplayName() != "Tab 3" {
		t.Fatalf("expected default display name 'Tab 3', got %q", ts.DisplayName())
	}
	if ts.HasMessages() {
		t.Fatal("expected HasMessages to be false when empty")
	}

	ts.Name = "custom"
	ts.Messages = []message{{content: "hi"}}
	ts.Generating = true
	ts.WaitingForAuth = true

	if ts.DisplayName() != "custom" {
		t.Fatalf("expected custom display name, got %q", ts.DisplayName())
	}
	if !ts.HasMessages() {
		t.Fatal("expected HasMessages to be true when messages exist")
	}
	if !ts.IsGenerating() {
		t.Fatal("expected IsGenerating to reflect state")
	}
	if !ts.NeedsAuthorization() {
		t.Fatal("expected NeedsAuthorization to reflect auth state")
	}
}
