package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stevenlawton/hive/ui"
)

// The resize chord (^Space s) only acts on a tab with more than one split, so
// it is offered on exactly the same terms as o:orient.
func TestChordHintsOfferResizeWhenSplit(t *testing.T) {
	m := newHintTestModel(2)
	got := m.renderWorkspaceStatusBar()
	if !strings.Contains(got, "s:resize") {
		t.Errorf("chord hints omit s:resize with 2 splits:\n%s", got)
	}
	if !strings.Contains(got, "o:orient") {
		t.Errorf("chord hints omit o:orient with 2 splits:\n%s", got)
	}
}

func TestChordHintsHideResizeWithOneSplit(t *testing.T) {
	m := newHintTestModel(1)
	got := m.renderWorkspaceStatusBar()
	if strings.Contains(got, "s:resize") {
		t.Errorf("s:resize offered with a single split, where it does nothing:\n%s", got)
	}
}

// newHintTestModel builds a workspace model with n splits on one tab and the
// chord already pending, so the status bar renders its stage-2 key list.
func newHintTestModel(splits int) model {
	m := model{workspace: ui.NewWorkspaceView(), chord: NewChordHandler(time.Second), width: 200}
	m.workspace.OpenTab("proj", "proj", "hive-proj", "main")
	for i := 1; i < splits; i++ {
		m.workspace.AddSplitToActive(fmt.Sprintf("wt:%d", i), fmt.Sprintf("hive-proj-wt-%d", i))
	}
	m.chord.Start()
	return m
}
