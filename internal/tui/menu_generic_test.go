package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// testMenuItem implements MenuItem for testing
type testMenuItem struct {
	title       string
	description string
	filterValue string
}

func (t testMenuItem) Title() string       { return t.title }
func (t testMenuItem) Description() string { return t.description }
func (t testMenuItem) FilterValue() string {
	if t.filterValue != "" {
		return t.filterValue
	}
	return t.title
}

// Helper to create test items
func createTestItems(count int) []MenuItem {
	items := make([]MenuItem, count)
	for i := 0; i < count; i++ {
		items[i] = testMenuItem{
			title:       string(rune('A' + i)),
			description: string(rune('a' + i)),
		}
	}
	return items
}

// TestNewGenericMenu tests menu creation
func TestNewGenericMenu(t *testing.T) {
	config := DefaultMenuConfig()
	config.Title = "Test Menu"

	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	if menu == nil {
		t.Fatal("NewGenericMenu returned nil")
	}

	if menu.config.Title != "Test Menu" {
		t.Errorf("Expected title 'Test Menu', got '%s'", menu.config.Title)
	}

	if menu.quitting {
		t.Error("New menu should not be quitting")
	}

	if menu.selectedItem != nil {
		t.Error("New menu should have no selected item")
	}
}

// TestMenuInit tests menu initialization
func TestMenuInit(t *testing.T) {
	t.Run("without auto-filter", func(t *testing.T) {
		config := DefaultMenuConfig()
		config.StartFiltering = false

		items := createTestItems(3)
		menu := NewGenericMenu(items, config)

		cmd := menu.Init()
		if cmd != nil {
			t.Error("Init should return nil when not auto-filtering")
		}
	})

	t.Run("with auto-filter", func(t *testing.T) {
		config := DefaultMenuConfig()
		config.StartFiltering = true

		items := createTestItems(3)
		menu := NewGenericMenu(items, config)

		cmd := menu.Init()
		if cmd == nil {
			t.Error("Init should return textinput.Blink when auto-filtering")
		}
	})
}

// TestMenuEnterKey tests Enter key selection
func TestMenuEnterKey(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = true // Don't quit on esc to simplify testing

	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Simulate Enter key press
	keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
	updatedModel, cmd := menu.Update(keyMsg)

	menu, ok := updatedModel.(*GenericMenu)
	if !ok {
		t.Fatal("Update should return *GenericMenu")
	}

	if !menu.quitting {
		t.Error("Menu should be quitting after Enter")
	}

	if menu.selectedItem == nil {
		t.Error("Selected item should not be nil")
	}

	// Execute the command to get the message
	if cmd != nil {
		msgs := executeBatchCmd(cmd)
		foundSelectedMsg := false
		for _, msg := range msgs {
			if _, ok := msg.(MenuSelectedMsg); ok {
				foundSelectedMsg = true
				break
			}
		}
		if !foundSelectedMsg {
			t.Error("Expected MenuSelectedMsg from command")
		}
	} else {
		t.Error("Expected command after Enter key")
	}
}

// TestMenuEscKey tests Esc key cancellation
func TestMenuEscKey(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = false

	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Simulate Esc key press
	keyMsg := tea.KeyMsg{Type: tea.KeyEsc}
	updatedModel, cmd := menu.Update(keyMsg)

	menu, ok := updatedModel.(*GenericMenu)
	if !ok {
		t.Fatal("Update should return *GenericMenu")
	}

	if !menu.quitting {
		t.Error("Menu should be quitting after Esc")
	}

	if menu.selectedItem != nil {
		t.Error("Selected item should be nil when cancelled")
	}

	// Check for cancelled message
	if cmd != nil {
		// Execute batch commands
		msgs := executeBatchCmd(cmd)
		foundCancelMsg := false
		for _, msg := range msgs {
			if _, ok := msg.(MenuCancelledMsg); ok {
				foundCancelMsg = true
				break
			}
		}
		if !foundCancelMsg {
			t.Error("Expected MenuCancelledMsg from command")
		}
	}
}

// TestMenuCtrlCKey tests Ctrl+C cancellation
func TestMenuCtrlCKey(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = false

	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Simulate Ctrl+C key press
	keyMsg := tea.KeyMsg{Type: tea.KeyCtrlC}
	updatedModel, cmd := menu.Update(keyMsg)

	menu, ok := updatedModel.(*GenericMenu)
	if !ok {
		t.Fatal("Update should return *GenericMenu")
	}

	if !menu.quitting {
		t.Error("Menu should be quitting after Ctrl+C")
	}

	// Check for cancelled message
	if cmd != nil {
		msgs := executeBatchCmd(cmd)
		foundCancelMsg := false
		for _, msg := range msgs {
			if _, ok := msg.(MenuCancelledMsg); ok {
				foundCancelMsg = true
				break
			}
		}
		if !foundCancelMsg {
			t.Error("Expected MenuCancelledMsg from command")
		}
	}
}

// TestMenuDisabledQuitKeys tests that quit keys are disabled when configured
func TestMenuDisabledQuitKeys(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = true

	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Simulate Esc key press
	keyMsg := tea.KeyMsg{Type: tea.KeyEsc}
	updatedModel, _ := menu.Update(keyMsg)

	menu, ok := updatedModel.(*GenericMenu)
	if !ok {
		t.Fatal("Update should return *GenericMenu")
	}

	// Menu should NOT quit when quit keys are disabled
	// (The key will be passed to the list, which may handle it differently)
	// We just verify the menu structure is intact
	if menu.selectedItem != nil {
		t.Error("Selected item should be nil when quit keys are disabled")
	}
}

// TestMenuWindowResize tests window resize handling
func TestMenuWindowResize(t *testing.T) {
	config := DefaultMenuConfig()
	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Simulate window resize
	resizeMsg := tea.WindowSizeMsg{Width: 100, Height: 30}
	updatedModel, _ := menu.Update(resizeMsg)

	menu, ok := updatedModel.(*GenericMenu)
	if !ok {
		t.Fatal("Update should return *GenericMenu")
	}

	// Menu should still be functional after resize
	if menu.quitting {
		t.Error("Menu should not be quitting after resize")
	}
}

// TestMenuCustomKeyHandler tests custom key handlers
func TestMenuCustomKeyHandler(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = true

	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Set custom handler for 'x' key
	menu.SetCustomKeyHandler("x", func() tea.Msg {
		return struct{ custom bool }{custom: true}
	})

	// Simulate 'x' key press
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	_, cmd := menu.Update(keyMsg)

	// Execute command
	if cmd != nil {
		msg := cmd()
		if customMsg, ok := msg.(struct{ custom bool }); !ok || !customMsg.custom {
			t.Error("Custom key handler should return custom message")
		}
	} else {
		t.Error("Custom key handler should return a command")
	}
}

// TestMenuSetItems tests updating menu items
func TestMenuSetItems(t *testing.T) {
	config := DefaultMenuConfig()
	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Update items
	newItems := createTestItems(5)
	menu.SetItems(newItems)

	// Verify menu still works
	if menu.quitting {
		t.Error("Menu should not be quitting after SetItems")
	}
}

// TestMenuSetSize tests updating menu size
func TestMenuSetSize(t *testing.T) {
	config := DefaultMenuConfig()
	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	originalWidth := menu.config.Width
	originalHeight := menu.config.Height

	// Update size
	newWidth := 120
	newHeight := 40
	menu.SetSize(newWidth, newHeight)

	if menu.config.Width != newWidth {
		t.Errorf("Expected width %d, got %d", newWidth, menu.config.Width)
	}

	if menu.config.Height != newHeight {
		t.Errorf("Expected height %d, got %d", newHeight, menu.config.Height)
	}

	if menu.config.Width == originalWidth && menu.config.Height == originalHeight {
		t.Error("Menu size should have changed")
	}
}

// TestMenuGetSelectedItem tests retrieving selected item
func TestMenuGetSelectedItem(t *testing.T) {
	config := DefaultMenuConfig()
	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Initially no selection
	if menu.GetSelectedItem() != nil {
		t.Error("Initially selected item should be nil")
	}

	// Simulate Enter to select
	keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
	menu.Update(keyMsg)

	// Now should have selection
	selected := menu.GetSelectedItem()
	if selected == nil {
		t.Error("Selected item should not be nil after Enter")
	}

	if selected.Title() != "A" {
		t.Errorf("Expected first item 'A', got '%s'", selected.Title())
	}
}

// TestMenuView tests view rendering doesn't panic
func TestMenuView(t *testing.T) {
	config := DefaultMenuConfig()
	config.HelpText = "Test help"
	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Should not panic
	view := menu.View()
	if view == "" {
		t.Error("View should not be empty")
	}

	// After quitting with selection, view should be empty
	menu.quitting = true
	menu.selectedItem = items[0]
	view = menu.View()
	if view != "" {
		t.Error("View should be empty when quitting with selection")
	}
}

// TestMenuViewEmptyItems tests view rendering with empty items doesn't panic
func TestMenuViewEmptyItems(t *testing.T) {
	config := DefaultMenuConfig()
	config.Title = "Empty Menu"
	config.HelpText = "Test help"

	// Create menu with empty items - this should not panic
	menu := NewGenericMenu([]MenuItem{}, config)

	// View should show empty message instead of crashing
	view := menu.View()
	if view == "" {
		t.Error("View should not be empty even with no items")
	}
	if !contains(view, "No items available") {
		t.Error("View should show 'No items available' message")
	}

	// Update with window size should also not panic
	resizeMsg := tea.WindowSizeMsg{Width: 80, Height: 24}
	updatedModel, _ := menu.Update(resizeMsg)
	menu = updatedModel.(*GenericMenu)

	// View after resize should still work
	view = menu.View()
	if view == "" {
		t.Error("View should not be empty after resize")
	}
}

// TestMenuSetItemsEmpty tests setting empty items doesn't panic
func TestMenuSetItemsEmpty(t *testing.T) {
	config := DefaultMenuConfig()
	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Set empty items - should not panic
	menu.SetItems([]MenuItem{})

	// View should show empty message
	view := menu.View()
	if !contains(view, "No items available") {
		t.Error("View should show 'No items available' after SetItems with empty slice")
	}
}

// TestDefaultMenuConfig tests default config values
func TestDefaultMenuConfig(t *testing.T) {
	config := DefaultMenuConfig()

	if config.Width != 80 {
		t.Errorf("Expected default width 80, got %d", config.Width)
	}

	if config.Height != 20 {
		t.Errorf("Expected default height 20, got %d", config.Height)
	}

	if !config.EnableFiltering {
		t.Error("Expected filtering to be enabled by default")
	}

	if config.StartFiltering {
		t.Error("Expected start filtering to be false by default")
	}

	if !config.ShowStatusBar {
		t.Error("Expected status bar to be shown by default")
	}

	if config.DisableQuitKeys {
		t.Error("Expected quit keys to be enabled by default")
	}

	if config.HelpText == "" {
		t.Error("Expected default help text")
	}
}

// TestMenuItemInterface tests that testMenuItem implements MenuItem
func TestMenuItemInterface(t *testing.T) {
	var _ MenuItem = testMenuItem{}

	item := testMenuItem{
		title:       "Test",
		description: "Description",
		filterValue: "filter",
	}

	if item.Title() != "Test" {
		t.Errorf("Expected title 'Test', got '%s'", item.Title())
	}

	if item.Description() != "Description" {
		t.Errorf("Expected description 'Description', got '%s'", item.Description())
	}

	if item.FilterValue() != "filter" {
		t.Errorf("Expected filter value 'filter', got '%s'", item.FilterValue())
	}
}

// TestMenuArrowKeyNavigation tests that arrow keys navigate through items
func TestMenuArrowKeyNavigation(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = true

	items := createTestItems(5) // Create 5 items: A, B, C, D, E
	menu := NewGenericMenu(items, config)

	t.Run("down arrow moves selection down", func(t *testing.T) {
		// Start at first item (index 0)
		initialIndex := menu.list.Index()
		if initialIndex != 0 {
			t.Errorf("Expected initial index 0, got %d", initialIndex)
		}

		// Press down arrow
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updatedModel, _ := menu.Update(keyMsg)
		menu = updatedModel.(*GenericMenu)

		// Should be at second item (index 1)
		newIndex := menu.list.Index()
		if newIndex != 1 {
			t.Errorf("Expected index 1 after down arrow, got %d", newIndex)
		}
	})

	t.Run("up arrow moves selection up", func(t *testing.T) {
		// We're at index 1 from previous test
		currentIndex := menu.list.Index()
		if currentIndex != 1 {
			t.Logf("Starting from index %d", currentIndex)
		}

		// Press up arrow
		keyMsg := tea.KeyMsg{Type: tea.KeyUp}
		updatedModel, _ := menu.Update(keyMsg)
		menu = updatedModel.(*GenericMenu)

		// Should be back at first item (index 0)
		newIndex := menu.list.Index()
		if newIndex != 0 {
			t.Errorf("Expected index 0 after up arrow, got %d", newIndex)
		}
	})

	t.Run("multiple down arrows navigate correctly", func(t *testing.T) {
		// Reset menu
		menu = NewGenericMenu(items, config)

		// Press down arrow 3 times
		for i := 0; i < 3; i++ {
			keyMsg := tea.KeyMsg{Type: tea.KeyDown}
			updatedModel, _ := menu.Update(keyMsg)
			menu = updatedModel.(*GenericMenu)
		}

		// Should be at index 3 (item D)
		finalIndex := menu.list.Index()
		if finalIndex != 3 {
			t.Errorf("Expected index 3 after 3 down arrows, got %d", finalIndex)
		}
	})

	t.Run("down arrow at bottom stays at bottom", func(t *testing.T) {
		// Reset menu and navigate to last item
		menu = NewGenericMenu(items, config)

		// Navigate to last item (index 4)
		for i := 0; i < 4; i++ {
			keyMsg := tea.KeyMsg{Type: tea.KeyDown}
			updatedModel, _ := menu.Update(keyMsg)
			menu = updatedModel.(*GenericMenu)
		}

		lastIndex := menu.list.Index()
		if lastIndex != 4 {
			t.Errorf("Expected index 4 at last item, got %d", lastIndex)
		}

		// Press down arrow again
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updatedModel, _ := menu.Update(keyMsg)
		menu = updatedModel.(*GenericMenu)

		// Should still be at last item
		stillLastIndex := menu.list.Index()
		if stillLastIndex != 4 {
			t.Errorf("Expected to stay at index 4, got %d", stillLastIndex)
		}
	})

	t.Run("up arrow at top stays at top", func(t *testing.T) {
		// Reset menu (starts at index 0)
		menu = NewGenericMenu(items, config)

		initialIndex := menu.list.Index()
		if initialIndex != 0 {
			t.Errorf("Expected initial index 0, got %d", initialIndex)
		}

		// Press up arrow
		keyMsg := tea.KeyMsg{Type: tea.KeyUp}
		updatedModel, _ := menu.Update(keyMsg)
		menu = updatedModel.(*GenericMenu)

		// Should still be at first item
		stillFirstIndex := menu.list.Index()
		if stillFirstIndex != 0 {
			t.Errorf("Expected to stay at index 0, got %d", stillFirstIndex)
		}
	})
}

// TestMenuArrowKeyWithSelection tests that selection works after navigation
func TestMenuArrowKeyWithSelection(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = true

	items := createTestItems(3)
	menu := NewGenericMenu(items, config)

	// Navigate down to second item
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	updatedModel, _ := menu.Update(downMsg)
	menu = updatedModel.(*GenericMenu)

	// Verify we're at index 1
	if menu.list.Index() != 1 {
		t.Errorf("Expected index 1, got %d", menu.list.Index())
	}

	// Select current item with Enter
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	updatedModel, _ = menu.Update(enterMsg)
	menu = updatedModel.(*GenericMenu)

	// Verify correct item was selected
	selected := menu.GetSelectedItem()
	if selected == nil {
		t.Fatal("Expected selected item, got nil")
	}

	// Should be item B (second item)
	if selected.Title() != "B" {
		t.Errorf("Expected selected item 'B', got '%s'", selected.Title())
	}
}

// TestMenuPageNavigation tests page up/down navigation
func TestMenuPageNavigation(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = true

	// Create many items for page navigation
	items := make([]MenuItem, 20)
	for i := 0; i < 20; i++ {
		items[i] = testMenuItem{
			title:       fmt.Sprintf("Item %d", i),
			description: fmt.Sprintf("Description %d", i),
		}
	}

	menu := NewGenericMenu(items, config)

	t.Run("page down navigates multiple items", func(t *testing.T) {
		initialIndex := menu.list.Index()

		// Press page down
		keyMsg := tea.KeyMsg{Type: tea.KeyPgDown}
		updatedModel, _ := menu.Update(keyMsg)
		menu = updatedModel.(*GenericMenu)

		newIndex := menu.list.Index()

		// Should have moved down (exact amount depends on page size)
		if newIndex <= initialIndex {
			t.Errorf("Expected to move down from index %d, still at %d", initialIndex, newIndex)
		}
	})

	t.Run("page up navigates multiple items", func(t *testing.T) {
		// We're somewhere in the middle from previous test
		currentIndex := menu.list.Index()

		// Press page up
		keyMsg := tea.KeyMsg{Type: tea.KeyPgUp}
		updatedModel, _ := menu.Update(keyMsg)
		menu = updatedModel.(*GenericMenu)

		newIndex := menu.list.Index()

		// Should have moved up
		if newIndex >= currentIndex {
			t.Errorf("Expected to move up from index %d, at %d", currentIndex, newIndex)
		}
	})
}

// TestMenuNavigationDoesNotQuit tests that navigation keys don't quit
func TestMenuNavigationDoesNotQuit(t *testing.T) {
	config := DefaultMenuConfig()
	config.DisableQuitKeys = false // Quit keys enabled

	items := createTestItems(5)
	menu := NewGenericMenu(items, config)

	// Test various navigation keys
	navigationKeys := []tea.KeyMsg{
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyPgDown},
	}

	for _, keyMsg := range navigationKeys {
		updatedModel, _ := menu.Update(keyMsg)
		menu = updatedModel.(*GenericMenu)

		if menu.quitting {
			t.Errorf("Menu should not quit on navigation key: %v", keyMsg.Type)
		}
	}
}

// Helper function to execute batch commands and collect messages
func executeBatchCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if batchMsg, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batchMsg {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
		return msgs
	}

	return []tea.Msg{msg}
}
