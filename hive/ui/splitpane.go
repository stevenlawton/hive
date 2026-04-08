package ui

import (
	"charm.land/lipgloss/v2"
)

// Split represents one pane in a split layout.
type Split struct {
	Label       string
	SessionName string
	Terminal    *TerminalPane
}

// SplitOrientation controls the split layout direction.
type SplitOrientation int

const (
	SplitVertical   SplitOrientation = iota // side by side (ctrl+space v)
	SplitHorizontal                         // stacked top/bottom (ctrl+space h)
)

// SplitPane manages terminal splits in a given orientation.
type SplitPane struct {
	Splits      []Split
	FocusIdx    int
	Orientation SplitOrientation
	Width       int
	Height      int
}

// NewSplitPane creates an empty split pane layout.
func NewSplitPane() *SplitPane {
	return &SplitPane{}
}

// SetSize updates the total available area and recalculates split widths.
func (sp *SplitPane) SetSize(w, h int) {
	sp.Width = w
	sp.Height = h
	sp.recalcWidths()
}

// AddSplit adds a new split with a terminal pane.
func (sp *SplitPane) AddSplit(label, sessionName string) {
	term := NewTerminalPane(sessionName)
	term.HasBorder = true
	sp.Splits = append(sp.Splits, Split{
		Label:       label,
		SessionName: sessionName,
		Terminal:    term,
	})
	sp.recalcWidths()
}

// RemoveSplit removes a split by label and adjusts focus.
func (sp *SplitPane) RemoveSplit(label string) {
	for i, s := range sp.Splits {
		if s.Label == label {
			sp.Splits = append(sp.Splits[:i], sp.Splits[i+1:]...)
			if sp.FocusIdx >= len(sp.Splits) && len(sp.Splits) > 0 {
				sp.FocusIdx = len(sp.Splits) - 1
			}
			sp.recalcWidths()
			return
		}
	}
}

// FocusedSplit returns the currently focused split, or nil.
func (sp *SplitPane) FocusedSplit() *Split {
	if len(sp.Splits) == 0 {
		return nil
	}
	return &sp.Splits[sp.FocusIdx]
}

// FocusRight moves focus to the right.
func (sp *SplitPane) FocusRight() {
	if sp.FocusIdx < len(sp.Splits)-1 {
		sp.FocusIdx++
	}
}

// FocusLeft moves focus to the left.
func (sp *SplitPane) FocusLeft() {
	if sp.FocusIdx > 0 {
		sp.FocusIdx--
	}
}

func (sp *SplitPane) recalcWidths() {
	n := len(sp.Splits)
	if n == 0 {
		return
	}

	if sp.Orientation == SplitHorizontal {
		// Stacked top/bottom: each split gets full width, split height
		available := sp.Height
		if available < n {
			available = n
		}
		splitHeight := available / n

		for i := range sp.Splits {
			h := splitHeight
			if i == n-1 {
				h = available - splitHeight*(n-1)
			}
			sp.Splits[i].Terminal.SetSize(sp.Width, h)
			sp.Splits[i].Terminal.Focused = (i == sp.FocusIdx)
		}
	} else {
		// Side by side: each split gets full height, split width
		separators := n - 1
		available := sp.Width - separators
		if available < n {
			available = n
		}
		splitWidth := available / n

		for i := range sp.Splits {
			w := splitWidth
			if i == n-1 {
				w = available - splitWidth*(n-1)
			}
			sp.Splits[i].Terminal.SetSize(w, sp.Height)
			sp.Splits[i].Terminal.Focused = (i == sp.FocusIdx)
		}
	}
}

// View renders splits with borders in the configured orientation.
func (sp *SplitPane) View() string {
	if len(sp.Splits) == 0 {
		return ""
	}

	for i := range sp.Splits {
		sp.Splits[i].Terminal.Focused = (i == sp.FocusIdx)
	}

	var panes []string
	for i, split := range sp.Splits {
		borderStyle := BorderStyle
		if i == sp.FocusIdx {
			borderStyle = FocusedBorderStyle
		}

		content := split.Terminal.View()

		rendered := borderStyle.
			Width(split.Terminal.Width).
			Height(split.Terminal.Height).
			Render(content)

		panes = append(panes, rendered)
	}

	if sp.Orientation == SplitHorizontal {
		return lipgloss.JoinVertical(lipgloss.Left, panes...)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

// SessionNames returns all session names in the split pane.
func (sp *SplitPane) SessionNames() []string {
	names := make([]string, len(sp.Splits))
	for i, s := range sp.Splits {
		names[i] = s.SessionName
	}
	return names
}
