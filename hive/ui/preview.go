package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// PreviewTab represents which tab is active in the preview pane.
type PreviewTab int

const (
	PreviewTabTerminal PreviewTab = iota
	PreviewTabDiff
)

// PreviewPane shows a read-only preview of a session with tab switching.
type PreviewPane struct {
	Terminal  *TerminalPane
	DiffView *DiffPane
	ActiveTab PreviewTab
	Width     int
	Height    int
}

// NewPreviewPane creates a new preview pane.
func NewPreviewPane() *PreviewPane {
	return &PreviewPane{
		Terminal:  NewTerminalPane(""),
		DiffView: NewDiffPane(),
		ActiveTab: PreviewTabTerminal,
	}
}

// SetSize updates dimensions for the preview and its children.
func (p *PreviewPane) SetSize(w, h int) {
	p.Width = w
	p.Height = h
	tabBarHeight := 1
	contentHeight := h - tabBarHeight
	if contentHeight < 1 {
		contentHeight = 1
	}
	p.Terminal.SetSize(w, contentHeight)
	p.DiffView.SetSize(w, contentHeight)
}

// SetSession updates which tmux session is being previewed.
func (p *PreviewPane) SetSession(sessionName string) {
	p.Terminal.SessionName = sessionName
}

// ToggleTab switches between Preview and Diff tabs.
func (p *PreviewPane) ToggleTab() {
	if p.ActiveTab == PreviewTabTerminal {
		p.ActiveTab = PreviewTabDiff
	} else {
		p.ActiveTab = PreviewTabTerminal
	}
}

// View renders the preview pane with tab bar.
func (p *PreviewPane) View() string {
	tabBar := p.renderTabBar()
	var content string
	switch p.ActiveTab {
	case PreviewTabTerminal:
		content = p.Terminal.View()
	case PreviewTabDiff:
		content = p.DiffView.View()
	}
	return tabBar + "\n" + content
}

func (p *PreviewPane) renderTabBar() string {
	previewLabel := "Preview"
	diffLabel := "Diff"

	var previewStyle, diffStyle lipgloss.Style
	if p.ActiveTab == PreviewTabTerminal {
		previewStyle = TabActiveStyle
		diffStyle = TabInactiveStyle
	} else {
		previewStyle = TabInactiveStyle
		diffStyle = TabActiveStyle
	}

	tabs := previewStyle.Render("["+previewLabel+"]") + " " + diffStyle.Render("["+diffLabel+"]")
	remaining := p.Width - lipgloss.Width(tabs)
	if remaining < 0 {
		remaining = 0
	}
	separator := strings.Repeat("─", remaining)
	return tabs + StatusBarStyle.Render(separator)
}
