package main

import (
	"testing"

	"github.com/stevenlawton/hive/ui"
)

func newHoverTestModel(t *testing.T) (*model, *ui.WorkspaceTab) {
	t.Helper()
	m := &model{mode: viewWorkspace, width: 80, height: 24, draggingDivider: -1}
	m.workspace = ui.NewWorkspaceView()
	m.workspace.SetSize(80, 24)
	m.workspace.OpenTab("repo", "repo", "ses-a", "a")
	tab := m.workspace.ActiveTab()
	if tab == nil {
		t.Fatal("expected an active tab")
	}
	tab.SplitPane.AddSplit("b", "ses-b")
	return m, tab
}

func TestDividerHoverMsgTracksAndClears(t *testing.T) {
	m, tab := newHoverTestModel(t)

	if _, _ = m.Update(dividerHoverMsg{index: 0}); tab.SplitPane.HoverDivider != 0 {
		t.Fatalf("got %d, want 0", tab.SplitPane.HoverDivider)
	}
	if _, _ = m.Update(dividerHoverMsg{index: -1}); tab.SplitPane.HoverDivider != -1 {
		t.Errorf("got %d, want -1 once the pointer leaves the divider", tab.SplitPane.HoverDivider)
	}
}

// A press keeps the divider lit for the whole drag: motion is consumed by the
// drag, so nothing else would set it.
func TestDividerPressLightsTheDivider(t *testing.T) {
	m, tab := newHoverTestModel(t)

	if _, _ = m.Update(dividerPressMsg{index: 0}); tab.SplitPane.HoverDivider != 0 {
		t.Errorf("got %d, want 0", tab.SplitPane.HoverDivider)
	}
}

func TestDividerHoverClearsOnTabSwitch(t *testing.T) {
	m, tab := newHoverTestModel(t)
	m.Update(dividerHoverMsg{index: 0})

	m.syncModeFromActiveTab()

	if tab.SplitPane.HoverDivider != -1 {
		t.Errorf("got %d, want -1: hover state should not survive a tab switch", tab.SplitPane.HoverDivider)
	}
}
