package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/config"
	"github.com/codefionn/scriptschnell/internal/tools"
)

func TestMCPMenuOpenAPIInputDoesNotSkipKeystrokes(t *testing.T) {
	cfg := config.DefaultConfig()
	menu := NewMCPMenu(cfg, 80, 20, func(string, bool) (string, error) { return "", nil })

	updated, _ := menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	var ok bool
	if menu, ok = updated.(*MCPMenuModel); !ok {
		t.Fatalf("expected *MCPMenuModel after enter, got %T", updated)
	}

	if !menu.inputMode {
		t.Fatal("menu should be in input mode after selecting add-openapi")
	}

	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'a'}},
		{Type: tea.KeyRunes, Runes: []rune{'b'}},
		{Type: tea.KeyRunes, Runes: []rune{'c'}},
	}

	for _, key := range keys {
		updated, _ = menu.Update(key)
		if menu, ok = updated.(*MCPMenuModel); !ok {
			t.Fatalf("expected *MCPMenuModel after rune input, got %T", updated)
		}
	}

	if got := menu.inputs[0].Value(); got != "abc" {
		t.Fatalf("expected first input value 'abc', got %q", got)
	}
	if menu.inputIndex != 0 {
		t.Fatalf("expected focus to remain on first input, got %d", menu.inputIndex)
	}
}

func TestMCPMenuPersistOpenAPIServer(t *testing.T) {
	cfg := config.DefaultConfig()
	var persisted bool
	menu := NewMCPMenu(cfg, 80, 20, func(name string, validate bool) (string, error) {
		persisted = true
		if validate {
			return "ok", nil
		}
		return "", nil
	})

	updated, _ := menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	var ok bool
	if menu, ok = updated.(*MCPMenuModel); !ok {
		t.Fatalf("expected *MCPMenuModel after enter, got %T", updated)
	}

	values := []string{
		"weather",                 // name
		"./specs/weather.yaml",    // spec
		"https://api.example.com", // url
		"token123",                // bearer token
		"WEATHER_TOKEN",           // bearer env
	}

	if len(menu.inputs) != len(values) {
		t.Fatalf("expected %d input fields, got %d", len(values), len(menu.inputs))
	}

	for i, val := range values {
		ti := menu.inputs[i]
		ti.SetValue(val)
		menu.inputs[i] = ti
		if i < len(values)-1 {
			updated, _ = menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if menu, ok = updated.(*MCPMenuModel); !ok {
				t.Fatalf("expected *MCPMenuModel after enter, got %T", updated)
			}
		}
	}

	updated, _ = menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if menu, ok = updated.(*MCPMenuModel); !ok {
		t.Fatalf("expected *MCPMenuModel after final enter, got %T", updated)
	}

	if !persisted {
		t.Fatalf("expected persist function to be called")
	}

	if menu.status != "ok" {
		t.Fatalf("expected success status message 'ok', got %q", menu.status)
	}

	server, ok := cfg.MCP.Servers["weather"]
	if !ok {
		t.Fatalf("expected server 'weather' to be stored in config")
	}
	if server.OpenAPI == nil {
		t.Fatalf("expected OpenAPI configuration to be set")
	}
	if server.OpenAPI.URL != "https://api.example.com" {
		t.Fatalf("expected URL to be 'https://api.example.com', got %q", server.OpenAPI.URL)
	}
	if server.OpenAPI.AuthBearerToken != "token123" {
		t.Fatalf("expected bearer token to be stored, got %q", server.OpenAPI.AuthBearerToken)
	}
	if server.OpenAPI.AuthBearerEnv != "WEATHER_TOKEN" {
		t.Fatalf("expected bearer env to be stored, got %q", server.OpenAPI.AuthBearerEnv)
	}
}

type fakeTodoActorRef struct {
	list *tools.TodoList
}

func (f *fakeTodoActorRef) Send(msg actor.Message) error {
	switch m := msg.(type) {
	case tools.TodoListMsg:
		if f.list == nil {
			m.ResponseChan <- &tools.TodoList{}
			return nil
		}
		items := make([]*tools.TodoItem, len(f.list.Items))
		copy(items, f.list.Items)
		m.ResponseChan <- &tools.TodoList{Items: items}
	default:
		// ignore other message types for tests
	}
	return nil
}

func TestRenderTodoPanelShowsEnabledMCPServers(t *testing.T) {
	cfg := config.DefaultConfig()

	actorRef := &fakeTodoActorRef{list: &tools.TodoList{Items: []*tools.TodoItem{}}}
	todoClient := tools.NewTodoActorClient(actorRef)

	model := New("model", "", false)
	model.SetConfig(cfg)
	model.todoClient = todoClient
	model.SetActiveMCPProvider(func() []string { return []string{"alpha", "gamma"} })

	panel := model.renderTodoPanel()
	if panel == "" {
		t.Fatal("expected todo panel output")
	}
	if !strings.Contains(panel, "MCP Servers") {
		t.Fatalf("expected panel to include MCP Servers section, got: %s", panel)
	}
	if !strings.Contains(panel, "alpha") {
		t.Fatalf("expected enabled server 'alpha' to be listed: %s", panel)
	}
	if !strings.Contains(panel, "gamma") {
		t.Fatalf("expected enabled server 'gamma' to be listed: %s", panel)
	}
}
