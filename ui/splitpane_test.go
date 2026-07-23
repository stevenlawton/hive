package ui

import "testing"

func TestSplitPaneAddRemove(t *testing.T) {
	sp := NewSplitPane()
	sp.SetSize(80, 24)

	sp.AddSplit("main", "hive-workspace")
	if len(sp.Splits) != 1 {
		t.Fatalf("expected 1 split, got %d", len(sp.Splits))
	}
	// Full width for single split (lipgloss Width = total including border)
	if sp.Splits[0].Terminal.Width != 80 {
		t.Errorf("single split should use full width, got %d", sp.Splits[0].Terminal.Width)
	}

	sp.AddSplit("wt:auth", "hive-workspace-wt-auth")
	if len(sp.Splits) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(sp.Splits))
	}
	// 80 - 1 separator = 79, 79/2 = 39
	if sp.Splits[0].Terminal.Width != 39 {
		t.Errorf("expected 39 width for 2-split, got %d", sp.Splits[0].Terminal.Width)
	}

	sp.RemoveSplit("main")
	if len(sp.Splits) != 1 {
		t.Fatalf("expected 1 split after remove, got %d", len(sp.Splits))
	}
}

func TestSplitPaneFocusNavigation(t *testing.T) {
	sp := NewSplitPane()
	sp.SetSize(120, 24)
	sp.AddSplit("a", "ses-a")
	sp.AddSplit("b", "ses-b")
	sp.AddSplit("c", "ses-c")

	if sp.FocusIdx != 0 {
		t.Errorf("expected focus 0, got %d", sp.FocusIdx)
	}

	sp.FocusRight()
	if sp.FocusIdx != 1 {
		t.Errorf("expected focus 1, got %d", sp.FocusIdx)
	}

	sp.FocusRight()
	sp.FocusRight() // should clamp
	if sp.FocusIdx != 2 {
		t.Errorf("expected focus 2 (clamped), got %d", sp.FocusIdx)
	}

	sp.FocusLeft()
	if sp.FocusIdx != 1 {
		t.Errorf("expected focus 1, got %d", sp.FocusIdx)
	}
}

func TestSplitPaneFocusedSplit(t *testing.T) {
	sp := NewSplitPane()
	if sp.FocusedSplit() != nil {
		t.Error("expected nil for empty split pane")
	}

	sp.SetSize(80, 24)
	sp.AddSplit("main", "ses-main")
	split := sp.FocusedSplit()
	if split == nil || split.Label != "main" {
		t.Errorf("expected focused split 'main', got %+v", split)
	}
}

func TestSplitPaneProportional(t *testing.T) {
	sp := NewSplitPane()
	sp.SetSize(80, 24) // vertical, 2 panes → available 79
	sp.AddSplit("a", "sa")
	sp.AddSplit("b", "sb")
	// 2:1 → a=52 (int(79*2/3)), b=remainder 27
	sp.Splits[0].Ratio = 2
	sp.Splits[1].Ratio = 1
	sp.recalcWidths()
	if sp.Splits[0].Terminal.Width != 52 || sp.Splits[1].Terminal.Width != 27 {
		t.Errorf("2:1 vertical: got %d/%d, want 52/27", sp.Splits[0].Terminal.Width, sp.Splits[1].Terminal.Width)
	}
	if sp.Splits[0].Terminal.Width+sp.Splits[1].Terminal.Width != 79 {
		t.Errorf("widths must sum to available 79")
	}
}

func TestSplitPaneProportionalHorizontal(t *testing.T) {
	sp := NewSplitPane()
	sp.Orientation = SplitHorizontal
	sp.SetSize(80, 30) // horizontal, available 30 rows
	sp.AddSplit("a", "sa")
	sp.AddSplit("b", "sb")
	sp.Splits[0].Ratio = 2
	sp.Splits[1].Ratio = 1
	sp.recalcWidths()
	if sp.Splits[0].Terminal.Height != 20 || sp.Splits[1].Terminal.Height != 10 {
		t.Errorf("2:1 horizontal: got %d/%d, want 20/10", sp.Splits[0].Terminal.Height, sp.Splits[1].Terminal.Height)
	}
}

func TestSplitPaneAdjustClamp(t *testing.T) {
	sp := NewSplitPane()
	sp.SetSize(80, 24)
	sp.AddSplit("a", "sa")
	sp.AddSplit("b", "sb")
	// Try to shove the divider far past pane b's minimum (10 cols).
	for i := 0; i < 50; i++ {
		sp.AdjustRatio(0, 2)
	}
	if w := sp.Splits[1].Terminal.Width; w < minSplitCols {
		t.Errorf("pane b clamped below min: got %d, want >= %d", w, minSplitCols)
	}
	if sp.Splits[0].Terminal.Width+sp.Splits[1].Terminal.Width != 79 {
		t.Errorf("widths must still sum to 79")
	}
}

func TestSplitPaneEqualizeAndReset(t *testing.T) {
	sp := NewSplitPane()
	sp.SetSize(80, 24)
	sp.AddSplit("a", "sa")
	sp.AddSplit("b", "sb")
	sp.AdjustRatio(0, 6) // unbalance
	sp.Equalize()
	if sp.Splits[0].Terminal.Width != 39 {
		t.Errorf("Equalize should restore 39, got %d", sp.Splits[0].Terminal.Width)
	}
	// Adding a split also resets ratios to equal.
	sp.AdjustRatio(0, 6)
	sp.AddSplit("c", "sc")
	// 3 panes, available 78 → 26 each (last remainder)
	if sp.Splits[0].Terminal.Width != 26 {
		t.Errorf("AddSplit should reset to equal (26), got %d", sp.Splits[0].Terminal.Width)
	}
}

func TestSplitPaneSetDividerAt(t *testing.T) {
	sp := NewSplitPane()
	sp.SetSize(80, 24) // vertical, available 79
	sp.AddSplit("a", "sa")
	sp.AddSplit("b", "sb")

	sp.SetDividerAt(0, 30) // left pane spans to column 30
	if sp.Splits[0].Terminal.Width != 30 {
		t.Errorf("SetDividerAt(0,30): left=%d want 30", sp.Splits[0].Terminal.Width)
	}
	if sp.Splits[0].Terminal.Width+sp.Splits[1].Terminal.Width != 79 {
		t.Errorf("widths must sum to 79")
	}

	sp.SetDividerAt(0, 2) // below the 10-col minimum → clamped
	if w := sp.Splits[0].Terminal.Width; w < minSplitCols {
		t.Errorf("left pane clamped below min: %d", w)
	}

	// A third pane's size is unaffected when dragging the first divider.
	sp.Equalize()
	sp.AddSplit("c", "sc") // 3 panes, equal
	c0 := sp.Splits[2].Terminal.Width
	sp.SetDividerAt(0, 20) // move divider between a and b only
	if sp.Splits[2].Terminal.Width != c0 {
		t.Errorf("third pane changed on unrelated divider drag: %d != %d", sp.Splits[2].Terminal.Width, c0)
	}
}

func TestSplitPaneSessionNames(t *testing.T) {
	sp := NewSplitPane()
	sp.SetSize(120, 24)
	sp.AddSplit("a", "ses-a")
	sp.AddSplit("b", "ses-b")

	names := sp.SessionNames()
	if len(names) != 2 || names[0] != "ses-a" || names[1] != "ses-b" {
		t.Errorf("unexpected session names: %v", names)
	}
}
