package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TerminalPane renders tmux pane output and optionally forwards input.
type TerminalPane struct {
	SessionName  string
	Content      string // live capture (visible portion)
	Width        int
	Height       int
	Focused      bool
	HasBorder    bool // true if rendered inside a lipgloss border
	ScrollOffset int  // 0 = live/bottom, >0 = lines scrolled up from bottom
	fullContent  string
	lastResizeW  int // last width we resized tmux to
	lastResizeH  int // last height we resized tmux to
}

// NewTerminalPane creates a terminal pane for the given tmux session.
func NewTerminalPane(sessionName string) *TerminalPane {
	return &TerminalPane{
		SessionName: sessionName,
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

// ScrollUp scrolls the viewport up by n lines.
func (t *TerminalPane) ScrollUp(n int) {
	t.ScrollOffset += n
	// Clamp to max scrollback
	if t.fullContent != "" {
		lines := strings.Split(t.fullContent, "\n")
		maxOffset := len(lines) - t.InnerHeight()
		if maxOffset < 0 {
			maxOffset = 0
		}
		if t.ScrollOffset > maxOffset {
			t.ScrollOffset = maxOffset
		}
	}
}

// ScrollDown scrolls the viewport down by n lines. Snaps to live at bottom.
func (t *TerminalPane) ScrollDown(n int) {
	t.ScrollOffset -= n
	if t.ScrollOffset < 0 {
		t.ScrollOffset = 0
	}
}

// IsScrolledUp returns true if the pane is showing history, not live.
func (t *TerminalPane) IsScrolledUp() bool {
	return t.ScrollOffset > 0
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

	var rendered string
	if t.ScrollOffset > 0 && t.fullContent != "" {
		// Scrolled up — show slice of full scrollback
		lines := strings.Split(t.fullContent, "\n")
		end := len(lines) - t.ScrollOffset
		if end < 0 {
			end = 0
		}
		start := end - ih
		if start < 0 {
			start = 0
		}
		rendered = strings.Join(lines[start:end], "\n")
	} else {
		// Live view — show bottom of current capture
		rendered = TruncateToHeight(t.Content, ih)
	}

	// Clamp each line to inner width and reset ANSI state.
	// Tmux capture output may contain unclosed ANSI sequences that, after
	// truncation, bleed into padding and the adjacent pane.
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = ClampToWidth(line, iw) + "\033[0m"
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
