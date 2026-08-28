package main

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

func testPrompts(n int) []CannedPrompt {
	out := make([]CannedPrompt, n)
	for i := range out {
		out[i] = CannedPrompt{Label: string(rune('a' + i)), Text: "text"}
	}
	return out
}

func TestCannedMenuCursorClampsAtEnds(t *testing.T) {
	menu := cannedMenu{items: testPrompts(3)}

	menu.move(-1)
	if menu.cursor != 0 {
		t.Errorf("up from the top: got %d, want 0", menu.cursor)
	}

	menu.move(1)
	menu.move(1)
	menu.move(1)
	if menu.cursor != 2 {
		t.Errorf("down past the end: got %d, want 2", menu.cursor)
	}
}

func TestCannedDigitIndexMapsOneThroughNineThenZero(t *testing.T) {
	cases := []struct {
		key  string
		want int
	}{
		{"1", 0},
		{"9", 8},
		{"0", 9},
		{"a", -1},
		{"", -1},
	}
	for _, c := range cases {
		if got := cannedDigitIndex(c.key); got != c.want {
			t.Errorf("cannedDigitIndex(%q): got %d, want %d", c.key, got, c.want)
		}
	}
}

func TestCannedMenuGeometryStaysOnScreen(t *testing.T) {
	g := cannedGeometry(testPrompts(10), 78, 22, 80, 24)

	if g.x < 0 || g.y < 0 {
		t.Errorf("box starts off-screen at (%d,%d)", g.x, g.y)
	}
	if g.x+g.width > 80 {
		t.Errorf("box right edge %d exceeds width 80", g.x+g.width)
	}
	if g.y+g.height > 24 {
		t.Errorf("box bottom edge %d exceeds height 24", g.y+g.height)
	}
}

func TestCannedMenuGeometryAnchorsAtTheClick(t *testing.T) {
	g := cannedGeometry(testPrompts(3), 10, 4, 120, 40)

	if g.x != 10 || g.y != 4 {
		t.Errorf("got (%d,%d), want the click point (10,4)", g.x, g.y)
	}
}

func TestCannedMenuRowAtFindsTheClickedRow(t *testing.T) {
	items := testPrompts(3)
	g := cannedGeometry(items, 10, 4, 120, 40)

	if got := g.rowAt(12, g.firstRowY()); got != 0 {
		t.Errorf("first row: got %d, want 0", got)
	}
	if got := g.rowAt(12, g.firstRowY()+2); got != 2 {
		t.Errorf("third row: got %d, want 2", got)
	}
	if got := g.rowAt(12, g.firstRowY()+3); got != -1 {
		t.Errorf("below the last row: got %d, want -1", got)
	}
	if got := g.rowAt(g.x-1, g.firstRowY()); got != -1 {
		t.Errorf("left of the box: got %d, want -1", got)
	}
	if got := g.rowAt(g.x+g.width, g.firstRowY()); got != -1 {
		t.Errorf("right of the box: got %d, want -1", got)
	}
}

func TestCannedSendPlanOnIdleSessionDoesNotInterrupt(t *testing.T) {
	plan := cannedSendPlan(false, "continue")

	for _, op := range plan {
		if op.key == "escape" {
			t.Fatalf("idle session: plan contains an escape: %+v", plan)
		}
	}
	if len(plan) != 2 || plan[0].literal != "continue" || plan[1].key != "enter" {
		t.Errorf("got %+v, want the text then enter", plan)
	}
}

func TestCannedSendPlanOnBusySessionInterruptsFirst(t *testing.T) {
	plan := cannedSendPlan(true, "continue")

	if len(plan) != 3 {
		t.Fatalf("got %d ops, want 3: %+v", len(plan), plan)
	}
	if plan[0].key != "escape" {
		t.Errorf("first op: got %+v, want an escape", plan[0])
	}
	if plan[1].literal != "continue" {
		t.Errorf("second op: got %+v, want the prompt text", plan[1])
	}
	if plan[1].delay <= 0 {
		t.Errorf("no pause after the escape: %+v", plan[1])
	}
	if plan[2].key != "enter" {
		t.Errorf("third op: got %+v, want enter", plan[2])
	}
}

func TestCannedSendPlanIgnoresEmptyText(t *testing.T) {
	if plan := cannedSendPlan(true, "   "); len(plan) != 0 {
		t.Errorf("got %+v, want no ops for empty text", plan)
	}
}

func TestSessionIsBusyReadsRichStatus(t *testing.T) {
	items := []repoItem{
		{tmuxSes: "hive-a", richStatus: &SessionStatus{Status: "running"}},
		{tmuxSes: "hive-b", richStatus: &SessionStatus{Status: "completed"}},
		{tmuxSes: "hive-c"},
	}

	if !sessionIsBusy(items, "hive-a") {
		t.Error("running session: got idle, want busy")
	}
	if sessionIsBusy(items, "hive-b") {
		t.Error("completed session: got busy, want idle")
	}
	if sessionIsBusy(items, "hive-c") {
		t.Error("session with no status file: got busy, want idle")
	}
	if sessionIsBusy(items, "hive-unknown") {
		t.Error("unknown session: got busy, want idle")
	}
}

func TestCannedRowTextNumbersOneThroughNineThenZero(t *testing.T) {
	cases := []struct {
		index int
		want  string
	}{
		{0, "1. continue"},
		{8, "9. continue"},
		{9, "0. continue"},
		{10, "   continue"},
	}
	for _, c := range cases {
		got := cannedRowText(c.index, CannedPrompt{Label: "continue", Text: "continue"})
		if got != c.want {
			t.Errorf("index %d: got %q, want %q", c.index, got, c.want)
		}
	}
}

func TestRenderCannedMenuMatchesItsGeometry(t *testing.T) {
	items := testPrompts(4)
	c := cannedMenu{items: items, cursor: 1, geom: cannedGeometry(items, 5, 5, 100, 40)}

	lines := strings.Split(renderCannedMenu(c), "\n")

	if len(lines) != c.geom.height {
		t.Errorf("got %d lines, want geometry height %d", len(lines), c.geom.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != c.geom.width {
			t.Errorf("line %d: width %d, want geometry width %d", i, w, c.geom.width)
		}
	}
}

func TestCannedPopupShowsTheEditFormWhileEditing(t *testing.T) {
	items := []CannedPrompt{{Label: "one", Text: "first prompt"}}
	m := model{width: 100, height: 40, cannedStore: newCannedStore(t.TempDir())}
	m.canned = cannedMenu{open: true, items: items, geom: cannedGeometry(items, 2, 2, 100, 40)}

	if got := m.renderCannedPopup(); !strings.Contains(got, "1. one") {
		t.Errorf("menu should list the numbered prompt:\n%s", got)
	}

	m.startCannedEdit(false)
	got := m.renderCannedPopup()

	if !strings.Contains(got, "one") || !strings.Contains(got, "first prompt") {
		t.Errorf("form should show both fields:\n%s", got)
	}
	if strings.Contains(got, "1. one") {
		t.Errorf("form should replace the list, not sit under it:\n%s", got)
	}
}

func TestCannedFormWidensANarrowPopupAndStaysOnScreen(t *testing.T) {
	items := []CannedPrompt{{Label: "a", Text: "run the tests and fix whatever fails"}}
	m := model{width: 100, height: 40, cannedStore: newCannedStore(t.TempDir())}
	m.canned = cannedMenu{open: true, items: items, geom: cannedGeometry(items, 90, 2, 100, 40)}

	m.startCannedEdit(false)

	if m.canned.geom.width < cannedFormMinWidth {
		t.Errorf("form width %d, want at least %d", m.canned.geom.width, cannedFormMinWidth)
	}
	if m.canned.geom.x+m.canned.geom.width > 100 {
		t.Errorf("form runs off screen: x %d + width %d > 100", m.canned.geom.x, m.canned.geom.width)
	}
	got := m.renderCannedPopup()
	if !strings.Contains(got, "run the tests and fix whatever fails") {
		t.Errorf("form truncated a prompt that fits:\n%s", got)
	}
	if strings.Contains(got, "…") {
		t.Errorf("form fields are wider than the box they sit in:\n%s", got)
	}
}
