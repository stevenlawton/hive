package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Tab represents a single tab in the tab bar.
type Tab struct {
	ID       string
	Label    string
	Flashing bool
}

// TabBar manages a row of tabs.
type TabBar struct {
	Tabs      []Tab
	ActiveIdx int
	Width     int
}

// NewTabBar creates an empty tab bar.
func NewTabBar() *TabBar {
	return &TabBar{}
}

// Add appends a new tab.
func (tb *TabBar) Add(id, label string) {
	tb.Tabs = append(tb.Tabs, Tab{ID: id, Label: label})
}

// Remove deletes a tab by ID and adjusts ActiveIdx.
func (tb *TabBar) Remove(id string) {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.Tabs = append(tb.Tabs[:i], tb.Tabs[i+1:]...)
			if tb.ActiveIdx >= len(tb.Tabs) && len(tb.Tabs) > 0 {
				tb.ActiveIdx = len(tb.Tabs) - 1
			}
			return
		}
	}
}

// SetActive sets the active tab by index.
func (tb *TabBar) SetActive(idx int) {
	if idx >= 0 && idx < len(tb.Tabs) {
		tb.ActiveIdx = idx
	}
}

// SetActiveByID sets the active tab by ID.
func (tb *TabBar) SetActiveByID(id string) {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.ActiveIdx = i
			return
		}
	}
}

// FocusOrAdd focuses an existing tab or adds a new one.
func (tb *TabBar) FocusOrAdd(id, label string) {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.ActiveIdx = i
			return
		}
	}
	tb.Add(id, label)
	tb.ActiveIdx = len(tb.Tabs) - 1
}

// FocusByID focuses the tab with the given ID. Returns true if found.
func (tb *TabBar) FocusByID(id string) bool {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.ActiveIdx = i
			return true
		}
	}
	return false
}

// Next moves to the next tab (wrapping).
func (tb *TabBar) Next() {
	if len(tb.Tabs) == 0 {
		return
	}
	tb.ActiveIdx = (tb.ActiveIdx + 1) % len(tb.Tabs)
}

// Prev moves to the previous tab (wrapping).
func (tb *TabBar) Prev() {
	if len(tb.Tabs) == 0 {
		return
	}
	tb.ActiveIdx = (tb.ActiveIdx - 1 + len(tb.Tabs)) % len(tb.Tabs)
}

// ActiveTab returns the currently active tab, or nil if empty.
func (tb *TabBar) ActiveTab() *Tab {
	if len(tb.Tabs) == 0 {
		return nil
	}
	return &tb.Tabs[tb.ActiveIdx]
}

// SetFlashing marks a tab as flashing by ID.
func (tb *TabBar) SetFlashing(id string, flashing bool) {
	for i := range tb.Tabs {
		if tb.Tabs[i].ID == id {
			tb.Tabs[i].Flashing = flashing
			return
		}
	}
}

// View renders the tab bar.
func (tb *TabBar) View() string {
	if len(tb.Tabs) == 0 {
		return ""
	}

	var parts []string
	for i, tab := range tb.Tabs {
		label := " " + tab.Label + " "
		var style lipgloss.Style
		switch {
		case tab.Flashing:
			style = TabFlashStyle
		case i == tb.ActiveIdx:
			style = TabActiveStyle
		default:
			style = TabInactiveStyle
		}
		parts = append(parts, style.Render(label))
	}

	tabs := strings.Join(parts, "")
	remaining := tb.Width - lipgloss.Width(tabs)
	if remaining < 0 {
		remaining = 0
	}
	separator := strings.Repeat("─", remaining)
	return tabs + StatusBarStyle.Render(separator)
}
