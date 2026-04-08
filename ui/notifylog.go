package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// NotifyEntry is a single notification event.
type NotifyEntry struct {
	Repo  string
	Event string
	Time  time.Time
}

// NotifyLog stores recent notification events for display.
type NotifyLog struct {
	Entries    []NotifyEntry
	MaxEntries int
}

// NewNotifyLog creates a notification log with a max size.
func NewNotifyLog(maxEntries int) *NotifyLog {
	return &NotifyLog{
		MaxEntries: maxEntries,
	}
}

// Add appends a notification entry (newest first).
func (n *NotifyLog) Add(repo, event string, t time.Time) {
	entry := NotifyEntry{Repo: repo, Event: event, Time: t}
	n.Entries = append([]NotifyEntry{entry}, n.Entries...)
	if len(n.Entries) > n.MaxEntries {
		n.Entries = n.Entries[:n.MaxEntries]
	}
}

// View renders the notification log for display in the manager view.
func (n *NotifyLog) View(width, height int) string {
	if len(n.Entries) == 0 {
		return ""
	}

	header := SectionHeaderStyle.Render("── notifications ──")
	lines := []string{header}

	maxLines := height - 1
	if maxLines < 1 {
		return header
	}

	for i, entry := range n.Entries {
		if i >= maxLines {
			break
		}
		lines = append(lines, n.renderEntry(entry))
	}

	return strings.Join(lines, "\n")
}

func (n *NotifyLog) renderEntry(entry NotifyEntry) string {
	ago := TimeAgo(entry.Time)

	var style lipgloss.Style
	switch entry.Event {
	case "completed":
		style = ClaudeStyle
	case "crashed", "dead":
		style = DeadStyle
	case "waiting", "bell":
		style = WaitStyle
	default:
		style = IdleStyle
	}

	return style.Render(fmt.Sprintf("%s %s %s", entry.Repo, entry.Event, ago))
}

// TimeAgo returns a human-readable relative time string.
func TimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
