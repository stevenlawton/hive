package main

import (
	"strings"
	"testing"

	"github.com/stevenlawton/hive/ui"
)

// Up/down are bound in the chord table, so they must move focus too — in a
// stacked layout they are the natural keys and left/right are the odd ones.
func TestChordFocusUpDownMovesFocus(t *testing.T) {
	m := newHintTestModel(3)
	tab := m.workspace.ActiveTab()
	tab.SplitPane.FocusIdx = 0

	m.handleChordAction(ChordFocusDown)
	if got := tab.SplitPane.FocusIdx; got != 1 {
		t.Errorf("focus down: got idx %d, want 1", got)
	}
	m.handleChordAction(ChordFocusDown)
	if got := tab.SplitPane.FocusIdx; got != 2 {
		t.Errorf("focus down twice: got idx %d, want 2", got)
	}
	m.handleChordAction(ChordFocusUp)
	if got := tab.SplitPane.FocusIdx; got != 1 {
		t.Errorf("focus up: got idx %d, want 1", got)
	}
}

func TestChordFocusUpDownStopsAtEnds(t *testing.T) {
	m := newHintTestModel(2)
	tab := m.workspace.ActiveTab()

	tab.SplitPane.FocusIdx = 0
	m.handleChordAction(ChordFocusUp)
	if got := tab.SplitPane.FocusIdx; got != 0 {
		t.Errorf("focus up at the top: got idx %d, want 0", got)
	}
	tab.SplitPane.FocusIdx = 1
	m.handleChordAction(ChordFocusDown)
	if got := tab.SplitPane.FocusIdx; got != 1 {
		t.Errorf("focus down at the bottom: got idx %d, want 1", got)
	}
}

// The hint must name the keys that match the axis panes are laid out on.
func TestChordHintFocusArrowsFollowOrientation(t *testing.T) {
	m := newHintTestModel(2)
	m.workspace.ActiveTab().SplitPane.Orientation = ui.SplitVertical
	if got := m.renderWorkspaceStatusBar(); !strings.Contains(got, "←→:focus") {
		t.Errorf("side-by-side split should hint ←→:\n%s", got)
	}

	m.workspace.ActiveTab().SplitPane.Orientation = ui.SplitHorizontal
	got := m.renderWorkspaceStatusBar()
	if !strings.Contains(got, "↑↓:focus") {
		t.Errorf("stacked split should hint ↑↓:\n%s", got)
	}
	if strings.Contains(got, "←→:focus") {
		t.Errorf("stacked split should not hint ←→:\n%s", got)
	}
}

func TestChordHintsOfferDrawer(t *testing.T) {
	if got := newHintTestModel(1).renderWorkspaceStatusBar(); !strings.Contains(got, "t:drawer") {
		t.Errorf("chord hints omit t:drawer:\n%s", got)
	}
}
