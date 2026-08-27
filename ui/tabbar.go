package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Tone backgrounds are deliberately muted and deliberately NOT the palette's
// ColorOrange / ColorRed: those already mean "active tab" and "flashing tab"
// respectively, and a tone that borrows one is not a signal, it is a collision.
// They also sit behind text you have to read all day, so they are dark enough
// to carry light foreground rather than glare.
var (
	toneWarnBG   = lipgloss.Color("#6b5314")
	toneDangerBG = lipgloss.Color("#7a2222")
	toneInfoBG   = lipgloss.Color("#243d63")
)

// TabTone is how urgently a tab wants attention. It is deliberately about
// presentation rather than cause: the ui package renders a tone, and the caller
// decides what earns one.
type TabTone int

const (
	ToneNone TabTone = iota
	ToneInfo
	ToneWarn
	ToneDanger
)

// Tab represents a single tab in the tab bar.
type Tab struct {
	ID       string
	Label    string
	Flashing bool
	Tone     TabTone
}

// TabBar manages a row of tabs.
type TabBar struct {
	Tabs      []Tab
	ActiveIdx int
	Width     int

	// RightStatus is fleet-wide text pinned to the right of the filler — for
	// things that are true of the machine rather than of any one tab, like the
	// shared rate-limit window. It never affects tab hit zones.
	RightStatus string
}

// NewTabBar creates an empty tab bar.
func NewTabBar() *TabBar {
	return &TabBar{}
}

// Add appends a new tab.
func (tb *TabBar) Add(id, label string) {
	tb.Tabs = append(tb.Tabs, Tab{ID: id, Label: label})
}

// Remove deletes a tab by ID and adjusts ActiveIdx.
func (tb *TabBar) Remove(id string) {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.Tabs = append(tb.Tabs[:i], tb.Tabs[i+1:]...)
			if tb.ActiveIdx >= len(tb.Tabs) && len(tb.Tabs) > 0 {
				tb.ActiveIdx = len(tb.Tabs) - 1
			}
			return
		}
	}
}

// SetActive sets the active tab by index.
func (tb *TabBar) SetActive(idx int) {
	if idx >= 0 && idx < len(tb.Tabs) {
		tb.ActiveIdx = idx
	}
}

// SetActiveByID sets the active tab by ID.
func (tb *TabBar) SetActiveByID(id string) {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.ActiveIdx = i
			return
		}
	}
}

// FocusOrAdd focuses an existing tab or adds a new one.
func (tb *TabBar) FocusOrAdd(id, label string) {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.ActiveIdx = i
			return
		}
	}
	tb.Add(id, label)
	tb.ActiveIdx = len(tb.Tabs) - 1
}

// FocusByID focuses the tab with the given ID. Returns true if found.
func (tb *TabBar) FocusByID(id string) bool {
	for i, tab := range tb.Tabs {
		if tab.ID == id {
			tb.ActiveIdx = i
			return true
		}
	}
	return false
}

// Next moves to the next tab (wrapping).
func (tb *TabBar) Next() {
	if len(tb.Tabs) == 0 {
		return
	}
	tb.ActiveIdx = (tb.ActiveIdx + 1) % len(tb.Tabs)
}

// Prev moves to the previous tab (wrapping).
func (tb *TabBar) Prev() {
	if len(tb.Tabs) == 0 {
		return
	}
	tb.ActiveIdx = (tb.ActiveIdx - 1 + len(tb.Tabs)) % len(tb.Tabs)
}

// ActiveTab returns the currently active tab, or nil if empty.
func (tb *TabBar) ActiveTab() *Tab {
	if len(tb.Tabs) == 0 {
		return nil
	}
	return &tb.Tabs[tb.ActiveIdx]
}

// TabWidths returns the visual (cell) width of each tab as rendered by
// View(), using lipgloss.Width to correctly account for emoji and other
// wide characters. Used by the mouse click handler to compute accurate
// hit zones.
func (tb *TabBar) TabWidths() []int {
	widths := make([]int, len(tb.Tabs))
	for i, tab := range tb.Tabs {
		widths[i] = lipgloss.Width(tb.renderTab(i, tab))
	}
	return widths
}

// renderTab is the single place a tab's appearance is decided. TabWidths and
// View both go through it: the mouse hit zones are derived from the former and
// drawn by the latter, so any divergence puts clicks on the wrong tab.
func (tb *TabBar) renderTab(i int, tab Tab) string {
	label := " " + tab.Label + " "
	var style lipgloss.Style
	switch {
	case tab.Flashing:
		// The session is asking for input now. That outranks a verdict, which
		// is advice about the next few minutes.
		style = TabFlashStyle
	case tab.ID == HomeTabID:
		style = TabHomeStyle
	case tab.Tone != ToneNone:
		style = toneStyle(tab.Tone, i == tb.ActiveIdx)
	case i == tb.ActiveIdx:
		style = TabActiveStyle
	default:
		style = TabInactiveStyle
	}
	return style.Render(label)
}

// toneStyle keeps the padding of the normal tab styles so a toned tab occupies
// exactly the same cells as an untoned one.
func toneStyle(tone TabTone, active bool) lipgloss.Style {
	var bg color.Color
	switch tone {
	case ToneDanger:
		bg = toneDangerBG
	case ToneWarn:
		bg = toneWarnBG
	case ToneInfo:
		bg = toneInfoBG
	default:
		if active {
			return TabActiveStyle
		}
		return TabInactiveStyle
	}
	s := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e8e8e8")).
		Background(bg).
		Padding(0, 1)
	if active {
		s = s.Bold(true)
	}
	return s
}

// SetFlashing marks a tab as flashing by ID.
func (tb *TabBar) SetFlashing(id string, flashing bool) {
	for i := range tb.Tabs {
		if tb.Tabs[i].ID == id {
			tb.Tabs[i].Flashing = flashing
			return
		}
	}
}

// View renders the tab bar.
func (tb *TabBar) View() string {
	if len(tb.Tabs) == 0 {
		return ""
	}

	var parts []string
	for i, tab := range tb.Tabs {
		parts = append(parts, tb.renderTab(i, tab))
	}

	tabs := strings.Join(parts, "")
	remaining := tb.Width - lipgloss.Width(tabs)
	if remaining < 0 {
		remaining = 0
	}

	// The status is dropped rather than truncated when it will not fit: half a
	// rate-limit figure is worse than none, and overflowing the bar would push
	// the layout around.
	status := tb.RightStatus
	if status != "" && lipgloss.Width(status)+1 > remaining {
		status = ""
	}
	if status != "" {
		remaining -= lipgloss.Width(status)
	}

	separator := strings.Repeat("─", remaining)
	return tabs + StatusBarStyle.Render(separator+status)
}
