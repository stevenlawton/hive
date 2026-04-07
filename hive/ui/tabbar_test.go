package ui

import "testing"

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
