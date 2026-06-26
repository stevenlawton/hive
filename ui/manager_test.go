package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// padLines must never let a line exceed the target width: the two-pane
// manager layout joins left+separator+right per line, so any over-long
// left line shoves the separator and preview pane off the right edge and
// the terminal wraps it — the "mangled top-left" worktree-form bug.
func TestPadLinesTruncatesOverlongLine(t *testing.T) {
	// 33-wide line (the worktree form's help line) against a narrow pane.
	long := "  ctrl+s to create, esc to cancel"
	out := padLines(long, 28)
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 28 {
			t.Errorf("expected every line clamped to width 28, got width %d: %q", w, line)
		}
	}
}

// Truncation must be ANSI-aware: styled content carries escape sequences
// that don't occupy columns, so clamping by raw byte length would corrupt
// color codes and miscount width.
func TestPadLinesTruncatesStyledLineByVisibleWidth(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("a very long highlighted status line")
	out := padLines(styled, 12)
	if w := lipgloss.Width(out); w != 12 {
		t.Errorf("expected visible width 12, got %d: %q", w, out)
	}
}

// Short lines must still be padded out to the full width (unchanged behavior).
func TestPadLinesPadsShortLine(t *testing.T) {
	out := padLines("hi", 10)
	if w := lipgloss.Width(out); w != 10 {
		t.Errorf("expected short line padded to width 10, got %d: %q", w, out)
	}
}

// A line clamped at a wide-glyph boundary lands at width-1; it must still be
// padded back to exactly width, or the per-row separator misaligns and stops
// looking like a vertical line. "你" is a 2-cell glyph: truncating
// "12345678你extra" to width 9 stops at the 8-cell prefix, leaving a short line.
func TestPadLinesClampedWideLinePadsToExactWidth(t *testing.T) {
	out := padLines("12345678你extra", 9)
	if w := lipgloss.Width(out); w != 9 {
		t.Errorf("expected clamped-then-padded width 9, got %d: %q", w, out)
	}
}

// End-to-end: composing the two-pane view with worktree-form content in a
// narrow pane must keep every line exactly the terminal width. Before the
// padLines clamp, the 33-wide help line overflowed the ~28-col list pane and
// pushed the separator/preview off-screen — the mangled worktree popup.
func TestManagerViewNarrowPaneKeepsUniformWidth(t *testing.T) {
	mv := NewManagerView()
	const termWidth = 80 // ListWidth = 28 here, narrower than the 33-wide help line
	mv.SetSize(termWidth, 24)

	listContent := strings.Join([]string{
		"  New worktree",
		"> Branch: feature-name",
		"  Prompt: optional task for Claude",
		"  [ ] Yolo",
		"  ctrl+s to create, esc to cancel",
		"── active ──",
		"  stevenlawton.com",
	}, "\n")

	out := mv.View(listContent, "status")
	// Drop the trailing status bar line appended after the panes.
	body := strings.TrimSuffix(out, "\nstatus")
	for i, line := range strings.Split(body, "\n") {
		if w := lipgloss.Width(line); w != termWidth {
			t.Errorf("line %d width %d, want %d (overflow corrupts the layout): %q", i, w, termWidth, line)
		}
	}
}
