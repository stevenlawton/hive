package main

import (
	"strings"
	"testing"
)

func TestChordCResolvesToTheCannedMenu(t *testing.T) {
	c := NewChordHandler(0)
	c.Start()

	action, ok := c.Complete("c")

	if !ok || action != ChordCannedMenu {
		t.Errorf("got (%v, %v), want (ChordCannedMenu, true)", action, ok)
	}
}

func TestWorkspaceChordHintsListTheCannedMenu(t *testing.T) {
	if got := newHintTestModel(1).renderWorkspaceStatusBar(); !strings.Contains(got, "c:canned") {
		t.Errorf("chord hints should mention the canned menu:\n%s", got)
	}
}

func TestChordCannedMenuOpensOnTheFocusedSession(t *testing.T) {
	m := newHintTestModel(1)
	m.height, m.width = 40, 200
	m.cannedStore = newCannedStore(t.TempDir())

	out, _ := m.handleChordAction(ChordCannedMenu)
	m, ok := out.(model)
	if !ok {
		t.Fatalf("handleChordAction returned %T, want model", out)
	}

	if !m.canned.open {
		t.Fatal("menu did not open")
	}
	if m.canned.session != "hive-proj" {
		t.Errorf("target session: got %q, want hive-proj", m.canned.session)
	}
	if len(m.canned.items) == 0 {
		t.Error("menu opened with no prompts")
	}
}
