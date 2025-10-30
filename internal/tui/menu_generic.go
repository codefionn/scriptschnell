package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MenuItem represents a generic menu item that can be displayed in a list
type MenuItem interface {
	list.Item
	Title() string
	Description() string
}

// MenuConfig holds configuration for a generic menu
type MenuConfig struct {
	Title             string
	Width             int
	Height            int
	EnableFiltering   bool
	StartFiltering    bool
	ShowStatusBar     bool
	DisableQuitKeys   bool
	HelpText          string
	TitleStyle        lipgloss.Style
	ItemStyle         lipgloss.Style
	SelectedItemStyle lipgloss.Style
	DescStyle         lipgloss.Style
	HelpStyle         lipgloss.Style
}

// DefaultMenuConfig returns a sensible default configuration
func DefaultMenuConfig() MenuConfig {
	return MenuConfig{
		Width:             80,
		Height:            20,
		EnableFiltering:   true,
		StartFiltering:    false,
		ShowStatusBar:     true,
		DisableQuitKeys:   false,
		HelpText:          "↑/↓: Navigate • Enter: Select • Esc: Cancel",
		TitleStyle:        lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1),
		ItemStyle:         lipgloss.NewStyle().PaddingLeft(4),
		SelectedItemStyle: lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170")),
		DescStyle:         lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		HelpStyle:         lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
	}
}

// genericItemDelegate renders menu items
type genericItemDelegate struct {
	itemStyle         lipgloss.Style
	selectedItemStyle lipgloss.Style
	descStyle         lipgloss.Style
}

func (d genericItemDelegate) Height() int  { return 2 }
func (d genericItemDelegate) Spacing() int { return 1 }
func (d genericItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd {
	return nil
}

func (d genericItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(MenuItem)
	if !ok {
		return
	}

	var title string
	if index == m.Index() {
		title = d.selectedItemStyle.Render(fmt.Sprintf("▸ %s", item.Title()))
	} else {
		title = d.itemStyle.Render(fmt.Sprintf("  %s", item.Title()))
	}

	desc := d.itemStyle.Render(d.descStyle.Render(item.Description()))
	fmt.Fprintf(w, "%s\n%s", title, desc)
}

// GenericMenu is a reusable menu component
type GenericMenu struct {
	list         list.Model
	config       MenuConfig
	selectedItem MenuItem
	quitting     bool
	startFilter  bool
	customKeyMap map[string]func() tea.Msg
}

// MenuSelectedMsg is sent when a menu item is selected
type MenuSelectedMsg struct {
	Item MenuItem
}

// MenuCancelledMsg is sent when the menu is cancelled
type MenuCancelledMsg struct{}

// NewGenericMenu creates a new generic menu with the given items and config
func NewGenericMenu(items []MenuItem, config MenuConfig) *GenericMenu {
	// Convert MenuItem to list.Item
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}

	// Create delegate with styles from config
	delegate := genericItemDelegate{
		itemStyle:         config.ItemStyle,
		selectedItemStyle: config.SelectedItemStyle,
		descStyle:         config.DescStyle,
	}

	// Create list
	l := list.New(listItems, delegate, config.Width, config.Height-4)
	l.Title = config.Title
	l.Styles.Title = config.TitleStyle
	l.SetShowStatusBar(config.ShowStatusBar)
	l.SetFilteringEnabled(config.EnableFiltering)

	if config.DisableQuitKeys {
		l.DisableQuitKeybindings()
	}

	// Configure filter keybindings
	l.KeyMap.Filter = key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "filter"),
	)
	l.KeyMap.ClearFilter = key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "clear filter"),
	)

	return &GenericMenu{
		list:         l,
		config:       config,
		startFilter:  config.StartFiltering,
		customKeyMap: make(map[string]func() tea.Msg),
	}
}

// SetCustomKeyHandler sets a custom handler for a specific key
func (m *GenericMenu) SetCustomKeyHandler(key string, handler func() tea.Msg) {
	m.customKeyMap[key] = handler
}

// Init initializes the menu
func (m *GenericMenu) Init() tea.Cmd {
	if m.startFilter {
		return textinput.Blink
	}
	return nil
}

// Update handles messages
func (m *GenericMenu) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Auto-start filtering if configured
	if m.startFilter {
		m.startFilter = false
		m.list.SetFilterText("")
		m.list.FilterInput.SetValue("")
		m.list.SetFilterState(list.Filtering)
		cmds = append(cmds, textinput.Blink)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 4)

	case tea.KeyMsg:
		key := msg.String()

		// Check custom key handlers first
		if handler, ok := m.customKeyMap[key]; ok {
			return m, func() tea.Msg { return handler() }
		}

		// Handle quit keys
		if !m.config.DisableQuitKeys && (key == "ctrl+c" || key == "ctrl+q" || key == "esc") {
			m.quitting = true
			return m, tea.Batch(
				tea.Quit,
				func() tea.Msg { return MenuCancelledMsg{} },
			)
		}

		// Arrow keys should always navigate, even when filtering
		// Call the cursor methods directly to bypass filter input capture
		if key == "up" {
			m.list.CursorUp()
			return m, nil
		}
		if key == "down" {
			m.list.CursorDown()
			return m, nil
		}
		if key == "pgup" {
			m.list.PrevPage()
			return m, nil
		}
		if key == "pgdown" {
			m.list.NextPage()
			return m, nil
		}

		// Handle Enter key - select item
		if key == "enter" {
			if item, ok := m.list.SelectedItem().(MenuItem); ok {
				m.selectedItem = item
				m.quitting = true
				return m, tea.Batch(
					tea.Quit,
					func() tea.Msg { return MenuSelectedMsg{Item: item} },
				)
			}
		}

		// All other keys fall through to list.Update
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// View renders the menu
func (m *GenericMenu) View() string {
	if m.quitting && m.selectedItem != nil {
		return ""
	}

	view := m.list.View()
	if m.config.HelpText != "" {
		view += "\n" + m.config.HelpStyle.Render(m.config.HelpText)
	}
	return view
}

// GetSelectedItem returns the selected menu item
func (m *GenericMenu) GetSelectedItem() MenuItem {
	return m.selectedItem
}

// SetItems updates the menu items
func (m *GenericMenu) SetItems(items []MenuItem) {
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}
	m.list.SetItems(listItems)
}

// SetSize updates the menu dimensions
func (m *GenericMenu) SetSize(width, height int) {
	m.config.Width = width
	m.config.Height = height
	m.list.SetWidth(width)
	m.list.SetHeight(height - 4)
}
