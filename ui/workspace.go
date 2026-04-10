package ui

import (
	"strings"
)

// HomeTabID is the reserved ID for the "main page" tab.
const HomeTabID = "__home__"

// BusTabID is the reserved ID for the message bus tab.
const BusTabID = "__bus__"

// WorkspaceTab holds the splits for one project tab.
type WorkspaceTab struct {
	ID        string
	Label     string
	SplitPane *SplitPane
}

// WorkspaceView manages the tab bar and split panes.
type WorkspaceView struct {
	TabBar *TabBar
	Tabs   map[string]*WorkspaceTab
	Width  int
	Height int
}

// NewWorkspaceView creates an empty workspace view.
func NewWorkspaceView() *WorkspaceView {
	wv := &WorkspaceView{
		TabBar: NewTabBar(),
		Tabs:   make(map[string]*WorkspaceTab),
	}
	// Add the persistent home and bus tabs as the first entries.
	wv.TabBar.Add(HomeTabID, "⚡")
	wv.TabBar.Add(BusTabID, "📬 bus")
	return wv
}

// IsBusActive returns true when the bus tab is selected.
func (wv *WorkspaceView) IsBusActive() bool {
	tab := wv.TabBar.ActiveTab()
	return tab != nil && tab.ID == BusTabID
}

// SetSize updates layout dimensions.
func (wv *WorkspaceView) SetSize(w, h int) {
	wv.Width = w
	wv.Height = h
	wv.TabBar.Width = w

	tabBarHeight := 1
	statusBarHeight := 1
	contentHeight := h - tabBarHeight - statusBarHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	for _, tab := range wv.Tabs {
		tab.SplitPane.SetSize(w, contentHeight)
	}
}

// OpenTab creates or focuses a tab, adding an initial split.
func (wv *WorkspaceView) OpenTab(id, label, sessionName, splitLabel string) {
	if _, exists := wv.Tabs[id]; !exists {
		tab := &WorkspaceTab{
			ID:        id,
			Label:     label,
			SplitPane: NewSplitPane(),
		}
		tabBarHeight := 1
		statusBarHeight := 1
		contentHeight := wv.Height - tabBarHeight - statusBarHeight
		if contentHeight < 1 {
			contentHeight = 1
		}
		tab.SplitPane.SetSize(wv.Width, contentHeight)
		tab.SplitPane.AddSplit(splitLabel, sessionName)
		wv.Tabs[id] = tab
	}
	wv.TabBar.FocusOrAdd(id, label)
}

// CloseTab removes a tab. The home tab cannot be closed.
func (wv *WorkspaceView) CloseTab(id string) {
	if id == HomeTabID {
		return
	}
	delete(wv.Tabs, id)
	wv.TabBar.Remove(id)
}

// ActiveTab returns the current tab, or nil.
func (wv *WorkspaceView) ActiveTab() *WorkspaceTab {
	tab := wv.TabBar.ActiveTab()
	if tab == nil {
		return nil
	}
	return wv.Tabs[tab.ID]
}

// IsHomeActive returns true when the home tab is the selected tab.
func (wv *WorkspaceView) IsHomeActive() bool {
	tab := wv.TabBar.ActiveTab()
	return tab != nil && tab.ID == HomeTabID
}

// AddSplitToActive adds a split to the currently active tab.
func (wv *WorkspaceView) AddSplitToActive(label, sessionName string) {
	tab := wv.ActiveTab()
	if tab == nil {
		return
	}
	tab.SplitPane.AddSplit(label, sessionName)
}

// FocusedSessionName returns the session name of the focused split.
func (wv *WorkspaceView) FocusedSessionName() string {
	tab := wv.ActiveTab()
	if tab == nil {
		return ""
	}
	split := tab.SplitPane.FocusedSplit()
	if split == nil {
		return ""
	}
	return split.SessionName
}

// AllSessionNames returns all session names across all tabs.
func (wv *WorkspaceView) AllSessionNames() []string {
	var names []string
	for _, tab := range wv.Tabs {
		names = append(names, tab.SplitPane.SessionNames()...)
	}
	return names
}

// View renders the workspace view.
func (wv *WorkspaceView) View(statusBar string) string {
	tabBar := wv.TabBar.View()

	tab := wv.ActiveTab()
	var content string
	if tab != nil {
		content = tab.SplitPane.View()
	} else {
		content = "No tabs open"
	}

	var lines []string
	lines = append(lines, tabBar)
	lines = append(lines, content)
	lines = append(lines, statusBar)
	return strings.Join(lines, "\n")
}
