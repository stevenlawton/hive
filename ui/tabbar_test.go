package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTabBarAddRemove(t *testing.T) {
	tb := NewTabBar()
	tb.Add("workspace", "WS")
	tb.Add("polybot", "PB")
	tb.Add("slicewize", "SW")

	if len(tb.Tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(tb.Tabs))
	}
	if tb.ActiveIdx != 0 {
		t.Errorf("expected active 0, got %d", tb.ActiveIdx)
	}

	tb.SetActive(1)
	if tb.Tabs[tb.ActiveIdx].ID != "polybot" {
		t.Errorf("expected polybot active, got %s", tb.Tabs[tb.ActiveIdx].ID)
	}

	tb.Remove("polybot")
	if len(tb.Tabs) != 2 {
		t.Fatalf("expected 2 tabs after remove, got %d", len(tb.Tabs))
	}
	if tb.ActiveIdx > len(tb.Tabs)-1 {
		t.Errorf("active index out of bounds after remove")
	}
}

func TestTabBarNext(t *testing.T) {
	tb := NewTabBar()
	tb.Add("a", "A")
	tb.Add("b", "B")
	tb.Add("c", "C")
	tb.SetActive(2)
	tb.Next()
	if tb.ActiveIdx != 0 {
		t.Errorf("expected wrap to 0, got %d", tb.ActiveIdx)
	}
}

func TestTabBarPrev(t *testing.T) {
	tb := NewTabBar()
	tb.Add("a", "A")
	tb.Add("b", "B")
	tb.Add("c", "C")
	tb.SetActive(0)
	tb.Prev()
	if tb.ActiveIdx != 2 {
		t.Errorf("expected wrap to 2, got %d", tb.ActiveIdx)
	}
}

func TestTabBarFocusOrAdd(t *testing.T) {
	tb := NewTabBar()
	tb.Add("a", "A")
	tb.Add("b", "B")

	tb.FocusOrAdd("a", "A")
	if tb.ActiveIdx != 0 {
		t.Errorf("expected focus on 0, got %d", tb.ActiveIdx)
	}

	tb.FocusOrAdd("c", "C")
	if tb.ActiveIdx != 2 {
		t.Errorf("expected focus on new tab 2, got %d", tb.ActiveIdx)
	}
	if len(tb.Tabs) != 3 {
		t.Errorf("expected 3 tabs, got %d", len(tb.Tabs))
	}
}

func TestTabBarFlashing(t *testing.T) {
	tb := NewTabBar()
	tb.Add("a", "A")
	tb.SetFlashing("a", true)
	if !tb.Tabs[0].Flashing {
		t.Error("expected tab to be flashing")
	}
	tb.SetFlashing("a", false)
	if tb.Tabs[0].Flashing {
		t.Error("expected tab to stop flashing")
	}
}

// A tone must change how a tab looks without changing how wide it is: the
// mouse hit zones are computed from TabWidths, and View and TabWidths pick
// their style through the same helper precisely so they cannot drift.
func TestTabToneDoesNotChangeWidth(t *testing.T) {
	plain := NewTabBar()
	plain.Add("a", "repo-one")
	toned := NewTabBar()
	toned.Add("a", "repo-one")
	toned.Tabs[0].Tone = ToneDanger

	pw, tw := plain.TabWidths(), toned.TabWidths()
	if pw[0] != tw[0] {
		t.Errorf("width changed with tone: %d vs %d", pw[0], tw[0])
	}
	if plain.View() == toned.View() {
		t.Error("a toned tab should render differently from a plain one")
	}
}

func TestTabToneNoneRendersAsBefore(t *testing.T) {
	plain := NewTabBar()
	plain.Add("a", "repo-one")
	toned := NewTabBar()
	toned.Add("a", "repo-one")
	toned.Tabs[0].Tone = ToneNone
	if plain.View() != toned.View() {
		t.Error("ToneNone must be indistinguishable from no tone at all")
	}
}

// Flashing means the session is asking for input right now. That outranks a
// verdict, which is advice about the next few minutes.
func TestFlashingOutranksTone(t *testing.T) {
	tb := NewTabBar()
	tb.Add("a", "repo-one")
	tb.Tabs[0].Tone = ToneDanger
	tb.Tabs[0].Flashing = true

	flashOnly := NewTabBar()
	flashOnly.Add("a", "repo-one")
	flashOnly.Tabs[0].Flashing = true

	if tb.View() != flashOnly.View() {
		t.Error("a flashing tab should render as flashing regardless of tone")
	}
}

func TestTabBarRightStatus(t *testing.T) {
	tb := NewTabBar()
	tb.Add("a", "alpha")
	tb.Width = 60

	plain := tb.View()
	tb.RightStatus = "5h 94% · resets 14:20"
	toned := tb.View()

	if !strings.Contains(toned, "5h 94%") {
		t.Errorf("status not rendered: %q", toned)
	}
	if lipgloss.Width(plain) != lipgloss.Width(toned) {
		t.Errorf("status changed the bar width: %d vs %d", lipgloss.Width(plain), lipgloss.Width(toned))
	}
	// Tab hit zones must be untouched — the status lives in the filler.
	if w := tb.TabWidths(); w[0] != lipgloss.Width(TabActiveStyle.Render(" alpha ")) {
		t.Errorf("tab width changed with a right status: %d", w[0])
	}
}

func TestTabBarRightStatusTooNarrowIsDropped(t *testing.T) {
	tb := NewTabBar()
	tb.Add("a", "alpha")
	tb.Width = 10
	tb.RightStatus = "a very long fleet status that cannot fit"
	got := tb.View()
	if lipgloss.Width(got) > 10 {
		t.Errorf("bar overflowed its width: %d", lipgloss.Width(got))
	}
}
