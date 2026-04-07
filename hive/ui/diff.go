package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// DiffPane displays a colorized git diff.
type DiffPane struct {
	Diff      string
	Added     int
	Removed   int
	Width     int
	Height    int
	scrollPos int
}

// NewDiffPane creates a new diff pane.
func NewDiffPane() *DiffPane {
	return &DiffPane{}
}

// SetSize updates the pane dimensions.
func (d *DiffPane) SetSize(w, h int) {
	d.Width = w
	d.Height = h
}

// SetDiff updates the diff content and recomputes stats.
func (d *DiffPane) SetDiff(diff string) {
	d.Diff = diff
	d.Added, d.Removed = ParseDiffStats(diff)
	d.scrollPos = 0
}

// StatsString returns a short summary like "+42/-13".
func (d *DiffPane) StatsString() string {
	if d.Added == 0 && d.Removed == 0 {
		return ""
	}
	return fmt.Sprintf("+%d/-%d", d.Added, d.Removed)
}

// ScrollUp moves up.
func (d *DiffPane) ScrollUp(n int) {
	d.scrollPos -= n
	if d.scrollPos < 0 {
		d.scrollPos = 0
	}
}

// ScrollDown moves down.
func (d *DiffPane) ScrollDown(n int) {
	lines := strings.Split(d.Diff, "\n")
	maxPos := len(lines) - d.Height
	if maxPos < 0 {
		maxPos = 0
	}
	d.scrollPos += n
	if d.scrollPos > maxPos {
		d.scrollPos = maxPos
	}
}

// View renders the diff pane.
func (d *DiffPane) View() string {
	if d.Diff == "" {
		return lipgloss.NewStyle().
			Width(d.Width).
			Height(d.Height).
			Foreground(ColorGray).
			Render("No changes")
	}

	colorized := ColorizeDiff(d.Diff)
	lines := strings.Split(colorized, "\n")

	start := d.scrollPos
	if start >= len(lines) {
		start = len(lines) - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + d.Height
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

// ColorizeDiff applies ANSI colors to a unified diff string.
func ColorizeDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var out []string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			out = append(out, lipgloss.NewStyle().Bold(true).Render(line))
		case strings.HasPrefix(line, "@@"):
			out = append(out, DiffHunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			out = append(out, DiffAddStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			out = append(out, DiffDelStyle.Render(line))
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// ParseDiffStats counts additions and deletions in a unified diff.
func ParseDiffStats(diff string) (added, removed int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return
}
