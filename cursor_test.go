package main

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/stevenlawton/hive/ui"
)

var curAnsiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// visibleUpTo returns the visible text of the given screen row up to (not
// including) column x, with ANSI stripped.
func visibleUpTo(frame string, row, x int) string {
	lines := strings.Split(frame, "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}
	// Walk the ANSI-stripped line accumulating display width until we reach x.
	plain := curAnsiRe.ReplaceAllString(lines[row], "")
	var b strings.Builder
	w := 0
	for _, r := range plain {
		rw := lipgloss.Width(string(r))
		if w+rw > x {
			break
		}
		b.WriteString(string(r))
		w += rw
	}
	return b.String()
}

// The worktree modal must (a) strip the sentinel from the rendered frame and
// (b) place the real hardware cursor exactly after the focused field's value.
func TestWorktreeModalCursorPlacement(t *testing.T) {
	m := &model{mode: viewWorktree, width: 120, height: 40}
	m.wtFields = []textinput.Model{
		newWorktreeField("Branch: ", "feature-name", "hello"),
		newWorktreeField("Prompt: ", "optional", ""),
	}
	m.wtFocus = 0
	m.wtFields[0].Focus()

	v := (*m).View()

	if strings.Contains(v.Content, ui.CursorSentinel) {
		t.Fatalf("sentinel leaked into rendered frame")
	}
	if v.Cursor == nil {
		t.Fatalf("no hardware cursor set for focused field")
	}
	// The cursor sits at the end of "hello"; everything left of it on that row
	// must end with the prompt + value.
	prefix := visibleUpTo(v.Content, v.Cursor.Y, v.Cursor.X)
	if !strings.HasSuffix(prefix, "Branch: hello") {
		t.Fatalf("cursor misplaced: row=%d col=%d prefix=%q", v.Cursor.Y, v.Cursor.X, prefix)
	}
}

// When no text field is focused (e.g. focus is on the yolo toggle) no cursor is
// emitted and no sentinel leaks.
func TestWorktreeModalNoCursorOnToggle(t *testing.T) {
	m := &model{mode: viewWorktree, width: 120, height: 40}
	m.wtFields = []textinput.Model{
		newWorktreeField("Branch: ", "feature-name", "hello"),
		newWorktreeField("Prompt: ", "optional", ""),
	}
	m.wtFocus = wtFieldCount // the yolo toggle, not a text field

	v := (*m).View()
	if v.Cursor != nil {
		t.Fatalf("cursor should be nil when no field is focused, got %+v", v.Cursor)
	}
	if strings.Contains(v.Content, ui.CursorSentinel) {
		t.Fatalf("sentinel leaked into rendered frame")
	}
}
