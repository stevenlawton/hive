package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/stevenlawton/hive/ui"
)

func updateModel(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(msg)
	mm, ok := out.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", out)
	}
	return mm, cmd
}

func TestCannedRowClickSendsThatPromptAndCloses(t *testing.T) {
	sends := captureCannedSends(t)
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})
	m.mode = viewWorkspace

	m, cmd := updateModel(t, m, cannedRowClickMsg{row: 1})
	if cmd != nil {
		cmd()
	}

	if m.canned.open {
		t.Error("menu stayed open after a click")
	}
	if len(*sends) != 1 || (*sends)[0].plan[0].literal != "second prompt" {
		t.Errorf("got %+v, want the clicked prompt", *sends)
	}
}

func TestCannedClickOutsideClosesWithoutSending(t *testing.T) {
	sends := captureCannedSends(t)
	m := cannedTestModel(t, false, CannedPrompt{Label: "one", Text: "first prompt"})
	m.mode = viewWorkspace

	m, cmd := updateModel(t, m, cannedRowClickMsg{row: -1})
	if cmd != nil {
		cmd()
	}

	if m.canned.open {
		t.Error("a click outside the popup left it open")
	}
	if len(*sends) != 0 {
		t.Errorf("a click outside sent %+v, want nothing", *sends)
	}
}

func TestCannedWheelMovesTheMenuCursor(t *testing.T) {
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})
	m.mode = viewWorkspace

	m, _ = updateModel(t, m, cannedScrollMsg{dir: 1})

	if m.canned.cursor != 1 {
		t.Errorf("cursor: got %d, want 1", m.canned.cursor)
	}
}

func TestCannedHoverHighlightsTheRowUnderThePointer(t *testing.T) {
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})
	m.mode = viewWorkspace

	m, _ = updateModel(t, m, cannedHoverMsg{row: 1})

	if m.canned.cursor != 1 {
		t.Errorf("cursor: got %d, want the hovered row 1", m.canned.cursor)
	}
}

func TestCannedOpenMsgOpensOnTheClickedSession(t *testing.T) {
	m := model{width: 100, height: 40, mode: viewWorkspace, cannedStore: newCannedStore(t.TempDir())}

	m, _ = updateModel(t, m, cannedOpenMsg{session: "hive-clicked", x: 12, y: 7})

	if !m.canned.open {
		t.Fatal("menu did not open")
	}
	if m.canned.session != "hive-clicked" {
		t.Errorf("session: got %q, want hive-clicked", m.canned.session)
	}
	if m.canned.geom.x != 12 || m.canned.geom.y != 7 {
		t.Errorf("anchor: got (%d,%d), want the click point (12,7)", m.canned.geom.x, m.canned.geom.y)
	}
}

// With the menu open it owns the keyboard: keys must not reach the session.
func TestOpenCannedMenuOwnsWorkspaceKeys(t *testing.T) {
	m := cannedTestModel(t, false, CannedPrompt{Label: "one", Text: "first prompt"})
	m.mode = viewWorkspace
	m.workspace = ui.NewWorkspaceView()
	m.chord = NewChordHandler(0)

	m, _ = updateModel(t, m, keyEscape)

	if m.canned.open {
		t.Error("escape did not reach the menu — the key was forwarded instead")
	}
}
