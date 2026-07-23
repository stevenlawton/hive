package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var scrollStatusStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#ffff00")).
	Background(lipgloss.Color("#333333")).
	Bold(true)

// CursorSentinel is a zero-width marker embedded at the spot where the real
// terminal cursor should sit. It has display width 0 (so it never shifts
// layout), survives lipgloss rendering, and is located + stripped from the
// fully-composed frame just before display. See model.resolveFrameCursor.
const CursorSentinel = "​"

// TerminalPane renders tmux pane output and optionally forwards input.
type TerminalPane struct {
	SessionName  string
	Content      string // live capture (visible portion)
	Width        int
	Height       int
	Focused      bool
	HasBorder    bool // true if rendered inside a lipgloss border
	ScrollTop    int  // -1 = tail mode (follow newest); >=0 = absolute line index of top visible row
	fullContent  string
	lastResizeW  int // last width we resized tmux to
	lastResizeH  int // last height we resized tmux to

	// Real-cursor overlay. tmux capture-pane omits the cursor, so when this
	// pane is focused we ask tmux where the cursor is (ShowCursor/CursorX/
	// CursorY, set by the capture tick) and View injects the sentinel at its
	// content origin, resolving the on-screen offset into CursorLocalX/Y for
	// the top-level frame to place a hardware cursor. See model.resolveFrameCursor.
	ShowCursor     bool
	CursorX        int // tmux pane cursor column (0-based)
	CursorY        int // tmux pane cursor row (0-based, from top of pane)
	CursorLocalX   int // resolved column within this pane's rendered content
	CursorLocalY   int // resolved row within this pane's rendered content
	CursorResolved bool
}

// NewTerminalPane creates a terminal pane for the given tmux session.
func NewTerminalPane(sessionName string) *TerminalPane {
	return &TerminalPane{
		SessionName: sessionName,
		ScrollTop:   -1, // start in tail mode
	}
}

// SetSize updates the pane dimensions.
func (t *TerminalPane) SetSize(w, h int) {
	t.Width = w
	t.Height = h
}

// NeedsResize returns true if the tmux session should be resized.
func (t *TerminalPane) NeedsResize() bool {
	return t.SessionName != "" && t.Width > 0 && t.Height > 0 &&
		(t.Width != t.lastResizeW || t.Height != t.lastResizeH)
}

// MarkResized records that tmux was resized to current dimensions.
func (t *TerminalPane) MarkResized() {
	t.lastResizeW = t.Width
	t.lastResizeH = t.Height
}

// InnerWidth returns the usable content width (subtracts border if present).
func (t *TerminalPane) InnerWidth() int {
	if !t.HasBorder {
		return t.Width
	}
	if t.Width < 2 {
		return 0
	}
	return t.Width - 2
}

// InnerHeight returns the usable content height (subtracts border if present).
func (t *TerminalPane) InnerHeight() int {
	if !t.HasBorder {
		return t.Height
	}
	if t.Height < 2 {
		return 0
	}
	return t.Height - 2
}

// InvalidateResize forces the next NeedsResize check to return true.
func (t *TerminalPane) InvalidateResize() {
	t.lastResizeW = 0
	t.lastResizeH = 0
}

// SetContent updates the rendered content from capture-pane output.
func (t *TerminalPane) SetContent(content string) {
	t.Content = content
}

// SetFullContent sets the full scrollback for scroll mode.
func (t *TerminalPane) SetFullContent(content string) {
	t.fullContent = content
}

// ScrollBy moves the viewport by delta lines. Positive = down (toward
// newer), negative = up (toward older). Mirrors the bus view's scroll
// state machine: -1 means tail mode, any other value is an absolute
// line anchor.
func (t *TerminalPane) ScrollBy(delta int) {
	if t.fullContent == "" {
		return
	}
	total := strings.Count(t.fullContent, "\n") + 1
	page := t.InnerHeight()

	// If in tail mode, anchor at the current bottom so an upward
	// scroll starts from the visible content.
	if t.ScrollTop < 0 {
		if delta >= 0 {
			return // scroll down from tail is a no-op
		}
		t.ScrollTop = max(0, total-page)
	}

	t.ScrollTop += delta
	if t.ScrollTop < 0 {
		t.ScrollTop = 0
	}
	// If we've scrolled past the bottom, re-enter tail mode.
	if t.ScrollTop >= total-page {
		t.ScrollTop = -1
	}
}

// ScrollToTop jumps to the top of the scrollback.
func (t *TerminalPane) ScrollToTop() {
	t.ScrollTop = 0
}

// ScrollToBottom re-enters tail mode.
func (t *TerminalPane) ScrollToBottom() {
	t.ScrollTop = -1
}

// IsScrolledUp returns true if the pane is showing history, not live.
func (t *TerminalPane) IsScrolledUp() bool {
	return t.ScrollTop >= 0
}

// View renders the terminal pane content.
// Width/Height are the total allocation including border; content uses inner dimensions.
func (t *TerminalPane) View() string {
	iw, ih := t.InnerWidth(), t.InnerHeight()

	if t.Content == "" && t.fullContent == "" {
		return lipgloss.NewStyle().
			Width(iw).
			Height(ih).
			Foreground(ColorGray).
			Render("No session")
	}

	// Reserve a line for the scroll status bar when paused.
	contentHeight := ih
	scrolled := t.ScrollTop >= 0 && t.fullContent != ""
	if scrolled {
		contentHeight--
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	// carry accumulates SGR state over any rows above the visible slice so
	// styles that span rows (e.g. full-width diff backgrounds) survive the
	// per-line reset below. capture-pane -e only emits codes where they
	// change, so a row's starting state is implicit in the rows before it.
	carry := newSGRCarry()
	var rendered string
	if scrolled {
		// Frozen — show slice anchored at absolute line position.
		allLines := strings.Split(t.fullContent, "\n")
		start := t.ScrollTop
		if start > len(allLines) {
			start = len(allLines)
		}
		end := start + contentHeight
		if end > len(allLines) {
			end = len(allLines)
		}
		for _, line := range allLines[:start] {
			carry.Consume(line)
		}
		rendered = strings.Join(allLines[start:end], "\n")
	} else {
		// Live / tail mode — show bottom of current capture.
		all := strings.Split(t.Content, "\n")
		if drop := len(all) - contentHeight; drop > 0 {
			for _, line := range all[:drop] {
				carry.Consume(line)
			}
		}
		rendered = TruncateToHeight(t.Content, contentHeight)
	}

	// Clamp each line to inner width, restore carried ANSI state at the
	// start, reset at the end, and erase to end of line.
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		prefix := carry.Prefix()
		carry.Consume(line)
		lines[i] = prefix + ClampToWidth(line, iw) + "\033[0m\033[K"
	}

	// Append scroll status bar when paused.
	if scrolled {
		total := strings.Count(t.fullContent, "\n") + 1
		hidden := total - t.ScrollTop - contentHeight
		if hidden < 0 {
			hidden = 0
		}
		status := fmt.Sprintf("⏸ scrolled · %d rows hidden below · end to resume", hidden)
		lines = append(lines, scrollStatusStyle.Render(ClampToWidth(status, iw))+"\033[K")
	}

	// Cursor overlay (tail mode only — a frozen scroll has no live cursor).
	// tmux reports the cursor relative to the top of the pane; the tail view
	// may have dropped rows off the top, so shift by that amount. Mark the
	// origin with the sentinel and hand the resolved local offset upward.
	t.CursorResolved = false
	if t.ShowCursor && !scrolled && len(lines) > 0 {
		total := strings.Count(t.Content, "\n") + 1
		firstShown := 0
		if total > contentHeight {
			firstShown = total - contentHeight
		}
		row := t.CursorY - firstShown
		if row >= 0 && row < len(lines) && t.CursorX >= 0 && t.CursorX < iw {
			lines[0] = CursorSentinel + lines[0]
			t.CursorLocalX = t.CursorX
			t.CursorLocalY = row
			t.CursorResolved = true
		}
	}

	return strings.Join(lines, "\n")
}


// TruncateToHeight returns the last `height` lines of content.
func TruncateToHeight(content string, height int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return content
	}
	return strings.Join(lines[len(lines)-height:], "\n")
}

// ClampToWidth truncates a line to max visible width, preserving ANSI sequences.
func ClampToWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(line, width, "")
}
