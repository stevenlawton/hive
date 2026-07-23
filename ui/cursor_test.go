package ui

import (
	"strings"
	"testing"
)

// A focused pane in tail mode must inject the sentinel at its content origin
// and resolve the tmux cursor position into a local on-screen offset.
func TestTerminalPaneCursorOverlay(t *testing.T) {
	tp := NewTerminalPane("s")
	tp.SetSize(80, 10)
	tp.SetContent("line0\nline1\nprompt> ")
	tp.ShowCursor = true
	tp.CursorX = 8
	tp.CursorY = 2

	out := tp.View()

	if !strings.Contains(out, CursorSentinel) {
		t.Fatalf("sentinel not injected for focused pane")
	}
	if !tp.CursorResolved {
		t.Fatalf("cursor should resolve for an in-range position")
	}
	if tp.CursorLocalX != 8 || tp.CursorLocalY != 2 {
		t.Fatalf("local cursor = (%d,%d), want (8,2)", tp.CursorLocalX, tp.CursorLocalY)
	}
	// Sentinel must sit at the very start of the first rendered line (origin).
	if !strings.HasPrefix(strings.Split(out, "\n")[0], CursorSentinel) {
		t.Fatalf("sentinel not at content origin")
	}
}

// When the cursor is off the visible area or the pane isn't showing a cursor,
// nothing is injected and CursorResolved stays false.
func TestTerminalPaneNoCursorWhenHidden(t *testing.T) {
	tp := NewTerminalPane("s")
	tp.SetSize(80, 10)
	tp.SetContent("line0\nline1")
	tp.ShowCursor = false
	tp.CursorX, tp.CursorY = 3, 1

	out := tp.View()
	if strings.Contains(out, CursorSentinel) || tp.CursorResolved {
		t.Fatalf("no cursor should be shown when ShowCursor is false")
	}
}
