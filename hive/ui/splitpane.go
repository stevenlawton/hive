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

// SplitPane manages a horizontal row of terminal splits.
type SplitPane struct {
	Splits   []Split
	FocusIdx int
	Width    int
	Height   int
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

	// Account for border separators between splits (1 char each)
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

// View renders all splits side by side with borders.
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
			Height(sp.Height).
			Render(content)

		panes = append(panes, rendered)
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
