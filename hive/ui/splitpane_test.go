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
