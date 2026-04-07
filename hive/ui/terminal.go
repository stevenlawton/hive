package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// TerminalPane renders tmux pane output and optionally forwards input.
type TerminalPane struct {
	SessionName string
	Content     string
	Width       int
	Height      int
	Focused     bool
	ScrollMode  bool
	scrollPos   int
	fullContent string
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

// SetContent updates the rendered content from capture-pane output.
func (t *TerminalPane) SetContent(content string) {
	t.Content = content
}

// SetFullContent sets the full scrollback for scroll mode.
func (t *TerminalPane) SetFullContent(content string) {
	t.fullContent = content
}

// EnterScrollMode switches to scroll mode with full history.
func (t *TerminalPane) EnterScrollMode() {
	t.ScrollMode = true
	lines := strings.Split(t.fullContent, "\n")
	if len(lines) > t.Height {
		t.scrollPos = len(lines) - t.Height
	} else {
		t.scrollPos = 0
	}
}

// ExitScrollMode returns to normal mode.
func (t *TerminalPane) ExitScrollMode() {
	t.ScrollMode = false
	t.scrollPos = 0
}

// ScrollUp moves the viewport up.
func (t *TerminalPane) ScrollUp(n int) {
	t.scrollPos -= n
	if t.scrollPos < 0 {
		t.scrollPos = 0
	}
}

// ScrollDown moves the viewport down.
func (t *TerminalPane) ScrollDown(n int) {
	lines := strings.Split(t.fullContent, "\n")
	maxPos := len(lines) - t.Height
	if maxPos < 0 {
		maxPos = 0
	}
	t.scrollPos += n
	if t.scrollPos > maxPos {
		t.scrollPos = maxPos
	}
}

// View renders the terminal pane content.
func (t *TerminalPane) View() string {
	if t.Content == "" && !t.ScrollMode {
		return lipgloss.NewStyle().
			Width(t.Width).
			Height(t.Height).
			Foreground(ColorGray).
			Render("No session")
	}

	var rendered string
	if t.ScrollMode {
		rendered = t.viewScrollMode()
	} else {
		rendered = TruncateToHeight(t.Content, t.Height)
	}

	// Clamp each line to width
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = ClampToWidth(line, t.Width)
	}

	return strings.Join(lines, "\n")
}

func (t *TerminalPane) viewScrollMode() string {
	lines := strings.Split(t.fullContent, "\n")
	end := t.scrollPos + t.Height
	if end > len(lines) {
		end = len(lines)
	}
	start := t.scrollPos
	if start < 0 {
		start = 0
	}
	return strings.Join(lines[start:end], "\n")
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

// ClampToWidth truncates a line to max visible width.
func ClampToWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	return string(runes[:width])
}
