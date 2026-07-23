package ui

import (
	"charm.land/lipgloss/v2"
)

// Split represents one pane in a split layout.
type Split struct {
	Label       string
	SessionName string
	Terminal    *TerminalPane
	// Ratio is this pane's proportional share of the split axis. Ratios need
	// not sum to 1 — they are normalized on use. Zero means "uninitialized"
	// and is treated as equal.
	Ratio float64
}

// Minimum pane sizes enforced when adjusting the divider (see design spec
// docs/superpowers/specs/2026-07-17-split-resize-design.md).
const (
	minSplitCols = 10 // vertical (side-by-side) splits
	minSplitRows = 3  // horizontal (stacked) splits
)

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
	sp.equalizeRatios() // new split → reset to equal (predictable)
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
			sp.equalizeRatios() // removed split → reset to equal
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
	sp.ensureRatios()
	total := sp.ratioSum()
	available := sp.axisAvailable()
	if available < n {
		available = n
	}

	// Distribute proportionally; the last pane takes the remainder so the
	// cells always sum exactly. Truncation (not rounding) reproduces the
	// original equal-division sizes for equal ratios.
	used := 0
	for i := range sp.Splits {
		var size int
		if i == n-1 {
			size = available - used
		} else {
			size = int(float64(available) * sp.Splits[i].Ratio / total)
			used += size
		}
		if sp.Orientation == SplitHorizontal {
			sp.Splits[i].Terminal.SetSize(sp.Width, size)
		} else {
			sp.Splits[i].Terminal.SetSize(size, sp.Height)
		}
		sp.Splits[i].Terminal.Focused = (i == sp.FocusIdx)
	}
}

// axisAvailable is the cell budget along the split axis (columns for vertical
// splits, minus inter-pane separators; rows for horizontal).
func (sp *SplitPane) axisAvailable() int {
	if sp.Orientation == SplitHorizontal {
		return sp.Height
	}
	return sp.Width - (len(sp.Splits) - 1)
}

func (sp *SplitPane) minCells() int {
	if sp.Orientation == SplitHorizontal {
		return minSplitRows
	}
	return minSplitCols
}

func (sp *SplitPane) ratioSum() float64 {
	total := 0.0
	for i := range sp.Splits {
		total += sp.Splits[i].Ratio
	}
	if total <= 0 {
		return float64(len(sp.Splits))
	}
	return total
}

// ensureRatios initializes any zero/uninitialized ratios to an equal split.
func (sp *SplitPane) ensureRatios() {
	for i := range sp.Splits {
		if sp.Splits[i].Ratio <= 0 {
			sp.equalizeRatios()
			return
		}
	}
}

func (sp *SplitPane) equalizeRatios() {
	for i := range sp.Splits {
		sp.Splits[i].Ratio = 1
	}
}

// Equalize resets all panes to an equal share.
func (sp *SplitPane) Equalize() {
	sp.equalizeRatios()
	sp.recalcWidths()
}

// AdjustRatio moves the divider between pane leftIdx and leftIdx+1 by
// deltaCells (positive grows the left/top pane), clamped so neither adjacent
// pane drops below the minimum size.
func (sp *SplitPane) AdjustRatio(leftIdx, deltaCells int) {
	n := len(sp.Splits)
	if leftIdx < 0 || leftIdx >= n-1 {
		return
	}
	sp.ensureRatios()
	available := sp.axisAvailable()
	if available <= 0 {
		return
	}
	perCell := sp.ratioSum() / float64(available)
	minRatio := float64(sp.minCells()) * perCell
	delta := float64(deltaCells) * perCell

	l := &sp.Splits[leftIdx].Ratio
	r := &sp.Splits[leftIdx+1].Ratio
	if *l+delta < minRatio {
		delta = minRatio - *l
	}
	if *r-delta < minRatio {
		delta = *r - minRatio
	}
	*l += delta
	*r -= delta
	sp.recalcWidths()
}

// axisCells is pane i's current size along the split axis.
func (sp *SplitPane) axisCells(i int) int {
	if sp.Orientation == SplitHorizontal {
		return sp.Splits[i].Terminal.Height
	}
	return sp.Splits[i].Terminal.Width
}

// SetDividerAt positions divider idx (between pane idx and idx+1) so the
// left/top pane spans up to axisPos cells measured from the start of the axis.
// Only the two adjacent panes' ratios change — their combined share is held
// constant, so the rest of the layout is untouched. Clamped to minimum sizes.
func (sp *SplitPane) SetDividerAt(idx, axisPos int) {
	n := len(sp.Splits)
	if idx < 0 || idx >= n-1 {
		return
	}
	sp.ensureRatios()

	start := 0
	for i := 0; i < idx; i++ {
		start += sp.axisCells(i)
	}
	pairCells := sp.axisCells(idx) + sp.axisCells(idx + 1)
	if pairCells <= 0 {
		return
	}
	leftCells := axisPos - start
	min := sp.minCells()
	if leftCells < min {
		leftCells = min
	}
	if leftCells > pairCells-min {
		leftCells = pairCells - min
	}
	if leftCells < 1 {
		leftCells = 1
	}
	pairRatio := sp.Splits[idx].Ratio + sp.Splits[idx+1].Ratio
	sp.Splits[idx].Ratio = pairRatio * float64(leftCells) / float64(pairCells)
	sp.Splits[idx+1].Ratio = pairRatio - sp.Splits[idx].Ratio
	sp.recalcWidths()
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
